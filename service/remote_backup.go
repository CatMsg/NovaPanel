package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/CatMsg/NovaPanel/database"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var (
	backupOwnerPattern    = regexp.MustCompile(`^[a-f0-9]{32}$`)
	backupArtifactPattern = regexp.MustCompile(`^npb-[0-9]{8}T[0-9]{6}Z-[a-f0-9]{32}\.(db|json)$`)
	backupDigestPattern   = regexp.MustCompile(`^[a-f0-9]{64}$`)
	ErrRemoteBackupBusy   = errors.New("another remote backup operation is running")
)

const remoteBackupTimeout = 3 * time.Minute

type remoteBackupConfig struct {
	Enabled         bool   `json:"enabled"`
	IntervalMinutes int    `json:"intervalMinutes"`
	Retention       int    `json:"retention"`
	Destination     string `json:"destination"`
	Username        string `json:"username"`
	Password        string `json:"password"`
}

// Password is write-only. Nil preserves the saved value; clearing is explicit.
type RemoteBackupSettings struct {
	Enabled         bool    `json:"enabled"`
	IntervalMinutes int     `json:"intervalMinutes"`
	Retention       int     `json:"retention"`
	Destination     string  `json:"destination"`
	Username        string  `json:"username"`
	Password        *string `json:"password,omitempty"`
	ClearPassword   bool    `json:"clearPassword,omitempty"`
	HasPassword     bool    `json:"hasPassword"`
}

type remoteBackupState struct {
	LastOperation    string    `json:"lastOperation"`
	LastStartedAt    time.Time `json:"lastStartedAt"`
	LastFinishedAt   time.Time `json:"lastFinishedAt"`
	LastSuccessAt    time.Time `json:"lastSuccessAt"`
	LastArtifact     string    `json:"lastArtifact"`
	NextDueAt        time.Time `json:"nextDueAt"`
	LastError        string    `json:"lastError"`
	RetentionWarning string    `json:"retentionWarning"`
}

type RemoteBackupStatus struct {
	Settings         RemoteBackupSettings `json:"settings"`
	State            remoteBackupState    `json:"state"`
	Busy             bool                 `json:"busy"`
	SchedulerRunning bool                 `json:"schedulerRunning"`
	Namespace        string               `json:"namespace"`
}

type RemoteBackupManifest struct {
	Format      int       `json:"format"`
	Application string    `json:"application"`
	Owner       string    `json:"owner"`
	Artifact    string    `json:"artifact"`
	CreatedAt   time.Time `json:"createdAt"`
	Size        int64     `json:"size"`
	SHA256      string    `json:"sha256"`
}

type RemoteBackupResult struct {
	Artifact     *RemoteBackupManifest  `json:"artifact,omitempty"`
	Artifacts    []RemoteBackupManifest `json:"artifacts,omitempty"`
	Summary      map[string]int64       `json:"summary,omitempty"`
	Message      string                 `json:"message"`
	RecoveryPath string                 `json:"-"`
	Cleanup      func()                 `json:"-"`
}

type RemoteBackupService struct {
	gate             sync.Mutex
	busy             atomic.Bool
	schedulerRunning atomic.Bool
	settings         SettingService
	newDAV           func(remoteBackupConfig, string) (*backupDAV, error)
	snapshot         func(context.Context, string) (string, func(), error)
}

func NewRemoteBackupService() *RemoteBackupService {
	return &RemoteBackupService{newDAV: newBackupDAV, snapshot: database.CreateDBBackupContext}
}

var remoteBackups = NewRemoteBackupService()

func GetRemoteBackupService() *RemoteBackupService { return remoteBackups }

// The manual import API must acquire this before closing/swapping the database.
// A busy backup fails fast rather than allowing its status writes into a new DB.
func AcquireRemoteBackupRestoreGuard() (func(), error) {
	if !remoteBackups.gate.TryLock() {
		return nil, ErrRemoteBackupBusy
	}
	remoteBackups.busy.Store(true)
	return func() { remoteBackups.busy.Store(false); remoteBackups.gate.Unlock() }, nil
}

func backupRandomID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", errors.New("backup random ID generation failed")
	}
	return hex.EncodeToString(id[:]), nil
}

