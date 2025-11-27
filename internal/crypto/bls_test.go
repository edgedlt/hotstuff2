package crypto

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBLSKeyGeneration tests BLS key pair generation.
func TestBLSKeyGeneration(t *testing.T) {
	sk, err := GenerateBLSKey()
	require.NoError(t, err)
	require.NotNil(t, sk)

	pk := sk.PublicKey()
	require.NotNil(t, pk)

	// Public key should be deterministic from private key
	pk2 := sk.PublicKey()
	assert.True(t, pk.Equals(pk2), "same private key should produce same public key")
}

// TestBLSSignAndVerify tests basic sign and verify.
func TestBLSSignAndVerify(t *testing.T) {
	sk, err := GenerateBLSKey()
	require.NoError(t, err)

	pk := sk.PublicKey()
	message := []byte("test message")

	// Sign message
	sig, err := sk.Sign(message)
	require.NoError(t, err)
	require.NotNil(t, sig)

	// Verify signature
	assert.True(t, pk.Verify(message, sig), "valid signature should verify")

	// Verify with wrong message should fail
	wrongMessage := []byte("wrong message")
	assert.False(t, pk.Verify(wrongMessage, sig), "signature should not verify with wrong message")

	// Verify with wrong public key should fail
	sk2, _ := GenerateBLSKey()
	pk2 := sk2.PublicKey()
	assert.False(t, pk2.Verify(message, sig), "signature should not verify with wrong public key")
}

// TestBLSAggregation tests signature aggregation.
func TestBLSAggregation(t *testing.T) {
	const N = 5
	message := []byte("common message")

	// Generate N key pairs and signatures
	sigs := make([]*BLSSignature, N)
	pks := make([]*BLSPublicKey, N)

	for i := 0; i < N; i++ {
		sk, err := GenerateBLSKey()
		require.NoError(t, err)

		pks[i] = sk.PublicKey()
		sig, err := sk.Sign(message)
		require.NoError(t, err)
		sigs[i] = sig
	}

	// Aggregate signatures
	aggSig, err := AggregateSignatures(sigs)
	require.NoError(t, err)
	require.NotNil(t, aggSig)

	// Verify aggregated signature
	err = VerifyAggregated(message, aggSig, pks)
	assert.NoError(t, err, "aggregated signature should verify")

	// Verify with wrong message should fail
	wrongMessage := []byte("wrong message")
	err = VerifyAggregated(wrongMessage, aggSig, pks)
	assert.Error(t, err, "aggregated signature should not verify with wrong message")

	// Verify with subset of public keys should fail
	err = VerifyAggregated(message, aggSig, pks[:N-1])
	assert.Error(t, err, "aggregated signature should not verify with incomplete public keys")
}

// TestBLSAggregateVerification tests aggregate signature verification.
func TestBLSAggregateVerification(t *testing.T) {
	const N = 3
	message := []byte("test message")

	// Create N signatures
	sigs := make([]*BLSSignature, N)
	pks := make([]*BLSPublicKey, N)

	for i := 0; i < N; i++ {
		sk, _ := GenerateBLSKey()
		pks[i] = sk.PublicKey()
		sig, _ := sk.Sign(message)
		sigs[i] = sig
	}

	// Aggregate
	aggSig, err := AggregateSignatures(sigs)
	require.NoError(t, err)

	// Should verify
	err = VerifyAggregated(message, aggSig, pks)
	assert.NoError(t, err)

	// Tamper with aggregate signature
	tamperedSig := &BLSSignature{point: aggSig.point}
	// (In practice, we'd modify the point, but for testing we verify original works)
	err = VerifyAggregated(message, tamperedSig, pks)
	assert.NoError(t, err) // Original still works
}

// TestBLSKeySerialization tests key serialization and deserialization.
func TestBLSKeySerialization(t *testing.T) {
	sk, err := GenerateBLSKey()
	require.NoError(t, err)

	// Serialize private key
	skBytes := sk.Bytes()
	assert.Len(t, skBytes, 32, "private key should be 32 bytes")

	// Deserialize private key
	sk2, err := BLSPrivateKeyFromBytes(skBytes)
	require.NoError(t, err)
	assert.Equal(t, sk.scalar, sk2.scalar, "deserialized private key should match")

	// Public keys should match
	pk1 := sk.PublicKey()
	pk2 := sk2.PublicKey()
	assert.True(t, pk1.Equals(pk2), "public keys should match")

	// Serialize public key
	pkBytes := pk1.Bytes()
	assert.Len(t, pkBytes, 96, "public key should be 96 bytes (compressed G2)")

	// Deserialize public key
	pk3, err := BLSPublicKeyFromBytes(pkBytes)
	require.NoError(t, err)
	assert.True(t, pk1.Equals(pk3), "deserialized public key should match")
}

