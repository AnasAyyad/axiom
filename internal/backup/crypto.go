package backup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
)

const (
	chunkBytes      = 1024 * 1024
	backupMagic     = "AXIOMBKP1"
	noncePrefixSize = 12
)

var header = []byte(backupMagic)

// DecodeKey accepts exactly one base64-encoded random 256-bit key.
func DecodeKey(value string) ([32]byte, error) {
	decoded, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil || len(decoded) != 32 {
		return [32]byte{}, fmt.Errorf("backup_key_invalid")
	}
	var key [32]byte
	copy(key[:], decoded)
	return key, nil
}

// Encrypt authenticates and streams framed AES-256-GCM chunks.
func Encrypt(destination io.Writer, source io.Reader, key [32]byte) error {
	aead, err := newAEAD(key)
	if err != nil {
		return err
	}
	prefix := make([]byte, noncePrefixSize)
	if _, err = io.ReadFull(rand.Reader, prefix); err != nil {
		return fmt.Errorf("backup_random_unavailable")
	}
	if _, err = destination.Write(append(append([]byte(nil), header...), prefix...)); err != nil {
		return fmt.Errorf("backup_write_failed")
	}
	buffer := make([]byte, chunkBytes)
	var counter uint32
	for {
		count, readErr := io.ReadFull(source, buffer)
		if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
			return fmt.Errorf("backup_read_failed")
		}
		if count > 0 {
			if err = writeFrame(destination, aead, prefix, counter, buffer[:count]); err != nil {
				return err
			}
			if counter == ^uint32(0) {
				return fmt.Errorf("backup_size_exhausted")
			}
			counter++
		}
		if readErr == io.ErrUnexpectedEOF || readErr == io.EOF {
			break
		}
	}
	return writeFrame(destination, aead, prefix, counter, nil)
}

// Decrypt verifies every frame and the mandatory authenticated terminal marker.
func Decrypt(destination io.Writer, source io.Reader, key [32]byte) error {
	aead, err := newAEAD(key)
	if err != nil {
		return err
	}
	preamble := make([]byte, len(header)+noncePrefixSize)
	if _, err = io.ReadFull(source, preamble); err != nil || string(preamble[:len(header)]) != backupMagic {
		return fmt.Errorf("backup_header_invalid")
	}
	prefix := preamble[len(header):]
	for counter := uint32(0); ; counter++ {
		plaintext, terminal, frameErr := readFrame(source, aead, prefix, counter)
		if frameErr != nil {
			return frameErr
		}
		if terminal {
			var extra [1]byte
			if count, readErr := source.Read(extra[:]); count != 0 || readErr != io.EOF {
				return fmt.Errorf("backup_trailing_data")
			}
			return nil
		}
		if _, err = destination.Write(plaintext); err != nil {
			return fmt.Errorf("restore_write_failed")
		}
		if counter == ^uint32(0) {
			return fmt.Errorf("backup_size_exhausted")
		}
	}
}

func newAEAD(key [32]byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("backup_cipher_unavailable")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("backup_cipher_unavailable")
	}
	return aead, nil
}

func writeFrame(destination io.Writer, aead cipher.AEAD, prefix []byte, counter uint32, plaintext []byte) error {
	nonce := frameNonce(prefix, counter)
	ciphertext := aead.Seal(nil, nonce, plaintext, frameAAD(counter))
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(ciphertext)))
	if _, err := destination.Write(length[:]); err != nil {
		return fmt.Errorf("backup_write_failed")
	}
	if _, err := destination.Write(ciphertext); err != nil {
		return fmt.Errorf("backup_write_failed")
	}
	return nil
}

func readFrame(source io.Reader, aead cipher.AEAD, prefix []byte, counter uint32) ([]byte, bool, error) {
	var length [4]byte
	if _, err := io.ReadFull(source, length[:]); err != nil {
		return nil, false, fmt.Errorf("backup_truncated")
	}
	size := binary.BigEndian.Uint32(length[:])
	if size < uint32(aead.Overhead()) || size > chunkBytes+uint32(aead.Overhead()) {
		return nil, false, fmt.Errorf("backup_frame_invalid")
	}
	ciphertext := make([]byte, size)
	if _, err := io.ReadFull(source, ciphertext); err != nil {
		return nil, false, fmt.Errorf("backup_truncated")
	}
	plaintext, err := aead.Open(nil, frameNonce(prefix, counter), ciphertext, frameAAD(counter))
	if err != nil {
		return nil, false, fmt.Errorf("backup_authentication_failed")
	}
	return plaintext, len(plaintext) == 0, nil
}

func frameNonce(prefix []byte, counter uint32) []byte {
	nonce := make([]byte, noncePrefixSize)
	copy(nonce, prefix)
	base := binary.BigEndian.Uint32(nonce[noncePrefixSize-4:])
	binary.BigEndian.PutUint32(nonce[noncePrefixSize-4:], base^counter)
	return nonce
}

func frameAAD(counter uint32) []byte {
	aad := make([]byte, len(header)+4)
	copy(aad, header)
	binary.BigEndian.PutUint32(aad[len(header):], counter)
	return aad
}