func (s *RemoteBackupService) load() (remoteBackupConfig, string, remoteBackupState, error) {
	cfg := remoteBackupConfig{IntervalMinutes: 1440, Retention: 7}
	var state remoteBackupState
	raw, err := s.settings.getString("remoteBackupConfig")
	if err != nil {
		return cfg, "", state, errors.New("cannot read remote backup configuration")
	}
	if raw != "" && json.Unmarshal([]byte(raw), &cfg) != nil {
		return cfg, "", state, errors.New("invalid stored backup configuration")
	}
	owner, err := s.settings.getString("remoteBackupOwner")
	if err != nil {
		return cfg, "", state, errors.New("cannot read remote backup owner")
	}
	raw, err = s.settings.getString("remoteBackupState")
	if err != nil {
		return cfg, "", state, errors.New("cannot read remote backup status")
	}
	if raw != "" && json.Unmarshal([]byte(raw), &state) != nil {
		return cfg, "", state, errors.New("invalid stored backup status")
	}
	return cfg, owner, state, nil
}

func publicBackupSettings(c remoteBackupConfig) RemoteBackupSettings {
	return RemoteBackupSettings{Enabled: c.Enabled, IntervalMinutes: c.IntervalMinutes, Retention: c.Retention, Destination: c.Destination, Username: c.Username, HasPassword: c.Password != ""}
}

func (s *RemoteBackupService) Status() (RemoteBackupStatus, error) {
	cfg, owner, state, err := s.load()
	status := RemoteBackupStatus{Settings: publicBackupSettings(cfg), State: state, Busy: s.busy.Load(), SchedulerRunning: s.schedulerRunning.Load()}
	if backupOwnerPattern.MatchString(owner) {
		status.Namespace = "novapanel-" + owner
	}
	return status, err
}

func validateBackupConfig(c remoteBackupConfig) error {
	if c.IntervalMinutes < 15 || c.IntervalMinutes > 43200 {
		return errors.New("backup interval must be 15 to 43200 minutes")
	}
	if c.Retention < 1 || c.Retention > 100 {
		return errors.New("backup retention must be 1 to 100 copies")
	}
	if len(c.Destination) > 2048 || len(c.Username) > 256 || len(c.Password) > 4096 {
		return errors.New("backup setting exceeds size limit")
	}
	if strings.ContainsAny(c.Username, ":\r\n") || strings.ContainsAny(c.Password, "\r\n") {
		return errors.New("invalid backup authentication characters")
	}
	if c.Destination != "" {
		if _, err := backupDestination(c.Destination); err != nil {
			return err
		}
	}
	if c.Enabled && (c.Destination == "" || c.Username == "" || c.Password == "") {
		return errors.New("automatic backup requires destination and authentication")
	}
	return nil
}

