package service

import "testing"

func TestParsePortFromListenAddress(t *testing.T) {
	cases := map[string]int{
		"0.0.0.0:80":     80,
		"[::]:443":       443,
		"127.0.0.1:2222": 2222,
		"[fe80::1]:2053": 2053,
	}

	for input, want := range cases {
		got, err := parsePortFromListenAddress(input)
		if err != nil {
			t.Fatalf("parsePortFromListenAddress(%q) unexpected error: %v", input, err)
		}
		if got != want {
			t.Fatalf("parsePortFromListenAddress(%q) = %d, want %d", input, got, want)
		}
	}
}

func TestParseSSListenOutput(t *testing.T) {
	raw := `
LISTEN 0 128 0.0.0.0:22 0.0.0.0:* users:(("sshd",pid=1,fd=3))
LISTEN 0 128 [::]:443 [::]:* users:(("nginx",pid=2,fd=4))
LISTEN 0 128 127.0.0.1:8080 0.0.0.0:* users:(("nginx",pid=3,fd=5))
`
	entries := parseSSListenOutput("tcp", raw)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Port != 22 || entries[0].Process != "sshd" || entries[0].PID != 1 {
		t.Fatalf("unexpected first entry: %#v", entries[0])
	}
	if entries[1].Port != 443 || entries[1].Process != "nginx" || entries[1].PID != 2 {
		t.Fatalf("unexpected second entry: %#v", entries[1])
	}
}

func TestParseIptablesNatLine(t *testing.T) {
	line := "-A PREROUTING -p udp -m udp --dport 500 -j REDIRECT --to-ports 500"
	entry := parseIptablesNatLine(line, "ipv4")
	if entry == nil {
		t.Fatal("expected nat entry")
	}
	if entry.Chain != "PREROUTING" || entry.Protocol != "udp" || entry.DPort != "500" || entry.Target != "REDIRECT" || entry.ToPorts != "500" {
		t.Fatalf("unexpected nat entry: %#v", entry)
	}
}
