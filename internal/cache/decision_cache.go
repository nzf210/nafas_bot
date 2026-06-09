// ============================================================
// MODULE: cache
// Deskripsi: Redis-based persistent cache untuk AI decisions
// Mempertahankan cache keputusan AI antar restart
// ============================================================

package cache

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
)

// DecisionCache adalah Redis-based cache untuk AI trading decisions.
// Cache bertahan antar restart dan mendukung TTL adaptif berdasarkan confidence.
type DecisionCache struct {
	client *redis.Client
}

// NewDecisionCache creates a new Redis-based decision cache.
// Nama Function: NewDecisionCache
// Deskripsi: Membuat instance decision cache dengan Redis backend.
// Parameter/Value Input:
//   - addr: string — Redis server address (e.g., "localhost:6379")
//   - password: string — Redis password (empty jika tidak ada)
//   - db: int — Redis database number
// Output/Return Value:
//   - *DecisionCache: pointer ke cache instance
// Catatan: Perlu panggil Close() saat tidak digunakan
func NewDecisionCache(addr, password string, db int) *DecisionCache {
	return &DecisionCache{
		client: redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: password,
			DB:       db,
		}),
	}
}

// Get retrieves a cached decision by symbol.
// Nama Function: Get
// Deskripsi: Mengambil decision yang sudah di-cache dari Redis.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk Redis operation
//   - symbol: string — trading symbol (e.g., "BTCUSDT")
// Output/Return Value:
//   - *CachedDecision: decision yang di-cache, nil jika tidak ada
//   - error: error jika Redis operation gagal
func (c *DecisionCache) Get(ctx context.Context, symbol string) (*CachedDecision, error) {
	data, err := c.client.Get(ctx, cacheKey(symbol)).Bytes()
	if err == redis.Nil {
		return nil, nil // Not found
	}
	if err != nil {
		return nil, err
	}

	var cached CachedDecision
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, err
	}

	return &cached, nil
}

// Set stores a decision in Redis with adaptive TTL.
// TTL berdasarkan confidence level:
//   - confidence >= 90: 15 menit
//   - confidence >= 80: 10 menit
//   - default: 7 menit
// Nama Function: Set
// Deskripsi: Menyimpan decision ke Redis dengan TTL adaptif.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk Redis operation
//   - symbol: string — trading symbol
//   - decision: *CachedDecision — decision yang akan di-cache
// Output/Return Value:
//   - error: error jika Redis operation gagal
func (c *DecisionCache) Set(ctx context.Context, symbol string, decision *CachedDecision) error {
	data, err := json.Marshal(decision)
	if err != nil {
		return err
	}

	ttl := getAdaptiveTTL(decision.Confidence)
	return c.client.Set(ctx, cacheKey(symbol), data, ttl).Err()
}

// SetWithTTL stores a decision with custom TTL.
// Nama Function: SetWithTTL
// Deskripsi: Menyimpan decision dengan TTL custom.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk Redis operation
//   - symbol: string — trading symbol
//   - decision: *CachedDecision — decision yang akan di-cache
//   - ttl: time.Duration — custom TTL
// Output/Return Value:
//   - error: error jika Redis operation gagal
func (c *DecisionCache) SetWithTTL(ctx context.Context, symbol string, decision *CachedDecision, ttl time.Duration) error {
	data, err := json.Marshal(decision)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, cacheKey(symbol), data, ttl).Err()
}

// Delete removes a decision from cache.
// Nama Function: Delete
// Deskripsi: Menghapus decision dari cache.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk Redis operation
//   - symbol: string — trading symbol
// Output/Return Value:
//   - error: error jika Redis operation gagal
func (c *DecisionCache) Delete(ctx context.Context, symbol string) error {
	return c.client.Del(ctx, cacheKey(symbol)).Err()
}

// Exists checks if a decision exists in cache.
// Nama Function: Exists
// Deskripsi: Mengecek apakah decision ada di cache.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk Redis operation
//   - symbol: string — trading symbol
// Output/Return Value:
//   - bool: true jika decision ada di cache
//   - error: error jika Redis operation gagal
func (c *DecisionCache) Exists(ctx context.Context, symbol string) (bool, error) {
	n, err := c.client.Exists(ctx, cacheKey(symbol)).Result()
	return n > 0, err
}

// Close closes the Redis connection.
func (c *DecisionCache) Close() error {
	return c.client.Close()
}

// Ping checks Redis connection.
func (c *DecisionCache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

// cacheKey generates the Redis key for a symbol.
func cacheKey(symbol string) string {
	return "nafas:ai:" + symbol
}

// getAdaptiveTTL returns TTL based on confidence level.
func getAdaptiveTTL(confidence decimal.Decimal) time.Duration {
	conf, _ := confidence.Float64()
	switch {
	case conf >= 90:
		return 15 * time.Minute
	case conf >= 80:
		return 10 * time.Minute
	default:
		return 7 * time.Minute
	}
}

// CachedDecision represents a cached AI decision for persistence.
type CachedDecision struct {
	TradeDecision string            `json:"trade_decision"`
	Confidence    decimal.Decimal   `json:"confidence"`
	Regime        string            `json:"regime"`
	Sentiment     string            `json:"sentiment"`
	RiskLevel     string            `json:"risk_level"`
	StopLoss      float64           `json:"stop_loss,omitempty"`
	TakeProfit    []float64         `json:"take_profit,omitempty"`
	CachedAt      time.Time         `json:"cached_at"`
	Source        string            `json:"source"` // "llm" or "rule_based"
}