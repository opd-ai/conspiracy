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

// deriveEncryptionKey derives a ChaCha20-Poly1305 key from the mesh key using HKDF.
func deriveEncryptionKey(meshKey []byte) ([]byte, error) {
	if len(meshKey) != 32 {
		return nil, fmt.Errorf("invalid mesh key length: %d bytes (must be 32)", len(meshKey))
	}

	kdf := hkdf.New(sha256.New, meshKey, []byte("conspiracyd-aead-v1"), []byte("beacon-encryption"))
	derivedKey := make([]byte, chacha20poly1305.KeySize)
	if _, err := io.ReadFull(kdf, derivedKey); err != nil {
		return nil, fmt.Errorf("key derivation failed: %w", err)
	}
	return derivedKey, nil
}

// Encrypt encrypts plaintext using ChaCha20-Poly1305 AEAD with provided nonce.
// Returns ciphertext+tag (16-byte Poly1305 MAC appended).
//
// meshKey: 32-byte mesh encryption key
// nonce: 12-byte nonce from NonceGenerator
// plaintext: data to encrypt (typically BEACON payload)
func Encrypt(meshKey []byte, nonce [12]byte, plaintext []byte) ([]byte, error) {
	derivedKey, err := deriveEncryptionKey(meshKey)
	if err != nil {
		return nil, err
	}

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
	derivedKey, err := deriveEncryptionKey(meshKey)
	if err != nil {
		return nil, err
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

// ComputeHMAC computes HMAC-SHA256 over frame data, truncated to 12 bytes.
// Used for frame authentication (header + ciphertext).
//
// meshKey: 32-byte mesh encryption key
// frameData: complete frame (header + payload)
func ComputeHMAC(meshKey, frameData []byte) [12]byte {
	mac := sha256.New()
	mac.Write(meshKey)
	mac.Write(frameData)
	sum := mac.Sum(nil)

	var truncated [12]byte
	copy(truncated[:], sum[:12])
	return truncated
}
