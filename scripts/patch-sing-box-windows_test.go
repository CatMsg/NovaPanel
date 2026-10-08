package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsCompatCheckIsReadOnly(t *testing.T) {
	for _, test := range []struct {
		name, source string
		wantError    bool
	}{
		{"official-alpha4", "myInterfaces = s.interfaceMonitor.MyInterfaces()", false},
		{"legacy", "networkManager.InterfaceMonitor().MyInterface()", true},
		{"unknown", "package systemconfig", true},
		{"mixed", "s.interfaceMonitor.MyInterfaces(); s.interfaceMonitor.MyInterface()", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "source_windows.go")
			if err := os.WriteFile(target, []byte(test.source), 0o444); err != nil {
				t.Fatal(err)
			}
			if err := checkWindowsCompatFile(target); (err != nil) != test.wantError {
				t.Fatalf("check error=%v, want error=%v", err, test.wantError)
			}
			data, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != test.source {
				t.Fatal("compatibility check rewrote source")
			}
			info, err := os.Stat(target)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o444 {
				t.Fatal("compatibility check changed source permissions")
			}
		})
	}
}

func TestWindowsCompatCheckMissingFile(t *testing.T) {
	if err := checkWindowsCompatFile(filepath.Join(t.TempDir(), "missing.go")); err == nil {
		t.Fatal("missing implementation must fail rather than trigger a cache patch")
	}
}
