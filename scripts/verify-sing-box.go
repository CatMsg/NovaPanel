//go:build ignore

package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/sumdb/dirhash"
	"gopkg.in/yaml.v3"
)

const (
	singBoxModule  = "github.com/sagernet/sing-box"
	singBoxVersion = "v1.15.0-alpha.13"
	singBoxSum     = "h1:O6728RBadOUoC186Lsky8Mt0f6Ulca0XcM2EQFVOtEQ="
	singBoxModSum  = "h1:ZammbTArdjjUecHmXdXatOHMWI4lZNgKh/i+yvY+rng="
	singBoxZipSHA  = "8a037ba62bde6b1fd70bde56fee523e90ca36aa9d36d62fb6f0d3fa4740d0b7e"
	singBoxTest    = "route/network_started_test.go"
	singBoxTestSHA = "2f7b8eb0aa76ccc1348dca086e1ee8bd4eb68d360041a030fc8592b58898c0c2"
	masquePatch    = "docs/patches/sing-box-v1.15.0-alpha.13-masque-integration.patch"
	masquePatchSHA = "790ea3fb2fe68faa77ff84b32037c63fb9e55f214afb6af01cd33fc45a8db6fa"
)

type sourcePatchPin struct {
	path, baselineSHA, patchedSHA string
}

// Only these reviewed hooks and the URLTest selection race fix may differ.
var masquePatchPins = [3]sourcePatchPin{
	{"protocol/masque/server.go", "9403cf920cf5c6cfa7b35aaa77924298037125b3ad8c1bacba12210ddb9cd15a", "3269efd045bedc4cc7f800abda8f3b78d001c519cb1d7870441647956c231163"},
	{"transport/masque/server.go", "eedbba857ea834f38ca5873b039c7ae7a8d7034c37ea70525fbcfb0da197ddbc", "9d293dc4e545292e6995a858f5185453b6f8cda2870f66987e49540b02cc2c04"},
	{"protocol/group/urltest.go", "88ecf28c540d9eeebec0ea6463306d23cdb16231f45d91dd1cd5d57b8073bf98", "bad4a29061ed4505b44aa91444ddc1837c1ac3a2d1f06f28d38b3f8c22f0c8de"},
}

type moduleInfo struct {
	Path, Version, Dir, Zip, Sum, GoModSum string
	Replace                                *moduleInfo
}

func main() {
	archiveSource := flag.String("source", "", "also verify a Git archive's extracted third_party/sing-box directory")
	flag.Parse()
	if err := verifySingBox(*archiveSource); err != nil {
		fmt.Fprintln(os.Stderr, "sing-box verification failed:", err)
		os.Exit(1)
	}
}

func goModuleInfo(args ...string) (moduleInfo, error) {
	var info moduleInfo
	output, err := exec.Command("go", args...).Output()
	if err != nil {
		return info, fmt.Errorf("go %s: %w", strings.Join(args, " "), err)
	}
	if err := json.Unmarshal(output, &info); err != nil {
		return info, err
	}
	return info, nil
}

