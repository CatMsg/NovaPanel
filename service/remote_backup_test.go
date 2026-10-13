package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeBackupDAV struct {
	sync.Mutex
	files     map[string][]byte
	deleted   []string
	requests  int
	deny      bool
	failMove  bool
	corruptDB bool
	redirect  bool
	server    *httptest.Server
}

func (f *fakeBackupDAV) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.Lock()
	defer f.Unlock()
	f.requests++
	user, pass, _ := r.BasicAuth()
	if f.deny || user != "test-user" || pass != "synthetic-backup-password" {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "synthetic-backup-password <private upstream response>")
		return
	}
	if f.redirect {
		w.Header().Set("Location", "https://other.invalid/private?token=synthetic-backup-password")
		w.WriteHeader(307)
		return
	}
	path := r.URL.Path
	switch r.Method {
	case "MKCOL":
		w.WriteHeader(201)
	case "PROPFIND":
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(207)
		_, _ = io.WriteString(w, `<d:multistatus xmlns:d="DAV:">`)
		for name := range f.files {
			_, _ = fmt.Fprintf(w, `<d:response><d:href>%s</d:href></d:response>`, name)
		}
		// Hostile paths must not become requests or retention targets.
		_, _ = io.WriteString(w, `<d:response><d:href>/unrelated/a.db</d:href></d:response><d:response><d:href>https://evil.invalid/npb-20261012T000000Z-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.json</d:href></d:response></d:multistatus>`)
	case "PUT":
		if _, exists := f.files[path]; exists {
			w.WriteHeader(412)
			return
		}
		data, _ := io.ReadAll(io.LimitReader(r.Body, remoteBackupMaxSize+1))
		f.files[path] = data
		w.WriteHeader(201)
	case "MOVE":
		if f.failMove {
			w.WriteHeader(500)
			return
		}
		dest, _ := url.Parse(r.Header.Get("Destination"))
		data, exists := f.files[path]
		if !exists {
			w.WriteHeader(404)
			return
		}
		if _, exists := f.files[dest.Path]; exists {
			w.WriteHeader(412)
			return
		}
		if r.Header.Get("Overwrite") != "F" {
			w.WriteHeader(400)
			return
		}
		f.files[dest.Path] = data
		delete(f.files, path)
		w.WriteHeader(201)
	case "GET":
		data, ok := f.files[path]
		if !ok {
			w.WriteHeader(404)
			return
		}
		if f.corruptDB && strings.HasSuffix(path, ".db") {
			data = []byte("corrupt bytes")
		}
		_, _ = w.Write(data)
	case "DELETE":
		f.deleted = append(f.deleted, path)
		delete(f.files, path)
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}

func remoteBackupFixture(t *testing.T) (*RemoteBackupService, *fakeBackupDAV) {
	t.Helper()
	if err := database.InitDB(filepath.Join(t.TempDir(), "live-test.db")); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.GetDB().DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	fake := &fakeBackupDAV{files: map[string][]byte{}}
	fake.server = httptest.NewTLSServer(fake)
	t.Cleanup(fake.server.Close)
	svc := NewRemoteBackupService()
	svc.newDAV = func(cfg remoteBackupConfig, owner string) (*backupDAV, error) {
		d, err := newBackupDAV(cfg, owner)
		if err != nil {
			return nil, err
		}
		d.client.CloseIdleConnections()
		// Trust this fixture's certificate, never skip verification.
		d.client = fake.server.Client()
		d.client.Timeout = 2 * time.Second
		d.client.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("redirect refused") }
		return d, nil
	}
	return svc, fake
}

