// Package cryptobox encrypts small application secrets for database storage.
package cryptobox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

// Box encrypts and decrypts values with AES-GCM.
type Box struct {
	aead cipher.AEAD
}

// New derives an AES-256-GCM key from the configured secret.
func New(secret string) (*Box, error) {
	if secret == "" {
		return nil, fmt.Errorf("encryption secret is required")
	}
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("create aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}
	return &Box{aead: aead}, nil
}

// Encrypt returns base64url(nonce || ciphertext).
func (b *Box) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	ciphertext := b.aead.Seal(nil, nonce, []byte(plaintext), nil)
	sealed := append(nonce, ciphertext...)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt.
func (b *Box) Decrypt(value string) (string, error) {
	sealed, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	if len(sealed) < b.aead.NonceSize() {
		return "", fmt.Errorf("ciphertext is too short")
	}
	nonce := sealed[:b.aead.NonceSize()]
	ciphertext := sealed[b.aead.NonceSize():]
	plaintext, err := b.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt ciphertext: %w", err)
	}
	return string(plaintext), nil
}
