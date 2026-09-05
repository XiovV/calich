// connection_crypto.go encrypts a Connection's refresh token at rest with a
// key from the environment (#285, ADR-0052) — never stored in DATA_DIR,
// since the deployment story instructs self-hosters to mount and back up
// that directory, making a stray backup tarball the realistic exposure path
// for a standing grant against an entire Google account. Losing the key
// costs re-authorization, not data.
package service

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

// ErrConnectionsEncryptionKeyUnset is returned by encryptRefreshToken/
// decryptRefreshToken when no CONNECTIONS_ENCRYPTION_KEY was configured —
// unreachable in production, where NewConnectionService is only ever built
// behind config.GoogleConfigured (which requires the key), but guarded here
// too rather than trusted to the caller.
var ErrConnectionsEncryptionKeyUnset = errors.New("connections encryption key is not configured")

// deriveConnectionsEncryptionKey turns the operator-supplied
// CONNECTIONS_ENCRYPTION_KEY — an arbitrary string, not necessarily 32 raw
// bytes — into an AES-256 key via SHA-256. A fixed domain-separation prefix
// keeps this key independent of any other secret ConnectionsEncryptionKey
// might one day derive (e.g. the OAuth "state" signing key), so reusing the
// same raw material for both never reuses the same key.
func deriveConnectionsEncryptionKey(raw string) [32]byte {
	return sha256.Sum256([]byte("calich:connections:refresh-token:" + raw))
}

// encryptRefreshToken seals plaintext (the raw OAuth refresh token) with
// AES-256-GCM under a key derived from rawKey, returning base64 of
// nonce||ciphertext. rawKey is config.Config.ConnectionsEncryptionKey — empty
// only if a caller built a ConnectionService without going through
// config.GoogleConfigured, which NewConnectionService never allows.
func encryptRefreshToken(rawKey, plaintext string) (string, error) {
	if rawKey == "" {
		return "", ErrConnectionsEncryptionKeyUnset
	}

	key := deriveConnectionsEncryptionKey(rawKey)
	gcm, err := newGCM(key[:])
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// decryptRefreshToken reverses encryptRefreshToken. A wrong key or a
// tampered/corrupt ciphertext both surface as the same opaque error — GCM's
// authentication tag makes them indistinguishable, which is the property
// that matters here.
func decryptRefreshToken(rawKey, encoded string) (string, error) {
	if rawKey == "" {
		return "", ErrConnectionsEncryptionKeyUnset
	}

	key := deriveConnectionsEncryptionKey(rawKey)
	gcm, err := newGCM(key[:])
	if err != nil {
		return "", err
	}

	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode refresh token ciphertext: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(sealed) < nonceSize {
		return "", errors.New("refresh token ciphertext is too short")
	}
	nonce, ciphertext := sealed[:nonceSize], sealed[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt refresh token: %w", err)
	}
	return string(plaintext), nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("build aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("build gcm: %w", err)
	}
	return gcm, nil
}