func verifySingBox(archiveSource string) error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	source := filepath.Join(root, "third_party", "sing-box")
	resolved, err := goModuleInfo("list", "-mod=readonly", "-m", "-json", singBoxModule)
	if err != nil {
		return err
	}
	if resolved.Path != singBoxModule || resolved.Version != singBoxVersion || resolved.Replace == nil ||
		resolved.Replace.Path != "./third_party/sing-box" || filepath.Clean(resolved.Dir) != source {
		return fmt.Errorf("root module must resolve %s %s to %s", singBoxModule, singBoxVersion, source)
	}
	baseline, err := goModuleInfo("mod", "download", "-json", singBoxModule+"@"+singBoxVersion)
	if err != nil {
		return err
	}
	zipBytes, err := os.ReadFile(baseline.Zip)
	if err != nil {
		return err
	}
	expected, err := verifiedOfficialFiles(baseline, zipBytes)
	if err != nil {
		return err
	}
	officialCount := len(expected)
	patchData, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(masquePatch)))
	if err != nil {
		return fmt.Errorf("audited MASQUE patch: %w", err)
	}
	expected, err = verifyAuditedPatch(expected, patchData, masquePatchSHA, masquePatchPins[:])
	if err != nil {
		return err
	}
	testData, err := os.ReadFile(filepath.Join(source, filepath.FromSlash(singBoxTest)))
	if err != nil {
		return err
	}
	if sha256Hex(testData) != singBoxTestSHA {
		return fmt.Errorf("regression test SHA-256 mismatch: %s", singBoxTest)
	}
	if _, exists := expected[singBoxTest]; exists {
		return fmt.Errorf("local regression test collides with official source")
	}
	expected[singBoxTest] = testData
	if err := verifySourceTree(source, expected); err != nil {
		return err
	}
	if err := verifyCronetPins(root); err != nil {
		return err
	}
	// Include untracked, non-ignored files so this gate also works before the
	// initial commit. A fresh checkout has only tracked entries in this list.
	visible, err := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard", "-z", "--", "third_party/sing-box", masquePatch).Output()
	if err != nil {
		return fmt.Errorf("check Git source visibility: %w", err)
	}
	gitFiles := make(map[string]bool)
	for _, path := range strings.Split(string(visible), "\x00") {
		gitFiles[strings.TrimPrefix(path, "third_party/sing-box/")] = true
	}
	for name := range expected {
		if !gitFiles[name] {
			return fmt.Errorf("Git ignores or omits official file %s", name)
		}
	}
	if !gitFiles[singBoxTest] {
		return fmt.Errorf("Git ignores or omits %s", singBoxTest)
	}
	if !gitFiles[masquePatch] {
		return fmt.Errorf("Git ignores or omits audited MASQUE patch %s", masquePatch)
	}
	if archiveSource != "" {
		if err := verifySourceTree(archiveSource, expected); err != nil {
			return fmt.Errorf("Git archive: %w", err)
		}
	}
	fmt.Printf("Verified %s %s: official module sums and ZIP SHA-256 intact; %d official files, audited MASQUE core integration changes and 1 URLTest race fix, %d byte-identical official files, one hash-pinned regression test; patch SHA-256 and exact replay verified; Git-visible source complete; release Cronet pin matches official source and module requirements\n", singBoxModule, singBoxVersion, officialCount, officialCount-len(masquePatchPins))
	return nil
}

