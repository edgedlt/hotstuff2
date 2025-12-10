package hotstuff2

import (
	"errors"
	"strings"
	"testing"

	"github.com/edgedlt/hotstuff2/internal/crypto"
	"github.com/edgedlt/hotstuff2/timer"
	"go.uber.org/zap"
)

// TestConfigCreation tests basic configuration creation.
func TestConfigCreation(t *testing.T) {
	validators := NewTestValidatorSet(4)
	key, _ := crypto.GenerateEd25519Key()
	storage := NewMockStorage[TestHash]()
	network := NewMockNetwork[TestHash]()
	executor := NewMockExecutor[TestHash]()
	mockTimer := timer.NewMockTimer()

	cfg, err := NewConfig(
		WithMyIndex[TestHash](0),
		WithValidators[TestHash](validators),
		WithPrivateKey[TestHash](key),
		WithStorage[TestHash](storage),
		WithNetwork[TestHash](network),
		WithExecutor[TestHash](executor),
		WithTimer[TestHash](mockTimer),
	)

	if err != nil {
		t.Fatalf("Failed to create config: %v", err)
	}

	if cfg.MyIndex != 0 {
		t.Errorf("MyIndex should be 0, got %d", cfg.MyIndex)
	}

	if cfg.CryptoScheme != CryptoSchemeEd25519 {
		t.Errorf("Default CryptoScheme should be Ed25519, got %s", cfg.CryptoScheme)
	}

	if cfg.EnableVerification {
		t.Error("EnableVerification should be false by default")
	}
}

// TestConfigValidationMissingFields tests that required fields are validated.
func TestConfigValidationMissingFields(t *testing.T) {
	tests := []struct {
		name    string
		opts    []ConfigOption[TestHash]
		wantErr string
	}{
		{
			name:    "MissingValidators",
			opts:    []ConfigOption[TestHash]{},
			wantErr: "validators is required",
		},
		{
			name: "MissingPrivateKey",
			opts: []ConfigOption[TestHash]{
				WithValidators[TestHash](NewTestValidatorSet(4)),
			},
			wantErr: "private key is required",
		},
		{
			name: "MissingStorage",
			opts: []ConfigOption[TestHash]{
				WithValidators[TestHash](NewTestValidatorSet(4)),
				WithPrivateKey[TestHash](mustGenerateKey()),
			},
			wantErr: "storage is required",
		},
		{
			name: "MissingNetwork",
			opts: []ConfigOption[TestHash]{
				WithValidators[TestHash](NewTestValidatorSet(4)),
				WithPrivateKey[TestHash](mustGenerateKey()),
				WithStorage[TestHash](NewMockStorage[TestHash]()),
			},
			wantErr: "network is required",
		},
		{
			name: "MissingExecutor",
			opts: []ConfigOption[TestHash]{
				WithValidators[TestHash](NewTestValidatorSet(4)),
				WithPrivateKey[TestHash](mustGenerateKey()),
				WithStorage[TestHash](NewMockStorage[TestHash]()),
				WithNetwork[TestHash](NewMockNetwork[TestHash]()),
			},
			wantErr: "executor is required",
		},
		{
			name: "MissingTimer",
			opts: []ConfigOption[TestHash]{
				WithValidators[TestHash](NewTestValidatorSet(4)),
				WithPrivateKey[TestHash](mustGenerateKey()),
				WithStorage[TestHash](NewMockStorage[TestHash]()),
				WithNetwork[TestHash](NewMockNetwork[TestHash]()),
				WithExecutor[TestHash](NewMockExecutor[TestHash]()),
			},
			wantErr: "timer is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewConfig(tt.opts...)
			if err == nil {
				t.Fatal("Expected error, got nil")
			}

			// Check that it's a config error
			if !errors.Is(err, ErrConfig) {
				t.Errorf("Expected ErrConfig, got: %v", err)
			}

			// Check that error message contains the expected text
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Expected error to contain '%s', got '%s'", tt.wantErr, err.Error())
			}
		})
	}
}

// TestConfigInvalidIndex tests validation of my index.
func TestConfigInvalidIndex(t *testing.T) {
	validators := NewTestValidatorSet(4) // indices 0-3

	_, err := NewConfig(
		WithMyIndex[TestHash](5), // Invalid: out of range
		WithValidators[TestHash](validators),
		WithPrivateKey[TestHash](mustGenerateKey()),
		WithStorage[TestHash](NewMockStorage[TestHash]()),
		WithNetwork[TestHash](NewMockNetwork[TestHash]()),
		WithExecutor[TestHash](NewMockExecutor[TestHash]()),
		WithTimer[TestHash](timer.NewMockTimer()),
	)

	if err == nil {
		t.Fatal("Expected error for invalid index")
	}

	if !errors.Is(err, ErrConfig) {
		t.Errorf("Expected ErrConfig, got: %v", err)
	}
}

