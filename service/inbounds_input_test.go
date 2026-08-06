package service

import "testing"

func TestParseClientIDs(t *testing.T) {
	ids, err := parseClientIDs("1, 2,7")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 3 || ids[0] != 1 || ids[2] != 7 {
		t.Fatalf("unexpected ids: %#v", ids)
	}
}

func TestParseClientIDsRejectsSQLAndZero(t *testing.T) {
	for _, value := range []string{"1); DELETE FROM clients;--", "0", "1,,2", "-1"} {
		if _, err := parseClientIDs(value); err == nil {
			t.Fatalf("expected %q to be rejected", value)
		}
	}
}
