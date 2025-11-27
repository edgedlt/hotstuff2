package hotstuff2

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

// PacemakerConfig configures the pacemaker timing behavior.
//
// This follows the standard BFT timing model used by production systems like
// CometBFT (Tendermint) and Aptos. The key parameters are:
//
//   - TimeoutPropose: How long to wait for a block proposal before timing out
//   - TimeoutVote: How long to wait for votes after receiving a proposal
//   - TimeoutCommit: Minimum time between blocks (target block time)
//   - BackoffMultiplier: Exponential backoff factor for failed rounds
//
// For public blockchains, TimeoutCommit is typically the primary control for
// block time. For example, setting TimeoutCommit to 5 seconds results in
// approximately 5-second blocks under normal conditions.
//
// # Minimum Block Time Enforcement
//
// For stronger block time guarantees, enable MinBlockTime. When set:
//   - Leaders wait until last_commit_time + MinBlockTime before proposing
//   - Validators refuse to vote for proposals that arrive too early
//
// This prevents malicious/fast leaders from speeding up the chain while
// preserving HotStuff-2 safety and remaining compatible with the liveness model.
type PacemakerConfig struct {
	// TimeoutPropose is how long to wait for a proposal before timing out.
	// This is the initial timeout for each view.
	// Default: 1000ms (1 second)
	TimeoutPropose time.Duration

	// TimeoutVote is how long to wait for votes after receiving +2/3 prevotes.
	// In HotStuff-2, this is mainly used for the two-chain commit rule.
	// Default: 1000ms (1 second)
	TimeoutVote time.Duration

	// TimeoutCommit is the minimum delay after committing a block before
	// starting the next view. This is the primary control for target block time.
	// Setting this to 0 allows blocks to be produced as fast as possible.
	// Default: 0 (immediate, optimistically responsive)
	TimeoutCommit time.Duration

	// MinBlockTime enforces a minimum time between blocks at the consensus level.
	// When set (> 0):
	//   - Leaders wait until last_commit_time + MinBlockTime before proposing
	//   - Validators reject proposals that arrive before last_commit_time + MinBlockTime - MinBlockTimeSkew
	//
	// This provides stronger guarantees than TimeoutCommit alone, as it prevents
	// malicious leaders from proposing blocks faster than the target rate.
	//
	// Default: 0 (disabled, use TimeoutCommit for block pacing)
	MinBlockTime time.Duration

	// MinBlockTimeSkew is the clock skew tolerance for MinBlockTime enforcement.
	// Validators will accept proposals that arrive up to this much before the
	// minimum block time. This accounts for clock drift between nodes.
	//
	// Only used when MinBlockTime > 0.
	// Default: 500ms
	MinBlockTimeSkew time.Duration

	// BackoffMultiplier is the factor by which timeouts increase after each
	// failed round (view change without commit). Values > 1.0 provide exponential
	// backoff which helps the network synchronize during periods of instability.
	// Default: 1.5 (50% increase per failed round)
	BackoffMultiplier float64

	// MaxTimeout is the maximum timeout duration. Timeouts will not grow beyond
	// this value regardless of backoff.
	// Default: 30s
	MaxTimeout time.Duration

	// SkipTimeoutCommit, if true, skips the commit delay when we receive
	// all votes (fast path). This allows faster block times when the network
	// is healthy while still enforcing the target block time under degraded
	// conditions.
	// Default: false
	SkipTimeoutCommit bool
}

// DefaultPacemakerConfig returns the default pacemaker configuration.
// This is tuned for low-latency networks with optimistic responsiveness.
func DefaultPacemakerConfig() PacemakerConfig {
	return PacemakerConfig{
		TimeoutPropose:    1000 * time.Millisecond,
		TimeoutVote:       1000 * time.Millisecond,
		TimeoutCommit:     0, // Immediate - optimistically responsive
		MinBlockTime:      0, // Disabled by default
		MinBlockTimeSkew:  500 * time.Millisecond,
		BackoffMultiplier: 1.5,
		MaxTimeout:        30 * time.Second,
		SkipTimeoutCommit: false,
	}
}

