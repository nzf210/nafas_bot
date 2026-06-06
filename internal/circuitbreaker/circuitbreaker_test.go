// ============================================================
// MODULE: circuitbreaker
// Deskripsi: Unit tests untuk circuit breaker
// ============================================================

package circuitbreaker

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCircuitBreaker_ClosedState(t *testing.T) {
	cb := New("test", WithFailureThreshold(3))

	// Should allow calls in closed state
	if !cb.IsAvailable() {
		t.Error("Expected circuit to be available in closed state")
	}

	if cb.State() != StateClosed {
		t.Errorf("Expected state Closed, got %v", cb.State())
	}
}

func TestCircuitBreaker_OpensAfterFailures(t *testing.T) {
	cb := New("test", WithFailureThreshold(3), WithResetTimeout(1*time.Millisecond))

	// Record 3 failures
	for i := 0; i < 3; i++ {
		cb.Execute(context.Background(), func() error {
			return errors.New("test error")
		})
	}

	if cb.State() != StateOpen {
		t.Errorf("Expected state Open after 3 failures, got %v", cb.State())
	}
}

func TestCircuitBreaker_AllowsAfterResetTimeout(t *testing.T) {
	cb := New("test", WithFailureThreshold(1), WithResetTimeout(10*time.Millisecond), WithHalfOpenMaxCalls(1))

	// Open the circuit
	cb.Execute(context.Background(), func() error {
		return errors.New("test error")
	})

	if cb.State() != StateOpen {
		t.Error("Expected circuit to be open")
	}

	// Wait for reset timeout
	time.Sleep(15 * time.Millisecond)

	// Make a new call to trigger transition to half-open and succeed
	// With halfOpenMaxCalls=1, one success should close the circuit
	_ = cb.Execute(context.Background(), func() error {
		return nil // success
	})

	// Should transition to half-open and then close after success
	if cb.State() != StateClosed {
		t.Errorf("Expected state Closed after successful half-open call, got %v", cb.State())
	}
}

func TestCircuitBreaker_ClosesAfterHalfOpenSuccesses(t *testing.T) {
	cb := New("test", WithFailureThreshold(1), WithResetTimeout(5*time.Millisecond), WithHalfOpenMaxCalls(2))

	// Open the circuit
	cb.Execute(context.Background(), func() error {
		return errors.New("test error")
	})

	// Wait for reset timeout
	time.Sleep(10 * time.Millisecond)

	// First call triggers transition to half-open and should succeed
	err := cb.Execute(context.Background(), func() error {
		return nil
	})
	if err != nil {
		t.Errorf("First half-open call should succeed, got: %v", err)
	}

	// Second call should also succeed and close the circuit
	err = cb.Execute(context.Background(), func() error {
		return nil
	})
	if err != nil {
		t.Errorf("Second half-open call should succeed, got: %v", err)
	}

	if cb.State() != StateClosed {
		t.Errorf("Expected state Closed after 2 successes in half-open, got %v", cb.State())
	}
}

func TestCircuitBreaker_FailureInHalfOpenReopens(t *testing.T) {
	cb := New("test", WithFailureThreshold(1), WithResetTimeout(1*time.Millisecond), WithHalfOpenMaxCalls(3))

	// Open the circuit
	cb.Execute(context.Background(), func() error {
		return errors.New("test error")
	})

	// Wait for reset timeout
	time.Sleep(5 * time.Millisecond)

	// Should be in half-open state
	cb.Execute(context.Background(), func() error {
		return errors.New("test error in half-open")
	})

	if cb.State() != StateOpen {
		t.Errorf("Expected state Open after failure in half-open, got %v", cb.State())
	}
}

func TestCircuitBreaker_RejectsWhenOpen(t *testing.T) {
	cb := New("test", WithFailureThreshold(1), WithResetTimeout(1*time.Hour))

	// Open the circuit
	cb.Execute(context.Background(), func() error {
		return errors.New("test error")
	})

	// Try to execute - should return error without calling fn
	fnCalled := false
	err := cb.Execute(context.Background(), func() error {
		fnCalled = true
		return nil
	})

	if err == nil {
		t.Error("Expected error when circuit is open")
	}
	if fnCalled {
		t.Error("Function should not be called when circuit is open")
	}
}

func TestCircuitBreaker_GetStats(t *testing.T) {
	cb := New("test", WithFailureThreshold(3))

	stats := cb.GetStats()

	if stats.State != "closed" {
		t.Errorf("Expected state 'closed', got %v", stats.State)
	}
	if stats.Failures != 0 {
		t.Errorf("Expected 0 failures, got %v", stats.Failures)
	}
}

func TestCircuitBreaker_ResetsOnSuccess(t *testing.T) {
	cb := New("test", WithFailureThreshold(3))

	// Record 2 failures
	for i := 0; i < 2; i++ {
		cb.Execute(context.Background(), func() error {
			return errors.New("test error")
		})
	}

	// Record a success
	cb.Execute(context.Background(), func() error {
		return nil
	})

	// Should still be closed and failures reset
	if cb.State() != StateClosed {
		t.Errorf("Expected state Closed, got %v", cb.State())
	}

	stats := cb.GetStats()
	if stats.Failures != 0 {
		t.Errorf("Expected failures to be reset to 0, got %v", stats.Failures)
	}
}