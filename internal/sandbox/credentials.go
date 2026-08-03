package sandbox

import (
	"errors"
	"os"
	"strings"

	"axiom/internal/security"
)

// Credential file-reference names are the only accepted exchange secret inputs.
const (
	BinanceAPIKeyFileEnvironment    = "AXIOM_BINANCE_TESTNET_API_KEY_FILE"
	BinanceAPISecretFileEnvironment = "AXIOM_BINANCE_TESTNET_API_SECRET_FILE"
	BybitAPIKeyFileEnvironment      = "AXIOM_BYBIT_DEMO_API_KEY_FILE"
	BybitAPISecretFileEnvironment   = "AXIOM_BYBIT_DEMO_API_SECRET_FILE"
	TOTPSeedFileEnvironment         = "AXIOM_TOTP_SEED_FILE"
)

// Credential loading errors remain generic and never contain secret values.
var (
	ErrCredentialReferenceMissing = errors.New("sandbox_credential_reference_missing")
	ErrRawCredentialEnvironment   = errors.New("sandbox_raw_credential_environment_rejected")
	ErrEndpointOverride           = errors.New("sandbox_endpoint_override_rejected")
	ErrUnsupportedAccount         = errors.New("sandbox_credential_account_unsupported")
)

// CredentialPair is intentionally short-lived constructor input. Callers must
// not log, serialize, or persist it.
type CredentialPair struct {
	APIKey    string
	APISecret string
}

// LoadCredentialPair resolves only the compile-time file references associated
// with one sandbox exchange.
func LoadCredentialPair(exchange Exchange) (CredentialPair, error) {
	if err := RejectUnsafeSandboxEnvironment(os.Environ()); err != nil {
		return CredentialPair{}, err
	}
	keyName, secretName, ok := credentialReferenceNames(exchange)
	if !ok {
		return CredentialPair{}, ErrUnsupportedAccount
	}
	keyPath, keyOK := os.LookupEnv(keyName)
	secretPath, secretOK := os.LookupEnv(secretName)
	if !keyOK || !secretOK || keyPath == "" || secretPath == "" {
		return CredentialPair{}, ErrCredentialReferenceMissing
	}
	key, err := security.ReadSecretFile(keyPath)
	if err != nil {
		return CredentialPair{}, err
	}
	secret, err := security.ReadSecretFile(secretPath)
	if err != nil {
		return CredentialPair{}, err
	}
	return CredentialPair{APIKey: key, APISecret: secret}, nil
}

func credentialReferenceNames(exchange Exchange) (string, string, bool) {
	switch exchange {
	case ExchangeBinance:
		return BinanceAPIKeyFileEnvironment, BinanceAPISecretFileEnvironment, true
	case ExchangeBybit:
		return BybitAPIKeyFileEnvironment, BybitAPISecretFileEnvironment, true
	default:
		return "", "", false
	}
}

// RejectUnsafeSandboxEnvironment makes raw credentials and endpoint/proxy
// overrides a startup error even if another package would otherwise ignore them.
func RejectUnsafeSandboxEnvironment(environment []string) error {
	for _, item := range environment {
		name, _, _ := strings.Cut(item, "=")
		upper := strings.ToUpper(name)
		if isApprovedFileReference(name) {
			continue
		}
		if strings.HasPrefix(upper, "AXIOM_BINANCE_") || strings.HasPrefix(upper, "AXIOM_BYBIT_") {
			if strings.Contains(upper, "URL") || strings.Contains(upper, "HOST") ||
				strings.Contains(upper, "ENDPOINT") || strings.Contains(upper, "PROXY") {
				return ErrEndpointOverride
			}
			if strings.Contains(upper, "KEY") || strings.Contains(upper, "SECRET") ||
				strings.Contains(upper, "TOKEN") || strings.Contains(upper, "SIGNATURE") {
				return ErrRawCredentialEnvironment
			}
		}
	}
	return nil
}

func isApprovedFileReference(name string) bool {
	switch name {
	case BinanceAPIKeyFileEnvironment, BinanceAPISecretFileEnvironment,
		BybitAPIKeyFileEnvironment, BybitAPISecretFileEnvironment, TOTPSeedFileEnvironment:
		return true
	default:
		return false
	}
}
