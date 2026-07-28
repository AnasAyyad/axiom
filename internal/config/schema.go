package config

import "axiom/internal/domain"

// Configuration schema identifiers remain immutable and explicitly accepted.
const (
	SchemaVersion      = "axiom.config.v1a.2"
	SchemaVersionV1B   = "axiom.config.v1b.1"
	SchemaVersionV1BB3 = "axiom.config.v1b.2"
	SchemaVersionV1BB4 = "axiom.config.v1b.3"
	SchemaVersionV1BB5 = "axiom.config.v1b.4"
	SchemaVersionV1BB6 = "axiom.config.v1b.5"
	SchemaVersionV1C   = "axiom.config.v1c.1"
)

// Environment identifies an allowed V1A deployment class.
type Environment string

// Allowed V1A deployment environments.
const (
	EnvironmentLocal   Environment = "local"
	EnvironmentTest    Environment = "test"
	EnvironmentShadow  Environment = "shadow"
	EnvironmentSandbox Environment = "sandbox"
)

// Configuration is the complete versioned V1A product configuration graph.
type Configuration struct {
	SchemaVersion string                     `json:"schema_version"`
	Revision      uint64                     `json:"revision"`
	Environment   Environment                `json:"environment"`
	Mode          ExecutionMode              `json:"mode"`
	Product       domain.ProductKind         `json:"product"`
	Safety        SafetyConfiguration        `json:"safety"`
	Endpoint      EndpointConfiguration      `json:"endpoint"`
	Exchanges     []ExchangeConfiguration    `json:"exchanges,omitempty"`
	Assets        []domain.Asset             `json:"assets"`
	Instruments   []Instrument               `json:"instruments"`
	Risk          RiskConfiguration          `json:"risk"`
	Portfolio     PortfolioConfiguration     `json:"portfolio"`
	Models        ModelConfiguration         `json:"models"`
	Trend         TrendConfiguration         `json:"trend"`
	MeanReversion MeanReversionConfiguration `json:"mean_reversion,omitempty"`
	Triangular    TriangularConfiguration    `json:"triangular,omitempty"`
	CrossExchange CrossExchangeConfiguration `json:"cross_exchange,omitempty"`
	Rebalancing   RebalancingConfiguration   `json:"rebalancing,omitempty"`
	Sandbox       SandboxConfiguration       `json:"sandbox,omitempty"`
	Capabilities  []CapabilityDisposition    `json:"capabilities"`
	Secrets       []SecretReference          `json:"secrets"`
}

