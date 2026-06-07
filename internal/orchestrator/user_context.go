// ============================================================
// MODULE: orchestrator
// Deskripsi: Multi-account trading orchestration
// ============================================================

package orchestrator

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/nzf210/nafas-bot/internal/auth"
	"github.com/nzf210/nafas-bot/internal/logger"
	"github.com/nzf210/nafas-bot/internal/models"
	"github.com/shopspring/decimal"
)

// UserContext holds all data needed for a single user's trading execution.
// Includes decrypted credentials (cached with TTL to avoid repeated decryption).
// Nama Function: UserContext
// Deskripsi: Struct yang menyimpan semua data untuk trading execution satu user.
// Parameter/Value Input:
//   - User: *models.User — user model dari database
//   - APIKey: string — API key yang sudah di-decrypt
//   - APISecret: string — API secret yang sudah di-decrypt
//   - Passphrase: string — passphrase untuk OKX (nullable)
//   - Config: *models.UserConfig — konfigurasi risk user
//   - Pairs: []models.TradingPair — trading pairs yang diaktifkan user
//   - fetchedAt: time.Time — timestamp kapan context di-fetch
// Function yang Dipanggil/Dikonsumsi:
//   - GetDecryptedCredentials: dipanggil untuk ambil kredensial
//   - GetAPIKeyModel: dipanggil untuk ambil API key model
// Output/Return Value:
//   - UserContext: struct context untuk satu user
type UserContext struct {
	User       *models.User
	APIKey     string
	APISecret  string
	Passphrase string
	Config     *models.UserConfig
	Pairs      []models.TradingPair
	Exchange   string
	fetchedAt  time.Time
}

// Cache TTL: 5 minutes untuk avoid repeated decryption
const userContextCacheTTL = 5 * time.Minute

// userContextCache caches UserContext per userID with TTL
type userContextCache struct {
	mu      sync.RWMutex
	context map[string]*UserContext
}

// Global cache instance
var cache = &userContextCache{
	context: make(map[string]*UserContext),
}

// GetUserContext retrieves or creates UserContext for a user.
// Uses cache with TTL to avoid repeated API key decryption.
// Nama Function: GetUserContext
// Deskripsi: Mengambil atau membuat UserContext untuk satu user.
//   Menggunakan cache dengan TTL untuk avoid repeated decryption.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk database operation
//   - db: *sql.DB — koneksi database
//   - user: *models.User — user yang akan di-get context-nya
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryRowContext: dipanggil untuk ambil API key dari database
//   - auth.Decrypt: dipanggil untuk decrypt API key dan secret
//   - db.QueryContext: dipanggil untuk ambil user config dan pairs
// Output/Return Value:
//   - *UserContext: context untuk user (cached)
//   - error: error jika gagal mengambil data
func GetUserContext(ctx context.Context, db *sql.DB, user *models.User) (*UserContext, error) {
	userID := user.ID.String()

	// Check cache first
	cache.mu.RLock()
	if cached, ok := cache.context[userID]; ok {
		if time.Since(cached.fetchedAt) < userContextCacheTTL {
			cache.mu.RUnlock()
			return cached, nil
		}
	}
	cache.mu.RUnlock()

	// Build fresh context
	userCtx, err := buildUserContext(ctx, db, user)
	if err != nil {
		return nil, err
	}

	// Update cache
	cache.mu.Lock()
	cache.context[userID] = userCtx
	cache.mu.Unlock()

	return userCtx, nil
}

