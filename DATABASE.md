# NAFAS BOT DATABASE SCHEMA

**Versi:** 1.0
**Status:** Aktif
**PostgreSQL:** 15+

---

## OVERVIEW

Database ini menyimpan seluruh data yang dibutuhkan NAFAS Bot untuk:
- Autentikasi user via Telegram
- Manajemen API keys dan exchange accounts
- Konfigurasi sistem dan user preferences
- Market data dan signal history
- Strategi dan strategy runs
- Eksekusi order dan trade
- Portfolio dan BTC treasury
- AI providers, prompts, decisions, dan memory
- WCH ecosystem transactions
- System logging dan daily reporting

---

## ENTITY RELATIONSHIP (TEXT)

```
users (1) ────── (N) api_keys
   │
   ├───── (1) user_configs
   │
   ├───── (N) exchange_accounts
   │
   ├───── (N) trading_pairs
   │
   ├───── (N) asset_targets
   │
   ├───── (N) orders
   │
   ├───── (N) asset_inventory
   │
   ├───── (N) btc_treasury_snapshots
   │
   ├───── (N) btc_accumulation_ledger
   │
   ├───── (N) btc_accumulation_metrics
   │
   ├───── (N) wch_transactions
   │
   └───── (N) system_logs
 │
            └───── (N) daily_reports

strategies (1) ────── (N) strategy_runs
                          │
                          └───── (N) ai_decisions

ai_providers (1) ────── (N) ai_decisions
 │
                              └───── (N) ai_feedback

ai_prompt_versions (standalone, versioned by name)

ai_memory (standalone, typed by memory_type)
```

---

## 1. USERS

### `users`

Tabel utama untuk menyimpan data user Telegram.

| Kolom | Tipe | Constraint | Deskripsi |
|-------|------|------------|-----------|
| `id` | UUID | PK, DEFAULT uuid_generate_v4() | Identitas unik user |
| `telegram_id` | BIGINT | UNIQUE, NOT NULL | Telegram user ID |
| `username` | VARCHAR(100) | | Username Telegram (nullable) |
| `first_name` | VARCHAR(100) | | Nama depan Telegram |
| `last_name` | VARCHAR(100) | | Nama belakang Telegram |
| `status` | VARCHAR(20) | DEFAULT 'active' | Status user: active, suspended, banned |
| `wch_balance` | DECIMAL(36,18) | DEFAULT 0 | Saldo WCH token |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | Waktu registrasi |
| `updated_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | Waktu update terakhir |

**Indeks:**
- `idx_users_telegram_id` ON (`telegram_id`)

**Catatan:**
- `telegram_id` adalah pengenal utama untuk autentikasi Telegram Bot
- `status` digunakan untuk banning/suspension user

---

## 2. API & EXCHANGE

### `api_keys`

Menyimpan kredensial API exchange yang terenkripsi.

| Kolom | Tipe | Constraint | Deskripsi |
|-------|------|------------|-----------|
| `id` | UUID | PK | Identitas unik |
| `user_id` | UUID | FK → users(id), ON DELETE CASCADE | Owner dari API key |
| `exchange` | VARCHAR(50) | NOT NULL | Nama exchange: binance, okx |
| `encrypted_api_key` | TEXT | NOT NULL | API Key terenkripsi AES-256 |
| `encrypted_api_secret` | TEXT | NOT NULL | API Secret terenkripsi AES-256 |
| `encrypted_passphrase` | TEXT | | Passphrase (untuk OKX) terenkripsi |
| `is_active` | BOOLEAN | DEFAULT true | Apakah key aktif |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | Waktu input |
| `updated_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | Waktu update |

**Constraint:**
- `UNIQUE(user_id, exchange)` — satu API key per exchange per user

**Indeks:**
- `idx_api_keys_user_id` ON (`user_id`)
- `idx_api_keys_exchange` ON (`exchange`)

**Catatan Keamanan:**
- Semua field `encrypted_*` HARUS dienkripsi AES-256 sebelum disimpan
- Kunci enkripsi disimpan di environment variable, bukan di database
- Tidak ada API key dalam plaintext

---

### `exchange_accounts`

Menyimpan informasi akun exchange (sub-account atau main account).

