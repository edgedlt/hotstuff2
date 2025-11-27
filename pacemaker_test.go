package hotstuff2

import (
	"sync"
	"testing"
	"time"

	"github.com/edgedlt/hotstuff2/timer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestPacemakerConfig_Default tests default configuration values.
func TestPacemakerConfig_Default(t *testing.T) {
	cfg := DefaultPacemakerConfig()

	assert.Equal(t, 1000*time.Millisecond, cfg.TimeoutPropose)
	assert.Equal(t, 1000*time.Millisecond, cfg.TimeoutVote)
	assert.Equal(t, time.Duration(0), cfg.TimeoutCommit)
	assert.Equal(t, time.Duration(0), cfg.MinBlockTime, "MinBlockTime should be 0 (disabled) by default")
	assert.Equal(t, 500*time.Millisecond, cfg.MinBlockTimeSkew, "MinBlockTimeSkew should have sensible default")
	assert.Equal(t, 1.5, cfg.BackoffMultiplier)
	assert.Equal(t, 30*time.Second, cfg.MaxTimeout)
	assert.False(t, cfg.SkipTimeoutCommit)
}

// TestPacemakerConfig_Production tests production configuration.
func TestPacemakerConfig_Production(t *testing.T) {
	cfg := ProductionPacemakerConfig(5 * time.Second)

	assert.Equal(t, 5*time.Second, cfg.TimeoutPropose)
	assert.Equal(t, 2500*time.Millisecond, cfg.TimeoutVote)
	assert.Equal(t, 5*time.Second, cfg.TimeoutCommit)
	assert.Equal(t, 5*time.Second, cfg.MinBlockTime, "MinBlockTime should match target block time")
	assert.Equal(t, 500*time.Millisecond, cfg.MinBlockTimeSkew)
}

// TestPacemakerConfig_Demo tests demo configuration.
func TestPacemakerConfig_Demo(t *testing.T) {
	cfg := DemoPacemakerConfig()

	assert.Equal(t, 2*time.Second, cfg.TimeoutPropose)
	assert.Equal(t, 1*time.Second, cfg.TimeoutVote)
	assert.Equal(t, 1*time.Second, cfg.TimeoutCommit)
	assert.Equal(t, 1*time.Second, cfg.MinBlockTime, "MinBlockTime should enforce ~1 block/sec")
	assert.Equal(t, 200*time.Millisecond, cfg.MinBlockTimeSkew)
}

// TestPacemakerConfig_Validate tests configuration validation.
func TestPacemakerConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     PacemakerConfig
		wantErr bool
	}{
		{
			name:    "valid default",
			cfg:     DefaultPacemakerConfig(),
			wantErr: false,
		},
		{
			name: "zero propose timeout",
			cfg: PacemakerConfig{
				TimeoutPropose:    0,
				TimeoutVote:       1 * time.Second,
				BackoffMultiplier: 1.5,
				MaxTimeout:        10 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "negative commit timeout",
			cfg: PacemakerConfig{
				TimeoutPropose:    1 * time.Second,
				TimeoutVote:       1 * time.Second,
				TimeoutCommit:     -1 * time.Second,
				BackoffMultiplier: 1.5,
				MaxTimeout:        10 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "backoff < 1",
			cfg: PacemakerConfig{
				TimeoutPropose:    1 * time.Second,
				TimeoutVote:       1 * time.Second,
				BackoffMultiplier: 0.5,
				MaxTimeout:        10 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "max < propose",
			cfg: PacemakerConfig{
				TimeoutPropose:    10 * time.Second,
				TimeoutVote:       1 * time.Second,
				BackoffMultiplier: 1.5,
				MaxTimeout:        5 * time.Second,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestPacemaker_Start tests pacemaker initialization.
func TestPacemaker_Start(t *testing.T) {
	mockTimer := timer.NewMockTimer()
	logger := zap.NewNop()
	var timeoutCalled bool

	pm := NewPacemaker(mockTimer, logger, func(view uint32) {
		timeoutCalled = true
	})

	pm.Start(0)

	// Timer should be running
	assert.True(t, mockTimer.IsRunning(), "timer should be started")
	assert.Equal(t, uint64(1000), mockTimer.Duration(), "should start with 1000ms timeout")
	assert.False(t, timeoutCalled, "timeout callback should not be called yet")
}

// TestPacemaker_Stop tests pacemaker shutdown.
func TestPacemaker_Stop(t *testing.T) {
	mockTimer := timer.NewMockTimer()
	logger := zap.NewNop()

	pm := NewPacemaker(mockTimer, logger, nil)
	pm.Start(0)

	require.True(t, mockTimer.IsRunning())

	pm.Stop()

	assert.False(t, mockTimer.IsRunning(), "timer should be stopped")
}

// TestPacemaker_OnProgress tests that progress resets the timer.
func TestPacemaker_OnProgress(t *testing.T) {
	mockTimer := timer.NewMockTimer()
	logger := zap.NewNop()

	pm := NewPacemaker(mockTimer, logger, nil)
	pm.Start(0)

	initialDuration := mockTimer.Duration()

	// Simulate progress
	pm.OnProgress(0)

	// Timer should be reset with same duration
	assert.Equal(t, initialDuration, mockTimer.Duration(), "timeout should remain the same after progress")
	assert.True(t, mockTimer.IsRunning(), "timer should still be running")
}

// TestPacemaker_OnTimeout tests timeout callback invocation.
func TestPacemaker_OnTimeout(t *testing.T) {
	mockTimer := timer.NewMockTimer()
	logger := zap.NewNop()

	var calledView uint32
	var callCount int
	var mu sync.Mutex

	pm := NewPacemaker(mockTimer, logger, func(view uint32) {
		mu.Lock()
		calledView = view
		callCount++
		mu.Unlock()
	})

	pm.Start(0)

	// Manually trigger timeout
	pm.OnTimeout(0)

	mu.Lock()
	assert.Equal(t, uint32(0), calledView, "timeout callback should receive correct view")
	assert.Equal(t, 1, callCount, "callback should be called once")
	mu.Unlock()

	// Trigger another timeout with different view
	pm.OnTimeout(1)

	mu.Lock()
	assert.Equal(t, uint32(1), calledView, "timeout callback should receive updated view")
	assert.Equal(t, 2, callCount, "callback should be called twice")
	mu.Unlock()
}

// TestPacemaker_IncreaseTimeout tests exponential backoff.
func TestPacemaker_IncreaseTimeout(t *testing.T) {
	mockTimer := timer.NewMockTimer()
	logger := zap.NewNop()

	pm := NewPacemaker(mockTimer, logger, nil)

	// Initial timeout: 1000ms (default)
	assert.Equal(t, uint64(1000), pm.CurrentTimeout())

	// First increase: 1000 * 1.5 = 1500ms
	pm.IncreaseTimeout()
	assert.Equal(t, uint64(1500), pm.CurrentTimeout())

	// Second increase: 1500 * 1.5 = 2250ms
	pm.IncreaseTimeout()
	assert.Equal(t, uint64(2250), pm.CurrentTimeout())

	// Third increase: 2250 * 1.5 = 3375ms
	pm.IncreaseTimeout()
	assert.Equal(t, uint64(3375), pm.CurrentTimeout())
}

// TestPacemaker_MaxTimeout tests that timeout is capped at maximum.
func TestPacemaker_MaxTimeout(t *testing.T) {
	mockTimer := timer.NewMockTimer()
	logger := zap.NewNop()

	pm := NewPacemaker(mockTimer, logger, nil)

	// Set to near-max value (default max is 30s)
	pm.currentTimeout = 25 * time.Second

	// Increase beyond max (25s * 1.5 = 37.5s)
	pm.IncreaseTimeout()

	// Should be capped at 30000ms
	assert.Equal(t, uint64(30000), pm.CurrentTimeout(), "timeout should be capped at 30s")

	// Further increases should keep it at max
	pm.IncreaseTimeout()
	assert.Equal(t, uint64(30000), pm.CurrentTimeout())
}

// TestPacemaker_ResetTimeout tests timeout reset after progress.
func TestPacemaker_ResetTimeout(t *testing.T) {
	mockTimer := timer.NewMockTimer()
	logger := zap.NewNop()

	pm := NewPacemaker(mockTimer, logger, nil)

	// Increase timeout multiple times
	pm.IncreaseTimeout()
	pm.IncreaseTimeout()
	pm.IncreaseTimeout()

	currentTimeout := pm.CurrentTimeout()
	require.Greater(t, currentTimeout, uint64(1000), "timeout should be increased")

	// Reset to base
	pm.ResetTimeout()

	assert.Equal(t, uint64(1000), pm.CurrentTimeout(), "timeout should reset to 1000ms")
}

// TestPacemaker_CustomConfig tests pacemaker with custom configuration.
func TestPacemaker_CustomConfig(t *testing.T) {
	mockTimer := timer.NewMockTimer()
	logger := zap.NewNop()

	config := PacemakerConfig{
		TimeoutPropose:    500 * time.Millisecond,
		TimeoutVote:       500 * time.Millisecond,
		TimeoutCommit:     1 * time.Second,
		BackoffMultiplier: 2.0,
		MaxTimeout:        10 * time.Second,
	}

	pm := NewPacemakerWithConfig(mockTimer, logger, nil, config)

	// Initial timeout should match config
	assert.Equal(t, uint64(500), pm.CurrentTimeout())

	// Backoff should use custom multiplier (2.0)
	pm.IncreaseTimeout()
	assert.Equal(t, uint64(1000), pm.CurrentTimeout())

	pm.IncreaseTimeout()
	assert.Equal(t, uint64(2000), pm.CurrentTimeout())
}

// TestPacemaker_OnCommit_WithDelay tests commit delay functionality.
func TestPacemaker_OnCommit_WithDelay(t *testing.T) {
	mockTimer := timer.NewMockTimer()
	logger := zap.NewNop()

	config := PacemakerConfig{
		TimeoutPropose:    1 * time.Second,
		TimeoutVote:       1 * time.Second,
		TimeoutCommit:     100 * time.Millisecond, // Short delay for test
		BackoffMultiplier: 1.5,
		MaxTimeout:        10 * time.Second,
	}

	pm := NewPacemakerWithConfig(mockTimer, logger, nil, config)

	// Increase timeout first
	pm.IncreaseTimeout()
	require.Greater(t, pm.CurrentTimeout(), uint64(1000))

	// OnCommit should reset backoff and return a channel
	done := pm.OnCommit(0)

	// Timeout should be reset to base
	assert.Equal(t, uint64(1000), pm.CurrentTimeout())

	// Wait for commit delay
	select {
	case <-done:
		// Success - delay completed
	case <-time.After(200 * time.Millisecond):
		t.Fatal("commit delay should have completed")
	}
}

// TestPacemaker_OnCommit_NoDelay tests immediate commit when TimeoutCommit is 0.
func TestPacemaker_OnCommit_NoDelay(t *testing.T) {
	mockTimer := timer.NewMockTimer()
	logger := zap.NewNop()

	// Default config has TimeoutCommit = 0
	pm := NewPacemaker(mockTimer, logger, nil)

	done := pm.OnCommit(0)

	// Should complete immediately
	select {
	case <-done:
		// Success - no delay
	default:
		t.Fatal("commit should complete immediately when TimeoutCommit is 0")
	}
}

// TestPacemaker_ConcurrentAccess tests thread-safety.
func TestPacemaker_ConcurrentAccess(t *testing.T) {
	mockTimer := timer.NewMockTimer()
	logger := zap.NewNop()

	var timeoutCount int
	var mu sync.Mutex

	pm := NewPacemaker(mockTimer, logger, func(view uint32) {
		mu.Lock()
		timeoutCount++
		mu.Unlock()
	})

	pm.Start(0)

	var wg sync.WaitGroup

	// Concurrent increases
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pm.IncreaseTimeout()
		}()
	}

	// Concurrent resets
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pm.ResetTimeout()
		}()
	}

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = pm.CurrentTimeout()
		}()
	}

	// Concurrent progress notifications
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(view uint32) {
			defer wg.Done()
			pm.OnProgress(view)
		}(uint32(i))
	}

	wg.Wait()

	// Should not crash or deadlock
	assert.True(t, true, "pacemaker should handle concurrent access")
}

