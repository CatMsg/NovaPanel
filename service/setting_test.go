package service

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

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
	if err := writeSelfSignedCert(certFile, keyFile, domain); err != nil {
		t.Fatalf("write cert files: %v", err)
	}

	gotCert, gotKey, ok := resolveAcmeCertFilesFromHome(homeDir, domain)
	if !ok {
		t.Fatal("expected cert files to resolve")
	}
	if gotCert != certFile || gotKey != keyFile {
		t.Fatalf("unexpected resolved files: got %s and %s", gotCert, gotKey)
	}
}

func TestResolveAcmeCertFilesRejectsDomainMismatch(t *testing.T) {
	homeDir := t.TempDir()
	certDomain := "tk.mile.news"
	requestDomain := "la.mile.news"
	certDir := filepath.Join(homeDir, ".acme.sh", certDomain+"_ecc")
	if err := os.MkdirAll(certDir, 0o755); err != nil {
		t.Fatalf("mkdir cert dir: %v", err)
	}

	certFile := filepath.Join(certDir, "fullchain.cer")
	keyFile := filepath.Join(certDir, certDomain+".key")
	if err := writeSelfSignedCert(certFile, keyFile, certDomain); err != nil {
		t.Fatalf("write self-signed cert: %v", err)
	}

	if _, _, ok := resolveAcmeCertFilesFromHome(homeDir, requestDomain); ok {
		t.Fatal("expected mismatched certificate to be rejected")
	}
}

