// ============================================================
// MODULE: orchestrator
// Deskripsi: Multi-account trading orchestration service
// ============================================================

package orchestrator

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/nzf210/nafas-bot/internal/ai"
	"github.com/nzf210/nafas-bot/internal/config"
	"github.com/nzf210/nafas-bot/internal/exchange"
	"github.com/nzf210/nafas-bot/internal/execution"
	"github.com/nzf210/nafas-bot/internal/logger"
	"github.com/nzf210/nafas-bot/internal/models"
	"github.com/nzf210/nafas-bot/internal/risk"
	"github.com/nzf210/nafas-bot/internal/scanner"
	"github.com/nzf210/nafas-bot/internal/strategy"
	"github.com/shopspring/decimal"
)

// Worker pool configuration
const (
	// MaxConcurrentUsers adalah max jumlah user yang diproses concurrently.
	// Dipilih 5 untuk avoid overwhelming exchange API rate limits.
	MaxConcurrentUsers = 5
)

// decisionCacheEntry stores AI decision with TTL
type decisionCacheEntry struct {
	decision *ai.CoordinatorDecision
	expireAt time.Time
}

// Orchestrator coordinates trading across multiple user accounts.
// Runs market scan once, then executes for each user with auto-trade enabled.
// Uses worker pool pattern to limit concurrent user processing.
// Nama Function: Orchestrator
// Deskripsi: Service utama untuk orchestrate trading multi-account.
//
//	Menjalankan market scan SEKALI, lalu execute untuk setiap user.
//
// Parameter/Value Input:
//   - db: *sql.DB — koneksi database
//   - exchange: exchange.Exchange — exchange client
//   - scanner: *scanner.Scanner — market scanner
//   - ai: ai.AIClient — AI decision client (LLM atau TradingAgents)
//   - engine: *strategy.StrategyEngine — strategy engine
//   - logger: *logger.Logger — logger instance
//
// Function yang Dipanggil/Dikonsumsi:
//   - Start: dipanggil untuk mulai background worker
//   - RunCycle: dipanggil untuk jalankan satu cycle trading
//   - ProcessUser: dipanggil untuk process satu user
//
// Output/Return Value:
//   - Orchestrator: struct orchestration service
type Orchestrator struct {
	db          *sql.DB
	exchange    exchange.Exchange
	scanner     *scanner.Scanner
	ai          ai.AIClient
	engine      *strategy.StrategyEngine
	logger      *logger.Logger
	stopCh      chan struct{}
	wg          sync.WaitGroup
	config      *config.Config
	pairManager *scanner.PairManager

	// AI decision cache — 1 LLM call per unique symbol (bukan per user)
	decisionCache    map[string]*decisionCacheEntry
	decisionCacheMu sync.RWMutex
}

// NewOrchestrator creates a new multi-account orchestrator
// Nama Function: NewOrchestrator
// Deskripsi: Membuat instance orchestrator baru.
// Parameter/Value Input:
//   - db: *sql.DB — koneksi database
//   - exch: exchange.Exchange — exchange client
//   - scan: *scanner.Scanner — market scanner
//   - taClient: ai.AIClient — AI decision client
//   - eng: *strategy.StrategyEngine — strategy engine
//   - cfg: *config.Config — application configuration
//   - pm: *scanner.PairManager — pair manager untuk sync user pairs ke scanner
//
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung, hanya inisialisasi
//
// Output/Return Value:
//   - *Orchestrator: pointer ke orchestrator instance
func NewOrchestrator(db *sql.DB, exch exchange.Exchange, scan *scanner.Scanner, taClient ai.AIClient, eng *strategy.StrategyEngine, cfg *config.Config, pm *scanner.PairManager) *Orchestrator {
	return &Orchestrator{
		db:          db,
		exchange:    exch,
		scanner:     scan,
		ai:          taClient,
		engine:      eng,
		logger:      logger.Default().WithField("module", "orchestrator"),
		stopCh:      make(chan struct{}),
		config:      cfg,
		pairManager: pm,
		decisionCache: make(map[string]*decisionCacheEntry),
	}
}

