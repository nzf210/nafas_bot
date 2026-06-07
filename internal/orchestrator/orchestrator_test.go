// ============================================================
// MODULE: orchestrator
// Deskripsi: Unit tests untuk multi-account orchestrator
// ============================================================

package orchestrator

import (
	"testing"
	"time"
)

// TestUserContextCache tests the user context caching mechanism
func TestUserContextCache(t *testing.T) {
	// Clear cache before test
	ClearCache()

	// Verify cache is empty
	cache.mu.RLock()
	if len(cache.context) != 0 {
		t.Error("Cache should be empty after ClearCache")
	}
	cache.mu.RUnlock()
}

// TestInvalidateCache tests cache invalidation
func TestInvalidateCache(t *testing.T) {
	// Clear cache first
	ClearCache()

	// Invalidate non-existent user should not panic
	InvalidateCache("non-existent-user-id")

	// Cache should still be empty
	cache.mu.RLock()
	if len(cache.context) != 0 {
		t.Error("Cache should be empty after invalidating non-existent user")
	}
	cache.mu.RUnlock()
}

// TestClearCache tests cache clearing
func TestClearCache(t *testing.T) {
	// Add some dummy data to cache (simulated)
	cache.mu.Lock()
	cache.context["user1"] =&UserContext{fetchedAt: time.Now()}
	cache.context["user2"] = &UserContext{fetchedAt: time.Now()}
	cache.mu.Unlock()

	// Verify data was added
	cache.mu.RLock()
	if len(cache.context) != 2 {
		t.Errorf("Expected 2 cached items, got %d", len(cache.context))
	}
	cache.mu.RUnlock()

	// Clear cache
	ClearCache()

	// Verify cache is empty
	cache.mu.RLock()
	if len(cache.context) != 0 {
		t.Error("Cache should be empty after ClearCache")
	}
	cache.mu.RUnlock()
}

// TestUserContextCacheTTL tests that cached contexts respect TTL
func TestUserContextCacheTTL(t *testing.T) {
	ClearCache()

	// Create a context with old timestamp
	oldContext := &UserContext{
		fetchedAt: time.Now().Add(-10 * time.Minute), // 10 minutes ago
	}

	// Add to cache
	cache.mu.Lock()
	cache.context["old-user"] = oldContext
	cache.mu.Unlock()

	// Verify it's in cache
	cache.mu.RLock()
	if cache.context["old-user"] == nil {
		t.Error("Old context should be in cache")
	}
	cache.mu.RUnlock()

	// The context should be considered stale (fetchedAt > 5 min ago)
	// when GetUserContext is called, it should rebuild
	if time.Since(oldContext.fetchedAt) < userContextCacheTTL {
		t.Error("Test setup error: context should be stale")
	}
}

// TestMaxConcurrentUsersConstant tests that constants are set correctly
func TestMaxConcurrentUsersConstant(t *testing.T) {
	if MaxConcurrentUsers != 5 {
		t.Errorf("MaxConcurrentUsers should be 5, got %d", MaxConcurrentUsers)
	}
}

