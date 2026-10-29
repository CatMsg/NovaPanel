package security

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestGenerateRandomPassword(t *testing.T) {
	first, err := GenerateRandomPassword()
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateRandomPassword()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 48 || first == second {
		t.Fatalf("generated passwords are not unique 192-bit hex values: %q %q", first, second)
	}
	if _, err := hex.DecodeString(first); err != nil {
		t.Fatalf("generated password is not hexadecimal: %v", err)
	}
}

func TestHashUserPasswordUsesUniqueArgon2idHashes(t *testing.T) {
	first, err := HashUserPassword("alice", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	second, err := HashUserPassword("alice", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !strings.HasPrefix(first, passwordHashPrefix) {
		t.Fatal("password hashes must use Argon2id with independent salts")
	}
	valid, needsUpgrade, err := VerifyPassword(first, "correct horse battery staple")
	if err != nil || !valid || needsUpgrade {
		t.Fatalf("current hash did not verify: valid=%v upgrade=%v err=%v", valid, needsUpgrade, err)
	}
	valid, _, err = VerifyPassword(first, "wrong password")
	if err != nil || valid {
		t.Fatalf("wrong password was accepted: valid=%v err=%v", valid, err)
	}
}

func TestVerifyPasswordSupportsOneTimePlaintextMigration(t *testing.T) {
	valid, needsUpgrade, err := VerifyPassword("legacy-cleartext", "legacy-cleartext")
	if err != nil || !valid || !needsUpgrade {
		t.Fatalf("legacy password should verify and request migration: valid=%v upgrade=%v err=%v", valid, needsUpgrade, err)
	}
	valid, needsUpgrade, err = VerifyPassword("legacy-cleartext", "wrong")
	if err != nil || valid || needsUpgrade {
		t.Fatalf("wrong legacy password result: valid=%v upgrade=%v err=%v", valid, needsUpgrade, err)
	}
}

func TestVerifyPasswordRejectsEmptyPasswords(t *testing.T) {
	valid, needsUpgrade, err := VerifyPassword("", "")
	if err != nil || valid || needsUpgrade {
		t.Fatalf("empty legacy credentials must be rejected: valid=%v upgrade=%v err=%v", valid, needsUpgrade, err)
	}
}

func TestDefaultAdminMarkerIsVerifiableAndDetectable(t *testing.T) {
	stored, err := HashUserPassword("admin", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !IsDefaultUserPassword("admin", stored) || IsDefaultUserPassword("other", stored) {
		t.Fatal("default-password marker was not scoped to the admin/admin credential")
	}
	valid, needsUpgrade, err := VerifyPassword(stored, "admin")
	if err != nil || !valid || needsUpgrade {
		t.Fatalf("default admin hash did not verify: valid=%v upgrade=%v err=%v", valid, needsUpgrade, err)
	}
	if IsDefaultUserPassword("admin", "custom") {
		t.Fatal("legacy default detector accepted a custom password")
	}
}

func TestVerifyPasswordRejectsMalformedHashParameters(t *testing.T) {
	malformed := passwordHashPrefix + "v=19$m=999999,t=100,p=100$bad$bad"
	if _, _, err := VerifyPassword(malformed, "password"); err == nil {
		t.Fatal("malformed or unbounded hash parameters were accepted")
	}
}