// Start starts the background trading cycle worker.
// Runs until Stop() is called or context is cancelled.
// Nama Function: Start
// Deskripsi: Memulai background worker untuk trading cycles.
//
//	Worker berjalan terus sampai Stop() dipanggil atau context cancelled.
//
// Parameter/Value Input:
//   - ctx: context.Context — context untuk cancellation
//
// Function yang Dipanggil/Dikonsumsi:
//   - RunCycle: dipanggil secara periodic sesuai CycleInterval
//
// Output/Return Value:
//   - Tidak ada return value langsung
func (o *Orchestrator) Start(ctx context.Context) {
	o.logger.Info("Starting orchestrator background worker")
	o.wg.Add(1)
	go o.worker(ctx)
}

// Stop gracefully stops the orchestrator worker
// Nama Function: Stop
// Deskripsi: Menghentikan orchestrator worker secara graceful.
// Parameter/Value Input:
//   - Tidak ada parameter input langsung
//
// Function yang Dipanggil/Dikonsumsi:
//   - close(o.stopCh): dipanggil untuk signal worker berhenti
//   - o.wg.Wait: dipanggil untuk wait worker selesai
//
// Output/Return Value:
//   - Tidak ada return value langsung
func (o *Orchestrator) Stop() {
	o.logger.Info("Stopping orchestrator worker...")
	close(o.stopCh)
	o.wg.Wait()
	o.logger.Info("Orchestrator worker stopped")
}

// worker is the background goroutine that runs trading cycles
func (o *Orchestrator) worker(ctx context.Context) {
	defer o.wg.Done()

	o.logger.Info("Orchestrator worker started")

	// Jalankan cycle pertama kali agar bot tidak terlihat stuck menunggu interval
	o.runCycle(ctx)

	for {
		// Baca interval dari config setiap iterasi agar perubahan .env langsung tercermin
		interval := o.config.ScannerCycleInterval
		if interval == 0 {
			interval = 7 * time.Minute
		}

		ticker := time.NewTicker(interval)
		o.logger.Infof("Orchestrator cycle interval: %v", interval)

		select {
		case <-ctx.Done():
			ticker.Stop()
			o.logger.Info("Orchestrator worker: context cancelled")
			return
		case <-o.stopCh:
			ticker.Stop()
			o.logger.Info("Orchestrator worker: stop signal received")
			return
		case <-ticker.C:
			ticker.Stop()
			o.runCycle(ctx)
		}
	}
}

// RunCycle executes one complete trading cycle for all active users.
// This is the main entry point for a trading cycle.
// Nama Function: RunCycle
// Deskripsi: Menjalankan satu complete trading cycle untuk semua active users.
//
//	Ini adalah entry point utama untuk satu trading cycle.
//
// Parameter/Value Input:
//   - ctx: context.Context — context untuk operasi
//
// Function yang Dipanggil/Dikonsumsi:
//   - GetActiveUsers: dipanggil untuk ambil list user aktif
//   - SyncUserPairsToScanner: dipanggil untuk sync user pairs ke scanner
//   - ScanMarket: dipanggil untuk scan market data
//   - ProcessUser: dipanggil untuk setiap user secara concurrent
//
// Output/Return Value:
//   - error: error jika cycle gagal total
func (o *Orchestrator) RunCycle(ctx context.Context) error {
	return o.runCycle(ctx)
}

