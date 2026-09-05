package service

import "testing"

func TestEncryptDecryptRefreshToken_RoundTrips(t *testing.T) {
	ciphertext, err := encryptRefreshToken("test-key", "1/fFAGRNJru1FTz70BzhT3Zg")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if ciphertext == "1/fFAGRNJru1FTz70BzhT3Zg" {
		t.Fatalf("expected ciphertext to differ from plaintext")
	}

	plaintext, err := decryptRefreshToken("test-key", ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if plaintext != "1/fFAGRNJru1FTz70BzhT3Zg" {
		t.Fatalf("expected round-tripped plaintext %q, got %q", "1/fFAGRNJru1FTz70BzhT3Zg", plaintext)
	}
}

func TestEncryptRefreshToken_SameInputProducesDifferentCiphertext(t *testing.T) {
	first, err := encryptRefreshToken("test-key", "same-refresh-token")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	second, err := encryptRefreshToken("test-key", "same-refresh-token")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// A fresh random nonce every call (#285) — otherwise two Connections
	// carrying the same refresh token would be visibly linkable from the
	// ciphertext alone, and GCM's security guarantee assumes a nonce is
	// never reused under one key.
	if first == second {
		t.Fatalf("expected two encryptions of the same plaintext to differ")
	}
}

func TestDecryptRefreshToken_WrongKeyFails(t *testing.T) {
	ciphertext, err := encryptRefreshToken("correct-key", "a-refresh-token")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if _, err := decryptRefreshToken("wrong-key", ciphertext); err == nil {
		t.Fatalf("expected decrypting with the wrong key to fail")
	}
}

func TestDecryptRefreshToken_TamperedCiphertextFails(t *testing.T) {
	ciphertext, err := encryptRefreshToken("test-key", "a-refresh-token")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	tampered := []byte(ciphertext)
	tampered[len(tampered)-1] ^= 0xFF
	if _, err := decryptRefreshToken("test-key", string(tampered)); err == nil {
		t.Fatalf("expected decrypting tampered ciphertext to fail")
	}
}

func TestEncryptDecryptRefreshToken_KeyUnset(t *testing.T) {
	if _, err := encryptRefreshToken("", "a-refresh-token"); err != ErrConnectionsEncryptionKeyUnset {
		t.Fatalf("expected ErrConnectionsEncryptionKeyUnset, got %v", err)
	}
	if _, err := decryptRefreshToken("", "anything"); err != ErrConnectionsEncryptionKeyUnset {
		t.Fatalf("expected ErrConnectionsEncryptionKeyUnset, got %v", err)
	}
}