// ProductionPacemakerConfig returns a configuration suitable for production
// public blockchains with a target block time.
//
// This enables MinBlockTime enforcement, which ensures:
//   - Leaders wait until last_commit_time + targetBlockTime before proposing
//   - Validators reject proposals that arrive too early
//
// This provides stronger guarantees against malicious leaders trying to
// speed up block production.
func ProductionPacemakerConfig(targetBlockTime time.Duration) PacemakerConfig {
	return PacemakerConfig{
		TimeoutPropose:    targetBlockTime,
		TimeoutVote:       targetBlockTime / 2,
		TimeoutCommit:     targetBlockTime,
		MinBlockTime:      targetBlockTime,
		MinBlockTimeSkew:  500 * time.Millisecond,
		BackoffMultiplier: 1.5,
		MaxTimeout:        30 * time.Second,
		SkipTimeoutCommit: false,
	}
}

// DemoPacemakerConfig returns a configuration suitable for demos and testing
// with visible block production (approximately 1 block per second).
func DemoPacemakerConfig() PacemakerConfig {
	return PacemakerConfig{
		TimeoutPropose:    2 * time.Second,
		TimeoutVote:       1 * time.Second,
		TimeoutCommit:     1 * time.Second, // Target ~1 block/sec
		MinBlockTime:      1 * time.Second, // Enforce ~1 block/sec
		MinBlockTimeSkew:  200 * time.Millisecond,
		BackoffMultiplier: 1.5,
		MaxTimeout:        10 * time.Second,
		SkipTimeoutCommit: false,
	}
}

// Validate checks that the configuration values are sensible.
func (c PacemakerConfig) Validate() error {
	if c.TimeoutPropose <= 0 {
		return &ConfigError{Field: "TimeoutPropose", Message: "must be positive"}
	}
	if c.TimeoutVote <= 0 {
		return &ConfigError{Field: "TimeoutVote", Message: "must be positive"}
	}
	if c.TimeoutCommit < 0 {
		return &ConfigError{Field: "TimeoutCommit", Message: "must be non-negative"}
	}
	if c.MinBlockTime < 0 {
		return &ConfigError{Field: "MinBlockTime", Message: "must be non-negative"}
	}
	if c.MinBlockTime > 0 && c.MinBlockTimeSkew < 0 {
		return &ConfigError{Field: "MinBlockTimeSkew", Message: "must be non-negative when MinBlockTime is set"}
	}
	if c.MinBlockTime > 0 && c.MinBlockTimeSkew >= c.MinBlockTime {
		return &ConfigError{Field: "MinBlockTimeSkew", Message: "must be less than MinBlockTime"}
	}
	if c.BackoffMultiplier < 1.0 {
		return &ConfigError{Field: "BackoffMultiplier", Message: "must be >= 1.0"}
	}
	if c.MaxTimeout <= 0 {
		return &ConfigError{Field: "MaxTimeout", Message: "must be positive"}
	}
	if c.MaxTimeout < c.TimeoutPropose {
		return &ConfigError{Field: "MaxTimeout", Message: "must be >= TimeoutPropose"}
	}
	return nil
}

// ConfigError represents a configuration validation error.
type ConfigError struct {
	Field   string
	Message string
}

func (e *ConfigError) Error() string {
	return "pacemaker config: " + e.Field + " " + e.Message
}

// Pacemaker manages view synchronization and timeouts for HotStuff2.
//
// The pacemaker triggers view changes when the current view doesn't make progress.
// It uses adaptive timeouts with exponential backoff for liveness under asynchrony.
//
// The timing model follows standard BFT practices:
//   - TimeoutPropose: Wait for leader to propose
//   - TimeoutVote: Wait for votes (unused in two-phase HotStuff-2)
//   - TimeoutCommit: Minimum delay between blocks (target block time)
type Pacemaker struct {
	mu     sync.Mutex
	timer  Timer
	logger *zap.Logger
	config PacemakerConfig

	// Current round timeout (increases with backoff on failures)
	currentTimeout time.Duration

	// Track consecutive failures for backoff
	consecutiveFailures int

	// Commit delay timer (for TimeoutCommit)
	commitTimer     *time.Timer
	commitTimerDone chan struct{}

	// Callbacks
	onTimeout func(view uint32)
}

