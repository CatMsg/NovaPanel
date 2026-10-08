package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func freshUFWScript(t *testing.T) string {
	t.Helper()
	script, err := os.ReadFile("test-fresh-ufw-linux.sh")
	if err != nil {
		t.Fatal(err)
	}
	return string(script)
}

// Load definitions only. None of the privileged native acceptance body runs.
func runFreshUFWHelper(t *testing.T, body string, env ...string) (string, error) {
	t.Helper()
	return runFreshUFWHelperContents(t, freshUFWScript(t), body, env...)
}

func runFreshUFWHelperContents(t *testing.T, contents, body string, env ...string) (string, error) {
	t.Helper()
	main := strings.Index(contents, "\nrequire_github_hosted_release_job\n")
	if main < 0 {
		t.Fatal("cannot locate guarded acceptance entry point")
	}
	cmd := exec.Command("bash", "-c", contents[:main]+"\n"+body)
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func TestFreshUFWAcceptanceUsesValidatedAbsoluteSSHD(t *testing.T) {
	contents := freshUFWScript(t)

	resolve := strings.Index(contents, `SSHD_BIN=$(command -v sshd) || die "OpenSSH daemon executable is unavailable"`)
	validate := strings.Index(contents, `[[ "$SSHD_BIN" == /* && -x "$SSHD_BIN" ]] || die "resolved sshd path must be absolute and executable"`)
	if resolve < 0 || validate <= resolve {
		t.Fatal("sshd must be resolved with command -v and validated as an absolute executable before use")
	}

	for _, invocation := range []string{
		`INITIAL_SSH_EFFECTIVE=$(LC_ALL=C "$SSHD_BIN" -T)`,
		`sshd_effective=$(LC_ALL=C "$SSHD_BIN" -T -f "$WORK_DIR/sshd_config")`,
		`"$SSHD_BIN" -t -f "$WORK_DIR/sshd_config"`,
		`"$SSHD_BIN" -D -f "$WORK_DIR/sshd_config" -E "$WORK_DIR/sshd.log" &`,
	} {
		if position := strings.Index(contents, invocation); position <= validate {
			t.Errorf("expected validated absolute sshd path invocation after guard: %s", invocation)
		}
	}

	for lineNumber, line := range strings.Split(contents, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 1 && fields[0] == "sshd" && (fields[1] == "-T" || fields[1] == "-t" || fields[1] == "-D") {
			t.Errorf("line %d invokes sshd without the validated absolute path", lineNumber+1)
		}
	}
}

func TestFreshUFWWorkflowWhitelist(t *testing.T) {
	contents := freshUFWScript(t)
	start := strings.Index(contents, `    case "${NOVAS_GITHUB_WORKFLOW:-}" in`)
	if start < 0 {
		t.Fatal("missing fixed workflow whitelist in hosted-runner guard")
	}
	end := strings.Index(contents[start:], "    esac\n")
	guardEnd := strings.Index(contents, "\nufw_user_rules()")
	if end < 0 || guardEnd < 0 || start+end >= guardEnd {
		t.Fatal("workflow whitelist must remain inside the hosted-runner guard")
	}
	for _, workflow := range []string{
		"发布 NovaPanel", "NovaPanel 安装验收", "", "arbitrary workflow",
		"发布 NovaPanel copy", "NovaPanel 安装验收 copy", " NovaPanel 安装验收", "NovaPanel 安装验收\n",
	} {
		t.Run(workflow, func(t *testing.T) {
			output, err := runFreshUFWHelper(t, contents[start:start+end+len("    esac\n")],
				"NOVAS_GITHUB_WORKFLOW="+workflow)
			wantOK := workflow == "发布 NovaPanel" || workflow == "NovaPanel 安装验收"
			if (err == nil) != wantOK {
				t.Fatalf("workflow=%q allowed=%v, want %v: %s", workflow, err == nil, wantOK, output)
			}
			if !wantOK && !strings.Contains(output, "refusing a different workflow") {
				t.Fatalf("missing refusal diagnostic: %s", output)
			}
		})
	}
}

func TestFreshUFWRuntimeDirectoryOrdering(t *testing.T) {
	contents := freshUFWScript(t)
	previous := -1
	for _, step := range []string{
		"\nrequire_github_hosted_release_job\n",
		`[[ "${ID:-}" == "ubuntu" ]]`,
		`[[ "$SSHD_BIN" == /* && -x "$SSHD_BIN" ]]`,
		"\nSSHD_RUN_DIR_CREATED=0\n",
		"\ntrap cleanup_ssh_preflight EXIT\n",
		"\n    mkdir -m 0755 /run/sshd",
		"\n    SSHD_RUN_DIR_CREATED=1\n",
		`INITIAL_SSH_EFFECTIVE=$(LC_ALL=C "$SSHD_BIN" -T)`,
		"INITIAL_SSH_FINGERPRINT=$(ssh_config_fingerprint)",
		"\nWORK_DIR=$(mktemp",
		"\nSSH_BASELINE_READY=1\n",
		"\ntrap cleanup EXIT\n",
	} {
		position := strings.Index(contents, step)
		if position <= previous {
			t.Fatalf("missing or out-of-order runtime-directory step: %s", step)
		}
		previous = position
	}
	if strings.Count(contents, "\nSSHD_RUN_DIR_CREATED=0\n") != 1 ||
		strings.Count(contents, "\n    SSHD_RUN_DIR_CREATED=1\n") != 1 {
		t.Fatal("runtime-directory ownership must not be reset during cleanup handoff")
	}
}

func TestNativeAcceptanceScriptsAreIndependent(t *testing.T) {
	contents := freshUFWScript(t)
	for _, forbidden := range []string{"ACME", "acme", "crontab", " cron", "runuser"} {
		if strings.Contains(contents, forbidden) {
			t.Errorf("ACME/cron work must not run in the UFW harness: %s", forbidden)
		}
	}
	previous := -1
	for _, step := range []string{
		`apt-get install -y --no-upgrade --no-install-recommends ufw openssh-client iproute2`,
		`verify_ssh_baseline_unchanged || die "package installation changed host SSH configuration, ownership or listeners"`,
		`ssh-keygen -q -t ed25519 -N '' -f "$WORK_DIR/ssh_host_ed25519_key"`,
		`(set -o noclobber; cat "$WORK_DIR/ssh-global-drop-in" >"$SSH_DROP_IN")`,
		`install_fresh_ufw "$WORK_DIR/panel-home" "" "$PANEL_PORT" "$SUB_PORT"`,
	} {
		position := strings.Index(contents, step)
		if strings.Count(contents, step) != 1 || position <= previous {
			t.Fatalf("missing, duplicated or out-of-order isolated acceptance step: %s", step)
		}
		previous = position
	}

	for _, line := range strings.Split(contents, "\n") {
		fields := strings.Fields(line)
		for i, field := range fields {
			if i >= 2 && fields[i-2] == "command" && fields[i-1] == "-v" {
				continue
			}
			if field == "systemctl" && (i+1 == len(fields) || (fields[i+1] != "show" && fields[i+1] != "is-active")) {
				t.Errorf("UFW harness must only inspect systemd: %s", line)
			}
		}
	}
	cron := acmeCronScript(t)
	for _, required := range []string{
		`apt-get install -y --no-upgrade --no-install-recommends cron curl`,
		`systemctl enable --now cron`, `runuser -u "$RUNNER_USER" -- env -i`,
		`HOME="$ACME_USER_HOME"`, `ACME_USER_HOME="$WORK_DIR/acme-home"`,
		`ensure_acme_cron prepare_cloudflare_acme install_acme_payload`,
		"        prepare_cloudflare_acme\n", "        verify_acme_renewal_cron\n",
		`"$HOME/.acme.sh/acme.sh" --version`,
		`[[ "$(count_owned_acme_jobs "$ACME_CRONTAB_AFTER")" == "1" ]]`,
		`cmp -s "$ACME_CRONTAB_BEFORE" "$ACME_CRONTAB_WITHOUT_OWNED"`,
		"trap cleanup EXIT", "trap 'exit 130' INT", "trap 'exit 143' TERM",
	} {
		if !strings.Contains(cron, required) {
			t.Errorf("missing real isolated ACME/cron acceptance: %s", required)
		}
	}
	for _, forbidden := range []string{"test-fresh-ufw", "sshd", "/etc/ssh", "ufw ", "verify_ssh_baseline", "--issue", "--renew", "CF_Token=", "secrets."} {
		if strings.Contains(cron, forbidden) {
			t.Errorf("cron harness crosses isolation/no-CA boundary: %s", forbidden)
		}
	}
	guard := strings.Index(cron, "\nrequire_github_hosted_release_job\n")
	for _, mutation := range []string{"\nWORK_DIR=$(mktemp", "\napt-get update", "\nsystemctl enable"} {
		if guard < 0 || strings.Index(cron, mutation) <= guard {
			t.Errorf("cron mutation precedes hosted-runner guard: %s", mutation)
		}
	}
}

func acmeCronScript(t *testing.T) string {
	t.Helper()
	contents, err := os.ReadFile("test-acme-cron-linux.sh")
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func TestNativeAcceptanceJobGuards(t *testing.T) {
	for _, harness := range []struct{ job, optIn, script string }{
		{"test-fresh-ufw-linux", "NOVAS_FRESH_UFW_OPT_IN", freshUFWScript(t)},
		{"test-acme-cron-linux", "NOVAS_ACME_CRON_OPT_IN", acmeCronScript(t)},
	} {
		t.Run(harness.job, func(t *testing.T) {
			// Mock identity inspection only; never execute the privileged body.
			contents := strings.ReplaceAll(harness.script, `"$EUID"`, `"$FIXTURE_EUID"`)
			valid := map[string]string{
				harness.optIn: "1", "NOVAS_GITHUB_ACTIONS": "true",
				"NOVAS_RUNNER_ENVIRONMENT": "github-hosted", "NOVAS_GITHUB_JOB": harness.job,
				"NOVAS_GITHUB_WORKFLOW": "发布 NovaPanel", "NOVAS_GITHUB_EVENT_NAME": "workflow_dispatch",
				"NOVAS_GITHUB_SERVER_URL": "https://github.com", "NOVAS_GITHUB_REPOSITORY": "CatMsg/NovaPanel",
				"NOVAS_GITHUB_RUN_ID": "123", "NOVAS_GITHUB_RUN_ATTEMPT": "1",
				"NOVAS_RUNNER_OS": "Linux", "NOVAS_RUNNER_NAME": "GitHub Actions 1",
				"NOVAS_RUNNER_TEMP": "/home/runner/work/_temp", "NOVAS_WORKSPACE": "/home/runner/work/NovaPanel/NovaPanel",
				"SUDO_USER": "runner", "FIXTURE_EUID": "0", "FIXTURE_UNAME": "Linux", "FIXTURE_RUNNER_UID": "1001",
			}
			cases := []struct {
				key, value string
				allowed    bool
			}{
				{"", "", true}, {"NOVAS_GITHUB_WORKFLOW", "NovaPanel 安装验收", true},
				{harness.optIn, "", false}, {harness.optIn, "true", false},
				{"NOVAS_GITHUB_ACTIONS", "false", false}, {"NOVAS_RUNNER_ENVIRONMENT", "self-hosted", false},
				{"NOVAS_GITHUB_JOB", "other-job", false}, {"NOVAS_GITHUB_JOB", "", false},
				{"NOVAS_GITHUB_WORKFLOW", "发布 NovaPanel copy", false}, {"NOVAS_GITHUB_WORKFLOW", "", false},
				{"NOVAS_GITHUB_EVENT_NAME", "push", false}, {"NOVAS_GITHUB_SERVER_URL", "https://example.com", false},
				{"NOVAS_GITHUB_REPOSITORY", "other/NovaPanel", false},
				{"NOVAS_GITHUB_RUN_ID", "", false}, {"NOVAS_GITHUB_RUN_ID", "123x", false},
				{"NOVAS_GITHUB_RUN_ATTEMPT", "", false}, {"NOVAS_GITHUB_RUN_ATTEMPT", "x1", false},
				{"NOVAS_RUNNER_OS", "macOS", false}, {"FIXTURE_UNAME", "Darwin", false},
				{"NOVAS_RUNNER_NAME", "self-hosted", false}, {"NOVAS_RUNNER_TEMP", "/tmp", false},
				{"NOVAS_WORKSPACE", "/tmp/NovaPanel", false}, {"FIXTURE_EUID", "1001", false},
				{"SUDO_USER", "root", false}, {"FIXTURE_RUNNER_UID", "0", false},
			}
			otherJob := "test-fresh-ufw-linux"
			if harness.job == otherJob {
				otherJob = "test-acme-cron-linux"
			}
			cases = append(cases, struct {
				key, value string
				allowed    bool
			}{"NOVAS_GITHUB_JOB", otherJob, false})
			for _, tt := range cases {
				t.Run(tt.key+"="+tt.value, func(t *testing.T) {
					env := make([]string, 0, len(valid))
					for key, value := range valid {
						if key == tt.key {
							value = tt.value
						}
						env = append(env, key+"="+value)
					}
					output, err := runFreshUFWHelperContents(t, contents, `
uname() { printf '%s\n' "$FIXTURE_UNAME"; }
id() { [[ "$*" == '-u runner' ]] || return 1; printf '%s\n' "$FIXTURE_RUNNER_UID"; }
require_github_hosted_release_job
printf 'guard accepted\n'
`, env...)
					if (err == nil) != tt.allowed || strings.Contains(output, "guard accepted") != tt.allowed {
						t.Fatalf("guard allowed=%v, want %v: %v\n%s", err == nil, tt.allowed, err, output)
					}
				})
			}
		})
	}
}

func TestNativeAcceptanceWorkflowIsolationAndReleaseGates(t *testing.T) {
	for _, path := range []string{"../.github/workflows/native-install-test.yml", "../.github/workflows/release.yml"} {
		t.Run(path, func(t *testing.T) {
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var workflow struct{ Jobs map[string]yaml.Node }
			if err := yaml.Unmarshal(contents, &workflow); err != nil {
				t.Fatal(err)
			}
			for _, tt := range []struct{ job, optIn, script string }{
				{"test-fresh-ufw-linux", "NOVAS_FRESH_UFW_OPT_IN", "test-fresh-ufw-linux.sh"},
				{"test-acme-cron-linux", "NOVAS_ACME_CRON_OPT_IN", "test-acme-cron-linux.sh"},
			} {
				node, exists := workflow.Jobs[tt.job]
				if !exists {
					t.Fatalf("missing independent job %s", tt.job)
				}
				var job struct {
					RunsOn          string `yaml:"runs-on"`
					ContinueOnError bool   `yaml:"continue-on-error"`
					If              string
					Needs           []string
					Permissions     map[string]string
					Steps           []struct {
						If, Run         string
						ContinueOnError bool `yaml:"continue-on-error"`
						Env             map[string]string
					}
				}
				if err := node.Decode(&job); err != nil {
					t.Fatal(err)
				}
				if job.RunsOn != "ubuntu-latest" || job.If != "" || job.ContinueOnError || job.Permissions["contents"] != "read" {
					t.Fatalf("job %s must run unconditionally on its own hosted runner with read-only contents", tt.job)
				}
				for _, dependency := range job.Needs {
					if dependency != "test-backend" {
						t.Errorf("acceptance jobs must not depend on one another: %s needs %s", tt.job, dependency)
					}
				}
				found := 0
				for _, step := range job.Steps {
					if step.ContinueOnError || strings.Contains(step.Run, "|| true") || strings.Contains(step.Run, "secrets.") {
						t.Errorf("acceptance bypass or credential reference in %s", tt.job)
					}
					other := "test-acme-cron-linux.sh"
					if tt.script == other {
						other = "test-fresh-ufw-linux.sh"
					}
					if strings.Contains(step.Run, other) {
						t.Errorf("%s invokes the other environment's script", tt.job)
					}
					if !strings.Contains(step.Run, tt.script) {
						continue
					}
					syntax := exec.Command("bash", "-n")
					syntax.Stdin = strings.NewReader(step.Run)
					if output, err := syntax.CombinedOutput(); err != nil {
						t.Errorf("invalid workflow shell syntax in %s: %v\n%s", tt.job, err, output)
					}
					found++
					if step.If != "" || step.Env[tt.optIn] != "1" || !strings.Contains(step.Run, "sudo env") ||
						!strings.Contains(step.Run, tt.optIn+`="$`+tt.optIn+`"`) ||
						!strings.Contains(step.Run, `NOVAS_GITHUB_ACTIONS="$GITHUB_ACTIONS"`) {
						t.Errorf("missing unconditional opt-in/root/action identity forwarding in %s", tt.job)
					}
					for key, expression := range map[string]string{
						"NOVAS_RUNNER_ENVIRONMENT": "runner.environment", "NOVAS_GITHUB_JOB": "github.job",
						"NOVAS_GITHUB_WORKFLOW": "github.workflow", "NOVAS_GITHUB_EVENT_NAME": "github.event_name",
						"NOVAS_GITHUB_SERVER_URL": "github.server_url", "NOVAS_GITHUB_REPOSITORY": "github.repository",
						"NOVAS_GITHUB_RUN_ID": "github.run_id", "NOVAS_GITHUB_RUN_ATTEMPT": "github.run_attempt",
						"NOVAS_RUNNER_OS": "runner.os", "NOVAS_RUNNER_NAME": "runner.name",
						"NOVAS_RUNNER_TEMP": "runner.temp", "NOVAS_WORKSPACE": "github.workspace",
					} {
						if step.Env[key] != "${{ "+expression+" }}" || !strings.Contains(step.Run, key+`="$`+key+`"`) {
							t.Errorf("missing exact %s forwarding in %s", key, tt.job)
						}
					}
				}
				if found != 1 {
					t.Errorf("expected exactly one guarded script invocation in %s, got %d", tt.job, found)
				}
			}
			if strings.HasSuffix(path, "/release.yml") {
				var build struct {
					Needs []string
					If    string
				}
				node := workflow.Jobs["build-linux"]
				if err := node.Decode(&build); err != nil {
					t.Fatal(err)
				}
				if build.If != "" {
					t.Fatal("release build must not override failed acceptance dependencies")
				}
				for _, gate := range []string{"test-fresh-ufw-linux", "test-acme-cron-linux"} {
					found := false
					for _, dependency := range build.Needs {
						if dependency == gate {
							found = true
						}
					}
					if !found {
						t.Errorf("release build missing required gate %s", gate)
					}
				}
			}
		})
	}
}

func TestACMECronOwnedCleanup(t *testing.T) {
	for _, tt := range []struct {
		name, before               string
		present                    bool
		owned                      int
		drift, readFail, stripFail bool
		status                     int
	}{
		{name: "originally absent", owned: 1},
		{name: "originally empty", present: true, owned: 1},
		{name: "unrelated and lookalike jobs retained", present: true, before: "fixtures", owned: 1},
		{name: "all exact owned jobs removed", present: true, before: "fixtures", owned: 2},
		{name: "unrelated concurrent addition retained and surfaced", owned: 1, drift: true, status: 1},
		{name: "listing error refuses mutation", owned: 1, readFail: true, status: 1},
		{name: "filter error refuses mutation", owned: 1, stripFail: true, status: 1},
		{name: "original failure preserved", owned: 1, status: 23},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			work := filepath.Join(dir, "work")
			if err := os.Mkdir(work, 0700); err != nil {
				t.Fatal(err)
			}
			home := filepath.Join(work, "acme home", ".acme.sh")
			owned := `5 4 * * * "` + home + `"/acme.sh --cron --home "` + home + `" > /dev/null` + "\n"
			before := ""
			if tt.before != "" {
				before = "# preserve this comment\nMAILTO=runner\n0 * * * * echo unrelated\n" +
					"# " + owned +
					`5 4 * * * "` + home + `"/acme.sh --cron --home "` + home + `-other"` + "\n" +
					`5 4 * * * "` + home + `"/acme.sh --cron --home "` + home + `"suffix` + "\n" +
					`5 4 * * * sh -c '"` + home + `"/acme.sh --cron --home "` + home + `"'` + "\n"
			}
			current := before + strings.Repeat(owned, tt.owned)
			want := before
			if tt.drift {
				current += "0 2 * * * echo added-concurrently\n"
				want += "0 2 * * * echo added-concurrently\n"
			}
			if tt.readFail || tt.stripFail {
				want = current
			}
			for path, data := range map[string]string{filepath.Join(work, "before"): before, filepath.Join(dir, "current"): current} {
				if err := os.WriteFile(path, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			present := "0"
			if tt.present {
				present = "1"
			}
			originalStatus := "0"
			if tt.status == 23 {
				originalStatus = "23"
			}
			body := `
RUNNER_USER=runner
WORK_DIR="$FIXTURE_ROOT/work"
ACME_USER_HOME="$WORK_DIR/acme home"
ACME_CRONTAB_BEFORE="$WORK_DIR/before"
ACME_CRONTAB_WAS_PRESENT="$FIXTURE_PRESENT"
ACME_CRONTAB_READY=1
crontab() {
    [[ "$1" == -u && "$2" == runner ]] || return 1
    case "$3" in
        -l)
            if [[ "$FIXTURE_READ_FAIL" == true ]]; then printf 'permission denied\n' >&2; return 1; fi
            if [[ -f "$FIXTURE_ROOT/current" ]]; then cat "$FIXTURE_ROOT/current"
            else printf 'no crontab for runner\n' >&2; return 1; fi ;;
        -r) rm "$FIXTURE_ROOT/current" ;;
        *) cp "$3" "$FIXTURE_ROOT/current" ;;
    esac
}
if [[ "$FIXTURE_STRIP_FAIL" == true ]]; then strip_owned_acme_jobs() { return 1; }; fi
trap cleanup EXIT
exit "$FIXTURE_STATUS"
`
			output, err := runFreshUFWHelperContents(t, acmeCronScript(t), body,
				"FIXTURE_ROOT="+dir, "FIXTURE_PRESENT="+present, "FIXTURE_STATUS="+originalStatus,
				"FIXTURE_READ_FAIL="+strconv.FormatBool(tt.readFail),
				"FIXTURE_STRIP_FAIL="+strconv.FormatBool(tt.stripFail))
			status := 0
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					status = exitErr.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			if status != tt.status {
				t.Fatalf("cleanup status=%d, want %d: %s", status, tt.status, output)
			}
			data, readErr := os.ReadFile(filepath.Join(dir, "current"))
			wantPresent := tt.present || want != ""
			if wantPresent {
				if readErr != nil || string(data) != want {
					t.Fatalf("unowned crontab changed: %v\ngot %q\nwant %q", readErr, data, want)
				}
			} else if !os.IsNotExist(readErr) {
				t.Fatalf("originally absent crontab not removed: %v", readErr)
			}
			_, workErr := os.Stat(work)
			if (tt.status == 1 && workErr != nil) || (tt.status != 1 && !os.IsNotExist(workErr)) {
				t.Fatalf("cleanup recovery directory state incorrect: %v", workErr)
			}
		})
	}
}

func TestFreshUFWRuntimeDirectoryCleanup(t *testing.T) {
	contents := freshUFWScript(t)
	main := strings.Index(contents, "\nrequire_github_hosted_release_job\n")
	prepare := strings.Index(contents, "\nSSHD_RUN_DIR_CREATED=0\n")
	preflight := strings.Index(contents, `source "${NOVAS_WORKSPACE}/install.sh"`)
	handoff := strings.Index(contents, "\nWORK_DIR=$(mktemp")
	handoffEnd := strings.Index(contents, "\nchmod 0755 \"$WORK_DIR\"")
	if main < 0 || prepare < main || preflight <= prepare || handoff <= preflight || handoffEnd <= handoff {
		t.Fatal("cannot isolate definitions, guarded preparation, and cleanup handoff")
	}
	for _, tt := range []struct {
		name, initial, stage string
		status               int
		remains              bool
	}{
		{"first sshd T fails", "missing", "sshd", 1, false},
		{"later baseline fails", "missing", "baseline", 1, false},
		{"full cleanup succeeds", "missing", "full", 0, false},
		{"full cleanup preserves failure", "missing", "fullfail", 23, false},
		{"existing directory early failure", "directory", "sshd", 1, true},
		{"existing directory full cleanup", "directory", "full", 0, true},
		{"linked directory refused", "symlink", "full", 1, true},
		{"dangling link refused", "dangling", "full", 1, true},
		{"unknown file refused", "file", "full", 1, true},
		{"changed identity retained", "missing", "changed", 1, true},
		{"changed link retained", "missing", "linked", 1, true},
		{"nonempty owned directory retained", "missing", "nonempty", 1, true},
		{"identity inspection fails safely", "missing", "statfail", 1, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			runtimeDir := filepath.Join(dir, "sshd")
			target := filepath.Join(dir, "target")
			if err := os.Mkdir(target, 0700); err != nil {
				t.Fatal(err)
			}
			var setupErr error
			switch tt.initial {
			case "directory":
				setupErr = os.Mkdir(runtimeDir, 0700)
			case "symlink":
				setupErr = os.Symlink(target, runtimeDir)
			case "dangling":
				setupErr = os.Symlink(filepath.Join(dir, "absent"), runtimeDir)
			case "file":
				setupErr = os.WriteFile(runtimeDir, []byte("preserve me"), 0600)
			}
			if setupErr != nil {
				t.Fatal(setupErr)
			}
			// Execute only the real preparation and trap handoff, against temp paths.
			// GNU stat and host/service inspection are fixtures for portability.
			body := contents[:main] + `
stat() {
    [[ "$*" == "-c %d:%i $FIXTURE_RUN_DIR" ]] || return 1
    [[ "$FIXTURE_STAGE" != statfail ]] || return 1
    if [[ -e "$FIXTURE_ROOT/changed" ]]; then printf '1:124\n'; else printf '1:123\n'; fi
}
ufw() { [[ "$*" == status ]] || return 1; printf 'Status: inactive\n'; }
verify_ssh_baseline_unchanged() { [[ -d "$FIXTURE_RUN_DIR" && "$SSH_BASELINE_READY" == 1 ]]; }
fixture_sshd() {
    [[ "$*" == -T && -d "$FIXTURE_RUN_DIR" && ! -L "$FIXTURE_RUN_DIR" ]] || return 1
    [[ "$FIXTURE_STAGE" != sshd ]] || return 37
    printf 'port 22\n'
}
` + contents[prepare:preflight] + `
SSHD_BIN=fixture_sshd
INITIAL_SSH_EFFECTIVE=$(LC_ALL=C "$SSHD_BIN" -T) || die "fixture initial sshd inspection failed"
[[ "$FIXTURE_STAGE" != baseline ]] || die "fixture baseline inspection failed"
` + contents[handoff:handoffEnd] + `
case "$FIXTURE_STAGE" in
    changed) touch "$FIXTURE_ROOT/changed" ;;
    linked) rmdir "$FIXTURE_RUN_DIR"; ln -s "$FIXTURE_ROOT/target" "$FIXTURE_RUN_DIR" ;;
    nonempty) touch "$FIXTURE_RUN_DIR/unowned" ;;
    fullfail) exit 23 ;;
esac
`
			body = strings.ReplaceAll(body, "/run/sshd", runtimeDir)
			cmd := exec.Command("bash", "-c", body)
			cmd.Env = append(os.Environ(), "FIXTURE_ROOT="+dir, "FIXTURE_RUN_DIR="+runtimeDir,
				"FIXTURE_STAGE="+tt.stage, "NOVAS_RUNNER_TEMP="+dir)
			output, err := cmd.CombinedOutput()
			status := 0
			if err != nil {
				exitErr, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatal(err)
				}
				status = exitErr.ExitCode()
			}
			if status != tt.status {
				t.Fatalf("exit=%d, want %d: %s", status, tt.status, output)
			}
			if tt.stage == "sshd" && !strings.Contains(string(output), "fixture initial sshd inspection failed") {
				t.Fatalf("failure did not reach the initial sshd preflight: %s", output)
			}
			if tt.stage == "baseline" && !strings.Contains(string(output), "fixture baseline inspection failed") {
				t.Fatalf("failure did not reach the later baseline preflight: %s", output)
			}
			info, statErr := os.Lstat(runtimeDir)
			if tt.remains {
				if statErr != nil {
					t.Fatalf("unowned/unknown runtime path was removed: %v\n%s", statErr, output)
				}
				if tt.initial == "directory" && info.Mode().Perm() != 0700 {
					t.Fatalf("existing directory permissions changed: %v", info.Mode())
				}
			} else if !os.IsNotExist(statErr) {
				t.Fatalf("owned runtime directory leaked: %v\n%s", statErr, output)
			}
		})
	}
}

