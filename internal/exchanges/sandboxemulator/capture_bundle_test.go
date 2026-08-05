package sandboxemulator

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"axiom/internal/sandbox"
)

func TestRedactedCaptureBundleContainsOnlyApprovedShape(t *testing.T) {
	const apiKey = "private-api-key-value"
	const secret = "private-signing-secret-value"
	emulator, err := New(Config{
		Exchange: sandbox.ExchangeBinance, APIKey: apiKey, APISecret: secret,
	})
	if err != nil {
		t.Fatal(err)
	}
	values := url.Values{
		"recvWindow": {"5000"},
		"timestamp":  {"1700000000000"},
	}
	values.Set("signature", sign(secret, values.Encode()))
	request, _ := http.NewRequestWithContext(
		context.Background(), http.MethodGet,
		"https://testnet.binance.vision/api/v3/account?"+values.Encode(), nil,
	)
	request.Header.Set("X-MBX-APIKEY", apiKey)
	response, err := emulator.Do(request)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("request rejected: status=%d err=%v", response.StatusCode, err)
	}
	payload, err := emulator.RedactedCaptureBundle(strings.Repeat("a", 40))
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(payload)
	for _, forbidden := range []string{
		apiKey,
		secret,
		values.Get("signature"),
		"X-MBX-APIKEY",
		"Authorization",
	} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("capture leaked %q: %s", forbidden, encoded)
		}
	}
	for _, required := range []string{
		`"host": "testnet.binance.vision"`,
		`"path": "/api/v3/account"`,
		`"field_names"`,
		`"request_hash"`,
	} {
		if !strings.Contains(encoded, required) {
			t.Fatalf("capture omits %s: %s", required, encoded)
		}
	}
}

func TestRedactedCaptureBundleRejectsMissingOrPlaceholderIdentity(t *testing.T) {
	emulator, _ := New(Config{
		Exchange: sandbox.ExchangeBybit, APIKey: "key", APISecret: "secret",
	})
	for _, sourceSHA := range []string{"", strings.Repeat("0", 40), "wrong"} {
		if _, err := emulator.RedactedCaptureBundle(sourceSHA); err == nil {
			t.Fatalf("source %q accepted", sourceSHA)
		}
	}
}
