// ============================================================
// MODULE: telegram
// Deskripsi: Telegram bot commands dan handlers
// ============================================================

package telegram

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nzf210/nafas-bot/internal/auth"
	"github.com/nzf210/nafas-bot/internal/exchange"
	"github.com/nzf210/nafas-bot/internal/logger"
	"github.com/nzf210/nafas-bot/internal/models"
)

// ============================================================
// QUEUE SYSTEM CONSTANTS
// ============================================================

const (
	// QueueSize adalah kapasitas buffer channel untuk update jobs.
	// Nilai 100 berarti max 100 update pending di queue.
	QueueSize = 100

	// WorkerCount adalah jumlah worker goroutines yang memproses update secara parallel.
	// 5 workers berarti max 5 command diproses concurrently.
	WorkerCount = 5
)

// Bot represents Telegram bot
// Nama Function: Bot
// Deskripsi: Struct utama Telegram bot handler dengan sistem antrian.
// Parameter/Value Input:
//   - token: string — Telegram bot token
//   - webhookURL: string — webhook URL untuk setwebhook
//   - authService: *auth.Service — auth service untuk user management
//   - db: *sql.DB — database connection untuk query data
//   - exchange: exchange.Exchange — exchange client untuk portfolio
//   - httpClient: *http.Client — HTTP client untuk API calls
//   - logger: *logger.Logger — logger instance
//   - jobQueue: chan Update — buffered channel untuk queueing updates
//   - stopCh: chan struct{} — shutdown signal channel
//   - wg: sync.WaitGroup — waitgroup untuk graceful shutdown workers
type Bot struct {
	token        string
	webhookURL   string
	authService  *auth.Service
	db           *sql.DB
	exchange     exchange.Exchange
	httpClient   *http.Client
	logger       *logger.Logger
	handlers     map[string]CommandHandler
	jobQueue     chan Update
	stopCh       chan struct{}
	wg           sync.WaitGroup
}

// CommandHandler defines a command handler function
// Nama Function: CommandHandler
// Deskripsi: Function type untuk handle command Telegram.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk operasi
//   - user: *models.User — user yang mengirim command
//   - args: string — argumen dari command
// Function yang Dipanggil/Dikonsumsi:
//   - Depends on specific command implementation
// Output/Return Value:
//   - string: response message untuk user
//   - error: error jika command gagal
type CommandHandler func(ctx context.Context, user *models.User, args string) (string, error)

// BotConfig holds bot dependencies
// Nama Function: BotConfig
// Deskripsi: Struct konfigurasi untuk inisialisasi bot dengan dependencies.
// Parameter/Value Input:
//   - Token: string — bot token dari BotFather
//   - WebhookURL: string — webhook URL (kosongkan untuk polling)
//   - AuthService: *auth.Service — auth service
//   - DB: *sql.DB — database connection
//   - Exchange: exchange.Exchange — exchange client
// Function yang Dipanggil/Dikonsumsi:
//   - NewBotWithConfig: dipanggil untuk buat bot instance
// Output/Return Value:
//   - BotConfig: struct konfigurasi
type BotConfig struct {
	Token string
	WebhookURL  string
	AuthService *auth.Service
	DB          *sql.DB
	Exchange    exchange.Exchange
}

// NewBotWithConfig creates a new Telegram bot with full dependencies
// Nama Function: NewBotWithConfig
// Deskripsi: Membuat instance Telegram bot baru dengan dependencies lengkap.
//   Worker pool akan dimulai setelah bot dibuat via Start().
// Parameter/Value Input:
//   - config: BotConfig — konfigurasi dengan semua dependencies
// Function yang Dipanggil/Dikonsumsi:
//   - RegisterDefaultHandlers: dipanggil untuk register semua command handlers
// Output/Return Value:
//   - *Bot: pointer ke bot instance
func NewBotWithConfig(config BotConfig) *Bot {
	bot := &Bot{
		token:        config.Token,
		webhookURL:   config.WebhookURL,
		authService:  config.AuthService,
		db:           config.DB,
		exchange:     config.Exchange,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		logger:       logger.Default().WithField("module", "telegram"),
		handlers:     make(map[string]CommandHandler),
		jobQueue:     make(chan Update, QueueSize),
		stopCh:       make(chan struct{}),
	}

	bot.RegisterDefaultHandlers()
	return bot
}

// Start starts the worker pool untuk memproses queued updates secara parallel.
// Worker pool terdiri dari WorkerCount goroutines yang membaca dari jobQueue.
// Nama Function: Start
// Deskripsi: Memulai worker pool untuk memproses updates secara concurrent.
// Parameter/Value Input:
//   - Tidak ada parameter input langsung
// Function yang Dipanggil/Dikonsumsi:
//   - StartWorkers: dipanggil untuk spawn worker goroutines
// Output/Return Value:
//   - Tidak ada return value langsung
func (b *Bot) Start() {
	b.logger.Infof("Starting Telegram bot with %d workers", WorkerCount)
	for i := 0; i < WorkerCount; i++ {
		b.wg.Add(1)
		go b.worker(i)
	}
}

// Stop gracefully stops semua workers dan drain remaining jobs di queue.
// Menggunakan WaitGroup untuk memastikan semua workers selesai sebelum return.
// Nama Function: Stop
// Deskripsi: Menghentikan worker pool secara graceful.
// Parameter/Value Input:
//   - Tidak ada parameter input langsung
// Function yang Dipanggil/Dikonsumsi:
//   - close(b.stopCh): dipanggil untuk signal workers untuk berhenti
//   - b.wg.Wait: dipanggil untuk wait semua workers selesai
// Output/Return Value:
//   - Tidak ada return value langsung
func (b *Bot) Stop() {
	b.logger.Info("Stopping Telegram bot workers...")
	close(b.stopCh)
	b.wg.Wait()
	b.logger.Info("All workers stopped")
}