| Kolom | Tipe | Constraint | Deskripsi |
|-------|------|------------|-----------|
| `id` | UUID | PK | Identitas unik |
| `user_id` | UUID | FK → users(id), ON DELETE CASCADE | Owner |
| `exchange` | VARCHAR(50) | NOT NULL | Nama exchange |
| `account_type` | VARCHAR(50) | DEFAULT 'main' | Tipe akun: main, sub, isolated_margin |
| `is_active` | BOOLEAN | DEFAULT true | Status sinkronisasi |
| `last_sync_at` | TIMESTAMP | | Waktu sinkronisasi terakhir |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |
| `updated_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |

**Indeks:**
- `idx_exchange_accounts_user_id` ON (`user_id`)

---

## 3. CONFIGURATION

### `system_settings`

Konfigurasi sistem global (key-value store).

| Kolom | Tipe | Constraint | Deskripsi |
|-------|------|------------|-----------|
| `key` | VARCHAR | PK | Nama setting |
| `value` | JSONB | NOT NULL | Nilai setting |
| `updated_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |

**Contoh Penggunaan:**
```sql
-- Risk thresholds
INSERT INTO system_settings (key, value) VALUES
('risk_max_position_size', '{"btc": 0.01, "eth": 0.1, "sol": 1.0}'),
('trading_enabled', '{"global": false, "reason": "maintenance"}'),
('ai_model_config', '{"provider": "openai", "model": "gpt-4o", "temperature": 0.3}');
```

**Catatan:**
- Semua konfigurasi sistem harus disimpan di sini, bukan hardcoded
- Perubahan harus logged via system_logs

---

### `user_configs`

Konfigurasi personal user untuk trading dan notifikasi.

| Kolom | Tipe | Constraint | Deskripsi |
|-------|------|------------|-----------|
| `id` | UUID | PK | Identitas unik |
| `user_id` | UUID | FK → users(id), UNIQUE, ON DELETE CASCADE | User owner |
| `max_risk_per_trade` | DECIMAL(10,4) | DEFAULT 1.00 | Max risk per trade (% dari portfolio) |
| `daily_loss_limit` | DECIMAL(18,8) | DEFAULT 5.00 | Batas kerugian harian (absolute amount) |
| `max_open_positions` | INTEGER | DEFAULT 3 | Max posisi terbuka |
| `notify_on_trade` | BOOLEAN | DEFAULT true | Notifikasi saat trade executed |
| `notify_on_error` | BOOLEAN | DEFAULT true | Notifikasi saat error |
| `daily_report_time` | TIME | DEFAULT '00:00:00' | Waktu kirim laporan harian |
| `auto_trade_enabled` | BOOLEAN | DEFAULT false | Auto trading on/off |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |
| `updated_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |

**Trigger:**
- `update_user_configs_modtime` — auto-update `updated_at` pada UPDATE

**Catatan:**
- `max_risk_per_trade` dalam persen (1.00 = 1%)
- `auto_trade_enabled` harus false default untuk safety

---

### `trading_pairs`

Daftar pair trading yang diizinkan per user.

| Kolom | Tipe | Constraint | Deskripsi |
|-------|------|------------|-----------|
| `id` | UUID | PK | Identitas unik |
| `user_id` | UUID | FK → users(id), ON DELETE CASCADE | User owner |
| `symbol` | VARCHAR(20) | NOT NULL | Symbol pair: SOLBTC, ETHBTC |
| `base_asset` | VARCHAR(20) | NOT NULL | Asset utama: SOL, ETH |
| `quote_asset` | VARCHAR(20) | NOT NULL | Asset quote: BTC |
| `priority` | INTEGER | DEFAULT 0 | Prioritas (higher = lebih priorit) |
| `enabled` | BOOLEAN | DEFAULT true | Aktif/nonaktif |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |
| `updated_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |

**Contoh Data:**
```
SOLBTC | SOL | BTC | 1 | true
ETHBTC | ETH | BTC | 2 | true
SUIBTC | SUI | BTC | 3 | true
DOGEBTC | DOGE | BTC | 4 | true
XRPBTC | XRP | BTC | 5 | true
```

**Indeks:**
- `idx_trading_pairs_user_id` ON (`user_id`)
- `idx_trading_pairs_symbol` ON (`symbol`)

---

### `asset_targets`

Target akumulasi aset per user.

