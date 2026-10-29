//go:build ignore

package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
		"protocol/masque/server.go":               []byte("exact audited protocol hook"),
		"transport/masque/server.go":              []byte("exact audited transport hook"),
		singBoxTest:                               []byte("package route"),
	}
	for _, test := range []struct {
		name, path string
		data       []byte
		remove     bool
		symlink    bool
	}{
		{name: "unchanged"},
		{name: "production-change", path: "route/network.go", data: []byte("unrelated change")},
		{name: "protocol-hook-tamper", path: "protocol/masque/server.go", data: []byte("altered hook")},
		{name: "transport-hook-tamper", path: "transport/masque/server.go", data: []byte("altered hook")},
		{name: "missing-protocol-hook", path: "protocol/masque/server.go", remove: true},
		{name: "missing-transport-hook", path: "transport/masque/server.go", remove: true},
		{name: "missing-license", path: "LICENSE", remove: true},
		{name: "missing-public-resource", path: "common/certificate/chrome.pem", remove: true},
		{name: "missing-embed", path: "common/windivert/assets/WinDivert64.sys", remove: true},
		{name: "extra-source", path: "route/extra.go", data: []byte("package route")},
		{name: "missing-regression", path: singBoxTest, remove: true},
		{name: "empty-regression", path: singBoxTest},
		{name: "changed-regression", path: singBoxTest, data: []byte("package route // changed")},
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

func auditedPatchFixture() (map[string][]byte, []byte, []sourcePatchPin) {
	baseline := map[string][]byte{"LICENSE": []byte("official license\n")}
	var patch strings.Builder
	pins := make([]sourcePatchPin, len(masquePatchPins))
	for i, known := range masquePatchPins {
		original := []byte("package masque\n\nconst hook = false\n")
		patched := []byte("package masque\n\nconst hook = true\n")
		baseline[known.path] = original
		pins[i] = sourcePatchPin{known.path, sha256Hex(original), sha256Hex(patched)}
		fmt.Fprintf(&patch, "--- a/%s\n+++ b/%s\n@@ -1,3 +1,3 @@\n package masque\n \n-const hook = false\n+const hook = true\n", known.path, known.path)
	}
	return baseline, []byte(patch.String()), pins
}

func TestAuditedPatchExactReplayAndTampering(t *testing.T) {
	for _, name := range []string{
		"exact-replay", "patch-byte-tamper", "baseline-byte-tamper", "missing-baseline", "wrong-baseline-pin",
		"wrong-patched-pin", "unknown-allowlist-path", "missing-allowlist-entry", "extra-allowlist-entry",
		"reordered-allowlist", "unknown-header", "removed-file", "created-file", "trailing-file",
		"missing-file-patch", "wrong-context", "fuzzy-offset", "wrong-new-offset", "wrong-count",
		"truncated-hunk", "invalid-hunk", "invalid-line", "integer-overflow", "missing-final-newline",
	} {
		t.Run(name, func(t *testing.T) {
			baseline, patch, pins := auditedPatchFixture()
			patchSHA := sha256Hex(patch)
			replace := func(old, new string) { patch = bytes.Replace(patch, []byte(old), []byte(new), 1) }
			switch name {
			case "patch-byte-tamper":
				replace("true", "TRUE")
			case "baseline-byte-tamper":
				baseline[pins[0].path] = []byte("tampered\n")
			case "missing-baseline":
				delete(baseline, pins[0].path)
			case "wrong-baseline-pin":
				pins[0].baselineSHA = strings.Repeat("0", 64)
			case "wrong-patched-pin":
				pins[0].patchedSHA = strings.Repeat("0", 64)
			case "unknown-allowlist-path":
				pins[0].path = "route/network.go"
			case "missing-allowlist-entry":
				pins = pins[:1]
			case "extra-allowlist-entry":
				pins = append(pins, sourcePatchPin{path: "route/network.go"})
			case "reordered-allowlist":
				pins[0], pins[1] = pins[1], pins[0]
			case "unknown-header":
				replace("--- a/protocol/masque/server.go", "--- a/../../outside")
			case "removed-file":
				replace("+++ b/protocol/masque/server.go", "+++ /dev/null")
			case "created-file":
				replace("--- a/protocol/masque/server.go", "--- /dev/null")
			case "trailing-file":
				patch = append(patch, []byte("--- a/route/network.go\n+++ b/route/network.go\n@@ -1 +1 @@\n-old\n+new\n")...)
			case "missing-file-patch":
				patch = patch[:bytes.Index(patch, []byte("--- a/transport/masque/server.go"))]
			case "wrong-context":
				replace(" package masque", " package changed")
			case "fuzzy-offset":
				replace("@@ -1,3 +1,3 @@", "@@ -2,3 +1,3 @@")
			case "wrong-new-offset":
				replace("@@ -1,3 +1,3 @@", "@@ -1,3 +2,3 @@")
			case "wrong-count":
				replace("@@ -1,3 +1,3 @@", "@@ -1,3 +1,2 @@")
			case "truncated-hunk":
				patch = patch[:bytes.Index(patch, []byte("+const hook = true"))]
			case "invalid-hunk":
				replace("@@ -1,3 +1,3 @@", "@@ INVALID @@")
			case "invalid-line":
				replace(" package masque", "!package masque")
			case "integer-overflow":
				replace("@@ -1,3 +1,3 @@", "@@ -99999999999999999999999999,3 +1,3 @@")
			case "missing-final-newline":
				patch = patch[:len(patch)-1]
			}
			// Authenticate altered syntax in parser-negative cases to exercise the
			// replay layer independently of the artifact integrity guard.
			if name != "patch-byte-tamper" {
				patchSHA = sha256Hex(patch)
			}
			before := make(map[string][]byte)
			for path, data := range baseline {
				before[path] = bytes.Clone(data)
			}
			expected, err := verifyAuditedPatch(baseline, patch, patchSHA, pins)
			if name == "exact-replay" {
				if err != nil {
					t.Fatal(err)
				}
				if len(expected) != len(baseline) || !bytes.Equal(expected["LICENSE"], baseline["LICENSE"]) {
					t.Fatal("patch added/removed files or changed non-approved source")
				}
				for _, pin := range pins {
					if sha256Hex(expected[pin.path]) != pin.patchedSHA {
						t.Fatalf("incorrect replay: %s", pin.path)
					}
				}
			} else if err == nil {
				t.Fatal("tampering accepted")
			}
			for path, data := range before {
				if !bytes.Equal(baseline[path], data) {
					t.Fatalf("replay modified baseline in place: %s", path)
				}
			}
		})
	}
}

func TestExactUnifiedPatchMultipleHunksAndEmptyCounts(t *testing.T) {
	const path = "protocol/masque/server.go"
	for _, test := range []struct {
		name, original, patched, hunks string
	}{
		{"multiple-hunks", "one\ntwo\nthree\nfour\n", "ONE\ntwo\nthree\nFOUR\n", "@@ -1 +1 @@\n-one\n+ONE\n@@ -4 +4 @@\n-four\n+FOUR\n"},
		{"insert-at-start", "one\n", "zero\none\n", "@@ -0,0 +1 @@\n+zero\n"},
		{"insert-at-end", "one\n", "one\ntwo\n", "@@ -1,0 +2 @@\n+two\n"},
		{"delete-at-start", "zero\none\n", "one\n", "@@ -1 +0,0 @@\n-zero\n"},
		{"delete-at-end", "one\ntwo\n", "one\n", "@@ -2 +1,0 @@\n-two\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			baseline := map[string][]byte{path: []byte(test.original)}
			patch := []byte("--- a/" + path + "\n+++ b/" + path + "\n" + test.hunks)
			pins := []sourcePatchPin{{path, sha256Hex([]byte(test.original)), sha256Hex([]byte(test.patched))}}
			got, err := applyExactUnifiedPatch(baseline, patch, pins)
			if err != nil || string(got[path]) != test.patched {
				t.Fatalf("replay = %q, %v", got[path], err)
			}
		})
	}
}