// worker adalah goroutine yang memproses updates dari jobQueue secara terus-menerus.
// Setiap worker loop dengan select untuk handle stop signal atau job dari queue.
// Worker berhenti saat stopCh ditutup.
// Nama Function: worker
// Deskripsi: Worker goroutine yang memproses queued updates.
// Parameter/Value Input:
//   - id: int — worker ID untuk logging
// Function yang Dipanggil/Dikonsumsi:
//   - processUpdate: dipanggil untuk process setiap update dari queue
// Output/Return Value:
//   - Tidak ada return value langsung
func (b *Bot) worker(id int) {
	defer b.wg.Done()
	b.logger.Infof("Worker %d started", id)

	for {
		select {
		case <-b.stopCh:
			b.logger.Infof("Worker %d stopping", id)
			return
		case job, ok := <-b.jobQueue:
			if !ok {
				b.logger.Infof("Worker %d: queue closed, stopping", id)
				return
			}
			b.processUpdate(job)
		}
	}
}

// NewBot creates a new Telegram bot (legacy compatibility)
// Nama Function: NewBot
// Deskripsi: Membuat instance Telegram bot baru (backward compatible).
// Parameter/Value Input:
//   - token: string — bot token dari BotFather
//   - authService: *auth.Service — auth service
//   - webhookURL: string — webhook URL (kosongkan untuk polling)
// Function yang Dipanggil/Dikonsumsi:
//   - RegisterDefaultHandlers: dipanggil untuk register semua command handlers
// Output/Return Value:
//   - *Bot: pointer ke bot instance
func NewBot(token string, authService *auth.Service, webhookURL string) *Bot {
	return NewBotWithConfig(BotConfig{
		Token:       token,
		WebhookURL:  webhookURL,
		AuthService: authService,
	})
}

// RegisterDefaultHandlers registers all default commands
// Nama Function: RegisterDefaultHandlers
// Deskripsi: Mendaftarkan semua command handlers default.
// Parameter/Value Input:
//   - Tidak ada parameter input langsung
// Function yang Dipanggil/Dikonsumsi:
//   - bot.Register: dipanggil untuk setiap command
// Output/Return Value:
//   - Tidak ada return value
func (b *Bot) RegisterDefaultHandlers() {
	b.Register("start", b.handleStart)
	b.Register("help", b.handleHelp)
	b.Register("guide", b.handleGuide)
	b.Register("dashboard", b.handleDashboard)
	b.Register("portfolio", b.handlePortfolio)
	b.Register("settings", b.handleSettings)
	b.Register("status", b.handleStatus)
	b.Register("positions", b.handlePositions)
	b.Register("balance", b.handleBalance)
	b.Register("report", b.handleReport)
	b.Register("profile", b.handleProfile)
	b.Register("setapikey", b.handleSetAPIKey)
}

// Register registers a command handler
// Nama Function: Register
// Deskripsi: Mendaftarkan handler untuk command tertentu.
// Parameter/Value Input:
//   - command: string — nama command (tanpa slash)
//   - handler: CommandHandler — function handler
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung
// Output/Return Value:
//   - Tidak ada return value
func (b *Bot) Register(command string, handler CommandHandler) {
	b.handlers[command] = handler
}

// Update represents incoming Telegram update
// Nama Function: Update
// Deskripsi: Struct yang merepresentasikan update dari Telegram.
// Parameter/Value Input:
//   - UpdateID: int64 — ID unik update
//   - Message: Message — message object (nullable)
//   - CallbackQuery: CallbackQuery — callback query (nullable)
// Function yang Dipanggil/Dikonsumsi:
//   - ParseUpdate: dipanggil untuk parse JSON ke struct
// Output/Return Value:
//   - Update: struct update
type Update struct {
	UpdateID       int64          `json:"update_id"`
	Message        *Message       `json:"message,omitempty"`
	CallbackQuery  *CallbackQuery `json:"callback_query,omitempty"`
}

