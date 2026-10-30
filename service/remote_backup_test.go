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
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/util"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeBackupDAV struct {
	sync.Mutex
	files                map[string][]byte
	deleted              []string
	requests             int
	deny                 bool
	failMove             bool
	failManifestMove     bool
	failManifestReadback bool
	hiddenFromListing    map[string]bool
	corruptDB            bool
	redirect             bool
	server               *httptest.Server
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
			if f.hiddenFromListing[name] {
				continue
			}
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
		dest, _ := url.Parse(r.Header.Get("Destination"))
		if f.failMove || (f.failManifestMove && strings.HasSuffix(dest.Path, ".json")) {
			w.WriteHeader(500)
			return
		}
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
		if f.failManifestReadback && strings.HasSuffix(path, ".json") {
			w.WriteHeader(500)
			return
		}
		data, ok := f.files[path]
		if !ok {
			w.WriteHeader(404)
			return
		}
		if f.corruptDB && strings.HasSuffix(path, ".db") {
			data = []byte("corrupt bytes")
		}
		_, _ = w.Write(data)
	case http.MethodHead:
		if _, ok := f.files[path]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
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
	fake := &fakeBackupDAV{files: map[string][]byte{}, hiddenFromListing: map[string]bool{}}
	fake.server = httptest.NewTLSServer(fake)
	t.Cleanup(fake.server.Close)
	svc := NewRemoteBackupService()
	svc.newDAV = func(cfg remoteBackupConfig, owner string) (*backupDAV, error) {
		d, err := newBackupDAV(cfg, owner)
		if err != nil {
			return nil, err
		}
		d.base, err = url.Parse(fake.server.URL + "/dav/novapanel-" + owner + "/")
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
	if err := s.Save(RemoteBackupSettings{Enabled: enabled, IntervalMinutes: 15, Retention: retention, Destination: "https://backup.example/dav/", Username: "test-user", Password: &password}); err != nil {
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

func TestRemoteBackupTargetChangeClearsPreviousTargetStatus(t *testing.T) {
	s, f := remoteBackupFixture(t)
	saveFixtureBackup(t, s, f, false, 3)

	now := time.Now().UTC()
	state := remoteBackupState{
		LastOperation:    "backup",
		LastStartedAt:    now.Add(-2 * time.Minute),
		LastFinishedAt:   now.Add(-time.Minute),
		LastSuccessAt:    now.Add(-time.Minute),
		LastArtifact:     "npb-20261030T120000Z-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.db",
		LastError:        "old destination error",
		RetentionWarning: "old destination retention warning",
	}
	if err := s.saveState(state); err != nil {
		t.Fatal(err)
	}

	status, err := s.Status()
	if err != nil {
		t.Fatal(err)
	}
	status.Settings.Retention = 5
	if err := s.Save(status.Settings); err != nil {
		t.Fatal(err)
	}
	status, err = s.Status()
	if err != nil || status.State.LastSuccessAt != state.LastSuccessAt || status.State.LastArtifact != state.LastArtifact {
		t.Fatalf("non-target setting change lost backup history: %+v, err=%v", status.State, err)
	}

	status.Settings.Destination = "https://new-destination.invalid/dav/"
	newPassword := "new-synthetic-backup-password"
	status.Settings.Password = &newPassword
	if err := s.Save(status.Settings); err != nil {
		t.Fatal(err)
	}
	status, err = s.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !status.State.LastSuccessAt.IsZero() || status.State.LastArtifact != "" || status.State.LastOperation != "" || !status.State.LastStartedAt.IsZero() || !status.State.LastFinishedAt.IsZero() || status.State.LastError != "" || status.State.RetentionWarning != "" {
		t.Fatalf("new remote target retained stale state: %+v", status.State)
	}
	if f.requests != 0 {
		t.Fatalf("saving settings unexpectedly touched WebDAV: %d requests", f.requests)
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

func TestRemoteBackupRetentionFailsClosedForManifestWithoutData(t *testing.T) {
	s, f := remoteBackupFixture(t)
	saveFixtureBackup(t, s, f, false, 1)
	first, err := s.Run(context.Background(), "backup", "")
	if err != nil {
		t.Fatal(err)
	}
	base := "/dav/novapanel-" + first.Artifact.Owner + "/"
	missing := *first.Artifact
	missing.CreatedAt = time.Now().UTC().Add(-48 * time.Hour)
	missing.Artifact = "npb-" + missing.CreatedAt.Format("20060102T150405Z") + "-" + strings.Repeat("e", 32) + ".db"
	manifest, err := json.Marshal(missing)
	if err != nil {
		t.Fatal(err)
	}
	missingManifest := strings.TrimSuffix(missing.Artifact, ".db") + ".json"
	f.files[base+missingManifest] = manifest

	if _, err := s.Run(context.Background(), "backup", ""); err != nil {
		t.Fatalf("new verified backup should survive ambiguous older inventory: %v", err)
	}
	if _, exists := f.files[base+first.Artifact.Artifact]; !exists {
		t.Fatal("retention deleted the previous valid snapshot despite an incomplete manifest")
	}
	status, err := s.Status()
	if err != nil || status.State.RetentionWarning == "" {
		t.Fatalf("missing data artifact should produce a retention warning: %+v %v", status.State, err)
	}
	if _, err := s.Run(context.Background(), "list", ""); err == nil {
		t.Fatal("list accepted a manifest with no database artifact")
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
	for _, mode := range []string{"auth", "corrupt", "move", "manifest-move", "manifest-readback", "redirect"} {
		f.deny = mode == "auth"
		f.corruptDB = mode == "corrupt"
		f.failMove = mode == "move"
		f.failManifestMove = mode == "manifest-move"
		f.failManifestReadback = mode == "manifest-readback"
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
	f.failManifestMove = false
	f.failManifestReadback = false
	f.redirect = false
	for name := range f.files {
		if strings.HasSuffix(name, ".upload") {
			t.Fatalf("owned staging file orphaned: %s", name)
		}
		if strings.HasPrefix(name, base+"npb-") && (strings.HasSuffix(name, ".db") || strings.HasSuffix(name, ".json")) && name != base+first.Artifact.Artifact && name != base+strings.TrimSuffix(first.Artifact.Artifact, ".db")+".json" {
			t.Fatalf("failed attempt left an uncommitted remote artifact: %s", name)
		}
	}
}

func TestRemoteBackupCleansOnlyStaleUncommittedArtifacts(t *testing.T) {
	s, f := remoteBackupFixture(t)
	saveFixtureBackup(t, s, f, false, 10)
	first, err := s.Run(context.Background(), "backup", "")
	if err != nil {
		t.Fatal(err)
	}
	base := "/dav/novapanel-" + first.Artifact.Owner + "/"
	oldTime := time.Now().UTC().Add(-backupOrphanGracePeriod - time.Hour)
	oldStamp := oldTime.Format("20060102T150405Z")
	oldOrphan := "npb-" + oldStamp + "-" + strings.Repeat("a", 32) + ".db"
	oldStage := ".npb-" + oldStamp + "-" + strings.Repeat("b", 32) + ".json.upload"
	recentOrphan := "npb-" + time.Now().UTC().Add(-time.Hour).Format("20060102T150405Z") + "-" + strings.Repeat("c", 32) + ".db"
	f.files[base+oldOrphan] = []byte("uncommitted database")
	f.files[base+oldStage] = []byte("interrupted upload")
	f.files[base+recentOrphan] = []byte("recent database upload")
	f.files[base+"unrelated.txt"] = []byte("not ours")

	committedData := []byte("previous committed recovery point")
	committedHash := sha256.Sum256(committedData)
	committed := *first.Artifact
	committed.Artifact = "npb-" + oldStamp + "-" + strings.Repeat("d", 32) + ".db"
	committed.CreatedAt = oldTime
	committed.Size = int64(len(committedData))
	committed.SHA256 = hex.EncodeToString(committedHash[:])
	manifest, err := json.Marshal(committed)
	if err != nil {
		t.Fatal(err)
	}
	f.files[base+committed.Artifact] = committedData
	committedManifest := base + strings.TrimSuffix(committed.Artifact, ".db") + ".json"
	f.files[committedManifest] = manifest
	// Simulate an eventually consistent PROPFIND result; GET must prevent deletion.
	f.hiddenFromListing[committedManifest] = true

	if _, err := s.Run(context.Background(), "backup", ""); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{oldOrphan, oldStage} {
		if _, exists := f.files[base+name]; exists {
			t.Fatalf("stale incomplete artifact was not cleaned: %s", name)
		}
	}
	for _, name := range []string{recentOrphan, "unrelated.txt", first.Artifact.Artifact, committed.Artifact, strings.TrimSuffix(committed.Artifact, ".db") + ".json"} {
		if _, exists := f.files[base+name]; !exists {
			t.Fatalf("cleanup removed or lost a protected artifact: %s", name)
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

func TestRemoteBackupProbeAndRejectsUnsafeURLs(t *testing.T) {
	s, f := remoteBackupFixture(t)
	saveFixtureBackup(t, s, f, false, 2)
	result, err := s.Run(context.Background(), "test", "")
	if err != nil || !strings.Contains(result.Message, "no database uploaded") {
		t.Fatalf("probe: %v", err)
	}
	if len(f.files) != 0 {
		t.Fatal("probe left remote artifacts")
	}
	for _, raw := range []string{"http://host/dav/", "https://user:pass@host/dav/", "https://host/dav/?token=x", "https://host/a/../b/", "https://host/a%2Fb/", "https://127.0.0.1/dav/", "https://[fd00::1]/dav/"} {
		if _, err := backupDestination(raw); err == nil {
			t.Fatalf("unsafe destination accepted: %s", raw)
		}
	}
}

func TestRemoteBackupStrictTLSRejectsUntrustedCertificate(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	dialer := &net.Dialer{Timeout: time.Second}
	d, err := newBackupDAVWithDialContext(remoteBackupConfig{
		Destination: "https://backup.example/dav/",
		Username:    "test-user",
		Password:    "test-password",
	}, strings.Repeat("a", 32), func(ctx context.Context, network, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, parsed.Host)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer d.close()
	if err := d.ensure(context.Background()); err == nil {
		t.Fatal("untrusted TLS certificate accepted")
	}
	if requests.Load() != 0 {
		t.Fatal("WebDAV request reached a server with an untrusted certificate")
	}
}

func TestPublicRoutableIPFilter(t *testing.T) {
	tests := []struct {
		address string
		public  bool
	}{
		{address: "8.8.8.8", public: true},
		{address: "2606:4700:4700::1111", public: true},
		{address: "10.0.0.1"},
		{address: "100.64.0.1"},
		{address: "127.0.0.1"},
		{address: "169.254.169.254"},
		{address: "192.0.2.1"},
		{address: "198.18.0.1"},
		{address: "::ffff:192.168.1.1"},
		{address: "fc00::1"},
		{address: "fe80::1"},
		{address: "64:ff9b::a9fe:a9fe"},
		{address: "2001:db8::1"},
		{address: "2002:0808:0808::1"},
	}
	for _, test := range tests {
		t.Run(test.address, func(t *testing.T) {
			address, err := netip.ParseAddr(test.address)
			if err != nil {
				t.Fatal(err)
			}
			if got := util.IsPublicRoutableIP(address); got != test.public {
				t.Fatalf("util.IsPublicRoutableIP(%s) = %t, want %t", address, got, test.public)
			}
		})
	}
}

func TestSafeBackupDialContextPinsPublicDNSResults(t *testing.T) {
	public := netip.MustParseAddr("8.8.8.8")
	lookupCalls, dialCalls := 0, 0
	var dialAddress string
	dial := safeBackupDialContext(
		func(context.Context, string, string) ([]netip.Addr, error) {
			lookupCalls++
			return []netip.Addr{public}, nil
		},
		func(_ context.Context, _, address string) (net.Conn, error) {
			dialCalls++
			dialAddress = address
			client, server := net.Pipe()
			_ = server.Close()
			return client, nil
		},
	)
	conn, err := dial(context.Background(), "tcp", "dav.example:443")
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if lookupCalls != 1 || dialCalls != 1 || dialAddress != "8.8.8.8:443" {
		t.Fatalf("lookup=%d dial=%d address=%q", lookupCalls, dialCalls, dialAddress)
	}
}

func TestSafeBackupDialContextRejectsMixedPublicAndPrivateDNS(t *testing.T) {
	lookupCalls, dialCalls := 0, 0
	dial := safeBackupDialContext(
		func(context.Context, string, string) ([]netip.Addr, error) {
			lookupCalls++
			return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("169.254.169.254")}, nil
		},
		func(context.Context, string, string) (net.Conn, error) {
			dialCalls++
			return nil, errors.New("unexpected dial")
		},
	)
	if _, err := dial(context.Background(), "tcp", "dav.example:443"); err == nil {
		t.Fatal("DNS result containing a cloud metadata address was accepted")
	}
	if lookupCalls != 1 || dialCalls != 0 {
		t.Fatalf("lookup=%d dial=%d; expected rejection before any connection", lookupCalls, dialCalls)
	}
}

func TestRemoteBackupReportsPrivateDNSResolution(t *testing.T) {
	var dialCalls atomic.Int32
	d, err := newBackupDAVWithDialContext(remoteBackupConfig{
		Destination: "https://backup.example/dav/",
		Username:    "test-user",
		Password:    "test-password",
	}, strings.Repeat("b", 32), safeBackupDialContext(
		func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		},
		func(context.Context, string, string) (net.Conn, error) {
			dialCalls.Add(1)
			return nil, errors.New("private address must not be dialed")
		},
	))
	if err != nil {
		t.Fatal(err)
	}
	defer d.close()
	if err := d.ensure(context.Background()); err == nil || !strings.Contains(err.Error(), "non-public address") {
		t.Fatalf("private DNS resolution error = %v", err)
	}
	if dialCalls.Load() != 0 {
		t.Fatal("private DNS result reached the dialer")
	}
}

func TestSafeBackupDialContextRejectsPrivateIPLiteral(t *testing.T) {
	lookupCalls, dialCalls := 0, 0
	dial := safeBackupDialContext(
		func(context.Context, string, string) ([]netip.Addr, error) {
			lookupCalls++
			return nil, errors.New("IP literals must not be resolved")
		},
		func(context.Context, string, string) (net.Conn, error) {
			dialCalls++
			return nil, errors.New("private literal must not be dialed")
		},
	)
	if _, err := dial(context.Background(), "tcp", "127.0.0.1:443"); err == nil {
		t.Fatal("private IP literal was accepted")
	}
	if lookupCalls != 0 || dialCalls != 0 {
		t.Fatalf("lookup=%d dial=%d; expected direct rejection", lookupCalls, dialCalls)
	}
}
