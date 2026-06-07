// ============================================================
// MODULE: models
// Deskripsi: Semua struct/model yang merepresentasikan tabel database V1.0
// ============================================================

package models

import (
	"encoding/json"
	"time"

	"github.com/shopspring/decimal"
	"github.com/google/uuid"
)

// ============================================================
// 1. USERS
// ============================================================

// Nama Function: User
// Deskripsi: Struct yang merepresentasikan tabel users di database.
// Parameter/Value Input:
//   - ID: uuid.UUID — primary key unik user
//   - TelegramID: int64 — ID telegram unik untuk autentikasi
//   - Username, FirstName, LastName: string — data profil telegram
//   - Status: string — status user (active, suspended, banned)
//   - WCHBalance: decimal.Decimal — saldo WCH token
//   - CreatedAt, UpdatedAt: time.Time — timestamp pembuatan dan update
// Function yang Dipanggil/Dikonsumsi:
//   - sql.NullString: dipanggil untuk fields yang nullable
// Output/Return Value:
//   - User: struct lengkap sesuai tabel users
type User struct {
	ID         UUID   `json:"id" db:"id"`
	TelegramID int64  `json:"telegram_id" db:"telegram_id"`
	Username   *string `json:"username,omitempty" db:"username"`
	FirstName  *string `json:"first_name,omitempty" db:"first_name"`
	LastName   *string `json:"last_name,omitempty" db:"last_name"`
	Status     string  `json:"status" db:"status"`
	WCHBalance decimal.Decimal `json:"wch_balance" db:"wch_balance"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time `json:"updated_at" db:"updated_at"`
}

// ============================================================
// 2. API KEYS & EXCHANGE ACCOUNTS
// ============================================================

// Nama Function: APIKey
// Deskripsi: Struct yang merepresentasikan tabel api_keys dengan field terenkripsi.
// Parameter/Value Input:
//   - EncryptedAPIKey, EncryptedAPISecret, EncryptedPassphrase: string — sudah terenkripsi AES
//   - IsActive: bool — apakah API key masih aktif
// Function yang Dipanggil/Dikonsumsi:
//   - repository.GetAPIKeyByUserID: dipanggil untuk mengambil API key user
// Output/Return Value:
//   - APIKey: struct dengan field terenkripsi
type APIKey struct {
	ID                    UUID   `json:"id" db:"id"`
	UserID                UUID   `json:"user_id" db:"user_id"`
	Exchange              string `json:"exchange" db:"exchange"`
	EncryptedAPIKey       string `json:"-" db:"encrypted_api_key"`
	EncryptedAPISecret    string `json:"-" db:"encrypted_api_secret"`
	EncryptedPassphrase   *string `json:"-" db:"encrypted_passphrase"`
	IsActive              bool   `json:"is_active" db:"is_active"`
	CreatedAt             time.Time `json:"created_at" db:"created_at"`
	UpdatedAt             time.Time `json:"updated_at" db:"updated_at"`
}

// Nama Function: ExchangeAccount
// Deskripsi: Struct yang merepresentasikan tabel exchange_accounts.
// Parameter/Value Input:
//   - AccountType: string — tipe akun (main, sub, etc.)
//   - LastSyncAt: time.Time — timestamp sinkronisasi terakhir dengan exchange
// Function yang Dipanggil/Dikonsumsi:
//   - exchange.SyncAccountBalance: dipanggil untuk sync saldo dari exchange
// Output/Return Value:
//   - ExchangeAccount: struct akun exchange
type ExchangeAccount struct {
	ID          UUID      `json:"id" db:"id"`
	UserID      UUID      `json:"user_id" db:"user_id"`
	Exchange    string    `json:"exchange" db:"exchange"`
	AccountType string    `json:"account_type" db:"account_type"`
	IsActive    bool      `json:"is_active" db:"is_active"`
	LastSyncAt  *time.Time `json:"last_sync_at,omitempty" db:"last_sync_at"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// ============================================================
// 3. CONFIGURATION
// ============================================================

// Nama Function: SystemSetting
// Deskripsi: Struct yang merepresentasikan tabel system_settings (key-value JSONB).
// Parameter/Value Input:
//   - Key: string — primary key untuk setting
//   - Value: json.RawMessage — value dalam format JSONB
// Function yang Dipanggil/Dikonsumsi:
//   - json.Marshal/Unmarshal: dipanggil untuk konversi JSONB
// Output/Return Value:
//   - SystemSetting: struct untuk menyimpan konfigurasi sistem
type SystemSetting struct {
	Key       string          `json:"key" db:"key"`
	Value     json.RawMessage `json:"value" db:"value"`
	UpdatedAt time.Time       `json:"updated_at" db:"updated_at"`
}

// Nama Function: UserConfig
// Deskripsi: Struct yang merepresentasikan tabel user_configs untuk pengaturan trading user.
// Parameter/Value Input:
//   - MaxRiskPerTrade: decimal — max risk per trade dalam persen (0.1-5.0)
//   - DailyLossLimit: decimal — batas loss harian dalam persen dari modal (1-20)
//   - MaxOpenPositions: int — maksimal posisi terbuka
//   - NotifyOnTrade, NotifyOnError: bool — notifikasi Telegram
//   - AutoTradeEnabled: bool — apakah auto trading aktif
// Function yang Dipanggil/Dikonsumsi:
//   - risk.CheckRiskLimits: dipanggil untuk validasi risk sebelum eksekusi
// Output/Return Value:
//   - UserConfig: struct konfigurasi user
type UserConfig struct {
	ID               UUID    `json:"id" db:"id"`
	UserID                UUID    `json:"user_id" db:"user_id"`
	MaxRiskPerTrade       decimal.Decimal `json:"max_risk_per_trade" db:"max_risk_per_trade"`
	MaxAllocationPerTrade decimal.Decimal `json:"max_allocation_per_trade" db:"max_allocation_per_trade"`
	DailyLossLimit        decimal.Decimal `json:"daily_loss_limit" db:"daily_loss_limit"`
	MaxOpenPositions      int     `json:"max_open_positions" db:"max_open_positions"`
	NotifyOnTrade     bool    `json:"notify_on_trade" db:"notify_on_trade"`
	NotifyOnError     bool    `json:"notify_on_error" db:"notify_on_error"`
	DailyReportTime   string  `json:"daily_report_time" db:"daily_report_time"`
	ReportInterval    string  `json:"report_interval" db:"report_interval"`
	LastReportSentAt  time.Time `json:"last_report_sent_at" db:"last_report_sent_at"`
	AutoTradeEnabled  bool    `json:"auto_trade_enabled" db:"auto_trade_enabled"`
	CreatedAt         time.Time `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time `json:"updated_at" db:"updated_at"`
}

// Nama Function: TradingPair
// Deskripsi: Struct yang merepresentasikan tabel trading_pairs.
// Parameter/Value Input:
//   - Symbol: string — symbol trading pair (BTCUSDT)
//   - BaseAsset, QuoteAsset: string — asset dasar dan quote (BTC, USDT)
//   - Priority: int — prioritas pair (lebih tinggi = lebih diutamakan)
//   - Enabled: bool — apakah pair aktif untuk scanning
// Function yang Dipanggil/Dikonsumsi:
//   - scanner.ScanPairs: dipanggil untuk scan market data pair
// Output/Return Value:
//   - TradingPair: struct pair trading
type TradingPair struct {
	ID         UUID      `json:"id" db:"id"`
	UserID     UUID      `json:"user_id" db:"user_id"`
	Exchange   string    `json:"exchange" db:"exchange"`
	Symbol     string    `json:"symbol" db:"symbol"`
	BaseAsset  string    `json:"base_asset" db:"base_asset"`
	QuoteAsset string    `json:"quote_asset" db:"quote_asset"`
	Priority   int       `json:"priority" db:"priority"`
	Enabled    bool      `json:"enabled" db:"enabled"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time `json:"updated_at" db:"updated_at"`
}

// Nama Function: AssetTarget
// Deskripsi: Struct yang merepresentasikan tabel asset_targets untuk target akumulasi.
// Parameter/Value Input:
//   - Asset: string — asset yang di-target (BTC, ETH, SOL)
//   - TargetAmount: decimal — jumlah target akumulasi
//   - CurrentAmount: decimal — jumlah saat ini
//   - Priority: int — prioritas asset
// Function yang Dipanggil/Dikonsumsi:
//   - ai.TradingAgentsClient: dipanggil untuk decision making berdasarkan target
// Output/Return Value:
//   - AssetTarget: struct target asset
type AssetTarget struct {
	ID           UUID            `json:"id" db:"id"`
	UserID       UUID            `json:"user_id" db:"user_id"`
	Asset        string          `json:"asset" db:"asset"`
	TargetAmount decimal.Decimal `json:"target_amount" db:"target_amount"`
	CurrentAmount decimal.Decimal `json:"current_amount" db:"current_amount"`
	Priority     int             `json:"priority" db:"priority"`
	CreatedAt    time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at" db:"updated_at"`
}

// ============================================================
// 4. MARKET DATA
// ============================================================

// Nama Function: MarketCandle
// Deskripsi: Struct yang merepresentasikan tabel market_candles (OHLCV data).
// Parameter/Value Input:
//   - Symbol, Interval: string — pair dan timeframe (1m, 5m, 1h, 4h, 1d)
//   - Open, High, Low, Close, Volume: decimal — OHLCV values
//   - CandleTime: time.Time — timestamp candle
// Function yang Dipanggil/Dikonsumsi:
//   - scanner.FetchCandles: dipanggil untuk fetch data dari exchange
//   - signal.CalcRSI, signal.CalcMACD: dipanggil untuk kalkulasi indikator
// Output/Return Value:
//   - MarketCandle: struct candle market
type MarketCandle struct {
	ID         UUID            `json:"id" db:"id"`
	Symbol     string          `json:"symbol" db:"symbol"`
	Interval   string          `json:"interval" db:"interval"`
	Open       decimal.Decimal `json:"open" db:"open"`
	High       decimal.Decimal `json:"high" db:"high"`
	Low        decimal.Decimal `json:"low" db:"low"`
	Close      decimal.Decimal `json:"close" db:"close"`
	Volume     decimal.Decimal `json:"volume" db:"volume"`
	CandleTime time.Time       `json:"candle_time" db:"candle_time"`
	CreatedAt  time.Time       `json:"created_at" db:"created_at"`
}

// Nama Function: MarketSnapshot
// Deskripsi: Struct yang merepresentasikan tabel market_snapshots (latest price).
// Parameter/Value Input:
//   - LastPrice: decimal — harga terakhir
//   - Volume24h: decimal — volume 24 jam
//   - Timestamp: time.Time — timestamp snapshot
// Function yang Dipanggil/Dikonsumsi:
//   - scanner.FetchTicker: dipanggil untuk fetch harga terbaru
// Output/Return Value:
//   - MarketSnapshot: struct snapshot harga
type MarketSnapshot struct {
	ID         UUID            `json:"id" db:"id"`
	Symbol     string          `json:"symbol" db:"symbol"`
	LastPrice  decimal.Decimal `json:"last_price" db:"last_price"`
	Volume24h  decimal.Decimal `json:"volume_24h" db:"volume_24h"`
	Timestamp  time.Time       `json:"timestamp" db:"timestamp"`
}

// ============================================================
// 5. SIGNAL ENGINE
// ============================================================

// Nama Function: SignalHistory
// Deskripsi: Struct yang merepresentasikan tabel signal_history.
// Parameter/Value Input:
//   - RSI, MACD, VolumeScore, TrendScore, MomentumScore: decimal — indikator
//   - SignalStrength: decimal — kekuatan sinyal (0-100)
// Function yang Dipanggil/Dikonsumsi:
//   - signal.CalcSignal: dipanggil untuk kalkulasi sinyal
// Output/Return Value:
//   - SignalHistory: struct history sinyal
type SignalHistory struct {
	ID             UUID            `json:"id" db:"id"`
	Symbol         string          `json:"symbol" db:"symbol"`
	RSI            decimal.Decimal `json:"rsi" db:"rsi"`
	MACD           decimal.Decimal `json:"macd" db:"macd"`
	VolumeScore    decimal.Decimal `json:"volume_score" db:"volume_score"`
	TrendScore     decimal.Decimal `json:"trend_score" db:"trend_score"`
	MomentumScore  decimal.Decimal `json:"momentum_score" db:"momentum_score"`
	SignalStrength decimal.Decimal `json:"signal_strength" db:"signal_strength"`
	CreatedAt      time.Time       `json:"created_at" db:"created_at"`
}

// ============================================================
// 6. STRATEGIES
// ============================================================

// Nama Function: Strategy
// Deskripsi: Struct yang merepresentasikan tabel strategies.
// Parameter/Value Input:
//   - Name: string — nama strategi
//   - Description: string — deskripsi strategi
//   - Parameters: json.RawMessage — parameter strategi dalam JSON
//   - Enabled: bool — apakah strategi aktif
// Function yang Dipanggil/Dikonsumsi:
//   - strategy.Run: dipanggil untuk eksekusi strategi
// Output/Return Value:
//   - Strategy: struct strategi
type Strategy struct {
	ID          UUID            `json:"id" db:"id"`
	Name        string          `json:"name" db:"name"`
	Description *string         `json:"description,omitempty" db:"description"`
	Parameters  json.RawMessage `json:"parameters" db:"parameters"`
	Enabled     bool            `json:"enabled" db:"enabled"`
	CreatedAt   time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at" db:"updated_at"`
}

// Nama Function: StrategyRun
// Deskripsi: Struct yang merepresentasikan tabel strategy_runs.
// Parameter/Value Input:
//   - StrategyID: UUID — relasi ke strategi yang dijalankan
//   - Symbol: string — pair yang dianalisis
//   - DecisionSource: string — sumber keputusan (ai, manual, signal)
//   - Status: string — status run (running, completed, failed)
//   - StartedAt, CompletedAt: time.Time — timestamp start dan selesai
// Function yang Dipanggil/Dikonsumsi:
//   - orchestrator.Orchestrator.RunCycle: dipanggil untuk orchestrate strategy run
// Output/Return Value:
//   - StrategyRun: struct execution log strategi
type StrategyRun struct {
	ID             UUID       `json:"id" db:"id"`
	StrategyID     UUID       `json:"strategy_id" db:"strategy_id"`
	Symbol         string     `json:"symbol" db:"symbol"`
	DecisionSource string     `json:"decision_source" db:"decision_source"`
	Status         string     `json:"status" db:"status"`
	StartedAt      time.Time  `json:"started_at" db:"started_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty" db:"completed_at"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
}

// ============================================================
// 7. ORDERS
// ============================================================

// Nama Function: Order
// Deskripsi: Struct yang merepresentasikan tabel orders.
// Parameter/Value Input:
//   - Exchange, Symbol, Side, OrderType: string — detail order
//   - Price: decimal — harga order (null untuk market order)
//   - Quantity, ExecutedQuantity: decimal — jumlah dan jumlah tereksekusi
//   - Status: string — status order (pending, filled, cancelled, rejected)
//   - ExchangeOrderID: string — order ID dari exchange
// Function yang Dipanggil/Dikonsumsi:
//   - execution.PlaceOrder: dipanggil untuk kirim order ke exchange
//   - execution.FetchOrderStatus: dipanggil untuk cek status order
// Output/Return Value:
//   - Order: struct order
type Order struct {
	ID               UUID            `json:"id" db:"id"`
	UserID           UUID            `json:"user_id" db:"user_id"`
	Exchange         string          `json:"exchange" db:"exchange"`
	Symbol           string          `json:"symbol" db:"symbol"`
	Side             string          `json:"side" db:"side"`
	OrderType        string          `json:"order_type" db:"order_type"`
	Price            decimal.Decimal `json:"price" db:"price"`
	Quantity         decimal.Decimal `json:"quantity" db:"quantity"`
	ExecutedQuantity decimal.Decimal `json:"executed_quantity" db:"executed_quantity"`
	Status           string          `json:"status" db:"status"`
	ExchangeOrderID  *string         `json:"exchange_order_id,omitempty" db:"exchange_order_id"`
	CreatedAt        time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at" db:"updated_at"`
}

// Nama Function: TradeExecution
// Deskripsi: Struct yang merepresentasikan tabel trade_executions (fills).
// Parameter/Value Input:
//   - OrderID: UUID — relasi ke order parent
//   - Price, Quantity: decimal — harga dan jumlah tereksekusi
//   - Fee, FeeAsset: decimal, string — fee trading
//   - ExecutedAt: time.Time — timestamp eksekusi
// Function yang Dipanggil/Dikonsumsi:
//   - execution.ProcessFill: dipanggil ketika ada fill dari exchange
// Output/Return Value:
//   - TradeExecution: struct execution/fill
type TradeExecution struct {
	ID          UUID            `json:"id" db:"id"`
	OrderID     UUID            `json:"order_id" db:"order_id"`
	Price       decimal.Decimal `json:"price" db:"price"`
	Quantity    decimal.Decimal `json:"quantity" db:"quantity"`
	Fee         decimal.Decimal `json:"fee" db:"fee"`
	FeeAsset    *string         `json:"fee_asset,omitempty" db:"fee_asset"`
	ExecutedAt  time.Time       `json:"executed_at" db:"executed_at"`
}

// ============================================================
// 8. PORTFOLIO
// ============================================================

// Nama Function: AssetInventory
// Deskripsi: Struct yang merepresentasikan tabel asset_inventory.
// Parameter/Value Input:
//   - Asset: string — nama asset (BTC, ETH, SOL)
//   - Balance, LockedBalance: decimal — saldo available dan locked
//   - BTCEquivalent: decimal — estimasi nilai dalam BTC
// Function yang Dipanggil/Dikonsumsi:
//   - exchange.SyncBalances: dipanggil untuk sync saldo dari exchange
//   - portfolio.CalcTotalValue: dipanggil untuk kalkulasi total value
// Output/Return Value:
//   - AssetInventory: struct inventory asset user
type AssetInventory struct {
	ID              UUID            `json:"id" db:"id"`
	UserID          UUID            `json:"user_id" db:"user_id"`
	Asset           string          `json:"asset" db:"asset"`
	Balance         decimal.Decimal `json:"balance" db:"balance"`
	LockedBalance   decimal.Decimal `json:"locked_balance" db:"locked_balance"`
	BTCEquivalent   decimal.Decimal `json:"btc_equivalent" db:"btc_equivalent"`
	UpdatedAt       time.Time       `json:"updated_at" db:"updated_at"`
}

// ============================================================
// 9. BTC TREASURY
// ============================================================

// Nama Function: BTCTreasurySnapshot
// Deskripsi: Struct yang merepresentasikan tabel btc_treasury_snapshots.
// Parameter/Value Input:
//   - BTCBalance, BTCLocked, BTCTotal: decimal — saldo BTC
//   - USDValue: decimal — estimasi nilai dalam USD
//   - SnapshotTime: time.Time — timestamp snapshot
// Function yang Dipanggil/Dikonsumsi:
//   - portfolio.CalcBTCTotal: dipanggil untuk kalkulasi total BTC
// Output/Return Value:
//   - BTCTreasurySnapshot: struct snapshot treasury BTC
type BTCTreasurySnapshot struct {
	ID           UUID            `json:"id" db:"id"`
	UserID       UUID            `json:"user_id" db:"user_id"`
	BTCBalance   decimal.Decimal `json:"btc_balance" db:"btc_balance"`
	BTCLocked    decimal.Decimal `json:"btc_locked" db:"btc_locked"`
	BTCTotal     decimal.Decimal `json:"btc_total" db:"btc_total"`
	USDValue     decimal.Decimal `json:"usd_value" db:"usd_value"`
	SnapshotTime time.Time       `json:"snapshot_time" db:"snapshot_time"`
}

// Nama Function: BTCAccumulationLedger
// Deskripsi: Struct yang merepresentasikan tabel btc_accumulation_ledger.
// Parameter/Value Input:
//   - Source: string — sumber akumulasi (buy, trade, reward)
//   - Asset: string — asset yang dikonversi
//   - AmountAsset: decimal — jumlah asset
//   - BTCReceived: decimal — BTC yang diterima
//   - FeeBTC: decimal — fee dalam BTC
// Function yang Dipanggil/Dikonsumsi:
//   - learning.LogAccumulation: dipanggil untuk log akumulasi BTC
// Output/Return Value:
//   - BTCAccumulationLedger: struct ledger akumulasi BTC
type BTCAccumulationLedger struct {
	ID           UUID            `json:"id" db:"id"`
	UserID       UUID            `json:"user_id" db:"user_id"`
	Source       string          `json:"source" db:"source"`
	Asset        string          `json:"asset" db:"asset"`
	AmountAsset  decimal.Decimal `json:"amount_asset" db:"amount_asset"`
	BTCReceived  decimal.Decimal `json:"btc_received" db:"btc_received"`
	FeeBTC       decimal.Decimal `json:"fee_btc" db:"fee_btc"`
	CreatedAt    time.Time       `json:"created_at" db:"created_at"`
}

// Nama Function: BTCAccumulationMetrics
// Deskripsi: Struct yang merepresentasikan tabel btc_accumulation_metrics.
// Parameter/Value Input:
//   - Period: string — periode metrics (daily, weekly, monthly)
//   - StartingBTC, EndingBTC: decimal — BTC awal dan akhir periode
//   - BTCGrowth: decimal — pertumbuhan BTC
//   - GrowthPercent: decimal — persentase pertumbuhan
// Function yang Dipanggil/Dikonsumsi:
//   - learning.CalcMetrics: dipanggil untuk kalkulasi metrics periodik
// Output/Return Value:
//   - BTCAccumulationMetrics: struct metrics akumulasi
type BTCAccumulationMetrics struct {
	ID             UUID            `json:"id" db:"id"`
	UserID         UUID            `json:"user_id" db:"user_id"`
	Period         string          `json:"period" db:"period"`
	StartingBTC    decimal.Decimal `json:"starting_btc" db:"starting_btc"`
	EndingBTC      decimal.Decimal `json:"ending_btc" db:"ending_btc"`
	BTCGrowth      decimal.Decimal `json:"btc_growth" db:"btc_growth"`
	GrowthPercent  decimal.Decimal `json:"growth_percent" db:"growth_percent"`
	CreatedAt      time.Time       `json:"created_at" db:"created_at"`
}

// ============================================================
// 10-14. AI SYSTEM
// ============================================================

// Nama Function: AIProvider
// Deskripsi: Struct yang merepresentasikan tabel ai_providers.
// Parameter/Value Input:
//   - Name, Provider, Model: string — konfigurasi provider AI
//   - EncryptedAPIKey: string — API key terenkripsi
//   - IsActive: bool — apakah provider aktif
// Function yang Dipanggil/Dikonsumsi:
//   - ai.CallProvider: dipanggil untuk generate text via AI
// Output/Return Value:
//   - AIProvider: struct provider AI
type AIProvider struct {
	ID             UUID      `json:"id" db:"id"`
	Name           string    `json:"name" db:"name"`
	Provider       string    `json:"provider" db:"provider"`
	Model          string    `json:"model" db:"model"`
	EncryptedAPIKey string   `json:"-" db:"encrypted_api_key"`
	IsActive       bool      `json:"is_active" db:"is_active"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time `json:"updated_at" db:"updated_at"`
}

// Nama Function: AIPromptVersion
// Deskripsi: Struct yang merepresentasikan tabel ai_prompt_versions.
// Parameter/Value Input:
//   - Name, Version: string — identifier prompt
//   - SystemPrompt: string — system prompt untuk AI agent
//   - IsActive: bool — apakah versi ini aktif
// Function yang Dipanggil/Dikonsumsi:
//   - ai.LoadPrompt: dipanggil untuk load prompt version
// Output/Return Value:
//   - AIPromptVersion: struct versi prompt
type AIPromptVersion struct {
	ID           UUID      `json:"id" db:"id"`
	Name         string    `json:"name" db:"name"`
	Version      string    `json:"version" db:"version"`
	SystemPrompt string    `json:"system_prompt" db:"system_prompt"`
	IsActive     bool      `json:"is_active" db:"is_active"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
}

// Nama Function: AIDecision
// Deskripsi: Struct yang merepresentasikan tabel ai_decisions.
// Parameter/Value Input:
//   - StrategyRunID, ProviderID: UUID — relasi ke run dan provider
//   - Symbol: string — pair yang di-decide
//   - Decision: string — keputusan (buy, sell, hold, skip)
//   - Confidence: decimal — confidence score (0-100)
//   - Reasoning: string — reasoning dari AI
//   - InputContext, OutputContext: json.RawMessage — context penuh untuk debugging
// Function yang Dipanggil/Dikonsumsi:
//   - ai.TradingAgentsClient.Decide: dipanggil untuk generate decision
//   - learning.LogDecision: dipanggil untuk store decision
// Output/Return Value:
//   - AIDecision: struct decision AI
type AIDecision struct {
	ID             UUID            `json:"id" db:"id"`
	StrategyRunID  UUID            `json:"strategy_run_id" db:"strategy_run_id"`
	ProviderID     UUID            `json:"provider_id" db:"provider_id"`
	Symbol         string          `json:"symbol" db:"symbol"`
	Decision       string          `json:"decision" db:"decision"`
	Confidence     decimal.Decimal `json:"confidence" db:"confidence"`
	Reasoning      *string         `json:"reasoning,omitempty" db:"reasoning"`
	PromptVersion  *string         `json:"prompt_version,omitempty" db:"prompt_version"`
	InputContext   json.RawMessage `json:"input_context" db:"input_context"`
	OutputContext  json.RawMessage `json:"output_context" db:"output_context"`
	CreatedAt      time.Time       `json:"created_at" db:"created_at"`
}

// Nama Function: AIFeedback
// Deskripsi: Struct yang merepresentasikan tabel ai_feedback.
// Parameter/Value Input:
//   - DecisionID: UUID — relasi ke decision
//   - BTCBefore, BTCAfter: decimal — BTC sebelum dan sesudah trade
//   - BTCDelta: decimal — perubahan BTC (profit/loss)
//   - Success: bool — apakah trade berhasil
// Function yang Dipanggil/Dikonsumsi:
//   - learning.LogFeedback: dipanggil untuk store feedback
//   - learning.UpdateStrategy: dipanggil untuk update strategi berdasarkan feedback
// Output/Return Value:
//   - AIFeedback: struct feedback untuk learning
type AIFeedback struct {
	ID         UUID            `json:"id" db:"id"`
	DecisionID UUID            `json:"decision_id" db:"decision_id"`
	BTCBefore  decimal.Decimal `json:"btc_before" db:"btc_before"`
	BTCAfter   decimal.Decimal `json:"btc_after" db:"btc_after"`
	BTCDelta   decimal.Decimal `json:"btc_delta" db:"btc_delta"`
	Success    bool            `json:"success" db:"success"`
	CreatedAt  time.Time       `json:"created_at" db:"created_at"`
}

// Nama Function: AIMemory
// Deskripsi: Struct yang merepresentasikan tabel ai_memory.
// Parameter/Value Input:
//   - MemoryType: string — tipe memory (pattern, lesson, market_event)
//   - Content: string — konten memory
//   - ImportanceScore: decimal — skor kepentingan (0-1)
//   - Metadata: json.RawMessage — metadata tambahan
// Function yang Dipanggil/Dikonsumsi:
//   - learning.StoreMemory: dipanggil untuk store memory
//   - learning.Recall: dipanggil untuk recall memory
// Output/Return Value:
//   - AIMemory: struct memory untuk AI
type AIMemory struct {
	ID              UUID            `json:"id" db:"id"`
	MemoryType      string          `json:"memory_type" db:"memory_type"`
	Content         string          `json:"content" db:"content"`
	ImportanceScore decimal.Decimal `json:"importance_score" db:"importance_score"`
	Metadata        json.RawMessage `json:"metadata" db:"metadata"`
	CreatedAt       time.Time       `json:"created_at" db:"created_at"`
}

// ============================================================
// 15. WCH ECOSYSTEM
// ============================================================

// Nama Function: WCHTransaction
// Deskripsi: Struct yang merepresentasikan tabel wch_transactions.
// Parameter/Value Input:
//   - Amount: decimal — jumlah WCH
//   - TransactionType: string — tipe transaksi (stake, unstake, reward, purchase)
//   - Status: string — status transaksi (pending, completed, failed)
//   - TxHash: string — hash transaksi blockchain
// Function yang Dipanggil/Dikonsumsi:
//   - user.UpdateWCHBalance: dipanggil setelah transaksi selesai
// Output/Return Value:
//   - WCHTransaction: struct transaksi WCH
type WCHTransaction struct {
	ID              UUID            `json:"id" db:"id"`
	UserID          UUID            `json:"user_id" db:"user_id"`
	Amount          decimal.Decimal `json:"amount" db:"amount"`
	TransactionType string          `json:"transaction_type" db:"transaction_type"`
	Status          string          `json:"status" db:"status"`
	TxHash          *string         `json:"tx_hash,omitempty" db:"tx_hash"`
	CreatedAt       time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at" db:"updated_at"`
}

// ============================================================
// 16. LOGGING
// ============================================================

// Nama Function: SystemLog
// Deskripsi: Struct yang merepresentasikan tabel system_logs.
// Parameter/Value Input:
//   - UserID: UUID — user yang terkait (nullable)
//   - Level: string — level log (debug, info, warn, error)
//   - Module: string — module yang menghasilkan log
//   - Message: string — pesan log
//   - Metadata: json.RawMessage — data tambahan
// Function yang Dipanggil/Dikonsumsi:
//   - logger.Log: dipanggil untuk menyimpan log
//   - logger.Query: dipanggil untuk query log
// Output/Return Value:
//   - SystemLog: struct log sistem
type SystemLog struct {
	ID        UUID            `json:"id" db:"id"`
	UserID    *UUID           `json:"user_id,omitempty" db:"user_id"`
	Level     string          `json:"level" db:"level"`
	Module    string          `json:"module" db:"module"`
	Message   string          `json:"message" db:"message"`
	Metadata  json.RawMessage `json:"metadata" db:"metadata"`
	CreatedAt time.Time       `json:"created_at" db:"created_at"`
}

// ============================================================
// 17. REPORTING
// ============================================================

// Nama Function: DailyReport
// Deskripsi: Struct yang merepresentasikan tabel daily_reports.
// Parameter/Value Input:
//   - UserID: UUID — relasi ke user
//   - BTCStart, BTCEnd: decimal — BTC awal dan akhir hari
//   - BTCGrowth: decimal — pertumbuhan BTC hari ini
//   - TradeCount: int — jumlah trade hari ini
//   - WinRate: decimal — win rate percentage
//   - ReportDate: time.Time — tanggal laporan
// Function yang Dipanggil/Dikonsumsi:
//   - learning.GenerateDailyReport: dipanggil untuk generate laporan harian
// Output/Return Value:
//   - DailyReport: struct laporan harian
type DailyReport struct {
	ID          UUID            `json:"id" db:"id"`
	UserID      UUID            `json:"user_id" db:"user_id"`
	BTCStart    decimal.Decimal `json:"btc_start" db:"btc_start"`
	BTCEnd      decimal.Decimal `json:"btc_end" db:"btc_end"`
	BTCGrowth   decimal.Decimal `json:"btc_growth" db:"btc_growth"`
	TradeCount  int             `json:"trade_count" db:"trade_count"`
	WinRate     decimal.Decimal `json:"win_rate" db:"win_rate"`
	ReportDate  time.Time       `json:"report_date" db:"report_date"`
	CreatedAt   time.Time       `json:"created_at" db:"created_at"`
}

// ============================================================
// HELPERS
// ============================================================

// UUID type alias for database (using google/uuid)
type UUID = uuid.UUID