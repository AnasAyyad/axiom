package backup

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestEncryptedBackupRoundTripAcrossChunks(t *testing.T) {
	key := testKey(t)
	source := bytes.Repeat([]byte("axiom-backup-fixture"), 120000)
	var encrypted bytes.Buffer
	if err := Encrypt(&encrypted, bytes.NewReader(source), key); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted.Bytes(), []byte("axiom-backup-fixture")) {
		t.Fatal("plaintext remained visible in encrypted artifact")
	}
	var restored bytes.Buffer
	if err := Decrypt(&restored, bytes.NewReader(encrypted.Bytes()), key); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored.Bytes(), source) {
		t.Fatal("restored content differs")
	}
}

func TestEncryptedBackupRejectsMutationTruncationAndTrailingData(t *testing.T) {
	key := testKey(t)
	var encrypted bytes.Buffer
	_ = Encrypt(&encrypted, bytes.NewReader([]byte("database dump")), key)
	original := encrypted.Bytes()
	mutated := append([]byte(nil), original...)
	mutated[len(mutated)/2] ^= 1
	for name, payload := range map[string][]byte{
		"mutation":   mutated,
		"truncation": original[:len(original)-1],
		"trailing":   append(append([]byte(nil), original...), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if err := Decrypt(new(bytes.Buffer), bytes.NewReader(payload), key); err == nil {
				t.Fatal("unsafe backup accepted")
			}
		})
	}
}

func TestDecodeKeyRequiresExactRandomKeyMaterial(t *testing.T) {
	valid := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	if _, err := DecodeKey(valid); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", "CHANGE_ME", base64.StdEncoding.EncodeToString([]byte("short"))} {
		if _, err := DecodeKey(value); err == nil {
			t.Fatalf("invalid key accepted: %q", value)
		}
	}
}

func testKey(t *testing.T) [32]byte {
	t.Helper()
	encoded := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{42}, 32))
	key, err := DecodeKey(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