func (s *RemoteBackupService) Save(in RemoteBackupSettings) error {
	if !s.gate.TryLock() {
		return ErrRemoteBackupBusy
	}
	defer s.gate.Unlock()
	old, owner, state, err := s.load()
	if err != nil {
		return err
	}
	cfg := remoteBackupConfig{Enabled: in.Enabled, IntervalMinutes: in.IntervalMinutes, Retention: in.Retention, Destination: strings.TrimSpace(in.Destination), Username: strings.TrimSpace(in.Username), Password: old.Password}
	if in.ClearPassword {
		cfg.Password = ""
	}
	if in.Password != nil {
		cfg.Password = *in.Password
	}
	if old.Password != "" && (cfg.Destination != old.Destination || cfg.Username != old.Username) && !in.ClearPassword && (in.Password == nil || *in.Password == "") {
		return errors.New("re-enter the password when changing the backup destination or username")
	}
	if err := validateBackupConfig(cfg); err != nil {
		return err
	}
	if owner == "" {
		owner, err = backupRandomID()
		if err != nil {
			return err
		}
	}
	if !backupOwnerPattern.MatchString(owner) {
		return errors.New("invalid stored backup owner")
	}
	// Saving never initiates network I/O. First automatic upload is one full
	// interval after enabling, and failed attempts also wait a full interval.
	state.NextDueAt = time.Time{}
	if cfg.Enabled {
		state.NextDueAt = time.Now().UTC().Add(time.Duration(cfg.IntervalMinutes) * time.Minute)
	}
	configJSON, _ := json.Marshal(cfg)
	stateJSON, _ := json.Marshal(state)
	err = retryWriteTx(func(tx *gorm.DB) error {
		// Debug SQL logging must not interpolate the write-only password.
		tx = tx.Session(&gorm.Session{Logger: gormlogger.Discard})
		for key, value := range map[string]string{"remoteBackupConfig": string(configJSON), "remoteBackupOwner": owner, "remoteBackupState": string(stateJSON)} {
			if err := s.settings.saveSettingTx(tx, key, value); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return errors.New("cannot save remote backup settings")
	}
	return nil
}

func (s *RemoteBackupService) saveState(state remoteBackupState) error {
	raw, _ := json.Marshal(state)
	if err := s.settings.saveSetting("remoteBackupState", string(raw)); err != nil {
		return errors.New("cannot persist remote backup status")
	}
	return nil
}

func (s *RemoteBackupService) Run(ctx context.Context, action, artifact string) (*RemoteBackupResult, error) {
	return s.run(ctx, action, artifact, false)
}

func (s *RemoteBackupService) run(ctx context.Context, action, artifact string, scheduled bool) (*RemoteBackupResult, error) {
	if !s.gate.TryLock() {
		return nil, ErrRemoteBackupBusy
	}
	defer s.gate.Unlock()
	s.busy.Store(true)
	defer s.busy.Store(false)
	cfg, owner, state, err := s.load()
	if err != nil {
		return nil, err
	}
	if scheduled && (!cfg.Enabled || state.NextDueAt.IsZero() || time.Now().Before(state.NextDueAt)) {
		return nil, nil
	}
	if err := validateBackupConfig(cfg); err != nil {
		return nil, err
	}
	if cfg.Destination == "" || cfg.Username == "" || cfg.Password == "" || !backupOwnerPattern.MatchString(owner) {
		return nil, errors.New("configure and save a backup destination and authentication first")
	}
	switch action {
	case "test", "backup", "list", "rehearse", "download":
	default:
		return nil, errors.New("unknown remote backup operation")
	}
	ctx, cancel := context.WithTimeout(ctx, remoteBackupTimeout)
	defer cancel()
	state.LastOperation, state.LastStartedAt, state.LastError = action, time.Now().UTC(), ""
	if action == "backup" {
		state.NextDueAt = time.Time{}
		if cfg.Enabled {
			state.NextDueAt = state.LastStartedAt.Add(time.Duration(cfg.IntervalMinutes) * time.Minute)
		}
	}
	if err := s.saveState(state); err != nil {
		return nil, err
	}
	dav, err := s.newDAV(cfg, owner)
	var result *RemoteBackupResult
	if err == nil {
		defer dav.close()
		switch action {
		case "test":
			result, err = s.testDestination(ctx, dav)
		case "backup":
			result, err = s.backup(ctx, dav, owner, cfg.Retention, &state)
		case "list":
			var artifacts []RemoteBackupManifest
			artifacts, err = s.manifests(ctx, dav, owner)
			result = &RemoteBackupResult{Artifacts: artifacts, Message: "Only this instance's committed artifacts are listed"}
		case "rehearse", "download":
			result, err = s.rehearse(ctx, dav, owner, artifact, action == "download")
		}
	}
	state.LastFinishedAt = time.Now().UTC()
	if err != nil {
		state.LastError = err.Error()
	}
	if statusErr := s.saveState(state); statusErr != nil {
		if result != nil && result.Cleanup != nil {
			result.Cleanup()
		}
		return nil, errors.Join(err, statusErr)
	}
	return result, err
}

func (s *RemoteBackupService) testDestination(ctx context.Context, d *backupDAV) (*RemoteBackupResult, error) {
	if err := d.ensure(ctx); err != nil {
		return nil, err
	}
	// A connection test intentionally uploads no database or credentials file.
	id, err := backupRandomID()
	if err != nil {
		return nil, err
	}
	name := "npb-" + time.Now().UTC().Format("20060102T150405Z") + "-" + id + ".db"
	defer d.cleanupStaging("." + name + ".upload")
	marker := []byte("NovaPanel WebDAV capability probe\n")
	if err := d.put(ctx, "."+name+".upload", bytes.NewReader(marker)); err != nil {
		return nil, err
	}
	if err := d.move(ctx, "."+name+".upload", name); err != nil {
		return nil, err
	}
	var got bytes.Buffer
	readErr := d.read(ctx, name, int64(len(marker)), &got)
	deleteErr := d.delete(ctx, name)
	if readErr != nil {
		return nil, readErr
	}
	if deleteErr != nil {
		return nil, deleteErr
	}
	if !bytes.Equal(marker, got.Bytes()) {
		return nil, errors.New("WebDAV probe read-back mismatch")
	}
	return &RemoteBackupResult{Message: "Authenticated PUT, MOVE, GET, listing and exact-file DELETE verified; no database uploaded"}, nil
}

func readManifest(ctx context.Context, d *backupDAV, owner, name string) (RemoteBackupManifest, error) {
	var m RemoteBackupManifest
	if !backupArtifactPattern.MatchString(name) || !strings.HasSuffix(name, ".json") {
		return m, errors.New("invalid backup artifact selection")
	}
	var raw bytes.Buffer
	if err := d.read(ctx, name, 16<<10, &raw); err != nil {
		return m, err
	}
	if json.Unmarshal(raw.Bytes(), &m) != nil {
		return m, errors.New("invalid backup manifest")
	}
	expected := strings.TrimSuffix(name, ".json") + ".db"
	if m.Format != 1 || m.Application != "NovaPanel" || m.Owner != owner || m.Artifact != expected || m.Size < 1 || m.Size > remoteBackupMaxSize || !backupDigestPattern.MatchString(m.SHA256) || m.CreatedAt.IsZero() {
		return m, errors.New("backup manifest ownership, format, size, or checksum is invalid")
	}
	if "npb-"+m.CreatedAt.UTC().Format("20060102T150405Z")+"-" != expected[:21] {
		return m, errors.New("backup manifest timestamp mismatch")
	}
	return m, nil
}

func (s *RemoteBackupService) manifests(ctx context.Context, d *backupDAV, owner string) ([]RemoteBackupManifest, error) {
	names, err := d.list(ctx)
	if err != nil {
		return nil, err
	}
	manifests := make([]RemoteBackupManifest, 0)
	for _, name := range names {
		m, err := readManifest(ctx, d, owner, name)
		if err != nil {
			return nil, err
		} // Fail closed: no retention on ambiguous inventory.
		manifests = append(manifests, m)
	}
	return manifests, nil
}

func (s *RemoteBackupService) backup(ctx context.Context, d *backupDAV, owner string, retention int, state *remoteBackupState) (*RemoteBackupResult, error) {
	if err := d.ensure(ctx); err != nil {
		return nil, err
	}
	path, cleanup, err := s.snapshot(ctx, "")
	if err != nil {
		return nil, errors.New("private SQLite snapshot failed")
	}
	defer cleanup()
	if _, err := database.RehearseDB(ctx, path); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot open private snapshot")
	}
	defer f.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(f, remoteBackupMaxSize+1))
	if err != nil || size < 1 || size > remoteBackupMaxSize {
		return nil, errors.New("snapshot unreadable or exceeds 1 GiB limit")
	}
	id, err := backupRandomID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	name := "npb-" + now.Format("20060102T150405Z") + "-" + id + ".db"
	defer d.cleanupStaging("." + name + ".upload")
	m := RemoteBackupManifest{Format: 1, Application: "NovaPanel", Owner: owner, Artifact: name, CreatedAt: now, Size: size, SHA256: hex.EncodeToString(hash.Sum(nil))}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, errors.New("cannot rewind private snapshot")
	}
	if err := d.put(ctx, "."+name+".upload", f); err != nil {
		return nil, err
	}
	if err := d.move(ctx, "."+name+".upload", name); err != nil {
		return nil, err
	}
	// Verify remote bytes, not just a successful PUT or Content-Length.
	if err := verifyRemoteBytes(ctx, d, m, io.Discard); err != nil {
		return nil, err
	}
	manifestName := strings.TrimSuffix(name, ".db") + ".json"
	defer d.cleanupStaging("." + manifestName + ".upload")
	raw, _ := json.Marshal(m)
	if err := d.put(ctx, "."+manifestName+".upload", bytes.NewReader(raw)); err != nil {
		return nil, err
	}
	// The manifest is the commit marker and is published last.
	if err := d.move(ctx, "."+manifestName+".upload", manifestName); err != nil {
		return nil, err
	}
	confirmed, err := readManifest(ctx, d, owner, manifestName)
	if err != nil || confirmed != m {
		return nil, errors.New("published manifest read-back failed")
	}
	state.LastSuccessAt, state.LastArtifact, state.RetentionWarning = time.Now().UTC(), name, ""
	if err := s.saveState(*state); err != nil {
		return nil, err
	}
	if err := s.retain(ctx, d, owner, retention, name); err != nil {
		state.RetentionWarning = err.Error()
	}
	return &RemoteBackupResult{Artifact: &m, Message: "Snapshot uploaded and SHA-256 verified; manifest published atomically"}, nil
}

