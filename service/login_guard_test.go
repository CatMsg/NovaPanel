package service

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeNetworkList(t *testing.T) {
	got, err := normalizeNetworkList("192.0.2.10, 10.2.3.4/8\n2001:db8::1 192.0.2.10")
	if err != nil {
		t.Fatal(err)
	}
	if got != "192.0.2.10 10.0.0.0/8 2001:db8::1" {
		t.Fatalf("unexpected normalized list: %q", got)
	}
	if _, err := normalizeNetworkList("192.0.2.1;action=allow"); err == nil {
		t.Fatal("expected invalid network list to fail")
	}
}

func TestParseFail2banBannedIPs(t *testing.T) {
	output := "Status for the jail: novapanel\n`- Actions\n   |- Currently banned:\t2\n   `- Banned IP list:\t192.0.2.8 2001:db8::9\n"
	want := []string{"192.0.2.8", "2001:db8::9"}
	if got := parseFail2banBannedIPs(output); !reflect.DeepEqual(got, want) {
		t.Fatalf("parse = %#v, want %#v", got, want)
	}
}

func TestLoginGuardScriptWritesIsolatedConfiguration(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	client := filepath.Join(bin, "fail2ban-client")
	systemctl := filepath.Join(bin, "systemctl")
	nft := filepath.Join(bin, "nft")
	for _, path := range []string{client, systemctl, nft} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "etc/fail2ban/action.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc/fail2ban/action.d/nftables-multiport.conf"), []byte("[Definition]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(root, "etc/fail2ban/jail.d/other.local")
	if err := os.MkdirAll(filepath.Dir(sentinel), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sentinel, []byte("[other]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOVAS_LOGIN_GUARD_ROOT", root)
	t.Setenv("NOVAS_FAIL2BAN_CLIENT", client)
	t.Setenv("NOVAS_SYSTEMCTL", systemctl)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if _, err := runCommandOutput(5e9, "bash", filepath.Join("..", loginGuardScript), "sync", "9999", "192.0.2.1 2001:db8::/64"); err != nil {
		t.Fatal(err)
	}
	filter, err := os.ReadFile(filepath.Join(root, "etc/fail2ban/filter.d/novapanel.conf"))
	if err != nil {
		t.Fatal(err)
	}
	jail, err := os.ReadFile(filepath.Join(root, "etc/fail2ban/jail.d/novapanel.local"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(filter), "NOVAS_LOGIN_FAILED remote_ip=<HOST>") {
		t.Fatalf("unexpected filter: %s", filter)
	}
	for _, expected := range []string{"port = 9999", "bantime = -1", "maxretry = 10", "usedns = no", "ignoreip = 127.0.0.1/8 ::1 192.0.2.1 2001:db8::/64", "banaction = nftables-multiport"} {
		if !strings.Contains(string(jail), expected) {
			t.Fatalf("jail missing %q: %s", expected, jail)
		}
	}
	if _, err := runCommandOutput(5e9, "bash", filepath.Join("..", loginGuardScript), "remove"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(root, "etc/fail2ban/filter.d/novapanel.conf"),
		filepath.Join(root, "etc/fail2ban/jail.d/novapanel.local"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, got %v", path, err)
		}
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("unrelated jail was changed: %v", err)
	}
}
