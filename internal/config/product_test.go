package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadProductConfigurationFromDeploymentFile(t *testing.T) {
	path, err := filepath.Abs("../../deploy/config/platform-shadow.json")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_CONFIG_FILE", path)
	t.Setenv("EXECUTION_MODE", "shadow")
	configuration, source, err := LoadProductConfiguration(ModeShadow)
	if err != nil {
		t.Fatal(err)
	}
	if source != SourceFile || configuration.Mode != ModeShadow || configuration.Portfolio.StartingCapital.Value != "500" {
		t.Fatalf("configuration = %#v, source = %q", configuration, source)
	}
	expected := DefaultConfiguration()
	expected.Mode = ModeShadow
	expected.Environment = EnvironmentShadow
	if !reflect.DeepEqual(configuration, expected) {
		t.Fatal("deployment configuration drifted from code-owned defaults")
	}
}

func TestLoadProductConfigurationRejectsModeMismatchAndSymlink(t *testing.T) {
	path, err := filepath.Abs("../../deploy/config/platform-shadow.json")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_CONFIG_FILE", path)
	t.Setenv("EXECUTION_MODE", "paper")
	if _, _, err := LoadProductConfiguration(ModeShadow); configurationErrorCode(err) != "prohibited_mode" {
		t.Fatalf("mode mismatch error = %v", err)
	}
	link := filepath.Join(t.TempDir(), "config.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_CONFIG_FILE", link)
	t.Setenv("EXECUTION_MODE", "shadow")
	if _, _, err := LoadProductConfiguration(ModeShadow); configurationErrorCode(err) != "invalid_configuration" {
		t.Fatalf("symlink error = %v", err)
	}
}
