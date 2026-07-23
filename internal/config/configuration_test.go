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
		{name: "missing trend", alter: func(c *Configuration) { c.Trend = TrendConfiguration{} }, code: "invalid_trend_configuration"},
		{name: "trend timeframe", alter: func(c *Configuration) { c.Trend.Timeframe = "1h" }, code: "invalid_trend_configuration"},
		{name: "trend parameter metadata", alter: func(c *Configuration) { c.Trend.Parameters[0].Rounding = "ceiling" }, code: "invalid_trend_parameter"},
		{name: "trend parameter range", alter: func(c *Configuration) { c.Trend.Parameters[0].Value = "0" }, code: "trend_parameter_out_of_range"},
		{name: "trend duplicate", alter: func(c *Configuration) { c.Trend.Parameters[1].ID = c.Trend.Parameters[0].ID }, code: "invalid_trend_parameter"},
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

func TestTrendConfigurationRecordsCompleteImmutableParameterContracts(t *testing.T) {
	configuration := DefaultConfiguration()
	if configuration.SchemaVersion != "axiom.config.v1a.2" || configuration.Trend.StrategyVersion != "trend.v1a.1" ||
		configuration.Trend.Timeframe != "4h" || len(configuration.Trend.Parameters) != 16 {
		t.Fatalf("trend graph = %#v", configuration.Trend)
	}
	for _, parameter := range configuration.Trend.Parameters {
		if parameter.ID == "" || parameter.Description == "" || parameter.Value == "" || parameter.Unit == "" ||
			parameter.Minimum == "" || parameter.Maximum == "" || parameter.Cadence == "" || parameter.WarmUp == "" ||
			parameter.Mutability != "immutable_per_run" || len(parameter.ModelDependencies) == 0 {
			t.Fatalf("incomplete trend parameter = %#v", parameter)
		}
	}
	cloned := cloneConfiguration(configuration)
	cloned.Trend.Parameters[0].ModelDependencies[0] = "mutated"
	if configuration.Trend.Parameters[0].ModelDependencies[0] == "mutated" {
		t.Fatal("configuration clone shared trend dependencies")
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

func TestV1BConfigurationIsOrderedPublicOnlyAndLegacyStillProjects(t *testing.T) {
	configuration := DefaultV1BConfiguration()
	if err := Validate(configuration); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(configuration)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeJSON(encoded)
	if err != nil || len(decoded.PublicExchanges()) != 2 || decoded.Exchanges[1].ID != "bybit" ||
		len(decoded.Exchanges[1].Instruments) != 3 || len(decoded.Exchanges[1].CandleIntervals) != 3 ||
		len(decoded.Secrets) != 0 {
		t.Fatalf("V1B graph = %#v, %v", decoded.Exchanges, err)
	}
	legacy := DefaultConfiguration().PublicExchanges()
	if len(legacy) != 1 || legacy[0].ID != "binance" || len(legacy[0].Instruments) != 2 {
		t.Fatalf("legacy projection = %#v", legacy)
	}
	decoded.Exchanges[0].Instruments[0].Base = "MUTATED"
	if configuration.Exchanges[0].Instruments[0].Base == "MUTATED" {
		t.Fatal("V1B clone shares exchange instruments")
	}
}

func TestB3ConfigurationRequiresCompleteMeanReversionGraphWithoutReinterpretingOlderSchemas(t *testing.T) {
	configuration := DefaultV1BConfiguration()
	if configuration.SchemaVersion != SchemaVersionV1BB4 ||
		configuration.MeanReversion.StrategyVersion != "mean-reversion.v1b.1" ||
		configuration.MeanReversion.PrimaryTimeframe != "1h" || configuration.MeanReversion.HigherTimeframe != "4h" ||
		len(configuration.MeanReversion.Parameters) != MeanReversionParameterCount {
		t.Fatalf("B3 graph = %#v", configuration.MeanReversion)
	}
	for _, parameter := range configuration.MeanReversion.Parameters {
		if parameter.ID == "" || parameter.Description == "" || parameter.AlgorithmVersion == "" ||
			parameter.EvaluationTimezone != "UTC" || parameter.ChangeBehavior == "" || parameter.ApprovalActor == "" ||
			parameter.ApprovalReference == "" || parameter.ApprovedAt == "" || parameter.ChangeReason == "" {
			t.Fatalf("incomplete B3 parameter = %#v", parameter)
		}
	}

	legacyV1B := configuration
	legacyV1B.SchemaVersion = SchemaVersionV1B
	legacyV1B.MeanReversion = MeanReversionConfiguration{}
	legacyV1B.Triangular = TriangularConfiguration{}
	if err := Validate(legacyV1B); err != nil {
		t.Fatalf("original V1B.1 graph reinterpreted: %v", err)
	}
	legacyV1A := DefaultConfiguration()
	if err := Validate(legacyV1A); err != nil {
		t.Fatalf("original V1A graph reinterpreted: %v", err)
	}
	legacyV1B.MeanReversion = configuration.MeanReversion
	if code := configurationErrorCode(Validate(legacyV1B)); code != "invalid_configuration" {
		t.Fatalf("B3 graph accepted under old schema: %q", code)
	}
}

func TestB4ConfigurationRequiresCompleteTriangularGraphAndPreservesB3Schema(t *testing.T) {
	configuration := DefaultV1BConfiguration()
	if configuration.SchemaVersion != SchemaVersionV1BB4 ||
		configuration.Triangular.StrategyVersion != "triangular.v1b.1" ||
		configuration.Triangular.SettlementAsset != "USDT" ||
		configuration.Triangular.DispatchMode != "sequential" ||
		len(configuration.Triangular.Cycles) != 2 ||
		len(configuration.Triangular.Parameters) != TriangularParameterCount {
		t.Fatalf("B4 graph = %#v", configuration.Triangular)
	}
	for _, parameter := range configuration.Triangular.Parameters {
		if parameter.ID == "" || parameter.Description == "" || parameter.AlgorithmVersion == "" ||
			parameter.EvaluationTimezone != "UTC" || parameter.ChangeBehavior == "" ||
			parameter.ApprovalActor == "" || parameter.ApprovalReference == "" ||
			parameter.ApprovedAt == "" || parameter.ChangeReason == "" {
			t.Fatalf("incomplete B4 parameter = %#v", parameter)
		}
	}
	legacyB3 := configuration
	legacyB3.SchemaVersion = SchemaVersionV1BB3
	legacyB3.Triangular = TriangularConfiguration{}
	if err := Validate(legacyB3); err != nil {
		t.Fatalf("B3 schema reinterpreted: %v", err)
	}
	legacyB3.Triangular = configuration.Triangular
	if code := configurationErrorCode(Validate(legacyB3)); code != "invalid_configuration" {
		t.Fatalf("B4 graph accepted under B3 schema: %q", code)
	}
}

func TestB4ConfigurationRejectsMetadataRangeCycleAndMissingGraph(t *testing.T) {
	tests := []struct {
		name  string
		alter func(*Configuration)
		code  string
	}{
		{name: "missing", alter: func(c *Configuration) {
			c.Triangular = TriangularConfiguration{}
		}, code: "invalid_triangular_configuration"},
		{name: "metadata", alter: func(c *Configuration) {
			c.Triangular.Parameters[0].AlgorithmVersion = "other"
		}, code: "invalid_triangular_parameter"},
		{name: "range", alter: func(c *Configuration) {
			c.Triangular.Parameters[10].Value = "0.0014"
		}, code: "triangular_parameter_out_of_range"},
		{name: "cycle", alter: func(c *Configuration) {
			c.Triangular.Cycles[0] = "USDT-BTC-USDT"
		}, code: "invalid_triangular_configuration"},
		{name: "duplicate", alter: func(c *Configuration) {
			c.Triangular.Parameters[1].ID = c.Triangular.Parameters[0].ID
		}, code: "invalid_triangular_parameter"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configuration := DefaultV1BConfiguration()
			test.alter(&configuration)
			if code := configurationErrorCode(Validate(configuration)); code != test.code {
				t.Fatalf("error code = %q, want %q", code, test.code)
			}
		})
	}
}

func TestB3ConfigurationRejectsMetadataRangeAndMissingGraph(t *testing.T) {
	tests := []struct {
		name  string
		alter func(*Configuration)
		code  string
	}{
		{name: "missing", alter: func(c *Configuration) { c.MeanReversion = MeanReversionConfiguration{} }, code: "invalid_mean_reversion_configuration"},
		{name: "metadata", alter: func(c *Configuration) { c.MeanReversion.Parameters[0].AlgorithmVersion = "other" }, code: "invalid_mean_reversion_parameter"},
		{name: "negative boundary", alter: func(c *Configuration) { c.MeanReversion.Parameters[8].Value = "0" }, code: "mean_reversion_parameter_out_of_range"},
		{name: "duplicate", alter: func(c *Configuration) { c.MeanReversion.Parameters[1].ID = c.MeanReversion.Parameters[0].ID }, code: "invalid_mean_reversion_parameter"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configuration := DefaultV1BConfiguration()
			test.alter(&configuration)
			if code := configurationErrorCode(Validate(configuration)); code != test.code {
				t.Fatalf("error code = %q, want %q", code, test.code)
			}
		})
	}
}

