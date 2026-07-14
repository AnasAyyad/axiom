package config

import (
	"fmt"
	"sort"
	"strings"
)

var prohibitedCapabilityTokens = []string{
	"LIVE_TRADING",
	"REAL_TRADING",
	"WITHDRAW",
	"TRANSFER_EXECUTION",
	"MARGIN",
	"FUTURES",
	"PERPETUAL",
	"LEVERAGE",
	"BORROW",
	"LENDING",
	"STAKING",
	"SHORT_SELL",
}

var credentialTokens = []string{
	"API_KEY",
	"API_SECRET",
	"PRIVATE_KEY",
	"CREDENTIAL",
	"LISTEN_KEY",
	"SIGNING_KEY",
}

// ValidateEnvironment rejects forbidden capability or exchange-credential keys.
func ValidateEnvironment(environ []string) error {
	var rejected []string
	for _, entry := range environ {
		key, _, found := strings.Cut(entry, "=")
		if !found {
			key = entry
		}
		upper := strings.ToUpper(key)
		for _, token := range prohibitedCapabilityTokens {
			if strings.Contains(upper, token) {
				rejected = append(rejected, key)
				break
			}
		}
		if hasExchangeScope(upper) && containsAny(upper, credentialTokens) {
			rejected = append(rejected, key)
		}
	}
	if len(rejected) > 0 {
		sort.Strings(rejected)
		return fmt.Errorf("prohibited_environment_key:%s", rejected[0])
	}
	return nil
}

func hasExchangeScope(key string) bool {
	return strings.Contains(key, "BINANCE") || strings.Contains(key, "BYBIT") || strings.Contains(key, "EXCHANGE")
}

func containsAny(value string, tokens []string) bool {
	for _, token := range tokens {
		if strings.Contains(value, token) {
			return true
		}
	}
	return false
}
