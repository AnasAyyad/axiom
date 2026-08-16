package bybit

import (
	"bytes"
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
	"sync"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/sandbox"
)

// Authenticated Bybit Demo failures are generic and redact request data.
var (
	ErrDemoStartupPermission = errors.New("bybit_demo_permission_rejected")
	ErrDemoRequest           = errors.New("bybit_demo_request_failed")
	ErrDemoAmbiguous         = errors.New("bybit_demo_request_ambiguous")
	ErrDemoRejected          = errors.New("bybit_demo_request_rejected")
	ErrDemoRateLimited       = errors.New("bybit_demo_rate_limited")
	ErrDemoClockRejected     = errors.New("bybit_demo_clock_rejected")
	ErrDemoOrderNotFound     = errors.New("bybit_demo_order_not_found")
)

const authenticatedResponseLimit = 1 << 20

type sandboxDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// SandboxClient is an authenticated Bybit Demo client whose endpoint and
// operation surface are compile-time closed.
type SandboxClient struct {
	doer             sandboxDoer
	publicDoer       sandboxDoer
	apiKey           string
	apiSecret        string
	evidence         exchangecontracts.AuthenticatedEvidenceSink
	configurationID  string
	now              func() time.Time
	clockMutex       sync.Mutex
	clockOffset      time.Duration
	clockObservedAt  time.Time
	clockValidated   bool
	rateMutex        sync.Mutex
	rateBlockedUntil time.Time
}

// BybitDemoAttestation binds a Demo-only key and independent Demo account to
// an owner-reviewed identity before the engine can start.
type BybitDemoAttestation struct {
	AccountIdentityHash string
	KeyFingerprint      string
	DemoOnly            bool
}

// NewSandboxClient constructs the fixed-host, fixed-proxy Demo client.
func NewSandboxClient(
	evidence exchangecontracts.AuthenticatedEvidenceSink,
	configurationID string,
) (*SandboxClient, error) {
	if evidence == nil || configurationID == "" {
		return nil, ErrDemoRequest
	}
	credentials, err := sandbox.LoadCredentialPair(sandbox.ExchangeBybit)
	if err != nil {
		return nil, err
	}
	proxyURL, err := url.Parse(demoProxyOrigin)
	if err != nil {
		return nil, ErrDemoRequest
	}
	publicProxyURL, err := url.Parse(sandboxPeerPublicProxyOrigin)
	if err != nil {
		return nil, ErrDemoRequest
	}
	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			ServerName: demoRESTHost,
		},
		IdleConnTimeout:     30 * time.Second,
		MaxIdleConns:        4,
		MaxIdleConnsPerHost: 4,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("redirect_rejected")
		},
	}
	publicClient := newSandboxPublicHTTPClient(publicProxyURL)
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
	result.publicDoer = publicClient
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
		return nil, ErrDemoRequest
	}
	current := now().UTC()
	return &SandboxClient{
		doer: doer, publicDoer: doer,
		apiKey: credentials.APIKey, apiSecret: credentials.APISecret,
		evidence: evidence, configurationID: configurationID, now: now,
		clockObservedAt: current, clockValidated: true,
	}, nil
}

