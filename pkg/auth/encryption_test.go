package auth

import "testing"

func TestEncryptor_RoundTrip(t *testing.T) {
	enc, err := NewEncryptor(DeriveKey("test-passphrase"))
	if err != nil {
		t.Fatalf("new encryptor: %v", err)
	}
	ciphertext, err := enc.Encrypt("ghp_super_secret_token")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if ciphertext == "ghp_super_secret_token" {
		t.Fatal("ciphertext must not equal plaintext")
	}
	plaintext, err := enc.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if plaintext != "ghp_super_secret_token" {
		t.Fatalf("expected round-trip plaintext, got %q", plaintext)
	}
}

func TestEncryptor_DecryptWrongKeyFails(t *testing.T) {
	enc1, _ := NewEncryptor(DeriveKey("key-one"))
	enc2, _ := NewEncryptor(DeriveKey("key-two"))

	ciphertext, err := enc1.Encrypt("secret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := enc2.Decrypt(ciphertext); err != ErrDecryptionFailed {
		t.Fatalf("expected ErrDecryptionFailed, got %v", err)
	}
}

func TestEncryptor_DecryptMalformedInput(t *testing.T) {
	enc, _ := NewEncryptor(DeriveKey("key"))

	cases := []string{
		"not-base64!!!",
		"",
		"c2hvcnQ=", // valid base64, but shorter than the GCM nonce size
	}
	for _, c := range cases {
		if _, err := enc.Decrypt(c); err != ErrDecryptionFailed {
			t.Errorf("Decrypt(%q): expected ErrDecryptionFailed, got %v", c, err)
		}
	}
}

func TestEncryptor_EncryptProducesUniqueCiphertexts(t *testing.T) {
	enc, _ := NewEncryptor(DeriveKey("key"))
	a, _ := enc.Encrypt("same-plaintext")
	b, _ := enc.Encrypt("same-plaintext")
	if a == b {
		t.Fatal("expected distinct ciphertexts due to random nonce")
	}
}

func TestNewEncryptor_InvalidKeySize(t *testing.T) {
	if _, err := NewEncryptor([]byte("too-short")); err == nil {
		t.Fatal("expected error for non-32-byte key")
	}
}

func TestDeriveKey_Is32Bytes(t *testing.T) {
	if len(DeriveKey("anything")) != 32 {
		t.Fatalf("expected 32-byte derived key, got %d", len(DeriveKey("anything")))
	}
}
