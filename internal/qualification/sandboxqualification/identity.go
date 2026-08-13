package sandboxQualification

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// VerifyCurrentExecutableHash binds a qualification process to the exact
// binary hash declared in its immutable run identity.
func VerifyCurrentExecutableHash(expected string) error {
	if !sha256Pattern.MatchString(expected) {
		return fmt.Errorf("sandbox_qualification_executable_hash_rejected")
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("sandbox_qualification_executable_identity_failed")
	}
	actual, err := fileSHA256(executable)
	if err != nil {
		return fmt.Errorf("sandbox_qualification_executable_identity_failed")
	}
	if actual != expected {
		return fmt.Errorf("sandbox_qualification_executable_hash_mismatch")
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