// TestPacemaker_Integration_ViewChangeSequence tests a realistic view change sequence.
func TestPacemaker_Integration_ViewChangeSequence(t *testing.T) {
	mockTimer := timer.NewMockTimer()
	logger := zap.NewNop()

	viewChanges := make([]uint32, 0)
	var mu sync.Mutex

	pm := NewPacemaker(mockTimer, logger, func(view uint32) {
		mu.Lock()
		viewChanges = append(viewChanges, view)
		mu.Unlock()
	})

	// View 0: Start consensus
	pm.Start(0)
	assert.Equal(t, uint64(1000), pm.CurrentTimeout())

	// View 0: Timeout (leader failed)
	mockTimer.Fire()
	pm.OnTimeout(0)
	pm.IncreaseTimeout()
	assert.Equal(t, uint64(1500), pm.CurrentTimeout())

	// View 1: Start with increased timeout
	pm.Start(1)

	// View 1: Timeout again (new leader also failed)
	mockTimer.Fire()
	pm.OnTimeout(1)
	pm.IncreaseTimeout()
	assert.Equal(t, uint64(2250), pm.CurrentTimeout())

	// View 2: Start with further increased timeout
	pm.Start(2)

	// View 2: Success! Make progress and commit
	pm.OnProgress(2)
	pm.OnCommit(2) // This resets the timeout
	assert.Equal(t, uint64(1000), pm.CurrentTimeout())

	// Verify view change sequence
	mu.Lock()
	assert.Equal(t, []uint32{0, 1}, viewChanges, "should have triggered view changes for views 0 and 1")
	mu.Unlock()
}