func (o *Orchestrator) runCycle(ctx context.Context) error {
	// Add timeout for the entire cycle to prevent stuck cycles
	// 30 menit: cukup untuk scan 2000+ pairs + AI analysis semua user
	cycleCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	startTime := time.Now()
	o.logger.Info("Starting trading cycle")

	// Get all users with auto-trade enabled
	users := o.GetActiveUsers(cycleCtx)
	if len(users) == 0 {
		o.logger.Info("No active users with auto-trade enabled, skipping cycle")
		return nil
	}
	o.logger.Infof("Found %d users with auto-trade enabled", len(users))

	// Sync user pairs to scanner BEFORE market scan
	o.syncUserPairsToScanner()

	// Scan market data ONCE for all users
	marketData := o.ScanMarket(cycleCtx)
	if marketData == nil {
		o.logger.Warn("Market scan failed, skipping cycle")
		return fmt.Errorf("market scan failed")
	}
	o.logger.Infof("Market scan completed: %d symbols", len(marketData))

	// Pre-compute AI decisions ONCE per unique symbol (minimize token usage)
	// AI decisions di-cache dan reuse untuk semua user
	o.preComputeAIDecisions(cycleCtx, marketData)

	// Process each user with worker pool
	sem := make(chan struct{}, MaxConcurrentUsers)
	var wg sync.WaitGroup
	var cycleErr error

	for _, user := range users {
		wg.Add(1)
		go func(u models.User) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := o.ProcessUser(cycleCtx, u, marketData); err != nil {
				o.logger.WithError(err).WithField("user_id", u.ID).Warn("Failed to process user")
				cycleErr = err
			}
		}(user)
	}
	wg.Wait()

	duration := time.Since(startTime)
	o.logger.Infof("Trading cycle completed in %v", duration)

	return cycleErr
}

// syncUserPairsToScanner syncs all user pairs to the scanner before market scan.
// This ensures that user-configured pairs (e.g., SOLBTC, NEARBTC) are scanned
// along with the default system pairs.
// Nama Function: syncUserPairsToScanner
// Deskripsi: Mensinkronkan semua user pairs ke scanner sebelum market scan.
// Menggunakan PairManager sebagai authorative source untuk semua pair.
// Tidak perlu parameter karena membaca langsung dari pairManager.
//
// Parameter/Value Input:
//   - Tidak ada (menggunakan o.pairManager secara internal)
//
// Function yang Dipanggil/Dikonsumsi:
//   - pairManager.GetMasterPairs: dipanggil untuk ambil semua pair aktif
//   - scanner.AddSymbol: dipanggil untuk tambahkan symbol ke scanner
//
// Output/Return Value:
//   - Tidak ada return value langsung
func (o *Orchestrator) syncUserPairsToScanner() {
	if o.pairManager == nil {
		o.logger.Warn("PairManager not configured, skipping user pair sync")
		return
	}

	// Collect all unique pairs from PairManager (authoritative source)
	allPairs := make(map[string]bool)
	for exchangeName, symbols := range o.pairManager.GetMasterPairs() {
		for symbol := range symbols {
			allPairs[symbol] = true
		}
		o.logger.Debugf("PairManager %s has %d pairs", exchangeName, len(symbols))
	}

	// Add each unique pair to the scanner
	addedCount := 0
	for symbol := range allPairs {
		o.scanner.AddSymbol(symbol)
		addedCount++
	}

	if addedCount > 0 {
		o.logger.Infof("Synced %d pairs from PairManager to scanner", addedCount)
	} else {
		o.logger.Warn("No pairs found in PairManager — scanner will scan with empty symbol list")
	}
}

// GetActiveUsers retrieves all users with auto-trade enabled
// Nama Function: GetActiveUsers
// Deskripsi: Mengambil semua user dengan auto-trade enabled.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk database operation
//
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryContext: dipanggil untuk SELECT dari user_configs + users
//
// Output/Return Value:
//   - []models.User: list user aktif
func (o *Orchestrator) GetActiveUsers(ctx context.Context) []models.User {
	rows, err := o.db.QueryContext(ctx, `
		SELECT u.id, u.telegram_id, u.username, u.first_name, u.last_name,
			   u.status, u.wch_balance, u.created_at, u.updated_at
		FROM users u
		INNER JOIN user_configs uc ON u.id = uc.user_id
		WHERE uc.auto_trade_enabled = true AND u.status = 'active'
	`)
	if err != nil {
		o.logger.WithError(err).Warn("Failed to get active users")
		return nil
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var user models.User
		if err := rows.Scan(&user.ID, &user.TelegramID, &user.Username, &user.FirstName,
			&user.LastName, &user.Status, &user.WCHBalance, &user.CreatedAt, &user.UpdatedAt); err != nil {
			continue
		}
		users = append(users, user)
	}

	return users
}

