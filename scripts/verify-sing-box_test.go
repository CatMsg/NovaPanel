//go:build ignore

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestExpectedNetworkPatch(t *testing.T) {
	original := []byte("\t\"sync\"\n\tstarted                  bool\n\t\tr.started = true\n\tif !r.started {\n")
	expected := []byte("\t\"sync\"\n\t\"sync/atomic\"\n\tstarted                  atomic.Bool\n\t\tr.started.Store(true)\n\tif !r.started.Load() {\n")
	patched, err := expectedNetworkPatch(original)
	if err != nil || !bytes.Equal(patched, expected) {
		t.Fatalf("patch=%q, error=%v", patched, err)
	}
	for _, drifted := range [][]byte{
		bytes.Replace(original, []byte("bool"), []byte("int"), 1),
		append(bytes.Clone(original), []byte("\t\tr.started = true\n")...),
		patched,
	} {
		if _, err := expectedNetworkPatch(drifted); err == nil {
			t.Fatalf("accepted missing, duplicate, or already-patched context: %q", drifted)
		}
	}
}

func TestVerifySourceTreeRejectsDrift(t *testing.T) {
	expected := map[string][]byte{
		"route/network.go":                        []byte("atomic patch"),
		"LICENSE":                                 []byte("upstream notice"),
		"common/certificate/chrome.pem":           []byte("public certificate"),
		"common/windivert/assets/WinDivert64.sys": []byte("embedded driver"),
	}
	for _, test := range []struct {
		name, path string
		data       []byte
		remove     bool
	}{
		{name: "unchanged"},
		{name: "production-change", path: "route/network.go", data: []byte("unrelated change")},
		{name: "missing-license", path: "LICENSE", remove: true},
		{name: "missing-public-resource", path: "common/certificate/chrome.pem", remove: true},
		{name: "missing-embed", path: "common/windivert/assets/WinDivert64.sys", remove: true},
		{name: "extra-source", path: "route/extra.go", data: []byte("package route")},
		{name: "missing-regression", path: singBoxTest, remove: true},
		{name: "empty-regression", path: singBoxTest},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			write := func(name string, data []byte) {
				t.Helper()
				path := filepath.Join(root, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			for name, data := range expected {
				write(name, data)
			}
			write(singBoxTest, []byte("package route"))
			if test.path != "" {
				if test.remove {
					if err := os.Remove(filepath.Join(root, filepath.FromSlash(test.path))); err != nil {
						t.Fatal(err)
					}
				} else {
					write(test.path, test.data)
				}
			}
			if err := verifySourceTree(root, expected); (err != nil) != (test.name != "unchanged") {
				t.Fatalf("verify error=%v for %s", err, test.name)
			}
		})
	}
}