// ValidateStartup requires read-write SpotTrade capability and accepts either
// a least-privilege Spot-only key or Bybit Demo's exact UI-coupled Unified
// Trading bundle. The bundle changes only key admission: every executable
// Axiom operation remains constrained by the compiled Spot-only route policy.
func (client *SandboxClient) ValidateStartup(
	ctx context.Context,
	attestation BybitDemoAttestation,
) (sandbox.AccountIdentity, error) {
	body, err := client.execute(ctx, authenticatedKeyInspection, nil)
	if err != nil {
		return sandbox.AccountIdentity{}, err
	}
	response, err := decodeDemoResult[keyInspectionResult](body)
	if err != nil || response.ID == "" || response.UserID == 0 ||
		response.ReadOnly != 0 || response.Secret != "" ||
		response.UTA != 1 ||
		!validDemoKeyPermissions(response.Permissions) ||
		!attestation.DemoOnly {
		return sandbox.AccountIdentity{}, ErrDemoStartupPermission
	}
	accountHash := hashString(strconv.FormatUint(response.UserID, 10) + "|UNIFIED")
	keyFingerprint := fingerprintString(client.apiKey)
	if !hmac.Equal([]byte(accountHash), []byte(attestation.AccountIdentityHash)) ||
		!hmac.Equal([]byte(keyFingerprint), []byte(attestation.KeyFingerprint)) {
		return sandbox.AccountIdentity{}, ErrDemoStartupPermission
	}
	return sandbox.AccountIdentity{
		AccountID: sandbox.AccountID("bybit-demo-" + accountHash[:16]),
		Exchange:  sandbox.ExchangeBybit, Environment: sandbox.EnvironmentBybitDemo,
		AccountIdentityHash: accountHash, KeyFingerprint: keyFingerprint,
		CredentialGeneration: 1, OwnerAttested: true,
		ValidatedAt: client.now().UTC(),
	}, nil
}

type signedRequest struct {
	method  string
	path    string
	query   string
	body    []byte
	hash    [sha256.Size]byte
	fields  []string
	enums   map[string]string
	headers signedHeaders
}

type signedHeaders struct {
	timestamp  string
	recvWindow string
	signature  string
}

func (client *SandboxClient) buildSignedRequest(
	route authenticatedRoute,
	fields url.Values,
) (signedRequest, error) {
	policy, err := validateAuthenticatedFields(route, fields)
	if err != nil {
		return signedRequest{}, err
	}
	timestamp := strconv.FormatInt(
		client.now().UTC().Add(client.demoClockOffset()).UnixMilli(),
		10,
	)
	query, body, signedPayload, err := demoSignedPayload(policy, fields)
	if err != nil {
		return signedRequest{}, err
	}
	signingInput := timestamp + client.apiKey + demoReceiveWindow + signedPayload
	mac := hmac.New(sha256.New, []byte(client.apiSecret))
	if _, err := mac.Write([]byte(signingInput)); err != nil {
		return signedRequest{}, ErrDemoRequest
	}
	requestHash := sha256.Sum256([]byte(
		policy.method + "\n" + policy.path + "\n" + timestamp + "\n" + signedPayload,
	))
	enumerated := make(map[string]string)
	for name := range policy.enumerations {
		if value := fields.Get(name); value != "" {
			enumerated[name] = value
		}
	}
	return signedRequest{
		method: policy.method, path: policy.path, query: query, body: body,
		hash: requestHash, fields: sortedAuthenticatedFieldNames(fields), enums: enumerated,
		headers: signedHeaders{
			timestamp: timestamp, recvWindow: demoReceiveWindow,
			signature: hex.EncodeToString(mac.Sum(nil)),
		},
	}, nil
}

func demoSignedPayload(
	policy authenticatedRoutePolicy,
	fields url.Values,
) (string, []byte, string, error) {
	if policy.method == http.MethodGet {
		query := fields.Encode()
		return query, nil, query, nil
	}
	object := make(map[string]string, len(fields))
	for name := range fields {
		object[name] = fields.Get(name)
	}
	body, err := json.Marshal(object)
	if err != nil {
		return "", nil, "", ErrDemoRequest
	}
	return "", body, string(body), nil
}

func (client *SandboxClient) execute(
	ctx context.Context,
	route authenticatedRoute,
	fields url.Values,
) ([]byte, error) {
	clockRetried := false
	transportRetried := false
	for attempt := 0; attempt < 3; attempt++ {
		body, err := client.executeOnce(ctx, route, fields)
		if errors.Is(err, ErrDemoClockRejected) && !clockRetried {
			clockRetried = true
			client.invalidateDemoClock()
			continue
		}
		if errors.Is(err, ErrDemoRequest) &&
			demoRouteIsReadOnly(route) &&
			!transportRetried {
			transportRetried = true
			client.closeDemoIdleConnections()
			continue
		}
		return body, err
	}
	return nil, ErrDemoRequest
}

