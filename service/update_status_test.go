package service

import (
	"strings"
	"testing"
)

func TestTailLinesKeepsLatestEntries(t *testing.T) {
	lines, err := tailLines(strings.NewReader("one\ntwo\nthree\nfour\n"), 2)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(lines, ","), "three,four"; got != want {
		t.Fatalf("tailLines() = %q, want %q", got, want)
	}
}

func TestTailLinesEmptyLimit(t *testing.T) {
	lines, err := tailLines(strings.NewReader("one\n"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 {
		t.Fatalf("tailLines() returned %d lines, want 0", len(lines))
	}
}