// ScanMarket performs market scan once for all symbols
// Returns market data for all configured pairs
// Nama Function: ScanMarket
// Deskripsi: Melakukan market scan SEKALI untuk semua symbol.
//
//	Market data di-scan sekali dan digunakan untuk semua user.
//
// Parameter/Value Input:
//   - ctx: context.Context — context untuk HTTP request
//
// Function yang Dipanggil/Dikonsumsi:
//   - scanner.ScanAllPairs: dipanggil untuk scan semua pairs
//
// Output/Return Value:
//   - map[string]*scanner.MarketData: map symbol ke market data
func (o *Orchestrator) ScanMarket(ctx context.Context) map[string]*scanner.MarketData {
	marketDataMap := make(map[string]*scanner.MarketData)
	var mu sync.Mutex

	// Create context with timeout for scan
	// Timeout 15 menit untuk handle 2000+ pairs × 3 intervals × 10 concurrency
	// Kalkulasi: ~6000 HTTP calls / 10 concurrency ≈ 600 batches × ~150ms ≈ 90 detik ideal
	// Dengan retry + rate limit, 15 menit masih aman
	scanCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()

	// Run scan with callback
	err := o.scanner.ScanAllPairs(scanCtx, func(data scanner.MarketData) {
		mu.Lock()
		copied := data
		marketDataMap[data.Symbol] = &copied
		mu.Unlock()
	})
	if err != nil {
		o.logger.WithError(err).Warn("Market scan error")
	}

	return marketDataMap
}