func saveFixtureBackup(t *testing.T, s *RemoteBackupService, f *fakeBackupDAV, enabled bool, retention int) {
	t.Helper()
	password := "synthetic-backup-password"
	if err := s.Save(RemoteBackupSettings{Enabled: enabled, IntervalMinutes: 15, Retention: retention, Destination: f.server.URL + "/dav/", Username: "test-user", Password: &password}); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteBackupUnconfiguredStatusAndSecretMasking(t *testing.T) {
	s, f := remoteBackupFixture(t)
	status, err := s.Status()
	if err != nil || status.Settings.Enabled || status.Settings.HasPassword || status.Settings.IntervalMinutes != 1440 {
		t.Fatalf("bad unconfigured status: %+v %v", status, err)
	}
	s.tick(context.Background())
	if f.requests != 0 {
		t.Fatal("unconfigured status or tick touched network")
	}
	saveFixtureBackup(t, s, f, false, 3)
	status, err = s.Status()
	if err != nil || !status.Settings.HasPassword {
		t.Fatal("saved password presence missing")
	}
	raw, _ := json.Marshal(status)
	if bytes.Contains(raw, []byte("synthetic-backup-password")) || bytes.Contains(raw, []byte(`"password"`)) {
		t.Fatal("status exposed password")
	}
	all, err := s.settings.GetAllSetting()
	if err != nil {
		t.Fatal(err)
	}
	for key := range *all {
		if strings.HasPrefix(key, "remoteBackup") {
			t.Fatalf("generic settings exposed %s", key)
		}
	}
	for _, payload := range []string{`{"remoteBackupConfig":"x"}`, `{"maintenanceMode":"true"}`} {
		err := database.GetDB().Transaction(func(tx *gorm.DB) error { _, err := s.settings.Save(tx, json.RawMessage(payload)); return err })
		if err == nil {
			t.Fatalf("generic save accepted %s", payload)
		}
	}
	settings := status.Settings
	settings.Destination = "https://new-destination.invalid/dav/"
	if err := s.Save(settings); err == nil {
		t.Fatal("saved secret could be forwarded to a changed destination")
	}
	if f.requests != 0 {
		t.Fatal("settings save touched network")
	}
}

func TestRemoteBackupSnapshotRehearsalDownloadAndRetention(t *testing.T) {
	s, f := remoteBackupFixture(t)
	saveFixtureBackup(t, s, f, false, 1)
	_, owner, _, _ := s.load()
	base := "/dav/novapanel-" + owner + "/"
	f.files[base+"personal.db"] = []byte("must survive")
	f.files[base+".unknown.upload"] = []byte("must survive")
	var first string
	for i := 0; i < 2; i++ {
		result, err := s.Run(context.Background(), "backup", "")
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = result.Artifact.Artifact
		}
		if bytes.Contains(f.files[base+result.Artifact.Artifact], []byte("synthetic-backup-password")) {
			t.Fatal("backup contains auth bytes")
		}
	}
	listed, err := s.Run(context.Background(), "list", "")
	if err != nil || len(listed.Artifacts) != 1 {
		t.Fatalf("retention/list: %+v %v", listed, err)
	}
	if _, exists := f.files[base+first]; exists {
		t.Fatal("old app-owned backup survived retention")
	}
	if string(f.files[base+"personal.db"]) != "must survive" || string(f.files[base+".unknown.upload"]) != "must survive" {
		t.Fatal("retention touched unrelated files")
	}
	selected := listed.Artifacts[0].Artifact
	before, _ := s.settings.getString("remoteBackupConfig")
	rehearsed, err := s.Run(context.Background(), "rehearse", selected)
	if err != nil || rehearsed.Summary["users"] != 1 {
		t.Fatalf("rehearsal: %+v %v", rehearsed, err)
	}
	after, _ := s.settings.getString("remoteBackupConfig")
	if before != after {
		t.Fatal("rehearsal mutated live config")
	}
	result, err := s.Run(context.Background(), "download", selected)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(result.RecoveryPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("download not private")
	}
	result.Cleanup()
	if _, err := os.Stat(result.RecoveryPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("download cleanup failed")
	}
	for _, name := range f.deleted {
		if !strings.HasPrefix(name, base) || strings.HasSuffix(name, "/") {
			t.Fatalf("unsafe delete: %s", name)
		}
	}
}