func TestFreshUFWBaselineOwnership(t *testing.T) {
	v4 := `LISTEN 0 128 0.0.0.0:22 0.0.0.0:* users:(("sshd",pid=411,fd=3))`
	v6 := `LISTEN 0 128 [::]:22 [::]:* users:(("sshd",pid=411,fd=4))`
	config := "port 22\nlistenaddress 0.0.0.0:22\nlistenaddress [::]:22"
	tests := []struct {
		name, listeners, config, ports string
		ok                             bool
	}{
		{"hosted SSH22", v4 + "\n" + v6, config, "22", true},
		{"IPv4 only baseline", v4, config, "22", true},
		{"known socket", strings.ReplaceAll(v6, `"sshd",pid=411`, `"systemd",pid=1`), config, "22", true},
		{"socket and service coownership", strings.Replace(v6, "fd=4))", `fd=4),("systemd",pid=1,fd=9))`, 1), config, "22", true},
		{"known socket override", strings.ReplaceAll(strings.ReplaceAll(v6, ":22", ":2200"), `"sshd",pid=411`, `"systemd",pid=1`), config, "2200", true},
		{"multiple actual ports", v4 + "\n" + strings.ReplaceAll(v6, ":22", ":2200"), config + "\nport 2200", "22\n2200", true},
		{"unrelated listener", v4 + "\nLISTEN 0 128 127.0.0.1:8080 0.0.0.0:* users:((\"node\",pid=555,fd=3))", config, "22", true},
		{"unknown owner on SSH port", strings.ReplaceAll(v4, `"sshd"`, `"node"`), config, "22", false},
		{"unknown coowner", strings.Replace(v4, "fd=3))", `fd=3),("node",pid=555,fd=9))`, 1), config, "22", false},
		{"wrong daemon PID", strings.ReplaceAll(v4, "pid=411", "pid=555"), config, "22", false},
		{"non-init systemd", strings.ReplaceAll(v6, `"sshd",pid=411`, `"systemd",pid=555`), config, "22", false},
		{"unverified socket", strings.ReplaceAll(strings.ReplaceAll(v6, ":22", ":2201"), `"sshd",pid=411`, `"systemd",pid=1`), config, "2201", false},
		{"sshd-session is not service listener", strings.ReplaceAll(v4, `"sshd"`, `"sshd-session"`), config, "22", false},
		{"missing ownership", "LISTEN 0 128 0.0.0.0:22 0.0.0.0:*", config, "22", false},
		{"malformed ownership suffix", v4 + " unexpected", config, "22", false},
		{"unknown SSH port", strings.ReplaceAll(v4, ":22", ":2200"), config, "22", false},
		{"missing expected port", v4, config, "22\n2200", false},
		{"invalid config port", v4, "port invalid", "22", false},
		{"empty baseline", "", config, "22", false},
	}
	for _, port := range []string{"2222", "39095", "39096", "39998"} {
		tests = append(tests, struct {
			name, listeners, config, ports string
			ok                             bool
		}{"reserved " + port, v4 + "\nLISTEN 0 128 127.0.0.1:" + port + " 0.0.0.0:*", config, "22", false})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := runFreshUFWHelper(t, `
verify_baseline_sshd_pid() { [[ "$1" == 411 ]]; }
verify_baseline_socket() { [[ "$1" == '[::]:22' || "$1" == '[::]:2200' ]]; }
inspect_baseline_ssh "$FIXTURE_LISTENERS" "$FIXTURE_CONFIG" "$FIXTURE_PORTS"
`, "FIXTURE_LISTENERS="+tt.listeners, "FIXTURE_CONFIG="+tt.config, "FIXTURE_PORTS="+tt.ports)
			if (err == nil) != tt.ok {
				t.Fatalf("success=%v, want %v: %v\n%s", err == nil, tt.ok, err, output)
			}
		})
	}
}