func TestSourceAndArchiveHaveSameExactExpectation(t *testing.T) {
	baseline, patch, pins := auditedPatchFixture()
	expected, err := verifyAuditedPatch(baseline, patch, sha256Hex(patch), pins)
	if err != nil {
		t.Fatal(err)
	}
	expected[singBoxTest] = []byte("package route\n")
	for _, name := range []string{"source", "git-archive"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			for path, data := range expected {
				full := filepath.Join(root, filepath.FromSlash(path))
				if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := verifySourceTree(root, expected); err != nil {
				t.Fatal(err)
			}
			for path, data := range expected {
				full := filepath.Join(root, filepath.FromSlash(path))
				if err := os.WriteFile(full, append(bytes.Clone(data), '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := verifySourceTree(root, expected); err == nil {
					t.Fatalf("accepted tampered %s in %s", path, name)
				}
				if err := os.WriteFile(full, data, 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(full); err != nil {
					t.Fatal(err)
				}
				if err := verifySourceTree(root, expected); err == nil {
					t.Fatalf("accepted missing %s in %s", path, name)
				}
				if err := os.WriteFile(full, data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestOfficialArchiveIntegrity(t *testing.T) {
	info, err := goModuleInfo("mod", "download", "-json", singBoxModule+"@"+singBoxVersion)
	if err != nil {
		t.Fatal(err)
	}
	zipBytes, err := os.ReadFile(info.Zip)
	if err != nil {
		t.Fatal(err)
	}
	files, err := verifiedOfficialFiles(info, zipBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1566 {
		t.Fatalf("official archive file count = %d, want 1566", len(files))
	}
	for _, pin := range masquePatchPins {
		if sha256Hex(files[pin.path]) != pin.baselineSHA {
			t.Fatalf("baseline file pin does not match authenticated ZIP: %s", pin.path)
		}
	}
	for _, name := range []string{"module-path", "module-version", "replacement", "module-sum", "go-mod-sum", "zip-sha"} {
		t.Run(name, func(t *testing.T) {
			altered, data := info, zipBytes
			switch name {
			case "module-path":
				altered.Path = "example.org/other"
			case "module-version":
				altered.Version = "v1.15.0-alpha.12"
			case "replacement":
				altered.Replace = &moduleInfo{Path: "../untrusted"}
			case "module-sum":
				altered.Sum = "h1:untrusted"
			case "go-mod-sum":
				altered.GoModSum = "h1:untrusted"
			case "zip-sha":
				data = bytes.Clone(zipBytes)
				data[len(data)/2] ^= 1
			}
			if _, err := verifiedOfficialFiles(altered, data); err == nil {
				t.Fatal("accepted altered official baseline identity/checksum")
			}
		})
	}
}

func TestVerifySourceRejectsSymlinkRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "source")
	if err := os.Symlink(t.TempDir(), root); err != nil {
		t.Fatal(err)
	}
	if err := verifySourceTree(root, nil); err == nil {
		t.Fatal("accepted symlink source root")
	}
}

func FuzzExactAuditedPatch(f *testing.F) {
	_, patch, _ := auditedPatchFixture()
	f.Add(patch)
	f.Add([]byte("--- a/../../outside\n+++ /dev/null\n"))
	f.Fuzz(func(t *testing.T, patch []byte) {
		baseline, _, pins := auditedPatchFixture()
		expected, err := verifyAuditedPatch(baseline, patch, sha256Hex(patch), pins)
		if err != nil {
			return
		}
		if len(expected) != len(baseline) || !bytes.Equal(expected["LICENSE"], baseline["LICENSE"]) {
			t.Fatal("accepted source addition/removal or unrelated source mutation")
		}
		for _, pin := range pins {
			if sha256Hex(expected[pin.path]) != pin.patchedSHA || sha256Hex(baseline[pin.path]) != pin.baselineSHA {
				t.Fatal("accepted non-exact patch replay or modified baseline")
			}
		}
	})
}

func TestPinnedRepositoryAndGitArchive(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository fixture")
	}
	repository := filepath.Dir(filepath.Dir(filename))
	t.Chdir(repository)
	if err := verifySingBox(""); err != nil {
		t.Fatalf("actual repository: %v", err)
	}
	info, err := goModuleInfo("mod", "download", "-json", singBoxModule+"@"+singBoxVersion)
	if err != nil {
		t.Fatal(err)
	}
	zipBytes, err := os.ReadFile(info.Zip)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := verifiedOfficialFiles(info, zipBytes)
	if err != nil {
		t.Fatal(err)
	}
	official := expected
	patchData, err := os.ReadFile(masquePatch)
	if err != nil {
		t.Fatal(err)
	}
	expected, err = verifyAuditedPatch(expected, patchData, masquePatchSHA, masquePatchPins[:])
	if err != nil {
		t.Fatal(err)
	}
	testData, err := os.ReadFile(filepath.Join("third_party/sing-box", singBoxTest))
	if err != nil {
		t.Fatal(err)
	}
	expected[singBoxTest] = testData
	root := t.TempDir()
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for path, data := range expected {
		write(filepath.Join(root, "third_party/sing-box", filepath.FromSlash(path)), data)
	}
	for _, name := range []string{"go.mod", "go.sum", ".github/workflows/release.yml", masquePatch} {
		data, err := os.ReadFile(filepath.Join(repository, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		write(filepath.Join(root, filepath.FromSlash(name)), data)
	}
	git := func(args ...string) []byte {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = root
		output, err := command.Output()
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return output
	}
	git("init", "--quiet")
	git("add", "--force", "--", ".")
	// A tree object is sufficient for git archive: no commit and no real index use.
	tree := strings.TrimSpace(string(git("write-tree")))
	archiveBytes := git("archive", "--format=zip", tree, "--", "third_party/sing-box")
	archive, err := zip.NewReader(bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
	if err != nil {
		t.Fatal(err)
	}
	archiveRoot := t.TempDir()
	for _, entry := range archive.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		if !filepath.IsLocal(filepath.FromSlash(entry.Name)) || !entry.Mode().IsRegular() {
			t.Fatalf("unexpected Git archive file: %s", entry.Name)
		}
		data, err := readZipFile(entry)
		if err != nil {
			t.Fatal(err)
		}
		write(filepath.Join(archiveRoot, filepath.FromSlash(entry.Name)), data)
	}
	t.Chdir(root)
	archiveSource := filepath.Join(archiveRoot, "third_party/sing-box")
	if err := verifySingBox(archiveSource); err != nil {
		t.Fatalf("temporary repository + actual Git archive: %v", err)
	}
	for _, test := range []struct {
		name, path, data, want string
	}{
		{"protocol-source-tamper", "third_party/sing-box/protocol/masque/server.go", "tampered\n", "source differs"},
		{"transport-source-tamper", "third_party/sing-box/transport/masque/server.go", "tampered\n", "source differs"},
		{"protocol-reverted-to-official", "third_party/sing-box/protocol/masque/server.go", string(official["protocol/masque/server.go"]), "source differs"},
		{"transport-reverted-to-official", "third_party/sing-box/transport/masque/server.go", string(official["transport/masque/server.go"]), "source differs"},
		{"official-source-tamper", "third_party/sing-box/LICENSE", "tampered\n", "source differs"},
		{"regression-tamper", "third_party/sing-box/" + singBoxTest, "tampered\n", "regression test SHA-256"},
		{"patch-artifact-tamper", masquePatch, string(patchData) + "\n", "patch SHA-256"},
		{"cronet-release-tamper", ".github/workflows/release.yml", "env: {}\n", "must match official pin"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(root, filepath.FromSlash(test.path))
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			defer write(path, original)
			write(path, []byte(test.data))
			err = verifySingBox(archiveSource)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
	for _, pin := range masquePatchPins {
		path := filepath.Join(archiveSource, filepath.FromSlash(pin.path))
		write(path, []byte("tampered\n"))
		if err := verifySingBox(archiveSource); err == nil || !strings.Contains(err.Error(), "Git archive: source differs") {
			t.Fatalf("archive accepted tampered approved file %s: %v", pin.path, err)
		}
		write(path, expected[pin.path])
	}
	for _, name := range []string{masquePatch, "third_party/sing-box/LICENSE"} {
		// An existing ignored file is not Git-visible once removed from the index.
		git("rm", "--cached", "--", name)
		write(filepath.Join(root, ".git/info/exclude"), []byte("/"+name+"\n"))
		if err := verifySingBox(archiveSource); err == nil || !strings.Contains(err.Error(), "Git ignores or omits") {
			t.Fatalf("accepted ignored/untracked %s: %v", name, err)
		}
		git("add", "--force", "--", name)
	}
}