// NewPacemaker creates a new Pacemaker with the given configuration.
func NewPacemaker(timer Timer, logger *zap.Logger, onTimeout func(view uint32)) *Pacemaker {
	return NewPacemakerWithConfig(timer, logger, onTimeout, DefaultPacemakerConfig())
}

// NewPacemakerWithConfig creates a new Pacemaker with explicit configuration.
func NewPacemakerWithConfig(timer Timer, logger *zap.Logger, onTimeout func(view uint32), config PacemakerConfig) *Pacemaker {
	// Initialize commitTimerDone as closed so the first WaitForCommitDelay() doesn't block
	initialDone := make(chan struct{})
	close(initialDone)

	return &Pacemaker{
		timer:           timer,
		logger:          logger,
		onTimeout:       onTimeout,
		config:          config,
		currentTimeout:  config.TimeoutPropose,
		commitTimerDone: initialDone,
	}
}

// Start starts the pacemaker timer for the given view.
func (pm *Pacemaker) Start(view uint32) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.logger.Debug("pacemaker started",
		zap.Uint32("view", view),
		zap.Duration("timeout", pm.currentTimeout))

	pm.timer.Start(uint64(pm.currentTimeout.Milliseconds()))
}

// Stop stops the pacemaker.
func (pm *Pacemaker) Stop() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.logger.Debug("pacemaker stopped")
	pm.timer.Stop()
	pm.stopCommitTimer()
}

// OnProgress should be called when consensus makes progress (receives valid proposal/votes).
// This resets the view timeout but does NOT affect the commit delay.
func (pm *Pacemaker) OnProgress(view uint32) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.logger.Debug("progress made, resetting timeout",
		zap.Uint32("view", view))

	pm.timer.Reset(uint64(pm.currentTimeout.Milliseconds()))
}

// OnCommit should be called when a block is committed. It enforces the minimum
// block time (TimeoutCommit) before allowing the next view to start proposing.
// Returns a channel that will be closed when the commit delay is complete.
func (pm *Pacemaker) OnCommit(view uint32) <-chan struct{} {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Reset backoff on successful commit
	pm.consecutiveFailures = 0
	pm.currentTimeout = pm.config.TimeoutPropose

	// Stop any existing commit timer
	pm.stopCommitTimer()

	// If no commit delay configured, return immediately closed channel
	// and set commitTimerDone so WaitForCommitDelay doesn't block
	if pm.config.TimeoutCommit <= 0 {
		pm.commitTimerDone = make(chan struct{})
		close(pm.commitTimerDone)
		return pm.commitTimerDone
	}

	pm.logger.Debug("commit delay started",
		zap.Uint32("view", view),
		zap.Duration("delay", pm.config.TimeoutCommit))

	// Create new commit delay
	pm.commitTimerDone = make(chan struct{})
	done := pm.commitTimerDone
	pm.commitTimer = time.AfterFunc(pm.config.TimeoutCommit, func() {
		pm.mu.Lock()
		if pm.commitTimerDone == done {
			close(pm.commitTimerDone)
		}
		pm.mu.Unlock()
	})

	return done
}

// OnTimeout handles timer expiration (triggers view change).
func (pm *Pacemaker) OnTimeout(view uint32) {
	pm.mu.Lock()
	callback := pm.onTimeout
	timeout := pm.currentTimeout
	pm.mu.Unlock()

	pm.logger.Warn("view timeout",
		zap.Uint32("view", view),
		zap.Duration("timeout", timeout))

	// Trigger view change callback
	if callback != nil {
		callback(view)
	}
}

// IncreaseTimeout increases the timeout duration using exponential backoff.
// This should be called after a view change (timeout without commit).
func (pm *Pacemaker) IncreaseTimeout() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.consecutiveFailures++

	// Exponential backoff
	newTimeout := time.Duration(float64(pm.currentTimeout) * pm.config.BackoffMultiplier)

	// Cap at maximum
	if newTimeout > pm.config.MaxTimeout {
		newTimeout = pm.config.MaxTimeout
	}

	pm.currentTimeout = newTimeout

	pm.logger.Info("timeout increased",
		zap.Duration("new_timeout", pm.currentTimeout),
		zap.Int("consecutive_failures", pm.consecutiveFailures))
}

