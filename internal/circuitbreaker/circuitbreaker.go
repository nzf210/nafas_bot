// ============================================================
// MODULE: circuitbreaker
// Deskripsi: Circuit breaker pattern untuk mencegah cascade failures
// ============================================================

package circuitbreaker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/nzf210/nafas-bot/internal/logger"
)

// State represents circuit breaker state
type State int

const (
	StateClosed State = iota // Normal operation
	StateOpen               // Failing, reject requests
	StateHalfOpen           // Testing if service recovered
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreaker prevents cascade failures by opening circuit on repeated errors
// Nama Function: CircuitBreaker
// Deskripsi: Circuit breaker untuk prevent cascade failures dari exchange API calls.
// Parameter/Value Input:
//   - failureThreshold: int — jumlah failure sebelum circuit open (default 5)
//   - resetTimeout: time.Duration — durasi sebelum coba lagi (default 30s)
//   - halfOpenMaxCalls: int — max calls yang diizinkan dalam half-open state (default 3)
// Function yang Dipanggil/Dikonsumsi:
//   - Execute: dipanggil untuk execute operation dengan circuit breaker protection
// Output/Return Value:
//   - CircuitBreaker: struct yang ready digunakan
type CircuitBreaker struct {
	mu              sync.RWMutex
	name            string
	state           State
	failures        int
	successes       int
	failureThreshold int
	resetTimeout    time.Duration
	halfOpenMaxCalls int
	lastFailureTime time.Time
	lastStateChange time.Time
	logger          *logger.Logger
}

// New creates a new circuit breaker
// Nama Function: New
// Deskripsi: Membuat instance circuit breaker baru.
// Parameter/Value Input:
//   - name: string — nama circuit breaker untuk logging
//   - options: ...Option — optional configuration
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung, hanya inisialisasi
// Output/Return Value:
//   - *CircuitBreaker: pointer ke circuit breaker instance
func New(name string, options ...Option) *CircuitBreaker {
	cb := &CircuitBreaker{
		name:             name,
		state:            StateClosed,
		failureThreshold: 5,
		resetTimeout:     30 * time.Second,
		halfOpenMaxCalls: 3,
		logger:           logger.Default().WithField("circuit_breaker", name),
	}

	for _, opt := range options {
		opt(cb)
	}

	return cb
}

// Option is a functional option for CircuitBreaker
type Option func(*CircuitBreaker)

// WithFailureThreshold sets the failure threshold
func WithFailureThreshold(n int) Option {
	return func(cb *CircuitBreaker) {
		cb.failureThreshold = n
	}
}

// WithResetTimeout sets the reset timeout
func WithResetTimeout(d time.Duration) Option {
	return func(cb *CircuitBreaker) {
		cb.resetTimeout = d
	}
}

// WithHalfOpenMaxCalls sets the max calls in half-open state
func WithHalfOpenMaxCalls(n int) Option {
	return func(cb *CircuitBreaker) {
		cb.halfOpenMaxCalls = n
	}
}

// Execute runs the function with circuit breaker protection
// Nama Function: Execute
// Deskripsi: Menjalankan operation dengan circuit breaker protection.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk cancellation
//   - fn: func() error — operation yang akan dijalankan
// Function yang Dipanggil/Dikonsumsi:
//   - State(): dipanggil untuk cek current state
//   - recordSuccess/recordFailure: dipanggil untuk update state
// Output/Return Value:
//   - error: error jika circuit open atau operation gagal
// Catatan: Jika circuit open, langsung return error tanpa menjalankan fn
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func() error) error {
	state := cb.currentState()

	switch state {
	case StateOpen:
		// Check if reset timeout has passed
		if time.Since(cb.lastFailureTime) >= cb.resetTimeout {
			cb.transitionTo(StateHalfOpen)
			cb.logger.Info("Circuit transitioning to half-open after reset timeout")
		} else {
			return fmt.Errorf("circuit breaker open, retry after %v", time.Until(cb.lastFailureTime.Add(cb.resetTimeout)))
		}

	case StateHalfOpen:
		cb.mu.RLock()
		if cb.successes >= cb.halfOpenMaxCalls {
			cb.mu.RUnlock()
			// Allow through, will transition based on result
		} else {
			cb.mu.RUnlock()
		}
	}

	// Execute the function
	err := fn()
	if err != nil {
		cb.recordFailure()
		return err
	}

	cb.recordSuccess()
	return nil
}

// currentState returns the current circuit state
func (cb *CircuitBreaker) currentState() State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// recordSuccess records a successful call
func (cb *CircuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == StateHalfOpen {
		cb.successes++
		if cb.successes >= cb.halfOpenMaxCalls {
			cb.transitionToLocked(StateClosed)
			cb.logger.Info("Circuit closed after successful half-open calls")
		}
	} else {
		// Reset failure count on success in closed state
		cb.failures = 0
	}
}

// recordFailure records a failed call
func (cb *CircuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lastFailureTime = time.Now()

	switch cb.state {
	case StateClosed:
		cb.failures++
		if cb.failures >= cb.failureThreshold {
			cb.transitionToLocked(StateOpen)
			cb.logger.Warnf("Circuit opened after %d failures", cb.failures)
		}

	case StateHalfOpen:
		// Any failure in half-open state opens the circuit
		cb.transitionToLocked(StateOpen)
		cb.logger.Warn("Circuit opened due to failure in half-open state")
	}
}

// transitionTo changes circuit state (must hold lock if called from Lock)
// transitionToLocked assumes lock is already held
func (cb *CircuitBreaker) transitionTo(state State) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.transitionToLocked(state)
}

func (cb *CircuitBreaker) transitionToLocked(state State) {
	cb.state = state
	cb.lastStateChange = time.Now()

	if state == StateClosed {
		cb.failures = 0
		cb.successes = 0
	} else if state == StateHalfOpen {
		cb.successes = 0
	}
}

// State returns current circuit state
func (cb *CircuitBreaker) State() State {
	return cb.currentState()
}

// Stats returns circuit breaker statistics
type Stats struct {
	State           string
	Failures        int
	Successes       int
	LastFailureTime time.Time
	Uptime          time.Duration
}

// GetStats returns current statistics
func (cb *CircuitBreaker) GetStats() Stats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return Stats{
		State:           cb.state.String(),
		Failures:        cb.failures,
		Successes:       cb.successes,
		LastFailureTime: cb.lastFailureTime,
		Uptime:          time.Since(cb.lastStateChange),
	}
}

// IsAvailable returns true if circuit allows requests
func (cb *CircuitBreaker) IsAvailable() bool {
	state := cb.currentState()
	return state == StateClosed || state == StateHalfOpen
}