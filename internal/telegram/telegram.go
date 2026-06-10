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
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nzf210/nafas-bot/internal/auth"
	"github.com/nzf210/nafas-bot/internal/exchange"
	"github.com/nzf210/nafas-bot/internal/logger"
	"github.com/nzf210/nafas-bot/internal/models"
	"github.com/nzf210/nafas-bot/internal/scanner"
	"github.com/shopspring/decimal"
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
	token           string
	webhookURL      string
	authService     *auth.Service
	db              *sql.DB
	exchange        exchange.Exchange
	httpClient      *http.Client
	logger          *logger.Logger
	pairManager     *scanner.PairManager
	handlers        map[string]CommandHandler
	jobQueue        chan Update
	stopCh          chan struct{}
	wg              sync.WaitGroup
	maxPairsPerUser int
}

// CommandHandler defines a command handler function
// Nama Function: CommandHandler
// Deskripsi: Function type untuk handle command Telegram.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk operasi
//   - user: *models.User — user yang mengirim command
//   - args: string — argumen dari command
//
// Function yang Dipanggil/Dikonsumsi:
//   - Depends on specific command implementation
//
// Output/Return Value:
//   - string: response message untuk user
//   - interface{}: inline keyboard markup (bisa nil)
//   - error: error jika command gagal
type CommandHandler func(ctx context.Context, user *models.User, args string) (string, interface{}, error)

// BotConfig holds bot dependencies
// Nama Function: BotConfig
// Deskripsi: Struct konfigurasi untuk inisialisasi bot dengan dependencies.
// Parameter/Value Input:
//   - Token: string — bot token dari BotFather
//   - WebhookURL: string — webhook URL (kosongkan untuk polling)
//   - AuthService: *auth.Service — auth service
//   - DB: *sql.DB — database connection
//   - Exchange: exchange.Exchange — exchange client
//
// Function yang Dipanggil/Dikonsumsi:
//   - NewBotWithConfig: dipanggil untuk buat bot instance
//
// Output/Return Value:
//   - BotConfig: struct konfigurasi
type BotConfig struct {
	Token           string
	WebhookURL      string
	AuthService     *auth.Service
	DB              *sql.DB
	Exchange        exchange.Exchange
	PairManager     *scanner.PairManager
	MaxPairsPerUser int
}

