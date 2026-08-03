package bybit

import (
	"errors"
	"testing"

	"axiom/internal/domain"
)

func TestNormalizeDemoBalancesDerivesDeprecatedUnifiedFree(t *testing.T) {
	body := demoWalletBalanceFixture(
		`"coin":"USDT","walletBalance":"100000","locked":"1.25"`,
	)
	balances, err := normalizeDemoBalances(body)
	if err != nil || len(balances) != 1 {
		t.Fatalf("balances=%v error=%v", balances, err)
	}
	wantAvailable, _ := domain.ParseBalance("99998.75")
	wantReserved, _ := domain.ParseBalance("1.25")
	if balances[0].Asset != "USDT" ||
		balances[0].Available.Compare(wantAvailable) != 0 ||
		balances[0].Reserved.Compare(wantReserved) != 0 {
		t.Fatalf("unexpected balance: %#v", balances[0])
	}
}

func TestNormalizeDemoBalancesRejectsInconsistentLegacyFree(t *testing.T) {
	body := demoWalletBalanceFixture(
		`"coin":"USDT","walletBalance":"100","free":"99","locked":"2"`,
	)
	if _, err := normalizeDemoBalances(body); !errors.Is(err, ErrDemoPayload) {
		t.Fatalf("error=%v want=%v", err, ErrDemoPayload)
	}
}

func TestNormalizeDemoBalancesRejectsOrderMargin(t *testing.T) {
	body := demoWalletBalanceFixture(
		`"coin":"USDT","walletBalance":"100","locked":"0","totalOrderIM":"1"`,
	)
	if _, err := normalizeDemoBalances(body); !errors.Is(err, ErrDemoPayload) {
		t.Fatalf("error=%v want=%v", err, ErrDemoPayload)
	}
}

func TestNormalizeDemoBalancesAcceptsDocumentedCollateralMarkers(t *testing.T) {
	for _, marker := range []string{"-1", "0", "1", "2"} {
		body := demoWalletBalanceFixture(
			`"coin":"USDT","walletBalance":"100","locked":"0","colRes":"` +
				marker + `"`,
		)
		if _, err := normalizeDemoBalances(body); err != nil {
			t.Fatalf("marker=%q error=%v", marker, err)
		}
	}
}

func TestNormalizeDemoBalancesRejectsUnknownCollateralMarker(t *testing.T) {
	body := demoWalletBalanceFixture(
		`"coin":"USDT","walletBalance":"100","locked":"0","colRes":"3"`,
	)
	if _, err := normalizeDemoBalances(body); !errors.Is(err, ErrDemoPayload) {
		t.Fatalf("error=%v want=%v", err, ErrDemoPayload)
	}
}

func demoWalletBalanceFixture(coin string) []byte {
	return []byte(`{
	  "retCode":0,
	  "retMsg":"OK",
	  "result":{"list":[{"accountType":"UNIFIED","coin":[{` + coin + `}]}]},
	  "retExtInfo":{},
	  "time":1700000000000
	}`)
}
