package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
)

// Ed25519 signature scheme using stdlib crypto/ed25519.
// Provides O(n) multi-signatures (concatenated signatures).
//
// Unlike BLS, Ed25519 signatures cannot be aggregated into O(1) size.
// However, Ed25519 is faster for individual sign/verify operations.

var (
	// ErrInvalidEd25519Signature indicates Ed25519 signature verification failed.
	ErrInvalidEd25519Signature = errors.New("invalid Ed25519 signature")

	// ErrInvalidEd25519KeySize indicates wrong key size.
	ErrInvalidEd25519KeySize = errors.New("invalid Ed25519 key size")
)

const (
	// Ed25519PublicKeySize is the size of a Ed25519 public key in bytes.
	Ed25519PublicKeySize = ed25519.PublicKeySize // 32 bytes

	// Ed25519PrivateKeySize is the size of a Ed25519 private key in bytes.
	Ed25519PrivateKeySize = ed25519.PrivateKeySize // 64 bytes

	// Ed25519SignatureSize is the size of an Ed25519 signature in bytes.
	Ed25519SignatureSize = ed25519.SignatureSize // 64 bytes
)

// Ed25519PrivateKey wraps stdlib Ed25519 private key.
type Ed25519PrivateKey struct {
	key ed25519.PrivateKey
	pub *Ed25519PublicKey
}

// Ed25519PublicKey wraps stdlib Ed25519 public key.
type Ed25519PublicKey struct {
	key ed25519.PublicKey
}

// Ed25519MultiSignature represents multiple Ed25519 signatures concatenated.
// This is O(n) size, unlike BLS which is O(1).
type Ed25519MultiSignature struct {
	signatures [][]byte // Each signature is 64 bytes
	signers    []uint16 // Validator indices
}

// GenerateEd25519Key generates a new Ed25519 key pair.
func GenerateEd25519Key() (*Ed25519PrivateKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate Ed25519 key: %w", err)
	}

	pubKey := &Ed25519PublicKey{key: pub}
	return &Ed25519PrivateKey{key: priv, pub: pubKey}, nil
}

// PublicKey returns the public key corresponding to this private key.
// Returns a pointer to Ed25519PublicKey which implements the PublicKey interface.
func (sk *Ed25519PrivateKey) PublicKey() interface {
	Bytes() []byte
	Verify(message []byte, signature []byte) bool
	Equals(other interface{ Bytes() []byte }) bool
	String() string
} {
	if sk.pub == nil {
		pubBytes, _ := sk.key.Public().(ed25519.PublicKey)
		sk.pub = &Ed25519PublicKey{key: pubBytes}
	}
	return sk.pub
}

// Sign signs a message with this private key.
func (sk *Ed25519PrivateKey) Sign(message []byte) ([]byte, error) {
	signature := ed25519.Sign(sk.key, message)
	return signature, nil
}

// Bytes returns the 64-byte private key.
func (sk *Ed25519PrivateKey) Bytes() []byte {
	return []byte(sk.key)
}

// Ed25519PrivateKeyFromBytes reconstructs a private key from bytes.
func Ed25519PrivateKeyFromBytes(data []byte) (*Ed25519PrivateKey, error) {
	if len(data) != Ed25519PrivateKeySize {
		return nil, fmt.Errorf("%w: expected %d, got %d", ErrInvalidEd25519KeySize, Ed25519PrivateKeySize, len(data))
	}

	key := ed25519.PrivateKey(data)
	pubBytes, _ := key.Public().(ed25519.PublicKey)
	pubKey := &Ed25519PublicKey{key: pubBytes}
	return &Ed25519PrivateKey{key: key, pub: pubKey}, nil
}

// Verify verifies a signature over a message with this public key.
func (pk *Ed25519PublicKey) Verify(message []byte, signature []byte) bool {
	if len(signature) != Ed25519SignatureSize {
		return false
	}

	return ed25519.Verify(pk.key, message, signature)
}

// Bytes returns the 32-byte public key.
func (pk *Ed25519PublicKey) Bytes() []byte {
	return []byte(pk.key)
}

// Equals checks if two public keys are equal by comparing bytes.
func (pk *Ed25519PublicKey) Equals(other interface{ Bytes() []byte }) bool {
	otherBytes := other.Bytes()
	if len(otherBytes) != len(pk.key) {
		return false
	}
	return pk.key.Equal(ed25519.PublicKey(otherBytes))
}

// String returns hex representation of the public key (first 8 bytes).
func (pk *Ed25519PublicKey) String() string {
	if len(pk.key) < 8 {
		return fmt.Sprintf("%x", pk.key)
	}
	return fmt.Sprintf("%x...", pk.key[:8])
}

