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
// Output/Return Value:
//   - Tidak ada return value langsung, aplikasi exit dengan code 0 atau 1
func main() {
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

	// Initialize services
	authService := auth.NewService(db)
	_ = risk.NewGuardian()
	_ = learning.NewService(db)
	_ = strategy.NewStrategyEngine(db)

	// Initialize exchange client
	binanceClient := exchange.NewBinance()
	logg.Infof("Exchange client initialized: %s", binanceClient.GetName())

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

	// Initialize AI Coordinator (TradingAgents)
	taClient := ai.NewTradingAgentsClient(
		cfg.TradingAgentsURL,
		cfg.LLMAPIKey,
		cfg.LLMModel,
		cfg.LLMBaseURL,
	)
	logg.Info("TradingAgents AI client initialized")

	// Initialize Executor
	_ = execution.NewExecutor(db, binanceClient)
	logg.Info("Order executor initialized")

	// Initialize Multi-Account Orchestrator
	var tradingOrchestrator *orchestrator.Orchestrator
	if cfg.TradingAgentsURL != "" {
		strategyEngine := strategy.NewStrategyEngine(db)
		tradingOrchestrator = orchestrator.NewOrchestrator(db, binanceClient, marketScanner, taClient, strategyEngine)
		tradingOrchestrator.Start(context.Background())
		logg.Info("Multi-account trading orchestrator started")
	}

	// Initialize Telegram Bot with full dependencies
	var telegramBot *telegram.Bot
	if cfg.TelegramBotToken != "" {
		telegramBot = telegram.NewBotWithConfig(telegram.BotConfig{
			Token:       cfg.TelegramBotToken,
			WebhookURL:  cfg.TelegramWebhookURL,
			AuthService: authService,
			DB:          db,
			Exchange:    binanceClient,
			PairManager: pairManager,
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
// Function yang Dipanggil/Dikonsumsi:
//   - json.NewEncoder: dipanggil untuk encode JSON response
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
// Function yang Dipanggil/Dikonsumsi:
//   - bot.HandleUpdate: dipanggil untuk proses setiap update
//   - json.NewDecoder: dipanggil untuk decode incoming JSON
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
// Function yang Dipanggil/Dikonsumsi:
//   - scanner.FetchTicker: dipanggil untuk ambil ticker data
//   - scanner.FetchCandles: dipanggil untuk ambil candle data
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
// Function yang Dipanggil/Dikonsumsi:
//   - exchange.GetPrice: dipanggil untuk ambil harga asset
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
		json.NewEncoder(w).Encode(map[string]interface{}{
			"symbol": symbol,
			"price":  price.String(),
		})
	}
}