| Kolom | Tipe | Constraint | Deskripsi |
|-------|------|------------|-----------|
| `id` | UUID | PK | Identitas unik |
| `user_id` | UUID | FK → users(id), ON DELETE CASCADE | User owner |
| `asset` | VARCHAR(20) | NOT NULL | Nama asset: BTC, ETH, SOL |
| `target_amount` | DECIMAL(36,18) | NOT NULL | Target jumlah akumulasi |
| `current_amount` | DECIMAL(36,18) | DEFAULT 0 | Jumlah saat ini |
| `priority` | INTEGER | DEFAULT 1 | Prioritas akumulasi |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |
| `updated_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |

**Catatan:**
- `priority` menentukan urutan akumulasi (BTC =1 default)
- `current_amount` diupdate setiap kali ada akumulasi baru

---

## 4. MARKET DATA

### `market_candles`

Historical OHLCV candles untuk analisis.

| Kolom | Tipe | Constraint | Deskripsi |
|-------|------|------------|-----------|
| `id` | UUID | PK | Identitas unik |
| `symbol` | VARCHAR(20) | NOT NULL | Symbol: BTCUSDT, ETHBTC |
| `interval` | VARCHAR(10) | NOT NULL | Interval: 1m, 5m, 15m, 1h, 4h, 1d |
| `open` | DECIMAL(36,18) | NOT NULL | Harga open |
| `high` | DECIMAL(36,18) | NOT NULL | Harga highest |
| `low` | DECIMAL(36,18) | NOT NULL | Harga lowest |
| `close` | DECIMAL(36,18) | NOT NULL | Harga close |
| `volume` | DECIMAL(36,18) | NOT NULL | Volume trading |
| `candle_time` | TIMESTAMP | NOT NULL | Waktu candle (UTC) |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |

**Indeks:**
- `idx_market_candles_symbol_interval_time` ON (`symbol`, `interval`, `candle_time`)
- `idx_market_candles_candle_time` ON (`candle_time`)

**Catatan:**
- Partisi berdasarkan `candle_time` direkomendasikan untuk production
- Hapus data candle > 90 hari untuk manajemen storage

---

### `market_snapshots`

Snapshot harga terbaru untuk quick access.

| Kolom | Tipe | Constraint | Deskripsi |
|-------|------|------------|-----------|
| `id` | UUID | PK | Identitas unik |
| `symbol` | VARCHAR(20) | UNIQUE, NOT NULL | Symbol pair |
| `last_price` | DECIMAL(36,18) | NOT NULL | Harga terakhir |
| `volume_24h` | DECIMAL(36,18) | | Volume 24 jam |
| `timestamp` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | Waktu snapshot |

**Indeks:**
- `idx_market_snapshots_symbol` ON (`symbol`)

**Catatan:**
- Upsert setiap tick/1 menit untuk tracking harga real-time
- Digunakan untuk dashboard dan quick price checks

---

## 5. SIGNAL ENGINE

### `signal_history`

History sinyal teknikal yang dihasilkan scanner.

| Kolom | Tipe | Constraint | Deskripsi |
|-------|------|------------|-----------|
| `id` | UUID | PK | Identitas unik |
| `symbol` | VARCHAR(20) | NOT NULL | Symbol pair |
| `rsi` | DECIMAL(10,4) | | RSI value (0-100) |
| `macd` | DECIMAL(18,8) | | MACD value |
| `volume_score` | DECIMAL(10,4) | | Score volume (0-100) |
| `trend_score` | DECIMAL(10,4) | | Score trend (0-100) |
| `momentum_score` | DECIMAL(10,4) | | Score momentum (0-100) |
| `signal_strength` | DECIMAL(10,4) | | Strength keseluruhan (0-100) |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |

**Indeks:**
- `idx_signal_history_symbol_time` ON (`symbol`, `created_at` DESC)

**Catatan:**
- `signal_strength` adalah agregasi weighted dari component scores
- Retain 30 hari untuk backtesting

---

## 6. STRATEGIES

### `strategies`

Definisi strategi trading.

| Kolom | Tipe | Constraint | Deskripsi |
|-------|------|------------|-----------|
| `id` | UUID | PK | Identitas unik |
| `name` | VARCHAR(100) | NOT NULL | Nama strategi |
| `description` | TEXT | | Deskripsi strategi |
| `parameters` | JSONB | NOT NULL | Parameter strategi |
| `enabled` | BOOLEAN | DEFAULT true | Aktif/nonaktif |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |
| `updated_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |

**Contoh Parameters:**
```json
{
  "min_signal_strength": 65,
  "max_position_size_btc": 0.01,
  "stop_loss_percent": 2.0,
  "take_profit_targets": [3, 5, 8],
  "timeframes": ["4h", "1d"]
}
```

---

### `strategy_runs`

Eksekusi/run dari sebuah strategi.

