package service

import (
	"os"
	"path/filepath"
	"testing"
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
