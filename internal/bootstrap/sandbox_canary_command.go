package bootstrap

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"axiom/internal/config"
	"axiom/internal/domain"
	"axiom/internal/sandbox"
	"axiom/internal/security"
	postgresstore "axiom/internal/storage/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
)

type sandboxCanaryRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	TOTP       string `json:"totp"`
	Reason     string `json:"reason"`
	Instrument string `json:"instrument"`
	Side       string `json:"side"`
	Quantity   string `json:"quantity"`
	LimitPrice string `json:"limit_price"`
	Style      string `json:"style"`
}

func runSandboxCanary(
	ctx context.Context,
	runtimeConfig config.Runtime,
	product config.Configuration,
	source config.Source,
	command Command,
	output io.Writer,
) error {
	pool, err := postgresstore.Open(ctx, runtimeConfig.Database)
	if err != nil {
		return err
	}
	defer pool.Close()
	store, err := postgresstore.NewV1CDispatcherStore(pool)
	if err != nil {
		return err
	}
	snapshot, err := config.NewSnapshot(
		product,
		source,
		"sandbox-canary",
		&domain.SystemClock{},
	)
	if err != nil {
		return err
	}
	exchange := sandbox.Exchange(command.Exchange)
	identity, err := executeSandboxCanaryPhase(
		ctx,
		pool,
		store,
		runtimeConfig,
		product,
		snapshot.ID().String(),
		exchange,
		command,
	)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(
		output,
		"%s_id=%s\n",
		sandboxCanaryOutputLabel(command.Phase),
		identity,
	)
	return err
}

func sandboxCanaryOutputLabel(phase string) string {
	switch phase {
	case "verify":
		return "evidence"
	case "recover":
		return "recovered_canary"
	case "abort":
		return "aborted_canary"
	default:
		return "canary"
	}
}

func executeSandboxCanaryPhase(
	ctx context.Context,
	pool *pgxpool.Pool,
	store *postgresstore.V1CDispatcherStore,
	runtimeConfig config.Runtime,
	product config.Configuration,
	configurationID string,
	exchange sandbox.Exchange,
	command Command,
) (string, error) {
	switch command.Phase {
	case "prepare":
		return executeSandboxCanaryPreparePhase(
			ctx, pool, store, runtimeConfig, product,
			configurationID, exchange, command,
		)
	case "verify":
		return verifySandboxCanary(
			ctx,
			store,
			product,
			configurationID,
			exchange,
			command.CanaryID,
			command.EvidenceDirectory,
		)
	case "recover":
		return recoverSandboxCanaryPrepare(
			ctx,
			store,
			configurationID,
			exchange,
			command.CanaryID,
		)
	case "abort":
		return abortSandboxCanary(
			ctx,
			store,
			configurationID,
			exchange,
			command.CanaryID,
		)
	default:
		return "", fmt.Errorf("sandbox_canary_phase_invalid")
	}
}

func executeSandboxCanaryPreparePhase(
	ctx context.Context,
	pool *pgxpool.Pool,
	store *postgresstore.V1CDispatcherStore,
	runtimeConfig config.Runtime,
	product config.Configuration,
	configurationID string,
	exchange sandbox.Exchange,
	command Command,
) (string, error) {
	return prepareSandboxCanary(
		ctx,
		pool,
		store,
		runtimeConfig,
		product,
		configurationID,
		exchange,
		command.InputFile,
	)
}

func readSandboxCanaryRequest(path string) (sandboxCanaryRequest, error) {
	raw, err := security.ReadSecretFile(path)
	if err != nil {
		return sandboxCanaryRequest{}, fmt.Errorf("sandbox_canary_input_unavailable")
	}
	defer func() { raw = "" }()
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	var request sandboxCanaryRequest
	if decoder.Decode(&request) != nil {
		return sandboxCanaryRequest{}, fmt.Errorf("sandbox_canary_input_invalid")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return sandboxCanaryRequest{}, fmt.Errorf("sandbox_canary_input_invalid")
	}
	if strings.TrimSpace(request.Email) == "" ||
		request.Password == "" || len(request.TOTP) != 6 ||
		strings.TrimSpace(request.Reason) == "" ||
		request.Side != string(domain.SideBuy) {
		return sandboxCanaryRequest{}, fmt.Errorf("sandbox_canary_input_invalid")
	}
	return request, nil
}

func canaryInstrument(
	product config.Configuration,
	symbol string,
) (domain.Instrument, bool) {
	for _, candidate := range product.Instruments {
		if candidate.Base+candidate.Quote != symbol ||
			candidate.Product != string(domain.ProductSpot) {
			continue
		}
		instrument, err := domain.NewSpotInstrument(
			domain.AssetSymbol(candidate.Base),
			domain.AssetSymbol(candidate.Quote),
		)
		if err != nil || instrument.Quote != "USDT" {
			return domain.Instrument{}, false
		}
		baseApproved, quoteApproved := false, false
		for _, asset := range product.Assets {
			if asset.Symbol == instrument.Base &&
				asset.Status == domain.AssetApproved {
				baseApproved = true
			}
			if asset.Symbol == instrument.Quote &&
				asset.Status == domain.AssetApproved {
				quoteApproved = true
			}
		}
		return instrument, baseApproved && quoteApproved
	}
	return domain.Instrument{}, false
}

func canarySwitches(
	product config.Configuration,
	exchange sandbox.Exchange,
) ([4]bool, bool) {
	switches := [4]bool{
		product.Sandbox.IntegrationsEnabled,
		product.Sandbox.SubmissionEnabled,
		false,
		false,
	}
	for _, candidate := range product.Sandbox.Exchanges {
		if candidate.ID == string(exchange) {
			switches[2] = candidate.IntegrationEnabled
			switches[3] = candidate.SubmissionEnabled
			break
		}
	}
	return switches, switches[0] && switches[1] && switches[2] && switches[3]
}

func randomCanaryIdentifier(prefix string) (string, error) {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("sandbox_canary_identity_failed")
	}
	return prefix + "-" + hex.EncodeToString(value), nil
}

func canaryHash(values ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(digest[:])
}

func totpSeedPath(product config.Configuration) (string, error) {
	name := product.Sandbox.SecretFileEnvironment.TOTPSeedFile
	path := os.Getenv(name)
	if name != "AXIOM_TOTP_SEED_FILE" || path == "" {
		return "", fmt.Errorf("sandbox_canary_totp_unavailable")
	}
	return path, nil
}
