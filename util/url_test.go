package util

import "testing"

func TestAppendURLPathSegment(t *testing.T) {
	tests := []struct {
		name string
		base string
		want string
	}{
		{name: "trailing slash", base: "https://panel.example/sub/", want: "https://panel.example/sub/aggregate"},
		{name: "no trailing slash", base: "https://panel.example/sub", want: "https://panel.example/sub/aggregate"},
		{name: "query preserved", base: "https://panel.example/sub?key=value", want: "https://panel.example/sub/aggregate?key=value"},
		{name: "fragment preserved", base: "https://panel.example/sub#profile", want: "https://panel.example/sub/aggregate#profile"},
		{name: "absolute root", base: "/", want: "/aggregate"},
		{name: "blank base", base: "  ", want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := AppendURLPathSegment(test.base, "aggregate"); got != test.want {
				t.Fatalf("AppendURLPathSegment(%q, aggregate) = %q, want %q", test.base, got, test.want)
			}
		})
	}
}
