package egressproxy

import "sort"

// Policy identifies one closed authenticated-engine egress boundary.
type Policy string

// Closed sandbox runtime egress policies.
const (
	PolicyBinanceTestnet Policy = "binance_testnet"
	PolicyBybitDemo      Policy = "bybit_demo"
)

var policyHosts = map[Policy][]string{
	PolicyBinanceTestnet: {
		"stream.testnet.binance.vision",
		"testnet.binance.vision",
		"ws-api.testnet.binance.vision",
	},
	PolicyBybitDemo: {
		"api-demo.bybit.com",
		"api.bybit.com",
		"stream-demo.bybit.com",
		"stream.bybit.com",
	},
}

// Hosts returns the exact sorted host allowlist for one policy.
func Hosts(policy Policy) ([]string, error) {
	hosts, ok := policyHosts[policy]
	if !ok {
		return nil, proxyError("policy_invalid")
	}
	result := append([]string(nil), hosts...)
	sort.Strings(result)
	return result, nil
}

func hostAllowed(policy Policy, host string) bool {
	hosts, ok := policyHosts[policy]
	if !ok {
		return false
	}
	index := sort.SearchStrings(hosts, host)
	return index < len(hosts) && hosts[index] == host
}
