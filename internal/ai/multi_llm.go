// ============================================================
// MODULE: ai
// Deskripsi: Multi-LLM client dengan Main + Fallback support
// Mendukung automatic failover ke LLM backup jika Main LLM gagal
// ============================================================

package ai

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/nzf210/nafas-bot/internal/logger"
	"github.com/shopspring/decimal"
)

// MultiLLMClient adalah LLM client dengan Main + Fallback support.
// Jika Main LLM gagal, akan otomatis switch ke Fallback LLM.
// Nama Function: MultiLLMClient
// Deskripsi: LLM client dengan Main + Fallback support untuk high availability.
//   - Main LLM digunakan sebagai primary (e.g., GPT-4o)
//   - Fallback LLM digunakan sebagai backup (e.g., GPT-4o-mini atau Ollama)
//   - Automatic failover jika Main LLM timeout/error/502/503/529
//
// Parameter/Value Input:
//   - mainClient: *LLMClient — primary LLM client
//   - fallbackClient: *LLMClient — fallback LLM client (optional)
//   - logger: *logger.Logger — logger instance
//
// Function yang Dipanggil/Dikonsumsi:
//   - Decide: dipanggil untuk generate trading decision
//   - HealthCheck: dipanggil untuk cek koneksi provider
//   - GetActiveProvider: dipanggil untuk cek provider mana yang aktif
//
// Output/Return Value:
//   - MultiLLMClient: struct client dengan failover support
type MultiLLMClient struct {
	mainClient     *LLMClient
	fallbackClient *LLMClient
	logger         *logger.Logger
	mu             sync.RWMutex
	activeProvider string // "main" atau "fallback"
	fallbackUsed   int64  // counter untuk metrics
}

// NewMultiLLMClient creates a new Multi-LLM client with Main + Fallback support.
// Nama Function: NewMultiLLMClient
// Deskripsi: Membuat instance Multi-LLM client dengan Main + Fallback.
// Parameter/Value Input:
//   - mainBaseURL: string — Main LLM provider base URL
//   - mainAPIKey: string — Main LLM API key
//   - mainModel: string — Main LLM model name
//   - mainTemperature: float64 — sampling temperature
//   - mainMaxTokens: int — max response tokens
//   - fallbackBaseURL: string — Fallback LLM provider base URL (optional, "" = no fallback)
//   - fallbackAPIKey: string — Fallback LLM API key
//   - fallbackModel: string — Fallback LLM model name
//   - fallbackTemperature: float64 — fallback sampling temperature
//   - fallbackMaxTokens: int — fallback max response tokens
// Output/Return Value:
//   - *MultiLLMClient: pointer ke client
func NewMultiLLMClient(
	mainBaseURL, mainAPIKey, mainModel string, mainTemperature float64, mainMaxTokens int,
	fallbackBaseURL, fallbackAPIKey, fallbackModel string, fallbackTemperature float64, fallbackMaxTokens int,
) *MultiLLMClient {
	log := logger.Default().WithField("module", "ai/multi_llm")

	mainClient := NewLLMClient(mainBaseURL, mainAPIKey, mainModel, mainTemperature, mainMaxTokens)

	var fbClient *LLMClient
	if fallbackBaseURL != "" {
		fbClient = NewLLMClient(fallbackBaseURL, fallbackAPIKey, fallbackModel, fallbackTemperature, fallbackMaxTokens)
		log.Infof("Fallback LLM configured: %s/%s", fallbackBaseURL, fallbackModel)
	}

	return &MultiLLMClient{
		mainClient:     mainClient,
		fallbackClient: fbClient,
		logger:         log,
		activeProvider: "main",
	}
}

// Decide generates a trading decision using Main LLM with automatic fallback to Fallback LLM.
// Nama Function: Decide
// Deskripsi: Generate trading decision dengan Main LLM, auto-fallback ke Fallback jika gagal.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk HTTP request
//   - symbol: string — trading symbol (e.g., BTCUSDT)
//   - marketData: map[string]any — market data dari scanner
//   - systemPrompt: string — system prompt (unused, prompt built internally)
// Function yang Dipanggil/Dikonsumsi:
//   - mainClient.Decide: dipanggil untuk Main LLM decision
//   - fallbackClient.Decide: dipanggil sebagai fallback jika Main gagal
// Output/Return Value:
//   - *CoordinatorDecision: keputusan trading dalam format CoordinatorDecision
//   - error: error jika semua LLM gagal
func (c *MultiLLMClient) Decide(ctx context.Context, symbol string, marketData map[string]any, systemPrompt string) (*CoordinatorDecision, error) {
	// Try Main LLM first
	decision, err := c.mainClient.Decide(ctx, symbol, marketData, systemPrompt)
	if err == nil {
		return decision, nil
	}

	c.logger.WithError(err).WithField("symbol", symbol).Warn("Main LLM failed, trying fallback")

	// Check if fallback is available
	c.mu.RLock()
	hasFallback := c.fallbackClient != nil
	c.mu.RUnlock()

	if !hasFallback {
		return nil, fmt.Errorf("main LLM failed and no fallback configured: %w", err)
	}

	// Try Fallback LLM
	fbDecision, fbErr := c.fallbackClient.Decide(ctx, symbol, marketData, systemPrompt)
	if fbErr != nil {
		c.logger.WithError(fbErr).WithField("symbol", symbol).Error("Both Main and Fallback LLM failed")
		return nil, fmt.Errorf("main LLM failed (%w), fallback LLM also failed: %v", err, fbErr)
	}

	// Update metrics
	c.mu.Lock()
	c.activeProvider = "fallback"
	c.fallbackUsed++
	c.mu.Unlock()

	c.logger.WithField("symbol", symbol).
		WithField("provider", "fallback").
		WithField("decision", fbDecision.TradeDecision).
		Info("Fallback LLM decision generated successfully")

	return fbDecision, nil
}

