package hotstuff2

import (
	"fmt"
	"time"

	"go.uber.org/zap"
)

// Config holds the configuration for a HotStuff2 consensus instance.
type Config[H Hash] struct {
	// MyIndex is the validator index of this node.
	MyIndex uint16

	// Validators is the set of all validators in the network.
	Validators ValidatorSet

	// PrivateKey is the private key for signing messages.
	PrivateKey PrivateKey

	// Storage provides persistent storage for blocks and consensus state.
	Storage Storage[H]

	// Network provides message broadcasting and delivery.
	Network Network[H]

	// Executor handles block execution and validation.
	Executor Executor[H]

	// Timer provides timeout management for the pacemaker.
	Timer Timer

	// Pacemaker configures the timing behavior (timeouts, block time, backoff).
	// If nil, DefaultPacemakerConfig() is used.
	Pacemaker *PacemakerConfig

	// Logger for structured logging.
	Logger *zap.Logger

	// CryptoScheme specifies which signature scheme to use ("ed25519" or "bls").
	CryptoScheme string

	// EnableVerification enables runtime TLA+ model twins verification.
	EnableVerification bool
}

// ConfigOption is a functional option for configuring HotStuff2.
type ConfigOption[H Hash] func(*Config[H]) error

// NewConfig creates a new Config with the given options.
func NewConfig[H Hash](opts ...ConfigOption[H]) (*Config[H], error) {
	cfg := &Config[H]{
		Logger:             zap.NewNop(),        // Default: no-op logger
		CryptoScheme:       CryptoSchemeEd25519, // Default: Ed25519
		EnableVerification: false,               // Default: no verification
	}

	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return nil, err
		}
	}

	// Validate required fields
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

// validate checks that all required configuration fields are set.
func (c *Config[H]) validate() error {
	if c.Validators == nil {
		return wrapConfig("validators is required")
	}

	if c.PrivateKey == nil {
		return wrapConfig("private key is required")
	}

	if c.Storage == nil {
		return wrapConfig("storage is required")
	}

	if c.Network == nil {
		return wrapConfig("network is required")
	}

	if c.Executor == nil {
		return wrapConfig("executor is required")
	}

	if c.Timer == nil {
		return wrapConfig("timer is required")
	}

	if !c.Validators.Contains(c.MyIndex) {
		return wrapConfigf("validator index %d not in validator set", c.MyIndex)
	}

	if c.CryptoScheme != CryptoSchemeEd25519 && c.CryptoScheme != CryptoSchemeBLS {
		return wrapConfigf("unsupported crypto scheme: %s", c.CryptoScheme)
	}

	// Validate Byzantine fault tolerance: n >= 3f + 1
	n := c.Validators.Count()
	f := c.Validators.F()
	if n < 3*f+1 {
		return wrapConfigf("insufficient validators: n=%d, f=%d (need n >= 3f+1)", n, f)
	}

	return nil
}

// WithMyIndex sets the validator index for this node.
func WithMyIndex[H Hash](index uint16) ConfigOption[H] {
	return func(c *Config[H]) error {
		c.MyIndex = index
		return nil
	}
}

// WithValidators sets the validator set.
func WithValidators[H Hash](validators ValidatorSet) ConfigOption[H] {
	return func(c *Config[H]) error {
		if validators == nil {
			return fmt.Errorf("validators cannot be nil")
		}
		c.Validators = validators
		return nil
	}
}

// WithPrivateKey sets the private key for signing.
func WithPrivateKey[H Hash](key PrivateKey) ConfigOption[H] {
	return func(c *Config[H]) error {
		if key == nil {
			return fmt.Errorf("private key cannot be nil")
		}
		c.PrivateKey = key
		return nil
	}
}

// WithStorage sets the storage backend.
func WithStorage[H Hash](storage Storage[H]) ConfigOption[H] {
	return func(c *Config[H]) error {
		if storage == nil {
			return fmt.Errorf("storage cannot be nil")
		}
		c.Storage = storage
		return nil
	}
}

// WithNetwork sets the network layer.
func WithNetwork[H Hash](network Network[H]) ConfigOption[H] {
	return func(c *Config[H]) error {
		if network == nil {
			return fmt.Errorf("network cannot be nil")
		}
		c.Network = network
		return nil
	}
}

// WithExecutor sets the block executor.
func WithExecutor[H Hash](executor Executor[H]) ConfigOption[H] {
	return func(c *Config[H]) error {
		if executor == nil {
			return fmt.Errorf("executor cannot be nil")
		}
		c.Executor = executor
		return nil
	}
}

// WithTimer sets the timer implementation.
func WithTimer[H Hash](timer Timer) ConfigOption[H] {
	return func(c *Config[H]) error {
		if timer == nil {
			return fmt.Errorf("timer cannot be nil")
		}
		c.Timer = timer
		return nil
	}
}

// WithLogger sets the logger.
func WithLogger[H Hash](logger *zap.Logger) ConfigOption[H] {
	return func(c *Config[H]) error {
		if logger == nil {
			return fmt.Errorf("logger cannot be nil")
		}
		c.Logger = logger
		return nil
	}
}

// WithCryptoScheme sets the signature scheme ("ed25519" or "bls").
func WithCryptoScheme[H Hash](scheme string) ConfigOption[H] {
	return func(c *Config[H]) error {
		if scheme != CryptoSchemeEd25519 && scheme != CryptoSchemeBLS {
			return fmt.Errorf("unsupported crypto scheme: %s", scheme)
		}
		c.CryptoScheme = scheme
		return nil
	}
}

// WithVerification enables or disables runtime TLA+ verification.
func WithVerification[H Hash](enabled bool) ConfigOption[H] {
	return func(c *Config[H]) error {
		c.EnableVerification = enabled
		return nil
	}
}

// WithPacemaker sets the pacemaker configuration.
// Use this to configure target block time, timeouts, and backoff behavior.
func WithPacemaker[H Hash](config PacemakerConfig) ConfigOption[H] {
	return func(c *Config[H]) error {
		if err := config.Validate(); err != nil {
			return err
		}
		c.Pacemaker = &config
		return nil
	}
}

// WithTargetBlockTime is a convenience method to configure pacemaker for a
// specific target block time. This sets up appropriate timeouts for production use.
func WithTargetBlockTime[H Hash](blockTime time.Duration) ConfigOption[H] {
	return func(c *Config[H]) error {
		config := ProductionPacemakerConfig(blockTime)
		c.Pacemaker = &config
		return nil
	}
}

// Quorum returns the quorum size (2f+1).
func (c *Config[H]) Quorum() int {
	return 2*c.Validators.F() + 1
}

// IsLeader returns true if this node is the leader for the given view.
func (c *Config[H]) IsLeader(view uint32) bool {
	return c.Validators.GetLeader(view) == c.MyIndex
}