// TestPacemaker_RapidViewChanges tests behavior under rapid consecutive timeouts.
func TestPacemaker_RapidViewChanges(t *testing.T) {
	mockTimer := timer.NewMockTimer()
	logger := zap.NewNop()

	views := make([]uint32, 0)
	var mu sync.Mutex

	pm := NewPacemaker(mockTimer, logger, func(view uint32) {
		mu.Lock()
		views = append(views, view)
		mu.Unlock()
	})

	// Simulate 10 rapid view changes
	for view := uint32(0); view < 10; view++ {
		pm.Start(view)
		mockTimer.Fire()
		pm.OnTimeout(view)
		pm.IncreaseTimeout()
	}

	mu.Lock()
	assert.Len(t, views, 10, "should handle 10 rapid view changes")
	mu.Unlock()

	// Timeout should be significantly increased
	assert.Greater(t, pm.CurrentTimeout(), uint64(5000), "timeout should be substantially increased after 10 failures")
}

// TestPacemaker_NoCallback tests pacemaker with nil callback.
func TestPacemaker_NoCallback(t *testing.T) {
	mockTimer := timer.NewMockTimer()
	logger := zap.NewNop()

	pm := NewPacemaker(mockTimer, logger, nil)
	pm.Start(0)

	// Should not crash with nil callback
	pm.OnTimeout(0)

	assert.True(t, true, "should handle nil callback gracefully")
}

