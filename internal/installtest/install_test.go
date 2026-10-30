package installtest

import (
	"bytes"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
)

// macOS has flock(2) but no flock CLI. Use the inherited descriptor exactly as
// util-linux flock does; Linux CI additionally exercises its native executable.
func TestFlockHelper(t *testing.T) {
	if os.Getenv("NOVAS_FLOCK_HELPER") != "1" {
		return
	}
	if err := syscall.Flock(9, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func TestUpdaterUsesValidatedReleaseTaggedSources(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	updater, err := os.ReadFile(filepath.Join(root, "novas.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(updater)
	for _, mutableURL := range []string{
		"raw.githubusercontent.com/CatMsg/NovaPanel/main/install.sh",
		"raw.githubusercontent.com/CatMsg/NovaPanel/main/novas.sh",
	} {
		if strings.Contains(text, mutableURL) {
			t.Fatalf("updater still fetches a mutable main-branch script: %s", mutableURL)
		}
	}
	for lineNumber, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "curl ") && strings.Contains(line, "https://") {
			if !strings.Contains(line, "--proto '=https'") || !strings.Contains(line, "--proto-redir '=https'") {
				t.Fatalf("HTTPS download at novas.sh:%d does not forbid protocol downgrade: %s", lineNumber+1, line)
			}
		}
	}
	installer, err := os.ReadFile(filepath.Join(root, "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for lineNumber, line := range strings.Split(string(installer), "\n") {
		if strings.Contains(line, "curl ") && strings.Contains(line, "https://") {
			if !strings.Contains(line, "--proto '=https'") || !strings.Contains(line, "--proto-redir '=https'") {
				t.Fatalf("HTTPS download at install.sh:%d does not forbid protocol downgrade: %s", lineNumber+1, line)
			}
		}
	}

	for _, required := range []string{
		"verify_release_script_provenance \"$path\" \"$target_version\" \"$staging\"",
		"--bundle \"$bundle\"",
		"--signer-workflow \"$NOVAS_RELEASE_REPO/.github/workflows/release.yml\"",
		"--source-ref refs/heads/main",
		"--source-digest \"$source_digest\"",
		"download_verified_release_script \"$target_version\" install.sh \"$staging\"",
		"download_verified_release_script \"$latest_version\" novas.sh \"$staging\"",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("updater is missing attested script verification: %q", required)
		}
	}
	verify := strings.Index(text, "verify_release_script_provenance \"$path\" \"$target_version\" \"$staging\"")
	parse := strings.Index(text, "bash -n \"$path\"")
	execute := strings.Index(text, "bash \"${installer_path}\" \"${target_version}\"")
	if verify < 0 || parse < 0 || execute < 0 || verify >= parse || parse >= execute {
		t.Fatal("updater does not verify script provenance before syntax checking and execution")
	}
	managerVerify := strings.Index(text, `download_verified_release_script "$latest_version" novas.sh "$staging"`)
	managerReplace := strings.Index(text, `mv -f "$temp_script" /usr/bin/novas`)
	if managerVerify < 0 || managerReplace < 0 || managerVerify >= managerReplace {
		t.Fatal("manager script is replaced before its release provenance is verified")
	}

	extract := func(name string) string {
		t.Helper()
		start := strings.Index(text, name+"() {")
		if start < 0 {
			t.Fatalf("missing shell helper %s", name)
		}
		end := strings.Index(text[start:], "\n}\n")
		if end < 0 {
			t.Fatalf("unterminated shell helper %s", name)
		}
		return text[start : start+end+3]
	}
	script := extract("valid_release_tag") + extract("release_source_url") + `
set -eu
[[ "$(release_source_url v1.2.3 install.sh)" == "https://raw.githubusercontent.com/CatMsg/NovaPanel/v1.2.3/install.sh" ]]
[[ "$(release_source_url v1.2.3 novas.sh)" == "https://raw.githubusercontent.com/CatMsg/NovaPanel/v1.2.3/novas.sh" ]]
if release_source_url main install.sh || release_source_url v1.2.3 ../../evil; then exit 1; fi
`
	if output, err := exec.Command("bash", "-c", script).CombinedOutput(); err != nil {
		t.Fatalf("release source URL validation failed: %v\n%s", err, output)
	}
}

func TestManagerRunsOnlyAttestedReleaseInstaller(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	updater, err := os.ReadFile(filepath.Clean(filepath.Join(filepath.Dir(source), "../../novas.sh")))
	if err != nil {
		t.Fatal(err)
	}
	text := string(updater)
	extract := func(name string) string {
		t.Helper()
		start := strings.Index(text, name+"() {")
		if start < 0 {
			t.Fatalf("missing shell helper %s", name)
		}
		end := strings.Index(text[start:], "\n}\n")
		if end < 0 {
			t.Fatalf("unterminated shell helper %s", name)
		}
		return text[start : start+end+3]
	}

	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	write(t, filepath.Join(bin, "gh-fixture"), `#!/bin/sh
set -eu
printf '%s\n' "$@" > "$TEST_ROOT/gh-args"
printf 'verify\n' >> "$TEST_ACTIONS"
exit "$VERIFY_RC"
`, 0755)
	write(t, filepath.Join(bin, "bash"), `#!/bin/sh
set -eu
if [ "${1:-}" = -n ]; then
  printf 'syntax\n' >> "$TEST_ACTIONS"
  exit 0
fi
[ -f "$1" ]
shift
printf 'execute:%s\n' "$*" >> "$TEST_ACTIONS"
exit 23
`, 0755)

	shell := strings.Join([]string{
		extract("valid_release_tag"),
		extract("release_source_url"),
		extract("verify_release_script_provenance"),
		extract("download_verified_release_script"),
		extract("download_install_script"),
		extract("run_install_script"),
	}, "\n") + `
set -eu
NOVAS_RELEASE_REPO=CatMsg/NovaPanel
NOVAS_RELEASE_GH_PATH=""
LOGI() { printf 'info:%s\n' "$*" >&2; }
LOGE() { printf 'error:%s\n' "$*" >&2; }
release_download_to_file() {
  case "$1" in
    https://raw.githubusercontent.com/CatMsg/NovaPanel/v1.2.3/install.sh)
      printf '#!/bin/sh\nexit 0\n' > "$2"
      printf 'download:script\n' >> "$TEST_ACTIONS" ;;
    https://github.com/CatMsg/NovaPanel/releases/download/v1.2.3/NovaPanel-install.sh.sigstore.json)
      [[ "${FAIL_BUNDLE:-0}" == 0 ]] || return 1
      printf '{}\n' > "$2"
      printf 'download:bundle\n' >> "$TEST_ACTIONS" ;;
    https://api.github.com/repos/CatMsg/NovaPanel/releases/tags/v1.2.3)
      if [[ "${BAD_DIGEST:-0}" == 1 ]]; then
        printf '{\n  "target_commitish": "main"\n}\n' > "$2"
      else
        printf '{\n  "target_commitish": "9014ce970f2af32ab12824b1d57dc6e0d96d50ae"\n}\n' > "$2"
      fi
      printf 'download:release\n' >> "$TEST_ACTIONS" ;;
    *) return 92 ;;
  esac
}
ensure_release_gh() { NOVAS_RELEASE_GH_PATH="$TEST_GH"; }
status=0
run_install_script v1.2.3 || status=$?
printf 'status=%s\n' "$status"
`
	for _, test := range []struct {
		name        string
		failBundle  bool
		badDigest   bool
		verifyRC    string
		wantStatus  string
		wantActions string
	}{
		{name: "attested-installer-runs", verifyRC: "0", wantStatus: "status=23", wantActions: "download:script\ndownload:bundle\ndownload:release\nverify\nsyntax\nexecute:v1.2.3\n"},
		{name: "invalid-attestation-fails-closed", verifyRC: "47", wantStatus: "status=1", wantActions: "download:script\ndownload:bundle\ndownload:release\nverify\n"},
		{name: "missing-bundle-fails-closed", failBundle: true, verifyRC: "0", wantStatus: "status=1", wantActions: "download:script\n"},
		{name: "invalid-source-commit-fails-closed", badDigest: true, verifyRC: "0", wantStatus: "status=1", wantActions: "download:script\ndownload:bundle\ndownload:release\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			actions := filepath.Join(root, test.name+".actions")
			command := exec.Command("bash", "-c", shell)
			command.Env = append(os.Environ(),
				"PATH="+bin+":"+os.Getenv("PATH"),
				"TEST_ROOT="+root,
				"TEST_ACTIONS="+actions,
				"TEST_GH="+filepath.Join(bin, "gh-fixture"),
				"VERIFY_RC="+test.verifyRC,
				"FAIL_BUNDLE="+map[bool]string{true: "1", false: "0"}[test.failBundle],
				"BAD_DIGEST="+map[bool]string{true: "1", false: "0"}[test.badDigest],
			)
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("manager installer fixture failed: %v\n%s", err, output)
			}
			if !bytes.Contains(output, []byte(test.wantStatus)) {
				t.Fatalf("manager installer status output = %q, want %q", output, test.wantStatus)
			}
			got, err := os.ReadFile(actions)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != test.wantActions {
				t.Fatalf("manager installer action order = %q, want %q", got, test.wantActions)
			}
			if test.verifyRC == "0" && !test.failBundle && !test.badDigest {
				args, err := os.ReadFile(filepath.Join(root, "gh-args"))
				if err != nil {
					t.Fatal(err)
				}
				for _, required := range []string{
					"attestation\nverify\n",
					"--repo\nCatMsg/NovaPanel",
					"--bundle\n",
					"--signer-workflow\nCatMsg/NovaPanel/.github/workflows/release.yml",
					"--source-ref\nrefs/heads/main",
					"--source-digest\n9014ce970f2af32ab12824b1d57dc6e0d96d50ae",
					"--deny-self-hosted-runners",
				} {
					if !bytes.Contains(args, []byte(required)) {
						t.Errorf("attestation verifier args missing %q:\n%s", required, args)
					}
				}
			}
		})
	}
}