// preComputeAIDecisions pre-computes AI decisions for all unique symbols ONCE.
// Ini mencegah duplicate LLM calls — cukup 1 call per symbol untuk semua user.
// Menggunakan worker pool untuk concurrent AI calls (max 5 concurrent).
// Nama Function: preComputeAIDecisions
// Deskripsi: Pre-compute AI decisions untuk semua unique symbols.
//
//	Menggunakan worker pool pattern untuk concurrent AI calls.
//	Hasil di-cache dan reuse untuk semua user.
//
// Parameter/Value Input:
//   - ctx: context.Context — context untuk HTTP request
//   - marketData: map[string]*scanner.MarketData — market data hasil scan
//
// Function yang Dipanggil/Dikonsumsi:
//   - ai.Decide: dipanggil untuk setiap symbol yang lolos pre-AI filter
//
// Output/Return Value:
//   - Tidak ada return value langsung, hasil di-cache di o.decisionCache
func (o *Orchestrator) preComputeAIDecisions(ctx context.Context, marketData map[string]*scanner.MarketData) {
	// Clear old cache entries (expired ones)
	o.cleanDecisionCache()

	// Collect unique symbols from market data
	symbols := make([]string, 0, len(marketData))
	for symbol := range marketData {
		symbols = append(symbols, symbol)
	}

	if len(symbols) == 0 {
		return
	}

	o.logger.Infof("Pre-computing AI decisions for %d symbols (token optimization)", len(symbols))

	// Worker pool: max 5 concurrent AI calls
	const maxConcurrentAI = 5
	sem := make(chan struct{}, maxConcurrentAI)
	var wg sync.WaitGroup

	for _, symbol := range symbols {
		// Check cache first
		o.decisionCacheMu.RLock()
		_, exists := o.decisionCache[symbol]
		o.decisionCacheMu.RUnlock()
		if exists {
			continue // Already computed this cycle
		}

		wg.Add(1)
		go func(sym string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			data, ok := marketData[sym]
			if !ok {
				return
			}

			// PRE-AI FILTERING (moved from ProcessUser)
			// 1. Volume check
			volumeFloat, _ := data.Volume24h.Float64()
			if volumeFloat < o.config.MinVolume24h {
				o.logger.WithField("symbol", sym).Debug("Filtered: 24h volume below threshold")
				return
			}

			// 2. Volatility check
			if len(data.Candles) > 0 {
				minLow := data.Candles[len(data.Candles)-1].Low
				maxHigh := data.Candles[len(data.Candles)-1].High
				startIdx := max(0, len(data.Candles)-3)
				for i := startIdx; i < len(data.Candles); i++ {
					if data.Candles[i].Low.LessThan(minLow) {
						minLow = data.Candles[i].Low
					}
					if data.Candles[i].High.GreaterThan(maxHigh) {
						maxHigh = data.Candles[i].High
					}
				}
				minLowFloat, _ := minLow.Float64()
				maxHighFloat, _ := maxHigh.Float64()
				if minLowFloat > 0 {
					volatility := ((maxHighFloat - minLowFloat) / minLowFloat) * 100
					if volatility < o.config.MinVolatilityPercent {
						o.logger.WithField("symbol", sym).Debug("Filtered: volatility below threshold")
						return
					}
				}
			}

			// ============================================================
			// TOKEN OPTIMIZATION: Pre-calculate indicators in Go
			// ============================================================
			var rsi decimal.Decimal
			var trend decimal.Decimal
			if len(data.Candles) > 0 {
				rsi, _ = o.scanner.CalculateRSI(data.Candles, 14)
				trend = calculateTrend(data.Candles)
			}

			// ============================================================
			// TOKEN OPTIMIZATION: Trend-only filter (skip sideways markets)
			// ============================================================
			if !hasStrongTrend(data.Candles, 2.0) { // >2% change over 20 candles
				o.logger.WithField("symbol", sym).Debug("Filtered: no strong trend (sideways market)")
				// Default to HOLD for sideways markets
				o.cacheDecision(sym, &ai.CoordinatorDecision{
					TradeDecision: "HOLD",
					Confidence:   decimal.NewFromFloat(50),
					MarketContext: ai.MarketContext{
						Regime:           "crab",
						OverallSentiment: "Sideways market - no clear trend",
					},
				})
				return
			}

			// ============================================================
			// TOKEN OPTIMIZATION: RSI zone filter (rule-based fallback)
			// ============================================================
			if rsi.GreaterThanOrEqual(decimal.NewFromFloat(40)) && rsi.LessThanOrEqual(decimal.NewFromFloat(60)) {
				// RSI neutral zone - skip LLM, default to HOLD
				o.logger.WithField("symbol", sym).WithField("rsi", rsi.String()).Debug("Filtered: RSI neutral zone")
				o.cacheDecision(sym, &ai.CoordinatorDecision{
					TradeDecision: "HOLD",
					Confidence:    decimal.NewFromFloat(50),
					MarketContext: ai.MarketContext{
						Regime:           "crab",
						OverallSentiment: "RSI neutral zone (40-60)",
					},
				})
				return
			}

			// RSI extreme zones - rule-based decision (skip LLM)
			if rsi.LessThan(decimal.NewFromFloat(25)) {
				// Extreme oversold - BUY signal
				o.logger.WithField("symbol", sym).WithField("rsi", rsi.String()).Debug("Rule-based: RSI extreme oversold")
				o.cacheDecision(sym, &ai.CoordinatorDecision{
					TradeDecision: "BUY",
					Confidence:    decimal.NewFromFloat(75),
					MarketContext: ai.MarketContext{
						Regime:           "bear",
						OverallSentiment: "RSI extreme oversold - oversold bounce likely",
					},
					RiskAssessment: ai.RiskAssessment{Level: "medium"},
				})
				return
			}

			if rsi.GreaterThan(decimal.NewFromFloat(75)) {
				// Extreme overbought - SELL signal
				o.logger.WithField("symbol", sym).WithField("rsi", rsi.String()).Debug("Rule-based: RSI extreme overbought")
				o.cacheDecision(sym, &ai.CoordinatorDecision{
					TradeDecision: "SELL",
					Confidence:    decimal.NewFromFloat(75),
					MarketContext: ai.MarketContext{
						Regime:           "bull",
						OverallSentiment: "RSI extreme overbought - correction likely",
					},
					RiskAssessment: ai.RiskAssessment{Level: "medium"},
				})
				return
			}

			// ============================================================
			// Build market data map for AI (with pre-calculated indicators)
			// ============================================================
			marketDataMap := map[string]any{
				"latest_price": data.LatestPrice.String(),
				"volume_24h":  data.Volume24h.String(),
				"interval":    data.Interval,
				"rsi":         rsi.StringFixed(0),
				"trend":       trend.StringFixed(2),
				"candles":     data.Candles,
			}

			// Call AI (cached for all users)
			decision, err := o.ai.Decide(ctx, sym, marketDataMap, "")
			if err != nil {
				o.logger.WithError(err).WithField("symbol", sym).Warn("AI decision failed")
				return
			}

			// Cache the decision with adaptive TTL based on confidence
			o.cacheDecisionWithTTL(sym, decision, getAdaptiveTTL(decision.Confidence))

			o.logger.WithField("symbol", sym).
				WithField("decision", decision.TradeDecision).
				WithField("confidence", decision.Confidence.String()).
				Info("AI decision cached")
		}(symbol)
	}

	wg.Wait()
	o.logger.Infof("AI decisions pre-computed: %d cached", len(o.decisionCache))
}