func (client *SandboxClient) closeDemoIdleConnections() {
	type idleConnectionCloser interface {
		CloseIdleConnections()
	}
	if closer, ok := client.doer.(idleConnectionCloser); ok {
		closer.CloseIdleConnections()
	}
}

func (client *SandboxClient) executeOnce(
	ctx context.Context,
	route authenticatedRoute,
	fields url.Values,
) ([]byte, error) {
	if err := client.allowDemoRequest(); err != nil {
		return nil, err
	}
	if err := client.ensureDemoClock(ctx); err != nil {
		return nil, err
	}
	signed, err := client.buildSignedRequest(route, fields)
	if err != nil {
		return nil, err
	}
	request, err := client.newDemoHTTPRequest(ctx, signed)
	if err != nil {
		return nil, err
	}
	return client.performDemoHTTPRequest(request, route)
}

func (client *SandboxClient) newDemoHTTPRequest(
	ctx context.Context,
	signed signedRequest,
) (*http.Request, error) {
	evidence := exchangecontracts.AuthenticatedRequestEvidence{
		Exchange: "bybit", Host: demoRESTHost, Method: signed.method, Path: signed.path,
		FieldNames: signed.fields, Enumerated: signed.enums, RequestHash: signed.hash,
		ConfigurationID: client.configurationID, RecordedAt: client.now().UTC(),
	}
	if err := exchangecontracts.ValidateAuthenticatedRequestEvidence(evidence); err != nil {
		return nil, ErrDemoRequest
	}
	if err := client.evidence.RecordAuthenticatedRequest(ctx, evidence); err != nil {
		return nil, fmt.Errorf("%w: evidence", ErrDemoRequest)
	}
	target := demoRESTOrigin + signed.path
	if signed.query != "" {
		target += "?" + signed.query
	}
	request, err := http.NewRequestWithContext(ctx, signed.method, target, bytes.NewReader(signed.body))
	if err != nil {
		return nil, ErrDemoRequest
	}
	request.Header.Set("X-BAPI-API-KEY", client.apiKey)
	request.Header.Set("X-BAPI-TIMESTAMP", signed.headers.timestamp)
	request.Header.Set("X-BAPI-RECV-WINDOW", signed.headers.recvWindow)
	request.Header.Set("X-BAPI-SIGN", signed.headers.signature)
	request.Header.Set("Content-Type", "application/json")
	return request, nil
}

func (client *SandboxClient) performDemoHTTPRequest(
	request *http.Request,
	route authenticatedRoute,
) ([]byte, error) {
	response, err := client.doer.Do(request)
	if err != nil {
		if demoRouteCanChangeOrder(route) {
			return nil, ErrDemoAmbiguous
		}
		return nil, ErrDemoRequest
	}
	defer response.Body.Close()
	client.observeDemoRateLimit(response)
	body, err := io.ReadAll(io.LimitReader(response.Body, authenticatedResponseLimit+1))
	if err != nil || len(body) > authenticatedResponseLimit {
		if demoRouteCanChangeOrder(route) {
			return nil, ErrDemoAmbiguous
		}
		return nil, ErrDemoRequest
	}
	if response.StatusCode < http.StatusOK ||
		response.StatusCode >= http.StatusMultipleChoices {
		return nil, classifyDemoResponse(route, response.StatusCode, body)
	}
	if err = classifyDemoEnvelope(route, body); err != nil {
		if errors.Is(err, ErrDemoRateLimited) {
			client.blockDemoRateLimit(response)
		}
		return nil, err
	}
	return body, nil
}

func hashString(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func fingerprintString(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:16])
}
