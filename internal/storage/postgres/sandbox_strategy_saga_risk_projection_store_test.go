package postgres

import (
	"strings"
	"testing"
)

func TestStrategySagaRiskProjectionVerifiesEveryPeerLeaseWithoutBorrowingFence(t *testing.T) {
	for _, required := range []string{
		"JOIN sandbox_strategy_session_accounts membership",
		"JOIN sandbox_runtime_exchange_accounts account",
		"JOIN sandbox_runtime_account_leases lease",
		"lease.environment=account.environment",
		"lease.expires_at>$14",
		"membership.account_id=$11",
		"membership.account_epoch=$12",
		"membership.exchange=$13",
		"strategy.state='running'",
		"parent.state='ARMED'",
		"arm.revoked_at IS NULL",
	} {
		if !strings.Contains(sandboxSagaRiskMemberCurrentSQL, required) {
			t.Fatalf("peer lease/session proof missing %q", required)
		}
	}
	for _, forbidden := range []string{"lease.owner=$", "lease.fencing_token=$"} {
		if strings.Contains(sandboxSagaRiskMemberCurrentSQL, forbidden) {
			t.Fatalf("coordinator was allowed to supply peer authority: %q", forbidden)
		}
	}
}
