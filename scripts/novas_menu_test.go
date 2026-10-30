package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestSetSettingPassesUserValuesAsSingleArguments(t *testing.T) {
	source, err := os.ReadFile("../novas.sh")
	if err != nil {
		t.Fatal(err)
	}
	function := regexp.MustCompile(`(?ms)^set_setting\(\) \{\n.*?^\}`).Find(source)
	if len(function) == 0 {
		t.Fatal("could not find set_setting function")
	}
	function = bytes.Replace(function, []byte("/usr/local/novas/novas setting"), []byte("mock_novas setting"), 1)
	if !bytes.Contains(function, []byte(`"${params[@]}"`)) {
		t.Fatal("setting arguments must be passed as a quoted Bash array")
	}

	capture := filepath.Join(t.TempDir(), "arguments")
	harness := `#!/bin/bash
set -e
mock_novas() { printf '%s\0' "$@" > "$CAPTURE"; }
restart() { :; }
before_show_menu() { :; }
LOGE() { printf '%s\n' "$*" >&2; }
` + string(function) + `
set_setting
`
	command := exec.Command("bash", "-c", harness)
	command.Env = append(os.Environ(), "CAPTURE="+capture)
	command.Stdin = strings.NewReader("2095 -reset\n/panel path\n3100\n/sub path\n")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("run extracted setting menu: %v\n%s", err, output)
	}

	arguments, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSuffix(string(arguments), "\x00"), "\x00")
	want := []string{"setting", "-port", "2095 -reset", "-path", "/panel path", "-subPort", "3100", "-subPath", "/sub path"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("setting CLI args = %#v, want %#v", got, want)
	}
}