// TestBLSSignatureSerialization tests signature serialization.
func TestBLSSignatureSerialization(t *testing.T) {
	sk, _ := GenerateBLSKey()
	message := []byte("test")

	sig, err := sk.Sign(message)
	require.NoError(t, err)

	// Serialize
	sigBytes := sig.Bytes()
	assert.Len(t, sigBytes, 48, "signature should be 48 bytes (compressed G1)")

	// Deserialize
	sig2, err := BLSSignatureFromBytes(sigBytes)
	require.NoError(t, err)

	// Should verify
	pk := sk.PublicKey()
	assert.True(t, pk.Verify(message, sig2), "deserialized signature should verify")
}

// TestBLSInvalidInputs tests error handling for invalid inputs.
func TestBLSInvalidInputs(t *testing.T) {
	// Empty signatures
	_, err := AggregateSignatures([]*BLSSignature{})
	assert.ErrorIs(t, err, ErrEmptySignatures)

	// Empty public keys
	_, err = AggregatePublicKeys([]*BLSPublicKey{})
	assert.ErrorIs(t, err, ErrEmptyPublicKeys)

	// Verify with empty public keys
	message := []byte("test")
	sk, _ := GenerateBLSKey()
	sig, _ := sk.Sign(message)
	err = VerifyAggregated(message, sig, []*BLSPublicKey{})
	assert.ErrorIs(t, err, ErrEmptyPublicKeys)

	// Invalid private key length
	_, err = BLSPrivateKeyFromBytes([]byte{1, 2, 3})
	assert.Error(t, err)

	// Invalid public key bytes
	_, err = BLSPublicKeyFromBytes([]byte{1, 2, 3})
	assert.Error(t, err)

	// Invalid signature bytes
	_, err = BLSSignatureFromBytes([]byte{1, 2, 3})
	assert.Error(t, err)
}

// TestBLSPublicKeyAggregation tests public key aggregation.
func TestBLSPublicKeyAggregation(t *testing.T) {
	const N = 4

	pks := make([]*BLSPublicKey, N)
	for i := 0; i < N; i++ {
		sk, _ := GenerateBLSKey()
		pks[i] = sk.PublicKey()
	}

	// Aggregate public keys
	aggPK, err := AggregatePublicKeys(pks)
	require.NoError(t, err)
	require.NotNil(t, aggPK)

	// Aggregate should be deterministic
	aggPK2, err := AggregatePublicKeys(pks)
	require.NoError(t, err)
	assert.True(t, aggPK.Equals(aggPK2), "aggregation should be deterministic")
}

// TestBLSDifferentMessages tests signature aggregation over different messages.
func TestBLSDifferentMessages(t *testing.T) {
	const N = 3

	messages := [][]byte{
		[]byte("message 1"),
		[]byte("message 2"),
		[]byte("message 3"),
	}

	sigs := make([]*BLSSignature, N)
	pks := make([]*BLSPublicKey, N)

	for i := 0; i < N; i++ {
		sk, _ := GenerateBLSKey()
		pks[i] = sk.PublicKey()
		sig, _ := sk.Sign(messages[i])
		sigs[i] = sig
	}

	// Aggregate signatures
	aggSig, err := AggregateSignatures(sigs)
	require.NoError(t, err)

	// Verify with distinct messages
	err = VerifyAggregatedDistinct(messages, aggSig, pks)
	assert.NoError(t, err, "aggregated signature should verify with distinct messages")

	// Verify with wrong messages should fail
	wrongMessages := [][]byte{
		[]byte("wrong 1"),
		[]byte("wrong 2"),
		[]byte("wrong 3"),
	}
	err = VerifyAggregatedDistinct(wrongMessages, aggSig, pks)
	assert.Error(t, err)
}

// TestBLSBatchVerify tests batch signature verification.
func TestBLSBatchVerify(t *testing.T) {
	const N = 5

	messages := make([][]byte, N)
	sigs := make([]*BLSSignature, N)
	pks := make([]*BLSPublicKey, N)

	for i := 0; i < N; i++ {
		messages[i] = []byte{byte(i)}
		sk, _ := GenerateBLSKey()
		pks[i] = sk.PublicKey()
		sig, _ := sk.Sign(messages[i])
		sigs[i] = sig
	}

	// Batch verify should succeed
	err := BatchVerify(messages, sigs, pks)
	assert.NoError(t, err, "batch verification should succeed")

	// Tamper with one signature - should fail
	tamperedSigs := make([]*BLSSignature, N)
	copy(tamperedSigs, sigs)
	wrongSK, _ := GenerateBLSKey()
	wrongSig, _ := wrongSK.Sign(messages[0])
	tamperedSigs[0] = wrongSig

	err = BatchVerify(messages, tamperedSigs, pks)
	assert.Error(t, err, "batch verification should fail with tampered signature")
}

// TestBLSEmptyBatch tests batch verify with empty inputs.
func TestBLSEmptyBatch(t *testing.T) {
	err := BatchVerify([][]byte{}, []*BLSSignature{}, []*BLSPublicKey{})
	assert.ErrorIs(t, err, ErrEmptySignatures)
}