func writeSelfSignedCert(certFile string, keyFile string, domain string) error {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}

	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: domain,
		},
		DNSNames:  []string{domain},
		NotBefore: time.Now().Add(-time.Hour),
		NotAfter:  time.Now().Add(time.Hour),
		KeyUsage:  x509.KeyUsageDigitalSignature,
	}

	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &priv.PublicKey, priv)
	if err != nil {
		return err
	}

	certOut, err := os.Create(certFile)
	if err != nil {
		return err
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		return err
	}

	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return err
	}
	keyOut, err := os.Create(keyFile)
	if err != nil {
		return err
	}
	defer keyOut.Close()
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}); err != nil {
		return err
	}
	return nil
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
	if err := writeSelfSignedCert(subCert, subKey, subDomain); err != nil {
		t.Fatalf("write cert files: %v", err)
	}

	// exercise the helper directly with a map shaped like GetAllSetting output
	allSetting := map[string]string{
		"subDomain":   subDomain,
		"webDomain":   webDomain,
		"subMode":     "master",
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

func TestFillSubCertFilesReplacesMismatchedSubDomainCert(t *testing.T) {
	homeDir := t.TempDir()
	subDomain := "sub.example.com"
	webDomain := "web.example.com"

	subDir := filepath.Join(homeDir, ".acme.sh", subDomain+"_ecc")
	webDir := filepath.Join(homeDir, ".acme.sh", webDomain+"_ecc")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir sub cert dir: %v", err)
	}
	if err := os.MkdirAll(webDir, 0o755); err != nil {
		t.Fatalf("mkdir web cert dir: %v", err)
	}

	subCert := filepath.Join(subDir, "fullchain.cer")
	subKey := filepath.Join(subDir, subDomain+".key")
	webCert := filepath.Join(webDir, "fullchain.cer")
	webKey := filepath.Join(webDir, webDomain+".key")
	if err := writeSelfSignedCert(subCert, subKey, subDomain); err != nil {
		t.Fatalf("write sub cert files: %v", err)
	}
	if err := writeSelfSignedCert(webCert, webKey, webDomain); err != nil {
		t.Fatalf("write web cert files: %v", err)
	}

	allSetting := map[string]string{
		"subDomain":   subDomain,
		"webDomain":   webDomain,
		"subMode":     "master",
		"subCertFile": webCert,
		"subKeyFile":  webKey,
	}

	oldHome := os.Getenv("HOME")
	t.Cleanup(func() { _ = os.Setenv("HOME", oldHome) })
	if err := os.Setenv("HOME", homeDir); err != nil {
		t.Fatalf("set home: %v", err)
	}

	(&SettingService{}).fillSubCertFiles(allSetting)

	if allSetting["subCertFile"] != subCert || allSetting["subKeyFile"] != subKey {
		t.Fatalf("expected mismatched sub cert fields to be replaced: %#v", allSetting)
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

func TestSettingSaveNormalizesPaths(t *testing.T) {
	workDir := t.TempDir()
	if err := database.InitDB(filepath.Join(workDir, "normalize-paths.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	svc := &SettingService{}
	if _, err := svc.GetAllSetting(); err != nil {
		t.Fatalf("init default settings: %v", err)
	}

	tx := database.GetDB().Begin()
	if tx.Error != nil {
		t.Fatalf("begin tx: %v", tx.Error)
	}

	postCommit, err := svc.Save(tx, json.RawMessage(`{
		"webPath":"panel",
		"subPath":"sub"
	}`))
	if err != nil {
		tx.Rollback()
		t.Fatalf("save settings: %v", err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatalf("commit tx: %v", err)
	}
	if postCommit != nil {
		t.Fatal("did not expect post-commit action for path-only changes")
	}

	webPath, err := svc.GetWebPath()
	if err != nil {
		t.Fatalf("get webPath: %v", err)
	}
	subPath, err := svc.GetSubPath()
	if err != nil {
		t.Fatalf("get subPath: %v", err)
	}
	if webPath != "/panel/" || subPath != "/sub/" {
		t.Fatalf("expected normalized paths, got webPath=%s subPath=%s", webPath, subPath)
	}
}

func TestSettingSaveClearsStatsWhenTrafficAgeZero(t *testing.T) {
	workDir := t.TempDir()
	if err := database.InitDB(filepath.Join(workDir, "traffic-age-zero.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	svc := &SettingService{}
	if _, err := svc.GetAllSetting(); err != nil {
		t.Fatalf("init default settings: %v", err)
	}

	stats := []model.Stats{
		{DateTime: 1, Resource: "client", Tag: "a", Direction: true, Traffic: 1},
		{DateTime: 2, Resource: "inbound", Tag: "b", Direction: false, Traffic: 2},
	}
	if err := database.GetDB().Create(&stats).Error; err != nil {
		t.Fatalf("seed stats: %v", err)
	}

	tx := database.GetDB().Begin()
	if tx.Error != nil {
		t.Fatalf("begin tx: %v", tx.Error)
	}

	postCommit, err := svc.Save(tx, json.RawMessage(`{
		"trafficAge":"0"
	}`))
	if err != nil {
		tx.Rollback()
		t.Fatalf("save settings: %v", err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatalf("commit tx: %v", err)
	}
	if postCommit != nil {
		t.Fatal("did not expect post-commit action for trafficAge change")
	}

	var count int64
	if err := database.GetDB().Model(model.Stats{}).Count(&count).Error; err != nil {
		t.Fatalf("count stats: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected stats to be cleared, got %d rows", count)
	}
}

func TestSettingSaveReturnsNoChangesForIdenticalPayload(t *testing.T) {
	workDir := t.TempDir()
	if err := database.InitDB(filepath.Join(workDir, "setting-no-changes.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	svc := &SettingService{}
	if _, err := svc.GetAllSetting(); err != nil {
		t.Fatalf("init default settings: %v", err)
	}

	tx := database.GetDB().Begin()
	if tx.Error != nil {
		t.Fatalf("begin tx: %v", tx.Error)
	}

	postCommit, err := svc.Save(tx, json.RawMessage(`{
		"webPort":"2095",
		"subPort":"2096",
		"webPath":"/app/",
		"subPath":"/sub/"
	}`))
	if err == nil || err != ErrNoChanges {
		tx.Rollback()
		t.Fatalf("expected ErrNoChanges, got postCommitNil=%t err=%v", postCommit == nil, err)
	}
	_ = tx.Rollback()
}

func TestSettingSaveTriggersSubServerRestartForSubDomainChange(t *testing.T) {
	workDir := t.TempDir()
	if err := database.InitDB(filepath.Join(workDir, "setting-sub-restart.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	homeDir := t.TempDir()
	certDir := filepath.Join(homeDir, ".acme.sh", "cn2.example.com_ecc")
	if err := os.MkdirAll(certDir, 0o755); err != nil {
		t.Fatalf("mkdir cert dir: %v", err)
	}
	certFile := filepath.Join(certDir, "fullchain.cer")
	keyFile := filepath.Join(certDir, "cn2.example.com.key")
	if err := writeSelfSignedCert(certFile, keyFile, "cn2.example.com"); err != nil {
		t.Fatalf("write cert files: %v", err)
	}
	oldHome := os.Getenv("HOME")
	t.Cleanup(func() { _ = os.Setenv("HOME", oldHome) })
	if err := os.Setenv("HOME", homeDir); err != nil {
		t.Fatalf("set home: %v", err)
	}

	svc := &SettingService{}
	if _, err := svc.GetAllSetting(); err != nil {
		t.Fatalf("init default settings: %v", err)
	}

	restartCount := 0
	oldRestart := subServerRestartFunc
	subServerRestartFunc = func() error {
		restartCount++
		return nil
	}
	t.Cleanup(func() { subServerRestartFunc = oldRestart })

	tx := database.GetDB().Begin()
	if tx.Error != nil {
		t.Fatalf("begin tx: %v", tx.Error)
	}

	postCommit, err := svc.Save(tx, json.RawMessage(`{
		"subMode":"master",
		"subDomain":"cn2.example.com"
	}`))
	if err != nil {
		tx.Rollback()
		t.Fatalf("save settings: %v", err)
	}
	if postCommit == nil {
		tx.Rollback()
		t.Fatal("expected post-commit action for subDomain change")
	}

	if err := tx.Commit().Error; err != nil {
		t.Fatalf("commit tx: %v", err)
	}
	if err := postCommit(); err != nil {
		t.Fatalf("run post-commit: %v", err)
	}
	if restartCount != 1 {
		t.Fatalf("expected sub server restart once, got %d", restartCount)
	}

	if got, err := svc.GetSubCertFile(); err != nil || got != certFile {
		t.Fatalf("expected sub cert to be auto-filled, got %q err=%v", got, err)
	}
	if got, err := svc.GetSubKeyFile(); err != nil || got != keyFile {
		t.Fatalf("expected sub key to be auto-filled, got %q err=%v", got, err)
	}
}

func TestSaveConfigReturnsNoChangesForIdenticalPayload(t *testing.T) {
	workDir := t.TempDir()
	if err := database.InitDB(filepath.Join(workDir, "config-no-changes.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	svc := &SettingService{}
	if _, err := svc.GetAllSetting(); err != nil {
		t.Fatalf("init default settings: %v", err)
	}

	payload := json.RawMessage(`{"log":{"level":"warn"},"dns":{"servers":[],"rules":[]},"route":{"rules":[]},"experimental":{}}`)

	tx := database.GetDB().Begin()
	if tx.Error != nil {
		t.Fatalf("begin tx: %v", tx.Error)
	}
	if err := svc.SaveConfig(tx, payload); err != nil {
		tx.Rollback()
		t.Fatalf("seed config: %v", err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatalf("commit seed config: %v", err)
	}

	tx = database.GetDB().Begin()
	if tx.Error != nil {
		t.Fatalf("begin tx: %v", tx.Error)
	}

	if err := svc.SaveConfig(tx, payload); err == nil || err != ErrNoChanges {
		tx.Rollback()
		t.Fatalf("expected ErrNoChanges, got %v", err)
	}
	_ = tx.Rollback()
}
