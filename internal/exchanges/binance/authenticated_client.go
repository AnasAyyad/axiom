package binance

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/sandbox"
)

// Authenticated Binance Testnet failures are generic and redact request data.
var (
	ErrSandboxStartupIdentity = errors.New("binance_testnet_identity_rejected")
	ErrSandboxRequest         = errors.New("binance_testnet_request_failed")
	ErrSandboxAmbiguous       = errors.New("binance_testnet_request_ambiguous")
	ErrSandboxRejected        = errors.New("binance_testnet_request_rejected")
	ErrSandboxTimestamp       = errors.New("binance_testnet_timestamp_rejected")
	ErrSandboxRateLimited     = errors.New("binance_testnet_rate_limited")
	ErrSandboxOrderNotFound   = errors.New("binance_testnet_order_not_found")
)

const authenticatedResponseLimit = 1 << 20

type sandboxDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// SandboxClient is an authenticated Binance Spot Testnet client with no
// configurable host, method, path, header, or generic request entrypoint.
type SandboxClient struct {
	doer             sandboxDoer
	apiKey           string
	apiSecret        string
	evidence         exchangecontracts.AuthenticatedEvidenceSink
	configurationID  string
	now              func() time.Time
	clock            *TimeSynchronizer
	clockMutex       sync.Mutex
	clockValidated   bool
	rateMutex        sync.Mutex
	rateBlockedUntil time.Time
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
	result, err := newSandboxClientForTest(
		client,
		credentials,
		evidence,
		configurationID,
		time.Now,
	)
	if err != nil {
		return nil, err
	}
	result.clockValidated = false
	return result, nil
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
	clock, err := NewTimeSynchronizer(250 * time.Millisecond)
	if err != nil {
		return nil, ErrSandboxRequest
	}
	current := now().UTC()
	if err = clock.Observe(current, current, current, 0, 0); err != nil {
		return nil, ErrSandboxRequest
	}
	return &SandboxClient{
		doer: doer, apiKey: credentials.APIKey, apiSecret: credentials.APISecret,
		evidence: evidence, configurationID: configurationID, now: now,
		clock: clock, clockValidated: true,
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
	var response sandboxAccountPayload
	if err := strictDecode(body, &response); err != nil || !response.CanTrade ||
		response.AccountType != "SPOT" || !containsOnlyExact(response.Permissions, "SPOT") ||
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
	signature, err := hmacSHA256Hex(client.apiSecret, canonical)
	if err != nil {
		return signedRequest{}, ErrSandboxRequest
	}
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
	body, err := client.executeOnce(ctx, route, fields)
	if !errors.Is(err, ErrSandboxTimestamp) || routeCanChangeOrder(route) {
		return body, err
	}
	client.invalidateClock()
	return client.executeOnce(ctx, route, fields)
}

func (client *SandboxClient) executeOnce(
	ctx context.Context,
	route authenticatedRoute,
	fields url.Values,
) ([]byte, error) {
	if err := client.allowSandboxRequest(); err != nil {
		return nil, fmt.Errorf("%w: request_gate", err)
	}
	if err := client.ensureClock(ctx); err != nil {
		return nil, fmt.Errorf("%w: clock_sync", err)
	}
	client.addRequestTime(fields)
	signed, err := client.buildSignedRequest(route, fields)
	if err != nil {
		return nil, fmt.Errorf("%w: request_policy", err)
	}
	request, err := client.newSandboxHTTPRequest(ctx, signed)
	if err != nil {
		return nil, fmt.Errorf("%w: request_evidence", err)
	}
	body, err := client.performSandboxHTTPRequest(request, route)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: route_%s",
			err,
			authenticatedRouteName(route),
		)
	}
	return body, nil
}

func (client *SandboxClient) newSandboxHTTPRequest(
	ctx context.Context,
	signed signedRequest,
) (*http.Request, error) {
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
	return request, nil
}

func (client *SandboxClient) performSandboxHTTPRequest(
	request *http.Request,
	route authenticatedRoute,
) ([]byte, error) {
	response, err := client.doer.Do(request)
	if err != nil {
		if routeCanChangeOrder(route) {
			return nil, ErrSandboxAmbiguous
		}
		return nil, ErrSandboxRequest
	}
	defer response.Body.Close()
	client.observeSandboxRateLimit(response)
	body, err := io.ReadAll(io.LimitReader(response.Body, authenticatedResponseLimit+1))
	if err != nil || len(body) > authenticatedResponseLimit {
		if routeCanChangeOrder(route) {
			return nil, ErrSandboxAmbiguous
		}
		return nil, ErrSandboxRequest
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, classifySandboxResponse(route, response.StatusCode, body)
	}
	return body, nil
}

func (client *SandboxClient) allowSandboxRequest() error {
	now := client.now().UTC()
	client.rateMutex.Lock()
	defer client.rateMutex.Unlock()
	if !client.rateBlockedUntil.IsZero() && now.Before(client.rateBlockedUntil) {
		return ErrSandboxRateLimited
	}
	if !client.rateBlockedUntil.IsZero() {
		client.rateBlockedUntil = time.Time{}
	}
	return nil
}

func (client *SandboxClient) observeSandboxRateLimit(response *http.Response) {
	if response == nil ||
		(response.StatusCode != http.StatusTooManyRequests &&
			response.StatusCode != http.StatusTeapot) {
		return
	}
	wait := 72 * time.Hour
	if seconds, err := strconv.ParseUint(response.Header.Get("Retry-After"), 10, 32); err == nil &&
		seconds > 0 {
		wait = time.Duration(seconds) * time.Second
	}
	blockedUntil := client.now().UTC().Add(wait)
	client.rateMutex.Lock()
	if blockedUntil.After(client.rateBlockedUntil) {
		client.rateBlockedUntil = blockedUntil
	}
	client.rateMutex.Unlock()
}

func routeCanChangeOrder(route authenticatedRoute) bool {
	return route == authenticatedCreate || route == authenticatedCancel
}

func containsExact(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func containsOnlyExact(values []string, wanted string) bool {
	return len(values) == 1 && values[0] == wanted
}

func hashString(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func fingerprintString(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:16])
}

func hmacSHA256Hex(secret, payload string) (string, error) {
	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := mac.Write([]byte(payload)); err != nil {
		return "", ErrSandboxRequest
	}
	return hex.EncodeToString(mac.Sum(nil)), nil
}
