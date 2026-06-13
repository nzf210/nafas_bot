// ============================================================
// MODULE: main (cmd/api)
// Deskripsi: Entry point utama untuk NAFAS Bot API server
// ============================================================

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/nzf210/nafas-bot/internal/ai"
	"github.com/nzf210/nafas-bot/internal/auth"
	"github.com/nzf210/nafas-bot/internal/config"
	"github.com/nzf210/nafas-bot/internal/database"
	"github.com/nzf210/nafas-bot/internal/exchange"
	"github.com/nzf210/nafas-bot/internal/execution"
	"github.com/nzf210/nafas-bot/internal/learning"
	"github.com/nzf210/nafas-bot/internal/logger"
	"github.com/nzf210/nafas-bot/internal/orchestrator"
	"github.com/nzf210/nafas-bot/internal/risk"
	"github.com/nzf210/nafas-bot/internal/scanner"
	"github.com/nzf210/nafas-bot/internal/strategy"
	"github.com/nzf210/nafas-bot/internal/telegram"
)

// Nama Function: main
// Deskripsi: Entry point utama aplikasi. Melakukan inisialisasi semua services dan start HTTP server.
// Parameter/Value Input:
//   - Tidak ada parameter input langsung, membaca dari environment variables
//
// Function yang Dipanggil/Dikonsumsi:
//   - config.Load: dipanggil untuk load konfigurasi
//   - database.Connect: dipanggil untuk koneksi ke PostgreSQL
//   - logger.Init: dipanggil untuk inisialisasi logger
//   - auth.InitEncryption: dipanggil untuk inisialisasi encryption
//   - auth.NewService: dipanggil untuk inisialisasi auth service
//   - exchange.NewBinance: dipanggil untuk inisialisasi exchange client
//   - scanner.NewScanner: dipanggil untuk inisialisasi scanner
//   - risk.NewGuardian: dipanggil untuk inisialisasi risk guardian
//   - ai.NewTradingAgentsClient: dipanggil untuk inisialisasi TradingAgents AI client
//   - learning.NewService: dipanggil untuk inisialisasi learning service
//   - execution.NewExecutor: dipanggil untuk inisialisasi executor
//   - telegram.NewBotWithConfig: dipanggil untuk inisialisasi Telegram bot
//   - http.ListenAndServe: dipanggil untuk start HTTP server
//
// Output/Return Value:
//   - Tidak ada return value langsung, aplikasi exit dengan code 0 atau 1
func main() {
	// Load .env file if present (silent if not found)
	_ = godotenv.Load()

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize logger
	logger.Init(cfg.LogLevel, "app")
	logg := logger.Default()

	logg.Infof("Starting NAFAS BOT v0.1.0")
	logg.Infof("Environment: %s", cfg.AppEnv)

	// Initialize encryption (required for API keys)
	if err := auth.InitEncryption(); err != nil {
		logg.Warnf("Encryption not initialized: %v (API keys will not be available)", err)
	}

	// Connect to database
	db, err := database.Connect(cfg)
	if err != nil {
		logg.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()
	logg.Info("Database connected")

	// Auto-apply recent schema migrations programmatically
	autoMigrateQueries := []struct {
		sql      string
		desc     string
		critical bool // if true, exit app if this migration fails
	}{
		{"ALTER TABLE trading_pairs ADD COLUMN IF NOT EXISTS exchange VARCHAR(50) DEFAULT 'Binance';", "add exchange column to trading_pairs", true},
		{"ALTER TABLE trading_pairs DROP CONSTRAINT IF EXISTS unique_user_exchange_symbol;", "drop old unique constraint (no-op if not exists)", false},
		{"ALTER TABLE trading_pairs ADD CONSTRAINT unique_user_exchange_symbol UNIQUE (user_id, exchange, symbol);", "add unique constraint (ignore if already exists)", false},
		{"ALTER TABLE user_configs ADD COLUMN IF NOT EXISTS report_interval VARCHAR(20) DEFAULT '24h';", "add report_interval column to user_configs", false},
		{"ALTER TABLE user_configs ADD COLUMN IF NOT EXISTS last_report_sent_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP;", "add last_report_sent_at column to user_configs", false},
		{"ALTER TABLE user_configs ADD COLUMN IF NOT EXISTS max_allocation_per_trade DECIMAL(18,8) DEFAULT 10.00;", "add max_allocation_per_trade column to user_configs", false},
		// Seed AI provider "Direct LLM" jika belum ada
		{`INSERT INTO ai_providers (id, name, provider, model, is_active) VALUES ('00000000-0000-0000-0000-000000000001', 'Direct LLM', 'openai', 'gpt-4o', true) ON CONFLICT DO NOTHING;`, "seed ai_providers (Direct LLM)", false},
		// Realized P&L tracking table
		{`CREATE TABLE IF NOT EXISTS realized_pnl (
			id              UUID DEFAULT gen_random_uuid() PRIMARY KEY,
			user_id         UUID NOT NULL,
			symbol          VARCHAR(20) NOT NULL,
			buy_order_id    UUID NOT NULL,
			sell_order_id   UUID NOT NULL,
			entry_price     DECIMAL(30,10) NOT NULL,
			exit_price      DECIMAL(30,10) NOT NULL,
			quantity        DECIMAL(30,10) NOT NULL,
			gross_pnl       DECIMAL(30,10) NOT NULL,
			net_pnl         DECIMAL(30,10) NOT NULL,
			pnl_percent     DECIMAL(10,4) NOT NULL,
			is_profit       BOOLEAN NOT NULL DEFAULT false,
			closed_at       TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
		)`, "create realized_pnl table", false},
		{`CREATE UNIQUE INDEX IF NOT EXISTS idx_realized_pnl_sell_order ON realized_pnl (sell_order_id)`, "add unique index on realized_pnl.sell_order_id", false},
		{`CREATE INDEX IF NOT EXISTS idx_realized_pnl_user_closed ON realized_pnl (user_id, closed_at DESC)`, "add index on realized_pnl (user_id, closed_at)", false},
	}
	for _, m := range autoMigrateQueries {
		if _, err := db.Exec(m.sql); err != nil {
			if m.critical {
				logg.Fatalf("CRITICAL auto-migration failed [%s]: %v — app cannot start without this column", m.desc, err)
			}
			logg.Warnf("Non-critical auto-migration skipped [%s]: %v", m.desc, err)
		} else {
			logg.Infof("Auto-migration applied: %s", m.desc)
		}
	}

	// Initialize services
	authService := auth.NewService(db)
	_ = risk.NewGuardian()
	learningService := learning.NewService(db)
	_ = strategy.NewStrategyEngine(db)

	// Initialize exchange factory (supports multiple exchanges: Binance, OKX)
	exchangeFactory := exchange.NewExchangeFactory()

	// Initialize primary exchange client for market data (shared by all users)
	binanceClient := exchange.NewBinance()
	logg.Infof("Exchange factory initialized with: %v", exchangeFactory.GetSupportedExchanges())
	logg.Infof("Primary exchange client: %s", binanceClient.GetName())

	// Load LOT_SIZE filters — WAJIB sebelum bot mulai trading
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelStartup()

	if err := binanceClient.LoadSymbolFilters(startupCtx); err != nil {
		log.Fatalf("Failed to load Binance symbol filters: %v", err)
	}

	// Initialize scanner with empty pairs initially
	marketScanner := scanner.NewScanner(
		binanceClient,
		[]string{}, // Start empty, will be populated by PairManager
		[]string{"1h", "4h", "1d"},
	)

	// Initialize PairManager
	pairManager := scanner.NewPairManager(db)

	// Load existing pairs from database
	if err := pairManager.LoadFromDB(); err != nil {
		logg.Errorf("Failed to load pairs from DB: %v", err)
	}

	pairManager.RegisterScanner(binanceClient.GetName(), marketScanner)

	// Add default system pairs
	defaultPairs := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"}
	for _, pair := range defaultPairs {
		pairManager.AddUserPair("system", binanceClient.GetName(), pair)
	}

	logg.Info("Market scanner and PairManager initialized")

	// Initialize AI Coordinator (Multi-LLM with Main + Fallback support)
	// Main LLM: Use LLMBaseURL if set, otherwise fall back to LLMProviderURL
	mainBaseURL := cfg.LLMBaseURL
	if mainBaseURL == "" {
		mainBaseURL = cfg.LLMProviderURL
	}

	// Fallback LLM: Use LLMFallbackBaseURL if set
	fallbackBaseURL := cfg.LLMFallbackBaseURL
	if fallbackBaseURL == "" {
		fallbackBaseURL = cfg.LLMFallbackProviderURL
	}

	// Create Multi-LLM client with Main + Fallback support
	taClient := ai.NewMultiLLMClient(
		// Main LLM config
		mainBaseURL,
		cfg.LLMAPIKey,
		cfg.LLMModel,
		cfg.LLMTemperature,
		cfg.LLMMaxTokens,
		// Fallback LLM config
		fallbackBaseURL,
		cfg.LLMFallbackAPIKey,
		cfg.LLMFallbackModel,
		cfg.LLMTemperature, // reuse same temperature for fallback
		cfg.LLMMaxTokens,
	)
	logg.Info("Multi-LLM AI client initialized (Main + Fallback)")

	// Initialize Executor
	_ = execution.NewExecutor(db, binanceClient)
	logg.Info("Order executor initialized")

	// Initialize Multi-Account Orchestrator
	var tradingOrchestrator *orchestrator.Orchestrator
	var sltpMonitor *orchestrator.SLTPMonitor
	if cfg.LLMAPIKey != "" || cfg.LLMBaseURL != "" {
		strategyEngine := strategy.NewStrategyEngine(db)
		tradingOrchestrator = orchestrator.NewOrchestrator(db, binanceClient, marketScanner, taClient, strategyEngine, cfg, pairManager, exchangeFactory, learningService)
		tradingOrchestrator.Start(context.Background())
		logg.Info("Multi-account trading orchestrator started")

		// Start SL/TP monitoring service (background job, 30s interval)
		sltpMonitor = orchestrator.NewSLTPMonitor(tradingOrchestrator, 30*time.Second)
		sltpMonitor.Start(context.Background())
		logg.Info("SL/TP monitoring service started (30s interval)")
	}

	// Initialize Telegram Bot with full dependencies
	var telegramBot *telegram.Bot
	if cfg.TelegramBotToken != "" {
		telegramBot = telegram.NewBotWithConfig(telegram.BotConfig{
			Token:           cfg.TelegramBotToken,
			WebhookURL:      cfg.TelegramWebhookURL,
			AuthService:     authService,
			DB:              db,
			Exchange:        binanceClient,      // Primary exchange for market data
			ExchangeFactory: exchangeFactory,     // Per-user exchange factory (multi-exchange)
			PairManager:     pairManager,
			MaxPairsPerUser: cfg.MaxPairsPerUser,
		})
		telegramBot.RegisterDefaultHandlers()

		if cfg.TelegramWebhookURL != "" {
			if err := telegramBot.SetWebhook(cfg.TelegramWebhookURL + "/webhook"); err != nil {
				logg.Errorf("Failed to set webhook: %v", err)
			} else {
				logg.Infof("Webhook set to %s/webhook", cfg.TelegramWebhookURL)
			}
		}

		telegramBot.Start()
		logg.Infof("Telegram bot initialized with queue system")
	}

	// Create HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/webhook", webhookHandler(telegramBot))
	mux.HandleFunc("/api/market/", marketHandler(marketScanner))
	mux.HandleFunc("/api/balance/", balanceHandler(binanceClient))

	server := &http.Server{
		Addr:         ":" + cfg.AppPort,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// Start server in goroutine
	go func() {
		logg.Infof("Server listening on port %s", cfg.AppPort)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logg.Fatalf("Server error: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logg.Info("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logg.Errorf("Server shutdown error: %v", err)
	}

	// Stop Telegram bot workers gracefully
	if telegramBot != nil {
		telegramBot.Stop()
	}

	// Stop SL/TP monitor
	if sltpMonitor != nil {
		sltpMonitor.Stop()
	}

	// Stop trading orchestrator
	if tradingOrchestrator != nil {
		tradingOrchestrator.Stop()
	}

	logg.Info("Server stopped")
}

// healthHandler handles /health endpoint
// Nama Function: healthHandler
// Deskripsi: Handler untuk health check endpoint.
// Parameter/Value Input:
//   - w: http.ResponseWriter — response writer
//   - r: *http.Request — incoming request
//
// Function yang Dipanggil/Dikonsumsi:
//   - json.NewEncoder: dipanggil untuk encode JSON response
//
// Output/Return Value:
//   - Tidak ada return value langsung
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"ok","service":"nafas-bot","version":"0.1.0"}`)
}

// webhookHandler handles Telegram webhook
// Nama Function: webhookHandler
// Deskripsi: Handler untuk Telegram webhook endpoint.
// Parameter/Value Input:
//   - bot: *telegram.Bot — Telegram bot instance
//
// Function yang Dipanggil/Dikonsumsi:
//   - bot.HandleUpdate: dipanggil untuk proses setiap update
//   - json.NewDecoder: dipanggil untuk decode incoming JSON
//
// Output/Return Value:
//   - http.HandlerFunc: handler function untuk webhook
func webhookHandler(bot *telegram.Bot) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if bot == nil {
			http.Error(w, "Telegram bot not configured", http.StatusServiceUnavailable)
			return
		}

		var update telegram.Update
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		if err := bot.HandleUpdate(update); err != nil {
			logger.Default().Errorf("Failed to handle update: %v", err)
		}

		w.WriteHeader(http.StatusOK)
	}
}

// marketHandler handles market data API
// Nama Function: marketHandler
// Deskripsi: Handler untuk market data API endpoint.
// Parameter/Value Input:
//   - scanner: *scanner.Scanner — scanner instance
//
// Function yang Dipanggil/Dikonsumsi:
//   - scanner.FetchTicker: dipanggil untuk ambil ticker data
//   - scanner.FetchCandles: dipanggil untuk ambil candle data
//
// Output/Return Value:
//   - http.HandlerFunc: handler function untuk market API
func marketHandler(scanner *scanner.Scanner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.Background()
		symbol := r.URL.Path[len("/api/market/"):]

		if symbol == "" {
			http.Error(w, "Symbol required", http.StatusBadRequest)
			return
		}

		ticker, err := scanner.FetchTicker(ctx, symbol)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to fetch ticker: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ticker)
	}
}

// balanceHandler handles balance API
// Nama Function: balanceHandler
// Deskripsi: Handler untuk balance API endpoint.
// Parameter/Value Input:
//   - exchange: exchange.Exchange — exchange client
//
// Function yang Dipanggil/Dikonsumsi:
//   - exchange.GetPrice: dipanggil untuk ambil harga asset
//
// Output/Return Value:
//   - http.HandlerFunc: handler function untuk balance API
func balanceHandler(exchange exchange.Exchange) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.Background()
		symbol := r.URL.Path[len("/api/balance/"):]

		if symbol == "" {
			http.Error(w, "Symbol required", http.StatusBadRequest)
			return
		}

		price, err := exchange.GetPrice(ctx, symbol)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to fetch price: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"symbol": symbol,
			"price":  price.String(),
		})
	}
}