func verifyRemoteBytes(ctx context.Context, d *backupDAV, m RemoteBackupManifest, out io.Writer) error {
	h := sha256.New()
	counter := &backupCountingWriter{writer: io.MultiWriter(out, h)}
	if err := d.read(ctx, m.Artifact, m.Size, counter); err != nil {
		return err
	}
	if counter.n != m.Size || hex.EncodeToString(h.Sum(nil)) != m.SHA256 {
		return errors.New("backup size or SHA-256 mismatch")
	}
	return nil
}

type backupCountingWriter struct {
	writer io.Writer
	n      int64
}

func (w *backupCountingWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	w.n += int64(n)
	return n, err
}

func (s *RemoteBackupService) retain(ctx context.Context, d *backupDAV, owner string, keep int, current string) error {
	if keep < 1 || keep > 100 {
		return errors.New("invalid retention bound")
	}
	artifacts, err := s.manifests(ctx, d, owner)
	if err != nil {
		return fmt.Errorf("retention skipped: %w", err)
	}
	// Always preserve the newly confirmed snapshot, even if the remote listing
	// is stale or its timestamps were changed. Never delete orphan/temp files.
	kept := 1
	for _, m := range artifacts {
		if m.Artifact == current {
			continue
		}
		if kept < keep {
			kept++
			continue
		}
		manifestName := strings.TrimSuffix(m.Artifact, ".db") + ".json"
		if err := d.delete(ctx, manifestName); err != nil {
			return fmt.Errorf("retention stopped: %w", err)
		}
		if err := d.delete(ctx, m.Artifact); err != nil {
			return fmt.Errorf("retention stopped: %w", err)
		}
	}
	return nil
}