func TestRemoteBackupCorruptionAndWrongSchema(t *testing.T) {
	s, f := remoteBackupFixture(t)
	saveFixtureBackup(t, s, f, false, 3)
	result, err := s.Run(context.Background(), "backup", "")
	if err != nil {
		t.Fatal(err)
	}
	m := *result.Artifact
	base := "/dav/novapanel-" + m.Owner + "/"
	manifestName := base + strings.TrimSuffix(m.Artifact, ".db") + ".json"
	original := append([]byte(nil), f.files[base+m.Artifact]...)
	f.files[base+m.Artifact] = []byte("SQLite format 3\x00corrupted")
	if _, err := s.Run(context.Background(), "rehearse", m.Artifact); err == nil {
		t.Fatal("corrupt checksum accepted")
	}
	// A valid checksum alone must not make a different SQLite schema acceptable.
	path := filepath.Join(t.TempDir(), "wrong-schema.db")
	conn, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Exec("CREATE TABLE not_novapanel (id INTEGER)").Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := conn.DB()
	_ = sqlDB.Close()
	wrong, _ := os.ReadFile(path)
	setRemoteArtifact := func(data []byte) {
		h := sha256.Sum256(data)
		m.Size = int64(len(data))
		m.SHA256 = hex.EncodeToString(h[:])
		f.files[base+m.Artifact] = data
		raw, _ := json.Marshal(m)
		f.files[manifestName] = raw
	}
	setRemoteArtifact(wrong)
	if _, err := s.Run(context.Background(), "rehearse", m.Artifact); err == nil || !strings.Contains(err.Error(), "schema") {
		t.Fatalf("wrong schema accepted: %v", err)
	}
	setRemoteArtifact([]byte("not SQLite but checksum matches"))
	if _, err := s.Run(context.Background(), "rehearse", m.Artifact); err == nil || !strings.Contains(err.Error(), "quick_check") {
		t.Fatalf("invalid SQLite accepted: %v", err)
	}
	setRemoteArtifact(original)
	m.Size++
	raw, _ := json.Marshal(m)
	f.files[manifestName] = raw
	if _, err := s.Run(context.Background(), "download", m.Artifact); err == nil {
		t.Fatal("wrong size accepted for recovery download")
	}
	if _, err := s.Run(context.Background(), "rehearse", "../live.db"); err == nil {
		t.Fatal("unsafe artifact accepted")
	}
	var users int64
	if err := database.GetDB().Model(&model.User{}).Count(&users).Error; err != nil || users != 1 {
		t.Fatal("live database no longer usable")
	}
}

func TestRemoteBackupFailureNeverRetainsAndErrorsRedacted(t *testing.T) {
	s, f := remoteBackupFixture(t)
	saveFixtureBackup(t, s, f, false, 1)
	first, err := s.Run(context.Background(), "backup", "")
	if err != nil {
		t.Fatal(err)
	}
	base := "/dav/novapanel-" + first.Artifact.Owner + "/"
	for _, mode := range []string{"auth", "corrupt", "move", "redirect"} {
		f.deny = mode == "auth"
		f.corruptDB = mode == "corrupt"
		f.failMove = mode == "move"
		f.redirect = mode == "redirect"
		_, err := s.Run(context.Background(), "backup", "")
		if err == nil {
			t.Fatalf("%s failure accepted", mode)
		}
		if strings.Contains(err.Error(), "synthetic") || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "https://") {
			t.Fatalf("unredacted error: %v", err)
		}
		status, _ := s.Status()
		if status.Busy || status.State.LastError == "" {
			t.Fatalf("failure not reflected in status: %+v", status)
		}
		if _, ok := f.files[base+first.Artifact.Artifact]; !ok {
			t.Fatal("failed backup deleted old recovery point")
		}
	}
	f.deny = false
	f.corruptDB = false
	f.failMove = false
	f.redirect = false
	for name := range f.files {
		if strings.HasSuffix(name, ".upload") {
			t.Fatalf("owned staging file orphaned: %s", name)
		}
	}
}

