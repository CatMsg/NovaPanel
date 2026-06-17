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
