package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// GenerateAPIToken returns a new high-entropy plaintext API token. Per spec
// §2.1, the plaintext is shown to the user only once; only HashAPIToken's
// output is persisted (api_tokens.token_hash).
func GenerateAPIToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: generate token: %w", err)
	}
	return "trk_" + hex.EncodeToString(buf), nil
}

// HashAPIToken deterministically hashes a plaintext API token for storage
// and lookup. Unlike passwords, API tokens are already high-entropy random
// strings, so a fast, deterministic hash (rather than bcrypt/argon2) is
// appropriate here: it allows an indexed lookup by hash instead of an O(n)
// scan comparing against every stored hash.
func HashAPIToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}