// ResetTimeout resets the timeout to the base value.
// This is typically called after successful commit.
func (pm *Pacemaker) ResetTimeout() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.consecutiveFailures = 0
	pm.currentTimeout = pm.config.TimeoutPropose

	pm.logger.Debug("timeout reset to base",
		zap.Duration("timeout", pm.currentTimeout))
}

// CurrentTimeout returns the current timeout duration.
func (pm *Pacemaker) CurrentTimeout() uint64 {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return uint64(pm.currentTimeout.Milliseconds())
}

// Config returns the pacemaker configuration.
func (pm *Pacemaker) Config() PacemakerConfig {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return pm.config
}

// stopCommitTimer stops the commit timer if running (must hold lock).
func (pm *Pacemaker) stopCommitTimer() {
	if pm.commitTimer != nil {
		pm.commitTimer.Stop()
		pm.commitTimer = nil
	}
}

// WaitForCommitDelay waits for the commit delay to complete.
// This is a convenience method that blocks until the delay is done.
func (pm *Pacemaker) WaitForCommitDelay() {
	pm.mu.Lock()
	done := pm.commitTimerDone
	pm.mu.Unlock()

	if done != nil {
		<-done
	}
}

// WaitForMinBlockTime waits until the minimum block time has elapsed since lastCommitTime.
// This should be called by leaders before proposing a new block.
// Returns immediately if MinBlockTime is not configured (0).
func (pm *Pacemaker) WaitForMinBlockTime(lastCommitTime time.Time) {
	pm.mu.Lock()
	minBlockTime := pm.config.MinBlockTime
	pm.mu.Unlock()

	if minBlockTime <= 0 {
		return // MinBlockTime not configured
	}

	if lastCommitTime.IsZero() {
		return // No previous commit, no need to wait
	}

	elapsed := time.Since(lastCommitTime)
	if elapsed >= minBlockTime {
		return // Already past minimum block time
	}

	remaining := minBlockTime - elapsed
	pm.logger.Debug("waiting for minimum block time",
		zap.Duration("remaining", remaining),
		zap.Duration("min_block_time", minBlockTime))

	time.Sleep(remaining)
}

// CanAcceptProposal checks if a proposal can be accepted based on minimum block time.
// Returns true if MinBlockTime is not configured, or if enough time has elapsed.
// The skew parameter allows for clock drift tolerance.
func (pm *Pacemaker) CanAcceptProposal(lastCommitTime time.Time) bool {
	pm.mu.Lock()
	minBlockTime := pm.config.MinBlockTime
	skew := pm.config.MinBlockTimeSkew
	pm.mu.Unlock()

	if minBlockTime <= 0 {
		return true // MinBlockTime not configured
	}

	if lastCommitTime.IsZero() {
		return true // No previous commit
	}

	elapsed := time.Since(lastCommitTime)
	// Allow proposals that arrive within skew tolerance of the minimum block time
	threshold := minBlockTime - skew
	if threshold < 0 {
		threshold = 0
	}

	return elapsed >= threshold
}

// TimeUntilCanPropose returns how long until a proposal can be made.
// Returns 0 if MinBlockTime is not configured or already past threshold.
func (pm *Pacemaker) TimeUntilCanPropose(lastCommitTime time.Time) time.Duration {
	pm.mu.Lock()
	minBlockTime := pm.config.MinBlockTime
	pm.mu.Unlock()

	if minBlockTime <= 0 {
		return 0
	}

	if lastCommitTime.IsZero() {
		return 0
	}

	elapsed := time.Since(lastCommitTime)
	if elapsed >= minBlockTime {
		return 0
	}

	return minBlockTime - elapsed
}

// MinBlockTimeEnabled returns true if minimum block time enforcement is enabled.
func (pm *Pacemaker) MinBlockTimeEnabled() bool {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return pm.config.MinBlockTime > 0
}
