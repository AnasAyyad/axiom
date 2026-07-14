package config

// CapabilityDisposition is a non-activatable declaration of V1A unavailability.
type CapabilityDisposition string

// Every restricted capability is fixed to an unsupported disposition in V1A.
const (
	CapabilityExternalOrdersUnsupported   CapabilityDisposition = "external_orders_unsupported"
	CapabilityAuthenticatedAPIUnsupported CapabilityDisposition = "authenticated_api_unsupported"
	CapabilityWithdrawalsUnsupported      CapabilityDisposition = "withdrawals_unsupported"
	CapabilityTransfersUnsupported        CapabilityDisposition = "transfers_unsupported"
	CapabilityMarginUnsupported           CapabilityDisposition = "margin_unsupported"
	CapabilityFuturesUnsupported          CapabilityDisposition = "futures_unsupported"
	CapabilityPerpetualsUnsupported       CapabilityDisposition = "perpetuals_unsupported"
	CapabilityOptionsUnsupported          CapabilityDisposition = "options_unsupported"
	CapabilityLeverageUnsupported         CapabilityDisposition = "leverage_unsupported"
	CapabilityBorrowingUnsupported        CapabilityDisposition = "borrowing_unsupported"
	CapabilityLendingUnsupported          CapabilityDisposition = "lending_unsupported"
	CapabilityStakingUnsupported          CapabilityDisposition = "staking_unsupported"
	CapabilityShortSellingUnsupported     CapabilityDisposition = "short_selling_unsupported"
)

// UnsupportedCapabilities returns the complete, non-activatable V1A declaration.
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
