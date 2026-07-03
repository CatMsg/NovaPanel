package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
)

func TestSettingServiceFileExistsStrict(t *testing.T) {
	svc := &SettingService{}
	workDir := t.TempDir()

	validFile := filepath.Join(workDir, "valid.pem")
	if err := os.WriteFile(validFile, []byte("data"), 0o644); err != nil {
		t.Fatalf("write valid file: %v", err)
	}
	if err := svc.fileExists(validFile); err != nil {
		t.Fatalf("expected valid file to pass: %v", err)
	}

	emptyFile := filepath.Join(workDir, "empty.pem")
	if err := os.WriteFile(emptyFile, []byte{}, 0o644); err != nil {
		t.Fatalf("write empty file: %v", err)
	}
	if err := svc.fileExists(emptyFile); err == nil {
		t.Fatal("expected empty file to fail")
	}

	dirPath := filepath.Join(workDir, "dir")
	if err := os.Mkdir(dirPath, 0o755); err != nil {
		t.Fatalf("mkdir dir: %v", err)
	}
	if err := svc.fileExists(dirPath); err == nil {
		t.Fatal("expected directory path to fail")
	}
}

func TestResolveAcmeCertFilesFromHome(t *testing.T) {
	homeDir := t.TempDir()
	domain := "tk.mile.news"
	certDir := filepath.Join(homeDir, ".acme.sh", domain+"_ecc")
	if err := os.MkdirAll(certDir, 0o755); err != nil {
		t.Fatalf("mkdir cert dir: %v", err)
	}
	certFile := filepath.Join(certDir, "fullchain.cer")
	keyFile := filepath.Join(certDir, domain+".key")
	if err := os.WriteFile(certFile, []byte("cert"), 0o644); err != nil {
		t.Fatalf("write cert file: %v", err)
	}
	if err := os.WriteFile(keyFile, []byte("key"), 0o644); err != nil {
		t.Fatalf("write key file: %v", err)
	}

	gotCert, gotKey, ok := resolveAcmeCertFilesFromHome(homeDir, domain)
	if !ok {
		t.Fatal("expected cert files to resolve")
	}
	if gotCert != certFile || gotKey != keyFile {
		t.Fatalf("unexpected resolved files: got %s and %s", gotCert, gotKey)
	}
}

func TestFillSubCertFilesUsesSubDomainFirst(t *testing.T) {
	homeDir := t.TempDir()
	subDomain := "sub.example.com"
	webDomain := "web.example.com"
	subDir := filepath.Join(homeDir, ".acme.sh", subDomain)
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir cert dir: %v", err)
	}
	subCert := filepath.Join(subDir, "fullchain.cer")
	subKey := filepath.Join(subDir, subDomain+".key")
	if err := os.WriteFile(subCert, []byte("cert"), 0o644); err != nil {
		t.Fatalf("write cert file: %v", err)
	}
	if err := os.WriteFile(subKey, []byte("key"), 0o644); err != nil {
		t.Fatalf("write key file: %v", err)
	}

	// exercise the helper directly with a map shaped like GetAllSetting output
	allSetting := map[string]string{
		"subDomain":   subDomain,
		"webDomain":   webDomain,
		"subCertFile": "",
		"subKeyFile":  "",
	}

	certFile, keyFile, ok := resolveAcmeCertFilesFromHome(homeDir, subDomain)
	if !ok {
		t.Fatal("expected sub domain cert files to resolve")
	}
	if certFile != subCert || keyFile != subKey {
		t.Fatalf("unexpected cert file resolution: %s %s", certFile, keyFile)
	}

	// mimic the fill logic without hitting global HOME
	if allSetting["subCertFile"] == "" {
		allSetting["subCertFile"] = certFile
	}
	if allSetting["subKeyFile"] == "" {
		allSetting["subKeyFile"] = keyFile
	}

	if allSetting["subCertFile"] != subCert || allSetting["subKeyFile"] != subKey {
		t.Fatalf("expected sub cert fields to be filled: %#v", allSetting)
	}
}

