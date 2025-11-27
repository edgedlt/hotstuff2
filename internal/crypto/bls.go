// Package crypto provides cryptographic primitives for HotStuff-2.
//
// This package implements two signature schemes:
// 1. BLS12-381 - O(1) aggregate signatures (this file)
// 2. Ed25519 - O(n) multi-signatures (ed25519.go)
package crypto

import (
	"errors"
	"fmt"
	"math/big"

	bls12381 "github.com/consensys/gnark-crypto/ecc/bls12-381"
	"github.com/consensys/gnark-crypto/ecc/bls12-381/fr"
)

// BLS12-381 signature scheme using gnark-crypto.
// Provides O(1) signature aggregation ideal for QC formation.

var (
	// ErrInvalidSignature indicates signature verification failed.
	ErrInvalidSignature = errors.New("invalid signature")

	// ErrEmptySignatures indicates no signatures provided for aggregation.
	ErrEmptySignatures = errors.New("no signatures to aggregate")

	// ErrEmptyPublicKeys indicates no public keys provided for aggregation.
	ErrEmptyPublicKeys = errors.New("no public keys to aggregate")

	// ErrSignatureCountMismatch indicates signature and public key counts differ.
	ErrSignatureCountMismatch = errors.New("signature count does not match public key count")
)

// BLSPrivateKey wraps a BLS12-381 private key.
type BLSPrivateKey struct {
	scalar fr.Element
}

// BLSPublicKey wraps a BLS12-381 public key (G2 point).
type BLSPublicKey struct {
	point bls12381.G2Affine
}

// BLSSignature wraps a BLS12-381 signature (G1 point).
type BLSSignature struct {
	point bls12381.G1Affine
}

// GenerateBLSKey generates a new BLS12-381 key pair.
func GenerateBLSKey() (*BLSPrivateKey, error) {
	// Generate random scalar in Fr
	var scalar fr.Element
	_, err := scalar.SetRandom()
	if err != nil {
		return nil, fmt.Errorf("failed to generate random scalar: %w", err)
	}

	return &BLSPrivateKey{scalar: scalar}, nil
}

// PublicKey returns the public key corresponding to this private key.
func (sk *BLSPrivateKey) PublicKey() *BLSPublicKey {
	// Public key = scalar * G2.Generator
	var pk bls12381.G2Affine
	_, _, _, g2Gen := bls12381.Generators()
	pk.ScalarMultiplication(&g2Gen, sk.scalar.BigInt(new(big.Int)))

	return &BLSPublicKey{point: pk}
}

// Sign signs a message with this private key.
// Message is hashed to G1, then multiplied by the private key scalar.
func (sk *BLSPrivateKey) Sign(message []byte) (*BLSSignature, error) {
	// Hash message to G1 point
	hashPoint, err := bls12381.HashToG1(message, []byte("BLS_SIG_BLS12381G1_XMD:SHA-256_SSWU_RO_NUL_"))
	if err != nil {
		return nil, fmt.Errorf("failed to hash message to G1: %w", err)
	}

	// Signature = scalar * H(m)
	var sig bls12381.G1Affine
	sig.ScalarMultiplication(&hashPoint, sk.scalar.BigInt(new(big.Int)))

	return &BLSSignature{point: sig}, nil
}

// Bytes returns the 32-byte scalar representation.
func (sk *BLSPrivateKey) Bytes() []byte {
	b := sk.scalar.Bytes()
	return b[:]
}

// BLSPrivateKeyFromBytes reconstructs a private key from bytes.
func BLSPrivateKeyFromBytes(data []byte) (*BLSPrivateKey, error) {
	if len(data) != fr.Bytes {
		return nil, fmt.Errorf("invalid private key length: expected %d, got %d", fr.Bytes, len(data))
	}

	var scalar fr.Element
	scalar.SetBytes(data) // SetBytes now returns the element itself, not an error

	return &BLSPrivateKey{scalar: scalar}, nil
}

// Verify verifies a signature over a message with this public key.
func (pk *BLSPublicKey) Verify(message []byte, signature *BLSSignature) bool {
	// Hash message to G1
	hashPoint, err := bls12381.HashToG1(message, []byte("BLS_SIG_BLS12381G1_XMD:SHA-256_SSWU_RO_NUL_"))
	if err != nil {
		return false
	}

	// Verify: e(H(m), pk) == e(sig, G2)
	_, _, _, g2Gen := bls12381.Generators()

	// Compute pairings
	left, err := bls12381.Pair([]bls12381.G1Affine{hashPoint}, []bls12381.G2Affine{pk.point})
	if err != nil {
		return false
	}

	right, err := bls12381.Pair([]bls12381.G1Affine{signature.point}, []bls12381.G2Affine{g2Gen})
	if err != nil {
		return false
	}

	return left.Equal(&right)
}

