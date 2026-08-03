package config

import (
	"os"
	"path/filepath"
	"strings"

	"axiom/internal/security"
)

func validateSecrets(references []SecretReference) error {
	seen := make(map[string]struct{}, len(references))
	for _, reference := range references {
		if reference.Name == "" || placeholder(reference.Name) {
			return configError("secret_reference_rejected", "secrets.name")
		}
		if _, duplicate := seen[reference.Name]; duplicate {
			return configError("secret_reference_rejected", "secrets.name")
		}
		seen[reference.Name] = struct{}{}
		if err := validateSecretFile(reference); err != nil {
			return err
		}
	}
	return nil
}

func validateSecretFile(reference SecretReference) error {
	if !filepath.IsAbs(reference.File) || placeholder(filepath.Base(reference.File)) {
		return configError("secret_reference_rejected", "secrets.file")
	}
	information, err := os.Lstat(reference.File)
	if err != nil {
		if reference.Required {
			return configError("required_secret_missing", "secrets.file")
		}
		return nil
	}
	if information.Mode()&os.ModeSymlink != 0 || !information.Mode().IsRegular() {
		return configError("secret_reference_rejected", "secrets.file")
	}
	if _, err := security.ReadSecretFile(reference.File); err != nil {
		return configError("secret_reference_rejected", "secrets.file")
	}
	return nil
}

func placeholder(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "placeholder") || strings.Contains(lower, "changeme") ||
		strings.Contains(lower, "<") || strings.Contains(lower, ">")
}
