# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## SYSTEM OVERVIEW

NAFAS (Native Asset Focused Accumulation System) is an AI-assisted crypto asset accumulation system. It is NOT a signal group, copy trading platform, high-frequency trading bot, or gambling system. It IS a long-term asset accumulation engine with AI-orchestrated, risk-first decision architecture and Telegram as the primary UI.

Target assets: BTC (primary), ETH (secondary), SOL (tertiary).

### Core Principles

1. Risk > Opportunity
2. Consistency > Profit spikes
3. Accumulation > Speculation
4. Data > Emotion
5. System > Manual decisions

### Non-Negotiable Rules

- No gambling behavior
- No reckless leverage
- No emotional AI decisions
- No undocumented trading logic
- System must remain predictable, auditable, and deterministic

---

## ARCHITECTURE

### Modular Monolith (Go)

```
internal/
  auth       — Telegram authentication
  user       — User management
  bot        — Bot orchestration
  exchange   — Exchange API wrappers (Binance, OKX)
  scanner    — Market data ingestion (OHLCV, orderbook, ticker)
  strategy   — Strategy definitions and runs
  risk       — Risk Guardian (hardcoded rules, NOT AI)
  execution  — Order execution (Market/Limit/TWAP)
  ai         — TradingAgents client (drop-in replacement untuk old Coordinator)
  learning   — Learning Engine, trade/market/strategy memories
  telegram   — Telegram bot commands and handlers
```

**DO NOT start with microservices.** Choose simplicity, modular monolith, and observable system when unsure.

### AI System: TradingAgents (Primary)

AI backend menggunakan **TradingAgents** (Python/LangGraph) sebagai drop-in replacement untuk direct LLM calls. File `.md` agent prompts sudah deprecated.

| Component | File | Fungsi |
|-----------|------|--------|
| TradingAgents Service | `services/trading_engine/` | Python FastAPI - multi-agent trading analysis |
| Go Client | `internal/ai/tradingagents.go` | Client untuk TradingAgents API |
| Decide() | `tradingagents.go` | Drop-in replacement untuk Coordinator.Decide() |

**TradingAgents Flow:**
```
NAFAS (Go) → TradingAgentsClient → TradingAgents API → Multi-Agent Analysis (LangGraph)
                                                                    ↓
                                                              Trading Decision
                                                                    ↓
NAFAS Executor ← Risk Guardian ← CoordinatorDecision format
```

**Risk Guardian is the FINAL AUTHORITY.** It is hardcoded Go logic (not an AI prompt) that enforces max exposure, stop-loss requirements, and position sizing. AI prompts cannot override it.

### Trading Pipeline (TradingAgents)

```
Market Data → Scanner → TradingAgentsClient.Decide() → Risk Guardian → Execution → Learning
```

TradingAgents orchestrates: Market analysis → Trading decision → Risk validation → Order execution. CoordinatorDecision format ensures compatibility with existing orchestrator.

```
Market Data → Scanner → Pair Ranking → AI Review → Risk Engine → Execution → Learning
```

The AI Coordinator orchestrates the flow: Market Analyst detects regime → Pair Analyst ranks pairs → AI Coordinator produces structured decision → Risk Guardian approves/rejects/scales → Execution Advisor defines order parameters → Trade Reviewer validates → Execution module sends orders.

### WCH Token

WCH (Wrapped Crypto Holder) balance affects execution priority, AI depth level, risk tolerance boundaries, and feature accessibility. It does NOT override risk constraints.

---

## FUNCTION DOCUMENTATION STANDARD (WAJIB)

Setiap membuat atau mengupdate function di project ini, **WAJIB** menyertakan dokumentasi dalam Bahasa Indonesia dengan format berikut. Diprioritaskan untuk `internal/` (FE/Frontend) dan service layer yang punya API contract.

### Format Dokumentasi Function

```go
// Nama Function: <nama_function>
// Deskripsi: <penjelasan apa yang dilakukan function ini dalam Bahasa Indonesia>
// Parameter/Value Input:
//   - <nama_param>: <tipe> — <penjelasan dalam Bahasa Indonesia>
//   - ...
// Function yang Dipanggil/Dikonsumsi:
//   - <nama_func>: <penjelasan kapan dipanggil dan untuk apa>
//   - ...
// Output/Return Value:
//   - <tipe>: <penjelasan dalam Bahasa Indonesia>
//   - ...
// Catatan: <catatan penting jika ada>
func NamaFunction(param1 string, param2 int) (ResultType, error) {
    // ...
}
```