// buildUserContext constructs UserContext from database
func buildUserContext(ctx context.Context, db *sql.DB, user *models.User) (*UserContext, error) {
	logg := logger.Default().WithField("module", "orchestrator")

	// Get API key
	var apiKey models.APIKey
	err := db.QueryRowContext(ctx, `
		SELECT encrypted_api_key, encrypted_api_secret, encrypted_passphrase, exchange
		FROM api_keys
		WHERE user_id = $1 AND is_active = true
	`, user.ID).Scan(&apiKey.EncryptedAPIKey, &apiKey.EncryptedAPISecret, &apiKey.EncryptedPassphrase, &apiKey.Exchange)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // No API key configured
		}
		return nil, err
	}

	// Decrypt credentials
	apiKeyStr, err := auth.Decrypt(apiKey.EncryptedAPIKey)
	if err != nil {
		logg.WithError(err).WithField("user_id", user.ID).Warn("Failed to decrypt API key")
		return nil, err
	}

	apiSecret, err := auth.Decrypt(apiKey.EncryptedAPISecret)
	if err != nil {
		logg.WithError(err).WithField("user_id", user.ID).Warn("Failed to decrypt API secret")
		return nil, err
	}

	var passphrase string
	if apiKey.EncryptedPassphrase != nil {
		passphrase, err = auth.Decrypt(*apiKey.EncryptedPassphrase)
		if err != nil {
			logg.WithError(err).WithField("user_id", user.ID).Warn("Failed to decrypt passphrase")
			return nil, err
		}
	}

	// Get user config
	var config models.UserConfig
	err = db.QueryRowContext(ctx, `
		SELECT COALESCE(max_risk_per_trade, 1.0), COALESCE(max_allocation_per_trade, 10.0), COALESCE(daily_loss_limit, 5.0), max_open_positions,
			   notify_on_trade, notify_on_error, auto_trade_enabled
		FROM user_configs WHERE user_id = $1
	`, user.ID).Scan(&config.MaxRiskPerTrade, &config.MaxAllocationPerTrade, &config.DailyLossLimit, &config.MaxOpenPositions,
		&config.NotifyOnTrade, &config.NotifyOnError, &config.AutoTradeEnabled)

	if err != nil {
		if err != sql.ErrNoRows {
			logg.WithError(err).WithField("user_id", user.ID).Warn("Failed to get user config, using defaults")
		}
		config.MaxRiskPerTrade = decimal.NewFromFloat(1.0)
		config.MaxAllocationPerTrade = decimal.NewFromFloat(10.0)
		config.DailyLossLimit = decimal.NewFromFloat(5.0)
		config.MaxOpenPositions = 3
		config.NotifyOnTrade = true
		config.NotifyOnError = true
		config.AutoTradeEnabled = false
	}

	// Get trading pairs
	rows, err := db.QueryContext(ctx, `
		SELECT id, symbol, base_asset, quote_asset, priority, enabled
		FROM trading_pairs WHERE user_id = $1 AND enabled = true
		ORDER BY priority DESC
	`, user.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pairs []models.TradingPair
	for rows.Next() {
		var pair models.TradingPair
		if err := rows.Scan(&pair.ID, &pair.Symbol, &pair.BaseAsset, &pair.QuoteAsset, &pair.Priority, &pair.Enabled); err != nil {
			continue
		}
		pairs = append(pairs, pair)
	}

	return &UserContext{
		User:       user,
		APIKey:     apiKeyStr,
		APISecret:  apiSecret,
		Passphrase: passphrase,
		Config:     &config,
		Pairs:      pairs,
		Exchange:   apiKey.Exchange,
		fetchedAt:  time.Now(),
	}, nil
}

// InvalidateCache removes a user's context from cache.
// Call this when user updates API key or config.
// Nama Function: InvalidateCache
// Deskripsi: Menghapus context user dari cache.
//   Panggil ini ketika user update API key atau konfigurasi.
// Parameter/Value Input:
//   - userID: string — user ID yang akan di-invalidate
// Output/Return Value:
//   - Tidak ada return value langsung
func InvalidateCache(userID string) {
	cache.mu.Lock()
	delete(cache.context, userID)
	cache.mu.Unlock()
}

// ClearCache clears all cached user contexts
func ClearCache() {
	cache.mu.Lock()
	cache.context = make(map[string]*UserContext)
	cache.mu.Unlock()
}