// TestPacemaker_Config tests Config() method.
func TestPacemaker_Config(t *testing.T) {
	mockTimer := timer.NewMockTimer()
	logger := zap.NewNop()

	config := DemoPacemakerConfig()
	pm := NewPacemakerWithConfig(mockTimer, logger, nil, config)

	retrieved := pm.Config()
	assert.Equal(t, config.TimeoutPropose, retrieved.TimeoutPropose)
	assert.Equal(t, config.TimeoutCommit, retrieved.TimeoutCommit)
}

// =============================================================================
// Minimum Block Time Tests
// =============================================================================

// TestPacemakerConfig_MinBlockTime_Default tests that MinBlockTime is disabled by default.
func TestPacemakerConfig_MinBlockTime_Default(t *testing.T) {
	cfg := DefaultPacemakerConfig()

	assert.Equal(t, time.Duration(0), cfg.MinBlockTime, "MinBlockTime should be 0 (disabled) by default")
	assert.Equal(t, 500*time.Millisecond, cfg.MinBlockTimeSkew, "MinBlockTimeSkew should have default value")
}

// TestPacemakerConfig_MinBlockTime_Production tests that production config enables MinBlockTime.
func TestPacemakerConfig_MinBlockTime_Production(t *testing.T) {
	cfg := ProductionPacemakerConfig(5 * time.Second)

	assert.Equal(t, 5*time.Second, cfg.MinBlockTime, "MinBlockTime should match target block time")
	assert.Equal(t, 500*time.Millisecond, cfg.MinBlockTimeSkew, "MinBlockTimeSkew should be set")
}