// Bytes returns the compressed 96-byte G2 point representation.
func (pk *BLSPublicKey) Bytes() []byte {
	b := pk.point.Bytes()
	return b[:]
}

// Equals checks if two public keys are equal.
func (pk *BLSPublicKey) Equals(other *BLSPublicKey) bool {
	return pk.point.Equal(&other.point)
}

// BLSPublicKeyFromBytes reconstructs a public key from bytes.
func BLSPublicKeyFromBytes(data []byte) (*BLSPublicKey, error) {
	var point bls12381.G2Affine
	_, err := point.SetBytes(data)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize public key: %w", err)
	}

	return &BLSPublicKey{point: point}, nil
}

// Bytes returns the compressed 48-byte G1 point representation.
func (sig *BLSSignature) Bytes() []byte {
	b := sig.point.Bytes()
	return b[:]
}

// BLSSignatureFromBytes reconstructs a signature from bytes.
func BLSSignatureFromBytes(data []byte) (*BLSSignature, error) {
	var point bls12381.G1Affine
	_, err := point.SetBytes(data)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize signature: %w", err)
	}

	return &BLSSignature{point: point}, nil
}

// AggregateSignatures aggregates multiple BLS signatures into one.
// This is the key advantage of BLS: O(1) aggregate signature size.
func AggregateSignatures(signatures []*BLSSignature) (*BLSSignature, error) {
	if len(signatures) == 0 {
		return nil, ErrEmptySignatures
	}

	// Sum all signature points
	var aggPoint bls12381.G1Jac
	aggPoint.FromAffine(&signatures[0].point)

	for i := 1; i < len(signatures); i++ {
		var point bls12381.G1Jac
		point.FromAffine(&signatures[i].point)
		aggPoint.AddAssign(&point)
	}

	var result bls12381.G1Affine
	result.FromJacobian(&aggPoint)

	return &BLSSignature{point: result}, nil
}

// AggregatePublicKeys aggregates multiple BLS public keys into one.
// Used for batch verification of aggregate signatures.
func AggregatePublicKeys(publicKeys []*BLSPublicKey) (*BLSPublicKey, error) {
	if len(publicKeys) == 0 {
		return nil, ErrEmptyPublicKeys
	}

	// Sum all public key points
	var aggPoint bls12381.G2Jac
	aggPoint.FromAffine(&publicKeys[0].point)

	for i := 1; i < len(publicKeys); i++ {
		var point bls12381.G2Jac
		point.FromAffine(&publicKeys[i].point)
		aggPoint.AddAssign(&point)
	}

	var result bls12381.G2Affine
	result.FromJacobian(&aggPoint)

	return &BLSPublicKey{point: result}, nil
}

// VerifyAggregated verifies an aggregate signature over the same message
// signed by multiple public keys.
//
// CRITICAL: This assumes all signers signed the SAME message.
// For different messages, use VerifyAggregatedDistinct.
func VerifyAggregated(message []byte, aggregatedSig *BLSSignature, publicKeys []*BLSPublicKey) error {
	if len(publicKeys) == 0 {
		return ErrEmptyPublicKeys
	}

	// Aggregate public keys
	aggPK, err := AggregatePublicKeys(publicKeys)
	if err != nil {
		return fmt.Errorf("failed to aggregate public keys: %w", err)
	}

	// Verify using aggregated public key
	if !aggPK.Verify(message, aggregatedSig) {
		return ErrInvalidSignature
	}

	return nil
}

