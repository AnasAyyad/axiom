package console

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"strings"
)

// CursorCodec signs opaque stable list positions so clients cannot alter sort state.
type CursorCodec struct{ key []byte }

// NewCursorCodec constructs a cursor signer from an independent file-backed key.
func NewCursorCodec(key []byte) (CursorCodec, error) {
	if len(key) < 32 {
		return CursorCodec{}, ErrUnavailable
	}
	return CursorCodec{key: append([]byte(nil), key...)}, nil
}

// Encode signs one non-sensitive stable database position.
func (codec CursorCodec) Encode(scope, position string) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(scope + "\x00" + position))
	signature := hmac.New(sha256.New, codec.key)
	_, _ = signature.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(signature.Sum(nil))
}

// Decode validates and returns the position for one expected list scope.
func (codec CursorCodec) Decode(scope, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if len(value) < 16 || len(value) > 1024 {
		return "", ErrInvalidRequest
	}
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return "", ErrInvalidRequest
	}
	want := hmac.New(sha256.New, codec.key)
	_, _ = want.Write([]byte(parts[0]))
	actual, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || subtle.ConstantTimeCompare(actual, want.Sum(nil)) != 1 {
		return "", ErrInvalidRequest
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	fields := strings.Split(string(payload), "\x00")
	if err != nil || len(fields) != 2 || fields[0] != scope || fields[1] == "" {
		return "", ErrInvalidRequest
	}
	return fields[1], nil
}