// HealthCheck checks health of both Main and Fallback LLM providers.
// Nama Function: HealthCheck
// Deskripsi: Memeriksa kesehatan kedua provider LLM.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk HTTP request
// Output/Return Value:
//   - bool: true jika minimal satu provider tersedia
//   - error: error detail jika semua provider down
func (c *MultiLLMClient) HealthCheck(ctx context.Context) (bool, error) {
	// Check Main LLM
	mainOK, mainErr := c.mainClient.HealthCheck(ctx)
	if mainOK {
		c.mu.Lock()
		c.activeProvider = "main"
		c.mu.Unlock()
		return true, nil
	}

	// Check Fallback LLM
	c.mu.RLock()
	hasFallback := c.fallbackClient != nil
	c.mu.RUnlock()

	if hasFallback {
		fbOK, fbErr := c.fallbackClient.HealthCheck(ctx)
		if fbOK {
			c.mu.Lock()
			c.activeProvider = "fallback"
			c.mu.Unlock()
			return true, nil
		}
		return false, fmt.Errorf("main LLM error: %v, fallback LLM error: %v", mainErr, fbErr)
	}

	return false, fmt.Errorf("main LLM error: %v", mainErr)
}

// GetActiveProvider returns which provider is currently active.
// Nama Function: GetActiveProvider
// Deskripsi: Mengambil informasi provider mana yang sedang aktif.
// Output/Return Value:
//   - string: "main" atau "fallback"
func (c *MultiLLMClient) GetActiveProvider() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.activeProvider
}

// GetFallbackUsedCount returns how many times fallback was used.
// Nama Function: GetFallbackUsedCount
// Deskripsi: Mengambil jumlah penggunaan fallback untuk metrics.
// Output/Return Value:
//   - int64: jumlah penggunaan fallback
func (c *MultiLLMClient) GetFallbackUsedCount() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.fallbackUsed
}

// Stats returns current MultiLLM stats.
// Nama Function: Stats
// Deskripsi: Mengambil statistik MultiLLM client.
// Output/Return Value:
//   - map[string]any: statistik provider
func (c *MultiLLMClient) Stats() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return map[string]any{
		"active_provider": c.activeProvider,
		"fallback_used":   c.fallbackUsed,
		"has_fallback":    c.fallbackClient != nil,
	}
}

// ============================================================
// DECISION CACHE: In-Memory Cache dengan Unified TTL
// ============================================================

// DecisionCacheEntry dengan metadata untuk unified TTL
// Nama Function: DecisionCacheEntry
// Deskripsi: Cache entry untuk AI decisions dengan unified TTL management.
// Parameter/Value Input:
//   - decision: *CoordinatorDecision — AI decision
//   - expireAt: time.Time — expiration time
//   - createdAt: time.Time — creation time (untuk TTL calculation)
//   - provider: string — "main" atau "fallback"
type DecisionCacheEntry struct {
	Decision  *CoordinatorDecision
	ExpireAt  time.Time
	CreatedAt time.Time
	Provider  string
}

// DecisionCache manages AI decisions with unified TTL logic.
// Single source of truth untuk TTL calculation.
// Nama Function: DecisionCache
// Deskripsi: Cache manager untuk AI decisions dengan unified TTL.
//   - Menggunakan single TTL calculation function
//   - Support in-memory storage
//   - Adaptive TTL berdasarkan confidence level
type DecisionCache struct {
	mu      sync.RWMutex
	entries map[string]*DecisionCacheEntry
	maxTTL  time.Duration // dari config
}