// TestPacemakerConfig_MinBlockTime_Demo tests that demo config enables MinBlockTime.
func TestPacemakerConfig_MinBlockTime_Demo(t *testing.T) {
	cfg := DemoPacemakerConfig()

	assert.Equal(t, 1*time.Second, cfg.MinBlockTime, "MinBlockTime should be 1s for demo")
	assert.Equal(t, 200*time.Millisecond, cfg.MinBlockTimeSkew, "MinBlockTimeSkew should be smaller for demo")
}

// TestPacemakerConfig_MinBlockTime_Validate tests validation of MinBlockTime settings.
func TestPacemakerConfig_MinBlockTime_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     PacemakerConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid with MinBlockTime disabled",
			cfg: PacemakerConfig{
				TimeoutPropose:    1 * time.Second,
				TimeoutVote:       1 * time.Second,
				MinBlockTime:      0, // Disabled
				BackoffMultiplier: 1.5,
				MaxTimeout:        10 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "valid with MinBlockTime enabled",
			cfg: PacemakerConfig{
				TimeoutPropose:    1 * time.Second,
				TimeoutVote:       1 * time.Second,
				MinBlockTime:      1 * time.Second,
				MinBlockTimeSkew:  100 * time.Millisecond,
				BackoffMultiplier: 1.5,
				MaxTimeout:        10 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "negative MinBlockTime",
			cfg: PacemakerConfig{
				TimeoutPropose:    1 * time.Second,
				TimeoutVote:       1 * time.Second,
				MinBlockTime:      -1 * time.Second,
				BackoffMultiplier: 1.5,
				MaxTimeout:        10 * time.Second,
			},
			wantErr: true,
			errMsg:  "MinBlockTime",
		},
		{
			name: "negative MinBlockTimeSkew when MinBlockTime is set",
			cfg: PacemakerConfig{
				TimeoutPropose:    1 * time.Second,
				TimeoutVote:       1 * time.Second,
				MinBlockTime:      1 * time.Second,
				MinBlockTimeSkew:  -100 * time.Millisecond,
				BackoffMultiplier: 1.5,
				MaxTimeout:        10 * time.Second,
			},
			wantErr: true,
			errMsg:  "MinBlockTimeSkew",
		},
		{
			name: "skew >= MinBlockTime",
			cfg: PacemakerConfig{
				TimeoutPropose:    1 * time.Second,
				TimeoutVote:       1 * time.Second,
				MinBlockTime:      1 * time.Second,
				MinBlockTimeSkew:  1 * time.Second, // Same as MinBlockTime
				BackoffMultiplier: 1.5,
				MaxTimeout:        10 * time.Second,
			},
			wantErr: true,
			errMsg:  "MinBlockTimeSkew",
		},
		{
			name: "skew > MinBlockTime",
			cfg: PacemakerConfig{
				TimeoutPropose:    1 * time.Second,
				TimeoutVote:       1 * time.Second,
				MinBlockTime:      1 * time.Second,
				MinBlockTimeSkew:  2 * time.Second, // Greater than MinBlockTime
				BackoffMultiplier: 1.5,
				MaxTimeout:        10 * time.Second,
			},
			wantErr: true,
			errMsg:  "MinBlockTimeSkew",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestPacemaker_MinBlockTimeEnabled tests the MinBlockTimeEnabled() helper.
func TestPacemaker_MinBlockTimeEnabled(t *testing.T) {
	mockTimer := timer.NewMockTimer()
	logger := zap.NewNop()

	// Default config: MinBlockTime disabled
	pm := NewPacemaker(mockTimer, logger, nil)
	assert.False(t, pm.MinBlockTimeEnabled(), "should be disabled with default config")

	// Production config: MinBlockTime enabled
	pm2 := NewPacemakerWithConfig(mockTimer, logger, nil, ProductionPacemakerConfig(5*time.Second))
	assert.True(t, pm2.MinBlockTimeEnabled(), "should be enabled with production config")

	// Custom config with MinBlockTime = 0
	cfg := PacemakerConfig{
		TimeoutPropose:    1 * time.Second,
		TimeoutVote:       1 * time.Second,
		MinBlockTime:      0,
		BackoffMultiplier: 1.5,
		MaxTimeout:        10 * time.Second,
	}
	pm3 := NewPacemakerWithConfig(mockTimer, logger, nil, cfg)
	assert.False(t, pm3.MinBlockTimeEnabled(), "should be disabled when MinBlockTime is 0")
}

// TestPacemaker_CanAcceptProposal_Disabled tests CanAcceptProposal when MinBlockTime is disabled.
func TestPacemaker_CanAcceptProposal_Disabled(t *testing.T) {
	mockTimer := timer.NewMockTimer()
	logger := zap.NewNop()

	// Default config has MinBlockTime = 0 (disabled)
	pm := NewPacemaker(mockTimer, logger, nil)

	// Should always accept proposals when MinBlockTime is disabled
	assert.True(t, pm.CanAcceptProposal(time.Time{}), "should accept with zero time")
	assert.True(t, pm.CanAcceptProposal(time.Now()), "should accept with current time")
	assert.True(t, pm.CanAcceptProposal(time.Now().Add(-1*time.Hour)), "should accept with old time")
}

// TestPacemaker_CanAcceptProposal_Enabled tests CanAcceptProposal when MinBlockTime is enabled.
func TestPacemaker_CanAcceptProposal_Enabled(t *testing.T) {
	mockTimer := timer.NewMockTimer()
	logger := zap.NewNop()

	cfg := PacemakerConfig{
		TimeoutPropose:    1 * time.Second,
		TimeoutVote:       1 * time.Second,
		MinBlockTime:      500 * time.Millisecond,
		MinBlockTimeSkew:  100 * time.Millisecond,
		BackoffMultiplier: 1.5,
		MaxTimeout:        10 * time.Second,
	}
	pm := NewPacemakerWithConfig(mockTimer, logger, nil, cfg)

	// Zero time (no previous commit) - should accept
	assert.True(t, pm.CanAcceptProposal(time.Time{}), "should accept with zero time (no previous commit)")

	// Old commit time (well past MinBlockTime) - should accept
	oldTime := time.Now().Add(-1 * time.Second)
	assert.True(t, pm.CanAcceptProposal(oldTime), "should accept when MinBlockTime has passed")

	// Very recent commit (within MinBlockTime - skew) - should reject
	recentTime := time.Now().Add(-100 * time.Millisecond) // Only 100ms ago
	assert.False(t, pm.CanAcceptProposal(recentTime), "should reject when too early")

	// Commit at threshold (MinBlockTime - skew = 400ms ago) - should accept
	thresholdTime := time.Now().Add(-400 * time.Millisecond)
	assert.True(t, pm.CanAcceptProposal(thresholdTime), "should accept at threshold")
}

// TestPacemaker_WaitForMinBlockTime_Disabled tests WaitForMinBlockTime when disabled.
func TestPacemaker_WaitForMinBlockTime_Disabled(t *testing.T) {
	mockTimer := timer.NewMockTimer()
	logger := zap.NewNop()

	// Default config has MinBlockTime = 0 (disabled)
	pm := NewPacemaker(mockTimer, logger, nil)

	start := time.Now()
	pm.WaitForMinBlockTime(time.Now()) // Should return immediately
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 50*time.Millisecond, "should return immediately when disabled")
}

// TestPacemaker_WaitForMinBlockTime_ZeroCommitTime tests WaitForMinBlockTime with zero commit time.
func TestPacemaker_WaitForMinBlockTime_ZeroCommitTime(t *testing.T) {
	mockTimer := timer.NewMockTimer()
	logger := zap.NewNop()

	cfg := ProductionPacemakerConfig(5 * time.Second)
	pm := NewPacemakerWithConfig(mockTimer, logger, nil, cfg)

	start := time.Now()
	pm.WaitForMinBlockTime(time.Time{}) // Zero time = no previous commit
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 50*time.Millisecond, "should return immediately with zero commit time")
}

