// Package auth implements password hashing, JWT session tokens, and RBAC
// helpers enforcing the role hierarchy described in the spec:
// REDACTEUR ⊂ GESTIONNAIRE ⊂ CONCEPTEUR, with ADMIN as a global bypass.
package auth

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// ErrInvalidCredentials is returned when a password does not match its hash.
var ErrInvalidCredentials = errors.New("auth: invalid credentials")

// DefaultBcryptCost balances security and login latency. Kept a package
// variable (not const) so tests can lower it to keep the suite fast.
var DefaultBcryptCost = bcrypt.DefaultCost

// HashPassword hashes a plaintext password using bcrypt (a slow hash,
// as required by the spec — never MD5/SHA alone).
func HashPassword(plaintext string) (string, error) {
	if len(plaintext) == 0 {
		return "", errors.New("auth: password must not be empty")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), DefaultBcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// VerifyPassword compares a plaintext password against a bcrypt hash.
// Returns ErrInvalidCredentials (not the raw bcrypt error) on mismatch,
// so callers can't accidentally leak hash-format details.
func VerifyPassword(hash, plaintext string) error {
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext)); err != nil {
		return ErrInvalidCredentials
	}
	return nil
}