| Kolom | Tipe | Constraint | Deskripsi |
|-------|------|------------|-----------|
| `id` | UUID | PK | Identitas unik |
| `strategy_id` | UUID | FK → strategies(id) | Strategi yang dijalankan |
| `symbol` | VARCHAR(20) | NOT NULL | Symbol yang dianalisis |
| `decision_source` | VARCHAR(30) | NOT NULL | Sumber keputusan |
| `status` | VARCHAR(20) | NOT NULL | Status run |
| `started_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |
| `completed_at` | TIMESTAMP | | Waktu selesai |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |

**decision_source values:**
- `rule_engine` — keputusan dari rule-based system
- `ai_hybrid` — keputusan AI dengan human oversight
- `ai_full` — keputusan AI fully autonomous

**status values:**
- `running` — sedang berjalan
- `completed` — selesai dengan keputusan
- `blocked` — diblokir Risk Guardian
- `failed` — error saat eksekusi

**Indeks:**
- `idx_strategy_runs_strategy_id` ON (`strategy_id`)
- `idx_strategy_runs_symbol` ON (`symbol`)
- `idx_strategy_runs_status` ON (`status`)

---

## 7. ORDERS

### `orders`

Order yang dikirim ke exchange.

| Kolom | Tipe | Constraint | Deskripsi |
|-------|------|------------|-----------|
| `id` | UUID | PK | Identitas unik |
| `user_id` | UUID | FK → users(id), ON DELETE CASCADE | User owner |
| `exchange` | VARCHAR(50) | NOT NULL | Exchange: binance, okx |
| `symbol` | VARCHAR(20) | NOT NULL | Symbol: BTCUSDT |
| `side` | VARCHAR(10) | NOT NULL | Side: buy, sell |
| `order_type` | VARCHAR(20) | NOT NULL | Tipe: market, limit, stop_loss |
| `price` | DECIMAL(36,18) | | Harga limit (nullable untuk market) |
| `quantity` | DECIMAL(36,18) | NOT NULL | Jumlah yang diminta |
| `executed_quantity` | DECIMAL(36,18) | DEFAULT 0 | Jumlah yang tereksekusi |
| `status` | VARCHAR(20) | NOT NULL | Status order |
| `exchange_order_id` | VARCHAR(100) | | Order ID dari exchange |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |
| `updated_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |

**status values:**
- `pending` — belum dikirim
- `submitted` — terkirim ke exchange
- `partial` — partially filled
- `filled` — fully filled
- `cancelled` — dibatalkan
- `rejected` — ditolak exchange

**Indeks:**
- `idx_orders_user_id` ON (`user_id`)
- `idx_orders_symbol` ON (`symbol`)
- `idx_orders_status` ON (`status`)
- `idx_orders_exchange_order_id` ON (`exchange_order_id`)

---

### `trade_executions`

Detail eksekusi per fill.

| Kolom | Tipe | Constraint | Deskripsi |
|-------|------|------------|-----------|
| `id` | UUID | PK | Identitas unik |
| `order_id` | UUID | FK → orders(id), ON DELETE CASCADE | Order parent |
| `price` | DECIMAL(36,18) | NOT NULL | Harga fill |
| `quantity` | DECIMAL(36,18) | NOT NULL | Jumlah fill |
| `fee` | DECIMAL(36,18) | | Fee yang dibayar |
| `fee_asset` | VARCHAR(20) | | Asset fee: BTC, USDT |
| `executed_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | Waktu fill |

**Catatan:**
- Satu order bisa punya banyak trade_executions (partial fills)
- `fee` dan `fee_asset` dari response exchange

---

## 8. PORTFOLIO

### `asset_inventory`

Saldo aset per user di semua exchange.

| Kolom | Tipe | Constraint | Deskripsi |
|-------|------|------------|-----------|
| `id` | UUID | PK | Identitas unik |
| `user_id` | UUID | FK → users(id), ON DELETE CASCADE | User owner |
| `asset` | VARCHAR(20) | NOT NULL | Nama asset: BTC, ETH, USDT |
| `balance` | DECIMAL(36,18) | DEFAULT 0 | Saldo available |
| `locked_balance` | DECIMAL(36,18) | DEFAULT 0 | Saldo locked (di order) |
| `btc_equivalent` | DECIMAL(36,18) | | Nilai setara BTC |
| `updated_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |

**Constraint:**
- `UNIQUE(user_id, asset)`

**Indeks:**
- `idx_asset_inventory_user_id` ON (`user_id`)

**Catatan:**
- `btc_equivalent` dihitung dari harga BTC/USDT saat sync
- Dilock saat ada open order

---

## 9. BTC TREASURY

### `btc_treasury_snapshots`

Snapshot BTC treasury per user (untuk tracking akumulasi).

| Kolom | Tipe | Constraint | Deskripsi |
|-------|------|------------|-----------|
| `id` | UUID | PK | Identitas unik |
| `user_id` | UUID | FK → users(id), ON DELETE CASCADE | User owner |
| `btc_balance` | DECIMAL(36,18) | NOT NULL | BTC available |
| `btc_locked` | DECIMAL(36,18) | DEFAULT 0 | BTC locked |
| `btc_total` | DECIMAL(36,18) | NOT NULL | Total BTC |
| `usd_value` | DECIMAL(36,18) | | Nilai USD saat snapshot |
| `snapshot_time` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |

**Indeks:**
- `idx_btc_treasury_user_time` ON (`user_id`, `snapshot_time` DESC)

**Catatan:**
- Snapshot dibuat setiap kali ada perubahan atau daily
- `usd_value` dari BTC/USDT price saat snapshot

---

### `btc_accumulation_ledger`

Ledger detail setiap akumulasi BTC.

| Kolom | Tipe | Constraint | Deskripsi |
|-------|------|------------|-----------|
| `id` | UUID | PK | Identitas unik |
| `user_id` | UUID | FK → users(id), ON DELETE CASCADE | User owner |
| `source` | VARCHAR(50) | NOT NULL | Sumber akumulasi |
| `asset` | VARCHAR(20) | NOT NULL | Asset yang dikonversi ke BTC |
| `amount_asset` | DECIMAL(36,18) | NOT NULL | Jumlah asset |
| `btc_received` | DECIMAL(36,18) | NOT NULL | BTC yang diterima |
| `fee_btc` | DECIMAL(36,18) | DEFAULT 0 | Fee dalam BTC |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |

**source values:**
- `trade` — dari spot trade
- `staking` — dari staking rewards
- `airdrop` — dari airdrop
- `manual` — deposit manual
- `transfer` — transfer antar akun

**Indeks:**
- `idx_btc_accumulation_user_time` ON (`user_id`, `created_at` DESC)
- `idx_btc_accumulation_source` ON (`source`)

---

### `btc_accumulation_metrics`

Aggregasi metrik akumulasi BTC per periode.

| Kolom | Tipe | Constraint | Deskripsi |
|-------|------|------------|-----------|
| `id` | UUID | PK | Identitas unik |
| `user_id` | UUID | FK → users(id), ON DELETE CASCADE | User owner |
| `period` | VARCHAR(20) | NOT NULL | Periode: daily, weekly, monthly |
| `starting_btc` | DECIMAL(36,18) | NOT NULL | BTC di awal periode |
| `ending_btc` | DECIMAL(36,18) | NOT NULL | BTC di akhir periode |
| `btc_growth` | DECIMAL(36,18) | NOT NULL | Pertumbuhan BTC |
| `growth_percent` | DECIMAL(18,8) | | Persen pertumbuhan |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |

**Indeks:**
- `idx_btc_metrics_user_period` ON (`user_id`, `period`, `created_at` DESC)

---

## 10. AI PROVIDERS

### `ai_providers`

Konfigurasi AI provider (OpenAI, Anthropic, Ollama, dll).

