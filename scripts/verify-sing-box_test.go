//go:build ignore

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyCronetPinsRejectsDrift(t *testing.T) {
	const (
		pin     = "4d18a60dc3a8d327065b058f92fbbfc9391b84bc"
		oldPin  = "0d28acc44093df24b2526dea3d6ffefd6b0a54f0"
		version = "v0.0.0-20260929213745-4d18a60dc3a8"
		pinFile = "third_party/sing-box/.github/CRONET_GO_VERSION"
		modFile = "third_party/sing-box/go.mod"
		wfFile  = ".github/workflows/release.yml"
		cronet  = "github.com/sagernet/cronet-go"
	)
	moduleData := "module example.com/test\n\ngo 1.26.6\n\nrequire (\n\t" + cronet + " " + version + "\n\t" + cronet + "/all " + version + "\n)\n"
	workflowData := "name: release\nenv:\n  CRONET_GO_VERSION: \"" + pin + "\"\njobs: {}\n"
	for _, test := range []struct {
		name, path, data, wantError string
		remove                      bool
	}{
		{name: "aligned"},
		{name: "pin-newline", path: pinFile, data: pin + "\n"},
		{name: "stale-release", path: wfFile, data: strings.ReplaceAll(workflowData, pin, oldPin), wantError: "must match official pin"},
		{name: "same-prefix-wrong-commit", path: wfFile, data: strings.ReplaceAll(workflowData, pin, pin[:12]+strings.Repeat("0", 28)), wantError: "must match official pin"},
		{name: "missing-release-pin", path: wfFile, data: "env: {}\n", wantError: "must match official pin"},
		{name: "duplicate-release-pin", path: wfFile, data: workflowData + "env: {}\n", wantError: "parse release workflow"},
		{name: "invalid-workflow", path: wfFile, data: "env: [\n", wantError: "parse release workflow"},
		{name: "missing-workflow", path: wfFile, remove: true, wantError: wfFile},
		{name: "short-official-pin", path: pinFile, data: pin[:12], wantError: "full 40-character commit"},
		{name: "invalid-official-pin", path: pinFile, data: strings.Repeat("z", 40), wantError: "full 40-character commit"},
		{name: "missing-official-pin", path: pinFile, remove: true, wantError: pinFile},
		{name: "stale-root-module", path: "go.mod", data: strings.ReplaceAll(moduleData, pin[:12], oldPin[:12]), wantError: "go.mod requires " + cronet},
		{name: "stale-official-module", path: modFile, data: strings.ReplaceAll(moduleData, pin[:12], oldPin[:12]), wantError: modFile + " requires " + cronet},
		{name: "stale-all-module", path: "go.mod", data: strings.Replace(moduleData, cronet+"/all "+version, cronet+"/all v0.0.0-20260929213745-"+oldPin[:12], 1), wantError: "go.mod requires " + cronet + "/all"},
		{name: "missing-root-requirement", path: "go.mod", data: strings.Replace(moduleData, "\t"+cronet+" "+version+"\n", "", 1), wantError: "go.mod requires " + cronet},
		{name: "missing-official-all", path: modFile, data: strings.Replace(moduleData, "\t"+cronet+"/all "+version+"\n", "", 1), wantError: modFile + " requires " + cronet + "/all"},
		{name: "non-pseudo-version", path: "go.mod", data: strings.ReplaceAll(moduleData, version, "v1.0.0"), wantError: "must reference official Cronet commit"},
		{name: "same-commit-wrong-version", path: "go.mod", data: strings.ReplaceAll(moduleData, "20260929213745", "20260929213746"), wantError: "want official version"},
		{name: "replaced-root-module", path: "go.mod", data: moduleData + "replace " + cronet + " => ../cronet\n", wantError: "must not replace " + cronet},
		{name: "replaced-all-module", path: "go.mod", data: moduleData + "replace " + cronet + "/all => ../cronet-all\n", wantError: "must not replace " + cronet + "/all"},
		{name: "invalid-module", path: "go.mod", data: "require (\n", wantError: "parse go.mod"},
		{name: "missing-module", path: "go.mod", remove: true, wantError: "go.mod"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			files := map[string]string{pinFile: pin, modFile: moduleData, "go.mod": moduleData, wfFile: workflowData}
			if test.path != "" {
				if test.remove {
					delete(files, test.path)
				} else {
					files[test.path] = test.data
				}
			}
			for name, data := range files {
				path := filepath.Join(root, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			err := verifyCronetPins(root)
			if test.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error=%v, want error containing %q", err, test.wantError)
			}
		})
	}
}

func TestVerifySourceTreeRejectsDrift(t *testing.T) {
	expected := map[string][]byte{
		"route/network.go":                        []byte("official synchronized lifecycle"),
		"LICENSE":                                 []byte("upstream notice"),
		"common/certificate/chrome.pem":           []byte("public certificate"),
		"common/windivert/assets/WinDivert64.sys": []byte("embedded driver"),
	}
	for _, test := range []struct {
		name, path string
		data       []byte
		remove     bool
		symlink    bool
	}{
		{name: "unchanged"},
		{name: "production-change", path: "route/network.go", data: []byte("unrelated change")},
		{name: "missing-license", path: "LICENSE", remove: true},
		{name: "missing-public-resource", path: "common/certificate/chrome.pem", remove: true},
		{name: "missing-embed", path: "common/windivert/assets/WinDivert64.sys", remove: true},
		{name: "extra-source", path: "route/extra.go", data: []byte("package route")},
		{name: "missing-regression", path: singBoxTest, remove: true},
		{name: "empty-regression", path: singBoxTest},
		{name: "symlink-production", path: "route/network.go", data: []byte("official synchronized lifecycle"), symlink: true},
		{name: "symlink-regression", path: singBoxTest, data: []byte("package route"), symlink: true},
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
				if test.symlink {
					target := filepath.Join(t.TempDir(), "target")
					if err := os.WriteFile(target, test.data, 0o644); err != nil {
						t.Fatal(err)
					}
					path := filepath.Join(root, filepath.FromSlash(test.path))
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(target, path); err != nil {
						t.Fatal(err)
					}
				} else if test.remove {
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