// TestConfigInvalidCryptoScheme tests validation of crypto scheme.
func TestConfigInvalidCryptoScheme(t *testing.T) {
	_, err := NewConfig(
		WithMyIndex[TestHash](0),
		WithValidators[TestHash](NewTestValidatorSet(4)),
		WithPrivateKey[TestHash](mustGenerateKey()),
		WithStorage[TestHash](NewMockStorage[TestHash]()),
		WithNetwork[TestHash](NewMockNetwork[TestHash]()),
		WithExecutor[TestHash](NewMockExecutor[TestHash]()),
		WithTimer[TestHash](timer.NewMockTimer()),
		WithCryptoScheme[TestHash]("invalid"),
	)

	if err == nil {
		t.Fatal("Expected error for invalid crypto scheme")
	}
}

// TestConfigWithBLS tests BLS configuration.
func TestConfigWithBLS(t *testing.T) {
	cfg, err := NewConfig(
		WithMyIndex[TestHash](0),
		WithValidators[TestHash](NewTestValidatorSet(4)),
		WithPrivateKey[TestHash](mustGenerateKey()),
		WithStorage[TestHash](NewMockStorage[TestHash]()),
		WithNetwork[TestHash](NewMockNetwork[TestHash]()),
		WithExecutor[TestHash](NewMockExecutor[TestHash]()),
		WithTimer[TestHash](timer.NewMockTimer()),
		WithCryptoScheme[TestHash](CryptoSchemeBLS),
	)

	if err != nil {
		t.Fatalf("Failed to create config with BLS: %v", err)
	}

	if cfg.CryptoScheme != CryptoSchemeBLS {
		t.Errorf("CryptoScheme should be BLS, got %s", cfg.CryptoScheme)
	}
}

// TestConfigWithLogger tests custom logger configuration.
func TestConfigWithLogger(t *testing.T) {
	logger := zap.NewExample()

	cfg, err := NewConfig(
		WithMyIndex[TestHash](0),
		WithValidators[TestHash](NewTestValidatorSet(4)),
		WithPrivateKey[TestHash](mustGenerateKey()),
		WithStorage[TestHash](NewMockStorage[TestHash]()),
		WithNetwork[TestHash](NewMockNetwork[TestHash]()),
		WithExecutor[TestHash](NewMockExecutor[TestHash]()),
		WithTimer[TestHash](timer.NewMockTimer()),
		WithLogger[TestHash](logger),
	)

	if err != nil {
		t.Fatalf("Failed to create config with logger: %v", err)
	}

	if cfg.Logger != logger {
		t.Error("Logger should be set correctly")
	}
}

// TestConfigWithNilLogger tests that nil logger is rejected.
func TestConfigWithNilLogger(t *testing.T) {
	_, err := NewConfig(
		WithMyIndex[TestHash](0),
		WithValidators[TestHash](NewTestValidatorSet(4)),
		WithPrivateKey[TestHash](mustGenerateKey()),
		WithStorage[TestHash](NewMockStorage[TestHash]()),
		WithNetwork[TestHash](NewMockNetwork[TestHash]()),
		WithExecutor[TestHash](NewMockExecutor[TestHash]()),
		WithTimer[TestHash](timer.NewMockTimer()),
		WithLogger[TestHash](nil),
	)

	if err == nil {
		t.Fatal("Expected error for nil logger")
	}
}

// TestConfigWithVerification tests verification flag.
func TestConfigWithVerification(t *testing.T) {
	cfg, err := NewConfig(
		WithMyIndex[TestHash](0),
		WithValidators[TestHash](NewTestValidatorSet(4)),
		WithPrivateKey[TestHash](mustGenerateKey()),
		WithStorage[TestHash](NewMockStorage[TestHash]()),
		WithNetwork[TestHash](NewMockNetwork[TestHash]()),
		WithExecutor[TestHash](NewMockExecutor[TestHash]()),
		WithTimer[TestHash](timer.NewMockTimer()),
		WithVerification[TestHash](true),
	)

	if err != nil {
		t.Fatalf("Failed to create config: %v", err)
	}

	if !cfg.EnableVerification {
		t.Error("EnableVerification should be true")
	}
}

