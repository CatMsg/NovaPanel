package main

import (
	"os"
	"strings"
	"testing"
)

func TestFreshUFWAcceptanceUsesValidatedAbsoluteSSHD(t *testing.T) {
	script, err := os.ReadFile("test-fresh-ufw-linux.sh")
	if err != nil {
		t.Fatal(err)
	}
	contents := string(script)

	resolve := strings.Index(contents, `SSHD_BIN=$(command -v sshd) || die "OpenSSH daemon executable is unavailable"`)
	validate := strings.Index(contents, `[[ "$SSHD_BIN" == /* && -x "$SSHD_BIN" ]] || die "resolved sshd path must be absolute and executable"`)
	if resolve < 0 || validate <= resolve {
		t.Fatal("sshd must be resolved with command -v and validated as an absolute executable before use")
	}

	for _, invocation := range []string{
		`sshd_effective=$(LC_ALL=C "$SSHD_BIN" -T)`,
		`"$SSHD_BIN" -t`,
		`"$SSHD_BIN" -D -E "$WORK_DIR/sshd.log" &`,
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
