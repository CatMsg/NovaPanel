//go:build ignore

package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	singBoxModule  = "github.com/sagernet/sing-box"
	singBoxVersion = "v1.15.0-alpha.4"
	singBoxSum     = "h1:dqPrdSREKapxsSW17ISeVFZF5I1KY3FagBFCsI2LpV4="
	singBoxModSum  = "h1:jtgVcFwVF4XhAlhzoQZkqjLLSnSPizqedVLy8ZQqZEk="
	singBoxZipSHA  = "04d2fe633d88676abda1010460f002b51d7e037a2caa4efa6a5ac609ad4ff473"
	singBoxTest    = "route/network_started_test.go"
)

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
	if baseline.Sum != singBoxSum || baseline.GoModSum != singBoxModSum {
		return fmt.Errorf("official module checksum mismatch")
	}
	zipBytes, err := os.ReadFile(baseline.Zip)
	if err != nil {
		return err
	}
	if fmt.Sprintf("%x", sha256.Sum256(zipBytes)) != singBoxZipSHA {
		return fmt.Errorf("official archive SHA-256 mismatch")
	}
	reader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return err
	}
	expected := make(map[string][]byte, len(reader.File))
	prefix := singBoxModule + "@" + singBoxVersion + "/"
	for _, file := range reader.File {
		name, ok := strings.CutPrefix(file.Name, prefix)
		if !ok || !filepath.IsLocal(filepath.FromSlash(name)) || !file.Mode().IsRegular() {
			return fmt.Errorf("unexpected archive entry %q", file.Name)
		}
		data, err := readZipFile(file)
		if err != nil {
			return err
		}
		if name == "route/network.go" {
			data, err = expectedNetworkPatch(data)
			if err != nil {
				return err
			}
		}
		expected[name] = data
	}
	if err := verifySourceTree(source, expected); err != nil {
		return err
	}
	// Include untracked, non-ignored files so this gate also works before the
	// initial commit. A fresh checkout has only tracked entries in this list.
	visible, err := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard", "-z", "--", "third_party/sing-box").Output()
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
	if archiveSource != "" {
		if err := verifySourceTree(archiveSource, expected); err != nil {
			return fmt.Errorf("Git archive: %w", err)
		}
	}
	fmt.Printf("Verified %s %s: %d official files, four atomic replacements, one regression test; Git-visible source complete\n", singBoxModule, singBoxVersion, len(expected))
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

func expectedNetworkPatch(data []byte) ([]byte, error) {
	for _, edit := range [][2]string{
		{"\t\"sync\"\n", "\t\"sync\"\n\t\"sync/atomic\"\n"},
		{"\tstarted                  bool\n", "\tstarted                  atomic.Bool\n"},
		{"\t\tr.started = true\n", "\t\tr.started.Store(true)\n"},
		{"\tif !r.started {\n", "\tif !r.started.Load() {\n"},
	} {
		if bytes.Count(data, []byte(edit[0])) != 1 {
			return nil, fmt.Errorf("official network.go patch context changed: %q", edit[0])
		}
		data = bytes.Replace(data, []byte(edit[0]), []byte(edit[1]), 1)
	}
	return data, nil
}

func verifySourceTree(source string, expected map[string][]byte) error {
	seen := make(map[string]bool, len(expected)+1)
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
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
		if name != singBoxTest {
			original, ok := expected[name]
			if !ok {
				return fmt.Errorf("extra source file %s", name)
			}
			if !bytes.Equal(data, original) {
				return fmt.Errorf("source differs beyond allowed atomic patch: %s", name)
			}
		} else if len(data) == 0 {
			return fmt.Errorf("empty regression test %s", name)
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
