package postgres

import (
	"strings"
	"testing"
)

func TestStrategySagaWorkQueryRequiresExactLiveTopologyAndEveryPeerLease(t *testing.T) {
	for _, fragment := range []string{
		"strategy.id=$1", "parent.revision=$7", "strategy.revision=$8",
		"arm.id=$9", "arm.revision=$10", "parent.state='ARMED'",
		"strategy.state='running'", "arm.revoked_at IS NULL",
		"JOIN sandbox_runtime_account_leases lease", "lease.expires_at>$11",
		"ORDER BY membership.exchange,membership.account_id",
	} {
		if !strings.Contains(strategySessionSagaWorkSQL, fragment) {
			t.Fatalf("strategy saga work query missing %q", fragment)
		}
	}
}

func TestStrategySagaEligibilityIsClosedToTheThreeApprovedSpotMarkets(t *testing.T) {
	values, err := validSandboxSagaEligibilityInstruments([]string{"ETHBTC", "BTCUSDT", "ETHUSDT"})
	if err != nil || strings.Join(values, ",") != "BTCUSDT,ETHBTC,ETHUSDT" {
		t.Fatalf("approved instruments=%v error=%v", values, err)
	}
	for _, values := range [][]string{{}, {"BTCUSD"}, {"BTCUSDT", "BTCUSDT"},
		{"BTCUSDT", "ETHUSDT", "ETHBTC", "BTCUSDT"}} {
		if _, err = validSandboxSagaEligibilityInstruments(values); err == nil {
			t.Fatalf("invalid instruments accepted: %v", values)
		}
	}
	for _, fragment := range []string{
		"account_id=$1", "account_epoch=$2", "exchange=$3", "startup_cycle=$4",
		"instrument=ANY($5::text[])", "observed_at<=$6",
		"$6-observed_at<=interval '250 milliseconds'", "ORDER BY instrument",
	} {
		if !strings.Contains(strategySessionSagaEligibilitySQL, fragment) {
			t.Fatalf("strategy saga eligibility query missing %q", fragment)
		}
	}
}