### Contoh

```go
// Nama Function: GetUserByTelegramID
// Deskripsi: Mengambil data user berdasarkan telegram_id dari database.
// Parameter/Value Input:
//   - telegramID: int64 — ID telegram unik user yang ingin diambil datanya
//   - db: *sql.DB — koneksi database PostgreSQL
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryRow — dipanggil untuk execute SELECT query ke tabel users
// Output/Return Value:
//   - *User: pointer ke struct User jika ditemukan, nil jika tidak ada
//   - error: error jika query gagal atau koneksi terputus
// Catatan: Mengembalikan error sql.ErrNoRows jika user tidak ditemukan
func GetUserByTelegramID(telegramID int64, db *sql.DB) (*User, error) {
    // ...
}
```

### Aturan Konsistensi

- Untuk setiap **function baru**, langsung tulis dokumentasi di atas function
- Ketika function diupdate (parameter, logic, output berubah), dokumentasi **WAJIB diupdate** di baris yang sama (atas function) — agar FE bisa langsung baca alur terbaru tanpa perlu trace kode
- Untuk interface/API contract: dokumentasi HARUS menjelaskan semua parameter, value yang diterima, function yang dipanggil, dan return value-nya
- Ini membantu tim FE memahami alur data dan integrasi tanpa perlu baca baris per baris kode

---

## CONCURRENCY (GOROUTINE)

Kodebase saat ini: **ZERO goroutine**. Semua operasi blocking& sequential. Ini perlu dirubah secara bertahap.

### Kapan WAJIB Pakai Goroutine

| Kasus | Contoh | Pattern |
|-------|--------|---------|
| I/O binding parallel | Fetch OHLCV + ticker untuk banyak symbol | `go func() { ... }()` + `sync.WaitGroup` |
| Multiple API call tanpa dependensi | Call ke Binance + OKX + LLM secara bersamaan | Goroutine per call, barrier dengan `WaitGroup` |
| Background worker | Periodic market scan, order monitoring | `go worker(ctx, jobs)` + buffered `chan` |
| Fan-out / Fan-in | Satu event, banyak subscriber | Channel broadcast |
| Non-blocking notification | Kirim alert Telegram tanpa block trading flow | `go sendAlert()` |

### Pattern yang WAJIB Dipakai

**1. Worker Pool dengan Semaphore (untuk HTTP calls)**

```go
// Batas concurrent HTTP calls — avoid overwhelming exchange APIs
sem := make(chan struct{}, maxConcurrent) // e.g. 10
var wg sync.WaitGroup

for _, symbol := range symbols {
    for _, interval := range intervals {
        wg.Add(1)
        go func(sym, intv string) {
            defer wg.Done()
            sem <- struct{}{}         // acquire
            defer func() { <-sem }()  // release

            data, err := s.FetchCandles(ctx, sym, intv)
            // ...
        }(symbol, interval)
    }
}
wg.Wait()
```

**2. Barrier (semua hasil dibutuhkan sebelum lanjut)**

```go
var wg sync.WaitGroup
results := make([]Result, len(items))

for i, item := range items {
    wg.Add(1)
    go func(idx int, it Item) {
        defer wg.Done()
        results[idx] = process(it)
    }(i, item)
}
wg.Wait() // barrier — lanjut setelah semua selesai
```

**3. Channel untuk shutdown signal**

```go
// Di struct Bot/Scanner:
type Scanner struct {
    stopCh chan struct{}
    jobsCh chan ScanJob
}

// Start worker
func (s *Scanner) Start(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        case <-s.stopCh:
            return
        case job := <-s.jobsCh:
            s.process(job)
        }
    }
}
```

**4. sync.Once untuk lazy initialization (WAJIB — race condition safe)**

```go
import "sync"

var (
    once       sync.Once
    globalLogger *Logger
)

func Default() *Logger {
    once.Do(func() {
        globalLogger = New("info", "app", os.Stdout)
    })
    return globalLogger
}
```

### Anti-Pattern yang HARUS Dihindari