// TestConfigQuorum tests quorum calculation.
func TestConfigQuorum(t *testing.T) {
	tests := []struct {
		name           string
		validatorCount int
		expectedQuorum int
	}{
		{"n=4", 4, 3},   // f=1, quorum=3
		{"n=7", 7, 5},   // f=2, quorum=5
		{"n=10", 10, 7}, // f=3, quorum=7
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := NewConfig(
				WithMyIndex[TestHash](0),
				WithValidators[TestHash](NewTestValidatorSet(tt.validatorCount)),
				WithPrivateKey[TestHash](mustGenerateKey()),
				WithStorage[TestHash](NewMockStorage[TestHash]()),
				WithNetwork[TestHash](NewMockNetwork[TestHash]()),
				WithExecutor[TestHash](NewMockExecutor[TestHash]()),
				WithTimer[TestHash](timer.NewMockTimer()),
			)

			if err != nil {
				t.Fatalf("Failed to create config: %v", err)
			}

			if cfg.Quorum() != tt.expectedQuorum {
				t.Errorf("Expected quorum %d, got %d", tt.expectedQuorum, cfg.Quorum())
			}
		})
	}
}

// TestConfigIsLeader tests leader determination.
func TestConfigIsLeader(t *testing.T) {
	cfg, err := NewConfig(
		WithMyIndex[TestHash](0),
		WithValidators[TestHash](NewTestValidatorSet(4)),
		WithPrivateKey[TestHash](mustGenerateKey()),
		WithStorage[TestHash](NewMockStorage[TestHash]()),
		WithNetwork[TestHash](NewMockNetwork[TestHash]()),
		WithExecutor[TestHash](NewMockExecutor[TestHash]()),
		WithTimer[TestHash](timer.NewMockTimer()),
	)

	if err != nil {
		t.Fatalf("Failed to create config: %v", err)
	}

	// View 0: leader should be validator 0
	if !cfg.IsLeader(0) {
		t.Error("Validator 0 should be leader for view 0")
	}

	// View 1: leader should be validator 1
	if cfg.IsLeader(1) {
		t.Error("Validator 0 should NOT be leader for view 1")
	}

	// View 4: round-robin back to validator 0
	if !cfg.IsLeader(4) {
		t.Error("Validator 0 should be leader for view 4")
	}
}

// TestConfigByzantineFaultTolerance tests n >= 3f+1 validation.
func TestConfigByzantineFaultTolerance(t *testing.T) {
	// n=3 gives f=(3-1)/3=0, which satisfies 3>=3*0+1=1, so it should succeed
	// We need n=2 to fail: f=(2-1)/3=0, but 2 < 3*0+1=1 is false, so it succeeds
	// Actually, with integer division: n=2, f=0, 2>=1 passes
	// We need n=0 or n=1 to actually fail

	t.Run("ValidMinimal", func(t *testing.T) {
		// n=3, f=0: 3 >= 3*0+1=1 ✓
		validators := NewTestValidatorSet(3)
		_, err := NewConfig(
			WithMyIndex[TestHash](0),
			WithValidators[TestHash](validators),
			WithPrivateKey[TestHash](mustGenerateKey()),
			WithStorage[TestHash](NewMockStorage[TestHash]()),
			WithNetwork[TestHash](NewMockNetwork[TestHash]()),
			WithExecutor[TestHash](NewMockExecutor[TestHash]()),
			WithTimer[TestHash](timer.NewMockTimer()),
		)
		if err != nil {
			t.Errorf("n=3 should be valid (f=0, 3>=1), got error: %v", err)
		}
	})

	t.Run("ValidStandard", func(t *testing.T) {
		// n=4, f=1: 4 >= 3*1+1=4 ✓
		validators := NewTestValidatorSet(4)
		_, err := NewConfig(
			WithMyIndex[TestHash](0),
			WithValidators[TestHash](validators),
			WithPrivateKey[TestHash](mustGenerateKey()),
			WithStorage[TestHash](NewMockStorage[TestHash]()),
			WithNetwork[TestHash](NewMockNetwork[TestHash]()),
			WithExecutor[TestHash](NewMockExecutor[TestHash]()),
			WithTimer[TestHash](timer.NewMockTimer()),
		)
		if err != nil {
			t.Errorf("n=4 should be valid (f=1, 4>=4), got error: %v", err)
		}
	})
}

// Helper functions

func mustGenerateKey() PrivateKey {
	key, err := crypto.GenerateEd25519Key()
	if err != nil {
		panic(err)
	}
	return key
}
