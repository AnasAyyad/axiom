package authentication

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"strings"
)

const opaqueTokenBytes = 32

func newOpaqueToken() (string, error) {
	value := make([]byte, opaqueTokenBytes)
	if _, err := rand.Read(value); err != nil {
		return "", ErrConfiguration
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func newIdentifier(prefix string) (string, error) {
	random, err := newOpaqueToken()
	if err != nil {
		return "", err
	}
	return prefix + "-" + random, nil
}

func tokenHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func signedCSRF(sessionID string, key []byte) (string, error) {
	nonce, err := newOpaqueToken()
	if err != nil {
		return "", err
	}
	payload := sessionID + "." + nonce
	signature := hmac.New(sha256.New, key)
	_, _ = signature.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(signature.Sum(nil)), nil
}

func validateSignedCSRF(value, sessionID string, key []byte) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 3 || parts[0] != sessionID {
		return false
	}
	payload := parts[0] + "." + parts[1]
	want := hmac.New(sha256.New, key)
	_, _ = want.Write([]byte(payload))
	actual, err := base64.RawURLEncoding.DecodeString(parts[2])
	return err == nil && subtle.ConstantTimeCompare(actual, want.Sum(nil)) == 1
}
