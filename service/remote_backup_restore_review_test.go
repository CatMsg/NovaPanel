package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRemoteBackupRestoreValidationPreservesLastGoodArtifact(t *testing.T) {
	for _, invalid := range []struct{ name, value string }{
		{"malformed", "not-json"}, {"null", "null"}, {"object", "{}"},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			t.Setenv("NOVAS_DB_FOLDER", t.TempDir())
			s, dav := remoteBackupFixture(t)
			saveFixtureBackup(t, s, dav, false, 1)
			good, err := s.Run(context.Background(), "backup", "")
			if err != nil {
				t.Fatal(err)
			}
			before, err := s.Status()
			if err != nil {
				t.Fatal(err)
			}
			base := "/dav/novapanel-" + good.Artifact.Owner + "/"
			manifest := strings.TrimSuffix(good.Artifact.Artifact, ".db") + ".json"
			dav.Lock()
			goodDB := bytes.Clone(dav.files[base+good.Artifact.Artifact])
			goodManifest := bytes.Clone(dav.files[base+manifest])
			dav.Unlock()
			if err := s.settings.setString("fleetServers", invalid.value); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Run(context.Background(), "backup", ""); err == nil {
				t.Error("backup accepted a snapshot that actual restore validation rejects")
			}
			after, err := s.Status()
			if err != nil {
				t.Fatal(err)
			}
			if after.Busy || after.State.LastError == "" || after.State.LastArtifact != before.State.LastArtifact || !after.State.LastSuccessAt.Equal(before.State.LastSuccessAt) {
				t.Error("rejected snapshot changed the last successful recovery point or lost its failure status")
			}
			dav.Lock()
			retained := bytes.Equal(goodDB, dav.files[base+good.Artifact.Artifact]) && bytes.Equal(goodManifest, dav.files[base+manifest])
			committed := 0
			for path := range dav.files {
				if strings.HasSuffix(path, ".json") {
					committed++
				}
			}
			dav.Unlock()
			if !retained || committed != 1 {
				t.Error("rejected snapshot deleted the last good artifact or committed a new manifest")
			}
			if live, err := s.settings.getString("fleetServers"); err != nil || live != invalid.value {
				t.Fatal("backup validation modified the live setting")
			}
			// The preserved recovery point must still pass the real restore preview.
			recovery, err := s.Run(context.Background(), "download", good.Artifact.Artifact)
			if err != nil {
				t.Fatal("last good recovery point is no longer downloadable")
			}
			defer recovery.Cleanup()
			file, err := os.Open(recovery.RecoveryPath)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if _, err := database.ValidateDB(file); err != nil {
				t.Fatal("preserved recovery point failed actual restore validation")
			}
		})
	}
}

func TestRemoteBackupRestoreValidationRejectsChecksummedInvalidRecovery(t *testing.T) {
	s, dav := remoteBackupFixture(t)
	saveFixtureBackup(t, s, dav, false, 1)
	if err := s.settings.setString("fleetServers", "[]"); err != nil {
		t.Fatal(err)
	}
	good, err := s.Run(context.Background(), "backup", "")
	if err != nil {
		t.Fatal(err)
	}
	path, cleanup, err := database.CreateDBBackup("")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	conn, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := conn.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Model(&model.Setting{}).Where("key = ?", "fleetServers").Update("value", "not-json").Error; err != nil {
		_ = sqlDB.Close()
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m := *good.Artifact
	digest := sha256.Sum256(data)
	m.Size, m.SHA256 = int64(len(data)), hex.EncodeToString(digest[:])
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	base := "/dav/novapanel-" + m.Owner + "/"
	manifest := strings.TrimSuffix(m.Artifact, ".db") + ".json"
	dav.Lock()
	dav.files[base+m.Artifact], dav.files[base+manifest] = data, raw
	deletedBefore := len(dav.deleted)
	dav.Unlock()
	for _, action := range []string{"rehearse", "download"} {
		result, err := s.Run(context.Background(), action, m.Artifact)
		if result != nil && result.Cleanup != nil {
			result.Cleanup()
		}
		if err == nil {
			t.Errorf("%s accepted a checksummed artifact that actual restore rejects", action)
		}
		if live, err := s.settings.getString("fleetServers"); err != nil || live != "[]" {
			t.Fatal("recovery validation modified the live setting")
		}
	}
	dav.Lock()
	unchanged := bytes.Equal(data, dav.files[base+m.Artifact]) && bytes.Equal(raw, dav.files[base+manifest]) && len(dav.deleted) == deletedBefore
	dav.Unlock()
	if !unchanged {
		t.Fatal("rejected recovery modified the remote artifact or deleted a file")
	}
}
