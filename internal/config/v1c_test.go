package config

import (
	"encoding/json"
	"testing"
)

func TestDefaultV1CConfigurationIsCompleteDefaultOffAndModeScoped(t *testing.T) {
	for _, mode := range []ExecutionMode{ModeTestnet, ModeDemo} {
		configuration, err := DefaultV1CConfiguration(mode)
		if err != nil {
			t.Fatal(err)
		}
		if err = Validate(configuration); err != nil {
			t.Fatalf("%s configuration rejected: %v", mode, err)
		}
		if configuration.SchemaVersion != SchemaVersionV1C ||
			configuration.Environment != EnvironmentSandbox ||
			configuration.Sandbox.IntegrationsEnabled ||
			configuration.Sandbox.SubmissionEnabled ||
			configuration.Sandbox.Exchanges[0].IntegrationEnabled ||
			configuration.Sandbox.Exchanges[1].IntegrationEnabled ||
			configuration.Sandbox.SandboxProfitabilityProof {
			t.Fatalf("unsafe default graph: %#v", configuration.Sandbox)
		}
		encoded, encodeErr := json.Marshal(configuration)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		decoded, decodeErr := DecodeJSON(encoded)
		if decodeErr != nil || decoded.Mode != mode {
			t.Fatalf("round trip = %#v, %v", decoded, decodeErr)
		}
	}
}

func TestV1CSandboxPolicyCannotBeLoosenedOrAppliedToEarlierSchema(t *testing.T) {
	tests := []struct {
		name  string
		alter func(*Configuration)
	}{
		{"arm duration", func(c *Configuration) { c.Sandbox.ArmDurationSeconds++ }},
		{"reauthorization", func(c *Configuration) { c.Sandbox.ReauthorizationSeconds++ }},
		{"order cap", func(c *Configuration) { c.Sandbox.MaximumOrderNotional.Value = "10.01" }},
		{"daily cap", func(c *Configuration) { c.Sandbox.MaximumDailyNotional.Value = "50.01" }},
		{"account orders", func(c *Configuration) { c.Sandbox.MaximumOpenPerAccount = 2 }},
		{"global orders", func(c *Configuration) { c.Sandbox.MaximumOpenGlobal = 3 }},
		{"market style", func(c *Configuration) { c.Sandbox.OrderStyles[0] = "MARKET" }},
		{"profit claim", func(c *Configuration) { c.Sandbox.SandboxProfitabilityProof = true }},
		{"host-like secret override", func(c *Configuration) {
			c.Sandbox.SecretFileEnvironment.BybitAPIKeyFile = "BYBIT_ENDPOINT_URL"
		}},
		{"submission without global integration", func(c *Configuration) {
			c.Sandbox.SubmissionEnabled = true
		}},
		{"exchange submission without exchange integration", func(c *Configuration) {
			c.Sandbox.IntegrationsEnabled = true
			c.Sandbox.SubmissionEnabled = true
			c.Sandbox.Exchanges[0].SubmissionEnabled = true
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configuration, err := DefaultV1CConfiguration(ModeTestnet)
			if err != nil {
				t.Fatal(err)
			}
			test.alter(&configuration)
			if Validate(configuration) == nil {
				t.Fatal("unsafe V1C policy accepted")
			}
		})
	}

	legacy := DefaultV1BConfiguration()
	v1c, err := DefaultV1CConfiguration(ModeDemo)
	if err != nil {
		t.Fatal(err)
	}
	legacy.Sandbox = v1c.Sandbox
	if Validate(legacy) == nil {
		t.Fatal("V1C policy accepted under a V1B schema")
	}
}

func TestV1CRequiresIndependentGlobalAndExchangeEnablement(t *testing.T) {
	configuration, err := DefaultV1CConfiguration(ModeTestnet)
	if err != nil {
		t.Fatal(err)
	}
	configuration.Sandbox.IntegrationsEnabled = true
	configuration.Sandbox.SubmissionEnabled = true
	configuration.Sandbox.Exchanges[0].IntegrationEnabled = true
	configuration.Sandbox.Exchanges[0].SubmissionEnabled = true
	if err = Validate(configuration); err != nil {
		t.Fatalf("independently enabled Binance policy rejected: %v", err)
	}
	if configuration.Sandbox.Exchanges[1].IntegrationEnabled ||
		configuration.Sandbox.Exchanges[1].SubmissionEnabled {
		t.Fatal("Binance enablement activated Bybit")
	}
}
