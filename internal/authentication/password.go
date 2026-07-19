package authentication

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Reviewed Argon2id profiles keep current hashing strong, reject weaker
// persisted profiles, and cap attacker-controlled verification cost.
const (
	CurrentMemoryKiB   uint32 = 64 * 1024
	CurrentIterations  uint32 = 3
	CurrentParallelism uint8  = 1
	CurrentSaltBytes          = 16
	CurrentOutputBytes uint32 = 32
	MinimumMemoryKiB   uint32 = 19 * 1024
	MinimumIterations  uint32 = 2
	MinimumParallelism uint8  = 1
	MaximumMemoryKiB   uint32 = 256 * 1024
	MaximumIterations  uint32 = 10
	MaximumParallelism uint8  = 4
)

type passwordProfile struct {
	memory, iterations uint32
	parallelism        uint8
	salt, output       []byte
}

// PasswordHasher implements the reviewed Argon2id PHC contract.
type PasswordHasher struct{}

// Hash creates a fresh current-profile encoded password hash.
func (PasswordHasher) Hash(password string) (string, error) {
	if !validPasswordInput(password) {
		return "", ErrAuthenticationFailed
	}
	salt := make([]byte, CurrentSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", ErrConfiguration
	}
	output := argon2.IDKey([]byte(password), salt, CurrentIterations, CurrentMemoryKiB, CurrentParallelism, CurrentOutputBytes)
	return encodePasswordProfile(passwordProfile{CurrentMemoryKiB, CurrentIterations, CurrentParallelism, salt, output}), nil
}

// Verify checks one encoded hash and reports whether a current-profile rehash is required.
func (PasswordHasher) Verify(password, encoded string) (bool, bool, error) {
	profile, err := parsePasswordProfile(encoded)
	if err != nil || !validPasswordInput(password) {
		return false, false, ErrAuthenticationFailed
	}
	actual := argon2.IDKey([]byte(password), profile.salt, profile.iterations, profile.memory, profile.parallelism, uint32(len(profile.output)))
	valid := subtle.ConstantTimeCompare(actual, profile.output) == 1
	return valid, valid && !isCurrentProfile(profile), nil
}

// ValidateBootstrapHash requires the exact initial A11 profile without reading a plaintext password.
func (PasswordHasher) ValidateBootstrapHash(encoded string) error {
	profile, err := parsePasswordProfile(encoded)
	if err != nil || !isCurrentProfile(profile) {
		return ErrConfiguration
	}
	return nil
}

func parsePasswordProfile(encoded string) (passwordProfile, error) {
	if len(encoded) > 512 {
		return passwordProfile{}, ErrAuthenticationFailed
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return passwordProfile{}, ErrAuthenticationFailed
	}
	profile, err := parseProfileParameters(parts[3])
	if err != nil {
		return passwordProfile{}, err
	}
	profile.salt, err = base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(profile.salt) < CurrentSaltBytes || len(profile.salt) > 64 {
		return passwordProfile{}, ErrAuthenticationFailed
	}
	profile.output, err = base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(profile.output) < int(CurrentOutputBytes) || len(profile.output) > 64 {
		return passwordProfile{}, ErrAuthenticationFailed
	}
	return profile, nil
}

func parseProfileParameters(raw string) (passwordProfile, error) {
	values := map[string]uint64{}
	for _, field := range strings.Split(raw, ",") {
		pair := strings.SplitN(field, "=", 2)
		if len(pair) != 2 {
			return passwordProfile{}, ErrAuthenticationFailed
		}
		value, err := strconv.ParseUint(pair[1], 10, 32)
		if err != nil {
			return passwordProfile{}, ErrAuthenticationFailed
		}
		values[pair[0]] = value
	}
	profile := passwordProfile{memory: uint32(values["m"]), iterations: uint32(values["t"]), parallelism: uint8(values["p"])}
	if len(values) != 3 || profile.memory < MinimumMemoryKiB || profile.iterations < MinimumIterations ||
		profile.parallelism < MinimumParallelism || profile.memory > MaximumMemoryKiB ||
		profile.iterations > MaximumIterations || profile.parallelism > MaximumParallelism {
		return passwordProfile{}, ErrAuthenticationFailed
	}
	return profile, nil
}

func encodePasswordProfile(profile passwordProfile) string {
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", profile.memory, profile.iterations,
		profile.parallelism, base64.RawStdEncoding.EncodeToString(profile.salt), base64.RawStdEncoding.EncodeToString(profile.output))
}

func isCurrentProfile(profile passwordProfile) bool {
	return profile.memory == CurrentMemoryKiB && profile.iterations == CurrentIterations &&
		profile.parallelism == CurrentParallelism && len(profile.salt) == CurrentSaltBytes &&
		len(profile.output) == int(CurrentOutputBytes)
}

func validPasswordInput(password string) bool { return len(password) > 0 && len(password) <= 1024 }
