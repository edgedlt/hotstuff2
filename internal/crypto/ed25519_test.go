package crypto

import (
	"bytes"
	"testing"
)

func TestEd25519KeyGeneration(t *testing.T) {
	key, err := GenerateEd25519Key()
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	if key == nil {
		t.Fatal("Generated key is nil")
	}

	if len(key.Bytes()) != Ed25519PrivateKeySize {
		t.Errorf("Private key size mismatch: expected %d, got %d", Ed25519PrivateKeySize, len(key.Bytes()))
	}

	pubKey := key.PublicKey()
	if pubKey == nil {
		t.Fatal("Public key is nil")
	}

	if len(pubKey.Bytes()) != Ed25519PublicKeySize {
		t.Errorf("Public key size mismatch: expected %d, got %d", Ed25519PublicKeySize, len(pubKey.Bytes()))
	}
}

func TestEd25519SignAndVerify(t *testing.T) {
	key, err := GenerateEd25519Key()
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	message := []byte("test message for signing")

	// Sign
	signature, err := key.Sign(message)
	if err != nil {
		t.Fatalf("Failed to sign: %v", err)
	}

	if len(signature) != Ed25519SignatureSize {
		t.Errorf("Signature size mismatch: expected %d, got %d", Ed25519SignatureSize, len(signature))
	}

	// Verify with correct key
	pubKey := key.PublicKey()
	if !pubKey.Verify(message, signature) {
		t.Error("Valid signature failed verification")
	}

	// Verify with wrong message
	wrongMessage := []byte("wrong message")
	if pubKey.Verify(wrongMessage, signature) {
		t.Error("Signature verified with wrong message")
	}

	// Verify with wrong signature
	wrongSignature := make([]byte, Ed25519SignatureSize)
	if pubKey.Verify(message, wrongSignature) {
		t.Error("Wrong signature incorrectly verified")
	}

	// Verify with different key
	otherKey, _ := GenerateEd25519Key()
	otherPubKey := otherKey.PublicKey()
	if otherPubKey.Verify(message, signature) {
		t.Error("Signature verified with wrong public key")
	}
}

func TestEd25519KeySerialization(t *testing.T) {
	// Test private key serialization
	key, err := GenerateEd25519Key()
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	keyBytes := key.Bytes()
	restoredKey, err := Ed25519PrivateKeyFromBytes(keyBytes)
	if err != nil {
		t.Fatalf("Failed to restore private key: %v", err)
	}

	if !bytes.Equal(key.Bytes(), restoredKey.Bytes()) {
		t.Error("Restored private key doesn't match original")
	}

	// Test public key serialization
	pubKey := key.PublicKey()
	pubKeyBytes := pubKey.Bytes()
	restoredPubKey, err := Ed25519PublicKeyFromBytes(pubKeyBytes)
	if err != nil {
		t.Fatalf("Failed to restore public key: %v", err)
	}

	if !bytes.Equal(pubKey.Bytes(), restoredPubKey.Bytes()) {
		t.Error("Restored public key doesn't match original")
	}

	// Verify signature still works with restored keys
	message := []byte("test message")
	signature, _ := restoredKey.Sign(message)
	if !restoredPubKey.Verify(message, signature) {
		t.Error("Signature verification failed with restored keys")
	}
}

func TestEd25519PublicKeyEquals(t *testing.T) {
	key1, _ := GenerateEd25519Key()
	key2, _ := GenerateEd25519Key()

	pub1 := key1.PublicKey()
	pub2 := key2.PublicKey()

	// Same key should equal itself
	if !pub1.Equals(pub1) {
		t.Error("Public key doesn't equal itself")
	}

	// Different keys should not be equal
	if pub1.Equals(pub2) {
		t.Error("Different public keys reported as equal")
	}

	// Restored key should equal original
	pub1Bytes := pub1.Bytes()
	pub1Restored, _ := Ed25519PublicKeyFromBytes(pub1Bytes)
	if !pub1.Equals(pub1Restored) {
		t.Error("Restored public key doesn't equal original")
	}
}

