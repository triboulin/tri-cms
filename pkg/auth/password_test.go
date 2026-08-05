package auth

import "testing"

func TestHashAndVerifyPassword(t *testing.T) {
	old := DefaultBcryptCost
	DefaultBcryptCost = 4 // keep tests fast
	defer func() { DefaultBcryptCost = old }()

	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if hash == "correct horse battery staple" {
		t.Fatal("hash must not equal plaintext")
	}

	if err := VerifyPassword(hash, "correct horse battery staple"); err != nil {
		t.Fatalf("expected password to verify, got %v", err)
	}
	if err := VerifyPassword(hash, "wrong password"); err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestHashPassword_Empty(t *testing.T) {
	if _, err := HashPassword(""); err == nil {
		t.Fatal("expected error for empty password")
	}
}
