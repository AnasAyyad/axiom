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
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/sandbox"
)

// Authenticated Bybit Demo failures are generic and redact request data.
var (
	ErrDemoStartupPermission = errors.New("bybit_demo_permission_rejected")
	ErrDemoRequest           = errors.New("bybit_demo_request_failed")
)

const authenticatedResponseLimit = 1 << 20

type sandboxDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// SandboxClient is an authenticated Bybit Demo client whose endpoint and
// operation surface are compile-time closed.
type SandboxClient struct {
	doer            sandboxDoer
	apiKey          string
	apiSecret       string
	evidence        exchangecontracts.AuthenticatedEvidenceSink
	configurationID string
	now             func() time.Time
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
	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			ServerName: demoRESTHost,
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
		return nil, ErrDemoRequest
	}
	return &SandboxClient{
		doer: doer, apiKey: credentials.APIKey, apiSecret: credentials.APISecret,
		evidence: evidence, configurationID: configurationID, now: now,
	}, nil
}

// ValidateStartup requires one read-write SpotTrade capability and rejects any
// Wallet, Contract, Options, or Derivatives permission.
func (client *SandboxClient) ValidateStartup(ctx context.Context) (sandbox.AccountIdentity, error) {
	body, err := client.execute(ctx, authenticatedKeyInspection, nil)
	if err != nil {
		return sandbox.AccountIdentity{}, err
	}
	var response struct {
		Result struct {
			ID          string              `json:"id"`
			ReadOnly    int                 `json:"readOnly"`
			Permissions map[string][]string `json:"permissions"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.Result.ID == "" ||
		response.Result.ReadOnly != 0 ||
		!containsOnly(response.Result.Permissions["Spot"], "SpotTrade") {
		return sandbox.AccountIdentity{}, ErrDemoStartupPermission
	}
	for category, permissions := range response.Result.Permissions {
		if category != "Spot" && len(permissions) != 0 {
			return sandbox.AccountIdentity{}, ErrDemoStartupPermission
		}
	}
	return sandbox.AccountIdentity{
		AccountID: sandbox.AccountID("bybit-demo-" + hashString(response.Result.ID)[:16]),
		Exchange:  sandbox.ExchangeBybit, Environment: sandbox.EnvironmentBybitDemo,
		AccountIdentityHash:  hashString(response.Result.ID),
		KeyFingerprint:       fingerprintString(client.apiKey),
		CredentialGeneration: 1, ValidatedAt: client.now().UTC(),
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
	timestamp := strconv.FormatInt(client.now().UTC().UnixMilli(), 10)
	var query string
	var body []byte
	var signedPayload string
	if policy.method == http.MethodGet {
		query = fields.Encode()
		signedPayload = query
	} else {
		object := make(map[string]string, len(fields))
		for name := range fields {
			object[name] = fields.Get(name)
		}
		body, err = json.Marshal(object)
		if err != nil {
			return signedRequest{}, ErrDemoRequest
		}
		signedPayload = string(body)
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
	response, err := client.doer.Do(request)
	if err != nil {
		return nil, ErrDemoRequest
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, ErrDemoRequest
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, authenticatedResponseLimit+1))
	if err != nil || len(body) > authenticatedResponseLimit {
		return nil, ErrDemoRequest
	}
	return body, nil
}

func containsOnly(values []string, wanted string) bool {
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
