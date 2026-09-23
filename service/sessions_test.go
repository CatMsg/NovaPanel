package service

import "testing"

func TestSourceIPFromSessionSource(t *testing.T) {
	tests := map[string]string{
		"203.0.113.9:44321":   "203.0.113.9",
		"[2001:db8::5]:44321": "2001:db8::5",
		"198.51.100.7":        "198.51.100.7",
		"::ffff:192.0.2.9":    "192.0.2.9",
		"example.com:443":     "",
		"":                    "",
		"not-an-address":      "",
	}
	for source, want := range tests {
		if got := sourceIPFromSessionSource(source); got != want {
			t.Fatalf("sourceIPFromSessionSource(%q) = %q, want %q", source, got, want)
		}
	}
}
