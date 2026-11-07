// Package secrets encrypts small values at rest with AES-256-GCM.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// Cipher encrypts and decrypts with one key; the key id travels with the ciphertext's owner, not here.
type Cipher struct {
	aead cipher.AEAD
}

// New returns a Cipher from a base64-encoded 32-byte key.
func New(keyBase64 string) (*Cipher, error) {
	key, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil {
		return nil, fmt.Errorf("secrets: key is not base64: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("secrets: key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt returns nonce || ciphertext; a fresh random nonce is used every time.
func (c *Cipher) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return c.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt reverses Encrypt; a wrong key or a tampered value is an error.
func (c *Cipher) Decrypt(ciphertext []byte) ([]byte, error) {
	n := c.aead.NonceSize()
	if len(ciphertext) < n {
		return nil, errors.New("secrets: ciphertext too short")
	}
	plaintext, err := c.aead.Open(nil, ciphertext[:n], ciphertext[n:], nil)
	if err != nil {
		return nil, fmt.Errorf("secrets: decrypt: %w", err)
	}
	return plaintext, nil
}