func TestFreshUFWBaselineNormalizesQueuesAndOwnerOrder(t *testing.T) {
	output, err := runFreshUFWHelper(t, `
verify_baseline_sshd_pid() { [[ "$1" == 411 ]]; }
verify_baseline_socket() { [[ "$1" == '[::]:22' ]]; }
config='port 22'
before='LISTEN 0 128 [::]:22 [::]:* users:(("sshd",pid=411,fd=4),("systemd",pid=1,fd=9))'
after='LISTEN 1 256 [::]:22 [::]:* users:(("systemd",pid=1,fd=9),("sshd",pid=411,fd=4))'
baseline=$(inspect_baseline_ssh "$before" "$config" 22)
[[ "$baseline" == "$(inspect_baseline_ssh "$after" "$config" 22)" ]]
changed_fd="${after/fd=4/fd=5}"
[[ "$baseline" != "$(inspect_baseline_ssh "$changed_fd" "$config" 22)" ]]
`)
	if err != nil {
		t.Fatalf("normalized listener identity comparison failed: %v\n%s", err, output)
	}
}

func TestFreshUFWBaselineRejectsDaemonOverrides(t *testing.T) {
	for _, tt := range []struct {
		command string
		ok      bool
	}{
		{"/usr/sbin/sshd -D", true},
		{"/usr/sbin/sshd -D -e", true},
		{"sshd: /usr/sbin/sshd -D [listener] 0 of 10-100 startups", true},
		{"sshd: /usr/sbin/sshd -D -e [listener] 1 of 10-100 startups", true},
		{"/usr/sbin/sshd -D -f /tmp/config", false},
		{"/usr/sbin/sshd -D -p 22", false},
		{"/usr/sbin/sshd -D -o Port=22", false},
		{"sshd: /usr/sbin/sshd -D -f /tmp/config [listener] 0 of 10-100 startups", false},
		{"sshd: runner [priv]", false},
		{"/unknown/sshd -D", false},
	} {
		t.Run(tt.command, func(t *testing.T) {
			output, err := runFreshUFWHelper(t, `SSHD_BIN=/usr/sbin/sshd; baseline_sshd_command_valid "$FIXTURE_COMMAND"`, "FIXTURE_COMMAND="+tt.command)
			if (err == nil) != tt.ok {
				t.Fatalf("success=%v, want %v: %v\n%s", err == nil, tt.ok, err, output)
			}
		})
	}
}

