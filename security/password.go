package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	passwordHashPrefix             = "$novapanel$argon2id$"
	defaultCredentialPrefix        = "$novapanel$default$"
	passwordHashMemoryKiB   uint32 = 19 * 1024
	passwordHashTime        uint32 = 2
	passwordHashThreads     uint8  = 1
	passwordHashLength             = 32
	passwordSaltLength             = 16
	maxPasswordLength              = 4096
)

var errMalformedPasswordHash = errors.New("malformed administrator password hash")

func GenerateRandomPassword() (string, error) {
	password := make([]byte, 24)
	if _, err := rand.Read(password); err != nil {
		return "", fmt.Errorf("generate random password: %w", err)
	}
	return hex.EncodeToString(password), nil
}

// HashUserPassword hashes a panel password. The marker lets health checks
// identify the known initial admin/admin credential without re-running Argon2.
func HashUserPassword(username, password string) (string, error) {
	if password == "" || len(password) > maxPasswordLength {
		return "", errors.New("password must contain 1 to 4096 bytes")
	}
	salt := make([]byte, passwordSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	derived := argon2.IDKey([]byte(password), salt, passwordHashTime, passwordHashMemoryKiB, passwordHashThreads, passwordHashLength)
	encoded := fmt.Sprintf("%sv=%d$m=%d,t=%d,p=%d$%s$%s",
		passwordHashPrefix,
		argon2.Version,
		passwordHashMemoryKiB,
		passwordHashTime,
		passwordHashThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(derived),
	)
	if username == "admin" && password == "admin" {
		return defaultCredentialPrefix + encoded, nil
	}
	return encoded, nil
}

// VerifyPassword accepts legacy plaintext values once so successful logins can
// upgrade existing databases without invalidating credentials.
func VerifyPassword(stored, password string) (valid bool, needsUpgrade bool, err error) {
	if password == "" || len(password) > maxPasswordLength {
		return false, false, nil
	}
	stored = strings.TrimPrefix(stored, defaultCredentialPrefix)
	if strings.HasPrefix(stored, passwordHashPrefix) {
		version, memory, iterations, threads, salt, expected, err := parsePasswordHash(stored)
		if err != nil {
			return false, false, err
		}
		actual := argon2.IDKey([]byte(password), salt, iterations, memory, threads, uint32(len(expected)))
		valid = subtle.ConstantTimeCompare(actual, expected) == 1
		needsUpgrade = valid && (version != argon2.Version || memory != passwordHashMemoryKiB || iterations != passwordHashTime || threads != passwordHashThreads)
		return valid, needsUpgrade, nil
	}
	if strings.HasPrefix(stored, "$novapanel$") {
		return false, false, errMalformedPasswordHash
	}
	valid = subtle.ConstantTimeCompare([]byte(stored), []byte(password)) == 1
	return valid, valid, nil
}

func IsDefaultUserPassword(username, stored string) bool {
	return username == "admin" && (stored == "admin" || strings.HasPrefix(stored, defaultCredentialPrefix))
}

func parsePasswordHash(encoded string) (version, memory, iterations uint32, threads uint8, salt, expected []byte, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 7 || parts[1] != "novapanel" || parts[2] != "argon2id" {
		err = errMalformedPasswordHash
		return
	}
	versionValue, ok := strings.CutPrefix(parts[3], "v=")
	if !ok {
		err = errMalformedPasswordHash
		return
	}
	parsedVersion, parseErr := strconv.ParseUint(versionValue, 10, 32)
	if parseErr != nil || parsedVersion != uint64(argon2.Version) {
		err = errMalformedPasswordHash
		return
	}
	version = uint32(parsedVersion)

	params := strings.Split(parts[4], ",")
	if len(params) != 3 {
		err = errMalformedPasswordHash
		return
	}
	values := make(map[string]uint64, 3)
	for _, param := range params {
		key, value, found := strings.Cut(param, "=")
		if !found || (key != "m" && key != "t" && key != "p") {
			err = errMalformedPasswordHash
			return
		}
		if _, duplicate := values[key]; duplicate {
			err = errMalformedPasswordHash
			return
		}
		parsed, parseErr := strconv.ParseUint(value, 10, 32)
		if parseErr != nil {
			err = errMalformedPasswordHash
			return
		}
		values[key] = parsed
	}
	if len(values) != 3 || values["m"] < 8*1024 || values["m"] > 128*1024 || values["t"] < 1 || values["t"] > 6 || values["p"] < 1 || values["p"] > 4 {
		err = errMalformedPasswordHash
		return
	}
	memory, iterations, threads = uint32(values["m"]), uint32(values["t"]), uint8(values["p"])
	salt, err = base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(salt) < passwordSaltLength || len(salt) > 64 {
		err = errMalformedPasswordHash
		return
	}
	expected, err = base64.RawStdEncoding.DecodeString(parts[6])
	if err != nil || len(expected) != passwordHashLength {
		err = errMalformedPasswordHash
		return
	}
	return
}