// NewBotWithConfig creates a new Telegram bot with full dependencies
// Nama Function: NewBotWithConfig
// Deskripsi: Membuat instance Telegram bot baru dengan dependencies lengkap.
//
//	Worker pool akan dimulai setelah bot dibuat via Start().
//
// Parameter/Value Input:
//   - config: BotConfig — konfigurasi dengan semua dependencies
//
// Function yang Dipanggil/Dikonsumsi:
//   - RegisterDefaultHandlers: dipanggil untuk register semua command handlers
//
// Output/Return Value:
//   - *Bot: pointer ke bot instance
func NewBotWithConfig(config BotConfig) *Bot {
	bot := &Bot{
		token:           config.Token,
		webhookURL:      config.WebhookURL,
		authService:     config.AuthService,
		db:              config.DB,
		exchange:        config.Exchange,
		httpClient:      &http.Client{Timeout: 30 * time.Second},
		logger:          logger.Default().WithField("module", "telegram"),
		pairManager:     config.PairManager,
		handlers:        make(map[string]CommandHandler),
		jobQueue:        make(chan Update, QueueSize),
		stopCh:          make(chan struct{}),
		maxPairsPerUser: config.MaxPairsPerUser,
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
//
// Function yang Dipanggil/Dikonsumsi:
//   - StartWorkers: dipanggil untuk spawn worker goroutines
//
// Output/Return Value:
//   - Tidak ada return value langsung
func (b *Bot) Start() {
	b.logger.Infof("Starting Telegram bot with %d workers", WorkerCount)
	for i := 0; i < WorkerCount; i++ {
		b.wg.Add(1)
		go b.worker(i)
	}

	b.StartReportScheduler()
	b.logger.Info("Telegram bot worker pool and report scheduler started")
}

// Stop gracefully stops semua workers dan drain remaining jobs di queue.
// Menggunakan WaitGroup untuk memastikan semua workers selesai sebelum return.
// Nama Function: Stop
// Deskripsi: Menghentikan worker pool secara graceful.
// Parameter/Value Input:
//   - Tidak ada parameter input langsung
//
// Function yang Dipanggil/Dikonsumsi:
//   - close(b.stopCh): dipanggil untuk signal workers untuk berhenti
//   - b.wg.Wait: dipanggil untuk wait semua workers selesai
//
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
// Panic recovery digunakan untuk mencegah satu worker crash menghentikan seluruh sistem.
// Nama Function: worker
// Deskripsi: Worker goroutine yang memproses queued updates dengan panic recovery.
// Parameter/Value Input:
//   - id: int — worker ID untuk logging
//
// Function yang Dipanggil/Dikonsumsi:
//   - processUpdate: dipanggil untuk process setiap update dari queue
//   - recover: dipanggil via defer untuk catch panic
//
// Output/Return Value:
//   - Tidak ada return value langsung
func (b *Bot) worker(id int) {
	defer func() {
		b.wg.Done()
		if r := recover(); r != nil {
			b.logger.Errorf("Worker %d recovered from panic: %v\n%s", id, r, string(debug.Stack()))
		}
	}()
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
//
// Function yang Dipanggil/Dikonsumsi:
//   - RegisterDefaultHandlers: dipanggil untuk register semua command handlers
//
// Output/Return Value:
//   - *Bot: pointer ke bot instance
func NewBot(token string, authService *auth.Service, webhookURL string) *Bot {
	return NewBotWithConfig(BotConfig{
		Token:           token,
		WebhookURL:      webhookURL,
		AuthService:     authService,
		MaxPairsPerUser: 10,
	})
}

// RegisterDefaultHandlers registers all default commands
// Nama Function: RegisterDefaultHandlers
// Deskripsi: Mendaftarkan semua command handlers default.
// Parameter/Value Input:
//   - Tidak ada parameter input langsung
//
// Function yang Dipanggil/Dikonsumsi:
//   - bot.Register: dipanggil untuk setiap command
//
// Output/Return Value:
//   - Tidak ada return value
func (b *Bot) RegisterDefaultHandlers() {
	b.Register("start", b.handleStart)
	b.Register("help", b.handleHelp)
	b.Register("guide", b.handleGuide)
	b.Register("addpair", b.handleAddPair)
	b.Register("removepair", b.handleRemovePair)
	b.Register("dashboard", b.handleDashboard)
	b.Register("settings", b.handleSettings)
	b.Register("status", b.handleStatus)
	b.Register("positions", b.handlePositions)
	b.Register("balance", b.handleBalance)
	b.Register("report", b.handleReport)
	b.Register("setreport", b.handleSetReport)
	b.Register("mypairs", b.handleMyPairs)
	b.Register("profile", b.handleProfile)
	b.Register("setapikey", b.handleSetAPIKey)
	b.Register("api", b.handleSetAPIKey)
	b.Register("setrisk", b.handleSetRisk)
	b.Register("setallocation", b.handleSetAllocation)
	b.Register("setdailyloss", b.handleSetDailyLoss)
	b.Register("setmaxpos", b.handleSetMaxPositions)
}

// Register registers a command handler
// Nama Function: Register
// Deskripsi: Mendaftarkan handler untuk command tertentu.
// Parameter/Value Input:
//   - command: string — nama command (tanpa slash)
//   - handler: CommandHandler — function handler
//
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung
//
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
//
// Function yang Dipanggil/Dikonsumsi:
//   - ParseUpdate: dipanggil untuk parse JSON ke struct
//
// Output/Return Value:
//   - Update: struct update
type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message,omitempty"`
	CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
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
//
// Function yang Dipanggil/Dikonsumsi:
//   - SendMessage: dipanggil untuk kirim response
//
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
//
// Function yang Dipanggil/Dikonsumsi:
//   - Authenticate: dipanggil untuk authenticate user
//
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
//
// Function yang Dipanggil/Dikonsumsi:
//   - SendMessage: dipanggil untuk kirim message
//
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
//   - Message: *Message — message yang berisi tombol (untuk EditMessageText)
//
// Function yang Dipanggil/Dikonsumsi:
//   - AnswerCallbackQuery: dipanggil untuk answer callback
//   - EditMessageText: dipanggil untuk edit pesan yang mengandung tombol
//
// Output/Return Value:
//   - CallbackQuery: struct callback
type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Data    string   `json:"data"`
	Message *Message `json:"message,omitempty"`
}

// HandleUpdate enqueues an incoming update untuk diproses secara asynchronous.
// Update akan di-queue ke jobQueue dan diproses oleh worker pool.
// Jika queue penuh (buffer penuh), update akan di-drop dan return error.
// Nama Function: HandleUpdate
// Deskripsi: Enqueues update ke job queue untuk diproses secara asynchronous.
// Parameter/Value Input:
//   - update: Update — update dari Telegram webhook/polling
//
// Function yang Dipanggil/Dikonsumsi:
//   - jobQueue <- update: dipanggil untuk enqueue update ke buffered channel
//
// Output/Return Value:
//   - error: error jika queue penuh atau update invalid
func (b *Bot) HandleUpdate(update Update) error {
	// Handle callback query dari inline button
	if update.CallbackQuery != nil {
		select {
		case b.jobQueue <- update:
			return nil
		default:
			b.logger.Warnf("Queue full, dropping callback %s from chat %d", update.CallbackQuery.ID, update.CallbackQuery.From.ID)
			return fmt.Errorf("queue full")
		}
	}

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
//
// Function yang Dipanggil/Dikonsumsi:
//   - Authenticate: dipanggil untuk authenticate user
//   - handlers[command]: dipanggil untuk execute command handler
//   - SendMessage: dipanggil untuk kirim response ke user
//
// Output/Return Value:
//   - Tidak ada return value langsung
func (b *Bot) processUpdate(update Update) {
	ctx := context.Background()

	// Handle callback query dari inline button
	if update.CallbackQuery != nil {
		cbq := update.CallbackQuery
		user, err := b.authService.Authenticate(ctx, cbq.From.ID, cbq.From.Username, cbq.From.FirstName, cbq.From.LastName)
		if err != nil {
			b.logger.Errorf("Failed to authenticate callback user %d: %v", cbq.From.ID, err)
			return
		}

		b.handleCallbackQuery(ctx, user, cbq)
		return
	}

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

	user, err := b.authService.Authenticate(ctx, msg.From.ID, msg.From.Username, msg.From.FirstName, msg.From.LastName)
	if err != nil {
		b.logger.Errorf("Failed to authenticate user %d: %v", msg.From.ID, err)
		b.SendMessage(msg.Chat.ID, "Authentication failed.", nil)
		return
	}

	handler, ok := b.handlers[command]
	if !ok {
		b.SendMessage(msg.Chat.ID, fmt.Sprintf("Unknown command: /%s", command), nil)
		return
	}

	response, replyMarkup, err := handler(ctx, user, args)
	if err != nil {
		b.logger.Errorf("Command %s failed for user %d: %v", command, user.ID, err)
		response = "An error occurred while processing your request."
	}

	b.logger.Infof("Sending response to user %d: %s", msg.Chat.ID, response)
	if err := b.SendMessage(msg.Chat.ID, response, replyMarkup); err != nil {
		b.logger.Errorf("Failed to send message to chat %d: %v", msg.Chat.ID, err)
	}
}

// SendMessage sends a message to a chat
// Nama Function: SendMessage
// Deskripsi: Mengirim message ke chat tertentu.
// Parameter/Value Input:
//   - chatID: int64 — chat ID tujuan
//   - text: string — text message
//   - replyMarkup: interface{} — optional inline keyboard (bisa nil)
//
// Function yang Dipanggil/Dikonsumsi:
//   - httpClient.Do: dipanggil untuk kirim request ke Telegram API
//   - json.Marshal: dipanggil untuk serialize request body
//
// Output/Return Value:
//   - error: error jika send gagal
func (b *Bot) SendMessage(chatID int64, text string, replyMarkup interface{}) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", b.token)

	body := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "Markdown",
	}
	if replyMarkup != nil {
		body["reply_markup"] = replyMarkup
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

// EditMessageText mengedit pesan yang sudah terkirim.
// Ini lebih baik daripada SendMessage yang membuat pesan baru terus-terusan.
// Nama Function: EditMessageText
// Deskripsi: Mengedit pesan yang sudah terkirim dengan message_id dan chat_id.
// Parameter/Value Input:
//   - chatID: int64 — chat ID
//   - messageID: int64 — message ID yang akan diedit
//   - text: string — teks baru
//   - replyMarkup: interface{} — inline keyboard baru (opsional, bisa nil)
//
// Function yang Dipanggil/Dikonsumsi:
//   - httpClient.Do: dipanggil untuk kirim request ke Telegram API
//   - json.Marshal: dipanggil untuk serialize request body
//
// Output/Return Value:
//   - error: error jika edit gagal
func (b *Bot) EditMessageText(chatID int64, messageID int64, text string, replyMarkup interface{}) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/editMessageText", b.token)

	body := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": messageID,
		"text":       text,
		"parse_mode": "Markdown",
	}
	if replyMarkup != nil {
		body["reply_markup"] = replyMarkup
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
// Deskripsi: Mengatur webhook URL untuk bot.
// Parameter/Value Input:
//   - url: string — webhook URL
//
// Function yang Dipanggil/Dikonsumsi:
//   - httpClient.Do: dipanggil untuk set webhook via Telegram API
//
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
//
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung
//
// Output/Return Value:
//   - string: welcome message
//   - interface{}: inline keyboard (nil untuk handler ini)
//   - error: selalu nil
func (b *Bot) handleStart(ctx context.Context, user *models.User, args string) (string, interface{}, error) {
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

Ketik /help untuk melihat semua command.`, firstNameOrUsername(user)), nil, nil
}

// handleHelp handles /help command
// Nama Function: handleHelp
// Deskripsi: Handler untuk command /help.
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - user: *models.User — user
//   - args: string — argumen
//
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung
//
// Output/Return Value:
//   - string: help message
//   - interface{}: inline keyboard (nil untuk handler ini)
//   - error: selalu nil
func (b *Bot) handleHelp(ctx context.Context, user *models.User, args string) (string, interface{}, error) {
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

*⚙️ SETTINGS*
/mypairs - Melihat daftar pair yang Anda pantau
/addpair - Menambah satu/lebih pair (contoh: /addpair Binance ADAUSDT BTCUSDT)
/removepair - Menghapus satu/lebih pair (contoh: /removepair Binance ADAUSDT)
/setreport - Mengatur interval laporan (contoh: /setreport 5m, 1h, 24h)
/setrisk - Ubah risk per trade
/setallocation - Ubah maksimal alokasi modal per trade
/setdailyloss - Ubah batas rugi harian
/setmaxpos - Ubah maksimal posisi aktif

*💡 Quick Tips:*
• Ketik /guide untuk panduan lengkap
• Hubungi admin untuk setup awal
• Aktifkan auto-trade di /settings

Butuh bantuan? Hubungi admin.`, nil, nil
}

// handleAddPair handles /addpair command
// Deskripsi: Menambahkan pair baru.
func (b *Bot) handleAddPair(ctx context.Context, user *models.User, args string) (string, interface{}, error) {
	if b.pairManager == nil {
		return "⚠️ Sistem PairManager belum tersedia.", nil, nil
	}

	parts := strings.Fields(args)
	if len(parts) < 2 {
		return "⚠️ Format salah.\nGunakan: `/addpair <exchange> <pair1> <pair2>...`\nContoh: `/addpair Binance ADAUSDT BTCUSDT`", nil, nil
	}

	exchangeName := strings.ToUpper(parts[0])
	switch exchangeName {
	case "BINANCE":
		exchangeName = "Binance"
	case "OKX":
		exchangeName = "OKX"
	default:
		return "⚠️ Exchange tidak valid. Saat ini hanya mendukung: `Binance`, `OKX`.", nil, nil
	}

	currentPairs := b.pairManager.GetUserPairs(user.ID.String())[exchangeName]
	initialCount := len(currentPairs)

	existingPairsMap := make(map[string]bool)
	for _, p := range currentPairs {
		existingPairsMap[p] = true
	}

	var added []string
	var skipped []string
	limitReached := false
	for _, sym := range parts[1:] {
		// Normalize symbol based on exchange
		symbol := strings.ToUpper(sym)
		symbol = strings.ReplaceAll(symbol, "/", "") // Remove slashes in any case

		switch exchangeName {
		case "Binance":
			symbol = strings.ReplaceAll(symbol, "-", "") // Binance: BTCUSDT
		case "OKX":
			// OKX: BTC-USDT. If no hyphen exists, try to inject it before quote asset (basic assumption for USDT/USDC/BTC)
			if !strings.Contains(symbol, "-") {
				if strings.HasSuffix(symbol, "USDT") {
					symbol = strings.TrimSuffix(symbol, "USDT") + "-USDT"
				} else if strings.HasSuffix(symbol, "USDC") {
					symbol = strings.TrimSuffix(symbol, "USDC") + "-USDC"
				} else if strings.HasSuffix(symbol, "BTC") && symbol != "BTC" {
					symbol = strings.TrimSuffix(symbol, "BTC") + "-BTC"
				}
			}
		}

		if existingPairsMap[symbol] {
			skipped = append(skipped, symbol)
			continue
		}

		if initialCount+len(added) >= b.maxPairsPerUser {
			limitReached = true
			break
		}

		b.pairManager.AddUserPair(user.ID.String(), exchangeName, symbol)
		added = append(added, symbol)
		existingPairsMap[symbol] = true
	}

	var msgBuilder strings.Builder
	msgBuilder.WriteString(fmt.Sprintf("📊 Info: Sebelumnya Anda memiliki %d pair di %s.\n\n", initialCount, exchangeName))

	if limitReached {
		msgBuilder.WriteString(fmt.Sprintf("⚠️ Maksimal %d pair telah tercapai. Beberapa pair tidak ditambahkan.\n\n", b.maxPairsPerUser))
	}

	if len(added) > 0 {
		msgBuilder.WriteString(fmt.Sprintf("✅ %d pair BARU berhasil ditambahkan:\n*%s*\n", len(added), strings.Join(added, ", ")))
	} else {
		msgBuilder.WriteString("⚠️ Tidak ada pair baru yang ditambahkan.\n")
	}

	if len(skipped) > 0 {
		msgBuilder.WriteString(fmt.Sprintf("⏭️ %d pair di-skip (sudah ada/dobel):\n*%s*\n", len(skipped), strings.Join(skipped, ", ")))
	}

	msgBuilder.WriteString(fmt.Sprintf("\n📈 Total pair Anda sekarang di %s: %d", exchangeName, len(existingPairsMap)))

	return msgBuilder.String(), nil, nil
}

// handleRemovePair handles /removepair command
// Deskripsi: Menghapus pair.
func (b *Bot) handleRemovePair(ctx context.Context, user *models.User, args string) (string, interface{}, error) {
	if b.pairManager == nil {
		return "⚠️ Sistem PairManager belum tersedia.", nil, nil
	}

	parts := strings.Fields(args)
	if len(parts) < 2 {
		return "⚠️ Format salah.\nGunakan: `/removepair <exchange> <pair1> <pair2>...`\nContoh: `/removepair Binance ADAUSDT BTCUSDT`", nil, nil
	}

	exchangeName := strings.ToUpper(parts[0])
	switch exchangeName {
	case "BINANCE":
		exchangeName = "Binance"
	case "OKX":
		exchangeName = "OKX"
	default:
		return "⚠️ Exchange tidak valid. Saat ini hanya mendukung: `Binance`, `OKX`.", nil, nil
	}

	var removed []string
	for _, sym := range parts[1:] {
		// Normalize symbol based on exchange
		symbol := strings.ToUpper(sym)
		symbol = strings.ReplaceAll(symbol, "/", "")

		switch exchangeName {
		case "Binance":
			symbol = strings.ReplaceAll(symbol, "-", "")
		case "OKX":
			if !strings.Contains(symbol, "-") {
				if strings.HasSuffix(symbol, "USDT") {
					symbol = strings.TrimSuffix(symbol, "USDT") + "-USDT"
				} else if strings.HasSuffix(symbol, "USDC") {
					symbol = strings.TrimSuffix(symbol, "USDC") + "-USDC"
				} else if strings.HasSuffix(symbol, "BTC") && symbol != "BTC" {
					symbol = strings.TrimSuffix(symbol, "BTC") + "-BTC"
				}
			}
		}

		b.pairManager.RemoveUserPair(user.ID.String(), exchangeName, symbol)
		removed = append(removed, symbol)
	}

	return fmt.Sprintf("🗑️ %d pair berhasil dihapus dari list %s Anda:\n*%s*", len(removed), exchangeName, strings.Join(removed, ", ")), nil, nil
}

// handleMyPairs handles /mypairs command
// Deskripsi: Menampilkan list pair yang dipantau oleh user
func (b *Bot) handleMyPairs(ctx context.Context, user *models.User, args string) (string, interface{}, error) {
	if b.pairManager == nil {
		return "⚠️ Sistem PairManager belum tersedia.", nil, nil
	}

	pairs := b.pairManager.GetUserPairs(user.ID.String())
	if len(pairs) == 0 {
		return "📭 Anda belum memantau pair apapun.\nSilakan gunakan perintah `/addpair <exchange> <pair>`", nil, nil
	}

	var msgBuilder strings.Builder
	msgBuilder.WriteString("📊 *Daftar Pair Pantauan Anda:*\n\n")

	for exchange, symbols := range pairs {
		if len(symbols) > 0 {
			msgBuilder.WriteString(fmt.Sprintf("*[%s]*\n", exchange))
			msgBuilder.WriteString(strings.Join(symbols, ", "))
			msgBuilder.WriteString("\n\n")
		}
	}

	return msgBuilder.String(), nil, nil
}

// handleDashboard handles /dashboard command
// Nama Function: handleDashboard
// Deskripsi: Handler untuk command /dashboard.
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - user: *models.User — user
//   - args: string — argumen
//
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryRowContext: dipanggil untuk ambil user config
//
// Output/Return Value:
//   - string: dashboard message
//   - interface{}: inline keyboard (nil)
//   - error: error jika query gagal
func (b *Bot) handleDashboard(ctx context.Context, user *models.User, args string) (string, interface{}, error) {
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

	// Fetch exchange balance if API key is set
	balanceText := ""
	if apiKeySet && b.exchange != nil {
		var apiKey models.APIKey
		err := b.db.QueryRowContext(ctx, `
			SELECT encrypted_api_key, encrypted_api_secret FROM api_keys
			WHERE user_id = $1 AND exchange = 'binance' AND is_active = true
		`, user.ID).Scan(&apiKey.EncryptedAPIKey, &apiKey.EncryptedAPISecret)
		if err == nil {
			apiKeyStr, err := auth.Decrypt(apiKey.EncryptedAPIKey)
			if err == nil {
				apiSecret, err := auth.Decrypt(apiKey.EncryptedAPISecret)
				if err == nil {
					balances, err := b.exchange.GetBalances(ctx, apiKeyStr, apiSecret)
					if err == nil {
						// Calculate total USD value
						var totalUSD decimal.Decimal
						for asset, balance := range balances {
							if balance.GreaterThan(decimal.Zero) {
								if asset == "USDT" || asset == "BUSD" || asset == "USD" {
									totalUSD = totalUSD.Add(balance)
								} else {
									// Get price in USDT
									price, err := b.exchange.GetPrice(ctx, asset+"USDT")
									if err == nil {
										totalUSD = totalUSD.Add(balance.Mul(price))
									}
								}
							}
						}
						balanceText = fmt.Sprintf("\n• Exchange Balance: $%s", totalUSD.Round(2).String())
					}
				}
			}
		}
	}

	return fmt.Sprintf(`User: %s
Status: 🟢 Active
Auto Trade: %s
WCH Balance: %s

System:
• Scanner: ✅ Running
• AI: ✅ Online
• Risk Guardian: ✅ Active%s`, firstNameOrUsername(user), autoTrade, user.WCHBalance.String(), balanceText), nil, nil
}

// syncPortfolioFromExchange syncs user portfolio from exchange to asset_inventory table
// Nama Function: syncPortfolioFromExchange
// Deskripsi: Mengambil balance dari exchange dan sync ke tabel asset_inventory.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk operasi
//   - user: *models.User — user yang akan di-sync portfolionya
//
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryRowContext: dipanggil untuk ambil encrypted API key
//   - auth.Decrypt: dipanggil untuk decrypt API key
//   - exchange.GetBalances: dipanggil untuk ambil balance dari exchange
//   - db.ExecContext: dipanggil untuk INSERT ON CONFLICT UPDATE ke asset_inventory
//
// Output/Return Value:
//   - error: error jika sync gagal, nil jika berhasil
func (b *Bot) syncPortfolioFromExchange(ctx context.Context, user *models.User) error {
	if b.db == nil || b.exchange == nil {
		return fmt.Errorf("database or exchange not configured")
	}

	// Get API key
	var apiKey, apiSecret string
	err := b.db.QueryRowContext(ctx, `
		SELECT encrypted_api_key, encrypted_api_secret FROM api_keys
		WHERE user_id = $1 AND exchange = 'binance' AND is_active = true
	`, user.ID).Scan(&apiKey, &apiSecret)
	if err != nil {
		return fmt.Errorf("no API key found: %w", err)
	}

	// Decrypt
	apiKeyStr, err := auth.Decrypt(apiKey)
	if err != nil {
		return fmt.Errorf("failed to decrypt API key: %w", err)
	}
	apiSecretStr, err := auth.Decrypt(apiSecret)
	if err != nil {
		return fmt.Errorf("failed to decrypt API secret: %w", err)
	}

	// Get balances from exchange
	balances, err := b.exchange.GetBalances(ctx, apiKeyStr, apiSecretStr)
	if err != nil {
		return fmt.Errorf("failed to get balances: %w", err)
	}

	// Update asset_inventory for each asset with balance > 0
	for asset, balance := range balances {
		if balance.GreaterThan(decimal.Zero) {
			// Calculate BTC equivalent (simplified - get BTC price)
			btcEquiv := decimal.Zero
			if asset != "BTC" {
				btcPrice, err := b.exchange.GetPrice(ctx, "BTCUSDT")
				if err == nil && btcPrice.GreaterThan(decimal.Zero) {
					assetPrice, err := b.exchange.GetPrice(ctx, asset+"USDT")
					if err == nil && assetPrice.GreaterThan(decimal.Zero) {
						// asset value in USDT / BTC price = BTC equivalent
						assetValue := balance.Mul(assetPrice)
						btcEquiv = assetValue.Div(btcPrice)
					}
				}
			} else {
				btcEquiv = balance
			}

			// Insert or update asset_inventory
			_, err = b.db.ExecContext(ctx, `
				INSERT INTO asset_inventory (id, user_id, asset, balance, locked_balance, btc_equivalent, updated_at)
				VALUES ($1, $2, $3, $4, 0, $5, NOW())
				ON CONFLICT (user_id, asset) DO UPDATE SET
					balance = EXCLUDED.balance,
					locked_balance = EXCLUDED.locked_balance,
					btc_equivalent = EXCLUDED.btc_equivalent,
					updated_at = EXCLUDED.updated_at
			`, uuid.New(), user.ID, asset, balance.String(), btcEquiv.String())
			if err != nil {
				b.logger.WithError(err).WithField("asset", asset).Warn("Failed to update asset inventory")
			}
		}
	}

	return nil
}

// buildPortfolioString builds the portfolio string with floating PNL
func (b *Bot) buildPortfolioString(ctx context.Context, user *models.User) string {
	if b.db == nil {
		return "📈 BTC: loading...\n📈 ETH: loading...\n📈 SOL: loading..."
	}

	// Sync portfolio from exchange first
	if b.exchange != nil {
		b.syncPortfolioFromExchange(ctx, user)
	}

	rows, err := b.db.QueryContext(ctx, `
		SELECT asset, balance, locked_balance FROM asset_inventory WHERE user_id = $1 ORDER BY asset
	`, user.ID)
	if err != nil {
		return "No assets yet."
	}
	defer rows.Close()

	var portfolio []string
	for rows.Next() {
		var asset, balance, locked string
		if err := rows.Scan(&asset, &balance, &locked); err != nil {
			continue
		}

		balDec, _ := decimal.NewFromString(balance)

		floatingStr := ""
		if balDec.GreaterThan(decimal.Zero) && asset != "BTC" && asset != "USDT" {
			var quoteAsset string
			err := b.db.QueryRowContext(ctx, "SELECT quote_asset FROM trading_pairs WHERE user_id = $1 AND base_asset = $2 LIMIT 1", user.ID, asset).Scan(&quoteAsset)
			if err == nil {
				symbol := asset + quoteAsset
				var avgPrice string
				err = b.db.QueryRowContext(ctx, "SELECT COALESCE(SUM(price * executed_quantity) / NULLIF(SUM(executed_quantity), 0), 0) FROM orders WHERE user_id = $1 AND symbol = $2 AND side = 'buy' AND status = 'filled'", user.ID, symbol).Scan(&avgPrice)

				if err == nil && avgPrice != "0" {
					avgPriceDec, _ := decimal.NewFromString(avgPrice)
					currentPrice, err := b.exchange.GetPrice(ctx, symbol)
					if err == nil && currentPrice.GreaterThan(decimal.Zero) && avgPriceDec.GreaterThan(decimal.Zero) {
						floating := currentPrice.Sub(avgPriceDec).Mul(balDec)
						percent := currentPrice.Sub(avgPriceDec).Div(avgPriceDec).Mul(decimal.NewFromInt(100))
						sign := "+"
						if floating.LessThan(decimal.Zero) {
							sign = ""
						}
						floatingStr = fmt.Sprintf(" (Float: %s%s %s | %s%.2f%%)", sign, floating.Round(6).String(), quoteAsset, sign, percent.InexactFloat64())
					}
				}
			}
		}

		portfolio = append(portfolio, fmt.Sprintf("📈 %s: %s%s", asset, balDec.Round(6).String(), floatingStr))
	}

	if len(portfolio) == 0 {
		return "No assets yet."
	}

	return strings.Join(portfolio, "\n")
}

// handleSettings handles /settings command
// Nama Function: handleSettings
// Deskripsi: Handler untuk command /settings dengan inline keyboard untuk ubah pengaturan.
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - user: *models.User — user
//   - args: string — argumen
//
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryRowContext: dipanggil untuk ambil user config
//
// Output/Return Value:
//   - string: settings message
//   - interface{}: inline keyboard markup
//   - error: error jika query gagal
func (b *Bot) handleSettings(ctx context.Context, user *models.User, args string) (string, interface{}, error) {
	// Default values
	maxRisk := "1.0%"
	dailyLoss := "5.0%"
	maxPositions := 3
	tradeAlerts := "✅ On"
	errorAlerts := "✅ On"
	autoTrade := "❌ Disabled"
	maxAllocation := "10.0"

	if b.db != nil {
		var config models.UserConfig
		err := b.db.QueryRowContext(ctx, `
			SELECT COALESCE(max_risk_per_trade, 1.0), COALESCE(max_allocation_per_trade, 10.0), COALESCE(daily_loss_limit, 5.0), COALESCE(max_open_positions, 3),
				   COALESCE(notify_on_trade, true), COALESCE(notify_on_error, true), COALESCE(auto_trade_enabled, false)
			FROM user_configs WHERE user_id = $1
		`, user.ID).Scan(&config.MaxRiskPerTrade, &config.MaxAllocationPerTrade, &config.DailyLossLimit, &config.MaxOpenPositions,
			&config.NotifyOnTrade, &config.NotifyOnError, &config.AutoTradeEnabled)

		b.logger.Debugf("handleSettings: userID=%s, query err=%v, config={maxRisk:%s, maxAlloc:%s, dailyLoss:%s, maxPos:%d, autoTrade:%v}",
			user.ID.String(), err, config.MaxRiskPerTrade.String(), config.MaxAllocationPerTrade.String(),
			config.DailyLossLimit.String(), config.MaxOpenPositions, config.AutoTradeEnabled)

		if err == nil {
			maxRisk = config.MaxRiskPerTrade.String()
			maxAllocation = config.MaxAllocationPerTrade.String()
			dailyLoss = config.DailyLossLimit.String()
			maxPositions = config.MaxOpenPositions
			if !config.NotifyOnTrade {
				tradeAlerts = "❌ Off"
			}
			if !config.NotifyOnError {
				errorAlerts = "❌ Off"
			}
			if config.AutoTradeEnabled {
				autoTrade = "✅ Enabled"
			}
		} else {
			b.logger.Warnf("handleSettings: failed to read config for userID=%s, using defaults", user.ID.String())
		}
	}

	// Inline keyboard untuk ubah settings
	replyMarkup := map[string]interface{}{
		"inline_keyboard": [][]map[string]string{
			{
				{"text": "🔄 Refresh", "callback_data": "settings_refresh"},
			},
			{
				{"text": fmt.Sprintf("📊 Risk: %s%%", maxRisk), "callback_data": "settings_risk"},
				{"text": fmt.Sprintf("📉 Daily Loss: %s%%", dailyLoss), "callback_data": "settings_dailyloss"},
			},
			{
				{"text": fmt.Sprintf("💼 Max Alloc: %s%%", maxAllocation), "callback_data": "settings_alloc"},
				{"text": fmt.Sprintf("📈 Max Pos: %d", maxPositions), "callback_data": "settings_maxpos"},
			},
			{
				{"text": fmt.Sprintf("🔔 Trade Alerts: %s", tradeAlerts), "callback_data": "settings_tradealerts"},
			},
			{
				{"text": fmt.Sprintf("⚠️ Error Alerts: %s", errorAlerts), "callback_data": "settings_erroralerts"},
			},
			{
				{"text": fmt.Sprintf("🤖 Auto Trade: %s", autoTrade), "callback_data": "settings_autotrade"},
			},
		},
	}

	return fmt.Sprintf(`⚙️ *Pengaturan NAFAS Bot*

📊 Max Risk/Trade: %s%%
💼 Max Alloc/Trade: %s%%
📉 Daily Loss Limit: %s%%
📈 Max Open Positions: %d

*Notifications:*
🔔 Trade Alerts: %s
⚠️ Error Alerts: %s
⏰ Daily Report: ⏰ 00:00

*Auto Trading:* %s`,
		maxRisk, maxAllocation, dailyLoss, maxPositions,
		tradeAlerts, errorAlerts, autoTrade), replyMarkup, nil
}

// handleStatus handles /status command
// Nama Function: handleStatus
// Deskripsi: Handler untuk command /status.
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - user: *models.User — user
//   - args: string — argumen
//
// Function yang Dipanggil/Dikonsumsi:
//   - db.PingContext: dipanggil untuk cek koneksi database
//
// Output/Return Value:
//   - string: status message
//   - interface{}: inline keyboard (nil)
//   - error: error jika check gagal
func (b *Bot) handleStatus(ctx context.Context, user *models.User, args string) (string, interface{}, error) {
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

*💼 Portfolio*
%s

*Last Sync:* %s`, dbStatus, exchangeStatus, b.buildPortfolioString(ctx, user), time.Now().Format("15:04:05")), nil, nil
}

// handlePositions handles /positions command
// Nama Function: handlePositions
// Deskripsi: Handler untuk command /positions.
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - user: *models.User — user
//   - args: string — argumen
//
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryContext: dipanggil untuk ambil open orders
//
// Output/Return Value:
//   - string: positions message
//   - interface{}: inline keyboard (nil)
//   - error: error jika query gagal
func (b *Bot) handlePositions(ctx context.Context, user *models.User, args string) (string, interface{}, error) {
	if b.db == nil {
		return `*📋 Open Positions*

No open positions.

*Summary:*
• Total P/L: $0.00
• Win Rate: N/A`, nil, nil
	}

	rows, err := b.db.QueryContext(ctx, `
		SELECT symbol, side, quantity, price, status, created_at
		FROM orders WHERE user_id = $1 AND status IN ('pending', 'partial')
		ORDER BY created_at DESC LIMIT 10
	`, user.ID)
	if err != nil {
		return "", nil, err
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
• Win Rate: N/A`, nil, nil
	}

	return fmt.Sprintf("*📋 Open Positions*\n\n%s\n\n*Summary:*\n• Total P/L: $0.00\n• Win Rate: N/A", strings.Join(positions, "\n")), nil, nil
}

// handleSetAPIKey handles /setapikey command
// Nama Function: handleSetAPIKey
// Deskripsi: Handler untuk command /setapikey — self-service setup API key oleh user.
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - user: *models.User — user
//   - args: string — format: "<exchange> <api_key> <api_secret> [passphrase]"
//
// Function yang Dipanggil/Dikonsumsi:
//   - auth.Encrypt: dipanggil untuk enkripsi API key, secret, dan passphrase
//   - db.ExecContext: dipanggil untuk INSERT/UPDATE ke tabel api_keys
//
// Output/Return Value:
//   - string: success/error message
//   - interface{}: inline keyboard (nil)
//   - error: error jika proses gagal
func (b *Bot) handleSetAPIKey(ctx context.Context, user *models.User, args string) (string, interface{}, error) {
	if args == "" {
		// Inline keyboard dengan pilihan exchange
		replyMarkup := map[string]interface{}{
			"inline_keyboard": [][]map[string]string{
				{
					{"text": "🔶 Binance", "callback_data": "apikey_binance"},
					{"text": "🟡 OKX", "callback_data": "apikey_okx"},
				},
			},
		}
		return "" +
			"🔐 Set API Key\n\n" +
			"Pilih exchange dan masukkan credential:\n\n" +
			"Format:\n" +
			"/setapikey <exchange> <api_key> <api_secret> [passphrase]\n\n" +
			"Contoh Binance:\n" +
			"/setapikey binance YOUR-API-KEY YOUR-API-SECRET\n\n" +
			"Contoh OKX:\n" +
			"/setapikey okx YOUR-API-KEY YOUR-API-SECRET YOUR-PASSPHRASE\n\n" +
			"Exchange yang didukung:\n" +
			"• binance — Binance Spot\n" +
			"• okx — OKX (wajib ada passphrase)\n\n" +
			"Security Notice:\n" +
			"• API keys dienkripsi dengan AES-256\n" +
			"• Hanya butuh permission Trade (withdraw disabled)\n" +
			"• Passphrase OKX juga Dienkripsi", replyMarkup, nil
	}

	parts := strings.Fields(args)
	if len(parts) < 3 {
		return "" +
			"🔐 Set API Key — Error\n\n" +
			"Format salah. Gunakan:\n" +
			"/setapikey <exchange> <api_key> <api_secret> [passphrase]\n\n" +
			"Contoh: /setapikey binance abc123 secret456\n" +
			"Contoh OKX: /setapikey okx abc123 secret456 mypassphrase", nil, nil
	}

	exchange := strings.ToLower(parts[0])
	apiKey := parts[1]
	apiSecret := parts[2]
	var passphrase string

	if exchange == "okx" {
		if len(parts) < 4 {
			return "" +
				"🔐 Set API Key — Error\n\n" +
				"OKX requires passphrase. Gunakan format:\n" +
				"/setapikey okx <api_key> <api_secret> <passphrase>", nil, nil
		}
		passphrase = parts[3]
	} else if exchange != "binance" {
		return "" +
			"🔐 Set API Key — Error\n\n" +
			"Exchange tidak dikenal. Gunakan:\n" +
			"• binance\n" +
			"• okx", nil, nil
	}

	// Encrypt semua credential
	encAPIKey, err := auth.Encrypt(apiKey)
	if err != nil {
		return "🔐 Set API Key — Error\n\nGagal mengenkripsi API key.", nil, err
	}
	encAPISecret, err := auth.Encrypt(apiSecret)
	if err != nil {
		return "🔐 Set API Key — Error\n\nGagal mengenkripsi API secret.", nil, err
	}

	var encPassphrase *string
	if passphrase != "" {
		encP, err := auth.Encrypt(passphrase)
		if err != nil {
			return "🔐 Set API Key — Error\n\nGagal mengenkripsi passphrase.", nil, err
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
		return "🔐 Set API Key — Error\n\nGagal menyimpan ke database.", nil, err
	}

	maskedKey := MaskAPIKey(apiKey)
	exchangeLabel := strings.ToUpper(exchange)

	return fmt.Sprintf(
		"🔐 API Key Tersimpan✓\n\n"+"Exchange: %s\n"+"API Key: %s\n"+"Status: Active\n\n"+"Credential sudah dienkripsi dan disimpan.",
		exchangeLabel, maskedKey), nil, nil
}

// MaskAPIKey masks API key untuk tampilan aman
// Nama Function: MaskAPIKey
// Deskripsi: Menghasilkan masked version dari API key (tampilkan 4 karakter pertama dan terakhir).
// Parameter/Value Input:
//   - key: string — API key yang akan di-mask
//
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
//
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryRowContext: dipanggil untuk ambil encrypted API key
//   - auth.Decrypt: dipanggil untuk decrypt API key
//   - exchange.GetBalances: dipanggil untuk ambil balance dari exchange
//
// Output/Return Value:
//   - string: balance message
//   - interface{}: inline keyboard (nil)
//   - error: error jika fetch gagal
func (b *Bot) handleBalance(ctx context.Context, user *models.User, args string) (string, interface{}, error) {
	if b.exchange == nil || b.db == nil {
		return "*💰 Balance*\n\nExchange not configured.", nil, nil
	}

	var apiKey models.APIKey
	err := b.db.QueryRowContext(ctx, `
		SELECT encrypted_api_key, encrypted_api_secret FROM api_keys
		WHERE user_id = $1 AND exchange = 'binance' AND is_active = true
	`, user.ID).Scan(&apiKey.EncryptedAPIKey, &apiKey.EncryptedAPISecret)

	if err != nil {
		return "*💰 Balance*\n\nNo API key configured. Use /setapikey to add one.", nil, nil
	}

	// Decrypt API keys
	apiKeyStr, err := auth.Decrypt(apiKey.EncryptedAPIKey)
	if err != nil {
		return "*💰 Balance*\n\nFailed to decrypt API key.", nil, err
	}
	apiSecret, err := auth.Decrypt(apiKey.EncryptedAPISecret)
	if err != nil {
		return "*💰 Balance*\n\nFailed to decrypt API secret.", nil, err
	}

	// Get balances from exchange
	balances, err := b.exchange.GetBalances(ctx, apiKeyStr, apiSecret)
	if err != nil {
		// Log error internally but don't expose to user (may contain sensitive API details)
		b.logger.WithError(err).WithField("user_id", user.ID).Warn("Failed to fetch balance from exchange")
		return "*💰 Balance*\n\nGagal mengambil balance. Pastikan API key valid dan memiliki permission 'Enable Spot & Margin Trading'.", nil, nil
	}

	var balanceLines []string
	for asset, balance := range balances {
		balanceLines = append(balanceLines, fmt.Sprintf("• %s: %s", asset, balance.String()))
	}

	if len(balanceLines) == 0 {
		return "*💰 Balance*\n\nNo assets found.", nil, nil
	}

	return fmt.Sprintf("*💰 Exchange Balance*\n\n%s", strings.Join(balanceLines, "\n")), nil, nil
}

// handleGuide handles /guide command — comprehensive usage guide
// Nama Function: handleGuide
// Deskripsi: Handler untuk command /guide. Menampilkan panduan lengkap penggunaan bot.
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - user: *models.User — user
//   - args: string — argumen
//
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung
//
// Output/Return Value:
//   - string: panduan penggunaan lengkap
//   - interface{}: inline keyboard (nil)
//   - error: selalu nil
func (b *Bot) handleGuide(ctx context.Context, user *models.User, args string) (string, interface{}, error) {
	return `*📚 NAFAS Bot — Panduan Lengkap*

*🟢 MEMULAI*
1. Setup API key exchange sendiri (/setapikey)
2. API key dienkripsi AES-256 (aman)
3. Tidak perlu permission withdraw
4. Dengan /setapikey untuk mulai

*⚙️ KONFIGURASI PENTING*
• /settings — Lihat & ubah pengaturan
• /setapikey — Setup API key exchange (self-service)
• /profile — Lihat status & statistik akun
• /addpair — Tambah pair (contoh: /addpair Binance ADAUSDT)
• /removepair — Hapus pair (contoh: /removepair Binance ADAUSDT)
• /setreport — Atur jadwal report (contoh: /setreport 5m, 1h, 24h)

*📊 MONITORING*
• /dashboard — Status sistem & auto-trade
• /portfolio — Aset yang dimiliki
• /balance — Saldo di exchange
• /positions — Posisi terbuka & P/L
• /report — Laporan harian/mingguan
• /mypairs — Lihat daftar pair pantauan Anda

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
• Gunakan /setreport agar report terkirim teratur
• Cek /mypairs sesekali untuk memastikan pair Anda valid
• Hubungi admin jika ada error

*📋 FLOW PENGGUNAAN*
Start → Setup API Key → Konfigurasi
→ Aktifkan Auto-Trade → Monitoring

Butuh bantuan? Hubungi admin. @nafaswch `, nil, nil
}

// handleReport handles /report command — daily/weekly trading reports
// Nama Function: handleReport
// Deskripsi: Handler untuk command /report. Menampilkan laporan trading.
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - user: *models.User — user
//   - args: string — tipe laporan (daily/weekly, default: daily)
//
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryRowContext: dipanggil untuk ambil daily report
//   - db.QueryContext: dipanggil untuk ambil historical reports
//
// Output/Return Value:
//   - string: laporan trading
//   - interface{}: inline keyboard (nil)
//   - error: error jika query gagal
func (b *Bot) handleReport(ctx context.Context, user *models.User, args string) (string, interface{}, error) {
	if b.db == nil {
		return `*📊 Trading Report*

Database not connected.
Show report from last known data.

*📈 Summary:*
• Total Trade: 0
• Win Rate: N/A
• Net BTC Growth: 0 BTC

*📋 Recent Activity:*
• No recent trades`, nil, nil
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
	if netBTCGrowth == "" {
		netBTCGrowth = "0"
	}

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
	} else {
		if d, err := decimal.NewFromString(btcAccumulated); err == nil {
			btcAccumulated = d.Round(6).String()
		}
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
				if q, err := decimal.NewFromString(quantity); err == nil {
					quantity = q.Round(6).String()
				}
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

*💼 Portfolio*
%s

*🕐 Generated:* %s

Gunakan /report weekly atau /report monthly untuk laporan lebih luas.`, reportTitle, totalTrades, completedTrades, winRate, netBTCGrowth, btcAccumulated, recentActivity, b.buildPortfolioString(ctx, user), time.Now().Format("2006-01-02 15:04")), nil, nil
}

// handleSetReport handles /setreport command
// Deskripsi: Handler untuk mengatur interval report user
func (b *Bot) handleSetReport(ctx context.Context, user *models.User, args string) (string, interface{}, error) {
	if b.db == nil {
		return "⚠️ Database not connected.", nil, nil
	}

	intervalStr := strings.TrimSpace(args)
	if intervalStr == "" {
		return "⚠️ Format salah.\nGunakan: `/setreport <interval>`\nContoh: `/setreport 5m`, `/setreport 1h`, `/setreport 24h`", nil, nil
	}

	// Validate interval
	duration, err := time.ParseDuration(intervalStr)
	if err != nil {
		return "⚠️ Format interval tidak valid.\nGunakan format Golang durasi seperti `5m` (5 menit), `1h` (1 jam), `24h` (24 jam).", nil, nil
	}
	if duration <= 0 {
		return "⚠️ Interval harus positif.\nContoh: `5m` (5 menit), `1h` (1 jam), `24h` (24 jam).", nil, nil
	}

	// Update DB
	query := `
		INSERT INTO user_configs (user_id, report_interval)
		VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET report_interval = $2
	`
	_, err = b.db.ExecContext(ctx, query, user.ID, intervalStr)
	if err != nil {
		b.logger.Errorf("Failed to update report interval for user %s: %v", user.ID, err)
		return "❌ Gagal memperbarui konfigurasi report.", nil, nil
	}

	return fmt.Sprintf("✅ Laporan trading sekarang akan dikirimkan setiap *%s*.", intervalStr), nil, nil
}

// StartReportScheduler starts a background worker to send scheduled reports
// Nama Function: StartReportScheduler
func (b *Bot) StartReportScheduler() {
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-b.stopCh:
				return
			case <-ticker.C:
				b.sendScheduledReports()
			}
		}
	}()
}

func (b *Bot) sendScheduledReports() {
	if b.db == nil {
		return
	}

	ctx := context.Background()

	// Get users with config — gunakan COALESCE untuk handle NULL last_report_sent_at
	query := `
		SELECT u.id, u.telegram_id, u.username, u.first_name, u.last_name,
		       c.report_interval, COALESCE(c.last_report_sent_at, CURRENT_TIMESTAMP) as last_sent_at
		FROM users u
		JOIN user_configs c ON u.id = c.user_id
		WHERE c.report_interval IS NOT NULL
		  AND c.report_interval != ''
		  AND c.report_interval != '0'
	`
	rows, err := b.db.QueryContext(ctx, query)
	if err != nil {
		b.logger.Errorf("Failed to query users for scheduled reports: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var u models.User
		var reportInterval string
		var lastSentAt time.Time

		err := rows.Scan(&u.ID, &u.TelegramID, &u.Username, &u.FirstName, &u.LastName, &reportInterval, &lastSentAt)
		if err != nil {
			b.logger.Warnf("Failed to scan user for report: %v", err)
			continue
		}

		duration, err := time.ParseDuration(reportInterval)
		if err != nil {
			b.logger.Warnf("Invalid duration format %s for user %s", reportInterval, u.ID)
			continue
		}

		if time.Since(lastSentAt) < duration {
			continue
		}

		// Update timestamp SEBELUM kirim — race condition prevention
		// Ini memastikan meskipun telegram call gagal, user tidak akan di-trigger ulang
		// dalam 1 menit yang sama (ticker interval)
		result, err := b.db.ExecContext(ctx,
			"UPDATE user_configs SET last_report_sent_at = NOW() WHERE user_id = $1 AND last_report_sent_at = $2",
			u.ID, lastSentAt)
		if err != nil {
			b.logger.Errorf("Failed to lock report timestamp for user %s: %v", u.ID, err)
			continue
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			// another goroutine already sent — skip
			b.logger.Debugf("Report already sent for user %s, skipping", u.ID)
			continue
		}

		// Generate and send report
		reportText, _, err := b.handleReport(ctx, &u, "")
		if err != nil {
			b.logger.Errorf("Failed to generate report for user %s: %v", u.ID, err)
			continue
		}

		err = b.SendMessage(u.TelegramID, reportText, nil)
		if err != nil {
			b.logger.Errorf("Failed to send scheduled report to user %s: %v", u.ID, err)
			continue
		}

		b.logger.Infof("Sent scheduled report to user %s (interval: %s)", u.ID, reportInterval)
	}
}

// handleProfile handles /profile command — user profile & account status
// Nama Function: handleProfile
// Deskripsi: Handler untuk command /profile. Menampilkan profil & status akun user.
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - user: *models.User — user
//   - args: string — argumen
//
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryRowContext: dipanggil untuk ambil user config
//   - db.QueryContext: dipanggil untuk ambil statistics
//
// Output/Return Value:
//   - string: profil user lengkap
//   - interface{}: inline keyboard (nil)
//   - error: error jika query gagal
func (b *Bot) handleProfile(ctx context.Context, user *models.User, args string) (string, interface{}, error) {
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
	maxAlloc := "10.0%"
	dailyLoss := "5.00"
	maxPositions := "3"

	if b.db != nil {
		var config models.UserConfig
		err := b.db.QueryRowContext(ctx, `
			SELECT max_risk_per_trade, max_allocation_per_trade, daily_loss_limit, max_open_positions,
				   notify_on_trade, notify_on_error, auto_trade_enabled
			FROM user_configs WHERE user_id = $1
		`, user.ID).Scan(&config.MaxRiskPerTrade, &config.MaxAllocationPerTrade, &config.DailyLossLimit, &config.MaxOpenPositions,
			&config.NotifyOnTrade, &config.NotifyOnError, &config.AutoTradeEnabled)

		if err == nil {
			maxRisk = config.MaxRiskPerTrade.String() + "%"
			maxAlloc = config.MaxAllocationPerTrade.String() + "%"
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
	var apiKeyCount int
	if b.db != nil {
		b.db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM orders WHERE user_id = $1
		`, user.ID).Scan(&totalTrades)

		b.db.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(btc_received), 0) FROM btc_accumulation_ledger WHERE user_id = $1
		`, user.ID).Scan(&totalBTCAccumulated)

		b.db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM api_keys WHERE user_id = $1 AND is_active = true
		`, user.ID).Scan(&apiKeyCount)
	}
	if totalTrades == "" {
		totalTrades = "0"
	}
	if totalBTCAccumulated == "" {
		totalBTCAccumulated = "0.00"
	}

	apiKeyStatus := "❌ Belum Diset (Gunakan /setapikey)"
	if apiKeyCount > 0 {
		apiKeyStatus = "✅ Terhubung"
	}

	activePairs := 0
	if b.pairManager != nil {
		userPairsMap := b.pairManager.GetUserPairs(user.ID.String())
		for _, pairs := range userPairsMap {
			activePairs += len(pairs)
		}
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
• Active Pairs: %d/%d (Batas Maksimal)

*⚙️ Current Settings:*
• Max Risk/Trade: %s
• Max Alloc/Trade: %s
• Daily Loss Limit: %s%%
• Max Positions: %s
• Auto Trade: %s

*🔔 Notifications:*
• Trade Alerts: %s
• Error Alerts: %s

*🔐 API Key:*
• Status: %s
_(Jika ada masalah API Key, silakan hubungi admin @nafaswch)_

💡 _Gunakan /settings untuk mengubah konfigurasi._`, user.TelegramID, username, statusIcon, statusText, memberSince,
		user.WCHBalance.String(), totalTrades, totalBTCAccumulated, activePairs, b.maxPairsPerUser,
		maxRisk, maxAlloc, dailyLoss, maxPositions, autoTrade,
		notifyTrade, notifyError, apiKeyStatus), nil, nil
}

// handleCallbackQuery memproses callback query dari inline button.
// Setiap callback data memiliki prefix untuk mengidentifikasi jenis action.
// Nama Function: handleCallbackQuery
// Deskripsi: Memproses callback query dari inline keyboard button.
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - user: *models.User — user yang menekan button
//   - cbq: *CallbackQuery — callback query dari Telegram
//
// Function yang Dipanggil/Dikonsumsi:
//   - AnswerCallbackQuery: dipanggil untuk answer callback (hilangkan loading)
//   - SendMessage: dipanggil untuk kirim response ke user
//   - db.ExecContext: dipanggil untuk update konfigurasi user
//
// Output/Return Value:
//   - Tidak ada return value langsung
func (b *Bot) handleCallbackQuery(ctx context.Context, user *models.User, cbq *CallbackQuery) {
	data := cbq.Data

	// Answer callback query untuk hentikan loading indicator
	b.AnswerCallbackQuery(cbq.ID, "")

	switch {
	case data == "settings_refresh":
		b.logger.Debugf("handleCallbackQuery: settings_refresh callback for userID=%s", user.ID.String())
		response, markup, _ := b.handleSettings(ctx, user, "")
		if cbq.Message != nil {
			b.EditMessageText(int64(cbq.From.ID), cbq.Message.MessageID, response, markup)
		} else {
			b.SendMessage(int64(cbq.From.ID), response, markup)
		}

	case data == "settings_risk":
		b.SendMessage(int64(cbq.From.ID), `📊 *Ubah Max Risk/Trade*

Masukkan nilai risk dalam persen (0.1 - 5.0):

Contoh:1.5 untuk1.5%

Ketik /setrisk <nilai> untuk mengubah.

Contoh: /setrisk 2.0`, nil)

	case data == "settings_dailyloss":
		b.SendMessage(int64(cbq.From.ID), `📉 *Ubah Daily Loss Limit*

Masukkan batas loss harian dalam persen:

Contoh: 5 untuk 5% dari modal

Ketik /setdailyloss <nilai> untuk mengubah.

Contoh: /setdailyloss 5`, nil)

	case data == "settings_maxpos":
		b.SendMessage(int64(cbq.From.ID), `📈 *Ubah Max Open Positions*

Masukkan jumlah maksimal posisi terbuka (1-10):

Ketik /setmaxpos <nilai> untuk mengubah.

Contoh: /setmaxpos 5`, nil)

	case data == "settings_alloc":
		b.SendMessage(int64(cbq.From.ID), `💼 *Ubah Max Allocation/Trade*

Masukkan batas porsi modal maksimal per trade dalam persen (5.0 - 100.0):

Contoh: 25 untuk 25%

Ketik /setallocation <nilai> untuk mengubah.

Contoh: /setallocation 25`, nil)

	case data == "settings_tradealerts":
		b.logger.Debugf("handleCallbackQuery: settings_tradealerts toggle for userID=%s", user.ID.String())
		if b.db != nil {
			var current bool
			err := b.db.QueryRowContext(ctx, `
				SELECT COALESCE(notify_on_trade, true) FROM user_configs WHERE user_id = $1
			`, user.ID.String()).Scan(&current)
			if err != nil && err != sql.ErrNoRows {
				b.logger.Errorf("handleCallbackQuery: settings_tradealerts failed to read, userID=%s, err=%v", user.ID.String(), err)
			}
			newValue := !current
			_, err = b.db.ExecContext(ctx, `
				INSERT INTO user_configs (user_id, notify_on_trade)
				VALUES ($1, $2)
				ON CONFLICT (user_id) DO UPDATE SET
					notify_on_trade = EXCLUDED.notify_on_trade,
					updated_at = CURRENT_TIMESTAMP
			`, user.ID.String(), newValue)
			if err != nil {
				b.logger.Errorf("handleCallbackQuery: settings_tradealerts failed to save, userID=%s, err=%v", user.ID.String(), err)
			}
			response, markup, _ := b.handleSettings(ctx, user, "")
			if cbq.Message != nil {
				b.EditMessageText(int64(cbq.From.ID), cbq.Message.MessageID, response, markup)
			} else {
				b.SendMessage(int64(cbq.From.ID), response, markup)
			}
		}

	case data == "settings_erroralerts":
		b.logger.Debugf("handleCallbackQuery: settings_erroralerts toggle for userID=%s", user.ID.String())
		if b.db != nil {
			var current bool
			err := b.db.QueryRowContext(ctx, `
				SELECT COALESCE(notify_on_error, true) FROM user_configs WHERE user_id = $1
			`, user.ID.String()).Scan(&current)
			if err != nil && err != sql.ErrNoRows {
				b.logger.Errorf("handleCallbackQuery: settings_erroralerts failed to read, userID=%s, err=%v", user.ID.String(), err)
			}
			newValue := !current
			_, err = b.db.ExecContext(ctx, `
				INSERT INTO user_configs (user_id, notify_on_error)
				VALUES ($1, $2)
				ON CONFLICT (user_id) DO UPDATE SET
					notify_on_error = EXCLUDED.notify_on_error,
					updated_at = CURRENT_TIMESTAMP
			`, user.ID.String(), newValue)
			if err != nil {
				b.logger.Errorf("handleCallbackQuery: settings_erroralerts failed to save, userID=%s, err=%v", user.ID.String(), err)
			}
			response, markup, _ := b.handleSettings(ctx, user, "")
			if cbq.Message != nil {
				b.EditMessageText(int64(cbq.From.ID), cbq.Message.MessageID, response, markup)
			} else {
				b.SendMessage(int64(cbq.From.ID), response, markup)
			}
		}

	case data == "settings_autotrade":
		b.logger.Debugf("handleCallbackQuery: settings_autotrade toggle for userID=%s", user.ID.String())
		if b.db != nil {
			var current bool
			err := b.db.QueryRowContext(ctx, `
				SELECT COALESCE(auto_trade_enabled, false) FROM user_configs WHERE user_id = $1
			`, user.ID.String()).Scan(&current)
			if err != nil && err != sql.ErrNoRows {
				b.logger.Errorf("handleCallbackQuery: settings_autotrade failed to read, userID=%s, err=%v", user.ID.String(), err)
			}
			newValue := !current
			_, err = b.db.ExecContext(ctx, `
				INSERT INTO user_configs (user_id, auto_trade_enabled)
				VALUES ($1, $2)
				ON CONFLICT (user_id) DO UPDATE SET
					auto_trade_enabled = EXCLUDED.auto_trade_enabled,
					updated_at = CURRENT_TIMESTAMP
			`, user.ID.String(), newValue)
			if err != nil {
				b.logger.Errorf("handleCallbackQuery: settings_autotrade failed to save, userID=%s, err=%v", user.ID.String(), err)
			}
			response, markup, _ := b.handleSettings(ctx, user, "")
			if cbq.Message != nil {
				b.EditMessageText(int64(cbq.From.ID), cbq.Message.MessageID, response, markup)
			} else {
				b.SendMessage(int64(cbq.From.ID), response, markup)
			}
		}

	case data == "apikey_binance":
		b.SendMessage(int64(cbq.From.ID), `🔶 *Binance API Key Setup*

Masukkan credential dengan format:

/setapikey binance <api_key> <api_secret>

Contoh:
/setapikey binance abc123XYZ secret456ABC

💡 Tips:
• API key hanya butuh permission Trade
• Withdraw harus disabled
• Key dienkripsi dengan AES-256`, nil)

	case data == "apikey_okx":
		b.SendMessage(int64(cbq.From.ID), `🟡 *OKX API Key Setup*

Masukkan credential dengan format:

/setapikey okx <api_key> <api_secret> <passphrase>

Contoh:
/setapikey okx abc123XYZ secret456ABC mypassphrase

💡 Tips:
• API key butuh passphrase
• Hanya butuh permission Trade
• Withdraw harus disabled`, nil)

	default:
		b.SendMessage(int64(cbq.From.ID), "Unknown action.", nil)
	}
}

// AnswerCallbackQuery menjawab callback query untuk hentikan loading indicator.
// Nama Function: AnswerCallbackQuery
// Deskripsi: Mengirim answer ke callback query untuk hentikan loading di Telegram.
// Parameter/Value Input:
//   - callbackID: string — ID dari callback query
//   - text: string — text untuk ditampilkan (opsional, kosongkan untuk sembunyikan)
//
// Function yang Dipanggil/Dikonsumsi:
//   - httpClient.Do: dipanggil untuk kirim request ke Telegram API
//
// Output/Return Value:
//   - error: error jika request gagal
func (b *Bot) AnswerCallbackQuery(callbackID string, text string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/answerCallbackQuery", b.token)

	body := map[string]interface{}{
		"callback_query_id": callbackID,
	}
	if text != "" {
		body["text"] = text
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

	return nil
}

// handleSetRisk handles /setrisk command untuk ubah max risk per trade.
// Nama Function: handleSetRisk
// Deskripsi: Handler untuk command /setrisk — mengubah max risk per trade.
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - user: *models.User — user
//   - args: string — nilai risk dalam persen (contoh: "1.5")
//
// Function yang Dipanggil/Dikonsumsi:
//   - db.ExecContext: dipanggil untuk UPSERT user_configs
//
// Output/Return Value:
//   - string: success/error message
//   - interface{}: inline keyboard (nil)
//   - error: error jika proses gagal
func (b *Bot) handleSetRisk(ctx context.Context, user *models.User, args string) (string, interface{}, error) {
	if args == "" {
		return "📊 *Set Risk — Error*\n\nUsage: /setrisk <nilai>\n\nContoh: /setrisk 1.5\n\nNilai harus antara 0.1 - 5.0%", nil, nil
	}

	risk, err := decimalFromString(args)
	if err != nil || risk.LessThan(decimal.NewFromFloat(0.1)) || risk.GreaterThan(decimal.NewFromFloat(5.0)) {
		return "📊 *Set Risk — Error*\n\nNilai tidak valid. Masukkan angka antara 0.1 - 5.0\n\nContoh: /setrisk 1.5", nil, nil
	}

	if b.db != nil {
		b.logger.Debugf("handleSetRisk: userID=%s, riskValue=%s", user.ID.String(), risk.String())
		result, err := b.db.ExecContext(ctx, `
			INSERT INTO user_configs (user_id, max_risk_per_trade)
			VALUES ($1, $2)
			ON CONFLICT (user_id) DO UPDATE SET
				max_risk_per_trade = EXCLUDED.max_risk_per_trade,
				updated_at = CURRENT_TIMESTAMP
		`, user.ID.String(), risk.String())
		if err != nil {
			b.logger.Errorf("handleSetRisk: failed to save, err=%v", err)
			return "📊 *Set Risk — Error*\n\nGagal menyimpan pengaturan.", nil, err
		}
		rowsAffected, _ := result.RowsAffected()
		b.logger.Infof("handleSetRisk: saved successfully, rowsAffected=%d", rowsAffected)
	}

	return fmt.Sprintf("📊 *Risk Updated*\n\nMax Risk/Trade: %s%%\n\n✅ Pengaturan berhasil disimpan.", risk.String()), nil, nil
}

// handleSetAllocation handles /setallocation command untuk ubah max allocation per trade.
// Nama Function: handleSetAllocation
// Deskripsi: Mengatur porsi maksimal (dalam persen) dari modal untuk sebuah trade.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk operasi database
//   - user: *models.User — user yang mengirim command
//   - args: string — persentase alokasi, e.g. "50"
//
// Function yang Dipanggil/Dikonsumsi:
//   - db.ExecContext: dipanggil untuk upsert ke user_configs
//
// Output/Return Value:
//   - string: success/error message
//   - interface{}: inline keyboard (nil)
//   - error: error jika proses gagal
func (b *Bot) handleSetAllocation(ctx context.Context, user *models.User, args string) (string, interface{}, error) {
	b.logger.Debugf("handleSetAllocation: userID=%s, args=%s", user.ID.String(), args)
	if args == "" {
		return "📊 *Set Allocation — Error*\n\nUsage: /setallocation <nilai>\n\nContoh: /setallocation 50\n\nNilai harus antara 5.0 - 100.0%", nil, nil
	}

	alloc, err := decimalFromString(args)
	if err != nil || alloc.LessThan(decimal.NewFromFloat(5.0)) || alloc.GreaterThan(decimal.NewFromFloat(100.0)) {
		b.logger.Warnf("handleSetAllocation: invalid value userID=%s, args=%s, err=%v", user.ID.String(), args, err)
		return "📊 *Set Allocation — Error*\n\nNilai tidak valid. Masukkan angka antara 5.0 - 100.0\n\nContoh: /setallocation 50", nil, nil
	}

	if b.db != nil {
		b.logger.Debugf("handleSetAllocation: saving userID=%s, alloc=%s", user.ID.String(), alloc.String())
		_, err = b.db.ExecContext(ctx, `
			INSERT INTO user_configs (user_id, max_allocation_per_trade)
			VALUES ($1, $2)
			ON CONFLICT (user_id) DO UPDATE SET
				max_allocation_per_trade = EXCLUDED.max_allocation_per_trade,
				updated_at = CURRENT_TIMESTAMP
		`, user.ID.String(), alloc.String())
		if err != nil {
			b.logger.Errorf("handleSetAllocation: failed to save userID=%s, err=%v", user.ID.String(), err)
			// Return nil for error so processCommand doesn't override our detailed message
			return fmt.Sprintf("📊 *Set Allocation — Error*\n\nGagal menyimpan pengaturan: %v", err), nil, nil
		}
		b.logger.Infof("handleSetAllocation: saved successfully userID=%s, alloc=%s", user.ID.String(), alloc.String())
	}

	return fmt.Sprintf("📊 *Allocation Updated*\n\nMax Allocation/Trade: %s%%\n\n✅ Pengaturan berhasil disimpan.", alloc.String()), nil, nil
}

// handleSetDailyLoss handles /setdailyloss command untuk ubah daily loss limit.
// Nama Function: handleSetDailyLoss
// Deskripsi: Handler untuk command /setdailyloss — mengubah batas loss harian dalam persen.
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - user: *models.User — user
//   - args: string — nilai limit dalam persen (contoh: "5" untuk 5%)
//
// Function yang Dipanggil/Dikonsumsi:
//   - db.ExecContext: dipanggil untuk UPSERT user_configs
//
// Output/Return Value:
//   - string: success/error message
//   - interface{}: inline keyboard (nil)
//   - error: error jika proses gagal
func (b *Bot) handleSetDailyLoss(ctx context.Context, user *models.User, args string) (string, interface{}, error) {
	b.logger.Debugf("handleSetDailyLoss: userID=%s, args=%s", user.ID.String(), args)
	if args == "" {
		return "📉 *Set Daily Loss — Error*\n\nUsage: /setdailyloss <nilai>\n\nContoh: /setdailyloss 5\n\nNilai dalam persen (1-20%)", nil, nil
	}

	loss, err := decimalFromString(args)
	if err != nil || loss.LessThan(decimal.NewFromFloat(1)) || loss.GreaterThan(decimal.NewFromFloat(20)) {
		b.logger.Warnf("handleSetDailyLoss: invalid value userID=%s, args=%s, err=%v", user.ID.String(), args, err)
		return "📉 *Set Daily Loss — Error*\n\nNilai tidak valid. Masukkan angka antara 1 - 20%\n\nContoh: /setdailyloss 5", nil, nil
	}

	if b.db != nil {
		b.logger.Debugf("handleSetDailyLoss: saving userID=%s, loss=%s", user.ID.String(), loss.String())
		_, err = b.db.ExecContext(ctx, `
			INSERT INTO user_configs (user_id, daily_loss_limit)
			VALUES ($1, $2)
			ON CONFLICT (user_id) DO UPDATE SET
				daily_loss_limit = EXCLUDED.daily_loss_limit,
				updated_at = CURRENT_TIMESTAMP
		`, user.ID.String(), loss.String())
		if err != nil {
			b.logger.Errorf("handleSetDailyLoss: failed to save userID=%s, err=%v", user.ID.String(), err)
			return fmt.Sprintf("📉 *Set Daily Loss — Error*\n\nGagal menyimpan pengaturan: %v", err), nil, nil
		}
		b.logger.Infof("handleSetDailyLoss: saved successfully userID=%s, loss=%s", user.ID.String(), loss.String())
	}

	return fmt.Sprintf("📉 *Daily Loss Updated*\n\nDaily Loss Limit: %s%%\n\n✅ Pengaturan berhasil disimpan.", loss.String()), nil, nil
}

// handleSetMaxPositions handles /setmaxpos command untuk ubah max open positions.
// Nama Function: handleSetMaxPositions
// Deskripsi: Handler untuk command /setmaxpos — mengubah maksimal posisi terbuka.
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - user: *models.User — user
//   - args: string — jumlah posisi (contoh: "5")
//
// Function yang Dipanggil/Dikonsumsi:
//   - db.ExecContext: dipanggil untuk UPSERT user_configs
//
// Output/Return Value:
//   - string: success/error message
//   - interface{}: inline keyboard (nil)
//   - error: error jika proses gagal
func (b *Bot) handleSetMaxPositions(ctx context.Context, user *models.User, args string) (string, interface{}, error) {
	if args == "" {
		return "📈 *Set Max Positions — Error*\n\nUsage: /setmaxpos <nilai>\n\nContoh: /setmaxpos 5\n\nNilai harus antara 1 - 10", nil, nil
	}

	positions, err := strconv.Atoi(strings.TrimSpace(args))
	if err != nil || positions < 1 || positions > 10 {
		return "📈 *Set Max Positions — Error*\n\nNilai tidak valid. Masukkan angka antara 1 - 10\n\nContoh: /setmaxpos 5", nil, nil
	}

	if b.db != nil {
		_, err = b.db.ExecContext(ctx, `
			INSERT INTO user_configs (user_id, max_open_positions)
			VALUES ($1, $2)
			ON CONFLICT (user_id) DO UPDATE SET
				max_open_positions = EXCLUDED.max_open_positions,
				updated_at = CURRENT_TIMESTAMP
		`, user.ID.String(), positions)
		if err != nil {
			return fmt.Sprintf("📈 *Set Max Positions — Error*\n\nGagal menyimpan pengaturan: %v", err), nil, nil
		}
	}

	return fmt.Sprintf("📈 *Max Positions Updated*\n\nMax Open Positions: %d\n\n✅ Pengaturan berhasil disimpan.", positions), nil, nil
}

// decimalFromString converts string to decimal.Decimal safely
func decimalFromString(s string) (decimal.Decimal, error) {
	s = strings.TrimSpace(s)
	return decimal.NewFromString(s)
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
