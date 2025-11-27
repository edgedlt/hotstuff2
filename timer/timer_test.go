package timer

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestRealTimerBasic tests basic RealTimer functionality.
func TestRealTimerBasic(t *testing.T) {
	timer := NewRealTimer()

	// Start timer with short duration
	timer.Start(50) // 50ms

	select {
	case <-timer.C():
		// Timer fired as expected
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timer did not fire within expected time")
	}
}

// TestRealTimerStop tests that Stop prevents timer from firing.
func TestRealTimerStop(t *testing.T) {
	timer := NewRealTimer()

	timer.Start(100) // 100ms
	timer.Stop()

	select {
	case <-timer.C():
		t.Fatal("timer should not fire after Stop()")
	case <-time.After(150 * time.Millisecond):
		// Expected - timer was stopped
	}
}

// TestRealTimerReset tests that Reset restarts the timer.
func TestRealTimerReset(t *testing.T) {
	timer := NewRealTimer()

	timer.Start(200) // 200ms
	time.Sleep(50 * time.Millisecond)
	timer.Reset(50) // Reset to 50ms

	start := time.Now()
	select {
	case <-timer.C():
		elapsed := time.Since(start)
		// Should fire around 50ms, not 150ms remaining from original
		assert.Less(t, elapsed, 120*time.Millisecond, "timer should have reset")
	case <-time.After(300 * time.Millisecond):
		t.Fatal("timer did not fire after reset")
	}
}

// TestRealTimerMultipleStarts tests that Start can be called multiple times.
func TestRealTimerMultipleStarts(t *testing.T) {
	timer := NewRealTimer()

	timer.Start(100)
	timer.Start(50) // Should replace the first timer

	start := time.Now()
	select {
	case <-timer.C():
		elapsed := time.Since(start)
		assert.Less(t, elapsed, 120*time.Millisecond, "second Start should replace first")
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timer did not fire")
	}
}

// TestRealTimerConcurrency tests that RealTimer is safe for concurrent use.
func TestRealTimerConcurrency(t *testing.T) {
	timer := NewRealTimer()
	const N = 100

	var wg sync.WaitGroup
	wg.Add(N)

	// Start timer in background
	timer.Start(100)

	// Hammer it with concurrent operations
	for i := range N {
		go func(idx int) {
			defer wg.Done()
			if idx%3 == 0 {
				timer.Start(50 + uint64(idx%50))
			} else if idx%3 == 1 {
				timer.Stop()
			} else {
				timer.Reset(50 + uint64(idx%50))
			}
		}(i)
	}

	wg.Wait()
	// If we get here without panic, test passes
}

// TestRealTimerChannelNonBlocking tests that channel sends don't block.
func TestRealTimerChannelNonBlocking(t *testing.T) {
	timer := NewRealTimer()

	// Start and let it fire
	timer.Start(10)
	time.Sleep(50 * time.Millisecond)

	// Start again - should not block even if channel is full
	done := make(chan bool, 1)
	go func() {
		timer.Start(10)
		done <- true
	}()

	select {
	case <-done:
		// Good - Start didn't block
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Start blocked when channel was full")
	}
}

// TestMockTimerBasic tests basic MockTimer functionality.
func TestMockTimerBasic(t *testing.T) {
	timer := NewMockTimer()

	// Start timer
	timer.Start(1000)

	// Should be running
	assert.True(t, timer.IsRunning())
	assert.Equal(t, uint64(1000), timer.Duration())

	// Should not fire automatically
	select {
	case <-timer.C():
		t.Fatal("MockTimer should not fire automatically")
	case <-time.After(50 * time.Millisecond):
		// Expected - manual control only
	}

	// Manually fire
	timer.Fire()

	// Should receive on channel
	select {
	case <-timer.C():
		// Expected
	case <-time.After(50 * time.Millisecond):
		t.Fatal("Fire() did not send to channel")
	}
}

// TestMockTimerStop tests that Stop prevents firing.
func TestMockTimerStop(t *testing.T) {
	timer := NewMockTimer()

	timer.Start(1000)
	assert.True(t, timer.IsRunning())

	timer.Stop()
	assert.False(t, timer.IsRunning())

	// Fire should not send to channel when stopped
	timer.Fire()

	select {
	case <-timer.C():
		t.Fatal("Fire() should not send when timer is stopped")
	case <-time.After(50 * time.Millisecond):
		// Expected
	}
}