// cleanDecisionCache removes expired cache entries
func (o *Orchestrator) cleanDecisionCache() {
	o.decisionCacheMu.Lock()
	defer o.decisionCacheMu.Unlock()

	now := time.Now()
	for symbol, entry := range o.decisionCache {
		if now.After(entry.expireAt) {
			delete(o.decisionCache, symbol)
		}
	}
}

// hasStrongTrend checks if candles show a strong trend (>minChangePercent over 20 candles).
// Returns true if not enough data (let LLM decide).
func hasStrongTrend(candles []models.MarketCandle, minChangePercent float64) bool {
	if len(candles) < 20 {
		return true // Not enough data, let LLM decide
	}
	recent := candles[len(candles)-1].Close
	older := candles[len(candles)-20].Close
	if older.IsZero() {
		return true
	}
	change := recent.Sub(older).Div(older).Abs()
	return change.GreaterThan(decimal.NewFromFloat(minChangePercent / 100))
}

// calculateTrend calculates trend direction from candles.
// Returns positive for bull, negative for bear, 0 for neutral.
func calculateTrend(candles []models.MarketCandle) decimal.Decimal {
	if len(candles) < 20 {
		return decimal.Zero
	}
	recent := candles[len(candles)-1].Close
	older := candles[len(candles)-20].Close
	if older.IsZero() {
		return decimal.Zero
	}
	return recent.Sub(older).Div(older).Mul(decimal.NewFromFloat(100))
}

// getAdaptiveTTL returns TTL based on confidence level.
// Higher confidence = longer cache (reduce calls for confident decisions).
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

// cacheDecision caches a decision with default TTL (7 minutes).
func (o *Orchestrator) cacheDecision(symbol string, decision *ai.CoordinatorDecision) {
	o.decisionCacheMu.Lock()
	defer o.decisionCacheMu.Unlock()
	o.decisionCache[symbol] = &decisionCacheEntry{
		decision: decision,
		expireAt: time.Now().Add(7 * time.Minute),
	}
}

// cacheDecisionWithTTL caches a decision with custom TTL.
func (o *Orchestrator) cacheDecisionWithTTL(symbol string, decision *ai.CoordinatorDecision, ttl time.Duration) {
	o.decisionCacheMu.Lock()
	defer o.decisionCacheMu.Unlock()
	o.decisionCache[symbol] = &decisionCacheEntry{
		decision: decision,
		expireAt: time.Now().Add(ttl),
	}
}