| Kolom | Tipe | Constraint | Deskripsi |
|-------|------|------------|-----------|
| `id` | UUID | PK | Identitas unik |
| `name` | VARCHAR(100) | NOT NULL | Nama provider instance |
| `provider` | VARCHAR(50) | NOT NULL | Provider: openai, anthropic, ollama |
| `model` | VARCHAR(100) | NOT NULL | Model name |
| `encrypted_api_key` | TEXT | | API key terenkripsi |
| `is_active` | BOOLEAN | DEFAULT true | Aktif/nonaktif |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |
| `updated_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |

**Indeks:**
- `idx_ai_providers_active` ON (`is_active`)

**Catatan:**
- Support multiple providers untuk failover
- `encrypted_api_key` nullable untuk Ollama (local)

---

## 11. AI PROMPTS

### `ai_prompt_versions`

Versioned system prompts untuk AI agents.

| Kolom | Tipe | Constraint | Deskripsi |
|-------|------|------------|-----------|
| `id` | UUID | PK | Identitas unik |
| `name` | VARCHAR(100) | NOT NULL | Nama prompt: market_analyst, risk_guardian |
| `version` | VARCHAR(50) | NOT NULL | Version: v1.0, v1.1 |
| `system_prompt` | TEXT | NOT NULL | Isi system prompt |
| `is_active` | BOOLEAN | DEFAULT false | Apakah aktif |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |

**Constraint:**
- `UNIQUE(name, version)`

**Catatan:**
- Hanya satu version per `name` yang `is_active = true`
- Versi lama tetap disimpan untuk audit dan rollback

---

## 12. AI DECISIONS

### `ai_decisions`

Log keputusan AI untuk setiap strategy run.

| Kolom | Tipe | Constraint | Deskripsi |
|-------|------|------------|-----------|
| `id` | UUID | PK | Identitas unik |
| `strategy_run_id` | UUID | FK → strategy_runs(id) | Strategy run terkait |
| `provider_id` | UUID | FK → ai_providers(id) | Provider yang digunakan |
| `symbol` | VARCHAR(20) | NOT NULL | Symbol yang dianalisis |
| `decision` | VARCHAR(20) | NOT NULL | Keputusan: buy, sell, hold, skip |
| `confidence` | DECIMAL(5,2) | | Confidence score (0-100) |
| `reasoning` | TEXT | | Alasan keputusan |
| `prompt_version` | VARCHAR(50) | | Prompt version yang digunakan |
| `input_context` | JSONB | | Context yang dikirim ke AI |
| `output_context` | JSONB | | Output lengkap dari AI |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |

**Indeks:**
- `idx_ai_decisions_symbol` ON (`symbol`)
- `idx_ai_decisions_created` ON (`created_at` DESC)
- `idx_ai_decisions_strategy_run` ON (`strategy_run_id`)

**Catatan:**
- `input_context` dan `output_context` untuk debugging dan learning
- AI SELALU menghasilkan JSON, tidak pernah langsung trade

---

## 13. AI FEEDBACK

### `ai_feedback`

Feedback hasil keputusan AI (untuk learning loop).

| Kolom | Tipe | Constraint | Deskripsi |
|-------|------|------------|-----------|
| `id` | UUID | PK | Identitas unik |
| `decision_id` | UUID | FK → ai_decisions(id), ON DELETE CASCADE | Decision yang di-feedback |
| `btc_before` | DECIMAL(36,18) | NOT NULL | BTC balance sebelum trade |
| `btc_after` | DECIMAL(36,18) | NOT NULL | BTC balance sesudah trade |
| `btc_delta` | DECIMAL(36,18) | NOT NULL | Perubahan BTC |
| `success` | BOOLEAN | NOT NULL | Apakah keputusan berhasil |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |

**Catatan:**
- `success` ditentukan oleh apakah BTC bertambah (akumulasi positif)
- Data ini digunakan untuk improve future AI decisions

---

## 14. AI MEMORY

### `ai_memory`

Long-term memory untuk AI agents (lessons, patterns, market insights).

| Kolom | Tipe | Constraint | Deskripsi |
|-------|------|------------|-----------|
| `id` | UUID | PK | Identitas unik |
| `memory_type` | VARCHAR(50) | NOT NULL | Tipe memory |
| `content` | TEXT | NOT NULL | Isi memory |
| `importance_score` | DECIMAL(10,4) | DEFAULT 1.0 | Skor kepentingan (0-10) |
| `metadata` | JSONB | | Metadata tambahan |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |

**memory_type values:**
- `trade_memory` — lessons dari trade
- `market_memory` — insights tentang market regime
- `strategy_memory` — lessons tentang strategi
- `risk_memory` — lessons tentang risk management
- `general` — general knowledge

**Indeks:**
- `idx_ai_memory_type` ON (`memory_type`)
- `idx_ai_memory_importance` ON (`importance_score` DESC)

**Catatan:**
- Importance score tinggi = lebih mungkin di-retrieve saat decision making
- pgvector extension untuk semantic search (future)

---

## 15. WCH ECOSYSTEM

### `wch_transactions`

Transaksi WCH (Wrapped Crypto Holder) token ecosystem.

| Kolom | Tipe | Constraint | Deskripsi |
|-------|------|------------|-----------|
| `id` | UUID | PK | Identitas unik |
| `user_id` | UUID | FK → users(id), ON DELETE CASCADE | User owner |
| `amount` | DECIMAL(36,18) | NOT NULL | Jumlah WCH |
| `transaction_type` | VARCHAR(50) | NOT NULL | Tipe transaksi |
| `status` | VARCHAR(20) | NOT NULL DEFAULT 'pending' | Status |
| `tx_hash` | VARCHAR(255) | | Transaction hash |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |
| `updated_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |

**transaction_type values:**
- `deposit` — deposit WCH
- `withdrawal` — penarikan WCH
- `fee_payment` — pembayaran fee dengan WCH
- `reward` — reward dari sistem

**status values:**
- `pending` — menunggu konfirmasi
- `confirmed` — confirmed on-chain
- `failed` — gagal

**Indeks:**
- `idx_wch_transactions_user_id` ON (`user_id`)
- `idx_wch_transactions_status` ON (`status`)

---

## 16. LOGGING

### `system_logs`

Logging terpusat untuk audit dan debugging.

| Kolom | Tipe | Constraint | Deskripsi |
|-------|------|------------|-----------|
| `id` | UUID | PK | Identitas unik |
| `user_id` | UUID | FK → users(id), ON DELETE SET NULL | User terkait (nullable) |
| `level` | VARCHAR(20) | NOT NULL | Level: DEBUG, INFO, WARN, ERROR |
| `module` | VARCHAR(50) | NOT NULL | Module: scanner, risk, execution |
| `message` | TEXT | NOT NULL | Log message |
| `metadata` | JSONB | | Data tambahan |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |

**Indeks:**
- `idx_system_logs_level` ON (`level`)
- `idx_system_logs_module` ON (`module`)
- `idx_system_logs_created` ON (`created_at` DESC)
- `idx_system_logs_user` ON (`user_id`)

**Retensi:**
- ERROR: retain 90 hari
- WARN: retain 60 hari
- INFO: retain 30 hari
- DEBUG: retain 7 hari

---

## 17. REPORTING

### `daily_reports`

Laporan harian performance per user.

| Kolom | Tipe | Constraint | Deskripsi |
|-------|------|------------|-----------|
| `id` | UUID | PK | Identitas unik |
| `user_id` | UUID | FK → users(id), ON DELETE CASCADE | User owner |
| `btc_start` | DECIMAL(36,18) | NOT NULL | BTC balance di awal hari |
| `btc_end` | DECIMAL(36,18) | NOT NULL | BTC balance di akhir hari |
| `btc_growth` | DECIMAL(36,18) | NOT NULL | Pertumbuhan BTC hari ini |
| `trade_count` | INTEGER | DEFAULT 0 | Jumlah trade hari ini |
| `win_rate` | DECIMAL(10,4) | | Win rate hari ini (0-100%) |
| `report_date` | DATE | UNIQUE, NOT NULL | Tanggal laporan |
| `created_at` | TIMESTAMP | DEFAULT CURRENT_TIMESTAMP | |

**Constraint:**
- `UNIQUE(user_id, report_date)`

**Indeks:**
- `idx_daily_reports_user_date` ON (`user_id`, `report_date` DESC)

---

## MIGRATION FILES

| File | Deskripsi |
|------|-----------|
| `000001_init_schema.up.sql` | Tabel dasar: users, api_keys, system_logs, wch_transactions |
| `000001_init_schema.down.sql` | Rollback migration 0001 |
| `000002_ai_memory_schema.up.sql` | AI tables: ai_strategies, ai_signal_weights, ai_decision_logs, ai_lessons, user_configs (v1), pool_memories |
| `000002_ai_memory_schema.down.sql` | Rollback migration 0002 |
| `000003_user_configs_refine.up.sql` | Refine user_configs: pisah kolom dari JSONB ke individual columns |
| `000003_user_configs_refine.down.sql` | Rollback ke JSONB generic |
| `000004_full_schema.up.sql` | Schema lengkap V1.0 (NEW - target akhir) |
| `000004_full_schema.down.sql` | Rollback |

---

## INDEXES RECAP

```sql
-- users
CREATE INDEX idx_users_telegram_id ON users(telegram_id);

-- api_keys
CREATE INDEX idx_api_keys_user_id ON api_keys(user_id);
CREATE INDEX idx_api_keys_exchange ON api_keys(exchange);

-- exchange_accounts
CREATE INDEX idx_exchange_accounts_user_id ON exchange_accounts(user_id);

-- trading_pairs
CREATE INDEX idx_trading_pairs_user_id ON trading_pairs(user_id);
CREATE INDEX idx_trading_pairs_symbol ON trading_pairs(symbol);

-- market_candles
CREATE INDEX idx_market_candles_symbol_interval_time ON market_candles(symbol, interval, candle_time);
CREATE INDEX idx_market_candles_candle_time ON market_candles(candle_time);

-- market_snapshots
CREATE INDEX idx_market_snapshots_symbol ON market_snapshots(symbol);

-- signal_history
CREATE INDEX idx_signal_history_symbol_time ON signal_history(symbol, created_at DESC);

-- strategy_runs
CREATE INDEX idx_strategy_runs_strategy_id ON strategy_runs(strategy_id);
CREATE INDEX idx_strategy_runs_symbol ON strategy_runs(symbol);
CREATE INDEX idx_strategy_runs_status ON strategy_runs(status);

-- orders
CREATE INDEX idx_orders_user_id ON orders(user_id);
CREATE INDEX idx_orders_symbol ON orders(symbol);
CREATE INDEX idx_orders_status ON orders(status);
CREATE INDEX idx_orders_exchange_order_id ON orders(exchange_order_id);

-- btc_treasury_snapshots
CREATE INDEX idx_btc_treasury_user_time ON btc_treasury_snapshots(user_id, snapshot_time DESC);

-- btc_accumulation_ledger
CREATE INDEX idx_btc_accumulation_user_time ON btc_accumulation_ledger(user_id, created_at DESC);
CREATE INDEX idx_btc_accumulation_source ON btc_accumulation_ledger(source);

-- btc_accumulation_metrics
CREATE INDEX idx_btc_metrics_user_period ON btc_accumulation_metrics(user_id, period, created_at DESC);

-- ai_providers
CREATE INDEX idx_ai_providers_active ON ai_providers(is_active);

-- ai_decisions
CREATE INDEX idx_ai_decisions_symbol ON ai_decisions(symbol);
CREATE INDEX idx_ai_decisions_created ON ai_decisions(created_at DESC);
CREATE INDEX idx_ai_decisions_strategy_run ON ai_decisions(strategy_run_id);

-- ai_memory
CREATE INDEX idx_ai_memory_type ON ai_memory(memory_type);
CREATE INDEX idx_ai_memory_importance ON ai_memory(importance_score DESC);

-- wch_transactions
CREATE INDEX idx_wch_transactions_user_id ON wch_transactions(user_id);
CREATE INDEX idx_wch_transactions_status ON wch_transactions(status);

-- system_logs
CREATE INDEX idx_system_logs_level ON system_logs(level);
CREATE INDEX idx_system_logs_module ON system_logs(module);
CREATE INDEX idx_system_logs_created ON system_logs(created_at DESC);
CREATE INDEX idx_system_logs_user ON system_logs(user_id);

-- daily_reports
CREATE INDEX idx_daily_reports_user_date ON daily_reports(user_id, report_date DESC);
```