func TestV1BConfigurationRejectsArbitraryPublicOriginsAndOrder(t *testing.T) {
	configuration := DefaultV1BConfiguration()
	configuration.Exchanges[1].REST = "https://example.invalid"
	if code := configurationErrorCode(Validate(configuration)); code != "endpoint_rejected" {
		t.Fatalf("origin error = %q", code)
	}
	configuration = DefaultV1BConfiguration()
	configuration.Exchanges[0], configuration.Exchanges[1] = configuration.Exchanges[1], configuration.Exchanges[0]
	if code := configurationErrorCode(Validate(configuration)); code != "invalid_configuration" {
		t.Fatalf("order error = %q", code)
	}
}

func TestReviewedV1BRecorderConfigurationDecodes(t *testing.T) {
	payload, err := os.ReadFile(filepath.Join("..", "..", "deploy", "config", "platform-shadow-v1b.json"))
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := DecodeJSON(payload)
	if err != nil || configuration.SchemaVersion != SchemaVersionV1BB4 || len(configuration.Exchanges) != 2 ||
		len(configuration.MeanReversion.Parameters) != MeanReversionParameterCount ||
		len(configuration.Triangular.Parameters) != TriangularParameterCount {
		t.Fatalf("reviewed V1B configuration = %#v, %v", configuration.Exchanges, err)
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
