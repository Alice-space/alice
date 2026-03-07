package governance

import (
	"sync"
	"time"
)

type CircuitState string

const (
	CircuitClosed   CircuitState = "closed"
	CircuitOpen     CircuitState = "open"
	CircuitHalfOpen CircuitState = "half_open"
)

type CircuitConfig struct {
	FailureThreshold int
	Window           time.Duration
	Cooldown         time.Duration
	ProbeLimit       int
}

type CircuitBreaker struct {
	mu sync.Mutex

	cfg CircuitConfig

	state          CircuitState
	windowStart    time.Time
	windowFailures int
	openedAt       time.Time
	probeCount     int
}

func NewCircuitBreaker(cfg CircuitConfig) *CircuitBreaker {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 5
	}
	if cfg.Window <= 0 {
		cfg.Window = 1 * time.Minute
	}
	if cfg.Cooldown <= 0 {
		cfg.Cooldown = 30 * time.Second
	}
	if cfg.ProbeLimit <= 0 {
		cfg.ProbeLimit = 1
	}
	return &CircuitBreaker{cfg: cfg, state: CircuitClosed, windowStart: time.Now().UTC()}
}

func (c *CircuitBreaker) Allow(now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch c.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		if now.Sub(c.openedAt) >= c.cfg.Cooldown {
			c.state = CircuitHalfOpen
			c.probeCount = 0
			return true
		}
		return false
	case CircuitHalfOpen:
		if c.probeCount >= c.cfg.ProbeLimit {
			return false
		}
		c.probeCount++
		return true
	default:
		return false
	}
}

func (c *CircuitBreaker) RecordSuccess(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == CircuitHalfOpen {
		c.state = CircuitClosed
		c.windowFailures = 0
		c.windowStart = now
		c.probeCount = 0
		return
	}
	if now.Sub(c.windowStart) > c.cfg.Window {
		c.windowStart = now
		c.windowFailures = 0
	}
}

func (c *CircuitBreaker) RecordFailure(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if now.Sub(c.windowStart) > c.cfg.Window {
		c.windowStart = now
		c.windowFailures = 0
	}
	c.windowFailures++

	if c.state == CircuitHalfOpen {
		c.state = CircuitOpen
		c.openedAt = now
		return
	}
	if c.windowFailures >= c.cfg.FailureThreshold {
		c.state = CircuitOpen
		c.openedAt = now
	}
}

func (c *CircuitBreaker) State() CircuitState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}