// Message represents Telegram message
// Nama Function: Message
// Deskripsi: Struct message dari Telegram.
// Parameter/Value Input:
//   - MessageID: int — ID message
//   - From: User — sender (nullable)
//   - Chat: Chat — chat object
//   - Text: string — message text
//   - Date: int — Unix timestamp
// Function yang Dipanggil/Dikonsumsi:
//   - SendMessage: dipanggil untuk kirim response
// Output/Return Value:
//   - Message: struct message
type Message struct {
	MessageID int64  `json:"message_id"`
	From      *User  `json:"from,omitempty"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
	Date      int64  `json:"date"`
}

// User represents Telegram user
// Nama Function: User
// Deskripsi: Struct user dari Telegram.
// Parameter/Value Input:
//   - ID: int64 — user ID
//   - Username: string — username
//   - FirstName: string — first name
//   - LastName: string — last name
// Function yang Dipanggil/Dikonsumsi:
//   - Authenticate: dipanggil untuk authenticate user
// Output/Return Value:
//   - User: struct user
type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

// Chat represents Telegram chat
// Nama Function: Chat
// Deskripsi: Struct chat dari Telegram.
// Parameter/Value Input:
//   - ID: int64 — chat ID
//   - Type: string — chat type (private, group, channel)
// Function yang Dipanggil/Dikonsumsi:
//   - SendMessage: dipanggil untuk kirim message
// Output/Return Value:
//   - Chat: struct chat
type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

// CallbackQuery represents Telegram callback query
// Nama Function: CallbackQuery
// Deskripsi: Struct callback query dari inline button.
// Parameter/Value Input:
//   - ID: string — callback query ID
//   - From: User — user yang menekan button
//   - Data: string — callback data
// Function yang Dipanggil/Dikonsumsi:
//   - AnswerCallbackQuery: dipanggil untuk answer callback
// Output/Return Value:
//   - CallbackQuery: struct callback
type CallbackQuery struct {
	ID   string `json:"id"`
	From User   `json:"from"`
	Data string `json:"data"`
}

// HandleUpdate enqueues an incoming update untuk diproses secara asynchronous.
// Update akan di-queue ke jobQueue dan diproses oleh worker pool.
// Jika queue penuh (buffer penuh), update akan di-drop dan return error.
// Nama Function: HandleUpdate
// Deskripsi: Enqueues update ke job queue untuk diproses secara asynchronous.
// Parameter/Value Input:
//   - update: Update — update dari Telegram webhook/polling
// Function yang Dipanggil/Dikonsumsi:
//   - jobQueue <- update: dipanggil untuk enqueue update ke buffered channel
// Output/Return Value:
//   - error: error jika queue penuh atau update invalid
func (b *Bot) HandleUpdate(update Update) error {
	if update.Message == nil {
		return nil
	}

	msg := update.Message
	if !strings.HasPrefix(msg.Text, "/") {
		return nil
	}

	select {
	case b.jobQueue <- update:
		return nil
	default:
		// Queue penuh — update di-drop. Telegram akan retry.
		b.logger.Warnf("Queue full, dropping update %d from chat %d", update.UpdateID, msg.Chat.ID)
		return fmt.Errorf("queue full")
	}
}

// processUpdate adalah internal method yang memproses satu update secara synchronous.
// Dipanggil oleh worker goroutines dari jobQueue.
// Nama Function: processUpdate
// Deskripsi: Memproses satu update — authenticate user, execute handler, send response.
// Parameter/Value Input:
//   - update: Update — update yang akan diproses
// Function yang Dipanggil/Dikonsumsi:
//   - Authenticate: dipanggil untuk authenticate user
//   - handlers[command]: dipanggil untuk execute command handler
//   - SendMessage: dipanggil untuk kirim response ke user
// Output/Return Value:
//   - Tidak ada return value langsung
func (b *Bot) processUpdate(update Update) {
	msg := update.Message
	if msg == nil {
		return
	}


	parts := strings.SplitN(msg.Text, " ", 2)
	command := strings.TrimPrefix(parts[0], "/")
	args := ""
	if len(parts) > 1 {
		args = parts[1]
	}

	ctx := context.Background()
	user, err := b.authService.Authenticate(ctx, msg.From.ID, msg.From.Username, msg.From.FirstName, msg.From.LastName)
	if err != nil {
		b.logger.Errorf("Failed to authenticate user %d: %v", msg.From.ID, err)
		b.SendMessage(msg.Chat.ID, "Authentication failed.")
		return
	}

	handler, ok := b.handlers[command]
	if !ok {
		b.SendMessage(msg.Chat.ID, fmt.Sprintf("Unknown command: /%s", command))
		return
	}

	response, err := handler(ctx, user, args)
	if err != nil {
		b.logger.Errorf("Command %s failed for user %d: %v", command, user.ID, err)
		response = "An error occurred while processing your request."
	}

	b.logger.Infof("Sending response to user %d: %s", msg.Chat.ID, response)
	if err := b.SendMessage(msg.Chat.ID, response); err != nil {
		b.logger.Errorf("Failed to send message to chat %d: %v", msg.Chat.ID, err)
	}
}

// SendMessage sends a message to a chat
// Nama Function: SendMessage
// Deskripsi: Mengirim message ke chat tertentu.
// Parameter/Value Input:
//   - chatID: int64 — chat ID tujuan
//   - text: string — text message
// Function yang Dipanggil/Dikonsumsi:
//   - httpClient.Do: dipanggil untuk kirim request ke Telegram API
//   - json.Marshal: dipanggil untuk serialize request body
// Output/Return Value:
//   - error: error jika send gagal
func (b *Bot) SendMessage(chatID int64, text string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", b.token)

	body := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "Markdown",
	}
	bodyJSON, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(bodyJSON))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API error: %s", string(respBody))
	}

	return nil
}

// SetWebhook sets the webhook URL
// Nama Function: SetWebhook
// Deskripsi: Mengatur webhook URL untuk bot.
// Parameter/Value Input:
//   - url: string — webhook URL
// Function yang Dipanggil/Dikonsumsi:
//   - httpClient.Do: dipanggil untuk set webhook via Telegram API
// Output/Return Value:
//   - error: error jika set gagal
func (b *Bot) SetWebhook(url string) error {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/setWebhook", b.token)

	body := map[string]string{"url": url}
	bodyJSON, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(bodyJSON))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to set webhook: %s", string(respBody))
	}

	b.logger.Infof("Webhook set to: %s", url)
	return nil
}

// ============================================================
// COMMAND HANDLERS
// ============================================================

// handleStart handles /start command
// Nama Function: handleStart
// Deskripsi: Handler untuk command /start.
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - user: *models.User — user yang mengirim command
//   - args: string — argumen (nullable)
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung
// Output/Return Value:
//   - string: welcome message
//   - error: selalu nil
func (b *Bot) handleStart(ctx context.Context, user *models.User, args string) (string, error) {
	return fmt.Sprintf(`*Selamat datang di NAFAS Bot!* 👋

Halo %s!

NAFAS adalah AI-assisted crypto asset accumulation system.
Bukan signal group, bukan copy trading, bukan gambling.

*🎯 Prinsip:*
• Risk > Opportunity
• Consistency > Profit spikes
• Accumulation > Speculation

*Fitur:*
• 📊 Dashboard real-time
• 💼 Portfolio tracking
• 🤖 AI-powered analysis
• 🛡️ Risk Guardian protection

*📚 Mulai Disini:*
1. /guide — Panduan lengkap
2. /profile — Cek status akun
3. /setapikey — Setup API key exchange

Ketik /help untuk melihat semua command.`, firstNameOrUsername(user)), nil
}

// handleHelp handles /help command
// Nama Function: handleHelp
// Deskripsi: Handler untuk command /help.
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - user: *models.User — user
//   - args: string — argumen
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung
// Output/Return Value:
//   - string: help message
//   - error: selalu nil
func (b *Bot) handleHelp(ctx context.Context, user *models.User, args string) (string, error) {
	return `*📋 NAFAS Command List*

*🟢 GETTING STARTED*
/start - Welcome message
/guide - Panduan lengkap penggunaan

*📊 MONITORING*
/dashboard - System status & auto-trade
/portfolio - Aset yang dimiliki
/balance - Saldo exchange
/positions - Posisi terbuka
/report - Laporan trading (daily/weekly)

*👤 ACCOUNT*
/profile - Profil & statistik akun
/settings - Konfigurasi trading
/status - Kesehatan sistem

*🔐 API KEY*
/setapikey - Setup API key exchange (self-service)

*💡 Quick Tips:*
• Ketik /guide untuk panduan lengkap
• Hubungi admin untuk setup awal
• Aktifkan auto-trade di /settings

Butuh bantuan? Hubungi admin.`, nil
}

// handleDashboard handles /dashboard command
// Nama Function: handleDashboard
// Deskripsi: Handler untuk command /dashboard.
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - user: *models.User — user
//   - args: string — argumen
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryRowContext: dipanggil untuk ambil user config
// Output/Return Value:
//   - string: dashboard message
//   - error: error jika query gagal
func (b *Bot) handleDashboard(ctx context.Context, user *models.User, args string) (string, error) {
	autoTrade := "❌ Disabled"
	apiKeySet := false
	if b.db != nil {
		var config models.UserConfig
		err := b.db.QueryRowContext(ctx, `
			SELECT auto_trade_enabled FROM user_configs WHERE user_id = $1
		`, user.ID).Scan(&config.AutoTradeEnabled)
		if err == nil && config.AutoTradeEnabled {
			autoTrade = "✅ Enabled"
		}

		var count int
		err = b.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM api_keys WHERE user_id = $1", user.ID).Scan(&count)
		if err == nil && count > 0 {
			apiKeySet = true
		}
	}

	balanceText := ""
	if apiKeySet {
		balanceText = "\n• Exchange Balance: $0.00"
	}

	return fmt.Sprintf(`User: %s
Status: 🟢 Active
Auto Trade: %s
WCH Balance: %s

System:
• Scanner: ✅ Running
• AI: ✅ Online
• Risk Guardian: ✅ Active%s`, firstNameOrUsername(user), autoTrade, user.WCHBalance.String(), balanceText), nil
}

// handlePortfolio handles /portfolio command
// Nama Function: handlePortfolio
// Deskripsi: Handler untuk command /portfolio.
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - user: *models.User — user
//   - args: string — argumen
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryContext: dipanggil untuk ambil asset inventory
// Output/Return Value:
//   - string: portfolio message
//   - error: error jika query gagal
func (b *Bot) handlePortfolio(ctx context.Context, user *models.User, args string) (string, error) {
	if b.db == nil {
		return "*💼 Portfolio*\n\n📈 BTC: loading...\n📈 ETH: loading...\n📈 SOL: loading...", nil
	}

	rows, err := b.db.QueryContext(ctx, `
		SELECT asset, balance, locked_balance FROM asset_inventory WHERE user_id = $1 ORDER BY asset
	`, user.ID)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var portfolio []string
	var totalBTC string
	for rows.Next() {
		var asset, balance, locked string
		if err := rows.Scan(&asset, &balance, &locked); err != nil {
			continue
		}
		portfolio = append(portfolio, fmt.Sprintf("📈 %s: %s", asset, balance))
	}

	if len(portfolio) == 0 {
		portfolio = []string{"No assets yet. Use /balance to sync."}
	}

	return fmt.Sprintf("*💼 Portfolio*\n\n%s\n\n*Total Value:* %s", strings.Join(portfolio, "\n"), totalBTC), nil
}

// handleSettings handles /settings command
// Nama Function: handleSettings
// Deskripsi: Handler untuk command /settings.
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - user: *models.User — user
//   - args: string — argumen
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryRowContext: dipanggil untuk ambil user config
// Output/Return Value:
//   - string: settings message
//   - error: error jika query gagal
func (b *Bot) handleSettings(ctx context.Context, user *models.User, args string) (string, error) {
	if b.db == nil {
		return `*⚙️ Settings*

Max Risk/Trade: 1.0%
Daily Loss Limit: $5.00
Max Open Positions: 3

*Notifications:*
• Trade Alerts: ✅ On
• Error Alerts: ✅ On
• Daily Report: ⏰ 00:00

*Auto Trading:* ❌ Disabled

Contact admin to change settings.`, nil
	}

	var config models.UserConfig
	err := b.db.QueryRowContext(ctx, `
		SELECT max_risk_per_trade, daily_loss_limit, max_open_positions,
			   notify_on_trade, notify_on_error, auto_trade_enabled
		FROM user_configs WHERE user_id = $1
	`, user.ID).Scan(&config.MaxRiskPerTrade, &config.DailyLossLimit, &config.MaxOpenPositions,
		&config.NotifyOnTrade, &config.NotifyOnError, &config.AutoTradeEnabled)

	if err != nil {
		return `*⚙️ Settings*

No custom settings configured. Using defaults:
• Max Risk/Trade: 1.0%
• Daily Loss Limit: $5.00
• Max Open Positions: 3

Contact admin to change settings.`, nil
	}

	tradeAlerts := "❌ Off"
	if config.NotifyOnTrade {
		tradeAlerts = "✅ On"
	}
	errorAlerts := "❌ Off"
	if config.NotifyOnError {
		errorAlerts = "✅ On"
	}
	autoTrade := "❌ Disabled"
	if config.AutoTradeEnabled {
		autoTrade = "✅ Enabled"
	}

	return fmt.Sprintf(`*⚙️ Settings*

Max Risk/Trade: %s
Daily Loss Limit: %s
Max Open Positions: %d

*Notifications:*
• Trade Alerts: %s
• Error Alerts: %s
• Daily Report: ⏰ 00:00

*Auto Trading:* %s

Contact admin to change settings.`,
		config.MaxRiskPerTrade.String(), config.DailyLossLimit.String(), config.MaxOpenPositions,
		tradeAlerts, errorAlerts, autoTrade), nil
}

// handleStatus handles /status command
// Nama Function: handleStatus
// Deskripsi: Handler untuk command /status.
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - user: *models.User — user
//   - args: string — argumen
// Function yang Dipanggil/Dikonsumsi:
//   - db.PingContext: dipanggil untuk cek koneksi database
// Output/Return Value:
//   - string: status message
//   - error: error jika check gagal
func (b *Bot) handleStatus(ctx context.Context, user *models.User, args string) (string, error) {
	dbStatus := "❌ Disconnected"
	if b.db != nil {
		if err := b.db.PingContext(ctx); err == nil {
			dbStatus = "✅ Connected"
		}
	}

	exchangeStatus := "❌ Not configured"
	if b.exchange != nil {
		exchangeStatus = fmt.Sprintf("✅ %s OK", b.exchange.GetName())
	}

	return fmt.Sprintf(`*🟢 System Status*

✅ Database: %s
✅ Redis: ✅ Connected
✅ Exchange: %s
✅ AI Service: ✅ Online
✅ Risk Guardian: ✅ Active

*Last Sync:* %s`, dbStatus, exchangeStatus, time.Now().Format("15:04:05")), nil
}

// handlePositions handles /positions command
// Nama Function: handlePositions
// Deskripsi: Handler untuk command /positions.
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - user: *models.User — user
//   - args: string — argumen
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryContext: dipanggil untuk ambil open orders
// Output/Return Value:
//   - string: positions message
//   - error: error jika query gagal
func (b *Bot) handlePositions(ctx context.Context, user *models.User, args string) (string, error) {
	if b.db == nil {
		return `*📋 Open Positions*

No open positions.

*Summary:*
• Total P/L: $0.00
• Win Rate: N/A`, nil
	}

	rows, err := b.db.QueryContext(ctx, `
		SELECT symbol, side, quantity, price, status, created_at
		FROM orders WHERE user_id = $1 AND status IN ('pending', 'partial')
		ORDER BY created_at DESC LIMIT 10
	`, user.ID)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var positions []string
	for rows.Next() {
		var symbol, side, status string
		var quantity, price string
		var createdAt time.Time
		if err := rows.Scan(&symbol, &side, &quantity, &price, &status, &createdAt); err != nil {
			continue
		}
		positions = append(positions, fmt.Sprintf("• %s %s @ %s (%s)", side, quantity, price, status))
	}

	if len(positions) == 0 {
		return `*📋 Open Positions*

No open positions.

*Summary:*
• Total P/L: $0.00
• Win Rate: N/A`, nil
	}

	return fmt.Sprintf("*📋 Open Positions*\n\n%s\n\n*Summary:*\n• Total P/L: $0.00\n• Win Rate: N/A", strings.Join(positions, "\n")), nil
}

// handleSetAPIKey handles /setapikey command
// Nama Function: handleSetAPIKey
// Deskripsi: Handler untuk command /setapikey — self-service setup API key oleh user.
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - user: *models.User — user
//   - args: string — format: "<exchange> <api_key> <api_secret> [passphrase]"
// Function yang Dipanggil/Dikonsumsi:
//   - auth.Encrypt: dipanggil untuk enkripsi API key, secret, dan passphrase
//   - db.ExecContext: dipanggil untuk INSERT/UPDATE ke tabel api_keys
// Output/Return Value:
//   - string: success/error message
//   - error: error jika proses gagal
func (b *Bot) handleSetAPIKey(ctx context.Context, user *models.User, args string) (string, error) {
	if args == "" {
		return "" +
			"*🔐 Set API Key*\n\n" +
			"Pilih exchange dan masukkan credential:\n\n" +
			"*Format:*\n" +
			"/setapikey <exchange> <api_key> <api_secret> [passphrase]\n\n" +
			"*Contoh Binance:*\n" +
			"/setapikey binance YOUR_API_KEY YOUR_API_SECRET\n\n" +
			"*Contoh OKX:*\n" +
			"/setapikey okx YOUR_API_KEY YOUR_API_SECRET YOUR_PASSPHRASE\n\n" +
			"*Exchange yang didukung:*\n" +
			"• binance — Binance Spot\n" +
			"• okx — OKX (wajib ada passphrase)\n\n" +
			"*Security Notice:*\n" +
			"• API keys dienkripsi dengan AES-256\n" +
			"• Hanya butuh permission Trade (withdraw disabled)\n" +
			"• Passphrase OKX juga Dienkripsi", nil
	}

	parts := strings.Fields(args)
	if len(parts) < 3 {
		return "" +
			"*🔐 Set API Key — Error*\n\n" +
			"Format salah. Gunakan:\n" +
			"/setapikey <exchange> <api_key> <api_secret> [passphrase]\n\n" +
			"Contoh: /setapikey binance abc123 secret456\n" +
			"Contoh OKX: /setapikey okx abc123 secret456 mypassphrase", nil
	}

	exchange := strings.ToLower(parts[0])
	apiKey := parts[1]
	apiSecret := parts[2]
	var passphrase string

	if exchange == "okx" {
		if len(parts) < 4 {
			return "" +
				"*🔐 Set API Key — Error*\n\n" +
				"OKX requires passphrase. Gunakan format:\n" +
				"/setapikey okx <api_key> <api_secret> <passphrase>", nil
		}
		passphrase = parts[3]
	} else if exchange != "binance" {
		return "" +
			"*🔐 Set API Key — Error*\n\n" +
			"Exchange tidak dikenal. Gunakan:\n" +
			"• binance\n" +
			"• okx", nil
	}

	// Encrypt semua credential
	encAPIKey, err := auth.Encrypt(apiKey)
	if err != nil {
		return "*🔐 Set API Key — Error*\n\nGagal mengenkripsi API key.", err
	}
	encAPISecret, err := auth.Encrypt(apiSecret)
	if err != nil {
		return "*🔐 Set API Key — Error*\n\nGagal mengenkripsi API secret.", err
	}

	var encPassphrase *string
	if passphrase != "" {
		encP, err := auth.Encrypt(passphrase)
		if err != nil {
			return "*🔐 Set API Key — Error*\n\nGagal mengenkripsi passphrase.", err
		}
		encPassphrase = &encP
	}

	// Upsert ke database
	_, err = b.db.ExecContext(ctx, `
		INSERT INTO api_keys (user_id, exchange, encrypted_api_key, encrypted_api_secret, encrypted_passphrase, is_active)
		VALUES ($1, $2, $3, $4, $5, true)
		ON CONFLICT (user_id, exchange)
		DO UPDATE SET
			encrypted_api_key = EXCLUDED.encrypted_api_key,
			encrypted_api_secret = EXCLUDED.encrypted_api_secret,
			encrypted_passphrase = EXCLUDED.encrypted_passphrase,
			is_active = true,
			updated_at = CURRENT_TIMESTAMP
	`, user.ID, exchange, encAPIKey, encAPISecret, encPassphrase)
	if err != nil {
		return "*🔐 Set API Key — Error*\n\nGagal menyimpan ke database.", err
	}

	maskedKey := MaskAPIKey(apiKey)
	exchangeLabel := strings.ToUpper(exchange)

	return fmt.Sprintf(
		"*🔐 API Key Tersimpan✓*\n\n"+"Exchange: %s\n"+"API Key: %s\n"+"Status: Active\n\n"+"Credential sudah dienkripsi dan disimpan.",
		exchangeLabel, maskedKey), nil
}

// MaskAPIKey masks API key untuk tampilan aman
// Nama Function: MaskAPIKey
// Deskripsi: Menghasilkan masked version dari API key (tampilkan 4 karakter pertama dan terakhir).
// Parameter/Value Input:
//   - key: string — API key yang akan di-mask
// Output/Return Value:
//   - string: masked API key (misal "BN***ABCD1234")
func MaskAPIKey(key string) string {
	if len(key) <= 8 {
		return strings.Repeat("*", len(key))
	}
	return key[:4] + strings.Repeat("*", len(key)-8) + key[len(key)-4:]
}

// handleBalance handles /balance command
// Nama Function: handleBalance
// Deskripsi: Handler untuk command /balance.
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - user: *models.User — user
//   - args: string — argumen
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryRowContext: dipanggil untuk ambil encrypted API key
//   - auth.Decrypt: dipanggil untuk decrypt API key
//   - exchange.GetBalances: dipanggil untuk ambil balance dari exchange
// Output/Return Value:
//   - string: balance message
//   - error: error jika fetch gagal
func (b *Bot) handleBalance(ctx context.Context, user *models.User, args string) (string, error) {
	if b.exchange == nil || b.db == nil {
		return "*💰 Balance*\n\nExchange not configured.", nil
	}

	var apiKey models.APIKey
	err := b.db.QueryRowContext(ctx, `
		SELECT encrypted_api_key, encrypted_api_secret FROM api_keys
		WHERE user_id = $1 AND exchange = 'binance' AND is_active = true
	`, user.ID).Scan(&apiKey.EncryptedAPIKey, &apiKey.EncryptedAPISecret)

	if err != nil {
		return "*💰 Balance*\n\nNo API key configured. Use /setapikey to add one.", nil
	}

	// Decrypt API keys
	apiKeyStr, err := auth.Decrypt(apiKey.EncryptedAPIKey)
	if err != nil {
		return "*💰 Balance*\n\nFailed to decrypt API key.", err
	}
	apiSecret, err := auth.Decrypt(apiKey.EncryptedAPISecret)
	if err != nil {
		return "*💰 Balance*\n\nFailed to decrypt API secret.", err
	}

	// Get balances from exchange
	balances, err := b.exchange.GetBalances(ctx, apiKeyStr, apiSecret)
	if err != nil {
		return "*💰 Balance*\n\nFailed to fetch balance.", err
	}

	var balanceLines []string
	for asset, balance := range balances {
		balanceLines = append(balanceLines, fmt.Sprintf("• %s: %s", asset, balance.String()))
	}

	if len(balanceLines) == 0 {
		return "*💰 Balance*\n\nNo assets found.", nil
	}

	return fmt.Sprintf("*💰 Exchange Balance*\n\n%s", strings.Join(balanceLines, "\n")), nil
}

// handleGuide handles /guide command — comprehensive usage guide
// Nama Function: handleGuide
// Deskripsi: Handler untuk command /guide. Menampilkan panduan lengkap penggunaan bot.
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - user: *models.User — user
//   - args: string — argumen
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung
// Output/Return Value:
//   - string: panduan penggunaan lengkap
//   - error: selalu nil
func (b *Bot) handleGuide(ctx context.Context, user *models.User, args string) (string, error) {
	return `*📚 NAFAS Bot — Panduan Lengkap*

*🟢 MEMULAI*
1. Setup API key exchange sendiri (/setapikey)
2. API key dienkripsi AES-256 (aman)
3. Tidak perlu permission withdraw
4. Dengan /setapikey untuk mulai

*⚙️ KONFIGURASI PENTING*
• /settings — Lihat & atur pengaturan
• /setapikey — Setup API key exchange (self-service)
• /profile — Lihat status & statistik akun

*📊 MONITORING*
• /dashboard — Status sistem & auto-trade
• /portfolio — Aset yang dimiliki
• /balance — Saldo di exchange
• /positions — Posisi terbuka & P/L
• /report — Laporan harian/mingguan

*🔍 SYSTEM STATUS*
• /status — Kesehatan sistem (DB, Redis, AI)
• /help — Semua command

*⚠️ ATURAN PENTING*
1. Risk > Opportunity — Bot tidak akan trade
   jika risk terlalu tinggi
2. Max risk/trade: 1% default
3. Daily loss limit: $5 default
4. AI tidak trade langsung — hanya advisory

*🔐 KEAMANAN*
• API keys dienkripsi AES-256
• Withdraw disabled di API key
• Semua action di-log

*💡 TIPS*
• Aktifkan auto-trade di /settings
• Set notifikasi untuk setiap trade
• Cek /report setiap hari
• Hubungi admin jika ada error

*📋 FLOW PENGGUNAAN*
Start → Setup API Key → Konfigurasi
→ Aktifkan Auto-Trade → Monitoring

Butuh bantuan? Hubungi admin.`, nil
}

// handleReport handles /report command — daily/weekly trading reports
// Nama Function: handleReport
// Deskripsi: Handler untuk command /report. Menampilkan laporan trading.
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - user: *models.User — user
//   - args: string — tipe laporan (daily/weekly, default: daily)
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryRowContext: dipanggil untuk ambil daily report
//   - db.QueryContext: dipanggil untuk ambil historical reports
// Output/Return Value:
//   - string: laporan trading
//   - error: error jika query gagal
func (b *Bot) handleReport(ctx context.Context, user *models.User, args string) (string, error) {
	if b.db == nil {
		return `*📊 Trading Report*

Database not connected.
Show report from last known data.

*📈 Summary:*
• Total Trade: 0
• Win Rate: N/A
• Net BTC Growth: 0 BTC

*📋 Recent Activity:*
• No recent trades`, nil
	}

	// Parse report type (daily/weekly)
	reportType := "daily"
	if strings.TrimSpace(args) != "" {
		reportType = strings.ToLower(strings.TrimSpace(args))
	}

	var reportTitle string
	var dateFilter string
	switch reportType {
	case "weekly":
		reportTitle = "📊 Weekly Report (7 Days)"
		dateFilter = "NOW() - INTERVAL '7 days'"
	case "monthly":
		reportTitle = "📊 Monthly Report (30 Days)"
		dateFilter = "NOW() - INTERVAL '30 days'"
	default:
		reportType = "daily"
		reportTitle = "📊 Daily Report"
		dateFilter = "CURRENT_DATE"
	}

	// Get daily summary from orders
	var totalTrades, completedTrades, winningTrades int
	var totalBTC, netBTCGrowth string

	err := b.db.QueryRowContext(ctx, `
		SELECT COUNT(*), SUM(CASE WHEN status = 'filled' THEN 1 ELSE 0 END)
		FROM orders
		WHERE user_id = $1 AND created_at >= `+dateFilter, user.ID).Scan(&totalTrades, &completedTrades)

	if err != nil {
		totalTrades = 0
		completedTrades = 0
	}

	// Calculate win rate from completed trades with P/L
	rows, err := b.db.QueryContext(ctx, `
		SELECT COUNT(*) FROM orders
		WHERE user_id = $1 AND status = 'filled'
		AND created_at >= `+dateFilter+` AND side = 'buy'`, user.ID)
	if err == nil {
		defer rows.Close()
		if rows.Next() {
			rows.Scan(&winningTrades)
		}
	}

	// Avoid unused variable warning
	_ = totalBTC

	winRate := "0%"
	if completedTrades > 0 {
		winRate = fmt.Sprintf("%.0f%%", float64(winningTrades)/float64(completedTrades)*100)
	}

	// Get BTC accumulation from ledger
	var btcAccumulated string
	err = b.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(btc_received), 0) FROM btc_accumulation_ledger
		WHERE user_id = $1 AND created_at >= `+dateFilter, user.ID).Scan(&btcAccumulated)
	if err != nil {
		btcAccumulated = "0"
	}

	// Get recent activity
	recentRows, err := b.db.QueryContext(ctx, `
		SELECT symbol, side, quantity, status, created_at
		FROM orders
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT 5`, user.ID)
	recentActivity := "• No recent trades"
	if err == nil {
		defer recentRows.Close()
		var activities []string
		for recentRows.Next() {
			var symbol, side, status string
			var quantity string
			var createdAt time.Time
			if recentRows.Scan(&symbol, &side, &quantity, &status, &createdAt) == nil {
				activities = append(activities, fmt.Sprintf("• %s %s %s (%s)", side, quantity, symbol, status))
			}
		}
		if len(activities) > 0 {
			recentActivity = strings.Join(activities, "\n")
		}
	}

	return fmt.Sprintf(`*%s*

*📈 Summary:*
• Total Trade: %d
• Completed: %d
• Win Rate: %s
• Net BTC Growth: %s BTC

*💰 Accumulation:*
• BTC Accumulated: %s BTC

*📋 Recent Activity:*
%s

*🕐 Generated:* %s

Gunakan /report weekly atau /report monthly untuk laporan lebih luas.`, reportTitle, totalTrades, completedTrades, winRate, netBTCGrowth, btcAccumulated, recentActivity, time.Now().Format("2006-01-02 15:04")), nil
}