func (s *RemoteBackupService) rehearse(ctx context.Context, d *backupDAV, owner, artifact string, download bool) (*RemoteBackupResult, error) {
	if !backupArtifactPattern.MatchString(artifact) || !strings.HasSuffix(artifact, ".db") {
		return nil, errors.New("select a committed backup artifact")
	}
	m, err := readManifest(ctx, d, owner, strings.TrimSuffix(artifact, ".db")+".json")
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", ".novapanel-rehearsal-*")
	if err != nil {
		return nil, errors.New("cannot create private rehearsal directory")
	}
	path := filepath.Join(dir, "snapshot.db")
	cleanup := func() { _ = os.Remove(path); _ = os.Remove(dir) }
	keep := false
	defer func() {
		if !keep {
			cleanup()
		}
	}()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, errors.New("cannot create private rehearsal file")
	}
	err = verifyRemoteBytes(ctx, d, m, f)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return nil, errors.New("rehearsal download failed size/checksum/disk validation")
	}
	summary, err := database.RehearseDB(ctx, path)
	if err != nil {
		return nil, err
	}
	result := &RemoteBackupResult{Artifact: &m, Summary: summary, Message: "Size, SHA-256, SQLite quick_check and required schema passed; live database untouched"}
	if download {
		result.RecoveryPath, result.Cleanup, keep = path, cleanup, true
	}
	return result, nil
}

var backupScheduler struct {
	sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// Start/Stop belong to the panel lifecycle, not the core/data-plane lifecycle.
func StartRemoteBackupScheduler() {
	backupScheduler.Lock()
	defer backupScheduler.Unlock()
	if backupScheduler.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	backupScheduler.cancel, backupScheduler.done = cancel, make(chan struct{})
	done := backupScheduler.done
	remoteBackups.schedulerRunning.Store(true)
	go func() {
		defer close(done)
		defer remoteBackups.schedulerRunning.Store(false)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				remoteBackups.tick(ctx)
			}
		}
	}()
}

func StopRemoteBackupScheduler() {
	backupScheduler.Lock()
	defer backupScheduler.Unlock()
	if backupScheduler.cancel == nil {
		return
	}
	backupScheduler.cancel()
	<-backupScheduler.done
	backupScheduler.cancel, backupScheduler.done = nil, nil
}

func (s *RemoteBackupService) tick(ctx context.Context) {
	cfg, _, state, err := s.load()
	if err != nil || !cfg.Enabled || state.NextDueAt.IsZero() || time.Now().Before(state.NextDueAt) {
		return
	}
	_, _ = s.run(ctx, "backup", "", true) // run persists its bounded, redacted outcome.
}
