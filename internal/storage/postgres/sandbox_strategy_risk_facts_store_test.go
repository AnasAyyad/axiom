package postgres

import (
	"strings"
	"testing"

	"axiom/internal/domain"
	"axiom/internal/sandbox"
)

func TestSandboxStrategySettlementBalanceRequiresOneUSDTBalance(t *testing.T) {
	available, _ := domain.ParseBalance("100")
	reserved, _ := domain.ParseBalance("5")
	btc, _ := domain.ParseBalance("1")
	snapshot := sandbox.AccountSnapshot{Balances: []sandbox.Balance{
		{Asset: "BTC", Available: btc}, {Asset: "USDT", Available: available, Reserved: reserved},
	}}
	gotAvailable, gotReserved, err := sandboxStrategySettlementBalance(snapshot)
	if err != nil || gotAvailable.String() != "100" || gotReserved.String() != "5" {
		t.Fatalf("balance available=%s reserved=%s error=%v", gotAvailable.String(), gotReserved.String(), err)
	}
	_, _, err = sandboxStrategySettlementBalance(sandbox.AccountSnapshot{Balances: []sandbox.Balance{{Asset: "BTC", Available: btc}}})
	if err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("missing USDT error=%v", err)
	}
	_, _, err = sandboxStrategySettlementBalance(sandbox.AccountSnapshot{Balances: []sandbox.Balance{
		{Asset: "USDT", Available: available}, {Asset: "USDT", Available: available},
	}})
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("duplicate USDT error=%v", err)
	}
}

func TestStrategyRiskFactsQueryUsesOnlyNormalPlatformPolicyAndRoundsReserveUp(t *testing.T) {
	for _, fragment := range []string{
		"policy.scope_kind='global'", "policy.scope_id='platform'", "policy.state='NORMAL'",
		"current_risk_state.state='NORMAL'", "limits.account_drawdown::text",
		"policy.effective_at<=$1", "ceil((($2::numeric+$3::numeric)*minimum_reserve::numeric)*1000000000000000000)",
		"maximum_reserved_capital", "ceil((($2::numeric+$3::numeric)*maximum_reserved_capital::numeric)*1000000000000000000)",
	} {
		if !strings.Contains(strategyRiskFactsSQL, fragment) {
			t.Fatalf("risk fact query missing %q", fragment)
		}
	}
}