func TestFreshUFWPortDeclarationRetainsImplicitAndExplicitDefaults(t *testing.T) {
	for _, tt := range []struct{ config, want string }{
		{"port 22\nlistenaddress 0.0.0.0:22\nlistenaddress [::]:22", "Port 22\nPort 2222\n"},
		{"port 2200\nlistenaddress [::]:2200", "Port 2200\nPort 2222\n"},
		{"port 22\nport 2200\nlistenaddress [::]:2255", "Port 22\nPort 2200\nPort 2222\nPort 2255\n"},
	} {
		output, err := runFreshUFWHelper(t, `
{ ssh_config_ports <<<"$FIXTURE_CONFIG"; printf '%s\n' "$SSH_PORT"; } | sort -nu | sed 's/^/Port /'
`, "FIXTURE_CONFIG="+tt.config)
		if err != nil || output != tt.want {
			t.Fatalf("ports=%q, want %q: %v", output, tt.want, err)
		}
	}
}

func TestFreshUFWBaselineSocketBinding(t *testing.T) {
	for _, tt := range []struct {
		name, endpoint, binding, trigger string
		ok                               bool
	}{
		{"Ubuntu wildcard socket22", "*:22", "[::]:22 (Stream)", "ssh.service", true},
		{"bare socket22", "[::]:22", "22 (Stream)", "ssh.service", true},
		{"explicit v4 socket", "0.0.0.0:22", "0.0.0.0:22 (Stream)", "ssh.service", true},
		{"dualstack socket", "[::]:22", "0.0.0.0:22 (Stream) [::]:22 (Stream)", "ssh.service", true},
		{"wrong endpoint", "127.0.0.1:22", "[::]:22 (Stream)", "ssh.service", false},
		{"wrong port", "[::]:22", "[::]:2200 (Stream)", "ssh.service", false},
		{"unknown trigger", "[::]:22", "[::]:22 (Stream)", "unknown.service", false},
		{"ambiguous triggers", "[::]:22", "[::]:22 (Stream)", "ssh.service unknown.service", false},
		{"datagram", "[::]:22", "[::]:22 (Datagram)", "ssh.service", false},
		{"empty binding", "[::]:22", "", "ssh.service", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			output, err := runFreshUFWHelper(t, `
systemctl() {
    case "$1" in
        is-active) [[ "$3" == ssh.socket ]] ;;
        show)
            [[ "$2" == ssh.socket ]] || return 3
            case "$4" in
                Triggers) printf '%s\n' "$FIXTURE_TRIGGER" ;;
                Listen) printf '%s\n' "$FIXTURE_BINDING" ;;
                *) return 1 ;;
            esac ;;
        *) return 1 ;;
    esac
}
verify_baseline_socket "$FIXTURE_ENDPOINT"
`, "FIXTURE_ENDPOINT="+tt.endpoint, "FIXTURE_BINDING="+tt.binding, "FIXTURE_TRIGGER="+tt.trigger)
			if (err == nil) != tt.ok {
				t.Fatalf("success=%v, want %v: %v\n%s", err == nil, tt.ok, err, output)
			}
		})
	}
}