func sha256Hex(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func verifiedOfficialFiles(info moduleInfo, zipBytes []byte) (map[string][]byte, error) {
	if info.Path != singBoxModule || info.Version != singBoxVersion || info.Replace != nil || info.Sum != singBoxSum || info.GoModSum != singBoxModSum {
		return nil, fmt.Errorf("official module checksum or identity mismatch")
	}
	if sha256Hex(zipBytes) != singBoxZipSHA {
		return nil, fmt.Errorf("official archive SHA-256 mismatch")
	}
	reader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, err
	}
	expected := make(map[string][]byte, len(reader.File))
	var names []string
	prefix := singBoxModule + "@" + singBoxVersion + "/"
	for _, file := range reader.File {
		name, ok := strings.CutPrefix(file.Name, prefix)
		if !ok || !filepath.IsLocal(filepath.FromSlash(name)) || !file.Mode().IsRegular() {
			return nil, fmt.Errorf("unexpected archive entry %q", file.Name)
		}
		if _, exists := expected[name]; exists {
			return nil, fmt.Errorf("duplicate archive entry %q", file.Name)
		}
		data, err := readZipFile(file)
		if err != nil {
			return nil, err
		}
		expected[name] = data
		names = append(names, file.Name)
	}
	// Recompute both h1 sums from ZIP contents, not just cached download metadata.
	open := func(name string) (io.ReadCloser, error) {
		data, exists := expected[strings.TrimPrefix(name, prefix)]
		if !exists {
			return nil, fmt.Errorf("missing official archive file %s", name)
		}
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	sum, err := dirhash.Hash1(names, open)
	if err != nil || sum != singBoxSum {
		return nil, fmt.Errorf("official module content checksum mismatch: %q (%v)", sum, err)
	}
	modSum, err := dirhash.Hash1([]string{"go.mod"}, open)
	if err != nil || modSum != singBoxModSum {
		return nil, fmt.Errorf("official go.mod content checksum mismatch: %q (%v)", modSum, err)
	}
	return expected, nil
}

func verifyAuditedPatch(baseline map[string][]byte, patch []byte, patchSHA string, pins []sourcePatchPin) (map[string][]byte, error) {
	if sha256Hex(patch) != patchSHA {
		return nil, fmt.Errorf("audited MASQUE patch SHA-256 mismatch")
	}
	if len(pins) != len(masquePatchPins) {
		return nil, fmt.Errorf("audited integration patch must pin exactly three known files")
	}
	for i, pin := range pins {
		if pin.path != masquePatchPins[i].path {
			return nil, fmt.Errorf("unknown or reordered audited MASQUE patch path %q", pin.path)
		}
		original, exists := baseline[pin.path]
		if !exists || sha256Hex(original) != pin.baselineSHA {
			return nil, fmt.Errorf("audited MASQUE baseline SHA-256 mismatch: %s", pin.path)
		}
	}
	return applyExactUnifiedPatch(baseline, patch, pins)
}

var unifiedHunk = regexp.MustCompile(`^@@ -([0-9]+)(?:,([0-9]+))? \+([0-9]+)(?:,([0-9]+))? @@\n$`)

// Replay a deliberately restricted unified diff: fixed ordered file headers,
// exact offsets, counts and context, no fuzzy offsets, renames or file creation.
func applyExactUnifiedPatch(baseline map[string][]byte, patch []byte, pins []sourcePatchPin) (map[string][]byte, error) {
	if len(patch) == 0 || patch[len(patch)-1] != '\n' {
		return nil, fmt.Errorf("audited MASQUE patch must end with a newline")
	}
	lines := strings.SplitAfter(string(patch), "\n")
	lines = lines[:len(lines)-1]
	expected := make(map[string][]byte, len(baseline))
	for name, data := range baseline {
		expected[name] = data
	}
	index := 0
	for _, pin := range pins {
		if index+2 > len(lines) || lines[index] != "--- a/"+pin.path+"\n" || lines[index+1] != "+++ b/"+pin.path+"\n" {
			return nil, fmt.Errorf("audited MASQUE patch must modify only %s in fixed order", pin.path)
		}
		index += 2
		original := string(baseline[pin.path])
		if original == "" || !strings.HasSuffix(original, "\n") {
			return nil, fmt.Errorf("audited MASQUE baseline must be newline-terminated: %s", pin.path)
		}
		sourceLines := strings.SplitAfter(original, "\n")
		sourceLines = sourceLines[:len(sourceLines)-1]
		var result []string
		cursor, hunks := 0, 0
		for index < len(lines) && strings.HasPrefix(lines[index], "@@ ") {
			match := unifiedHunk.FindStringSubmatch(lines[index])
			if match == nil {
				return nil, fmt.Errorf("invalid audited MASQUE hunk header: %s", pin.path)
			}
			var numbers [4]int
			for i, value := range match[1:] {
				if value == "" {
					numbers[i] = 1
					continue
				}
				parsed, err := strconv.Atoi(value)
				if err != nil {
					return nil, fmt.Errorf("invalid audited MASQUE hunk number: %s", pin.path)
				}
				numbers[i] = parsed
			}
			oldStart, oldCount, newStart, newCount := numbers[0], numbers[1], numbers[2], numbers[3]
			if oldCount > 0 {
				oldStart--
			}
			if newCount > 0 {
				newStart--
			}
			if oldStart < cursor || oldStart > len(sourceLines) || oldCount > len(sourceLines)-oldStart {
				return nil, fmt.Errorf("audited MASQUE hunk offset out of range: %s", pin.path)
			}
			result = append(result, sourceLines[cursor:oldStart]...)
			cursor = oldStart
			if newStart != len(result) {
				return nil, fmt.Errorf("audited MASQUE hunk new offset mismatch: %s", pin.path)
			}
			index++
			removed, added := 0, 0
			for removed < oldCount || added < newCount {
				if index == len(lines) || len(lines[index]) < 2 {
					return nil, fmt.Errorf("truncated audited MASQUE hunk: %s", pin.path)
				}
				line := lines[index]
				switch line[0] {
				case ' ', '-':
					if removed >= oldCount || cursor >= len(sourceLines) || sourceLines[cursor] != line[1:] {
						return nil, fmt.Errorf("audited MASQUE hunk context mismatch: %s", pin.path)
					}
					cursor++
					removed++
					if line[0] == ' ' {
						result = append(result, line[1:])
						added++
					}
				case '+':
					result = append(result, line[1:])
					added++
				default:
					return nil, fmt.Errorf("invalid audited MASQUE hunk line: %s", pin.path)
				}
				if added > newCount {
					return nil, fmt.Errorf("audited MASQUE hunk count mismatch: %s", pin.path)
				}
				index++
			}
			hunks++
		}
		result = append(result, sourceLines[cursor:]...)
		patched := []byte(strings.Join(result, ""))
		if hunks == 0 || sha256Hex(patched) != pin.patchedSHA || bytes.Equal(patched, baseline[pin.path]) {
			return nil, fmt.Errorf("audited MASQUE patched SHA-256 mismatch: %s", pin.path)
		}
		expected[pin.path] = patched
	}
	if index != len(lines) {
		return nil, fmt.Errorf("unexpected trailing content in audited MASQUE patch")
	}
	return expected, nil
}

func verifyCronetPins(root string) error {
	const cronetModule = "github.com/sagernet/cronet-go"
	read := func(name string) ([]byte, error) {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return nil, fmt.Errorf("Cronet compatibility: %s: %w", name, err)
		}
		return data, nil
	}
	// verifySourceTree authenticates the byte-identical official pin and go.mod.
	pinData, err := read("third_party/sing-box/.github/CRONET_GO_VERSION")
	if err != nil {
		return err
	}
	pin := strings.TrimSpace(string(pinData))
	if decoded, err := hex.DecodeString(pin); err != nil || len(decoded) != 20 {
		return fmt.Errorf("official CRONET_GO_VERSION must be a full 40-character commit: %q", pin)
	}
	workflowData, err := read(".github/workflows/release.yml")
	if err != nil {
		return err
	}
	var workflow struct {
		Env map[string]string `yaml:"env"`
	}
	if err := yaml.Unmarshal(workflowData, &workflow); err != nil {
		return fmt.Errorf("parse release workflow: %w", err)
	}
	if workflow.Env["CRONET_GO_VERSION"] != pin {
		return fmt.Errorf("release CRONET_GO_VERSION %q must match official pin %s", workflow.Env["CRONET_GO_VERSION"], pin)
	}
	versions := make(map[string]string)
	for _, name := range []string{"third_party/sing-box/go.mod", "go.mod"} {
		data, err := read(name)
		if err != nil {
			return err
		}
		file, err := modfile.Parse(name, data, nil)
		if err != nil {
			return fmt.Errorf("parse %s: %w", name, err)
		}
		for _, path := range []string{cronetModule, cronetModule + "/all"} {
			for _, replacement := range file.Replace {
				if replacement.Old.Path == path {
					return fmt.Errorf("%s must not replace %s", name, path)
				}
			}
			var version string
			for _, requirement := range file.Require {
				if requirement.Mod.Path == path {
					version = requirement.Mod.Version
				}
			}
			revision, err := module.PseudoVersionRev(version)
			if err != nil || revision != pin[:12] {
				return fmt.Errorf("%s requires %s %q, which must reference official Cronet commit %s", name, path, version, pin)
			}
			if official, ok := versions[path]; ok && version != official {
				return fmt.Errorf("%s requires %s %s, want official version %s", name, path, version, official)
			}
			versions[path] = version
		}
	}
	return nil
}

func readZipFile(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

func verifySourceTree(source string, expected map[string][]byte) error {
	rootInfo, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !rootInfo.IsDir() {
		return fmt.Errorf("source root must be a real directory: %s", source)
	}
	seen := make(map[string]bool, len(expected)+1)
	err = filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		name, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		name = filepath.ToSlash(name)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("non-regular source file %s", name)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		original, ok := expected[name]
		if !ok {
			return fmt.Errorf("extra source file %s", name)
		}
		if !bytes.Equal(data, original) {
			return fmt.Errorf("source differs from audited expectation: %s", name)
		}
		seen[name] = true
		return nil
	})
	if err != nil {
		return err
	}
	for name := range expected {
		if !seen[name] {
			return fmt.Errorf("missing official source file %s", name)
		}
	}
	if !seen[singBoxTest] {
		return fmt.Errorf("missing regression test %s", singBoxTest)
	}
	return nil
}