// ProcessUser processes trading for a single user.
// Gets user context, runs AI analysis, checks risk, executes if approved.
// Nama Function: ProcessUser
// Deskripsi: Memproses trading untuk satu user.
//
//	Mengambil user context, jalankan AI analysis, check risk, execute jika approved.
//
// Parameter/Value Input:
//   - ctx: context.Context — context untuk operasi
//   - user: models.User — user yang akan diproses
//   - marketData: map[string]*scanner.MarketData — market data dari scan
//
// Function yang Dipanggil/Dikonsumsi:
//   - GetUserContext: dipanggil untuk ambil user context
//   - ai.Decide: dipanggil untuk AI analysis
//   - risk.CheckTrade: dipanggil untuk risk check
//   - executor.ExecuteMarket: dipanggil untuk execute trade
//
// Output/Return Value:
//   - error: error jika processing gagal
func (o *Orchestrator) ProcessUser(ctx context.Context, user models.User, marketData map[string]*scanner.MarketData) error {
	userLogger := o.logger.WithField("user_id", user.ID)

	// Get user context (includes decrypted credentials)
	userCtx, err := GetUserContext(ctx, o.db, &user)
	if err != nil {
		userLogger.WithError(err).Warn("Failed to get user context")
		return err
	}
	if userCtx == nil {
		userLogger.Warn("No API key configured for user, skipping")
		return nil
	}

	userLogger = userLogger.WithField("exchange", userCtx.Exchange)
	userLogger.Info("Processing user trading cycle")

	// Get base capital balance (BTC)
	var portfolioValue decimal.Decimal
	balances, err := o.exchange.GetBalances(ctx, userCtx.APIKey, userCtx.APISecret)
	if err == nil {
		if btcBal, ok := balances["BTC"]; ok {
			portfolioValue = btcBal
			userLogger.WithField("btc_balance", portfolioValue).Debug("Fetched BTC balance for portfolio sizing")
		} else {
			portfolioValue = decimal.NewFromFloat(0.1) // fallback
			userLogger.Warn("BTC balance not found, using fallback 0.1 BTC")
		}
	} else {
		userLogger.WithError(err).Warn("Failed to get exchange balances, using fallback 0.1 BTC")
		portfolioValue = decimal.NewFromFloat(0.1)
	}

	// Run AI analysis for each configured pair concurrently
	// Max 5 AI calls concurrently per user — menghindari rate limit AI endpoint
	const maxConcurrentPairs = 5
	semPair := make(chan struct{}, maxConcurrentPairs)
	var wgPair sync.WaitGroup

	for _, pair := range userCtx.Pairs {
		data, ok := marketData[pair.Symbol]
		if !ok {
			userLogger.WithField("symbol", pair.Symbol).Warn("No market data for pair")
			continue
		}

		wgPair.Add(1)
		go func(p models.TradingPair) {
			defer wgPair.Done()
			semPair <- struct{}{}
			defer func() { <-semPair }()

			pairLogger := userLogger.WithField("symbol", p.Symbol)
			pairLogger.Info("Scanning pair for AI analysis")

			// Get cached AI decision (pre-computed in preComputeAIDecisions)
			o.decisionCacheMu.RLock()
			cached, exists := o.decisionCache[p.Symbol]
			o.decisionCacheMu.RUnlock()

			if !exists {
				pairLogger.Debug("No cached AI decision for symbol, skipping")
				return
			}

			decision := cached.decision

			// Log AI decision
			o.logAIDecision(ctx, &user, p.Symbol, decision)

			pairLogger.WithField("decision", decision.TradeDecision).
				WithField("confidence", decision.Confidence.String()).
				WithField("reasoning", decision.MarketContext.OverallSentiment).
				Info("Using cached AI decision")

			// Check Confidence Threshold
			confidenceFloat, _ := decision.Confidence.Float64()
			if confidenceFloat < o.config.MinConfidenceThreshold {
				pairLogger.WithField("confidence", confidenceFloat).
					WithField("threshold", o.config.MinConfidenceThreshold).
					Info("Trade blocked: AI confidence too low")
				return
			}

			// Check if AI recommends trade
			if decision.TradeDecision != "BUY" && decision.TradeDecision != "SELL" {
				pairLogger.WithField("decision", decision.TradeDecision).
					Info("Trade skipped: AI did not recommend buy/sell")
				return
			}

			pairLogger.Info("AI trade recommendation accepted, proceeding to Risk Guardian/Executor")

			// Build order for risk check
			order := &models.Order{
				UserID:    user.ID,
				Symbol:    p.Symbol,
				Side:      decision.TradeDecision,
				OrderType: "MARKET",
			}

			// Risk check via Risk Guardian
			guardian := risk.NewGuardian()
			riskResult := guardian.CheckTrade(&user, userCtx.Config, order, 0, decimal.Zero)

			riskMap := map[string]int{
				"low":     1,
				"medium":  2,
				"high":    3,
				"extreme": 4,
			}

			maxAllowedRiskIdx := riskMap[o.config.MaxRiskLevel]
			if maxAllowedRiskIdx == 0 {
				maxAllowedRiskIdx = 2 // default medium
			}

			currentRiskIdx := riskMap[string(riskResult.RiskLevel)]

			if !riskResult.Approved || currentRiskIdx > maxAllowedRiskIdx {
				pairLogger.WithField("risk_level", riskResult.RiskLevel).
					WithField("max_allowed", o.config.MaxRiskLevel).
					Info("Trade blocked by Risk Guardian or Max Risk Level exceeded")
				return
			}

			// Calculate position size using risk parameters
			stopLossPercent := decimal.NewFromFloat(2.0) // default 2%
			if len(decision.ExecutionPlan.TakeProfitLevels) > 0 {
				stopLossPercent = decimal.NewFromFloat(decision.ExecutionPlan.TakeProfitLevels[0].TargetPercent)
			}

			positionSizeQuote := guardian.CalculatePositionSize(
				portfolioValue, // BTC balance
				userCtx.Config.MaxRiskPerTrade,
				stopLossPercent,
			)

			// Cap by MaxAllocationPerTrade
			maxAllocation := portfolioValue.Mul(userCtx.Config.MaxAllocationPerTrade).Div(decimal.NewFromFloat(100))
			if positionSizeQuote.GreaterThan(maxAllocation) {
				positionSizeQuote = maxAllocation
				pairLogger.WithField("capped_at", maxAllocation).Debug("Position size capped by MaxAllocationPerTrade")
			}

			if positionSizeQuote.LessThanOrEqual(decimal.Zero) {
				pairLogger.Debug("Position size too small, skipping")
				return
			}

			// Convert Quote Asset (BTC) position size to Base Asset quantity
			if data.LatestPrice.LessThanOrEqual(decimal.Zero) {
				pairLogger.Warn("Latest price is invalid, skipping")
				return
			}

			positionSizeBase := positionSizeQuote.Div(data.LatestPrice)

			// Execute trade
			plan := execution.ExecutionPlan{
				Symbol:      p.Symbol,
				Side:        decision.TradeDecision,
				Quantity:    positionSizeBase,
				StopLoss:    decision.ExecutionPlan.StopLoss,
				MaxSlippage: decimal.NewFromFloat(0.005), // 0.5% max slippage
			}

			executor := execution.NewExecutor(o.db, o.exchange)
			result, err := executor.ExecuteMarket(ctx, user.ID, plan, userCtx.APIKey, userCtx.APISecret)
			if err != nil {
				pairLogger.WithError(err).Error("Trade execution failed")
				return
			}

			pairLogger.Infof("Trade executed: %s %s @ %s",
				decision.TradeDecision, positionSizeBase.String(), result.ExecutionPrice.String())
		}(pair)
	}
	wgPair.Wait()

	return nil
}

// logAIDecision logs AI decision to database for learning
func (o *Orchestrator) logAIDecision(ctx context.Context, user *models.User, symbol string, decision *ai.CoordinatorDecision) {
	if decision == nil {
		return
	}

	inputCtx, _ := json.Marshal(map[string]any{
		"symbol":  symbol,
		"user_id": user.ID,
	})
	outputCtx, _ := json.Marshal(decision)

	// Provider UUID untuk "Direct LLM" dari seed data ai_providers
	providerID := models.ProviderIDDirectLLM

	_, err := o.db.ExecContext(ctx, `
		INSERT INTO ai_decisions (provider_id, symbol, decision, confidence, input_context, output_context)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, providerID, symbol, decision.TradeDecision, decision.Confidence, inputCtx, outputCtx)

	if err != nil {
		o.logger.WithError(err).Warn("Failed to log AI decision")
	}
}