func TestFreshUFWOwnedListenerMatching(t *testing.T) {
	v4 := `LISTEN 0 128 127.0.0.1:2222 0.0.0.0:* users:(("sshd",pid=999999,fd=3))`
	v6 := `LISTEN 0 128 [::1]:2222 [::]:* users:(("sshd",pid=999999,fd=4))`
	for _, tt := range []struct {
		name, listeners string
		ok              bool
	}{
		{"owned dualstack", v4 + "\n" + v6, true},
		{"ss outer whitespace", "  " + v4 + " \t\n" + v6 + " \t", true},
		{"changed queues accepted", strings.Replace(v4, "0 128", "3 256", 1) + "\n" + v6, true},
		{"unrelated private row ignored", v4 + "\n" + v6 + "\nLISTEN 0 128 PRIVATE_HOST:8080 PRIVATE_PEER:* users:((\"PRIVATE_PROCESS\",pid=555,fd=9))", true},
		{"empty", "", false},
		{"missing v6", v4, false},
		{"missing v4", v6, false},
		{"duplicate v4", v4 + "\n" + v4, false},
		{"duplicate v6", v6 + "\n" + v6, false},
		{"wrong PID", v4 + "\n" + strings.ReplaceAll(v6, "999999", "123"), false},
		{"public bind", strings.ReplaceAll(v4, "127.0.0.1", "0.0.0.0") + "\n" + v6, false},
		{"mixed owners", strings.Replace(v4, "fd=3))", `fd=3),("node",pid=555,fd=9))`, 1) + "\n" + v6, false},
		{"extra listener", v4 + "\n" + v6 + "\n" + strings.ReplaceAll(v4, "127.0.0.1", "127.0.0.2"), false},
		{"unknown owner", strings.ReplaceAll(v4, `"sshd"`, `"sshd-session"`) + "\n" + v6, false},
		{"missing owner", `LISTEN 0 128 127.0.0.1:2222 0.0.0.0:*` + "\n" + v6, false},
		{"nonwhitespace suffix", v4 + " suffix \t\n" + v6, false},
		{"invalid fd", strings.Replace(v4, "fd=3", "fd=bad", 1) + "\n" + v6, false},
		{"unbracketed IPv6", v4 + "\n" + strings.Replace(v6, "[::1]:2222", "::1:2222", 1), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			output, err := runFreshUFWHelper(t, `SSHD_PID=999999; owned_ssh_listeners_match <<<"$FIXTURE_LISTENERS"`, "FIXTURE_LISTENERS="+tt.listeners)
			if (err == nil) != tt.ok {
				t.Fatalf("success=%v, want %v: %v\n%s", err == nil, tt.ok, err, output)
			}
		})
	}
}