// TestMockTimerReset tests that Reset updates duration and state.
func TestMockTimerReset(t *testing.T) {
	timer := NewMockTimer()

	timer.Start(1000)
	assert.Equal(t, uint64(1000), timer.Duration())

	timer.Reset(2000)
	assert.True(t, timer.IsRunning())
	assert.Equal(t, uint64(2000), timer.Duration())
}

// TestMockTimerMultipleFires tests that Fire can be called multiple times.
func TestMockTimerMultipleFires(t *testing.T) {
	timer := NewMockTimer()
	timer.Start(1000)

	// First fire
	timer.Fire()
	select {
	case <-timer.C():
		// Good
	case <-time.After(50 * time.Millisecond):
		t.Fatal("first Fire() did not send")
	}

	// Second fire should also work (channel buffered, doesn't block)
	timer.Fire()
	select {
	case <-timer.C():
		// Good
	case <-time.After(50 * time.Millisecond):
		t.Fatal("second Fire() did not send")
	}
}

// TestMockTimerChannelDrain tests that channel is drained on Start/Stop.
func TestMockTimerChannelDrain(t *testing.T) {
	timer := NewMockTimer()

	timer.Start(1000)
	timer.Fire()

	// Drain by starting again
	timer.Start(2000)

	// Channel should be empty now
	select {
	case <-timer.C():
		t.Fatal("channel should have been drained by Start")
	case <-time.After(10 * time.Millisecond):
		// Expected
	}
}

// TestAdaptiveTimerBasic tests basic AdaptiveTimer functionality.
func TestAdaptiveTimerBasic(t *testing.T) {
	config := AdaptiveConfig{
		BaseDuration:  100,
		MaxDuration:   1000,
		BackoffFactor: 2.0,
	}
	timer := NewAdaptiveTimer(config)

	// Should start at base duration
	assert.Equal(t, uint64(100), timer.CurrentDuration())

	// Start timer
	timer.Start(999) // Duration parameter is ignored

	select {
	case <-timer.C():
		// Timer fired
	case <-time.After(300 * time.Millisecond):
		t.Fatal("timer did not fire")
	}

	// Duration should still be base
	assert.Equal(t, uint64(100), timer.CurrentDuration())
}

// TestAdaptiveTimerBackoff tests exponential backoff on timeout.
func TestAdaptiveTimerBackoff(t *testing.T) {
	config := AdaptiveConfig{
		BaseDuration:  100,
		MaxDuration:   1000,
		BackoffFactor: 2.0,
	}
	timer := NewAdaptiveTimer(config)

	assert.Equal(t, uint64(100), timer.CurrentDuration())

	// Simulate timeout
	timer.OnTimeout()
	assert.Equal(t, uint64(200), timer.CurrentDuration())

	// Timeout again
	timer.OnTimeout()
	assert.Equal(t, uint64(400), timer.CurrentDuration())

	// Timeout again
	timer.OnTimeout()
	assert.Equal(t, uint64(800), timer.CurrentDuration())

	// Timeout again - should cap at max
	timer.OnTimeout()
	assert.Equal(t, uint64(1000), timer.CurrentDuration())

	// Further timeouts should not increase beyond max
	timer.OnTimeout()
	assert.Equal(t, uint64(1000), timer.CurrentDuration())
}

// TestAdaptiveTimerProgress tests that OnProgress resets to base.
func TestAdaptiveTimerProgress(t *testing.T) {
	config := AdaptiveConfig{
		BaseDuration:  100,
		MaxDuration:   1000,
		BackoffFactor: 2.0,
	}
	timer := NewAdaptiveTimer(config)

	// Increase timeout
	timer.OnTimeout()
	timer.OnTimeout()
	assert.Equal(t, uint64(400), timer.CurrentDuration())

	// Signal progress
	timer.OnProgress()
	assert.Equal(t, uint64(100), timer.CurrentDuration())
}

// TestAdaptiveTimerWithMock tests AdaptiveTimer with MockTimer underlying.
func TestAdaptiveTimerWithMock(t *testing.T) {
	config := AdaptiveConfig{
		BaseDuration:  100,
		MaxDuration:   1000,
		BackoffFactor: 1.5,
	}
	mock := NewMockTimer()
	timer := NewAdaptiveTimerWithUnderlying(config, mock)

	// Start should use base duration
	timer.Start(999) // Ignored
	assert.True(t, mock.IsRunning())
	assert.Equal(t, uint64(100), mock.Duration())

	// Increase backoff
	timer.OnTimeout()

	// Reset should use new duration
	timer.Reset(999) // Ignored
	assert.Equal(t, uint64(150), mock.Duration())
}