func TestInstallerSupportsOnlyAMD64(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	installer := filepath.Clean(filepath.Join(filepath.Dir(source), "../../install.sh"))
	bin := t.TempDir()
	uname := filepath.Join(bin, "uname")
	if err := os.WriteFile(uname, []byte("#!/bin/sh\nprintf '%s\\n' \"$NOVAS_TEST_UNAME_MACHINE\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "mktemp"), []byte("#!/bin/sh\nprintf called > \"$NOVAS_TEST_MKTEMP_MARKER\"\nprintf '%s\\n' /tmp/novapanel-installer-architecture-test\n"), 0755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		machine string
		want    string
		ok      bool
	}{
		{machine: "x86_64", want: "amd64", ok: true},
		{machine: "x64", want: "amd64", ok: true},
		{machine: "amd64", want: "amd64", ok: true},
		{machine: "386"},
		{machine: "armv5l"},
		{machine: "armv6l"},
		{machine: "armv7l"},
		{machine: "aarch64"},
		{machine: "s390x"},
	}
	for _, test := range tests {
		t.Run(test.machine, func(t *testing.T) {
			command := exec.Command("bash", "-c", `source "$1"; arch`, "fixture", installer)
			command.Env = []string{"PATH=" + bin, "NOVAS_TEST_UNAME_MACHINE=" + test.machine}
			output, err := command.CombinedOutput()
			if test.ok {
				if err != nil || strings.TrimSpace(string(output)) != test.want {
					t.Fatalf("arch() = %q, %v; want %q", output, err, test.want)
				}
				return
			}
			if err == nil || !strings.Contains(string(output), "仅支持 Linux amd64") {
				t.Fatalf("arch() did not reject unsupported architecture %q: %q, %v", test.machine, output, err)
			}
		})
	}

	marker := filepath.Join(t.TempDir(), "mktemp-called")
	command := exec.Command("bash", "-c", `source "$1"; install_novas v1.2.3`, "fixture", installer)
	command.Env = []string{"PATH=" + bin, "NOVAS_TEST_UNAME_MACHINE=aarch64", "NOVAS_TEST_MKTEMP_MARKER=" + marker}
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "仅支持 Linux amd64") {
		t.Fatalf("install_novas did not reject non-amd64 before deployment: %q, %v", output, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("installer created staging before architecture rejection, marker stat error: %v", err)
	}
}

func TestInstallerHTTPSDownloaderRejectsDowngradeRedirect(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl is required for the installer download policy test")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is required for the installer download policy test")
	}

	var downgradeHits atomic.Int32
	httpTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		downgradeHits.Add(1)
		_, _ = io.WriteString(w, "insecure payload")
	}))
	defer httpTarget.Close()
	secureTarget := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "trusted payload")
	}))
	defer secureTarget.Close()
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/downgrade" {
			http.Redirect(w, r, httpTarget.URL, http.StatusFound)
			return
		}
		http.Redirect(w, r, secureTarget.URL, http.StatusFound)
	}))
	defer origin.Close()

	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	installer, err := os.ReadFile(filepath.Join(root, "../../install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(installer)
	start := strings.Index(text, "download_to_file() {")
	if start < 0 {
		t.Fatal("missing installer download helper")
	}
	end := strings.Index(text[start:], "\n}\n")
	if end < 0 {
		t.Fatal("unterminated installer download helper")
	}
	helper := text[start : start+end+3]
	caBundle := filepath.Join(t.TempDir(), "test-ca.pem")
	var certs bytes.Buffer
	for _, server := range []*httptest.Server{origin, secureTarget} {
		if err := pem.Encode(&certs, &pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(caBundle, certs.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	downloaded := filepath.Join(t.TempDir(), "download.bin")
	script := helper + `
set -eu
download_to_file "$SAFE_URL" "$SAFE_OUTPUT"
[[ "$(cat "$SAFE_OUTPUT")" == "trusted payload" ]]
if download_to_file "$HTTP_URL" "$HTTP_OUTPUT"; then
  echo "plain HTTP download was accepted" >&2
  exit 1
fi
if download_to_file "$DOWNGRADE_URL" "$DOWNGRADE_OUTPUT"; then
  echo "HTTPS-to-HTTP redirect was accepted" >&2
  exit 1
fi
`
	command := exec.Command("bash", "-c", script)
	command.Env = append(os.Environ(),
		"CURL_CA_BUNDLE="+caBundle,
		"NO_PROXY=127.0.0.1,localhost",
		"no_proxy=127.0.0.1,localhost",
		"SAFE_URL="+origin.URL+"/secure",
		"SAFE_OUTPUT="+downloaded,
		"DOWNGRADE_URL="+origin.URL+"/downgrade",
		"DOWNGRADE_OUTPUT="+downloaded+".insecure",
		"HTTP_URL="+httpTarget.URL,
		"HTTP_OUTPUT="+downloaded+".plain-http",
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("HTTPS downloader policy failed: %v\n%s", err, output)
	}
	if downgradeHits.Load() != 0 {
		t.Fatal("curl followed an HTTPS download redirect to HTTP")
	}
	if _, err := os.Stat(downloaded + ".insecure"); !os.IsNotExist(err) {
		t.Fatal("insecure redirect left a downloaded file")
	}
	if _, err := os.Stat(downloaded + ".plain-http"); !os.IsNotExist(err) {
		t.Fatal("plain HTTP request left a downloaded file")
	}
}

func TestInstallerDoesNotInstallUnusedWget(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	installer, err := os.ReadFile(filepath.Join(root, "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(installer)
	start := strings.Index(text, "install_base() {")
	if start < 0 {
		t.Fatal("missing install_base helper")
	}
	end := strings.Index(text[start:], "\n}\n")
	if end < 0 {
		t.Fatal("unterminated install_base helper")
	}
	if strings.Contains(text[start:start+end], "wget") {
		t.Fatal("install_base still installs wget although the installer uses curl exclusively")
	}
}

func readmeInstallerExamples(t *testing.T, content []byte) []string {
	t.Helper()
	var examples []string
	var block strings.Builder
	inBashBlock := false
	for _, line := range strings.Split(string(content), "\n") {
		if !inBashBlock {
			if strings.TrimSpace(line) == "```bash" {
				inBashBlock = true
				block.Reset()
			}
			continue
		}
		if strings.TrimSpace(line) == "```" {
			example := block.String()
			if strings.Contains(example, `bash "$_novas_script"`) {
				examples = append(examples, example)
			}
			inBashBlock = false
			continue
		}
		block.WriteString(line)
		block.WriteByte('\n')
	}
	if inBashBlock {
		t.Fatal("unterminated bash code block in README")
	}
	return examples
}

func TestReadmeInstallerBootstrapValidatesHTTPSDownloadBeforeExecution(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	readmes, err := filepath.Glob(filepath.Join(root, "README*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(readmes) != 6 {
		t.Fatalf("expected six localized README files, found %d", len(readmes))
	}
	for _, path := range readmes {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(path) != "README.md" {
			if !strings.Contains(string(content), "](README.md)") {
				t.Fatalf("%s must link to the canonical verified installation steps", filepath.Base(path))
			}
			if strings.Contains(string(content), `bash "$_novas_installer_tmp"`) || strings.Contains(string(content), "bash <(curl") {
				t.Fatalf("%s retains an unverified installer command", filepath.Base(path))
			}
			continue
		}
		examples := readmeInstallerExamples(t, content)
		if len(examples) != 1 {
			t.Fatalf("README.md contains %d verified installer examples, want one canonical command", len(examples))
		}
		example := examples[0]
		for _, required := range []string{
			`curl --proto '=https' --proto-redir '=https' -fsSL`,
			`_novas_version='latest'`,
			`_novas_tag="v${_novas_version#v}"`,
			`NovaPanel-install.sh.sigstore.json`,
			`if [[ -s "$_novas_bundle" ]]; then`,
			`--bundle "$_novas_bundle"`,
			`--source-ref refs/heads/main`,
			`--source-digest "$_novas_source_digest"`,
			`_novas_tag" != 'v1.6.149'`,
			`71a91cf3e7a9d18a2f803e272d46266b4100162f9bfcbc69991d626f94534583`,
			`bash -n "$_novas_script"`,
			`bash "$_novas_script" "$_novas_tag"`,
			`https://raw.githubusercontent.com/CatMsg/NovaPanel/${_novas_tag}/install.sh`,
			"https://api.github.com/repos/CatMsg/NovaPanel/releases/latest",
		} {
			if !strings.Contains(example, required) {
				t.Fatalf("canonical installer bootstrap is missing %q", required)
			}
		}
		verify := strings.Index(example, `attestation verify "$_novas_script"`)
		syntaxCheck := strings.Index(example, `bash -n "$_novas_script"`)
		execute := strings.Index(example, `bash "$_novas_script" "$_novas_tag"`)
		if verify < 0 || syntaxCheck < 0 || execute < 0 || verify >= syntaxCheck || syntaxCheck >= execute {
			t.Fatal("README executes the installer before attestation and syntax verification")
		}
		legacyDigest := strings.Index(example, `sha256sum "$_novas_script"`)
		if legacyDigest < 0 || legacyDigest >= syntaxCheck {
			t.Fatal("README executes the installer before checking the pinned legacy digest")
		}
		if strings.Contains(example, "raw.githubusercontent.com/CatMsg/NovaPanel/main/install.sh") || strings.Contains(example, "bash <(curl") {
			t.Fatal("README uses a mutable or unverified installer bootstrap")
		}
	}
}

func TestReadmeInstallerBootstrapExecutionOrderAndExitCode(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	examples := readmeInstallerExamples(t, readme)
	if len(examples) != 1 {
		t.Fatalf("expected one verified install example, got %d", len(examples))
	}

	testRoot := t.TempDir()
	bin := filepath.Join(testRoot, "bin")
	write(t, filepath.Join(bin, "mktemp"), "#!/bin/sh\nset -eu\n[ \"${1:-}\" = -d ]\nmkdir -p \"$TEST_TMP\"\nprintf '%s\\n' \"$TEST_TMP\"\n", 0755)
	write(t, filepath.Join(bin, "curl"), `#!/bin/sh
set -eu
output=
url=
proto_seen=0
redir_seen=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    --proto) [ "${2:-}" = "=https" ] || exit 91; proto_seen=1; shift 2 ;;
    --proto-redir) [ "${2:-}" = "=https" ] || exit 92; redir_seen=1; shift 2 ;;
    -fsSL) shift ;;
    -o) output=${2:-}; shift 2 ;;
    https://*) url=$1; shift ;;
    *) exit 93 ;;
  esac
done
[ "$proto_seen" = 1 ] && [ "$redir_seen" = 1 ]
if [ "$url" = "https://api.github.com/repos/CatMsg/NovaPanel/releases/latest" ]; then
  printf 'curl:latest\n' >> "$TEST_ACTIONS"
  printf '{\n  "tag_name": "%s"\n}\n' "${LATEST_TAG:-v1.2.3}"
else
  case "$url" in
    https://raw.githubusercontent.com/CatMsg/NovaPanel/v1.2.3/install.sh) tag=v1.2.3 ;;
    https://raw.githubusercontent.com/CatMsg/NovaPanel/v9.8.7/install.sh) tag=v9.8.7 ;;
    https://raw.githubusercontent.com/CatMsg/NovaPanel/v1.6.149/install.sh) tag=v1.6.149 ;;
    https://raw.githubusercontent.com/CatMsg/NovaPanel/v1.6.150/install.sh) tag=v1.6.150 ;;
    https://github.com/CatMsg/NovaPanel/releases/download/v1.2.3/NovaPanel-install.sh.sigstore.json) tag=v1.2.3; kind=bundle ;;
    https://github.com/CatMsg/NovaPanel/releases/download/v9.8.7/NovaPanel-install.sh.sigstore.json) tag=v9.8.7; kind=bundle ;;
    https://github.com/CatMsg/NovaPanel/releases/download/v1.6.149/NovaPanel-install.sh.sigstore.json) tag=v1.6.149; kind=bundle ;;
    https://github.com/CatMsg/NovaPanel/releases/download/v1.6.150/NovaPanel-install.sh.sigstore.json) tag=v1.6.150; kind=bundle ;;
    https://api.github.com/repos/CatMsg/NovaPanel/releases/tags/v1.2.3) tag=v1.2.3; kind=release ;;
    https://api.github.com/repos/CatMsg/NovaPanel/releases/tags/v9.8.7) tag=v9.8.7; kind=release ;;
    https://api.github.com/repos/CatMsg/NovaPanel/releases/tags/v1.6.149) tag=v1.6.149; kind=release ;;
    https://api.github.com/repos/CatMsg/NovaPanel/releases/tags/v1.6.150) tag=v1.6.150; kind=release ;;
    *) exit 94 ;;
  esac
  [ -n "$output" ]
  if [ "${kind:-installer}" = installer ]; then
    printf '#!/bin/sh\nexit 0\n' > "$output"
    printf 'curl:installer:%s\n' "$tag" >> "$TEST_ACTIONS"
  elif [ "$kind" = bundle ]; then
    printf 'curl:bundle:%s\n' "$tag" >> "$TEST_ACTIONS"
    case "$tag" in v1.6.149|v1.6.150) exit 22 ;; esac
    printf '{}\n' > "$output"
  else
    printf '{\n  "target_commitish": "9014ce970f2af32ab12824b1d57dc6e0d96d50ae"\n}\n' > "$output"
    printf 'curl:release:%s\n' "$tag" >> "$TEST_ACTIONS"
  fi
fi
`, 0755)
	write(t, filepath.Join(bin, "sha256sum"), `#!/bin/sh
set -eu
case "${1:-}" in
  */install.sh)
    digest="${LEGACY_SHA:-71a91cf3e7a9d18a2f803e272d46266b4100162f9bfcbc69991d626f94534583}"
    printf 'sha256:%s\n' "$digest" >> "$TEST_ACTIONS"
    printf '%s  %s\n' "$digest" "$1"
    ;;
  *) exit 98 ;;
esac
`, 0755)
	write(t, filepath.Join(bin, "gh"), `#!/bin/sh
set -eu
if [ "${1:-}" = attestation ] && [ "${2:-}" = verify ] && [ "${3:-}" = --help ]; then
  printf '%s\n' '--bundle <string> --source-digest <string>'
  exit 0
fi
[ "${1:-}" = attestation ] && [ "${2:-}" = verify ]
printf 'gh:verify\n' >> "$TEST_ACTIONS"
exit "$VERIFY_RC"
`, 0755)
	write(t, filepath.Join(bin, "bash"), "#!/bin/sh\nset -eu\nif [ \"$1\" = -n ]; then\n  [ -f \"$2\" ]\n  printf 'syntax\\n' >> \"$TEST_ACTIONS\"\n  exit 0\nfi\n[ -f \"$1\" ]\nshift\nprintf 'run:%s\\n' \"$*\" >> \"$TEST_ACTIONS\"\nexit \"$INSTALL_RC\"\n", 0755)

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is required for installer bootstrap execution tests")
	}
	shells := []string{bash}
	if zsh, err := exec.LookPath("zsh"); err == nil {
		shells = append(shells, zsh)
	}
	for _, shell := range shells {
		for _, testCase := range []struct {
			name      string
			version   string
			latestTag string
			verifyRC  string
			legacySHA string
			wantExit  int
			shouldRun bool
			attested  bool
		}{{name: "latest-valid", version: "latest", latestTag: "v1.2.3", verifyRC: "0", wantExit: 23, shouldRun: true, attested: true}, {name: "pinned-valid", version: "v9.8.7", verifyRC: "0", wantExit: 23, shouldRun: true, attested: true}, {name: "latest-rejected", version: "latest", latestTag: "v1.2.3", verifyRC: "47", wantExit: 47, attested: true}, {name: "latest-legacy-pinned-digest", version: "latest", latestTag: "v1.6.149", wantExit: 23, shouldRun: true}, {name: "legacy-pinned-digest-mismatch", version: "v1.6.149", legacySHA: "bad-digest", wantExit: 1}, {name: "unknown-version-missing-bundle", version: "v1.6.150", wantExit: 1}} {
			t.Run(filepath.Base(shell)+"/"+testCase.name, func(t *testing.T) {
				testTemp := filepath.Join(testRoot, "installer.tmp")
				if err := os.RemoveAll(testTemp); err != nil {
					t.Fatal(err)
				}
				actions := filepath.Join(testRoot, "actions")
				_ = os.Remove(actions)
				executionExample := strings.Replace(examples[0], `_novas_version='latest'`, `_novas_version='`+testCase.version+`'`, 1)
				command := exec.Command(shell, "-c", executionExample)
				env := append([]string(nil), os.Environ()...)
				for _, key := range []string{"PATH", "TEST_TMP", "TEST_ACTIONS", "INSTALL_RC", "VERIFY_RC", "LATEST_TAG", "LEGACY_SHA"} {
					prefix := key + "="
					filtered := env[:0]
					for _, item := range env {
						if !strings.HasPrefix(item, prefix) {
							filtered = append(filtered, item)
						}
					}
					env = filtered
				}
				env = append(env,
					"PATH="+bin+":"+os.Getenv("PATH"),
					"TEST_TMP="+testTemp,
					"TEST_ACTIONS="+actions,
					"INSTALL_RC=23",
					"VERIFY_RC="+testCase.verifyRC,
					"LATEST_TAG="+testCase.latestTag,
					"LEGACY_SHA="+testCase.legacySHA,
				)
				command.Env = env
				output, runErr := command.CombinedOutput()
				exitErr, ok := runErr.(*exec.ExitError)
				if !ok || exitErr.ExitCode() != testCase.wantExit {
					t.Fatalf("bootstrap exit = %v, want %d: %s", runErr, testCase.wantExit, output)
				}
				result, err := os.ReadFile(actions)
				if err != nil {
					t.Fatal(err)
				}
				tag := testCase.version
				want := ""
				if tag == "latest" {
					tag = testCase.latestTag
					if tag == "" {
						tag = "v1.2.3"
					}
					want = "curl:latest\n"
				}
				want += "curl:installer:" + tag + "\ncurl:bundle:" + tag + "\ncurl:release:" + tag + "\n"
				if testCase.attested {
					want += "gh:verify\n"
				} else if tag == "v1.6.149" {
					legacySHA := testCase.legacySHA
					if legacySHA == "" {
						legacySHA = "71a91cf3e7a9d18a2f803e272d46266b4100162f9bfcbc69991d626f94534583"
					}
					want += "sha256:" + legacySHA + "\n"
				}
				if testCase.shouldRun {
					want += "syntax\nrun:" + tag + "\n"
				}
				if string(result) != want {
					t.Fatalf("bootstrap execution order/arguments = %q, want %q", result, want)
				}
				if _, err := os.Stat(testTemp); !os.IsNotExist(err) {
					t.Fatalf("bootstrap temporary directory was not removed: %v", err)
				}
			})
		}
	}
}

func TestInstallerBaseAvoidsUnrelatedSystemUpgrades(t *testing.T) {
	tests := []struct {
		name        string
		release     string
		manager     string
		archHasDeps bool
		want        []string
		avoid       []string
	}{
		{name: "centos", release: "centos", manager: "yum", want: []string{"package yum install -y -q curl tar tzdata util-linux"}, avoid: []string{"yum -y update"}},
		{name: "fedora", release: "fedora", manager: "dnf", want: []string{"package dnf install -y -q curl tar tzdata util-linux"}, avoid: []string{"dnf -y update"}},
		{name: "ubuntu", release: "ubuntu", manager: "apt-get", want: []string{"package apt-get update", "package apt-get install -y -q curl tar tzdata util-linux"}, avoid: []string{"apt-get upgrade"}},
		{name: "opensuse", release: "opensuse-tumbleweed", manager: "zypper", want: []string{"package zypper refresh", "package zypper -q install -y curl tar timezone util-linux"}, avoid: []string{"zypper dup", "zypper update"}},
		{name: "arch-dependencies-present", release: "arch", manager: "pacman", archHasDeps: true, want: []string{"package pacman -Q curl tar tzdata util-linux"}, avoid: []string{"pacman -Syu"}},
		{name: "arch-dependencies-missing", release: "arch", manager: "pacman", want: []string{"package pacman -Q curl tar tzdata util-linux", "package pacman -Syu --noconfirm --needed curl tar tzdata util-linux"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, bin, installer := installerFixture(t)
			manager := test.manager
			script := `#!/bin/bash
set -eu
echo "package ${0##*/} $*" >> "$TEST_ROOT/actions"
if [[ "${0##*/}" == pacman && "$1" == -Q ]]; then
  [[ "$SCENARIO" == arch-dependencies-present ]]
fi
`
			write(t, filepath.Join(bin, manager), script, 0755)
			scenario := test.name
			if test.archHasDeps {
				scenario = "arch-dependencies-present"
			}
			env := []string{"SCENARIO=" + scenario}
			if test.name == "arch-dependencies-missing" {
				env = append(env, "NOVAS_ALLOW_ARCH_SYSTEM_UPGRADE=1")
			}
			output, err := runInstallerFixture(t, root, bin, installer, `release=`+test.release+`; install_base`, env...)
			if err != nil {
				t.Fatalf("install_base: %v\n%s", err, output)
			}
			actions, err := os.ReadFile(filepath.Join(root, "actions"))
			if err != nil {
				t.Fatal(err)
			}
			log := string(actions)
			for _, want := range test.want {
				if !strings.Contains(log, want) {
					t.Fatalf("missing package operation %q in %q", want, log)
				}
			}
			for _, avoid := range test.avoid {
				if strings.Contains(log, avoid) {
					t.Fatalf("unexpected unrelated system update %q in %q", avoid, log)
				}
			}
		})
	}
}

func TestArchInstallerRequiresExplicitSystemUpgradeAuthorization(t *testing.T) {
	root, bin, installer := installerFixture(t)
	write(t, filepath.Join(bin, "pacman"), `#!/bin/bash
set -eu
echo "package pacman $*" >> "$TEST_ROOT/actions"
if [[ "$1" == -Q ]]; then exit 1; fi
`, 0755)
	output, err := runInstallerFixture(t, root, bin, installer, `release=arch; install_base`, "SCENARIO=arch-dependencies-missing")
	if err == nil || !bytes.Contains(output, []byte("NOVAS_ALLOW_ARCH_SYSTEM_UPGRADE=1")) {
		t.Fatalf("missing Arch packages triggered an unapproved system upgrade: %v\n%s", err, output)
	}
	actions, readErr := os.ReadFile(filepath.Join(root, "actions"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if bytes.Contains(actions, []byte("pacman -Syu")) {
		t.Fatalf("pacman system upgrade ran without authorization: %s", actions)
	}
}

func TestStopChecksStatusOfSelectedService(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	updater, err := os.ReadFile(filepath.Join(root, "novas.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(updater)
	start := strings.Index(text, "stop() {")
	if start < 0 {
		t.Fatal("missing stop helper")
	}
	end := strings.Index(text[start:], "\n}\n")
	if end < 0 {
		t.Fatal("unterminated stop helper")
	}
	script := text[start:start+end+3] + `
set -u
running=1
check_status() {
  if [[ "$#" -ne 1 || "$1" != "novas" ]]; then
    echo "status check did not receive the selected service name" >&2
    return 2
  fi
  [[ "$running" -eq 1 ]]
}
systemctl() {
  [[ "$1" == "stop" && "$2" == "novas" ]] || return 2
  running=0
}
sleep() { :; }
LOGI() { printf '%s\n' "$*"; }
LOGE() { printf 'ERROR: %s\n' "$*"; }
before_show_menu() { exit 99; }
stop novas 0
`
	output, err := exec.Command("bash", "-c", script).CombinedOutput()
	if err != nil {
		t.Fatalf("stop command failed: %v\n%s", err, output)
	}
	if !bytes.Contains(output, []byte("novas 停止成功")) || bytes.Contains(output, []byte("停止失败")) {
		t.Fatalf("stop result was incorrect: %s", output)
	}
}

func write(t *testing.T, path, data string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
}

func TestUninstallGuardRequiresStoppedUnitAndDisabledService(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	contents, err := os.ReadFile(filepath.Join(root, "novas.sh"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	start := strings.Index(text, "stop_novas_before_uninstall() {")
	if start < 0 {
		t.Fatal("missing uninstall service guard")
	}
	end := strings.Index(text[start:], "\n}\n")
	if end < 0 {
		t.Fatal("unterminated uninstall service guard")
	}
	helper := text[start : start+end+3]
	uninstallStart := strings.Index(text, "uninstall() {")
	if uninstallStart < 0 {
		t.Fatal("missing uninstall function")
	}
	uninstallEnd := strings.Index(text[uninstallStart:], "\n}\n\nstart() {")
	if uninstallEnd < 0 {
		t.Fatal("unterminated uninstall function")
	}
	uninstallBody := text[uninstallStart : uninstallStart+uninstallEnd]
	guardPosition := strings.Index(uninstallBody, "if ! stop_novas_before_uninstall; then")
	deletePosition := strings.Index(uninstallBody, "rm /usr/local/novas/ -rf")
	if guardPosition < 0 || deletePosition < 0 || guardPosition > deletePosition {
		t.Fatal("uninstall must verify stop/disable before deleting panel files")
	}
	bin := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "systemctl.log")
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte(`#!/bin/sh
printf '%s\n' "$*" >> "$TEST_LOG"
case "$1" in
  stop) exit "$TEST_STOP_STATUS";;
  is-active) printf '%s\n' "$TEST_ACTIVE_STATE"; [ "$TEST_ACTIVE_STATE" = active ];;
  disable) exit "$TEST_DISABLE_STATUS";;
  *) exit 90;;
esac
`), 0755); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name          string
		activeState   string
		stopStatus    string
		disableStatus string
		wantSuccess   bool
		wantLog       string
	}{
		{name: "still active after stop failure", activeState: "active", stopStatus: "1", wantLog: "stop novas\nis-active novas\n"},
		{name: "still active after successful stop", activeState: "active", stopStatus: "0", wantLog: "stop novas\nis-active novas\n"},
		{name: "inactive despite stop error", activeState: "inactive", stopStatus: "1", disableStatus: "0", wantSuccess: true, wantLog: "stop novas\nis-active novas\ndisable novas\n"},
		{name: "disable failure", activeState: "inactive", stopStatus: "0", disableStatus: "1", wantLog: "stop novas\nis-active novas\ndisable novas\n"},
		{name: "unknown state", activeState: "unknown", stopStatus: "0", wantLog: "stop novas\nis-active novas\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.Remove(logPath); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			script := "LOGE() { :; }; LOGI() { :; }\n" + helper + "\nstop_novas_before_uninstall"
			command := exec.Command("bash", "-c", script)
			command.Env = []string{
				"PATH=" + bin,
				"TEST_LOG=" + logPath,
				"TEST_ACTIVE_STATE=" + tc.activeState,
				"TEST_STOP_STATUS=" + tc.stopStatus,
				"TEST_DISABLE_STATUS=" + tc.disableStatus,
			}
			output, err := command.CombinedOutput()
			if tc.wantSuccess != (err == nil) {
				t.Fatalf("guard success = %v, error %v, output %s", err == nil, err, output)
			}
			gotLog, _ := os.ReadFile(logPath)
			if string(gotLog) != tc.wantLog {
				t.Fatalf("systemctl calls = %q, want %q", gotLog, tc.wantLog)
			}
		})
	}
}

func TestOfflineDeployment(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	helper := filepath.Join(filepath.Dir(source), "../../scripts/install-runtime.sh")
	if override := os.Getenv("NOVAS_INSTALL_HELPER"); override != "" {
		helper = override
	}
	installer := filepath.Join(filepath.Dir(source), "../../install.sh")
	for _, scenario := range []string{"fresh", "fresh-callback", "current", "concurrent-current", "stop-failure", "snapshot-cleanup-failure", "migration-failure", "validation-failure", "health-failure", "rollback-stop-stuck", "rollback-disable-failure", "rollback-state-unknown", "rollback-daemon-reload-failure", "rollback-enable-failure", "rollback-start-failure", "custom-db", "bad-package", "locked"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			fresh := scenario == "fresh" || scenario == "fresh-callback"
			if scenario == "concurrent-current" {
				// Observe fresh state, then simulate another installer completing
				// before this deployment acquires its real deployment lock.
				probe := exec.Command("bash", "-c", `source "$1"; installer_is_fresh "$2"`, "fixture", installer, root)
				probe.Env = []string{"PATH=" + filepath.Join(root, "empty-bin"), "NOVAS_DB_FOLDER="}
				if output, err := probe.CombinedOutput(); err != nil {
					t.Fatalf("initial freshness probe: %v\n%s", err, output)
				}
			}
			home := filepath.Join(root, "usr/local/novas")
			old := home
			oldName := "novas"
			units := filepath.Join(root, "etc/systemd/system")
			if !fresh {
				write(t, filepath.Join(old, "db/novas.db"), "user-data", 0644)
				write(t, filepath.Join(old, "cert/key.pem"), "preserve-key", 0600)
				write(t, filepath.Join(root, "root/cert/example.com/fullchain.pem"), "public certificate", 0755)
				write(t, filepath.Join(root, "root/cert/example.com/privkey.pem"), "private key", 0755)
				write(t, filepath.Join(root, "external-cert/linked.example/fullchain.pem"), "external certificate", 0755)
				write(t, filepath.Join(root, "external-cert/linked.example/privkey.pem"), "external private key", 0755)
				if err := os.Symlink(filepath.Join(root, "external-cert/linked.example"), filepath.Join(root, "root/cert/linked.example")); err != nil {
					t.Fatal(err)
				}
				write(t, filepath.Join(old, "novas"), "old-binary", 0755)
				write(t, filepath.Join(units, oldName+".service"), "[Service]\nExecStart=/usr/local/novas/novas\nWorkingDirectory=/usr/local/novas\nLimitNOFILE=65536\n", 0644)
				write(t, filepath.Join(units, oldName+".service.d/limits.conf"), "[Service]\nLimitNPROC=4096\n", 0644)
				write(t, filepath.Join(root, "usr/bin/"+oldName), "old-command", 0755)
				write(t, filepath.Join(root, "active-"+oldName), "", 0600)
				write(t, filepath.Join(root, "enabled-"+oldName), "", 0600)
				write(t, filepath.Join(root, "var/lib/novas/update.status"), "state=running", 0600)
			}
			if scenario == "snapshot-cleanup-failure" {
				for _, name := range []string{"20261001-000000.old1", "20261002-000000.old2", "20261003-000000.old3", "20261004-000000.old4"} {
					write(t, filepath.Join(root, "var/lib/novas/update-backup", name, "marker"), "old snapshot", 0600)
				}
			}
			if scenario == "custom-db" {
				write(t, filepath.Join(units, oldName+".service"), "[Service]\nEnvironment=NOVAS_DB_FOLDER=/external\n", 0644)
			}
			pkg := filepath.Join(root, "package")
			write(t, filepath.Join(pkg, "novas"), `#!/bin/bash
set -eu
case "$1" in
  -v) echo 'NovaPanel Panel 1.6.97';;
  upgrade-db) echo upgrade-db >> "$TEST_ROOT/db-actions"; [[ "$SCENARIO" != migration-failure ]];;
  checkdb) echo checkdb >> "$TEST_ROOT/db-actions"; [[ "$SCENARIO" != validation-failure ]];;
	  healthcheck) [[ "$SCENARIO" != health-failure && "$SCENARIO" != rollback-stop-stuck && "$SCENARIO" != rollback-disable-failure && "$SCENARIO" != rollback-state-unknown && "$SCENARIO" != rollback-daemon-reload-failure && "$SCENARIO" != rollback-enable-failure && "$SCENARIO" != rollback-start-failure ]];;
esac
`, 0755)
			for _, file := range []string{"novas.sh", "novas.service", "scripts/hy2-forward.sh", "scripts/login-guard.sh", "bin/mita"} {
				write(t, filepath.Join(pkg, file), "#!/bin/sh\nexit 0\n", 0755)
			}
			if scenario == "bad-package" {
				os.Remove(filepath.Join(pkg, "bin/mita"))
			}
			bin := filepath.Join(root, "test-bin")
			write(t, filepath.Join(bin, "sleep"), "#!/bin/sh\nexit 0\n", 0755)
			if scenario == "snapshot-cleanup-failure" {
				write(t, filepath.Join(bin, "rm"), "#!/bin/bash\ncase \"$*\" in *update-backup/*) exit 1;; esac\nexec /bin/rm \"$@\"\n", 0755)
			}
			write(t, filepath.Join(bin, "systemctl"), `#!/bin/bash
set -eu
printf '%s\n' "$*" >> "$TEST_ROOT/systemctl.log"
action="$1"; shift
name="${!#}"; name="${name%.service}"
case "$action" in
  cat) cat "$TEST_ROOT/etc/systemd/system/$name.service";;
  show)
    if [[ "$*" == *ActiveState* ]]; then
      unit="${1%.service}"
      [[ "$SCENARIO" != rollback-state-unknown ]] || exit 1
      if [[ "$SCENARIO" == rollback-stop-stuck && "$unit" == novas ]]; then echo active; else echo inactive; fi
    elif [[ "$*" == *MainPID* ]]; then echo 42; fi
    ;;
  is-active) test -f "$TEST_ROOT/active-$name";;
  is-enabled) test -f "$TEST_ROOT/enabled-$name";;
  stop)
    if [[ -f "$TEST_ROOT/new-service-started" ]]; then touch "$TEST_ROOT/rollback-phase"; fi
    if [[ "$SCENARIO" == rollback-stop-stuck && "$name" == novas && -f "$TEST_ROOT/new-service-started" ]]; then exit 1; fi
    rm -f "$TEST_ROOT/active-$name"
    [[ "$SCENARIO" != stop-failure ]]
    ;;
  start)
    if [[ "$SCENARIO" == rollback-start-failure && -f "$TEST_ROOT/rollback-phase" ]]; then exit 1; fi
    touch "$TEST_ROOT/active-$name"; touch "$TEST_ROOT/new-service-started"
    ;;
  enable)
    if [[ "$SCENARIO" == rollback-enable-failure && -f "$TEST_ROOT/rollback-phase" ]]; then exit 1; fi
    touch "$TEST_ROOT/enabled-$name"
    ;;
  disable) [[ "$SCENARIO" != rollback-disable-failure ]] || exit 1; rm -f "$TEST_ROOT/enabled-$name";;
  daemon-reload)
    if [[ "$SCENARIO" == rollback-daemon-reload-failure && -f "$TEST_ROOT/rollback-phase" ]]; then exit 1; fi
    ;;
  *) echo "unexpected systemctl $action" >&2; exit 1;;
esac
`, 0755)
			if _, err := exec.LookPath("flock"); err != nil {
				exe, _ := os.Executable()
				write(t, filepath.Join(bin, "flock"), "#!/bin/sh\nNOVAS_FLOCK_HELPER=1 exec '"+exe+"' -test.run=TestFlockHelper\n", 0755)
			}
			if scenario == "locked" {
				lockPath := filepath.Join(root, "var/lib/novas-install.lock")
				write(t, lockPath, "", 0600)
				lock, err := os.OpenFile(lockPath, os.O_RDWR, 0600)
				if err != nil {
					t.Fatal(err)
				}
				defer lock.Close()
				if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
					t.Fatal(err)
				}
			}
			script := `source "$1"; novas_configure() { :; }; novas_deploy "$2" "$3"`
			if scenario == "concurrent-current" || scenario == "fresh-callback" {
				script = `source "$4"; FRESH_INSTALL=1
config_after_install() { echo settings >> "$TEST_ROOT/callback-actions"; }
install_fresh_ufw() {
  [[ ! -e "$TEST_ROOT/active-novas" ]] || return 90
  echo firewall >> "$TEST_ROOT/callback-actions"
}
source "$1"; novas_deploy "$2" "$3"`
			}
			command := exec.Command("bash", "-c", script, "fixture", helper, pkg, root, installer)
			command.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "SCENARIO="+scenario, "TEST_ROOT="+root, "NOVAS_DB_FOLDER=")
			output, err := command.CombinedOutput()
			success := fresh || scenario == "current" || scenario == "concurrent-current" || scenario == "snapshot-cleanup-failure"
			if success && err != nil {
				t.Fatalf("%v\n%s", err, output)
			}
			if !success && err == nil {
				t.Fatalf("failure was accepted\n%s", output)
			}
			if scenario == "fresh" || scenario == "fresh-callback" || scenario == "current" || scenario == "concurrent-current" || scenario == "migration-failure" || scenario == "validation-failure" || scenario == "health-failure" {
				dbActions, _ := os.ReadFile(filepath.Join(root, "db-actions"))
				want := "upgrade-db\ncheckdb\n"
				if scenario == "migration-failure" {
					want = "upgrade-db\n"
				}
				if string(dbActions) != want {
					t.Fatalf("database preflight order = %q, want %q", dbActions, want)
				}
			}
			if success {
				if scenario == "snapshot-cleanup-failure" {
					if !strings.Contains(string(output), "WARNING: NovaPanel is installed, but old rollback snapshots could not be fully cleaned up") {
						t.Fatalf("post-commit snapshot cleanup failure was not reported: %s", output)
					}
					backups, err := filepath.Glob(filepath.Join(root, "var/lib/novas/update-backup", "????????-??????.*"))
					if err != nil || len(backups) != 5 {
						t.Fatalf("snapshot cleanup failure changed rollback history: found %d snapshots, err %v", len(backups), err)
					}
				}
				securityDropIn, err := os.ReadFile(filepath.Join(units, "novas.service.d/90-novapanel-security.conf"))
				if err != nil || string(securityDropIn) != "[Service]\nUMask=0077\n" {
					t.Fatalf("strict systemd umask drop-in missing or invalid: %q, %v", securityDropIn, err)
				}
				if scenario == "fresh-callback" || scenario == "concurrent-current" {
					actions, _ := os.ReadFile(filepath.Join(root, "callback-actions"))
					want := ""
					if fresh {
						want = "settings\nfirewall\n"
					}
					if string(actions) != want {
						t.Fatalf("unsafe deploy callback: %q want %q", actions, want)
					}
				}
				if _, err := os.Stat(filepath.Join(root, "active-novas")); err != nil {
					t.Fatal("new service not started", err)
				}
				if !fresh {
					for path, want := range map[string]os.FileMode{
						filepath.Join(home, "db"):          0700,
						filepath.Join(home, "db/novas.db"): 0600,
					} {
						info, err := os.Stat(path)
						if err != nil {
							t.Fatalf("stat protected database path %s: %v", path, err)
						}
						if got := info.Mode().Perm(); got != want {
							t.Fatalf("database permission for %s = %04o, want %04o", path, got, want)
						}
					}
					for path, want := range map[string]os.FileMode{
						filepath.Join(root, "root/cert"):                                0700,
						filepath.Join(root, "root/cert/example.com"):                    0700,
						filepath.Join(root, "root/cert/example.com/fullchain.pem"):      0644,
						filepath.Join(root, "root/cert/example.com/privkey.pem"):        0600,
						filepath.Join(root, "external-cert/linked.example/privkey.pem"): 0755,
					} {
						info, err := os.Stat(path)
						if err != nil {
							t.Fatalf("stat certificate path %s: %v", path, err)
						}
						if got := info.Mode().Perm(); got != want {
							t.Fatalf("certificate permission for %s = %v, want %v", path, got, want)
						}
					}
					data, _ := os.ReadFile(filepath.Join(home, "db/novas.db"))
					if string(data) != "user-data" {
						t.Fatal("lost database")
					}
					key, _ := os.ReadFile(filepath.Join(home, "cert/key.pem"))
					if string(key) != "preserve-key" {
						t.Fatal("lost certificate")
					}
					unit, _ := os.ReadFile(filepath.Join(units, "novas.service"))
					if !bytes.Contains(unit, []byte("/usr/local/novas/novas")) || !bytes.Contains(unit, []byte("LimitNOFILE=65536")) {
						t.Fatal("unit options not preserved", string(unit))
					}
				}

			} else {
				if scenario == "rollback-stop-stuck" || scenario == "rollback-disable-failure" || scenario == "rollback-state-unknown" {
					if !strings.Contains(string(output), "Rollback stopped") {
						calls, _ := os.ReadFile(filepath.Join(root, "systemctl.log"))
						t.Fatalf("unsafe rollback was not reported: %s\nsystemctl calls:\n%s", output, calls)
					}
					if _, err := os.Stat(filepath.Join(home, "novas")); err != nil {
						t.Fatalf("current installation was deleted despite an unsafe rollback state: %v\n%s", err, output)
					}
					backups, err := filepath.Glob(filepath.Join(root, "var/lib/novas/update-backup", "????????-??????.*", "install/novas"))
					if err != nil || len(backups) != 1 {
						t.Fatalf("old installation snapshot was not preserved: %v, %v\n%s", backups, err, output)
					}
					binary, err := os.ReadFile(backups[0])
					if err != nil || string(binary) != "old-binary" {
						t.Fatalf("rollback snapshot does not contain the prior installation: %q, %v", binary, err)
					}
					if scenario == "rollback-stop-stuck" {
						if _, err := os.Stat(filepath.Join(root, "active-novas")); err != nil {
							t.Fatalf("active service state was unexpectedly cleared: %v", err)
						}
					} else if _, err := os.Stat(filepath.Join(root, "active-novas")); !os.IsNotExist(err) {
						t.Fatalf("fixture should show the service stopped before its state query failed: %v", err)
					}
				} else if scenario == "rollback-daemon-reload-failure" || scenario == "rollback-enable-failure" || scenario == "rollback-start-failure" {
					if !strings.Contains(string(output), "service recovery is incomplete") {
						t.Fatalf("failed service recovery was not reported: %s", output)
					}
					data, _ := os.ReadFile(filepath.Join(home, "db/novas.db"))
					if string(data) != "user-data" {
						t.Fatalf("service recovery failure lost restored database: %q\n%s", data, output)
					}
					binary, _ := os.ReadFile(filepath.Join(home, "novas"))
					if string(binary) != "old-binary" {
						t.Fatalf("service recovery failure lost restored binary: %q\n%s", binary, output)
					}
					backups, err := filepath.Glob(filepath.Join(root, "var/lib/novas/update-backup", "????????-??????.*", "install/novas"))
					if err != nil || len(backups) != 1 {
						t.Fatalf("service recovery failure lost rollback snapshot: %v, %v\n%s", backups, err, output)
					}
					backupBinary, err := os.ReadFile(backups[0])
					if err != nil || string(backupBinary) != "old-binary" {
						t.Fatalf("service recovery snapshot is invalid: %q, %v", backupBinary, err)
					}
					if scenario == "rollback-start-failure" {
						if _, err := os.Stat(filepath.Join(root, "active-novas")); !os.IsNotExist(err) {
							t.Fatalf("start-failure fixture left prior service active: %v", err)
						}
					} else if _, err := os.Stat(filepath.Join(root, "active-novas")); err != nil {
						t.Fatalf("restored service did not start: %v\n%s", err, output)
					}
					if scenario == "rollback-enable-failure" {
						if _, err := os.Stat(filepath.Join(root, "enabled-novas")); !os.IsNotExist(err) {
							t.Fatalf("enable-failure fixture left old service enabled: %v", err)
						}
					} else if _, err := os.Stat(filepath.Join(root, "enabled-novas")); err != nil {
						t.Fatalf("old enabled service state was not restored: %v\n%s", err, output)
					}
				} else {
					data, _ := os.ReadFile(filepath.Join(old, "db/novas.db"))
					if string(data) != "user-data" {
						t.Fatalf("rollback lost data\n%s", output)
					}
					binary, _ := os.ReadFile(filepath.Join(old, "novas"))
					if string(binary) != "old-binary" {
						t.Fatalf("failed preflight replaced the previous binary\n%s", output)
					}
					if _, err := os.Stat(filepath.Join(root, "active-"+oldName)); err != nil {
						t.Fatalf("old service not recovered: %v\n%s", err, output)
					}
				}
			}
			if strings.Contains(string(output), "preserve-key") {
				t.Fatal("secret leaked to log")
			}
		})
	}
}

// Only these harmless utilities are visible; firewall, SSH and package commands
// below are isolated fixtures, never the host's executables.
func installerFixture(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "test-bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bash", "awk", "sed", "sort", "grep", "cat", "cp", "rm", "env", "sha256sum", "tar"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path, filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(bin, "od"), "#!/bin/bash\nprintf '0123456789abcdef0123456789abcdef0123456789abcdef\\n'\n", 0755)
	write(t, filepath.Join(bin, "tr"), "#!/bin/bash\nIFS= read -r value\nprintf '%s' \"$value\"\n", 0755)
	write(t, filepath.Join(bin, "sshd"), `#!/bin/bash
[[ "$*" == -T ]] || exit 90
[[ "$SCENARIO" != sshd-failure ]] || exit 1
cat "$TEST_ROOT/sshd-config"
`, 0755)
	write(t, filepath.Join(bin, "ss"), `#!/bin/bash
[[ "$*" == '-H -ltnp' ]] || exit 90
[[ "$SCENARIO" != ss-failure ]] || exit 1
cat "$TEST_ROOT/listeners"
`, 0755)
	ssData, err := os.ReadFile(filepath.Join(bin, "ss"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(bin, "ss-fixture"), string(ssData), 0755)
	write(t, filepath.Join(bin, "systemctl"), `#!/bin/bash
case "$*" in
  'is-active --quiet ssh.socket') [[ "$SCENARIO" == socket* ]];;
  'is-active --quiet sshd.socket') exit 1;;
  'show ssh.socket -p Triggers --value')
    if [[ "$SCENARIO" == socket-unknown-service ]]; then echo unrelated.service; else echo ssh.service; fi;;
  'show ssh.socket -p Listen --value') echo '0.0.0.0:2222 (Stream) [::]:2200 (Stream)';;
  *) exit 90;;
esac
`, 0755)
	write(t, filepath.Join(bin, "iptables"), "#!/bin/bash\necho iptables >> \"$TEST_ROOT/actions\"\nexit 91\n", 0755)
	write(t, filepath.Join(bin, "ufw-fixture"), `#!/bin/bash
set -eu
echo "ufw $*" >> "$TEST_ROOT/actions"
case "$1" in
  prepend)
    [[ "$2" == allow && "$3" == */tcp ]] || exit 90
    grep -qx IPV6=yes "$TEST_ROOT/etc/default/ufw" || exit 92
    [[ "$SCENARIO" != allow-failure ]] || exit 1
    echo "$3" >> "$TEST_ROOT/allowed";;
  --force)
    case "$2" in
      enable)
        echo active > "$TEST_ROOT/ufw-state"
        [[ "$SCENARIO" != *enable-failure ]];;
      disable)
        [[ "$SCENARIO" != recovery-failure ]] || exit 1
        echo inactive > "$TEST_ROOT/ufw-state";;
      *) exit 90;;
    esac;;
  reload) [[ "$SCENARIO" != *reload-failure && "$SCENARIO" != recovery-failure ]];;
  show)
    [[ "$2" == added && "$SCENARIO" != show-added-failure ]] || exit 1
    [[ "$SCENARIO" != shadowed-rule ]] || echo 'ufw deny 2222/tcp'
    while read -r port; do echo "ufw allow $port"; done < "$TEST_ROOT/allowed";;
  status)
    [[ "$SCENARIO" != snapshot-failure ]] || exit 1
    [[ "$SCENARIO" != status-failure || "$(cat "$TEST_ROOT/ufw-state")" != active ]] || exit 1
    [[ "$SCENARIO" != inactive ]] || { echo 'Status: inactive'; exit; }
    echo "Status: $(cat "$TEST_ROOT/ufw-state")"
    while read -r port; do
      echo "$port ALLOW IN Anywhere"
      [[ "$SCENARIO" == missing-v6 ]] || echo "$port (v6) ALLOW IN Anywhere (v6)"
    done < "$TEST_ROOT/allowed";;
  *) exit 90;;
esac
`, 0755)
	for _, name := range []string{"apt-get", "pacman", "zypper", "yum", "dnf"} {
		write(t, filepath.Join(bin, name), `#!/bin/bash
set -eu
echo "package ${0##*/} $*" >> "$TEST_ROOT/actions"
case "$*" in
  *ufw*)
    [[ "$SCENARIO" != *package-failure ]] || exit 1
    [[ "$SCENARIO" != *package-missing ]] || exit 0
    cp "$TEST_ROOT/test-bin/ufw-fixture" "$TEST_ROOT/test-bin/ufw";;
  *iproute*)
    [[ "$SCENARIO" != *ss-package-failure ]] || exit 1
    [[ "$SCENARIO" != *ss-package-missing ]] || exit 0
    cp "$TEST_ROOT/test-bin/ss-fixture" "$TEST_ROOT/test-bin/ss";;
  *) exit 90;;
esac
`, 0755)
	}
	write(t, filepath.Join(root, "sshd-config"), "port 2222\nport 2200\nlistenaddress [::]:2222\nlistenaddress 0.0.0.0:2200\n", 0600)
	write(t, filepath.Join(root, "listeners"), "LISTEN 0 128 0.0.0.0:2222 0.0.0.0:* users:((\"sshd\",pid=42,fd=3))\nLISTEN 0 128 [::]:2200 [::]:* users:((\"sshd\",pid=42,fd=4))\nLISTEN 0 128 *:9000 *:* users:((\"other\",pid=43,fd=5))\n", 0600)
	write(t, filepath.Join(root, "etc/default/ufw"), "# preserve config\nIPV6=no\nDEFAULT_INPUT_POLICY=\"DROP\"\n", 0600)
	write(t, filepath.Join(root, "etc/ufw/user.rules"), "existing rules including denies\n", 0600)
	write(t, filepath.Join(root, "etc/ufw/user6.rules"), "existing ipv6 rules\n", 0600)
	write(t, filepath.Join(root, "ufw-state"), "inactive\n", 0600)
	write(t, filepath.Join(root, "allowed"), "8443/tcp\n", 0600)
	home := filepath.Join(root, "usr/local/novas")
	write(t, filepath.Join(home, "novas"), `#!/bin/bash
if [[ "$SCENARIO" == interactive ]]; then
  case "$1 $2" in
    'setting -port')
      [[ "$(cat "$TEST_ROOT/ufw-state")" == active && "$NOVAS_DB_FOLDER" == '' && -z "${NOVAS_DEVELOPMENT_DEFAULT_ADMIN:-}" ]] || exit 90
      echo cli-settings >> "$TEST_ROOT/actions"
      exit 0;;
    'admin -username')
      [[ "$3" == admin && "$4" == -password-stdin ]] || exit 90
      password="$(cat)"
      [[ "$password" =~ ^[0-9a-f]{48}$ ]] || exit 90
      echo cli-admin-generated >> "$TEST_ROOT/actions"
      exit 0;;
    'admin -show') echo cli-admin-show >> "$TEST_ROOT/actions"; exit 0;;
  esac
fi
[[ "$*" == 'setting -show' && "$NOVAS_DB_FOLDER" == "$TEST_ROOT/usr/local/novas/db" ]] || exit 90
[[ "$SCENARIO" != setting-failure ]] || exit 1
if [[ "$SCENARIO" == malformed-settings ]]; then echo 'Panel port: invalid'; exit; fi
# Exact cmd.showSetting contract, also verified against local CLI setting -show.
printf '\tPanel port:\t %s\n' "${PANEL_PORT:-2095}"
printf '\tSub port:\t %s\n' "${SUB_PORT:-2096}"
`, 0755)
	_, source, _, _ := runtime.Caller(0)
	return root, bin, filepath.Join(filepath.Dir(source), "../../install.sh")
}

func runInstallerFixture(t *testing.T, root, bin, installer, script string, env ...string) ([]byte, error) {
	t.Helper()
	command := exec.Command("bash", "-c", `set -euo pipefail; source "$1"; `+script, "fixture", installer, root)
	// Do not inherit host SSH, storage, startup scripts, or package-manager state.
	command.Env = append([]string{"PATH=" + bin, "HOME=" + root, "TEST_ROOT=" + root,
		"SCENARIO=default", "NOVAS_DB_FOLDER=", "SSH_CONNECTION=192.0.2.1 54321 192.0.2.2 2222"}, env...)
	return command.CombinedOutput()
}

func TestFreshInstallUFW(t *testing.T) {
	for _, scenario := range []string{"default", "custom-ports", "already-active", "without-session", "arch", "opensuse", "preinstalled-unsupported", "socket", "socket-default-config",
		"fedora", "centos", "almalinux", "rocky", "oracle", "centos-yum", "ss-missing", "fedora-ss-missing", "ss-package-failure", "ss-package-missing",
		"armbian", "armbian-package-failure", "armbian-package-missing", "armbian-ss-missing", "armbian-ss-package-failure", "armbian-ss-package-missing",
		"fedora-package-failure", "rocky-package-failure", "centos-yum-package-failure", "fedora-package-missing", "rocky-package-missing",
		"unsupported", "package-failure", "package-missing", "sshd-failure", "ss-failure", "no-listener", "config-mismatch", "empty-config",
		"bad-config-port", "bad-listener-port", "session-mismatch", "bad-session", "setting-failure", "malformed-settings", "bad-panel-port",
		"missing-ipv6-config", "allow-failure", "show-added-failure", "shadowed-rule", "enable-failure", "reload-failure", "snapshot-failure", "status-failure", "inactive", "missing-v6",
		"active-enable-failure", "active-reload-failure", "recovery-failure", "socket-unknown-service", "socket-mismatch"} {
		t.Run(scenario, func(t *testing.T) {
			root, bin, installer := installerFixture(t)
			env := []string{"SCENARIO=" + scenario}
			release := "ubuntu"
			success := false
			switch scenario {
			case "default", "custom-ports", "already-active", "without-session", "arch", "opensuse", "preinstalled-unsupported", "socket", "socket-default-config",
				"fedora", "centos", "almalinux", "rocky", "oracle", "centos-yum", "ss-missing", "fedora-ss-missing", "armbian", "armbian-ss-missing":
				success = true
			}
			switch scenario {
			case "custom-ports":
				env = append(env, "PANEL_PORT=32095", "SUB_PORT=32096")
			case "already-active", "preinstalled-unsupported", "active-enable-failure", "active-reload-failure":
				data, _ := os.ReadFile(filepath.Join(bin, "ufw-fixture"))
				write(t, filepath.Join(bin, "ufw"), string(data), 0755)
				write(t, filepath.Join(root, "ufw-state"), "active\n", 0600)
				if scenario == "preinstalled-unsupported" {
					release = "gentoo"
				}
			case "without-session":
				env = append(env, "SSH_CONNECTION=")
			case "arch":
				release = "arch"
			case "opensuse":
				release = "opensuse-tumbleweed"
			case "unsupported":
				release = "gentoo"
			case "fedora", "centos", "almalinux", "rocky", "oracle":
				release = scenario
			case "centos-yum", "centos-yum-package-failure":
				release = "centos"
				if err := os.Remove(filepath.Join(bin, "dnf")); err != nil {
					t.Fatal(err)
				}
			case "no-listener":
				write(t, filepath.Join(root, "listeners"), "LISTEN 0 128 *:9000 *:* users:((\"other\",pid=43,fd=5))\n", 0600)
			case "config-mismatch":
				write(t, filepath.Join(root, "sshd-config"), "port 22\n", 0600)
			case "empty-config":
				write(t, filepath.Join(root, "sshd-config"), "passwordauthentication yes\n", 0600)
			case "bad-config-port":
				write(t, filepath.Join(root, "sshd-config"), "port 65536\n", 0600)
			case "bad-listener-port":
				write(t, filepath.Join(root, "listeners"), "LISTEN 0 128 *:invalid *:* users:((\"sshd\",pid=42,fd=3))\n", 0600)
			case "session-mismatch":
				env = append(env, "SSH_CONNECTION=192.0.2.1 54321 192.0.2.2 2223")
			case "bad-session":
				env = append(env, "SSH_CONNECTION=192.0.2.1 54321 192.0.2.2 invalid")
			case "bad-panel-port":
				env = append(env, "PANEL_PORT=0")
			case "missing-ipv6-config":
				if err := os.Remove(filepath.Join(root, "etc/default/ufw")); err != nil {
					t.Fatal(err)
				}
			}
			if strings.HasPrefix(scenario, "fedora-") {
				release = "fedora"
			}
			if strings.HasPrefix(scenario, "rocky-") {
				release = "rocky"
			}
			if scenario == "armbian" || strings.HasPrefix(scenario, "armbian-") {
				release = "armbian"
			}
			if strings.Contains(scenario, "ss-missing") || strings.Contains(scenario, "ss-package-") {
				if err := os.Remove(filepath.Join(bin, "ss")); err != nil {
					t.Fatal(err)
				}
				if scenario == "fedora-ss-missing" {
					release = "fedora"
				}
			}
			if strings.HasPrefix(scenario, "socket") {
				write(t, filepath.Join(root, "listeners"), "LISTEN 0 128 0.0.0.0:2222 *:* users:((\"systemd\",pid=1,fd=3))\nLISTEN 0 128 [::]:2200 [::]:* users:((\"systemd\",pid=1,fd=4))\nLISTEN 0 128 *:9000 *:* users:((\"systemd\",pid=1,fd=5))\n", 0600)
				if scenario == "socket-default-config" {
					write(t, filepath.Join(root, "sshd-config"), "port 22\nlistenaddress [::]:22\n", 0600)
				}
				if scenario == "socket-mismatch" {
					write(t, filepath.Join(root, "listeners"), "LISTEN 0 128 *:2223 *:* users:((\"systemd\",pid=1,fd=3))\n", 0600)
				}
			}
			output, err := runInstallerFixture(t, root, bin, installer, `release=`+release+`; install_fresh_ufw "$2/usr/local/novas" "$2"`, env...)
			if success != (err == nil) {
				t.Fatalf("success=%v err=%v\n%s", success, err, output)
			}
			actions, _ := os.ReadFile(filepath.Join(root, "actions"))
			log := string(actions)
			if strings.Contains(log, "iptables") || strings.Contains(log, "nftables") || strings.Contains(log, "reset") || strings.Contains(log, "delete") {
				t.Fatal("unsafe/fallback command", log)
			}
			if strings.Contains(log, "epel-release") || strings.Contains(log, "http") {
				t.Fatal("added a repository", log)
			}
			if release == "armbian" {
				want := "package apt-get install -y -q ufw\n"
				if strings.Contains(scenario, "-ss-") {
					want = "package apt-get install -y -q iproute2\n"
					if success {
						want += "package apt-get install -y -q ufw\n"
					}
				}
				var packages strings.Builder
				for _, line := range strings.Split(log, "\n") {
					if strings.HasPrefix(line, "package ") {
						packages.WriteString(line + "\n")
					}
				}
				if packages.String() != want {
					t.Fatalf("unexpected Armbian package operations: %q want %q", packages.String(), want)
				}
				if strings.Contains(scenario, "-ss-package-") && strings.Contains(log, "ufw ") {
					t.Fatal("touched UFW without SSH listener inspection", log)
				}
				if strings.HasSuffix(scenario, "package-failure") && !bytes.Contains(output, []byte("from configured repositories on armbian")) {
					t.Fatal("configured-repository failure not reported", string(output))
				}
				if strings.HasSuffix(scenario, "package-missing") {
					want := "UFW installation failed"
					if strings.Contains(scenario, "-ss-") {
						want = "SSH listener inspection requires ss"
					}
					if !bytes.Contains(output, []byte(want)) {
						t.Fatal("missing installed command not reported", string(output))
					}
				}
			}
			if strings.Contains(log, "package ") {
				manager := "apt-get"
				switch release {
				case "fedora", "centos", "almalinux", "rocky", "oracle":
					manager = "dnf"
				case "arch":
					manager = "pacman"
				case "opensuse-tumbleweed":
					manager = "zypper"
				}
				if strings.HasPrefix(scenario, "centos-yum") {
					manager = "yum"
				}
				if !strings.Contains(log, "package "+manager+" ") {
					t.Fatal("wrong distribution package manager", log)
				}
			}
			if success {
				panel, sub := "2095", "2096"
				if scenario == "custom-ports" {
					panel, sub = "32095", "32096"
				}
				enable := strings.Index(log, "ufw --force enable")
				for _, port := range []string{"2200", "2222", panel, sub} {
					allow := strings.Index(log, "ufw prepend allow "+port+"/tcp")
					if allow < 0 || enable < allow {
						t.Fatal("missing allow before activation", port, log)
					}
				}
				if strings.Contains(log, "allow 22/tcp\n") || strings.Contains(log, "9000/tcp") || strings.Contains(log, "/udp") {
					t.Fatal("unrelated port opened", log)
				}
				config, _ := os.ReadFile(filepath.Join(root, "etc/default/ufw"))
				if !strings.Contains(string(config), "IPV6=yes") || !strings.Contains(string(config), `DEFAULT_INPUT_POLICY="DROP"`) {
					t.Fatal("lost IPv6/other configuration", string(config))
				}
			} else if scenario != "enable-failure" && scenario != "reload-failure" && scenario != "status-failure" && scenario != "inactive" && scenario != "missing-v6" && !strings.HasPrefix(scenario, "active-") && scenario != "recovery-failure" {
				if strings.Contains(log, "ufw --force enable") {
					t.Fatal("activated UFW despite unsafe discovery/setup", log)
				}
			}
			state, _ := os.ReadFile(filepath.Join(root, "ufw-state"))
			if !success && strings.Contains(log, "ufw --force enable") {
				if strings.HasPrefix(scenario, "active-") {
					if string(state) != "active\n" || strings.Contains(log, "ufw --force disable") {
						t.Fatal("disabled previously active UFW", log)
					}
				} else if scenario == "recovery-failure" {
					if !bytes.Contains(output, []byte("CRITICAL: UFW recovery failed")) {
						t.Fatal("recovery failure not reported", string(output))
					}
				} else if string(state) != "inactive\n" || !strings.Contains(log, "ufw --force disable") {
					t.Fatal("failed activation not restored", log, string(state))
				}
			}
			for path, want := range map[string]string{"etc/ufw/user.rules": "existing rules including denies\n", "etc/ufw/user6.rules": "existing ipv6 rules\n"} {
				data, _ := os.ReadFile(filepath.Join(root, path))
				if string(data) != want {
					t.Fatal("existing rules changed", path)
				}
			}
		})
	}
}

func TestFreshInstallDetection(t *testing.T) {
	for _, artifact := range []string{"none", "usr/local/novas", "usr/bin/novas", "usr/local/bin/novas", "etc/systemd/system/novas.service",
		"etc/systemd/system/novas.service.d", "usr/lib/systemd/system/novas.service", "lib/systemd/system/novas.service", "var/lib/novas", "dangling-link", "custom-db", "path-command"} {
		t.Run(artifact, func(t *testing.T) {
			root, bin, installer := installerFixture(t)
			// The fixture's mock installed binary must not influence pre-install detection.
			if err := os.RemoveAll(filepath.Join(root, "usr/local/novas")); err != nil {
				t.Fatal(err)
			}
			env := []string{}
			switch artifact {
			case "none":
			case "custom-db":
				env = append(env, "NOVAS_DB_FOLDER=/external")
			case "path-command":
				write(t, filepath.Join(bin, "novas"), "#!/bin/bash\nexit 90\n", 0755)
			case "dangling-link":
				if err := os.Symlink("missing", filepath.Join(root, "usr/local/novas")); err != nil {
					t.Fatal(err)
				}
			default:
				write(t, filepath.Join(root, artifact), "existing artifact", 0600)
			}
			output, err := runInstallerFixture(t, root, bin, installer, `installer_is_fresh "$2"`, env...)
			if (artifact == "none") != (err == nil) {
				t.Fatalf("artifact=%s err=%v\n%s", artifact, err, output)
			}
		})
	}
}

func TestInstallerConfigureFirewallGate(t *testing.T) {
	for _, scenario := range []string{"fresh", "upgrade", "concurrent-upgrade", "ambiguous", "settings-fail", "ufw-fail"} {
		t.Run(scenario, func(t *testing.T) {
			root, bin, installer := installerFixture(t)
			output, err := runInstallerFixture(t, root, bin, installer, `
config_after_install() { echo settings >> "$TEST_ROOT/actions"; [[ "$SCENARIO" != settings-fail ]]; }
install_fresh_ufw() { echo firewall >> "$TEST_ROOT/actions"; [[ "$SCENARIO" != ufw-fail ]]; }
FRESH_INSTALL=1; old=""
case "$SCENARIO" in
  upgrade) FRESH_INSTALL=0; old=novas;;
  concurrent-upgrade) old=novas;;
  ambiguous) FRESH_INSTALL=0;;
esac
novas_configure "$2/usr/local/novas" "$old"
`, "SCENARIO="+scenario)
			if (scenario == "settings-fail" || scenario == "ufw-fail") != (err != nil) {
				t.Fatalf("%v\n%s", err, output)
			}
			actions, _ := os.ReadFile(filepath.Join(root, "actions"))
			want := ""
			switch scenario {
			case "fresh", "ufw-fail":
				want = "settings\nfirewall\n"
			case "ambiguous", "settings-fail":
				want = "settings\n"
			}
			if string(actions) != want {
				t.Fatalf("unexpected configure order/actions: %q want %q", actions, want)
			}
		})
	}
}

func TestInstallerUFWLateDeploymentFailure(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "new-activation", true: "already-active"}[active], func(t *testing.T) {
			root, bin, installer := installerFixture(t)
			if active {
				data, _ := os.ReadFile(filepath.Join(bin, "ufw-fixture"))
				write(t, filepath.Join(bin, "ufw"), string(data), 0755)
				write(t, filepath.Join(root, "ufw-state"), "active\n", 0600)
			}
			// Only archive/network plumbing and the deployment outcome are stubbed.
			// Real installer UFW setup and its outer deployment EXIT trap run here.
			staging := filepath.Join(root, "staging")
			write(t, filepath.Join(staging, "novas/novas"), "#!/bin/bash\nif [[ \"${1:-}\" == -v ]]; then printf 'NovaPanel Panel\\t1.6.128\\n'; else exit 90; fi\n", 0755)
			write(t, filepath.Join(staging, "novas/scripts/install-runtime.sh"), `novas_deploy() {
  install_fresh_ufw "$TEST_ROOT/usr/local/novas" "$TEST_ROOT" || return 1
  echo simulated-late-health-failure >&2
  return 7
}
`, 0600)
			output, err := runInstallerFixture(t, root, bin, installer, `
release=ubuntu
 mktemp() { mkdir -p "$TEST_ROOT/staging"; printf '%s\n' "$TEST_ROOT/staging"; }
arch() { echo amd64; }
download_to_file() { :; }
verify_archive_checksum() { :; }
verify_archive_provenance() { :; }
tar() {
  case "$1" in
    -tzf) echo novas/ ;;
    -tvzf) printf '%s\n' 'drwxr-xr-x runner/runner 0 2026-10-30 00:00 novas/' '-rwxr-xr-x runner/runner 0 2026-10-30 00:00 novas/novas' ;;
    -xzf) : ;;
  esac
}
install_novas v1.6.128
`)
			if err == nil || !bytes.Contains(output, []byte("simulated-late-health-failure")) || bytes.Contains(output, []byte("安装成功")) {
				t.Fatalf("late deployment failure was accepted: %v\n%s", err, output)
			}
			state, _ := os.ReadFile(filepath.Join(root, "ufw-state"))
			want := "inactive\n"
			if active {
				want = "active\n"
			}
			if string(state) != want {
				t.Fatalf("activation state not recovered: %q want %q\n%s", state, want, output)
			}
			actions, _ := os.ReadFile(filepath.Join(root, "actions"))
			if active && bytes.Contains(actions, []byte("ufw --force disable")) {
				t.Fatal("disabled preexisting firewall", string(actions))
			}
			if _, err := os.Stat(staging); !os.IsNotExist(err) {
				t.Fatal("installer staging was not cleaned", err)
			}
		})
	}
}

func TestInstallerRejectsReleaseTagAndArchiveVersionMismatch(t *testing.T) {
	root, bin, installer := installerFixture(t)
	staging := filepath.Join(root, "staging")
	write(t, filepath.Join(staging, "novas/novas"), "#!/bin/bash\nprintf 'NovaPanel Panel\\t1.6.149\\n'\n", 0755)
	write(t, filepath.Join(staging, "novas/scripts/install-runtime.sh"), `novas_deploy() {
  touch "$TEST_ROOT/deploy-called"
}
`, 0600)
	output, err := runInstallerFixture(t, root, bin, installer, `
release=ubuntu
mktemp() { mkdir -p "$TEST_ROOT/staging"; printf '%s\n' "$TEST_ROOT/staging"; }
arch() { echo amd64; }
download_to_file() { :; }
verify_archive_checksum() { :; }
verify_archive_provenance() { :; }
tar() {
  case "$1" in
    -tzf) echo novas/ ;;
    -tvzf) printf '%s\n' 'drwxr-xr-x runner/runner 0 2026-10-30 00:00 novas/' '-rwxr-xr-x runner/runner 0 2026-10-30 00:00 novas/novas' ;;
    -xzf) : ;;
  esac
}
install_novas v1.6.150
`)
	if err == nil || !bytes.Contains(output, []byte("安装包版本与 Release 标签不匹配")) {
		t.Fatalf("release/archive version mismatch was not rejected: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(root, "deploy-called")); !os.IsNotExist(err) {
		t.Fatalf("deployment ran despite a release/archive version mismatch: %v", err)
	}
}

func TestInstallerRejectsUnsafeArchiveMembers(t *testing.T) {
	tests := []struct {
		name    string
		listing string
	}{
		{name: "symbolic link", listing: "lrwxrwxrwx runner/runner 0 2026-10-30 00:00 novas/escape -> /etc/passwd"},
		{name: "hard link", listing: "-rw-r--r-- runner/runner 0 2026-10-30 00:00 novas/escape link to ../../etc/passwd"},
		{name: "special file", listing: "prw-r--r-- runner/runner 0 2026-10-30 00:00 novas/pipe"},
		{name: "setuid file", listing: "-rwsr-xr-x runner/runner 0 2026-10-30 00:00 novas/novas"},
		{name: "setgid file", listing: "-rwxr-Sr-x runner/runner 0 2026-10-30 00:00 novas/novas"},
		{name: "sticky directory", listing: "drwxr-xr-t runner/runner 0 2026-10-30 00:00 novas/tmp/"},
		{name: "group-writable file", listing: "-rw-rw-r-- runner/runner 0 2026-10-30 00:00 novas/config"},
		{name: "world-writable file", listing: "-rwxrwxrwx runner/runner 0 2026-10-30 00:00 novas/novas"},
		{name: "empty member listing", listing: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, bin, installer := installerFixture(t)
			if err := os.MkdirAll(filepath.Join(root, "staging"), 0755); err != nil {
				t.Fatal(err)
			}
			output, err := runInstallerFixture(t, root, bin, installer, `
release=ubuntu
arch() { echo amd64; }
mktemp() { printf '%s\n' "$TEST_ROOT/staging"; }
download_to_file() { :; }
verify_archive_checksum() { :; }
verify_archive_provenance() { :; }
tar() {
  case "$1" in
    -tzf) printf 'novas/\nnovas/escape\n' ;;
    -tvzf) printf '%s\n' "$TAR_VERBOSE_LISTING" ;;
    -xzf) touch "$TEST_ROOT/archive-extracted" ;;
  esac
}
install_novas v1.6.149
`, "TAR_VERBOSE_LISTING="+test.listing)
			if err == nil || !bytes.Contains(output, []byte("安装包包含不支持的链接、特殊文件或危险权限")) {
				t.Fatalf("unsafe archive was not rejected: %v\n%s", err, output)
			}
			if _, err := os.Stat(filepath.Join(root, "archive-extracted")); !os.IsNotExist(err) {
				t.Fatalf("unsafe archive was extracted before rejection: %v", err)
			}
		})
	}
}

func TestInstallerArchiveProvenancePolicies(t *testing.T) {
	t.Run("legacy release accepts only pinned digest", func(t *testing.T) {
		for _, test := range []struct {
			name   string
			digest string
			wantOK bool
		}{
			{name: "pinned", digest: "e4da2eb684dc1fadc9efef5d7db22c03da9c9558039cafb18eb22a3fc7dd2988", wantOK: true},
			{name: "unrecognized", digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		} {
			t.Run(test.name, func(t *testing.T) {
				root, bin, installer := installerFixture(t)
				write(t, filepath.Join(root, "archive.tar.gz"), "fixture", 0600)
				output, err := runInstallerFixture(t, root, bin, installer, `
sha256sum() { printf '%s  %s\n' "$TEST_DIGEST" "$1"; }
ensure_attestation_gh() { echo unexpected-gh-bootstrap >&2; return 90; }
verify_archive_provenance v1.6.149 "$TEST_ROOT/archive.tar.gz" "$TEST_ROOT"
`, "TEST_DIGEST="+test.digest)
				if (err == nil) != test.wantOK {
					t.Fatalf("provenance result mismatch: %v\n%s", err, output)
				}
				if !test.wantOK && !bytes.Contains(output, []byte("不在已验证的 SHA-256 过渡白名单")) {
					t.Fatalf("unexpected legacy rejection: %s", output)
				}
			})
		}
	})

	t.Run("future release without bundle fails closed", func(t *testing.T) {
		root, bin, installer := installerFixture(t)
		write(t, filepath.Join(root, "archive.tar.gz"), "fixture", 0600)
		output, err := runInstallerFixture(t, root, bin, installer, `
arch() { echo amd64; }
sha256sum() { printf '%064d  %s\n' 0 "$1"; }
download_to_file() { return 1; }
ensure_attestation_gh() { echo verifier-ran >&2; return 90; }
verify_archive_provenance v1.6.150 "$TEST_ROOT/archive.tar.gz" "$TEST_ROOT"
`)
		if err == nil || !bytes.Contains(output, []byte("缺少来源证明 bundle")) || bytes.Contains(output, []byte("verifier-ran")) {
			t.Fatalf("missing bundle was not rejected before verification: %v\n%s", err, output)
		}
	})

	t.Run("future release verifies exact identity and commit", func(t *testing.T) {
		root, bin, installer := installerFixture(t)
		archive := filepath.Join(root, "archive.tar.gz")
		write(t, archive, "fixture", 0600)
		write(t, filepath.Join(root, "gh-fixture"), `#!/bin/bash
printf '%s\n' "$@" > "$TEST_ROOT/gh-args"
`, 0755)
		output, err := runInstallerFixture(t, root, bin, installer, `
arch() { echo amd64; }
sha256sum() { printf '%064d  %s\n' 0 "$1"; }
download_to_file() {
  case "$1" in
    *sigstore.json) printf '{}\n' > "$2" ;;
    *api.github.com*) printf '{\n  "target_commitish": "9014ce970f2af32ab12824b1d57dc6e0d96d50ae"\n}\n' > "$2" ;;
    *) return 1 ;;
  esac
}
ensure_attestation_gh() { gh_cli_path="$TEST_ROOT/gh-fixture"; }
verify_archive_provenance v1.6.150 "$TEST_ROOT/archive.tar.gz" "$TEST_ROOT"
`)
		if err != nil {
			t.Fatalf("valid fixture provenance failed: %v\n%s", err, output)
		}
		args, readErr := os.ReadFile(filepath.Join(root, "gh-args"))
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, want := range []string{
			"attestation\nverify\n" + archive,
			"--repo\nCatMsg/NovaPanel",
			"--bundle\n" + archive + ".sigstore.json",
			"--signer-workflow\nCatMsg/NovaPanel/.github/workflows/release.yml",
			"--source-ref\nrefs/heads/main",
			"--source-digest\n9014ce970f2af32ab12824b1d57dc6e0d96d50ae",
			"--deny-self-hosted-runners",
		} {
			if !bytes.Contains(args, []byte(want)) {
				t.Errorf("verifier args missing %q:\n%s", want, args)
			}
		}
	})

	t.Run("bad pinned CLI digest stops before extraction", func(t *testing.T) {
		root, bin, installer := installerFixture(t)
		if err := os.MkdirAll(filepath.Join(root, "staging"), 0755); err != nil {
			t.Fatal(err)
		}
		output, err := runInstallerFixture(t, root, bin, installer, `
download_to_file() { printf 'fixture' > "$2"; }
sha256sum() { printf '%064d  %s\n' 0 "$1"; }
tar() { touch "$TEST_ROOT/tar-ran"; }
if ensure_attestation_gh "$TEST_ROOT/staging"; then exit 91; fi
`)
		if err != nil || !bytes.Contains(output, []byte("GitHub CLI 下载包 SHA-256 校验失败")) {
			t.Fatalf("bad CLI digest was not rejected: %v\n%s", err, output)
		}
		if _, err := os.Stat(filepath.Join(root, "tar-ran")); !os.IsNotExist(err) {
			t.Fatalf("CLI archive was processed before its digest passed: %v", err)
		}
	})

	t.Run("pinned CLI digest permits only expected member extraction", func(t *testing.T) {
		root, bin, installer := installerFixture(t)
		member := "gh_2.103.2_linux_amd64/bin/gh"
		staging := filepath.Join(root, "staging")
		if err := os.MkdirAll(filepath.Join(staging, filepath.Dir(member)), 0755); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(root, "fixture-gh"), "#!/bin/sh\nexit 0\n", 0755)
		output, err := runInstallerFixture(t, root, bin, installer, `
download_to_file() { printf 'fixture' > "$2"; }
sha256sum() { printf '%s  %s\n' "$github_cli_archive_sha256" "$1"; }
tar() {
  case "$1" in
    -tzf) printf '%s\n' 'gh_2.103.2_linux_amd64/bin/gh' ;;
    -xzf) cp "$TEST_ROOT/fixture-gh" "$4/$5" ;;
  esac
}
ensure_attestation_gh "$TEST_ROOT/staging"
[[ "$gh_cli_path" == "$TEST_ROOT/staging/gh_2.103.2_linux_amd64/bin/gh" && -x "$gh_cli_path" ]]
`)
		if err != nil {
			t.Fatalf("pinned CLI bootstrap fixture failed: %v\n%s", err, output)
		}
	})

	t.Run("GitHub CLI version floor", func(t *testing.T) {
		root, bin, installer := installerFixture(t)
		output, err := runInstallerFixture(t, root, bin, installer, `
installer_gh_version_supported 2.103.2
! installer_gh_version_supported 2.103.1
! installer_gh_version_supported not-a-version
`)
		if err != nil {
			t.Fatalf("version floor check failed: %v\n%s", err, output)
		}
	})
}

func TestFreshInteractiveUsesUFWBeforeSettingCLI(t *testing.T) {
	root, bin, installer := installerFixture(t)
	output, err := runInstallerFixture(t, root, bin, installer, `
release=ubuntu; FRESH_INSTALL=1
config_after_install "$2/usr/local/novas" "$2" <<'INPUT'
y
32095

32096

n
INPUT
`, "SCENARIO=interactive", "NOVAS_DEVELOPMENT_DEFAULT_ADMIN=1")
	if err != nil {
		t.Fatalf("interactive fresh install: %v\n%s", err, output)
	}
	actions, _ := os.ReadFile(filepath.Join(root, "actions"))
	log := string(actions)
	enable, settings := strings.Index(log, "ufw --force enable"), strings.Index(log, "cli-settings")
	if enable < 0 || settings < enable {
		t.Fatal("setting CLI could choose a non-UFW backend", log)
	}
	for _, port := range []string{"2222", "2200", "32095", "32096"} {
		allow := strings.Index(log, "ufw prepend allow "+port+"/tcp")
		if allow < 0 || allow > enable {
			t.Fatal("requested access missing before enable", port, log)
		}
	}
	if strings.Contains(log, "allow 2095/tcp") || strings.Contains(log, "allow 2096/tcp") || strings.Contains(log, "iptables") {
		t.Fatal("opened unused defaults or used iptables", log)
	}
	if !strings.Contains(log, "cli-admin-generated") || !strings.Contains(string(output), "0123456789abcdef0123456789abcdef0123456789abcdef") {
		t.Fatalf("fresh install did not create and display a one-time random admin password: actions=%s output=%s", log, output)
	}
}

func TestWindowsInstallerReportsConfigurationAndServiceFailures(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	installer, err := os.ReadFile(filepath.Join(root, "windows/install-windows.bat"))
	if err != nil {
		t.Fatal(err)
	}
	batch := string(installer)
	if !strings.Contains(batch, `-File "%~dp0install-windows.ps1"`) ||
		!strings.Contains(batch, `set "INSTALL_EXIT_CODE=%ERRORLEVEL%"`) ||
		!strings.Contains(batch, `exit /b %INSTALL_EXIT_CODE%`) {
		t.Fatal("Windows batch installer must invoke the transactional PowerShell installer and propagate its exit code")
	}
	powershellInstallerBytes, err := os.ReadFile(filepath.Join(root, "windows/install-windows.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	powershellInstaller := string(powershellInstallerBytes)
	initializerStart := strings.Index(powershellInstaller, "function Initialize-NovaPanelFreshInstall")
	initializerEnd := strings.Index(powershellInstaller, "function Wait-NovaPanelHealthy")
	if initializerStart < 0 || initializerEnd < 0 || initializerEnd <= initializerStart {
		t.Fatal("Windows installer must isolate first-install initialization from the upgrade path")
	}
	initializer := powershellInstaller[initializerStart:initializerEnd]
	for _, token := range []string{"'setting', '-port'", "'admin', '-username'", "'-password-prompt'"} {
		if !strings.Contains(initializer, token) {
			t.Fatalf("Windows fresh-install initializer is missing %q", token)
		}
	}
	mainFlow := powershellInstaller[strings.Index(powershellInstaller, "$target = Get-NovaPanelFullPath $InstallDirectory"):]
	ordered := []string{
		"$isFreshInstall = -not (Test-Path -LiteralPath $target)",
		"$stage = New-NovaPanelInstallStage",
		"Set-NovaPanelInstallAcl -Path $stage",
		"$env:NOVAS_DB_FOLDER = Join-Path $stage 'db'",
		"'upgrade-db'",
		"'checkdb'",
		"if ($isFreshInstall) {",
		"Initialize-NovaPanelFreshInstall -Executable $stagedExecutable",
		"Write-Host 'Existing panel settings and administrator credentials will be preserved.'",
		"'Final staged database validation'",
		"$commit = Commit-NovaPanelInstallStage",
		"-Arguments @('refresh')",
		"Start-Service -Name 'novas'",
		"Wait-NovaPanelHealthy",
		"Restore-NovaPanelInstallBackup -InstallDirectory $target",
	}
	searchFrom := 0
	for _, token := range ordered {
		index := strings.Index(mainFlow[searchFrom:], token)
		if index < 0 {
			t.Fatalf("Windows transactional installer is missing ordered stage/verify/swap/health/rollback step %q", token)
		}
		searchFrom += index + len(token)
	}
	if strings.Contains(mainFlow, "$configuration = Read-NovaPanelConfiguration") ||
		strings.Contains(mainFlow, "'setting', '-port'") || strings.Contains(mainFlow, "'admin', '-username'") {
		t.Fatal("Windows upgrade flow must not prompt for or reset panel settings and administrator credentials")
	}
	transactionBytes, err := os.ReadFile(filepath.Join(root, "windows/install-transaction.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	transaction := string(transactionBytes)
	for _, token := range []string{
		"/COPY:DATS",
		".novas-backup-",
		".novas-managed-backup",
		"function Restore-NovaPanelInstallBackup",
		"Select-Object -Skip 3",
	} {
		if !strings.Contains(transaction, token) {
			t.Fatalf("Windows installer transaction helper is missing %q", token)
		}
	}
	if strings.Contains(powershellInstaller, "Remove-Item -LiteralPath $target -Recurse") ||
		!strings.Contains(powershellInstaller, "NOVAS_DB_FOLDER") ||
		!strings.Contains(powershellInstaller, "'*S-1-5-32-545") {
		t.Fatal("Windows installer must preserve the live tree until staged validation and keep private install data protected")
	}

	controller, err := os.ReadFile(filepath.Join(root, "windows/novas-windows.bat"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(controller), "Failed to install service. Error code: !errorLevel!") ||
		!strings.Contains(string(controller), "Failed to uninstall service. Error code: !errorLevel!") {
		t.Fatal("Windows service management error messages must use delayed error-level expansion")
	}
	if !strings.Contains(string(controller), "Manual mode needs Administrator access") ||
		!strings.Contains(string(controller), "Viewing database-backed settings needs Administrator access") ||
		!strings.Contains(string(controller), "Viewing local service logs needs Administrator access") ||
		strings.Contains(string(controller), "net session >nul 2>&1") {
		t.Fatal("Windows manual mode and protected data views must use a reliable administrator check")
	}
}

func TestWindowsUninstallerFailsClosedUntilServiceIsRemoved(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	uninstaller, err := os.ReadFile(filepath.Join(root, "windows/uninstall-windows.bat"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(uninstaller)
	ordered := []string{
		"sc query %SERVICE_NAME% >nul 2>&1",
		`set "SERVICE_QUERY_RESULT=!errorLevel!"`,
		`if not "!SERVICE_QUERY_RESULT!"=="0" if not "!SERVICE_QUERY_RESULT!"=="1060" (`,
		`if "!SERVICE_QUERY_RESULT!"=="0" (`,
		`if not exist "%INSTALL_DIR%\novas-service.exe" (`,
		`"%INSTALL_DIR%\novas-service.exe" uninstall >nul 2>&1`,
		"sc query %SERVICE_NAME% >nul 2>&1",
		`set "SERVICE_QUERY_RESULT=!errorLevel!"`,
		`if not "!SERVICE_QUERY_RESULT!"=="1060" (`,
		"No application files, user data, shortcuts, or firewall rules have been removed.",
		"exit /b 1",
		"REM Remove desktop shortcut",
		"REM Remove firewall rules",
	}
	searchFrom := 0
	for _, token := range ordered {
		index := strings.Index(text[searchFrom:], token)
		if index < 0 {
			t.Fatalf("Windows uninstaller does not fail closed in the expected order near %q", token)
		}
		searchFrom += index + len(token)
	}
	dataPrompt := strings.Index(text, "choice /C YN")
	stopService := strings.Index(text, "REM Stop and remove Windows Service")
	if dataPrompt < 0 || stopService < 0 || dataPrompt > stopService {
		t.Fatal("Windows uninstaller must ask about data retention before changing service or installation state")
	}
}
