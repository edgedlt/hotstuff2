// Package timer provides timer implementations for HotStuff-2 pacemaker.
//
// Provides three implementations:
// 1. RealTimer - Production timer using time.Timer
// 2. MockTimer - Controllable timer for testing
// 3. AdaptiveTimer - Exponential backoff timer
package timer

import (
	"sync"
	"time"
)

// Timer provides timeout management for the pacemaker.
// All implementations must be safe for concurrent use.
type Timer interface {
	// Start starts the timer with the given duration in milliseconds.
	Start(duration uint64)

	// Stop stops the timer.
	Stop()

	// Reset resets the timer with a new duration.
	Reset(duration uint64)

	// C returns a channel that receives when the timer expires.
	C() <-chan struct{}
}

// RealTimer implements Timer using time.Timer from stdlib.
// Safe for concurrent use.
type RealTimer struct {
	timer *time.Timer
	ch    chan struct{}
	mu    sync.Mutex
}

// NewRealTimer creates a new RealTimer.
func NewRealTimer() *RealTimer {
	return &RealTimer{
		ch: make(chan struct{}, 1),
	}
}

// Start starts the timer with the given duration in milliseconds.
func (t *RealTimer) Start(duration uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Stop existing timer if any
	if t.timer != nil {
		t.timer.Stop()
		// Drain channel if needed
		select {
		case <-t.ch:
		default:
		}
	}

	t.timer = time.AfterFunc(time.Duration(duration)*time.Millisecond, func() {
		select {
		case t.ch <- struct{}{}:
		default:
			// Channel full, timer already fired
		}
	})
}

// Stop stops the timer.
func (t *RealTimer) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.timer != nil {
		t.timer.Stop()
		t.timer = nil
		// Drain channel
		select {
		case <-t.ch:
		default:
		}
	}
}

// Reset resets the timer with a new duration.
func (t *RealTimer) Reset(duration uint64) {
	t.Start(duration)
}

// C returns a channel that receives when the timer expires.
func (t *RealTimer) C() <-chan struct{} {
	return t.ch
}

// MockTimer implements Timer for testing with manual control.
// Safe for concurrent use.
type MockTimer struct {
	ch       chan struct{}
	mu       sync.Mutex
	duration uint64
	running  bool
}

// NewMockTimer creates a new MockTimer.
func NewMockTimer() *MockTimer {
	return &MockTimer{
		ch: make(chan struct{}, 1),
	}
}

// Start starts the timer (records duration but does not fire automatically).
func (t *MockTimer) Start(duration uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.duration = duration
	t.running = true

	// Drain channel
	select {
	case <-t.ch:
	default:
	}
}

// Stop stops the timer.
func (t *MockTimer) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.running = false

	// Drain channel
	select {
	case <-t.ch:
	default:
	}
}

// Reset resets the timer with a new duration.
func (t *MockTimer) Reset(duration uint64) {
	t.Start(duration)
}

// C returns a channel that receives when the timer expires.
func (t *MockTimer) C() <-chan struct{} {
	return t.ch
}

// Fire manually fires the timer (for testing).
func (t *MockTimer) Fire() {
	t.mu.Lock()
	running := t.running
	t.mu.Unlock()

	if running {
		select {
		case t.ch <- struct{}{}:
		default:
			// Channel full, already fired
		}
	}
}

// IsRunning returns true if the timer is running.
func (t *MockTimer) IsRunning() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.running
}

// Duration returns the current duration setting.
func (t *MockTimer) Duration() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.duration
}

// AdaptiveConfig configures exponential backoff for AdaptiveTimer.
type AdaptiveConfig struct {
	// BaseDuration is the initial timeout duration in milliseconds.
	BaseDuration uint64

	// MaxDuration is the maximum timeout duration in milliseconds.
	MaxDuration uint64

	// BackoffFactor is the multiplicative factor for exponential backoff.
	// Typical value: 1.5 (50% increase per timeout).
	BackoffFactor float64
}

// DefaultAdaptiveConfig returns the default adaptive timer configuration.
func DefaultAdaptiveConfig() AdaptiveConfig {
	return AdaptiveConfig{
		BaseDuration:  100,   // 100ms base
		MaxDuration:   10000, // 10s max
		BackoffFactor: 1.5,   // 50% increase
	}
}

// AdaptiveTimer implements Timer with exponential backoff.
// Increases timeout duration on each timeout, resets on progress.
// Safe for concurrent use.
type AdaptiveTimer struct {
	underlying      Timer
	config          AdaptiveConfig
	mu              sync.Mutex
	currentDuration uint64
}

// NewAdaptiveTimer creates a new AdaptiveTimer with the given configuration.
func NewAdaptiveTimer(config AdaptiveConfig) *AdaptiveTimer {
	return &AdaptiveTimer{
		underlying:      NewRealTimer(),
		config:          config,
		currentDuration: config.BaseDuration,
	}
}

// NewAdaptiveTimerWithUnderlying creates an AdaptiveTimer with a custom underlying timer.
// Useful for testing with MockTimer.
func NewAdaptiveTimerWithUnderlying(config AdaptiveConfig, underlying Timer) *AdaptiveTimer {
	return &AdaptiveTimer{
		underlying:      underlying,
		config:          config,
		currentDuration: config.BaseDuration,
	}
}

// Start starts the timer with the current adaptive duration.
func (t *AdaptiveTimer) Start(duration uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Ignore provided duration, use current adaptive duration
	t.underlying.Start(t.currentDuration)
}

// Stop stops the timer.
func (t *AdaptiveTimer) Stop() {
	t.underlying.Stop()
}

// Reset resets the timer with the current adaptive duration.
func (t *AdaptiveTimer) Reset(duration uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Ignore provided duration, use current adaptive duration
	t.underlying.Reset(t.currentDuration)
}

// C returns a channel that receives when the timer expires.
func (t *AdaptiveTimer) C() <-chan struct{} {
	return t.underlying.C()
}

// OnTimeout should be called when the timer fires.
// Increases the timeout duration using exponential backoff.
func (t *AdaptiveTimer) OnTimeout() {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Apply exponential backoff
	newDuration := uint64(float64(t.currentDuration) * t.config.BackoffFactor)

	// Cap at maximum
	if newDuration > t.config.MaxDuration {
		newDuration = t.config.MaxDuration
	}

	t.currentDuration = newDuration
}

// OnProgress should be called when consensus makes progress.
// Resets the timeout duration to base value.
func (t *AdaptiveTimer) OnProgress() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.currentDuration = t.config.BaseDuration
}

// CurrentDuration returns the current timeout duration.
func (t *AdaptiveTimer) CurrentDuration() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.currentDuration
}
