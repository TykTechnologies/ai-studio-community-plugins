package storage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
)

const secretEnvelopeScheme = "aes-256-gcm"

// secretEnvelope is the at-rest format for encrypted secrets. Its "enc"
// marker distinguishes it from legacy plaintext Secret JSON.
type secretEnvelope struct {
	Enc  string `json:"enc"`
	V    int    `json:"v"`
	Data string `json:"data"` // base64(nonce || ciphertext)
}

// deriveSecretKey derives a 32-byte AES key from a passphrase.
func deriveSecretKey(passphrase string) []byte {
	sum := sha256.Sum256([]byte(passphrase))
	return sum[:]
}

// encryptSecretPayload encrypts marshaled secret JSON into an envelope.
func encryptSecretPayload(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	sealed := gcm.Seal(nonce, nonce, plaintext, nil)
	return json.Marshal(&secretEnvelope{
		Enc:  secretEnvelopeScheme,
		V:    1,
		Data: base64.StdEncoding.EncodeToString(sealed),
	})
}

// decryptSecretPayload decrypts stored bytes. If the bytes are not an
// encryption envelope (legacy plaintext secrets), they are returned as-is
// with encrypted=false so callers can fall back to plain JSON parsing.
func decryptSecretPayload(key, stored []byte) (plaintext []byte, encrypted bool, err error) {
	var envelope secretEnvelope
	if jsonErr := json.Unmarshal(stored, &envelope); jsonErr != nil || envelope.Enc == "" {
		return stored, false, nil
	}

	if envelope.Enc != secretEnvelopeScheme {
		return nil, true, fmt.Errorf("unsupported secret encryption scheme: %s", envelope.Enc)
	}
	if key == nil {
		return nil, true, fmt.Errorf("secret is encrypted but no encryption key is configured")
	}

	sealed, err := base64.StdEncoding.DecodeString(envelope.Data)
	if err != nil {
		return nil, true, fmt.Errorf("failed to decode secret payload: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, true, fmt.Errorf("failed to create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, true, fmt.Errorf("failed to create GCM: %w", err)
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, true, fmt.Errorf("encrypted secret payload is truncated")
	}

	nonce, ciphertext := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plaintext, err = gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, true, fmt.Errorf("failed to decrypt secret (wrong encryption key?): %w", err)
	}
	return plaintext, true, nil
}
