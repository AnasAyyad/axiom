package config

import (
	"fmt"
	"path/filepath"
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

var sandboxSecretFileKeys = map[string]struct{}{
	binanceKeyFileEnvironment:    {},
	binanceSecretFileEnvironment: {},
	bybitKeyFileEnvironment:      {},
	bybitSecretFileEnvironment:   {},
	totpSeedFileEnvironment:      {},
}

// ValidateEnvironment rejects forbidden capability or exchange-credential keys.
func ValidateEnvironment(environ []string) error {
	var rejected []string
	for _, entry := range environ {
		key, value, found := strings.Cut(entry, "=")
		if !found {
			key = entry
		}
		upper := strings.ToUpper(key)
		if _, allowed := sandboxSecretFileKeys[upper]; allowed {
			if !found || !validSandboxSecretPath(value) {
				rejected = append(rejected, key)
			}
			continue
		}
		for _, token := range prohibitedCapabilityTokens {
			if strings.Contains(upper, token) {
				rejected = append(rejected, key)
				break
			}
		}
		if hasExchangeScope(upper) && containsAny(upper, credentialTokens) {
			rejected = append(rejected, key)
		}
		if hasExchangeScope(upper) && containsAny(upper, []string{"BASE_URL", "ENDPOINT_URL", "PROXY", "PRIVATE_HOST"}) {
			rejected = append(rejected, key)
		}
	}
	if len(rejected) > 0 {
		sort.Strings(rejected)
		return fmt.Errorf("prohibited_environment_key:%s", rejected[0])
	}
	return nil
}

func validSandboxSecretPath(value string) bool {
	return filepath.IsAbs(value) && filepath.Clean(value) == value &&
		strings.HasPrefix(value, "/run/secrets/") && filepath.Base(value) != "." &&
		filepath.Base(value) != ".."
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
