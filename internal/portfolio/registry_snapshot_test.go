package portfolio

import (
	"testing"

	"axiom/internal/domain"
)

func TestSnapshotAssetRegistryPreservesAuthoritativeVersions(t *testing.T) {
	registry, err := NewSnapshotAssetRegistry([]AssetEligibility{
		{Asset: "BTC", Status: domain.AssetApproved, Version: 7},
		{Asset: "USDT", Status: domain.AssetApproved, Version: 11},
	})
	if err != nil {
		t.Fatal(err)
	}
	btc, exists := registry.Current("BTC")
	if !exists || btc.Version != 7 || btc.Status != domain.AssetApproved {
		t.Fatalf("btc eligibility=%#v exists=%t", btc, exists)
	}
	if _, err := NewSnapshotAssetRegistry([]AssetEligibility{
		{Asset: "BTC", Status: domain.AssetApproved, Version: 1},
		{Asset: "BTC", Status: domain.AssetApproved, Version: 2},
	}); err == nil {
		t.Fatal("duplicate asset eligibility accepted")
	}
	if _, err := NewSnapshotAssetRegistry([]AssetEligibility{{
		Asset: "BTC", Status: domain.AssetApproved,
	}}); err == nil {
		t.Fatal("unversioned asset eligibility accepted")
	}
}