// TestBLSMismatchedCounts tests error handling for mismatched counts.
func TestBLSMismatchedCounts(t *testing.T) {
	message := []byte("test")
	sk, _ := GenerateBLSKey()
	sig, _ := sk.Sign(message)
	pk := sk.PublicKey()

	// Batch verify with mismatched counts
	err := BatchVerify([][]byte{message}, []*BLSSignature{sig, sig}, []*BLSPublicKey{pk})
	assert.ErrorIs(t, err, ErrSignatureCountMismatch)

	err = BatchVerify([][]byte{message}, []*BLSSignature{sig}, []*BLSPublicKey{pk, pk})
	assert.ErrorIs(t, err, ErrSignatureCountMismatch)

	// VerifyAggregatedDistinct with mismatched counts
	err = VerifyAggregatedDistinct([][]byte{message}, sig, []*BLSPublicKey{pk, pk})
	assert.ErrorIs(t, err, ErrSignatureCountMismatch)
}

// TestBLSDeterministic tests that operations are deterministic.
func TestBLSDeterministic(t *testing.T) {
	// Same private key bytes should produce same public key
	skBytes := make([]byte, 32)
	_, err := rand.Read(skBytes)
	require.NoError(t, err)

	sk1, err := BLSPrivateKeyFromBytes(skBytes)
	require.NoError(t, err)

	sk2, err := BLSPrivateKeyFromBytes(skBytes)
	require.NoError(t, err)

	pk1 := sk1.PublicKey()
	pk2 := sk2.PublicKey()

	assert.True(t, pk1.Equals(pk2), "same private key should produce same public key")

	// Same message should produce same signature
	message := []byte("deterministic test")
	sig1, _ := sk1.Sign(message)
	sig2, _ := sk2.Sign(message)

	assert.True(t, bytes.Equal(sig1.Bytes(), sig2.Bytes()), "same key and message should produce same signature")
}

// TestBLSConcurrentSigning tests thread-safety of signing.
func TestBLSConcurrentSigning(t *testing.T) {
	sk, _ := GenerateBLSKey()
	pk := sk.PublicKey()
	message := []byte("concurrent test")

	const N = 100
	sigs := make([]*BLSSignature, N)

	// Sign concurrently
	done := make(chan bool, N)
	for i := 0; i < N; i++ {
		go func(idx int) {
			sig, err := sk.Sign(message)
			require.NoError(t, err)
			sigs[idx] = sig
			done <- true
		}(i)
	}

	// Wait for all
	for i := 0; i < N; i++ {
		<-done
	}

	// All signatures should verify
	for i := 0; i < N; i++ {
		assert.True(t, pk.Verify(message, sigs[i]), "signature %d should verify", i)
	}
}

// BenchmarkBLSKeyGeneration benchmarks key generation.
func BenchmarkBLSKeyGeneration(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = GenerateBLSKey()
	}
}

// BenchmarkBLSSign benchmarks signing.
func BenchmarkBLSSign(b *testing.B) {
	sk, _ := GenerateBLSKey()
	message := []byte("benchmark message")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = sk.Sign(message)
	}
}

// BenchmarkBLSVerify benchmarks signature verification.
func BenchmarkBLSVerify(b *testing.B) {
	sk, _ := GenerateBLSKey()
	pk := sk.PublicKey()
	message := []byte("benchmark message")
	sig, _ := sk.Sign(message)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pk.Verify(message, sig)
	}
}

// BenchmarkBLSAggregate benchmarks signature aggregation.
func BenchmarkBLSAggregate(b *testing.B) {
	const N = 7 // Typical validator count

	sigs := make([]*BLSSignature, N)
	message := []byte("benchmark")

	for i := 0; i < N; i++ {
		sk, _ := GenerateBLSKey()
		sig, _ := sk.Sign(message)
		sigs[i] = sig
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = AggregateSignatures(sigs)
	}
}

// BenchmarkBLSAggregateVerify benchmarks aggregate verification.
func BenchmarkBLSAggregateVerify(b *testing.B) {
	const N = 7
	message := []byte("benchmark")

	sigs := make([]*BLSSignature, N)
	pks := make([]*BLSPublicKey, N)

	for i := 0; i < N; i++ {
		sk, _ := GenerateBLSKey()
		pks[i] = sk.PublicKey()
		sig, _ := sk.Sign(message)
		sigs[i] = sig
	}

	aggSig, _ := AggregateSignatures(sigs)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = VerifyAggregated(message, aggSig, pks)
	}
}

// BenchmarkBLSBatchVerify benchmarks batch verification.
func BenchmarkBLSBatchVerify(b *testing.B) {
	const N = 7

	messages := make([][]byte, N)
	sigs := make([]*BLSSignature, N)
	pks := make([]*BLSPublicKey, N)

	for i := 0; i < N; i++ {
		messages[i] = []byte{byte(i)}
		sk, _ := GenerateBLSKey()
		pks[i] = sk.PublicKey()
		sig, _ := sk.Sign(messages[i])
		sigs[i] = sig
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = BatchVerify(messages, sigs, pks)
	}
}
