package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"axiom/internal/domain"
)

func TestDefaultConfigurationIsValidAndCredentialFree(t *testing.T) {
	configuration := DefaultConfiguration()
	if err := Validate(configuration); err != nil {
		t.Fatal(err)
	}
	if len(configuration.Secrets) != 0 || !capabilitiesExactlyUnsupported(configuration.Capabilities) {
		t.Fatal("default configuration enables credentials or prohibited capability")
	}
	if !configuration.Safety.FailClosed || configuration.Safety.RiskInitialState != "PAUSED" {
		t.Fatal("default configuration is not fail closed and paused")
	}
}

func TestConfigurationNegativeMatrix(t *testing.T) {
	tests := []struct {
		name  string
		alter func(*Configuration)
		code  string
	}{
		{name: "schema", alter: func(c *Configuration) { c.SchemaVersion = "future" }, code: "invalid_configuration"},
		{name: "revision", alter: func(c *Configuration) { c.Revision = 0 }, code: "invalid_configuration"},
		{name: "environment", alter: func(c *Configuration) { c.Environment = "production" }, code: "prohibited_environment"},
		{name: "mode", alter: func(c *Configuration) { c.Mode = "live" }, code: "prohibited_mode"},
		{name: "product", alter: func(c *Configuration) { c.Product = "futures" }, code: "prohibited_product"},
		{name: "fail open", alter: func(c *Configuration) { c.Safety.FailClosed = false }, code: "unsafe_configuration"},
		{name: "auto unpause", alter: func(c *Configuration) { c.Safety.AutoUnpause = true }, code: "unsafe_configuration"},
		{name: "missing capability denial", alter: func(c *Configuration) { c.Capabilities = c.Capabilities[:1] }, code: "prohibited_capability"},
		{name: "unknown capability state", alter: func(c *Configuration) { c.Capabilities[0] = "enabled" }, code: "prohibited_capability"},
		{name: "private host", alter: func(c *Configuration) { c.Endpoint.REST = "https://api.binance.com" }, code: "endpoint_rejected"},
		{name: "product pair", alter: func(c *Configuration) { c.Instruments[0].Product = "margin" }, code: "prohibited_instrument"},
		{name: "whole percent", alter: func(c *Configuration) { c.Risk.MaximumAssetAllocation.Value = "25" }, code: "financial_value_out_of_range"},
		{name: "unit", alter: func(c *Configuration) { c.Risk.MaximumDailyLoss.Unit = "percent" }, code: "invalid_unit"},
		{name: "model", alter: func(c *Configuration) { c.Models.Fee = "remote-latest" }, code: "model_rejected"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configuration := DefaultConfiguration()
			test.alter(&configuration)
			if code := configurationErrorCode(Validate(configuration)); code != test.code {
				t.Fatalf("error code = %q, want %q", code, test.code)
			}
		})
	}
}

func TestUnknownAndUnapprovedAssetsFailClosed(t *testing.T) {
	configuration := DefaultConfiguration()
	configuration.Assets = append(configuration.Assets, domain.Asset{Symbol: "DOGE", Status: domain.AssetPendingReview})
	configuration.Instruments = append(configuration.Instruments, Instrument{Base: "DOGE", Quote: "USDT", Product: "spot"})
	if code := configurationErrorCode(Validate(configuration)); code != "unapproved_asset" {
		t.Fatalf("error code = %q", code)
	}
}

func TestRequiredSecretReferenceValidation(t *testing.T) {
	configuration := DefaultConfiguration()
	configuration.Secrets = []SecretReference{{Name: "database_runtime", File: filepath.Join(t.TempDir(), "missing"), Required: true}}
	if code := configurationErrorCode(Validate(configuration)); code != "required_secret_missing" {
		t.Fatalf("missing error code = %q", code)
	}
	file, err := os.CreateTemp(t.TempDir(), "runtime-value-")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file.Name(), []byte("fixture-reference"), 0o600); err != nil {
		t.Fatal(err)
	}
	configuration.Secrets[0].File = file.Name()
	if err := Validate(configuration); err != nil {
		t.Fatalf("regular secret reference rejected: %v", err)
	}
}

func TestDecodeJSONRejectsUnknownFieldsAndTrailingDocuments(t *testing.T) {
	encoded, err := json.Marshal(DefaultConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	withUnknown := append(append([]byte(nil), encoded[:len(encoded)-1]...), []byte(`,"unknown":true}`)...)
	if _, err := DecodeJSON(withUnknown); err == nil {
		t.Fatal("unknown field accepted")
	}
	if _, err := DecodeJSON(append(encoded, encoded...)); err == nil {
		t.Fatal("trailing JSON document accepted")
	}
	decoded, err := DecodeJSON(encoded)
	if err != nil || decoded.Revision != 1 {
		t.Fatalf("valid decode = %#v, %v", decoded, err)
	}
}

func configurationErrorCode(err error) string {
	var failure *Error
	if errors.As(err, &failure) {
		return failure.Code
	}
	return ""
}

func FuzzDecodeConfiguration(f *testing.F) {
	seed, err := json.Marshal(DefaultConfiguration())
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed)
	f.Add([]byte(`{"mode":"shadow"}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		configuration, err := DecodeJSON(data)
		if err != nil {
			return
		}
		encoded, err := json.Marshal(configuration)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeJSON(encoded); err != nil {
			t.Fatalf("accepted configuration did not round trip: %v", err)
		}
	})
}
