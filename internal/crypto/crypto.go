// Package crypto provides end-to-end encryption primitives for td-sync.
// It includes X25519 key exchange, AES-256-GCM encryption, ECDH+HKDF key
// wrapping, and Argon2id passphrase-based key derivation.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/hkdf"
)

const (
	// keyLen is the AES-256 key length in bytes.
	keyLen = 32
	// nonceLen is the GCM nonce length in bytes.
	nonceLen = 12
	// saltLen is the Argon2id salt length in bytes.
	saltLen = 32
	// hkdfInfo is the info string for HKDF key derivation.
	hkdfInfo = "td-sync-key-wrap"

	// Argon2id parameters.
	argonTime    = 1
	argonMemory  = 64 * 1024
	argonThreads = 4
)

// GenerateKeyPair generates an X25519 keypair for key exchange.
func GenerateKeyPair() (*ecdh.PrivateKey, *ecdh.PublicKey, error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate x25519 key: %w", err)
	}
	return priv, priv.PublicKey(), nil
}

// Encrypt encrypts plaintext using AES-256-GCM with a 256-bit key.
// Returns nonce || ciphertext (nonce is prepended).
func Encrypt(key, plaintext []byte) ([]byte, error) {
	if len(key) != keyLen {
		return nil, errors.New("key must be 32 bytes")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}

	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("random nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// Decrypt decrypts ciphertext produced by Encrypt.
func Decrypt(key, ciphertext []byte) ([]byte, error) {
	if len(key) != keyLen {
		return nil, errors.New("key must be 32 bytes")
	}

	if len(ciphertext) < nonceLen {
		return nil, errors.New("ciphertext too short")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}

	nonce := ciphertext[:nonceLen]
	ct := ciphertext[nonceLen:]

	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	return plaintext, nil
}

// deriveSharedKey performs ECDH and derives an AES-256 key via HKDF-SHA256.
func deriveSharedKey(priv *ecdh.PrivateKey, pub *ecdh.PublicKey) ([]byte, error) {
	secret, err := priv.ECDH(pub)
	if err != nil {
		return nil, fmt.Errorf("ecdh: %w", err)
	}

	r := hkdf.New(sha256.New, secret, nil, []byte(hkdfInfo))
	key := make([]byte, keyLen)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, fmt.Errorf("hkdf: %w", err)
	}

	return key, nil
}

// WrapKey wraps a data encryption key using ECDH shared secret + HKDF.
// senderPriv + recipientPub -> shared secret -> HKDF-derived AES key -> encrypt DEK.
func WrapKey(senderPriv *ecdh.PrivateKey, recipientPub *ecdh.PublicKey, dek []byte) ([]byte, error) {
	aesKey, err := deriveSharedKey(senderPriv, recipientPub)
	if err != nil {
		return nil, fmt.Errorf("derive wrap key: %w", err)
	}
	return Encrypt(aesKey, dek)
}

// UnwrapKey unwraps a data encryption key.
func UnwrapKey(recipientPriv *ecdh.PrivateKey, senderPub *ecdh.PublicKey, wrappedDEK []byte) ([]byte, error) {
	aesKey, err := deriveSharedKey(recipientPriv, senderPub)
	if err != nil {
		return nil, fmt.Errorf("derive unwrap key: %w", err)
	}
	return Decrypt(aesKey, wrappedDEK)
}

// DeriveKeyFromPassphrase derives a 256-bit key from a passphrase using Argon2id.
// Returns the derived key and the salt used (32 bytes random salt).
func DeriveKeyFromPassphrase(passphrase string) (key, salt []byte, err error) {
	salt = make([]byte, saltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, nil, fmt.Errorf("random salt: %w", err)
	}

	key = argon2.IDKey([]byte(passphrase), salt, argonTime, argonMemory, argonThreads, keyLen)
	return key, salt, nil
}

// DeriveKeyFromPassphraseWithSalt derives a key using a known salt (for recovery).
func DeriveKeyFromPassphraseWithSalt(passphrase string, salt []byte) ([]byte, error) {
	if len(salt) != saltLen {
		return nil, fmt.Errorf("salt must be %d bytes", saltLen)
	}
	key := argon2.IDKey([]byte(passphrase), salt, argonTime, argonMemory, argonThreads, keyLen)
	return key, nil
}

// GenerateDEK generates a random 256-bit data encryption key.
func GenerateDEK() ([]byte, error) {
	dek := make([]byte, keyLen)
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, fmt.Errorf("random dek: %w", err)
	}
	return dek, nil
}
