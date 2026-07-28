package authentication

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"strconv"
	"strings"
	"time"
)

const (
	totpDigits      = 6
	totpModulo      = uint32(1_000_000)
	minimumTOTPSeed = 20
)

func decodeTOTPSeed(encoded string) ([]byte, error) {
	canonical := strings.ToUpper(strings.TrimSpace(encoded))
	canonical = strings.TrimRight(canonical, "=")
	seed, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(canonical)
	if err != nil || len(seed) < minimumTOTPSeed {
		return nil, ErrConfiguration
	}
	return seed, nil
}

func validateTOTP(seed []byte, code string, now time.Time, window int64) (int64, bool) {
	if len(code) != totpDigits || window < 0 {
		return 0, false
	}
	if _, err := strconv.ParseUint(code, 10, 32); err != nil {
		return 0, false
	}
	current := now.UTC().Unix() / int64(TOTPStep/time.Second)
	for offset := -window; offset <= window; offset++ {
		counter := current + offset
		if counter < 0 {
			continue
		}
		want := totpCode(seed, uint64(counter))
		if hmac.Equal([]byte(want), []byte(code)) {
			return counter, true
		}
	}
	return 0, false
}

func totpCode(seed []byte, counter uint64) string {
	var value [8]byte
	binary.BigEndian.PutUint64(value[:], counter)
	mac := hmac.New(sha1.New, seed)
	_, _ = mac.Write(value[:])
	digest := mac.Sum(nil)
	offset := digest[len(digest)-1] & 0x0f
	number := binary.BigEndian.Uint32(digest[offset:offset+4]) & 0x7fffffff
	return fmtSixDigits(number % totpModulo)
}

func fmtSixDigits(value uint32) string {
	encoded := strconv.FormatUint(uint64(value), 10)
	return strings.Repeat("0", totpDigits-len(encoded)) + encoded
}