// TestAdaptiveTimerDefaultConfig tests the default configuration.
func TestAdaptiveTimerDefaultConfig(t *testing.T) {
	config := DefaultAdaptiveConfig()

	assert.Equal(t, uint64(100), config.BaseDuration)
	assert.Equal(t, uint64(10000), config.MaxDuration)
	assert.Equal(t, 1.5, config.BackoffFactor)
}

// TestAdaptiveTimerStop tests that Stop works correctly.
func TestAdaptiveTimerStop(t *testing.T) {
	config := DefaultAdaptiveConfig()
	timer := NewAdaptiveTimer(config)

	timer.Start(100)
	timer.Stop()

	// Channel should not receive after stop
	select {
	case <-timer.C():
		t.Fatal("timer fired after Stop()")
	case <-time.After(200 * time.Millisecond):
		// Expected
	}
}

// TestAdaptiveTimerBackoffFractional tests backoff with fractional factor.
func TestAdaptiveTimerBackoffFractional(t *testing.T) {
	config := AdaptiveConfig{
		BaseDuration:  100,
		MaxDuration:   500,
		BackoffFactor: 1.5,
	}
	timer := NewAdaptiveTimer(config)

	assert.Equal(t, uint64(100), timer.CurrentDuration())

	timer.OnTimeout()
	assert.Equal(t, uint64(150), timer.CurrentDuration())

	timer.OnTimeout()
	assert.Equal(t, uint64(225), timer.CurrentDuration())

	timer.OnTimeout()
	assert.Equal(t, uint64(337), timer.CurrentDuration())

	timer.OnTimeout()
	assert.Equal(t, uint64(500), timer.CurrentDuration()) // Capped at max
}

// TestAdaptiveTimerConcurrency tests that AdaptiveTimer is thread-safe.
func TestAdaptiveTimerConcurrency(t *testing.T) {
	config := DefaultAdaptiveConfig()
	timer := NewAdaptiveTimer(config)

	var wg sync.WaitGroup
	const N = 100

	wg.Add(N)
	for i := range N {
		go func(idx int) {
			defer wg.Done()
			switch idx % 5 {
			case 0:
				timer.Start(100)
			case 1:
				timer.Stop()
			case 2:
				timer.Reset(100)
			case 3:
				timer.OnTimeout()
			case 4:
				timer.OnProgress()
			}
		}(i)
	}

	wg.Wait()
	// If no panic, test passes
}

// TestTimerInterface tests that all implementations satisfy Timer interface.
func TestTimerInterface(t *testing.T) {
	var _ Timer = (*RealTimer)(nil)
	var _ Timer = (*MockTimer)(nil)
	var _ Timer = (*AdaptiveTimer)(nil)
}

// BenchmarkRealTimerStart benchmarks RealTimer.Start.
func BenchmarkRealTimerStart(b *testing.B) {
	timer := NewRealTimer()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		timer.Start(1000)
	}
}

// BenchmarkRealTimerStartStop benchmarks RealTimer Start/Stop cycle.
func BenchmarkRealTimerStartStop(b *testing.B) {
	timer := NewRealTimer()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		timer.Start(1000)
		timer.Stop()
	}
}

// BenchmarkMockTimerFire benchmarks MockTimer.Fire.
func BenchmarkMockTimerFire(b *testing.B) {
	timer := NewMockTimer()
	timer.Start(1000)

	// Drain channel in background
	done := make(chan bool)
	go func() {
		for range timer.C() {
			// Drain
		}
		done <- true
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		timer.Fire()
	}

	timer.Stop()
	<-done
}

// BenchmarkAdaptiveTimerOnTimeout benchmarks AdaptiveTimer.OnTimeout.
func BenchmarkAdaptiveTimerOnTimeout(b *testing.B) {
	config := DefaultAdaptiveConfig()
	timer := NewAdaptiveTimer(config)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		timer.OnTimeout()
		if i%10 == 0 {
			timer.OnProgress() // Reset occasionally to avoid always hitting max
		}
	}
}