func TestFreshUFWOwnedListenerDiagnosticsAreBounded(t *testing.T) {
	v4 := `LISTEN 0 128 127.0.0.1:2222 PRIVATE_PEER:* users:(("sshd",pid=999999,fd=3))`
	v6 := `LISTEN 0 128 [::1]:2222 PRIVATE_PEER:* users:(("sshd",pid=123,fd=4))`
	for _, tt := range []struct {
		name, listeners string
		want            []string
	}{
		{"wrong PID", v4 + "\n" + v6, []string{"row=2 endpoint=[::1]:2222 endpoint_allowed=1 ownership_match=0", "row=2 owner=sshd pid=123 fd=4", "rows=2 ipv4=1 ipv6=1"}},
		{"mixed owner", strings.Replace(v4, "fd=3))", `fd=3),("PRIVATE_PROCESS",pid=555,fd=9))`, 1) + "\n" + v6, []string{"row=1 owner=other pid=555 fd=9", "ownership_match=0"}},
		{"missing owner", `LISTEN 0 128 127.0.0.1:2222 PRIVATE_PEER:*`, []string{"owner_metadata=unavailable", "rows=1 ipv4=1 ipv6=0"}},
		{"unsafe endpoint", strings.Replace(v4, "127.0.0.1:2222", "PRIVATE_ENDPOINT:2222", 1), []string{"endpoint=redacted endpoint_allowed=0", "rows=1 ipv4=0 ipv6=0"}},
		{"empty", "", []string{"rows=0 ipv4=0 ipv6=0"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			output, err := runFreshUFWHelper(t, `SSHD_PID=999999; owned_ssh_listeners_match <<<"$FIXTURE_LISTENERS"`,
				"FIXTURE_LISTENERS="+tt.listeners+"\nLISTEN 0 128 PRIVATE_HOST:8080 PRIVATE_PEER:* users:((\"PRIVATE_PROCESS\",pid=777,fd=8))")
			if err == nil {
				t.Fatal("invalid owned listeners accepted")
			}
			for _, want := range tt.want {
				if !strings.Contains(output, want) {
					t.Errorf("missing diagnostic %q: %s", want, output)
				}
			}
			if strings.Contains(output, "PRIVATE_") || strings.Contains(output, "pid=777") || strings.Contains(output, "LISTEN") {
				t.Fatalf("raw/private listener metadata leaked: %s", output)
			}
		})
	}
}

func TestFreshUFWOwnedVerificationDiagnostics(t *testing.T) {
	for _, tt := range []struct {
		changed string
		want    []string
	}{
		{"none", nil},
		{"identity", []string{"stage=identity result=mismatch"}},
		{"identity-query", []string{"stage=identity result=query_failed status=7"}},
		{"listeners-query", []string{"stage=listeners result=query_failed status=8"}},
		{"listeners", []string{"rows=1 ipv4=1 ipv6=0"}},
		{"baseline-query", []string{"stage=baseline_inspection result=query_failed status=9"}},
		{"baseline-endpoint", []string{"endpoint_set_match=0 endpoint_owner_rows_match=0"}},
		{"baseline-owner", []string{"endpoint_set_match=1 endpoint_owner_rows_match=0"}},
		{"units-query", []string{"stage=units result=query_failed status=10"}},
		{"units", []string{"stage=units result=mismatch", "unit=ssh.service property=MainPID match=0", "unit=ssh.service property=ExecStart match=0", "unit=ssh.service property=ActiveState match=1"}},
		{"fingerprint-query", []string{"stage=fingerprint result=query_failed status=11"}},
		{"fingerprint", []string{"stage=fingerprint result=mismatch"}},
	} {
		t.Run(tt.changed, func(t *testing.T) {
			output, err := runFreshUFWHelper(t, `
SSHD_PID=999999
SSHD_IDENTITY=1234
INITIAL_SSH_EFFECTIVE='PRIVATE_CONFIG'
INITIAL_SSH_PORTS=22
INITIAL_SSH_LISTENERS='PRIVATE_HOST:22 sshd,pid=411,fd=3 '
INITIAL_SSH_UNITS=$'ssh.service\nActiveState=active\nMainPID=411\nExecStart=PRIVATE_COMMAND'
INITIAL_SSH_FINGERPRINT='PRIVATE_FINGERPRINT'
process_identity() {
    [[ "$FIXTURE_CHANGED" != identity-query ]] || { printf 'PRIVATE_ERROR\n' >&2; return 7; }
    if [[ "$FIXTURE_CHANGED" == identity ]]; then printf '5678\n'; else printf '1234\n'; fi
}
ss() {
    [[ "$*" == '-H -ltnp' ]] || return 99
    [[ "$FIXTURE_CHANGED" != listeners-query ]] || { printf 'PRIVATE_ERROR\n' >&2; return 8; }
    printf '%s\n' 'LISTEN 0 128 127.0.0.1:2222 0.0.0.0:* users:(("sshd",pid=999999,fd=3))'
    if [[ "$FIXTURE_CHANGED" != listeners ]]; then
        printf '%s\n' 'LISTEN 0 128 [::1]:2222 [::]:* users:(("sshd",pid=999999,fd=4))'
    fi
    printf '%s\n' 'LISTEN 0 128 PRIVATE_HOST:22 PRIVATE_PEER:* users:(("sshd",pid=411,fd=3))'
}
inspect_baseline_ssh() {
    [[ "$1" != *':2222 '* && "$1" == *'PRIVATE_HOST:22 '* && "$2" == "$INITIAL_SSH_EFFECTIVE" && "$3" == 22 ]] || return 99
    case "$FIXTURE_CHANGED" in
        baseline-query) return 9 ;;
        baseline-endpoint) printf 'PRIVATE_CHANGED_HOST:22 sshd,pid=411,fd=3 \n' ;;
        baseline-owner) printf 'PRIVATE_HOST:22 sshd,pid=411,fd=4 \n' ;;
        *) printf '%s\n' "$INITIAL_SSH_LISTENERS" ;;
    esac
}
ssh_unit_state() {
    case "$FIXTURE_CHANGED" in
        units-query) printf 'PRIVATE_ERROR\n' >&2; return 10 ;;
        units) printf '%s\n' $'ssh.service\nActiveState=active\nMainPID=412\nExecStart=PRIVATE_CHANGED_COMMAND' ;;
        *) printf '%s\n' "$INITIAL_SSH_UNITS" ;;
    esac
}
ssh_config_fingerprint() {
    case "$FIXTURE_CHANGED" in
        fingerprint-query) printf 'PRIVATE_ERROR\n' >&2; return 11 ;;
        fingerprint) printf 'PRIVATE_CHANGED_FINGERPRINT\n' ;;
        *) printf '%s\n' "$INITIAL_SSH_FINGERPRINT" ;;
    esac
}
verify_owned_ssh_listeners
`, "FIXTURE_CHANGED="+tt.changed)
			if (err == nil) != (tt.changed == "none") {
				t.Fatalf("changed=%s: %v\n%s", tt.changed, err, output)
			}
			for _, want := range tt.want {
				if !strings.Contains(output, want) {
					t.Errorf("missing %q: %s", want, output)
				}
			}
			if strings.Contains(output, "PRIVATE_") || strings.Contains(output, "pid=411") {
				t.Fatalf("host/config/private metadata leaked: %s", output)
			}
			if tt.changed == "none" && output != "" {
				t.Fatalf("successful verification must stay silent: %s", output)
			}
		})
	}
}