// VerifyAggregatedDistinct verifies an aggregate signature over DIFFERENT messages
// signed by multiple public keys.
//
// This is slower than VerifyAggregated but supports different messages per signer.
// Use this for multi-signature schemes where each validator signs different data.
func VerifyAggregatedDistinct(messages [][]byte, aggregatedSig *BLSSignature, publicKeys []*BLSPublicKey) error {
	if len(messages) != len(publicKeys) {
		return ErrSignatureCountMismatch
	}

	if len(publicKeys) == 0 {
		return ErrEmptyPublicKeys
	}

	// Hash all messages to G1
	hashPoints := make([]bls12381.G1Affine, len(messages))
	for i, msg := range messages {
		hashPoint, err := bls12381.HashToG1(msg, []byte("BLS_SIG_BLS12381G1_XMD:SHA-256_SSWU_RO_NUL_"))
		if err != nil {
			return fmt.Errorf("failed to hash message %d: %w", i, err)
		}
		hashPoints[i] = hashPoint
	}

	// Verify: e(H(m1), pk1) * e(H(m2), pk2) * ... == e(aggSig, G2)
	pkPoints := make([]bls12381.G2Affine, len(publicKeys))
	for i, pk := range publicKeys {
		pkPoints[i] = pk.point
	}

	left, err := bls12381.Pair(hashPoints, pkPoints)
	if err != nil {
		return fmt.Errorf("failed to compute left pairing: %w", err)
	}

	_, _, _, g2Gen := bls12381.Generators()
	right, err := bls12381.Pair([]bls12381.G1Affine{aggregatedSig.point}, []bls12381.G2Affine{g2Gen})
	if err != nil {
		return fmt.Errorf("failed to compute right pairing: %w", err)
	}

	if !left.Equal(&right) {
		return ErrInvalidSignature
	}

	return nil
}

// BatchVerify verifies multiple individual signatures in a batch.
// More efficient than verifying each signature separately.
func BatchVerify(messages [][]byte, signatures []*BLSSignature, publicKeys []*BLSPublicKey) error {
	if len(messages) != len(signatures) || len(messages) != len(publicKeys) {
		return ErrSignatureCountMismatch
	}

	if len(messages) == 0 {
		return ErrEmptySignatures
	}

	// Generate random coefficients for random linear combination
	coeffs := make([]fr.Element, len(signatures))
	for i := range coeffs {
		if _, err := coeffs[i].SetRandom(); err != nil {
			return fmt.Errorf("failed to generate random coefficient: %w", err)
		}
	}

	// Compute aggregated signature with random coefficients: Σ r_i * sig_i
	var aggSigJac bls12381.G1Jac
	for i, sig := range signatures {
		var scaled bls12381.G1Jac
		scaled.FromAffine(&sig.point)
		scaled.ScalarMultiplication(&scaled, coeffs[i].BigInt(new(big.Int)))
		if i == 0 {
			aggSigJac = scaled
		} else {
			aggSigJac.AddAssign(&scaled)
		}
	}

	var aggSig bls12381.G1Affine
	aggSig.FromJacobian(&aggSigJac)

	// Hash all messages and scale by coefficients: r_i * H(m_i) for each i
	scaledHashes := make([]bls12381.G1Affine, len(messages))
	for i, msg := range messages {
		hashPoint, err := bls12381.HashToG1(msg, []byte("BLS_SIG_BLS12381G1_XMD:SHA-256_SSWU_RO_NUL_"))
		if err != nil {
			return fmt.Errorf("failed to hash message %d: %w", i, err)
		}

		var scaled bls12381.G1Jac
		scaled.FromAffine(&hashPoint)
		scaled.ScalarMultiplication(&scaled, coeffs[i].BigInt(new(big.Int)))
		scaledHashes[i].FromJacobian(&scaled)
	}

	// Get public keys for multi-pairing
	pkPoints := make([]bls12381.G2Affine, len(publicKeys))
	for i, pk := range publicKeys {
		pkPoints[i] = pk.point
	}

	// Verify batch using multi-pairing:
	// e(Σ r_i * sig_i, G2) == e(r_1*H(m_1), pk_1) * ... * e(r_n*H(m_n), pk_n)
	// Right side: e(aggSig, G2)
	_, _, _, g2Gen := bls12381.Generators()
	right, err := bls12381.Pair([]bls12381.G1Affine{aggSig}, []bls12381.G2Affine{g2Gen})
	if err != nil {
		return fmt.Errorf("failed to compute right pairing: %w", err)
	}

	// Left side: multi-pairing e([r_1*H(m_1), ..., r_n*H(m_n)], [pk_1, ..., pk_n])
	left, err := bls12381.Pair(scaledHashes, pkPoints)
	if err != nil {
		return fmt.Errorf("failed to compute left pairing: %w", err)
	}

	if !left.Equal(&right) {
		return ErrInvalidSignature
	}

	return nil
}
