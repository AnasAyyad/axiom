package config

import "axiom/internal/domain"

// SchemaVersion is the current immutable configuration schema identifier.
const SchemaVersion = "axiom.config.v1a.2"

// Environment identifies an allowed V1A deployment class.
type Environment string

// Allowed V1A deployment environments.
const (
	EnvironmentLocal  Environment = "local"
	EnvironmentTest   Environment = "test"
	EnvironmentShadow Environment = "shadow"
)

// Configuration is the complete versioned V1A product configuration graph.
type Configuration struct {
	SchemaVersion string                  `json:"schema_version"`
	Revision      uint64                  `json:"revision"`
	Environment   Environment             `json:"environment"`
	Mode          ExecutionMode           `json:"mode"`
	Product       domain.ProductKind      `json:"product"`
	Safety        SafetyConfiguration     `json:"safety"`
	Endpoint      EndpointConfiguration   `json:"endpoint"`
	Assets        []domain.Asset          `json:"assets"`
	Instruments   []Instrument            `json:"instruments"`
	Risk          RiskConfiguration       `json:"risk"`
	Portfolio     PortfolioConfiguration  `json:"portfolio"`
	Models        ModelConfiguration      `json:"models"`
	Trend         TrendConfiguration      `json:"trend"`
	Capabilities  []CapabilityDisposition `json:"capabilities"`
	Secrets       []SecretReference       `json:"secrets"`
}

// SafetyConfiguration declares mandatory fail-closed runtime posture.
type SafetyConfiguration struct {
	FailClosed       bool   `json:"fail_closed"`
	RiskInitialState string `json:"risk_initial_state"`
	AutoUnpause      bool   `json:"auto_unpause"`
}

// EndpointConfiguration selects one code-owned public market-data endpoint set.
type EndpointConfiguration struct {
	Set       string `json:"set"`
	REST      string `json:"rest"`
	WebSocket string `json:"websocket"`
}

// Instrument declares one allowed canonical spot pair.
type Instrument struct {
	Base    string `json:"base"`
	Quote   string `json:"quote"`
	Product string `json:"product"`
}

// RiskConfiguration holds explicit decimal-string V1A risk limits.
type RiskConfiguration struct {
	MaximumAssetAllocation FinancialValue `json:"maximum_asset_allocation"`
	MaximumOrderNotional   FinancialValue `json:"maximum_order_notional"`
	MaximumDailyLoss       FinancialValue `json:"maximum_daily_loss"`
}

// PortfolioConfiguration declares the initial virtual settlement capital.
type PortfolioConfiguration struct {
	SettlementAsset string         `json:"settlement_asset"`
	StartingCapital FinancialValue `json:"starting_capital"`
}

// FinancialValue documents a decimal setting's complete numeric contract.
type FinancialValue struct {
	Value            string `json:"value"`
	Unit             string `json:"unit"`
	Minimum          string `json:"minimum"`
	Maximum          string `json:"maximum"`
	MinimumInclusive bool   `json:"minimum_inclusive"`
	MaximumInclusive bool   `json:"maximum_inclusive"`
	Scale            uint8  `json:"scale"`
	Rounding         string `json:"rounding"`
}

// ModelConfiguration identifies approved deterministic V1A model versions.
type ModelConfiguration struct {
	Fee     string `json:"fee"`
	Latency string `json:"latency"`
}

// TrendConfiguration identifies one immutable baseline Trend strategy graph.
type TrendConfiguration struct {
	StrategyVersion string              `json:"strategy_version"`
	Timeframe       string              `json:"timeframe"`
	Parameters      []StrategyParameter `json:"parameters"`
}

// StrategyParameter is the complete auditable contract for one numeric rule.
type StrategyParameter struct {
	ID                string   `json:"id"`
	Description       string   `json:"description"`
	Value             string   `json:"value"`
	Unit              string   `json:"unit"`
	Minimum           string   `json:"minimum"`
	Maximum           string   `json:"maximum"`
	MinimumInclusive  bool     `json:"minimum_inclusive"`
	MaximumInclusive  bool     `json:"maximum_inclusive"`
	Scale             uint8    `json:"scale"`
	Rounding          string   `json:"rounding"`
	Cadence           string   `json:"cadence"`
	WarmUp            string   `json:"warm_up"`
	Mutability        string   `json:"mutability"`
	ModelDependencies []string `json:"model_dependencies"`
}

// SecretReference names a required file without storing secret material.
type SecretReference struct {
	Name     string `json:"name"`
	File     string `json:"file"`
	Required bool   `json:"required"`
}