func TestEd25519MultiSignature(t *testing.T) {
	// Generate 4 validators
	validators := make([]*Ed25519PrivateKey, 4)
	for i := range validators {
		key, err := GenerateEd25519Key()
		if err != nil {
			t.Fatalf("Failed to generate key %d: %v", i, err)
		}
		validators[i] = key
	}

	message := []byte("block hash to sign")

	// Each validator signs
	signatures := make([][]byte, len(validators))
	signers := make([]uint16, len(validators))
	for i, val := range validators {
		sig, err := val.Sign(message)
		if err != nil {
			t.Fatalf("Validator %d failed to sign: %v", i, err)
		}
		signatures[i] = sig
		signers[i] = uint16(i)
	}

	// Create multi-signature
	multiSig, err := NewEd25519MultiSignature(signatures, signers)
	if err != nil {
		t.Fatalf("Failed to create multi-signature: %v", err)
	}

	if multiSig.Count() != len(validators) {
		t.Errorf("Multi-signature count mismatch: expected %d, got %d", len(validators), multiSig.Count())
	}

	// Verify multi-signature
	publicKeys := make(map[uint16]*Ed25519PublicKey)
	for i, val := range validators {
		publicKeys[uint16(i)] = val.PublicKey().(*Ed25519PublicKey)
	}

	if err := multiSig.Verify(message, publicKeys); err != nil {
		t.Errorf("Multi-signature verification failed: %v", err)
	}

	// Test with wrong message
	wrongMessage := []byte("wrong message")
	if err := multiSig.Verify(wrongMessage, publicKeys); err == nil {
		t.Error("Multi-signature verified with wrong message")
	}

	// Test with missing public key
	incompleteKeys := make(map[uint16]*Ed25519PublicKey)
	incompleteKeys[0] = validators[0].PublicKey().(*Ed25519PublicKey)
	if err := multiSig.Verify(message, incompleteKeys); err == nil {
		t.Error("Multi-signature verified with incomplete public keys")
	}
}

func TestEd25519MultiSignatureSerialization(t *testing.T) {
	// Create multi-signature
	validators := make([]*Ed25519PrivateKey, 3)
	for i := range validators {
		validators[i], _ = GenerateEd25519Key()
	}

	message := []byte("test message")
	signatures := make([][]byte, len(validators))
	signers := make([]uint16, len(validators))
	for i, val := range validators {
		signatures[i], _ = val.Sign(message)
		signers[i] = uint16(i)
	}

	multiSig, err := NewEd25519MultiSignature(signatures, signers)
	if err != nil {
		t.Fatalf("Failed to create multi-signature: %v", err)
	}

	// Serialize
	multiSigBytes := multiSig.Bytes()

	// Deserialize
	restoredMultiSig, err := Ed25519MultiSignatureFromBytes(multiSigBytes)
	if err != nil {
		t.Fatalf("Failed to deserialize multi-signature: %v", err)
	}

	if restoredMultiSig.Count() != multiSig.Count() {
		t.Errorf("Count mismatch after deserialization: expected %d, got %d", multiSig.Count(), restoredMultiSig.Count())
	}

	// Verify restored multi-signature
	publicKeys := make(map[uint16]*Ed25519PublicKey)
	for i, val := range validators {
		publicKeys[uint16(i)] = val.PublicKey().(*Ed25519PublicKey)
	}

	if err := restoredMultiSig.Verify(message, publicKeys); err != nil {
		t.Errorf("Restored multi-signature verification failed: %v", err)
	}
}

func TestEd25519InvalidInputs(t *testing.T) {
	// Test invalid private key size
	invalidPrivKey := make([]byte, 32) // Too short
	_, err := Ed25519PrivateKeyFromBytes(invalidPrivKey)
	if err == nil {
		t.Error("Expected error for invalid private key size")
	}

	// Test invalid public key size
	invalidPubKey := make([]byte, 16) // Too short
	_, err = Ed25519PublicKeyFromBytes(invalidPubKey)
	if err == nil {
		t.Error("Expected error for invalid public key size")
	}

	// Test multi-signature with mismatched counts
	signatures := [][]byte{make([]byte, 64)}
	signers := []uint16{0, 1} // More signers than signatures
	_, err = NewEd25519MultiSignature(signatures, signers)
	if err == nil {
		t.Error("Expected error for mismatched signature/signer counts")
	}

	// Test multi-signature with invalid signature size
	invalidSig := make([]byte, 32) // Too short
	_, err = NewEd25519MultiSignature([][]byte{invalidSig}, []uint16{0})
	if err == nil {
		t.Error("Expected error for invalid signature size")
	}
}

func BenchmarkEd25519Sign(b *testing.B) {
	key, _ := GenerateEd25519Key()
	message := []byte("benchmark message for signing")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = key.Sign(message)
	}
}

func BenchmarkEd25519Verify(b *testing.B) {
	key, _ := GenerateEd25519Key()
	message := []byte("benchmark message for signing")
	signature, _ := key.Sign(message)
	pubKey := key.PublicKey()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pubKey.Verify(message, signature)
	}
}

func BenchmarkEd25519KeyGeneration(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = GenerateEd25519Key()
	}
}