func TestFreshUFWFingerprintExcludesOnlyValidatedOwnedDropIn(t *testing.T) {
	for _, changed := range []string{"none", "unowned", "inode", "content", "symlink", "missing", "host", "binary", "cleanup"} {
		t.Run(changed, func(t *testing.T) {
			dir := t.TempDir()
			sshDir := filepath.Join(dir, "ssh")
			if err := os.MkdirAll(filepath.Join(sshDir, "sshd_config.d"), 0700); err != nil {
				t.Fatal(err)
			}
			contents := strings.ReplaceAll(freshUFWScript(t), "/etc/ssh", sshDir)
			contents = strings.ReplaceAll(contents, "/etc/default/ssh", filepath.Join(dir, "default-ssh"))
			output, err := runFreshUFWHelperContents(t, contents, `
WORK_DIR="$FIXTURE_DIR"
SSHD_BIN="$WORK_DIR/sshd-binary"
printf 'test binary\n' >"$SSHD_BIN"
printf 'test host config\n' >"$FIXTURE_SSH_DIR/sshd_config"
printf 'Port 22\nPort 2222\n' >"$WORK_DIR/ssh-global-drop-in"
SSHD_CONFIG_CREATED=0
SSH_DROP_IN_IDENTITY=1:1234
stat() {
    [[ "$*" == "-Lc %d:%i $SSH_DROP_IN" ]] || return 99
    if [[ "$FIXTURE_CHANGED" == inode ]]; then printf '1:5678\n'; else printf '1:1234\n'; fi
}
if ! command -v sha256sum >/dev/null; then sha256sum() { shasum -a 256 "$@"; }; fi
INITIAL_SSH_FINGERPRINT=$(ssh_config_fingerprint)
cp "$WORK_DIR/ssh-global-drop-in" "$SSH_DROP_IN"
SSHD_CONFIG_CREATED=1
case "$FIXTURE_CHANGED" in
    unowned) SSHD_CONFIG_CREATED=0 ;;
    content) printf 'Port 2200\n' >>"$SSH_DROP_IN" ;;
    symlink) rm "$SSH_DROP_IN"; ln -s "$WORK_DIR/ssh-global-drop-in" "$SSH_DROP_IN" ;;
    missing) rm "$SSH_DROP_IN" ;;
    host) printf 'changed host config\n' >>"$FIXTURE_SSH_DIR/sshd_config" ;;
    binary) printf 'changed binary\n' >>"$SSHD_BIN" ;;
    cleanup)
        SSHD_PID=''
        ALLOW_RULES_OWNED=0
        DENY_RULE_OWNED=0
        SSH_BASELINE_READY=1
        ufw() { printf 'Status: inactive\n'; }
        cleanup_sshd_run_directory() { return 0; }
        verify_ssh_baseline_unchanged() {
            [[ "$SSHD_CONFIG_CREATED" == 0 && ! -e "$SSH_DROP_IN" ]] &&
                [[ "$(ssh_config_fingerprint)" == "$INITIAL_SSH_FINGERPRINT" ]]
        }
        cleanup ;;
esac
fingerprint=$(ssh_config_fingerprint)
[[ "$fingerprint" == "$INITIAL_SSH_FINGERPRINT" ]]
`, "FIXTURE_DIR="+dir, "FIXTURE_SSH_DIR="+sshDir, "FIXTURE_CHANGED="+changed)
			wantOK := changed == "none" || changed == "cleanup"
			if (err == nil) != wantOK {
				t.Fatalf("changed=%s success=%v want=%v: %v\n%s", changed, err == nil, wantOK, err, output)
			}
			if strings.Contains(output, "Port ") || strings.Contains(output, "test host config") || strings.Contains(output, "test binary") {
				t.Fatalf("fingerprint operation leaked contents: %s", output)
			}
		})
	}
}

func TestFreshUFWOwnedListenerRetryBounds(t *testing.T) {
	contents := freshUFWScript(t)
	start := strings.Index(contents, "for attempt in {1..50}; do\n")
	if start < 0 {
		t.Fatal("missing exact 50-attempt startup loop")
	}
	end := strings.Index(contents[start:], "\ndone\n")
	if end < 0 {
		t.Fatal("missing exact 50-attempt startup loop")
	}
	loop := contents[start : start+end+len("\ndone\n")]
	if strings.Count(loop, "sleep 0.1\n") != 1 || !strings.Contains(loop, "verify_owned_ssh_listeners 2>/dev/null") ||
		!strings.HasPrefix(contents[start+end+len("\ndone\n"):], "verify_owned_ssh_listeners ||") {
		t.Fatal("retry delay or final diagnostic verification changed")
	}
	output, err := runFreshUFWHelper(t, `
SSHD_PID=999999
SSHD_IDENTITY=1234
checks=0
sleeps=0
kill() { [[ "$*" == '-0 999999' ]]; }
verify_owned_ssh_listeners() { checks=$((checks + 1)); printf 'suppressed polling diagnostic\n' >&2; return 1; }
sleep() { [[ "$1" == 0.1 ]] || return 99; sleeps=$((sleeps + 1)); }
`+loop+`
[[ "$checks" == 50 && "$sleeps" == 50 ]]
`)
	if err != nil || output != "" {
		t.Fatalf("retry bounds/quiet polling failed: %v\n%s", err, output)
	}
}

func TestFreshUFWBaselinePreservation(t *testing.T) {
	for _, changed := range []string{"none", "config", "ports", "listeners", "units", "fingerprint"} {
		t.Run(changed, func(t *testing.T) {
			output, err := runFreshUFWHelper(t, `
INITIAL_SSH_EFFECTIVE='port 22'
INITIAL_SSH_PORTS=22
INITIAL_SSH_LISTENERS='baseline-listeners'
INITIAL_SSH_UNITS='baseline-units'
INITIAL_SSH_FINGERPRINT='baseline-fingerprint'
SSHD_BIN=fixture_sshd
fixture_value() {
    if [[ "$FIXTURE_CHANGED" == "$1" ]]; then printf 'changed\n'; else printf '%s\n' "$2"; fi
}
fixture_sshd() { fixture_value config "$INITIAL_SSH_EFFECTIVE"; }
ss() { printf 'fixture-listeners\n'; }
installer_ssh_ports() { fixture_value ports "$INITIAL_SSH_PORTS"; }
inspect_baseline_ssh() { fixture_value listeners "$INITIAL_SSH_LISTENERS"; }
ssh_unit_state() { fixture_value units "$INITIAL_SSH_UNITS"; }
ssh_config_fingerprint() { fixture_value fingerprint "$INITIAL_SSH_FINGERPRINT"; }
verify_ssh_baseline_unchanged
`, "FIXTURE_CHANGED="+changed)
			if (err == nil) != (changed == "none") {
				t.Fatalf("changed=%s: %v\n%s", changed, err, output)
			}
			if changed != "none" && !strings.Contains(output, "FAIL: SSH inspection:") {
				t.Fatalf("missing safe stage diagnostic: %s", output)
			}
		})
	}
}

