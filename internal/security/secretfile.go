package security

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const maximumSecretBytes = 64 * 1024

// ReadSecretFile reads one absolute, regular, narrowly permissioned file.
// Errors contain stable reason codes and never include the secret or raw path.
func ReadSecretFile(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", secretError("path_not_absolute")
	}
	before, err := os.Lstat(path)
	if err != nil {
		return "", secretError("unavailable")
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return "", secretError("not_regular")
	}
	if err := validatePermissions(before); err != nil {
		return "", err
	}

	file, err := os.Open(path)
	if err != nil {
		return "", secretError("unreadable")
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return "", secretError("changed_during_open")
	}
	data, err := io.ReadAll(io.LimitReader(file, maximumSecretBytes+1))
	if err != nil {
		return "", secretError("unreadable")
	}
	if len(data) > maximumSecretBytes {
		return "", secretError("too_large")
	}
	value := strings.TrimRight(string(data), "\r\n")
	if value == "" || strings.ContainsRune(value, '\x00') {
		return "", secretError("invalid_content")
	}
	if strings.Contains(strings.ToUpper(value), "CHANGE_ME") {
		return "", secretError("placeholder")
	}
	return value, nil
}

func validatePermissions(info os.FileInfo) error {
	permissions := info.Mode().Perm()
	if permissions&0o400 == 0 || permissions&0o137 != 0 {
		return secretError("unsafe_permissions")
	}
	if permissions&0o040 != 0 && !processInFileGroup(info) {
		return secretError("unsafe_group")
	}
	return nil
}

func processInFileGroup(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	wanted := int(stat.Gid)
	if os.Getegid() == wanted {
		return true
	}
	groups, err := os.Getgroups()
	if err != nil {
		return false
	}
	for _, group := range groups {
		if group == wanted {
			return true
		}
	}
	return false
}

func secretError(code string) error {
	return fmt.Errorf("secret_file_%s", code)
}
