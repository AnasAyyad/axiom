package binance

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/sandbox"
)

// Authenticated Binance Testnet failures are generic and redact request data.
var (
	ErrSandboxStartupIdentity = errors.New("binance_testnet_identity_rejected")
	ErrSandboxRequest         = errors.New("binance_testnet_request_failed")
)

const authenticatedResponseLimit = 1 << 20

type sandboxDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// SandboxClient is an authenticated Binance Spot Testnet client with no
// configurable host, method, path, header, or generic request entrypoint.
type SandboxClient struct {
	doer            sandboxDoer
	apiKey          string
	apiSecret       string
	evidence        exchangecontracts.AuthenticatedEvidenceSink
	configurationID string
	now             func() time.Time
}

// BinanceTestnetAttestation is supplied by the owner before a key is enabled.
type BinanceTestnetAttestation struct {
	AccountIdentityHash string
	KeyFingerprint      string
	TestnetOnly         bool
}

// NewSandboxClient resolves credentials from the fixed V1C file references and
// sends all HTTPS traffic through the fixed Binance CONNECT proxy.
func NewSandboxClient(
	evidence exchangecontracts.AuthenticatedEvidenceSink,
	configurationID string,
) (*SandboxClient, error) {
	if evidence == nil || configurationID == "" {
		return nil, ErrSandboxRequest
	}
	credentials, err := sandbox.LoadCredentialPair(sandbox.ExchangeBinance)
	if err != nil {
		return nil, err
	}
	proxyURL, err := url.Parse(sandboxProxyOrigin)
	if err != nil {
		return nil, ErrSandboxRequest
	}
	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			ServerName: sandboxRESTHost,
		},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("redirect_rejected")
		},
	}
	return newSandboxClientForTest(client, credentials, evidence, configurationID, time.Now)
}

func newSandboxClientForTest(
	doer sandboxDoer,
	credentials sandbox.CredentialPair,
	evidence exchangecontracts.AuthenticatedEvidenceSink,
	configurationID string,
	now func() time.Time,
) (*SandboxClient, error) {
	if doer == nil || evidence == nil || configurationID == "" || now == nil ||
		credentials.APIKey == "" || credentials.APISecret == "" {
		return nil, ErrSandboxRequest
	}
	return &SandboxClient{
		doer: doer, apiKey: credentials.APIKey, apiSecret: credentials.APISecret,
		evidence: evidence, configurationID: configurationID, now: now,
	}, nil
}

// ValidateStartup verifies Testnet account access, Spot/HMAC capability, stable
// account identity, key fingerprint, and the owner-provided Testnet attestation.
func (client *SandboxClient) ValidateStartup(
	ctx context.Context,
	attestation BinanceTestnetAttestation,
) (sandbox.AccountIdentity, error) {
	fields := url.Values{
		"recvWindow": {sandboxReceiveWindow},
		"timestamp":  {strconv.FormatInt(client.now().UTC().UnixMilli(), 10)},
	}
	body, err := client.execute(ctx, authenticatedAccount, fields)
	if err != nil {
		return sandbox.AccountIdentity{}, err
	}
	var response struct {
		CanTrade    bool        `json:"canTrade"`
		AccountType string      `json:"accountType"`
		Permissions []string    `json:"permissions"`
		UID         json.Number `json:"uid"`
	}
	if err := json.Unmarshal(body, &response); err != nil || !response.CanTrade ||
		response.AccountType != "SPOT" || !containsExact(response.Permissions, "SPOT") ||
		response.UID.String() == "" || !attestation.TestnetOnly {
		return sandbox.AccountIdentity{}, ErrSandboxStartupIdentity
	}
	accountHash := hashString(response.UID.String() + "|" + response.AccountType)
	keyFingerprint := fingerprintString(client.apiKey)
	if !hmac.Equal([]byte(accountHash), []byte(attestation.AccountIdentityHash)) ||
		!hmac.Equal([]byte(keyFingerprint), []byte(attestation.KeyFingerprint)) {
		return sandbox.AccountIdentity{}, ErrSandboxStartupIdentity
	}
	return sandbox.AccountIdentity{
		AccountID: sandbox.AccountID("binance-testnet-" + accountHash[:16]),
		Exchange:  sandbox.ExchangeBinance, Environment: sandbox.EnvironmentBinanceSpotTestnet,
		AccountIdentityHash: accountHash, KeyFingerprint: keyFingerprint,
		CredentialGeneration: 1, OwnerAttested: true, ValidatedAt: client.now().UTC(),
	}, nil
}

type signedRequest struct {
	method string
	path   string
	query  string
	hash   [sha256.Size]byte
	fields []string
	enums  map[string]string
}

func (client *SandboxClient) buildSignedRequest(
	route authenticatedRoute,
	fields url.Values,
) (signedRequest, error) {
	policy, err := validateAuthenticatedFields(route, fields)
	if err != nil {
		return signedRequest{}, err
	}
	canonical := fields.Encode()
	mac := hmac.New(sha256.New, []byte(client.apiSecret))
	if _, err := mac.Write([]byte(canonical)); err != nil {
		return signedRequest{}, ErrSandboxRequest
	}
	signature := hex.EncodeToString(mac.Sum(nil))
	requestHash := sha256.Sum256([]byte(policy.method + "\n" + policy.path + "\n" + canonical))
	enumerated := make(map[string]string)
	for name := range policy.enumerations {
		if value := fields.Get(name); value != "" {
			enumerated[name] = value
		}
	}
	return signedRequest{
		method: policy.method, path: policy.path,
		query: canonical + "&" + sandboxSignatureField + "=" + signature,
		hash:  requestHash, fields: sortedFieldNames(fields), enums: enumerated,
	}, nil
}

func (client *SandboxClient) execute(
	ctx context.Context,
	route authenticatedRoute,
	fields url.Values,
) ([]byte, error) {
	signed, err := client.buildSignedRequest(route, fields)
	if err != nil {
		return nil, err
	}
	evidence := exchangecontracts.AuthenticatedRequestEvidence{
		Exchange: "binance", Host: sandboxRESTHost, Method: signed.method, Path: signed.path,
		FieldNames: signed.fields, Enumerated: signed.enums, RequestHash: signed.hash,
		ConfigurationID: client.configurationID, RecordedAt: client.now().UTC(),
	}
	if err := exchangecontracts.ValidateAuthenticatedRequestEvidence(evidence); err != nil {
		return nil, ErrSandboxRequest
	}
	if err := client.evidence.RecordAuthenticatedRequest(ctx, evidence); err != nil {
		return nil, fmt.Errorf("%w: evidence", ErrSandboxRequest)
	}
	request, err := http.NewRequestWithContext(
		ctx, signed.method, sandboxRESTOrigin+signed.path+"?"+signed.query, nil,
	)
	if err != nil {
		return nil, ErrSandboxRequest
	}
	request.Header.Set("X-MBX-APIKEY", client.apiKey)
	response, err := client.doer.Do(request)
	if err != nil {
		return nil, ErrSandboxRequest
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, ErrSandboxRequest
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, authenticatedResponseLimit+1))
	if err != nil || len(body) > authenticatedResponseLimit {
		return nil, ErrSandboxRequest
	}
	return body, nil
}

func containsExact(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func hashString(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func fingerprintString(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:16])
}
