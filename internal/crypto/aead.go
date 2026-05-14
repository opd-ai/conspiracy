// Package crypto provides ChaCha20-Poly1305 AEAD encryption, hybrid nonce construction,
// entropy validation, and anti-replay protection.
package crypto

import (
	"crypto/sha256"
	"fmt"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

// Encrypt encrypts plaintext using ChaCha20-Poly1305 AEAD with provided nonce.
// Returns ciphertext+tag (16-byte Poly1305 MAC appended).
//
// meshKey: 32-byte mesh encryption key
// nonce: 12-byte nonce from NonceGenerator
// plaintext: data to encrypt (typically BEACON payload)
func Encrypt(meshKey []byte, nonce [12]byte, plaintext []byte) ([]byte, error) {
	if len(meshKey) != 32 {
		return nil, fmt.Errorf("invalid mesh key length: %d bytes (must be 32)", len(meshKey))
	}

	// HKDF key derivation from MESH_KEY
	kdf := hkdf.New(sha256.New, meshKey, []byte("conspiracyd-aead-v1"), []byte("beacon-encryption"))
	derivedKey := make([]byte, chacha20poly1305.KeySize)
	if _, err := io.ReadFull(kdf, derivedKey); err != nil {
		return nil, fmt.Errorf("key derivation failed: %w", err)
	}

	// ChaCha20-Poly1305 encryption
	aead, err := chacha20poly1305.New(derivedKey)
	if err != nil {
		return nil, fmt.Errorf("cipher init failed: %w", err)
	}

	ciphertext := aead.Seal(nil, nonce[:], plaintext, nil)
	return ciphertext, nil
}

// Decrypt decrypts ciphertext+tag using ChaCha20-Poly1305 AEAD.
// Returns plaintext or error if MAC verification fails.
//
// meshKey: 32-byte mesh encryption key
// nonce: 12-byte nonce from frame header
// ciphertext: encrypted data + 16-byte Poly1305 tag
func Decrypt(meshKey []byte, nonce [12]byte, ciphertext []byte) ([]byte, error) {
	if len(meshKey) != 32 {
		return nil, fmt.Errorf("invalid mesh key length: %d bytes (must be 32)", len(meshKey))
	}

	// Same HKDF derivation as Encrypt()
	kdf := hkdf.New(sha256.New, meshKey, []byte("conspiracyd-aead-v1"), []byte("beacon-encryption"))
	derivedKey := make([]byte, chacha20poly1305.KeySize)
	if _, err := io.ReadFull(kdf, derivedKey); err != nil {
		return nil, fmt.Errorf("key derivation failed: %w", err)
	}

	aead, err := chacha20poly1305.New(derivedKey)
	if err != nil {
		return nil, fmt.Errorf("cipher init failed: %w", err)
	}

	plaintext, err := aead.Open(nil, nonce[:], ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("MAC verification failed: %w", err)
	}

	return plaintext, nil
}