---

## CONSTRAINTS & TRIGGERS

### Auto-update triggers

```sql
-- Fungsi untuk auto-update updated_at
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Trigger untuk semua tabel dengan updated_at
CREATE TRIGGER update_user_configs_modtime
    BEFORE UPDATE ON user_configs
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- (Lakukan serupa untuk tabel lain yang butuh auto-update)
```

### Foreign Key Constraints

- `api_keys.user_id` → `users.id` ON DELETE CASCADE
- `exchange_accounts.user_id` → `users.id` ON DELETE CASCADE
- `user_configs.user_id` → `users.id` ON DELETE CASCADE UNIQUE
- `trading_pairs.user_id` → `users.id` ON DELETE CASCADE
- `asset_targets.user_id` → `users.id` ON DELETE CASCADE
- `orders.user_id` → `users.id` ON DELETE CASCADE
- `asset_inventory.user_id` → `users.id` ON DELETE CASCADE
- `btc_treasury_snapshots.user_id` → `users.id` ON DELETE CASCADE
- `btc_accumulation_ledger.user_id` → `users.id` ON DELETE CASCADE
- `btc_accumulation_metrics.user_id` → `users.id` ON DELETE CASCADE
- `wch_transactions.user_id` → `users.id` ON DELETE CASCADE
- `system_logs.user_id` → `users.id` ON DELETE SET NULL
- `daily_reports.user_id` → `users.id` ON DELETE CASCADE
- `strategy_runs.strategy_id` → `strategies.id`
- `ai_decisions.strategy_run_id` → `strategy_runs.id`
- `ai_decisions.provider_id` → `ai_providers.id`
- `ai_feedback.decision_id` → `ai_decisions.id` ON DELETE CASCADE
- `trade_executions.order_id` → `orders.id` ON DELETE CASCADE

---

## FUTURE CONSIDERATIONS

### pgvector Extension (v0.2.0)
```sql
CREATE EXTENSION IF NOT EXISTS vector;

-- Tambahkan kolom untuk semantic search di ai_memory
ALTER TABLE ai_memory ADD COLUMN embedding vector(1536);
CREATE INDEX idx_ai_memory_embedding ON ai_memory USING ivfflat(embedding vector_cosine_ops);
```

### Table Partitioning (v0.2.0)
```sql
-- Partition market_candles by month
CREATE TABLE market_candles (
    LIKE market_candles_base INCLUDING ALL
) PARTITION BY RANGE (candle_time);

-- Partition system_logs by month
CREATE TABLE system_logs (
    LIKE system_logs_base INCLUDING ALL
) PARTITION BY RANGE (created_at);
```

### Connection Pooling
- Gunakan PgBouncer untuk production
- Mode: transaction (untuk app), session (untuk migrations)

---

## SECURITY CHECKLIST

- [ ] API keys dienkripsi AES-256 sebelum disimpan
- [ ] Encryption key di environment variable, bukan di code
- [ ] No plaintext secrets di logs
- [ ] All actions di-audit via system_logs
- [ ] User data di-hash/di-enkripsi sesuai kebutuhan
- [ ] Database connection pakai SSL
- [ ] Regular backup schedule
- [ ] Access control via RBAC (future)

---

**Versi Dokumen:** 1.0
**Terakhir Diupdate:**2026-06-05
**Maintainer:** NAFAS Dev Team