// TestPacemaker_WaitForMinBlockTime_AlreadyPast tests WaitForMinBlockTime when time has passed.
func TestPacemaker_WaitForMinBlockTime_AlreadyPast(t *testing.T) {
	mockTimer := timer.NewMockTimer()
	logger := zap.NewNop()

	cfg := PacemakerConfig{
		TimeoutPropose:    1 * time.Second,
		TimeoutVote:       1 * time.Second,
		MinBlockTime:      100 * time.Millisecond,
		MinBlockTimeSkew:  20 * time.Millisecond,
		BackoffMultiplier: 1.5,
		MaxTimeout:        10 * time.Second,
	}
	pm := NewPacemakerWithConfig(mockTimer, logger, nil, cfg)

	// Last commit was 200ms ago, MinBlockTime is 100ms
	lastCommit := time.Now().Add(-200 * time.Millisecond)

	start := time.Now()
	pm.WaitForMinBlockTime(lastCommit)
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 50*time.Millisecond, "should return immediately when past MinBlockTime")
}

// TestPacemaker_WaitForMinBlockTime_Waits tests that WaitForMinBlockTime actually waits.
func TestPacemaker_WaitForMinBlockTime_Waits(t *testing.T) {
	mockTimer := timer.NewMockTimer()
	logger := zap.NewNop()

	cfg := PacemakerConfig{
		TimeoutPropose:    1 * time.Second,
		TimeoutVote:       1 * time.Second,
		MinBlockTime:      150 * time.Millisecond, // Short for test
		MinBlockTimeSkew:  20 * time.Millisecond,
		BackoffMultiplier: 1.5,
		MaxTimeout:        10 * time.Second,
	}
	pm := NewPacemakerWithConfig(mockTimer, logger, nil, cfg)

	// Last commit just now
	lastCommit := time.Now()

	start := time.Now()
	pm.WaitForMinBlockTime(lastCommit)
	elapsed := time.Since(start)

	// Should have waited approximately MinBlockTime
	assert.GreaterOrEqual(t, elapsed, 140*time.Millisecond, "should wait for MinBlockTime")
	assert.Less(t, elapsed, 200*time.Millisecond, "should not wait too long")
}

