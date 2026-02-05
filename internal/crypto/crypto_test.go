package crypto

import (
	"bytes"
	"testing"
)

func TestGenerateKeyPair(t *testing.T) {
	priv, pub, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if priv == nil || pub == nil {
		t.Fatal("keys must not be nil")
	}
	// Public key should be derivable from private key.
	if !bytes.Equal(priv.PublicKey().Bytes(), pub.Bytes()) {
		t.Fatal("public key mismatch")
	}
}

func TestEncryptDecrypt(t *testing.T) {
	key, err := GenerateDEK()
	if err != nil {
		t.Fatalf("GenerateDEK: %v", err)
	}

	plaintext := []byte("hello, end-to-end encryption")
	ct, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	got, err := Decrypt(key, ct)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}

	if !bytes.Equal(got, plaintext) {
		t.Fatalf("round-trip mismatch: got %q, want %q", got, plaintext)
	}
}

func TestDecryptWrongKey(t *testing.T) {
	key1, _ := GenerateDEK()
	key2, _ := GenerateDEK()

	ct, err := Encrypt(key1, []byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	_, err = Decrypt(key2, ct)
	if err == nil {
		t.Fatal("expected error decrypting with wrong key")
	}
}

func TestWrapUnwrapKey(t *testing.T) {
	// Alice (sender) and Bob (recipient).
	alicePriv, alicePub, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("alice keypair: %v", err)
	}
	bobPriv, bobPub, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("bob keypair: %v", err)
	}

	dek, err := GenerateDEK()
	if err != nil {
		t.Fatalf("GenerateDEK: %v", err)
	}

	// Alice wraps DEK for Bob.
	wrapped, err := WrapKey(alicePriv, bobPub, dek)
	if err != nil {
		t.Fatalf("WrapKey: %v", err)
	}

	// Bob unwraps with his private key + Alice's public key.
	got, err := UnwrapKey(bobPriv, alicePub, wrapped)
	if err != nil {
		t.Fatalf("UnwrapKey: %v", err)
	}

	if !bytes.Equal(got, dek) {
		t.Fatal("unwrapped DEK mismatch")
	}
}

func TestWrapUnwrapWrongKey(t *testing.T) {
	alicePriv, _, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("alice keypair: %v", err)
	}
	_, bobPub, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("bob keypair: %v", err)
	}
	evePriv, _, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("eve keypair: %v", err)
	}

	dek, _ := GenerateDEK()

	wrapped, err := WrapKey(alicePriv, bobPub, dek)
	if err != nil {
		t.Fatalf("WrapKey: %v", err)
	}

	// Eve tries to unwrap — wrong recipient private key.
	_, err = UnwrapKey(evePriv, alicePriv.PublicKey(), wrapped)
	if err == nil {
		t.Fatal("expected error unwrapping with wrong key")
	}
}

func TestDeriveKeyFromPassphrase(t *testing.T) {
	pass := "correct horse battery staple"

	key1, salt, err := DeriveKeyFromPassphrase(pass)
	if err != nil {
		t.Fatalf("DeriveKeyFromPassphrase: %v", err)
	}

	if len(key1) != keyLen {
		t.Fatalf("key length: got %d, want %d", len(key1), keyLen)
	}
	if len(salt) != saltLen {
		t.Fatalf("salt length: got %d, want %d", len(salt), saltLen)
	}

	// Re-derive with same salt should produce same key.
	key2, err := DeriveKeyFromPassphraseWithSalt(pass, salt)
	if err != nil {
		t.Fatalf("DeriveKeyFromPassphraseWithSalt: %v", err)
	}

	if !bytes.Equal(key1, key2) {
		t.Fatal("re-derived key mismatch")
	}
}

func TestDeriveKeyDifferentPassphrase(t *testing.T) {
	key1, salt, err := DeriveKeyFromPassphrase("passphrase-one")
	if err != nil {
		t.Fatalf("derive 1: %v", err)
	}

	key2, err := DeriveKeyFromPassphraseWithSalt("passphrase-two", salt)
	if err != nil {
		t.Fatalf("derive 2: %v", err)
	}

	if bytes.Equal(key1, key2) {
		t.Fatal("different passphrases should produce different keys")
	}
}

func TestGenerateDEK(t *testing.T) {
	dek, err := GenerateDEK()
	if err != nil {
		t.Fatalf("GenerateDEK: %v", err)
	}
	if len(dek) != keyLen {
		t.Fatalf("DEK length: got %d, want %d", len(dek), keyLen)
	}
}
