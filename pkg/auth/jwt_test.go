package auth

import (
	"testing"
	"time"
)

func TestTokenIssuer_IssueAndParse(t *testing.T) {
	issuer, err := NewTokenIssuer([]byte("test-secret"), time.Hour)
	if err != nil {
		t.Fatalf("new issuer: %v", err)
	}

	tok, err := issuer.Issue("user-123", true)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if tok == "" {
		t.Fatal("expected non-empty token")
	}

	claims, err := issuer.Parse(tok)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.UserID != "user-123" || !claims.IsGlobalAdmin {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestTokenIssuer_ExpiredToken(t *testing.T) {
	issuer, err := NewTokenIssuer([]byte("secret"), time.Millisecond)
	if err != nil {
		t.Fatalf("new issuer: %v", err)
	}
	tok, err := issuer.Issue("u1", false)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := issuer.Parse(tok); err != ErrInvalidToken {
		t.Fatalf("expected ErrInvalidToken for expired token, got %v", err)
	}
}

func TestTokenIssuer_WrongSecret(t *testing.T) {
	issuerA, _ := NewTokenIssuer([]byte("secret-a"), time.Hour)
	issuerB, _ := NewTokenIssuer([]byte("secret-b"), time.Hour)

	tok, err := issuerA.Issue("u1", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := issuerB.Parse(tok); err != ErrInvalidToken {
		t.Fatalf("expected ErrInvalidToken with mismatched secret, got %v", err)
	}
}

func TestTokenIssuer_MalformedToken(t *testing.T) {
	issuer, _ := NewTokenIssuer([]byte("secret"), time.Hour)
	if _, err := issuer.Parse("not-a-jwt"); err != ErrInvalidToken {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}

func TestNewTokenIssuer_EmptySecret(t *testing.T) {
	if _, err := NewTokenIssuer(nil, time.Hour); err == nil {
		t.Fatal("expected error for empty secret")
	}
}

func TestNewTokenIssuer_DefaultTTL(t *testing.T) {
	issuer, err := NewTokenIssuer([]byte("s"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if issuer.ttl != 24*time.Hour {
		t.Fatalf("expected default ttl of 24h, got %v", issuer.ttl)
	}
}