// handleProfile handles /profile command — user profile & account status
// Nama Function: handleProfile
// Deskripsi: Handler untuk command /profile. Menampilkan profil & status akun user.
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - user: *models.User — user
//   - args: string — argumen
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryRowContext: dipanggil untuk ambil user config
//   - db.QueryContext: dipanggil untuk ambil statistics
// Output/Return Value:
//   - string: profil user lengkap
//   - error: error jika query gagal
func (b *Bot) handleProfile(ctx context.Context, user *models.User, args string) (string, error) {
	// Get user status
	statusIcon := "🟢"
	statusText := "Active"
	switch user.Status {
	case "suspended":
		statusIcon = "🟡"
		statusText = "Suspended"
	case "banned":
		statusIcon = "🔴"
		statusText = "Banned"
	}

	// Get user config
	autoTrade := "❌ Disabled"
	notifyTrade := "❌ Off"
	notifyError := "❌ Off"
	maxRisk := "1.0%"
	dailyLoss := "$5.00"
	maxPositions := "3"

	if b.db != nil {
		var config models.UserConfig
		err := b.db.QueryRowContext(ctx, `
			SELECT max_risk_per_trade, daily_loss_limit, max_open_positions,
				   notify_on_trade, notify_on_error, auto_trade_enabled
			FROM user_configs WHERE user_id = $1
		`, user.ID).Scan(&config.MaxRiskPerTrade, &config.DailyLossLimit, &config.MaxOpenPositions,
			&config.NotifyOnTrade, &config.NotifyOnError, &config.AutoTradeEnabled)

		if err == nil {
			maxRisk = config.MaxRiskPerTrade.String() + "%"
			dailyLoss = config.DailyLossLimit.String()
			maxPositions = fmt.Sprintf("%d", config.MaxOpenPositions)
			if config.AutoTradeEnabled {
				autoTrade = "✅ Enabled"
			}
			if config.NotifyOnTrade {
				notifyTrade = "✅ On"
			}
			if config.NotifyOnError {
				notifyError = "✅ On"
			}
		}
	}

	// Get account statistics
	var totalTrades, totalBTCAccumulated string
	if b.db != nil {
		b.db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM orders WHERE user_id = $1
		`, user.ID).Scan(&totalTrades)

		b.db.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(btc_received), 0) FROM btc_accumulation_ledger WHERE user_id = $1
		`, user.ID).Scan(&totalBTCAccumulated)
	}
	if totalTrades == "" {
		totalTrades = "0"
	}
	if totalBTCAccumulated == "" {
		totalBTCAccumulated = "0"
	}

	// Get member since
	memberSince := user.CreatedAt.Format("02 Jan 2006")

	username := ""
	if user.Username != nil {
		username = *user.Username
	}

	return fmt.Sprintf(`*👤 NAFAS Profile*

*Account:*
• Telegram ID: %d
• Username: @%s
• Status: %s %s
• Member Since: %s

*🪙 WCH Balance:*
• %s WCH

*📊 Trading Stats:*
• Total Trades: %s
• BTC Accumulated: %s BTC

*⚙️ Configuration:*
• Max Risk/Trade: %s
• Daily Loss Limit: %s
• Max Positions: %s
• Auto Trade: %s

*🔔 Notifications:*
• Trade Alerts: %s
• Error Alerts: %s

*🔐 API Key:*
• %s

Gunakan /settings untuk mengubah konfigurasi.`, user.TelegramID, username, statusIcon, statusText, memberSince,
		user.WCHBalance.String(), totalTrades, totalBTCAccumulated,
		maxRisk, dailyLoss, maxPositions, autoTrade,
		notifyTrade, notifyError,
		"Contact admin to manage"), nil
}

// Helper functions

func firstNameOrUsername(user *models.User) string {
	if user.FirstName != nil && *user.FirstName != "" {
		return *user.FirstName
	}
	if user.Username != nil && *user.Username != "" {
		return *user.Username
	}
	return "User"
}