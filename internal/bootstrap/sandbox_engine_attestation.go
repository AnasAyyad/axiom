package bootstrap

import (
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"axiom/internal/sandbox"
	postgresstore "axiom/internal/storage/postgres"
)

type sandboxEngineAttestation struct {
	accountHash    string
	keyFingerprint string
	ownerAttested  bool
}

func loadSandboxEngineAttestation(
	exchange sandbox.Exchange,
	now time.Time,
) (
	sandboxEngineAttestation,
	postgresstore.SandboxRuntimeEngineAccount,
	error,
) {
	accountHash := os.Getenv("AXIOM_SANDBOX_ACCOUNT_IDENTITY_HASH")
	fingerprint := os.Getenv("AXIOM_SANDBOX_KEY_FINGERPRINT")
	attested := os.Getenv("AXIOM_SANDBOX_OWNER_ATTESTED") == "true"
	generation, generationErr := strconv.ParseUint(
		os.Getenv("AXIOM_SANDBOX_CREDENTIAL_GENERATION"),
		10,
		64,
	)
	if generationErr != nil || generation != 1 ||
		!lowerHex(accountHash, 64) ||
		!lowerHex(fingerprint, 32) ||
		!attested || now.IsZero() || now.Location() != time.UTC {
		return sandboxEngineAttestation{},
			postgresstore.SandboxRuntimeEngineAccount{},
			fmt.Errorf("sandbox_engine_attestation_invalid")
	}
	prefix := "binance-testnet-"
	environment := sandbox.EnvironmentBinanceSpotTestnet
	if exchange == sandbox.ExchangeBybit {
		prefix = "bybit-demo-"
		environment = sandbox.EnvironmentBybitDemo
	}
	account := postgresstore.SandboxRuntimeEngineAccount{
		AccountID:            sandbox.AccountID(prefix + accountHash[:16]),
		Exchange:             exchange,
		Environment:          environment,
		AccountIdentityHash:  accountHash,
		CredentialGeneration: generation,
		State:                sandbox.EngineLocked,
	}
	return sandboxEngineAttestation{
		accountHash:    accountHash,
		keyFingerprint: fingerprint,
		ownerAttested:  true,
	}, account, nil
}

func lowerHex(value string, length int) bool {
	if len(value) != length || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