// TestPacemaker_TimeUntilCanPropose tests the TimeUntilCanPropose helper.
func TestPacemaker_TimeUntilCanPropose(t *testing.T) {
	mockTimer := timer.NewMockTimer()
	logger := zap.NewNop()

	cfg := PacemakerConfig{
		TimeoutPropose:    1 * time.Second,
		TimeoutVote:       1 * time.Second,
		MinBlockTime:      500 * time.Millisecond,
		MinBlockTimeSkew:  100 * time.Millisecond,
		BackoffMultiplier: 1.5,
		MaxTimeout:        10 * time.Second,
	}
	pm := NewPacemakerWithConfig(mockTimer, logger, nil, cfg)

	// Disabled case
	pmDisabled := NewPacemaker(mockTimer, logger, nil)
	assert.Equal(t, time.Duration(0), pmDisabled.TimeUntilCanPropose(time.Now()))

	// Zero commit time
	assert.Equal(t, time.Duration(0), pm.TimeUntilCanPropose(time.Time{}))

	// Already past MinBlockTime
	oldTime := time.Now().Add(-1 * time.Second)
	assert.Equal(t, time.Duration(0), pm.TimeUntilCanPropose(oldTime))

	// Recent commit - should return remaining time
	recentTime := time.Now().Add(-200 * time.Millisecond)
	remaining := pm.TimeUntilCanPropose(recentTime)
	// MinBlockTime (500ms) - elapsed (200ms) = ~300ms remaining
	assert.Greater(t, remaining, 250*time.Millisecond)
	assert.Less(t, remaining, 350*time.Millisecond)
}

