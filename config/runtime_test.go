package config

import "testing"

func TestRuntimeEnv(t *testing.T) {
	t.Setenv("NOVAS_TEST", "current")
	if got := RuntimeEnv("TEST"); got != "current" {
		t.Fatal(got)
	}
	t.Setenv("NOVAS_TEST", "")
	if got := RuntimeEnv("TEST"); got != "" {
		t.Fatal(got)
	}
}
