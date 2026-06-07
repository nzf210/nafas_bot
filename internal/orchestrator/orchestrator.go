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

	// CycleInterval adalah interval antar trading cycle.
	// Default 15 menit - cukup sering untuk opportunistic trading.
	CycleInterval = 15 * time.Minute
)

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
//   - ai: *ai.TradingAgentsClient — TradingAgents AI client
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
	db       *sql.DB
	exchange exchange.Exchange
	scanner  *scanner.Scanner
	ai       *ai.TradingAgentsClient
	engine   *strategy.StrategyEngine
	logger   *logger.Logger
	stopCh   chan struct{}
	wg       sync.WaitGroup
	config   *config.Config
}

// NewOrchestrator creates a new multi-account orchestrator
// Nama Function: NewOrchestrator
// Deskripsi: Membuat instance orchestrator baru.
// Parameter/Value Input:
//   - db: *sql.DB — koneksi database
//   - exch: exchange.Exchange — exchange client
//   - scan: *scanner.Scanner — market scanner
//   - taClient: *ai.TradingAgentsClient — TradingAgents AI client
//   - eng: *strategy.StrategyEngine — strategy engine
//   - cfg: *config.Config — application configuration
//
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung, hanya inisialisasi
//
// Output/Return Value:
//   - *Orchestrator: pointer ke orchestrator instance
func NewOrchestrator(db *sql.DB, exch exchange.Exchange, scan *scanner.Scanner, taClient *ai.TradingAgentsClient, eng *strategy.StrategyEngine, cfg *config.Config) *Orchestrator {
	return &Orchestrator{
		db:       db,
		exchange: exch,
		scanner:  scan,
		ai:       taClient,
		engine:   eng,
		logger:   logger.Default().WithField("module", "orchestrator"),
		stopCh:   make(chan struct{}),
		config:   cfg,
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

	ticker := time.NewTicker(CycleInterval)
	defer ticker.Stop()

	o.logger.Infof("Orchestrator worker started, cycle interval: %v", CycleInterval)

	for {
		select {
		case <-ctx.Done():
			o.logger.Info("Orchestrator worker: context cancelled")
			return
		case <-o.stopCh:
			o.logger.Info("Orchestrator worker: stop signal received")
			return
		case <-ticker.C:
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
//   - ScanMarket: dipanggil untuk scan market data
//   - ProcessUser: dipanggil untuk setiap user secara concurrent
//
// Output/Return Value:
//   - error: error jika cycle gagal total
func (o *Orchestrator) RunCycle(ctx context.Context) error {
	return o.runCycle(ctx)
}

func (o *Orchestrator) runCycle(ctx context.Context) error {
	startTime := time.Now()
	o.logger.Info("Starting trading cycle")

	// Get all users with auto-trade enabled
	users := o.GetActiveUsers(ctx)
	if len(users) == 0 {
		o.logger.Info("No active users with auto-trade enabled, skipping cycle")
		return nil
	}
	o.logger.Infof("Found %d users with auto-trade enabled", len(users))

	// Scan market data ONCE for all users
	marketData := o.ScanMarket(ctx)
	if marketData == nil {
		o.logger.Warn("Market scan failed, skipping cycle")
		return fmt.Errorf("market scan failed")
	}
	o.logger.Infof("Market scan completed: %d symbols", len(marketData))

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

			if err := o.ProcessUser(ctx, u, marketData); err != nil {
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
	// Create a channel to receive market data
	marketDataMap := make(map[string]*scanner.MarketData)
	dataCh := make(chan scanner.MarketData, 100)

	// Create context with timeout for scan
	scanCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	// Run scan with callback
	err := o.scanner.ScanAllPairs(scanCtx, func(data scanner.MarketData) {
		dataCh <- data
	})
	if err != nil {
		o.logger.WithError(err).Warn("Market scan error")
	}

	// Collect results
	close(dataCh)
	for data := range dataCh {
		copied := data
		marketDataMap[data.Symbol] = &copied
	}

	return marketDataMap
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

	// Run AI analysis for each configured pair
	for _, pair := range userCtx.Pairs {
		data, ok := marketData[pair.Symbol]
		if !ok {
			userLogger.WithField("symbol", pair.Symbol).Warn("No market data for pair")
			continue
		}

		// Prepare market data for AI
		marketDataMap := map[string]interface{}{
			"symbol":       data.Symbol,
			"latest_price": data.LatestPrice.String(),
			"volume_24h":   data.Volume24h.String(),
			"interval":     data.Interval,
		}

		// ---------------------------------------------------------
		// PRE-AI FILTERING
		// ---------------------------------------------------------
		// 1. Check 24h Volume
		volumeFloat, _ := data.Volume24h.Float64()
		if volumeFloat < o.config.MinVolume24h {
			userLogger.WithField("symbol", pair.Symbol).WithField("volume", volumeFloat).Debug("Skipping - 24h volume too low")
			continue
		}

		// 2. Check Volatility (Last 3 candles, e.g. 15m if interval is 5m)
		if len(data.Candles) > 0 {
			var minLow = data.Candles[len(data.Candles)-1].Low
			var maxHigh = data.Candles[len(data.Candles)-1].High

			startIdx := len(data.Candles) - 3
			if startIdx < 0 {
				startIdx = 0
			}

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
					userLogger.WithField("symbol", pair.Symbol).WithField("volatility", volatility).Debug("Skipping - volatility too low for AI analysis")
					continue
				}
			}
		}
		// ---------------------------------------------------------

		// Run AI analysis using existing Decide method
		decision, err := o.ai.Decide(ctx, pair.Symbol, marketDataMap, "")
		if err != nil {
			userLogger.WithError(err).WithField("symbol", pair.Symbol).Warn("AI analysis failed")
			continue
		}

		// Log AI decision
		o.logAIDecision(ctx, &user, pair.Symbol, decision)

		// Check Confidence Threshold
		confidenceFloat, _ := decision.Confidence.Float64()
		if confidenceFloat < o.config.MinConfidenceThreshold {
			userLogger.WithField("symbol", pair.Symbol).WithField("confidence", confidenceFloat).WithField("threshold", o.config.MinConfidenceThreshold).Info("Trade blocked: confidence too low")
			continue
		}

		// Check if AI recommends trade
		if decision.TradeDecision != "buy" && decision.TradeDecision != "sell" {
			userLogger.WithField("symbol", pair.Symbol).WithField("decision", decision.TradeDecision).Debug("Skipping - no trade signal")
			continue
		}

		// Build order for risk check
		order := &models.Order{
			UserID:    user.ID,
			Symbol:    pair.Symbol,
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
			userLogger.WithField("symbol", pair.Symbol).WithField("risk_level", riskResult.RiskLevel).WithField("max_allowed", o.config.MaxRiskLevel).Info("Trade blocked by Risk Guardian or Max Risk Level exceeded")
			continue
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
			userLogger.WithField("symbol", pair.Symbol).WithField("capped_at", maxAllocation).Debug("Position size capped by MaxAllocationPerTrade")
		}

		if positionSizeQuote.LessThanOrEqual(decimal.Zero) {
			userLogger.WithField("symbol", pair.Symbol).Debug("Position size too small, skipping")
			continue
		}

		// Convert Quote Asset (BTC) position size to Base Asset quantity
		if data.LatestPrice.LessThanOrEqual(decimal.Zero) {
			userLogger.WithField("symbol", pair.Symbol).Warn("Latest price is invalid, skipping")
			continue
		}
		
		positionSizeBase := positionSizeQuote.Div(data.LatestPrice)

		// Execute trade
		plan := execution.ExecutionPlan{
			Symbol:      pair.Symbol,
			Side:        decision.TradeDecision,
			Quantity:    positionSizeBase,
			StopLoss:    decision.ExecutionPlan.StopLoss,
			MaxSlippage: decimal.NewFromFloat(0.005), // 0.5% max slippage
		}

		executor := execution.NewExecutor(o.db, o.exchange)
		result, err := executor.ExecuteMarket(ctx, user.ID, plan, userCtx.APIKey, userCtx.APISecret)
		if err != nil {
			userLogger.WithError(err).WithField("symbol", pair.Symbol).Error("Trade execution failed")
			continue
		}

		userLogger.WithField("symbol", pair.Symbol).Infof("Trade executed: %s %s @ %s",
			decision.TradeDecision, positionSizeBase.String(), result.ExecutionPrice.String())
	}

	return nil
}

// logAIDecision logs AI decision to database for learning
func (o *Orchestrator) logAIDecision(ctx context.Context, user *models.User, symbol string, decision *ai.CoordinatorDecision) {
	if decision == nil {
		return
	}

	inputCtx, _ := json.Marshal(map[string]interface{}{
		"symbol":  symbol,
		"user_id": user.ID,
	})
	outputCtx, _ := json.Marshal(decision)

	_, err := o.db.ExecContext(ctx, `
		INSERT INTO ai_decisions (provider_id, symbol, decision, confidence, input_context, output_context)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, models.UUID{}, symbol, decision.TradeDecision, decision.Confidence, inputCtx, outputCtx)

	if err != nil {
		o.logger.WithError(err).Warn("Failed to log AI decision")
	}
}
