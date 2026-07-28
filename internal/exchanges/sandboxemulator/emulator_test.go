package sandboxemulator

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"axiom/internal/sandbox"
)

func TestBinanceEmulatorRejectsProductionAndDoesNotCaptureSecrets(t *testing.T) {
	emulator, err := New(Config{
		Exchange: sandbox.ExchangeBinance, APIKey: "key", APISecret: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	values := url.Values{"recvWindow": {"5000"}, "timestamp": {"1700000000000"}}
	signature := sign("secret", values.Encode())
	values.Set("signature", signature)
	request, _ := http.NewRequestWithContext(
		context.Background(), http.MethodGet,
		"https://testnet.binance.vision/api/v3/account?"+values.Encode(), nil,
	)
	request.Header.Set("X-MBX-APIKEY", "key")
	response, err := emulator.Do(request)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("valid request rejected: %v %v", response.StatusCode, err)
	}
	capture := emulator.Captures()[0]
	encoded := strings.Join(capture.FieldNames, ",") + capture.RequestHash
	if strings.Contains(encoded, "secret") || strings.Contains(encoded, "key") ||
		strings.Contains(encoded, "signature") || strings.Contains(encoded, signature) {
		t.Fatalf("secret-bearing capture: %#v", capture)
	}
	production, _ := http.NewRequestWithContext(
		context.Background(), http.MethodPost, "https://api.binance.com/api/v3/order?"+values.Encode(), nil,
	)
	production.Header.Set("X-MBX-APIKEY", "key")
	response, _ = emulator.Do(production)
	if response.StatusCode != http.StatusForbidden || len(emulator.Captures()) != 1 {
		t.Fatal("production target accepted or captured")
	}
}

func TestBybitEmulatorRejectsProductionPrivateHost(t *testing.T) {
	emulator, _ := New(Config{
		Exchange: sandbox.ExchangeBybit, APIKey: "key", APISecret: "secret",
	})
	request, _ := http.NewRequestWithContext(
		context.Background(), http.MethodGet, "https://api.bybit.com/v5/user/query-api", nil,
	)
	request.Header.Set("X-BAPI-API-KEY", "key")
	request.Header.Set("X-BAPI-TIMESTAMP", "1700000000000")
	request.Header.Set("X-BAPI-RECV-WINDOW", "5000")
	request.Header.Set("X-BAPI-SIGN", sign("secret", "1700000000000key5000"))
	response, _ := emulator.Do(request)
	if response.StatusCode != http.StatusForbidden || len(emulator.Captures()) != 0 {
		t.Fatal("Bybit production-private request accepted")
	}
}

func sign(secret, value string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}
