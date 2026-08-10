package config

// CapabilityDisposition is a non-activatable declaration of initial trend unavailability.
type CapabilityDisposition string

// Every restricted capability is fixed to an unsupported disposition in initial trend.
const (
	CapabilityExternalOrdersUnsupported    CapabilityDisposition = "external_orders_unsupported"
	CapabilityAuthenticatedAPIUnsupported  CapabilityDisposition = "authenticated_api_unsupported"
	CapabilityWithdrawalsUnsupported       CapabilityDisposition = "withdrawals_unsupported"
	CapabilityTransfersUnsupported         CapabilityDisposition = "transfers_unsupported"
	CapabilityMarginUnsupported            CapabilityDisposition = "margin_unsupported"
	CapabilityFuturesUnsupported           CapabilityDisposition = "futures_unsupported"
	CapabilityPerpetualsUnsupported        CapabilityDisposition = "perpetuals_unsupported"
	CapabilityOptionsUnsupported           CapabilityDisposition = "options_unsupported"
	CapabilityLeverageUnsupported          CapabilityDisposition = "leverage_unsupported"
	CapabilityBorrowingUnsupported         CapabilityDisposition = "borrowing_unsupported"
	CapabilityLendingUnsupported           CapabilityDisposition = "lending_unsupported"
	CapabilityStakingUnsupported           CapabilityDisposition = "staking_unsupported"
	CapabilityShortSellingUnsupported      CapabilityDisposition = "short_selling_unsupported"
	CapabilityProductionOrdersUnsupported  CapabilityDisposition = "production_orders_unsupported"
	CapabilitySandboxOrdersCompiled        CapabilityDisposition = "sandbox_orders_compiled"
	CapabilityAuthenticatedSandboxCompiled CapabilityDisposition = "authenticated_sandbox_compiled"
)

// UnsupportedCapabilities returns the complete, non-activatable initial trend declaration.
func UnsupportedCapabilities() []CapabilityDisposition {
	return []CapabilityDisposition{
		CapabilityExternalOrdersUnsupported,
		CapabilityAuthenticatedAPIUnsupported,
		CapabilityWithdrawalsUnsupported,
		CapabilityTransfersUnsupported,
		CapabilityMarginUnsupported,
		CapabilityFuturesUnsupported,
		CapabilityPerpetualsUnsupported,
		CapabilityOptionsUnsupported,
		CapabilityLeverageUnsupported,
		CapabilityBorrowingUnsupported,
		CapabilityLendingUnsupported,
		CapabilityStakingUnsupported,
		CapabilityShortSellingUnsupported,
	}
}

// SandboxRuntimeCapabilities returns the descriptive sandbox boundary. The compiled
// sandbox capabilities are still inert until all independent runtime gates
// pass, while production and prohibited products remain unavailable.
func SandboxRuntimeCapabilities() []CapabilityDisposition {
	return []CapabilityDisposition{
		CapabilityProductionOrdersUnsupported,
		CapabilitySandboxOrdersCompiled,
		CapabilityAuthenticatedSandboxCompiled,
		CapabilityWithdrawalsUnsupported,
		CapabilityTransfersUnsupported,
		CapabilityMarginUnsupported,
		CapabilityFuturesUnsupported,
		CapabilityPerpetualsUnsupported,
		CapabilityOptionsUnsupported,
		CapabilityLeverageUnsupported,
		CapabilityBorrowingUnsupported,
		CapabilityLendingUnsupported,
		CapabilityStakingUnsupported,
		CapabilityShortSellingUnsupported,
	}
}

func capabilitiesExactlyUnsupported(actual []CapabilityDisposition) bool {
	expected := UnsupportedCapabilities()
	if len(actual) != len(expected) {
		return false
	}
	for index := range expected {
		if actual[index] != expected[index] {
			return false
		}
	}
	return true
}

func capabilitiesExactlySandboxRuntime(actual []CapabilityDisposition) bool {
	expected := SandboxRuntimeCapabilities()
	if len(actual) != len(expected) {
		return false
	}
	for index := range expected {
		if actual[index] != expected[index] {
			return false
		}
	}
	return true
}