// ExchangeConfiguration selects one code-owned public venue and recording universe.
type ExchangeConfiguration struct {
	ID              string       `json:"id"`
	EndpointSet     string       `json:"endpoint_set"`
	REST            string       `json:"rest"`
	WebSocket       string       `json:"websocket"`
	Instruments     []Instrument `json:"instruments"`
	CandleIntervals []string     `json:"candle_intervals"`
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

// MeanReversionConfiguration identifies the immutable B3 baseline graph.
// It is absent from the original V1A and V1B.1 schemas so their hashes and
// interpretation remain unchanged.
type MeanReversionConfiguration struct {
	StrategyVersion  string              `json:"strategy_version"`
	PrimaryTimeframe string              `json:"primary_timeframe"`
	HigherTimeframe  string              `json:"higher_timeframe"`
	Parameters       []StrategyParameter `json:"parameters"`
}

// TriangularConfiguration identifies the immutable B4 exact-cycle graph. It is
// absent from earlier schemas so their hashes and interpretation remain stable.
type TriangularConfiguration struct {
	StrategyVersion string              `json:"strategy_version"`
	SettlementAsset string              `json:"settlement_asset"`
	Cycles          []string            `json:"cycles"`
	DispatchMode    string              `json:"dispatch_mode"`
	PricingModel    string              `json:"pricing_model"`
	ClaimModel      string              `json:"claim_model"`
	Parameters      []StrategyParameter `json:"parameters"`
}

// CrossExchangeConfiguration identifies the immutable B5 two-venue,
// inventory-restored, concurrent simulation graph. Earlier schema meanings
// remain unchanged because this graph must be absent from them.
type CrossExchangeConfiguration struct {
	StrategyVersion string              `json:"strategy_version"`
	SettlementAsset string              `json:"settlement_asset"`
	Instruments     []string            `json:"instruments"`
	Exchanges       []string            `json:"exchanges"`
	Directions      []string            `json:"directions"`
	DispatchMode    string              `json:"dispatch_mode"`
	PricingModel    string              `json:"pricing_model"`
	ClaimModel      string              `json:"claim_model"`
	RebalancingMode string              `json:"rebalancing_mode"`
	Parameters      []StrategyParameter `json:"parameters"`
}

// RebalancingConfiguration identifies the immutable B6 advisory optimizer and
// reviewed fact contract. Earlier schemas keep this graph absent so their
// interpretation and canonical hashes do not change.
type RebalancingConfiguration struct {
	OptimizerVersion      string              `json:"optimizer_version"`
	FactSchemaVersion     string              `json:"fact_schema_version"`
	CostModelVersion      string              `json:"cost_model_version"`
	Mode                  string              `json:"mode"`
	NaturalReversalPolicy string              `json:"natural_reversal_policy"`
	ApprovedAssets        []string            `json:"approved_assets"`
	Exchanges             []string            `json:"exchanges"`
	Parameters            []StrategyParameter `json:"parameters"`
}

// SandboxConfiguration is the complete C1-C6 default-off authenticated
// sandbox policy. It contains no credential values, endpoints, or proxy URLs.
type SandboxConfiguration struct {
	IntegrationsEnabled       bool                           `json:"integrations_enabled"`
	SubmissionEnabled         bool                           `json:"submission_enabled"`
	ArmDurationSeconds        uint32                         `json:"arm_duration_seconds"`
	ReauthorizationSeconds    uint32                         `json:"reauthorization_seconds"`
	MaximumOrderNotional      FinancialValue                 `json:"maximum_order_notional"`
	MaximumDailyNotional      FinancialValue                 `json:"maximum_daily_notional"`
	MaximumOpenPerAccount     uint32                         `json:"maximum_open_per_account"`
	MaximumOpenGlobal         uint32                         `json:"maximum_open_global"`
	OrderStyles               []string                       `json:"order_styles"`
	EligibleStrategies        []string                       `json:"eligible_strategies"`
	RebalancingMode           string                         `json:"rebalancing_mode"`
	SandboxProfitabilityProof bool                           `json:"sandbox_profitability_proof"`
	Exchanges                 []SandboxExchangeConfiguration `json:"exchanges"`
	SecretFileEnvironment     SandboxSecretEnvironment       `json:"secret_file_environment"`
}

// SandboxExchangeConfiguration contains only closed environment identifiers
// and independent enablement gates. Network destinations remain compiled code.
type SandboxExchangeConfiguration struct {
	ID                 string `json:"id"`
	Environment        string `json:"environment"`
	IntegrationEnabled bool   `json:"integration_enabled"`
	SubmissionEnabled  bool   `json:"submission_enabled"`
}

// SandboxSecretEnvironment freezes the only accepted file-reference variable
// names. Values are resolved by the owning process, never by product config.
type SandboxSecretEnvironment struct {
	BinanceAPIKeyFile    string `json:"binance_api_key_file"`
	BinanceAPISecretFile string `json:"binance_api_secret_file"`
	BybitAPIKeyFile      string `json:"bybit_api_key_file"`
	BybitAPISecretFile   string `json:"bybit_api_secret_file"`
	TOTPSeedFile         string `json:"totp_seed_file"`
}

// StrategyParameter is the complete auditable contract for one numeric rule.
type StrategyParameter struct {
	ID                 string   `json:"id"`
	Description        string   `json:"description"`
	Value              string   `json:"value"`
	Unit               string   `json:"unit"`
	Minimum            string   `json:"minimum"`
	Maximum            string   `json:"maximum"`
	MinimumInclusive   bool     `json:"minimum_inclusive"`
	MaximumInclusive   bool     `json:"maximum_inclusive"`
	Scale              uint8    `json:"scale"`
	Rounding           string   `json:"rounding"`
	Cadence            string   `json:"cadence"`
	WarmUp             string   `json:"warm_up"`
	Mutability         string   `json:"mutability"`
	ModelDependencies  []string `json:"model_dependencies"`
	AlgorithmVersion   string   `json:"algorithm_version,omitempty"`
	EvaluationTimezone string   `json:"evaluation_timezone,omitempty"`
	ChangeBehavior     string   `json:"change_behavior,omitempty"`
	ApprovalActor      string   `json:"approval_actor,omitempty"`
	ApprovalReference  string   `json:"approval_reference,omitempty"`
	ApprovedAt         string   `json:"approved_at,omitempty"`
	ChangeReason       string   `json:"change_reason,omitempty"`
}

// SecretReference names a required file without storing secret material.
type SecretReference struct {
	Name     string `json:"name"`
	File     string `json:"file"`
	Required bool   `json:"required"`
}
