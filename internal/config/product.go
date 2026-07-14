package config

import (
	"io"
	"os"
	"path/filepath"
)

const maximumConfigurationBytes = 1024 * 1024

// LoadProductConfiguration loads and validates the effective versioned graph.
func LoadProductConfiguration(requestedMode ExecutionMode) (Configuration, Source, error) {
	path, configured := os.LookupEnv("APP_CONFIG_FILE")
	if !configured || path == "" {
		configuration := DefaultConfiguration()
		if requestedMode != "" {
			configuration.Mode = requestedMode
			if requestedMode == ModeShadow {
				configuration.Environment = EnvironmentShadow
			}
		}
		if err := validateRequestedMode(configuration, requestedMode); err != nil {
			return Configuration{}, "", err
		}
		return configuration, SourceDefault, nil
	}
	data, err := readConfigurationFile(path)
	if err != nil {
		return Configuration{}, "", err
	}
	configuration, err := DecodeJSON(data)
	if err != nil {
		return Configuration{}, "", err
	}
	if err := validateRequestedMode(configuration, requestedMode); err != nil {
		return Configuration{}, "", err
	}
	return configuration, SourceFile, nil
}

func readConfigurationFile(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, configError("invalid_configuration", "APP_CONFIG_FILE")
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 {
		return nil, configError("invalid_configuration", "APP_CONFIG_FILE")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, configError("invalid_configuration", "APP_CONFIG_FILE")
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return nil, configError("invalid_configuration", "APP_CONFIG_FILE")
	}
	data, err := io.ReadAll(io.LimitReader(file, maximumConfigurationBytes+1))
	if err != nil || len(data) > maximumConfigurationBytes {
		return nil, configError("invalid_configuration", "APP_CONFIG_FILE")
	}
	return data, nil
}

func validateRequestedMode(configuration Configuration, requestedMode ExecutionMode) error {
	if requestedMode != "" && requestedMode != configuration.Mode {
		return configError("prohibited_mode", "mode_mismatch")
	}
	if environmentMode, configured := os.LookupEnv("EXECUTION_MODE"); configured {
		mode, err := ParseExecutionMode(environmentMode)
		if err != nil || mode != configuration.Mode {
			return configError("prohibited_mode", "EXECUTION_MODE")
		}
	}
	return Validate(configuration)
}