// NewDecisionCache creates a new decision cache.
// Nama Function: NewDecisionCache
// Deskripsi: Membuat instance decision cache baru.
// Parameter/Value Input:
//   - maxTTL: time.Duration — max TTL dari config (AIDecisionCacheMaxTTL)
// Output/Return Value:
//   - *DecisionCache: pointer ke cache
func NewDecisionCache(maxTTL time.Duration) *DecisionCache {
	return &DecisionCache{
		entries: make(map[string]*DecisionCacheEntry),
		maxTTL:  maxTTL,
	}
}

// Get retrieves a cached decision if not expired.
// Nama Function: Get
// Deskripsi: Mengambil decision dari cache jika belum expired.
// Parameter/Value Input:
//   - symbol: string — trading symbol
// Output/Return Value:
//   - *CoordinatorDecision: decision jika ada dan belum expired
//   - bool: true jika decision ditemukan dan valid
//   - string: provider yang digunakan ("main" atau "fallback")
func (c *DecisionCache) Get(symbol string) (*CoordinatorDecision, bool, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.entries[symbol]
	if !exists {
		return nil, false, ""
	}

	// Check expiration
	if time.Now().After(entry.ExpireAt) {
		return nil, false, ""
	}

	return entry.Decision, true, entry.Provider
}

// Set stores a decision with adaptive TTL based on confidence.
// Nama Function: Set
// Deskripsi: Menyimpan decision ke cache dengan adaptive TTL.
//   - TTL dihitung berdasarkan confidence (getUnifiedTTL)
//   - High confidence = longer TTL (reduce LLM calls)
//   - Low confidence = shorter TTL (fresher decisions)
// Parameter/Value Input:
//   - symbol: string — trading symbol
//   - decision: *CoordinatorDecision — AI decision
//   - provider: string — "main" atau "fallback"
func (c *DecisionCache) Set(symbol string, decision *CoordinatorDecision, provider string) {
	ttl := getUnifiedTTL(decision.Confidence, c.maxTTL)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[symbol] = &DecisionCacheEntry{
		Decision:  decision,
		ExpireAt:  time.Now().Add(ttl),
		CreatedAt: time.Now(),
		Provider:  provider,
	}
}

// SetWithTTL stores a decision with explicit TTL.
// Nama Function: SetWithTTL
// Deskripsi: Menyimpan decision dengan TTL yang spesifik.
// Parameter/Value Input:
//   - symbol: string — trading symbol
//   - decision: *CoordinatorDecision — AI decision
//   - provider: string — "main" atau "fallback"
//   - ttl: time.Duration — custom TTL
func (c *DecisionCache) SetWithTTL(symbol string, decision *CoordinatorDecision, provider string, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[symbol] = &DecisionCacheEntry{
		Decision:  decision,
		ExpireAt:  time.Now().Add(ttl),
		CreatedAt: time.Now(),
		Provider:  provider,
	}
}

// Delete removes a decision from cache.
// Nama Function: Delete
// Deskripsi: Menghapus decision dari cache.
// Parameter/Value Input:
//   - symbol: string — trading symbol
func (c *DecisionCache) Delete(symbol string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, symbol)
}

// CleanExpired removes all expired entries.
// Nama Function: CleanExpired
// Deskripsi: Membersihkan semua entry yang sudah expired.
func (c *DecisionCache) CleanExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for symbol, entry := range c.entries {
		if now.After(entry.ExpireAt) {
			delete(c.entries, symbol)
		}
	}
}

// Size returns the number of cached decisions.
// Nama Function: Size
// Deskripsi: Mengambil jumlah decision yang di-cache.
// Output/Return Value:
//   - int: jumlah entries
func (c *DecisionCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// getUnifiedTTL is the SINGLE SOURCE OF TRUTH untuk TTL calculation.
// Menggantikan duplicate implementations di orchestrator.go dan cache/decision_cache.go.
// Nama Function: getUnifiedTTL
// Deskripsi: Menghitung TTL berdasarkan confidence level.
//
//	Higher confidence → cache lebih lama (mengurangi LLM calls).
//	Low confidence → refresh lebih sering agar keputusan di-update.
//	Max TTL diambil dari config (AIDecisionCacheMaxTTL).
//
// Parameter/Value Input:
//   - confidence: decimal.Decimal — confidence score (0-100) dari AI decision
//   - maxTTL: time.Duration — max TTL dari config
//
// Output/Return Value:
//   - time.Duration: TTL untuk cache entry
func getUnifiedTTL(confidence decimal.Decimal, maxTTL time.Duration) time.Duration {
	conf, _ := confidence.Float64()
	switch {
	case conf >= 90:
		return maxTTL                              // Very confident → max TTL
	case conf >= 75:
		return time.Duration(float64(maxTTL) * 0.5)  // Fairly confident → 50% max TTL
	case conf >= 60:
		return time.Duration(float64(maxTTL) * 0.25) // Moderate → 25% max TTL
	default:
		return time.Duration(float64(maxTTL) * 0.125) // Low confidence → 12.5% max TTL
	}
}