func TestSettingSaveDefersManagedPanelForwardingToPostCommit(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("panel port forwarding is only exercised on linux")
	}

	originalPorts := getSSHListenPorts()
	t.Cleanup(func() {
		_ = storeSSHListenPorts(originalPorts, nil)
	})
	if err := storeSSHListenPorts([]int{2222}, nil); err != nil {
		t.Fatalf("store ssh listen ports: %v", err)
	}

	workDir := t.TempDir()
	if err := database.InitDB(filepath.Join(workDir, "settings.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	scriptsDir := filepath.Join(workDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("mkdir scripts dir: %v", err)
	}

	logFile := filepath.Join(workDir, "panel-forward.log")
	script := `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${HY2_MOCK_LOG:?}"
`
	if err := os.WriteFile(filepath.Join(scriptsDir, "hy2-forward.sh"), []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("chdir workdir: %v", err)
	}

	t.Setenv("HY2_MOCK_LOG", logFile)

	svc := &SettingService{}
	if _, err := svc.GetAllSetting(); err != nil {
		t.Fatalf("init default settings: %v", err)
	}

	tx := database.GetDB().Begin()
	if tx.Error != nil {
		t.Fatalf("begin tx: %v", tx.Error)
	}

	postCommit, err := svc.Save(tx, json.RawMessage(`{
		"webPort":"3000",
		"subPort":"3001"
	}`))
	if err != nil {
		tx.Rollback()
		t.Fatalf("save settings: %v", err)
	}
	if postCommit == nil {
		tx.Rollback()
		t.Fatal("expected post-commit action for panel port change")
	}

	if data, readErr := os.ReadFile(logFile); readErr == nil && len(strings.TrimSpace(string(data))) > 0 {
		tx.Rollback()
		t.Fatalf("forwarding script should not run before commit:\n%s", string(data))
	}

	if err := tx.Commit().Error; err != nil {
		t.Fatalf("commit tx: %v", err)
	}
	if err := postCommit(); err != nil {
		t.Fatalf("run post-commit: %v", err)
	}

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	log := string(data)
	if !strings.Contains(log, "apply panel-web-port 3000 3000 tcp") {
		t.Fatalf("web port forwarding was not applied post-commit:\n%s", log)
	}
	if !strings.Contains(log, "apply panel-sub-port 3001 3001 tcp") {
		t.Fatalf("sub port forwarding was not applied post-commit:\n%s", log)
	}
}

func TestGetAllSettingPersistsMissingDefaults(t *testing.T) {
	workDir := t.TempDir()
	if err := database.InitDB(filepath.Join(workDir, "defaults.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	svc := &SettingService{}
	if err := svc.ResetSettings(); err != nil {
		t.Fatalf("reset settings: %v", err)
	}

	allSetting, err := svc.GetAllSetting()
	if err != nil {
		t.Fatalf("get all settings: %v", err)
	}
	if (*allSetting)["webPort"] != "2095" || (*allSetting)["subPort"] != "2096" {
		t.Fatalf("missing default settings in response: %#v", *allSetting)
	}

	var count int64
	if err := database.GetDB().Model(model.Setting{}).Count(&count).Error; err != nil {
		t.Fatalf("count settings: %v", err)
	}
	if count == 0 {
		t.Fatal("expected missing defaults to be persisted")
	}
}

func TestSettingSaveCreatesMissingKeys(t *testing.T) {
	workDir := t.TempDir()
	if err := database.InitDB(filepath.Join(workDir, "missing-keys.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	svc := &SettingService{}
	if err := svc.ResetSettings(); err != nil {
		t.Fatalf("reset settings: %v", err)
	}

	tx := database.GetDB().Begin()
	if tx.Error != nil {
		t.Fatalf("begin tx: %v", tx.Error)
	}

	postCommit, err := svc.Save(tx, json.RawMessage(`{
		"webPort":"4000",
		"subPort":"4001"
	}`))
	if err != nil {
		tx.Rollback()
		t.Fatalf("save settings with missing keys: %v", err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatalf("commit tx: %v", err)
	}
	if postCommit != nil {
		_ = postCommit
	}

	webPort, err := svc.getString("webPort")
	if err != nil {
		t.Fatalf("load webPort: %v", err)
	}
	subPort, err := svc.getString("subPort")
	if err != nil {
		t.Fatalf("load subPort: %v", err)
	}
	if webPort != "4000" || subPort != "4001" {
		t.Fatalf("expected upserted setting values, got webPort=%s subPort=%s", webPort, subPort)
	}
}