| Anti-pattern | Masalah | Solusi |
|-------------|---------|--------|
| Goroutine tanpa `sync.WaitGroup` atau `chan` | Goroutine leak — tidak bisa di-track selesai atau tidak | Selalu gunakan `WaitGroup` atau `chan` |
| `for range ch` tanpa `close(ch)` | Goroutine leak — channel tidak pernah ditutup | Tutup channel setelah semua producer selesai |
| `append` ke slice dari multiple goroutine tanpa lock | Race condition | Gunakan `sync.Mutex` atau `sync.RWMutex` |
| `globalLogger = New(...)` tanpa `sync.Once` | Race condition di `Default()` | Gunakan `sync.Once` |
| `defer wg.Done()` sebelum `wg.Add(1)` | Jika `wg.Add` panic, `Done` tidak dipanggil | `wg.Add(1)` HARUS sebelum goroutine spawned |
| Context tanpa timeout di goroutine | Goroutine leak saat request stuck | Selalu gunakan `context.WithTimeout` |

### Lokasi yang Perlu Dirubah (Priority Order)

1. **`internal/logger/logger.go:238-243`** — Race condition di `Default()` → gunakan `sync.Once` *(SUDAH DIFIX)*
2. **`internal/scanner/scanner.go:136-164`** — Sequential symbol × interval loop → worker pool dengan semaphore *(SUDAH DIFIX)*
3. **`internal/telegram/telegram.go`** — Queue system untuk handle incoming Telegram updates *(SUDAH DIFIX)*
4. **`internal/ai/tradingagents.go`** — TradingAgents client (drop-in replacement) *(SUDAH DIFIX)*
5. **`internal/execution/execution.go:232-303`** — TWAP slice execution → async order status check
6. **`internal/learning/learning.go:246-305`** — Sequential DB queries → concurrent query dengan `WaitGroup`

### Aturan Penting

- `context.Context` HARUSditurunkan ke semua goroutine — untuk cancellation dan timeout
- Max concurrent HTTP calls: **10** (untuk menghindari rate limit exchange)
- Logger: aman untuk concurrent write (sudah thread-safe dengan `sync.Mutex` internal)
- Database connection: **NOT thread-safe** — gunakan connection pool dari `database/sql`, bukan manual goroutine access ke satu connection

---

## TELEGRAM QUEUE SYSTEM

Sistem antrian Telegram menggunakan worker pool pattern untuk memproses incoming updates secara asynchronous. Ini mencegah blocking HTTP handler dan menghindari Telegram retry storm saat command lambat (misal: exchange API call).

### Arsitektur

```
Telegram Webhook → HandleUpdate() → jobQueue (buffered channel) → Worker Pool (5 workers) → processUpdate()
```

### Konfigurasi

```go
const (
    QueueSize   = 100  // max pending updates di queue
    WorkerCount = 5     // jumlah workers concurrent
)
```

### Flow

1. **HandleUpdate** — Enqueue update ke `jobQueue` (buffered channel capacity 100). Jika queue penuh, update di-drop dan return error. Telegram akan retry.
2. **Worker goroutines** — 5 goroutines masing-masing loop `for` + `select` membaca dari `jobQueue`. Menerima job → proses → next.
3. **Graceful shutdown** — `Stop()` menutup `stopCh`, semua workers drain remaining jobs dari queue sebelum exit via `wg.Wait()`.

### Field Bot yang Ditambahkan

```go
type Bot struct {
    // ... existing fields ...
    jobQueue chan Update   // buffered channel untuk queued updates
    stopCh   chan struct{} // shutdown signal
    wg       sync.WaitGroup
}
```

### Metode Baru

| Method | Fungsi |
|--------|--------|
| `Start()` | Spawn 5 worker goroutines, set `wg.Add(5)` |
| `Stop()` | Tutup `stopCh`, `wg.Wait()` sampai semua workers selesai |
| `worker(id)` | Loop `select` — handle `stopCh` atau `jobQueue`, panggil `processUpdate()` |
| `processUpdate()` | Internal method — authenticate, execute handler, send response |

### Graceful Shutdown (main.go)

```go
// Start workers
telegramBot.Start()

// Shutdown — stop workers before server exit
if telegramBot != nil {
    telegramBot.Stop()
}
```

### Catatan Penting

- **Tidak ada backpressure ke Telegram** — jika queue penuh, update di-drop. Telegram retry mechanism menangani ini. Ini desain deliberate agar HTTP handler tetap responsif.
- **Workers async dari HTTP request** — `HandleUpdate` hanya enqueues, tidak blocking. Worker pool memproses independent dari request lifecycle.
- **Worker count = 5** — dipilih agar cukup untuk concurrency command, tidak terlalu banyak untuk exchange API rate limit.
- **Queue capacity = 100** —足够 untuk burst traffic tanpa memory blowup.