// Ed25519PublicKeyFromBytes reconstructs a public key from bytes.
func Ed25519PublicKeyFromBytes(data []byte) (*Ed25519PublicKey, error) {
	if len(data) != Ed25519PublicKeySize {
		return nil, fmt.Errorf("%w: expected %d, got %d", ErrInvalidEd25519KeySize, Ed25519PublicKeySize, len(data))
	}

	key := ed25519.PublicKey(data)
	return &Ed25519PublicKey{key: key}, nil
}

// NewEd25519MultiSignature creates a multi-signature from individual signatures.
func NewEd25519MultiSignature(signatures [][]byte, signers []uint16) (*Ed25519MultiSignature, error) {
	if len(signatures) != len(signers) {
		return nil, fmt.Errorf("signature count (%d) != signer count (%d)", len(signatures), len(signers))
	}

	// Validate signature sizes
	for i, sig := range signatures {
		if len(sig) != Ed25519SignatureSize {
			return nil, fmt.Errorf("signature %d has invalid size: expected %d, got %d", i, Ed25519SignatureSize, len(sig))
		}
	}

	return &Ed25519MultiSignature{
		signatures: signatures,
		signers:    signers,
	}, nil
}

// Verify verifies a multi-signature over a message with multiple public keys.
func (ms *Ed25519MultiSignature) Verify(message []byte, publicKeys map[uint16]*Ed25519PublicKey) error {
	if len(ms.signatures) != len(ms.signers) {
		return fmt.Errorf("signature/signer count mismatch")
	}

	for i, signerIdx := range ms.signers {
		pk, ok := publicKeys[signerIdx]
		if !ok {
			return fmt.Errorf("public key not found for signer %d", signerIdx)
		}

		if !pk.Verify(message, ms.signatures[i]) {
			return fmt.Errorf("%w: signer %d", ErrInvalidEd25519Signature, signerIdx)
		}
	}

	return nil
}

// Bytes serializes the multi-signature.
// Format: [signerCount:2][signer1:2][signer2:2]...[sig1:64][sig2:64]...
func (ms *Ed25519MultiSignature) Bytes() []byte {
	if len(ms.signers) == 0 {
		return nil
	}

	// Calculate size: 2 bytes count + (2 bytes per signer) + (64 bytes per signature)
	size := 2 + len(ms.signers)*2 + len(ms.signatures)*Ed25519SignatureSize
	result := make([]byte, size)

	// Write signer count (uint16)
	result[0] = byte(len(ms.signers) >> 8)
	result[1] = byte(len(ms.signers))

	// Write signer indices
	offset := 2
	for _, signer := range ms.signers {
		result[offset] = byte(signer >> 8)
		result[offset+1] = byte(signer)
		offset += 2
	}

	// Write signatures
	for _, sig := range ms.signatures {
		copy(result[offset:], sig)
		offset += Ed25519SignatureSize
	}

	return result
}

// Ed25519MultiSignatureFromBytes reconstructs a multi-signature from bytes.
func Ed25519MultiSignatureFromBytes(data []byte) (*Ed25519MultiSignature, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("data too short for multi-signature")
	}

	// Read signer count
	signerCount := int(data[0])<<8 | int(data[1])
	if signerCount == 0 {
		return &Ed25519MultiSignature{}, nil
	}

	// Check minimum size: 2 (count) + signerCount*2 (indices) + signerCount*64 (signatures)
	expectedSize := 2 + signerCount*2 + signerCount*Ed25519SignatureSize
	if len(data) != expectedSize {
		return nil, fmt.Errorf("invalid data size: expected %d, got %d", expectedSize, len(data))
	}

	// Read signer indices
	signers := make([]uint16, signerCount)
	offset := 2
	for i := 0; i < signerCount; i++ {
		signers[i] = uint16(data[offset])<<8 | uint16(data[offset+1])
		offset += 2
	}

	// Read signatures
	signatures := make([][]byte, signerCount)
	for i := 0; i < signerCount; i++ {
		sig := make([]byte, Ed25519SignatureSize)
		copy(sig, data[offset:offset+Ed25519SignatureSize])
		signatures[i] = sig
		offset += Ed25519SignatureSize
	}

	return &Ed25519MultiSignature{
		signatures: signatures,
		signers:    signers,
	}, nil
}

// Signers returns the list of validator indices who signed.
func (ms *Ed25519MultiSignature) Signers() []uint16 {
	return ms.signers
}

// Count returns the number of signatures in this multi-signature.
func (ms *Ed25519MultiSignature) Count() int {
	return len(ms.signers)
}
