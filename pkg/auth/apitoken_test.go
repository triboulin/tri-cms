package auth

import "testing"

func TestGenerateAndHashAPIToken(t *testing.T) {
	tok1, err := GenerateAPIToken()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	tok2, err := GenerateAPIToken()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if tok1 == tok2 {
		t.Fatal("expected distinct tokens")
	}
	if len(tok1) < 32 {
		t.Fatalf("expected long token, got %q", tok1)
	}

	h1 := HashAPIToken(tok1)
	h2 := HashAPIToken(tok1)
	if h1 != h2 {
		t.Fatal("expected deterministic hash")
	}
	if h1 == tok1 {
		t.Fatal("hash must differ from plaintext")
	}
	if HashAPIToken(tok2) == h1 {
		t.Fatal("expected different hashes for different tokens")
	}
}