// TestPacemaker_MinBlockTime_ConcurrentAccess tests thread-safety of MinBlockTime methods.
func TestPacemaker_MinBlockTime_ConcurrentAccess(t *testing.T) {
	mockTimer := timer.NewMockTimer()
	logger := zap.NewNop()

	cfg := PacemakerConfig{
		TimeoutPropose:    1 * time.Second,
		TimeoutVote:       1 * time.Second,
		MinBlockTime:      50 * time.Millisecond, // Short for test
		MinBlockTimeSkew:  10 * time.Millisecond,
		BackoffMultiplier: 1.5,
		MaxTimeout:        10 * time.Second,
	}
	pm := NewPacemakerWithConfig(mockTimer, logger, nil, cfg)

	var wg sync.WaitGroup

	// Concurrent CanAcceptProposal calls
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pm.CanAcceptProposal(time.Now().Add(-100 * time.Millisecond))
		}()
	}

	// Concurrent TimeUntilCanPropose calls
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pm.TimeUntilCanPropose(time.Now())
		}()
	}

	// Concurrent MinBlockTimeEnabled calls
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pm.MinBlockTimeEnabled()
		}()
	}

	wg.Wait()
	// Should not crash or deadlock
}

// TestPacemaker_MinBlockTime_SkewBoundary tests edge cases around the skew boundary.
func TestPacemaker_MinBlockTime_SkewBoundary(t *testing.T) {
	mockTimer := timer.NewMockTimer()
	logger := zap.NewNop()

	cfg := PacemakerConfig{
		TimeoutPropose:    1 * time.Second,
		TimeoutVote:       1 * time.Second,
		MinBlockTime:      1 * time.Second,
		MinBlockTimeSkew:  200 * time.Millisecond,
		BackoffMultiplier: 1.5,
		MaxTimeout:        10 * time.Second,
	}
	pm := NewPacemakerWithConfig(mockTimer, logger, nil, cfg)

	// Threshold is MinBlockTime - Skew = 800ms
	// At exactly 800ms elapsed, should accept
	time800ms := time.Now().Add(-800 * time.Millisecond)
	assert.True(t, pm.CanAcceptProposal(time800ms), "should accept at threshold (800ms)")

	// At 799ms elapsed, should reject (just under threshold)
	time799ms := time.Now().Add(-799 * time.Millisecond)
	assert.False(t, pm.CanAcceptProposal(time799ms), "should reject just under threshold")

	// At 801ms elapsed, should accept (just over threshold)
	time801ms := time.Now().Add(-801 * time.Millisecond)
	assert.True(t, pm.CanAcceptProposal(time801ms), "should accept just over threshold")
}
