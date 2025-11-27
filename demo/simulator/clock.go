// Package simulator provides a local network simulation for HotStuff-2 consensus.
package simulator

import (
	"sync"
	"time"
)

// Clock tracks elapsed time for the simulation.
// Unlike DeterministicClock, this uses real time - it just tracks when the sim started.
type Clock struct {
	mu        sync.RWMutex
	startTime time.Time
	running   bool
}

// NewClock creates a new clock.
func NewClock() *Clock {
	return &Clock{}
}

// Start starts tracking time.
func (c *Clock) Start() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running {
		c.startTime = time.Now()
		c.running = true
	}
}

// Stop stops the clock.
func (c *Clock) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.running = false
}

// Reset resets the clock.
func (c *Clock) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.startTime = time.Now()
}

// Now returns elapsed milliseconds since start.
func (c *Clock) Now() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.running {
		return 0
	}
	return uint64(time.Since(c.startTime).Milliseconds())
}

// IsRunning returns true if the clock is running.
func (c *Clock) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.running
}