func TestFreshUFWCleanupOwnsOnlyAddedRulesAndPID(t *testing.T) {
	for _, identityMatches := range []bool{true, false} {
		name := "owned PID"
		identity := "1234"
		if !identityMatches {
			name, identity = "reused PID", "different"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			work := filepath.Join(dir, "work")
			if err := os.Mkdir(work, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(work, "ufw-rules-before-test"), []byte("ufw allow 22/tcp\n"), 0600); err != nil {
				t.Fatal(err)
			}
			output, err := runFreshUFWHelper(t, `
SSHD_BIN=/fixture/sshd
SSHD_PID=999999
SSHD_IDENTITY=1234
process_identity() { printf '%s\n' "$FIXTURE_IDENTITY"; }
kill() { printf 'kill %s\n' "$*" >>"$FIXTURE_LOG"; return 0; }
wait() { return 0; }
ufw() {
    printf 'ufw %s\n' "$*" >>"$FIXTURE_LOG"
    if [[ "$1" == status ]]; then
        if [[ -f "$FIXTURE_STATE/inactive" ]]; then printf 'Status: inactive\n'; else printf 'Status: active\n'; fi
    fi
    if [[ "${1:-} ${2:-}" == '--force disable' ]]; then touch "$FIXTURE_STATE/inactive"; fi
    if [[ "${1:-} ${2:-} ${3:-}" == '--force delete allow' ]]; then
        touch "$FIXTURE_STATE/${4%/tcp}"
    fi
}
ufw_user_rules() {
    printf 'ufw allow 22/tcp\n'
    for port in 2222 39095 39096; do
        [[ -f "$FIXTURE_STATE/$port" ]] || printf 'ufw allow %s/tcp\n' "$port"
    done
}
UFW_ACTIVATION_OWNED=1
ALLOW_RULES_OWNED=1
OWNED_ALLOW_PORTS=(2222 39095 39096)
DENY_RULE_OWNED=0
UFW_BASELINE_READY=1
cleanup
`, "WORK_DIR="+work, "FIXTURE_STATE="+dir, "FIXTURE_LOG="+filepath.Join(dir, "commands"), "FIXTURE_IDENTITY="+identity)
			if (err == nil) != identityMatches {
				t.Fatalf("cleanup success=%v, want %v: %v\n%s", err == nil, identityMatches, err, output)
			}
			log, readErr := os.ReadFile(filepath.Join(dir, "commands"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			commands := string(log)
			if strings.Contains(commands, "delete allow 22/tcp") || strings.Contains(commands, "systemctl") {
				t.Fatalf("cleanup touched host SSH: %s", commands)
			}
			if strings.Contains(commands, "kill 999999\n") != identityMatches {
				t.Fatalf("PID signal ownership not respected: %s", commands)
			}
			for _, port := range []string{"2222", "39095", "39096"} {
				if !strings.Contains(commands, "ufw --force delete allow "+port+"/tcp") {
					t.Errorf("owned rule not removed: %s", port)
				}
			}
		})
	}
}

func TestFreshUFWCleanupRetainsSafetyRulesIfDisableFails(t *testing.T) {
	dir := t.TempDir()
	output, err := runFreshUFWHelper(t, `
SSHD_PID=''
UFW_ACTIVATION_OWNED=1
ALLOW_RULES_OWNED=1
OWNED_ALLOW_PORTS=(22 2222)
DENY_RULE_OWNED=1
ufw() {
    printf '%s\n' "$*" >>"$FIXTURE_LOG"
    if [[ "$1" == status ]]; then printf 'Status: active\n'; return 0; fi
    if [[ "${1:-} ${2:-}" == '--force disable' ]]; then return 1; fi
    return 0
}
cleanup
`, "FIXTURE_LOG="+filepath.Join(dir, "commands"))
	if err == nil || !strings.Contains(output, "retaining all safety rules") {
		t.Fatalf("failed disable was not surfaced: %v\n%s", err, output)
	}
	log, readErr := os.ReadFile(filepath.Join(dir, "commands"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(log), "delete") {
		t.Fatalf("safety rules removed with UFW still active: %s", log)
	}
}

func TestFreshUFWDenyFixtureUsesCanonicalUnforcedAddRule(t *testing.T) {
	contents := freshUFWScript(t)
	fixtureCommand := `ufw deny from "$DENY_SOURCE" to any port "$DENY_PORT" proto tcp`
	if !strings.Contains(contents, "\n"+fixtureCommand+"\n") {
		t.Fatalf("missing unforced deny fixture command: %s", fixtureCommand)
	}

	canonicalAddedRule := `ufw deny from $DENY_SOURCE to any port $DENY_PORT proto tcp`
	if !strings.Contains(contents, `grep -Fxq "`+canonicalAddedRule+`" <(ufw_user_rules)`) {
		t.Fatalf("deny fixture must match UFW's canonical show added syntax: %s", canonicalAddedRule)
	}

	for lineNumber, line := range strings.Split(contents, "\n") {
		fields := strings.Fields(line)
		for i, field := range fields {
			if field != "ufw" || i+2 >= len(fields) ||
				(fields[i+1] != "--force" && fields[i+1] != "-f") {
				continue
			}
			switch fields[i+2] {
			case "allow", "deny", "reject", "limit", "insert", "prepend", "route":
				t.Errorf("line %d uses --force for a rule-add command: %s", lineNumber+1, line)
			}
		}
	}
}

func TestFreshUFWHarnessPreservesNativeSafetyBoundaries(t *testing.T) {
	contents := freshUFWScript(t)
	for _, required := range []string{
		`NOVAS_FRESH_UFW_OPT_IN`, `github-hosted`, `workflow_dispatch`,
		`INITIAL_SSH_PORTS=$(installer_ssh_ports)`,
		`INITIAL_SSH_LISTENERS=$(inspect_baseline_ssh`,
		`verify_ssh_baseline_unchanged || die "package installation changed host SSH`,
		`apt-get install -y --no-upgrade --no-install-recommends ufw openssh-client iproute2`,
		`ssh_config_ports <<<"$INITIAL_SSH_EFFECTIVE"; printf '%s\n' "$SSH_PORT"`,
		`cmp -s "$WORK_DIR/ssh-global-drop-in" "$SSH_DROP_IN"`,
		`OWNED_ALLOW_PORTS+=("$port")`,
		`for port in "${EXPECTED_ALLOW_PORTS[@]}"; do`,
		`install_fresh_ufw "$WORK_DIR/panel-home" "" "$PANEL_PORT" "$SUB_PORT"`,
		`timeout 10 ssh-keyscan -T 5 -p "$SSH_PORT"`,
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("missing safety/acceptance requirement: %s", required)
		}
	}
	for _, forbidden := range []string{
		"systemctl stop ssh", "systemctl disable ssh", "systemctl restart ssh", "systemctl reload ssh",
		"ufw --force reset", "ufw reset", "apt-get install -y --no-install-recommends ufw openssh-server",
	} {
		if strings.Contains(contents, forbidden) {
			t.Errorf("host mutation or bypass introduced: %s", forbidden)
		}
	}
	preflight := strings.Index(contents, "INITIAL_SSH_LISTENERS=$(inspect_baseline_ssh")
	mutation := strings.Index(contents, "WORK_DIR=$(mktemp")
	if preflight < 0 || mutation <= preflight {
		t.Fatal("read-only SSH ownership preflight must precede package/firewall mutations")
	}
}