func TestRemoteBackupRetentionRejectsUnownedManifest(t *testing.T) {
	s, f := remoteBackupFixture(t)
	saveFixtureBackup(t, s, f, false, 1)
	first, err := s.Run(context.Background(), "backup", "")
	if err != nil {
		t.Fatal(err)
	}
	m := *first.Artifact
	base := "/dav/novapanel-" + m.Owner + "/"
	foreignName := "npb-20200101T000000Z-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.json"
	m.Owner = strings.Repeat("b", 32)
	raw, _ := json.Marshal(m)
	f.files[base+foreignName] = raw
	if _, err := s.Run(context.Background(), "backup", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.files[base+first.Artifact.Artifact]; !ok {
		t.Fatal("ambiguous inventory permitted retention")
	}
	status, _ := s.Status()
	if status.State.RetentionWarning == "" || status.State.LastSuccessAt.IsZero() {
		t.Fatal("retention warning/success not reported separately")
	}
}

func TestRemoteBackupConcurrencyCancellationAndScheduler(t *testing.T) {
	s, f := remoteBackupFixture(t)
	saveFixtureBackup(t, s, f, true, 2)
	s.tick(context.Background())
	if f.requests != 0 {
		t.Fatal("upload occurred before first interval")
	}
	started, release := make(chan struct{}), make(chan struct{})
	originalSnapshot := s.snapshot
	s.snapshot = func(ctx context.Context, exclude string) (string, func(), error) {
		close(started)
		select {
		case <-release:
			return originalSnapshot(ctx, exclude)
		case <-ctx.Done():
			return "", nil, ctx.Err()
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := s.Run(ctx, "backup", ""); done <- err }()
	<-started
	status, err := s.Status()
	if err != nil || !status.Busy {
		t.Fatal("global status blocked or did not show busy")
	}
	if _, err := s.Run(context.Background(), "test", ""); !errors.Is(err, ErrRemoteBackupBusy) {
		t.Fatal("overlapping operation accepted")
	}
	if err := s.Save(status.Settings); !errors.Is(err, ErrRemoteBackupBusy) {
		t.Fatal("settings changed during upload")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled run succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled run hung")
	}
	status, _ = s.Status()
	if status.Busy || status.State.LastError == "" {
		t.Fatal("cancelled status remained busy")
	}
	s.snapshot = originalSnapshot
	_, _, state, _ := s.load()
	state.NextDueAt = time.Now().Add(-time.Minute)
	if err := s.saveState(state); err != nil {
		t.Fatal(err)
	}
	f.deny = true
	s.tick(context.Background())
	count := f.requests
	s.tick(context.Background())
	if f.requests != count {
		t.Fatal("failed scheduled upload retried before next interval")
	}
	status, _ = s.Status()
	if status.State.NextDueAt.Before(time.Now()) || status.State.LastError == "" {
		t.Fatal("scheduled failure status lost")
	}
}

func TestRemoteBackupProbeAndStrictTLS(t *testing.T) {
	s, f := remoteBackupFixture(t)
	saveFixtureBackup(t, s, f, false, 2)
	result, err := s.Run(context.Background(), "test", "")
	if err != nil || !strings.Contains(result.Message, "no database uploaded") {
		t.Fatalf("probe: %v", err)
	}
	if len(f.files) != 0 {
		t.Fatal("probe left remote artifacts")
	}
	// Production client must reject the fixture's self-signed certificate.
	s.newDAV = newBackupDAV
	if _, err := s.Run(context.Background(), "test", ""); err == nil {
		t.Fatal("untrusted TLS certificate accepted")
	}
	for _, raw := range []string{"http://host/dav/", "https://user:pass@host/dav/", "https://host/dav/?token=x", "https://host/a/../b/", "https://host/a%2Fb/"} {
		if _, err := backupDestination(raw); err == nil {
			t.Fatalf("unsafe destination accepted: %s", raw)
		}
	}
}
