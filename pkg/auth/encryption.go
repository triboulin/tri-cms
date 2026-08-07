package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// ErrDecryptionFailed indicates a ciphertext could not be decrypted with the
// configured key (wrong key, or corrupted/tampered data).
var ErrDecryptionFailed = errors.New("auth: decryption failed")

// Encryptor performs symmetric AES-256-GCM encryption for secrets that must
// be stored at rest but later read back in plaintext -- unlike password
// hashing (one-way, see password.go), credentials such as a GitHub PAT used
// by a webhooks.KindGitHubDispatch delivery must be recoverable to be used.
type Encryptor struct {
	gcm cipher.AEAD
}

// NewEncryptor builds an Encryptor from a 32-byte AES-256 key. Use
// DeriveKey to turn an arbitrary-length passphrase (e.g. an env var) into a
// valid 32-byte key.
func NewEncryptor(key []byte) (*Encryptor, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("auth: init cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("auth: init gcm: %w", err)
	}
	return &Encryptor{gcm: gcm}, nil
}

// Encrypt returns a base64-encoded nonce+ciphertext+tag, safe to persist as
// a TEXT column.
func (e *Encryptor) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, e.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("auth: generate nonce: %w", err)
	}
	sealed := e.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt. Returns ErrDecryptionFailed if the input is
// malformed or wasn't produced with the same key (wrong key, tampering, or
// truncation are indistinguishable here by design -- GCM authentication
// doesn't tell us which).
func (e *Encryptor) Decrypt(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", ErrDecryptionFailed
	}
	nonceSize := e.gcm.NonceSize()
	if len(raw) < nonceSize {
		return "", ErrDecryptionFailed
	}
	nonce, ciphertext := raw[:nonceSize], raw[nonceSize:]
	plaintext, err := e.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", ErrDecryptionFailed
	}
	return string(plaintext), nil
}

// DeriveKey stretches an arbitrary-length secret (e.g. TRICMS_ENCRYPTION_KEY,
// which need not be exactly 32 bytes) into a 32-byte AES-256 key via
// SHA-256, so operators can set a passphrase rather than a precise hex key.
func DeriveKey(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

// DeriveJWTSecret derives the JWT session-signing key from the same
// passphrase as DeriveKey (TRICMS_ENCRYPTION_KEY), so operators only manage
// one secret. A distinct domain-separation label keeps the AES key and the
// HMAC key from ever being identical byte-for-byte.
func DeriveJWTSecret(secret string) []byte {
	sum := sha256.Sum256([]byte("tricms-jwt-signing:" + secret))
	return sum[:]
}