---

## PAIR MANAGER (DYNAMIC SCANNING)

Sistem NAFAS mendukung penambahan dan penghapusan pair secara dinamis per-user dan per-exchange menggunakan komponen `PairManager` yang terhubung ke `Scanner`.

### Arsitektur
`PairManager` bertugas menyimpan relasi:
- `userPairs`: Daftar pair yang di-track oleh `userID` pada `exchange` tertentu.
- `masterPairs`: Reference count (jumlah user) yang memantau sebuah pair pada suatu `exchange`.
- `scanners`: Map instance `*Scanner` untuk setiap exchange (e.g. "Binance", "OKX").

### Alur Kerja
1. User memanggil command `/addpair <exchange> <symbol>` di Telegram.
2. `PairManager` menambahkan pair tersebut ke `userPairs` milik user.
3. `PairManager` menaikkan reference count di `masterPairs`.
4. Jika reference count berubah dari `0` ke `1`, `PairManager` meneruskannya ke `scanner.AddSymbol(symbol)` untuk mulai menarik data market (OHLCV/Ticker).
5. Jika user memanggil `/removepair <exchange> <symbol>`, reference count turun. Jika mencapai `0`, pair dihapus dari master scanner.

**Fitur utama:** Thread-safe (menggunakan `sync.RWMutex`), isolasi per-exchange, dan mencegah duplicate scanner jobs.

---

## COMMANDS

```bash
# Build
go build ./...

# Run tests
go test ./...

# Run a single test
go test ./internal/risk/... -v

# Lint (requires golangci-lint)
golangci-lint run

# Docker Compose (Redis only — PostgreSQL is external)
docker compose up -d

# Run database migrations (psql)
psql "$DATABASE_URL" -f database/migrations/000004_full_schema.up.sql

# Rollback migration
psql "$DATABASE_URL" -f database/migrations/000004_full_schema.down.sql
```

---

## DATABASE

PostgreSQL is the core database. Migrations are numbered sequentially in `database/migrations/`:

- `000001_init_schema` — users, api_keys, system_logs, wch_transactions
- `000002_ai_memory_schema` — AI strategies, decision logs, memories
- `000003_user_configs_refine` — user_configs column refinement
- `000004_full_schema` — complete schema V1.0 (target)

All trades and decisions must be logged to `system_logs`. AI decisions are stored in `ai_decisions` with full `input_context` and `output_context` JSONB for debugging.

**pgvector** is planned for semantic search over `ai_memory` in a future migration.

---

## SECURITY

- Exchange API keys are encrypted with **AES-256** before storage. The encryption key lives in `ENCRYPTION_KEY` env var (32 bytes), never in code or database.
- No plaintext secrets in logs.
- All actions audited via `system_logs`.
- Database connection should use SSL in production.

---

## ENVIRONMENT

Copy `.env.example` to `.env` and fill in:

```
APP_ENV=development
APP_PORT=8080
LOG_LEVEL=debug

DB_HOST=          # External PostgreSQL host
DB_PORT=5432
DB_USER=
DB_PASSWORD=
DB_NAME=nafas_db

REDIS_HOST=localhost  # Local Docker
REDIS_PORT=6379

TELEGRAM_BOT_TOKEN=
TELEGRAM_WEBHOOK_URL=

ENCRYPTION_KEY= # 32-byte AES key — REQUIRED

LLM_PROVIDER_URL=https://api.openai.com/v1
LLM_API_KEY=
LLM_MODEL=gpt-4-turbo

# TradingAgents (Wajib jika pakai AI)
TRADING_AGENTS_URL=http://localhost:8000
LLM_BASE_URL=http://localhost:11434  # Optional: Ollama/LM Studio
LLM_MODEL=llama3                      # Optional: custom model
```

---

## DEPLOYMENT

Single VPS with Docker Compose:
- **Postgres** + **Redis** — data storage
- **NAFAS Bot** (Go) — API Gateway, Telegram, Execution
- **TradingAgents** (Python) — AI multi-agent analysis (containerized)
- **Ollama** (optional) — Local LLM for reduced API costs

```bash
# Start all services
docker compose up -d

# Check TradingAgents health
curl http://localhost:8000/health
```

---

## FUTURE VISION

Cross-chain trading, DEX integration, AI portfolio rotation engine, agent marketplace, and fully autonomous accumulation system.
