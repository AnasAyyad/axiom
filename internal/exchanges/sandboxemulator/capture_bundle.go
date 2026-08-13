package sandboxemulator

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"axiom/internal/sandbox"
)

const captureBundleSchema = "axiom.sandbox.redacted-request-capture.v1"

var (
	captureCommitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	captureHashPattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	captureFieldPattern  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,63}$`)
)

// CaptureBundle is safe to retain because it contains only destinations,
// methods, closed route names, field names, and one-way request hashes.
type CaptureBundle struct {
	SchemaVersion string    `json:"schema_version"`
	SourceSHA     string    `json:"source_sha"`
	Captures      []Capture `json:"captures"`
}

// RedactedCaptureBundle validates and serializes captured request shapes. It
// never serializes headers, credentials, signatures, or field values.
func (emulator *Emulator) RedactedCaptureBundle(sourceSHA string) ([]byte, error) {
	if !captureCommitPattern.MatchString(sourceSHA) || strings.Trim(sourceSHA, "0") == "" {
		return nil, errors.New("capture_source_identity_invalid")
	}
	captures := emulator.Captures()
	if len(captures) == 0 {
		return nil, errors.New("capture_evidence_missing")
	}
	for _, capture := range captures {
		if !validCapture(capture) {
			return nil, errors.New("capture_evidence_invalid")
		}
	}
	payload, err := json.MarshalIndent(CaptureBundle{
		SchemaVersion: captureBundleSchema,
		SourceSHA:     sourceSHA,
		Captures:      captures,
	}, "", "  ")
	if err != nil {
		return nil, errors.New("capture_evidence_encode_failed")
	}
	return append(payload, '\n'), nil
}

func validCapture(capture Capture) bool {
	expectedHost := ""
	switch capture.Exchange {
	case sandbox.ExchangeBinance:
		expectedHost = "testnet.binance.vision"
	case sandbox.ExchangeBybit:
		expectedHost = "api-demo.bybit.com"
	default:
		return false
	}
	if capture.Host != expectedHost || !captureHashPattern.MatchString(capture.RequestHash) ||
		!map[string]bool{"GET": true, "POST": true, "DELETE": true}[capture.Method] ||
		capture.Path == "" || len(capture.FieldNames) == 0 {
		return false
	}
	for _, field := range capture.FieldNames {
		lower := strings.ToLower(field)
		if !captureFieldPattern.MatchString(field) || strings.Contains(lower, "signature") ||
			strings.Contains(lower, "secret") || strings.Contains(lower, "apikey") ||
			strings.Contains(lower, "authorization") {
			return false
		}
	}
	return true
}
