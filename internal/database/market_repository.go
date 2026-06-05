// ============================================================
// MODULE: database/market_repository
// Deskripsi: Repository untuk menyimpan dan mengambil market data dari PostgreSQL
// ============================================================

package database

import (
	"context"
	"database/sql"
	"time"

	"github.com/nzf210/nafas-bot/internal/models"
)

// MarketRepository handles market data persistence
// Nama Function: MarketRepository
// Deskripsi: Struct repository untuk operasi CRUD pada market data.
// Parameter/Value Input:
//   - db: *sql.DB — koneksi database
//   - logger: *logger.Logger — logger instance
type MarketRepository struct {
	db *sql.DB
}

// NewMarketRepository creates a new market repository
// Nama Function: NewMarketRepository
// Deskripsi: Membuat instance market repository baru.
// Parameter/Value Input:
//   - db: *sql.DB — koneksi database
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung, hanya inisialisasi
// Output/Return Value:
//   - *MarketRepository: pointer ke repository
func NewMarketRepository(db *sql.DB) *MarketRepository {
	return&MarketRepository{db: db}
}

// SaveCandles bulk insert candles ke database
// Nama Function: SaveCandles
// Deskripsi: Menyimpan multiple candle data ke database market_candles.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk operasi database
//   - candles: []models.MarketCandle — list candle untuk disimpan
// Function yang Dipanggil/Dikonsumsi:
//   - db.ExecContext: dipanggil untuk bulk insert
// Output/Return Value:
//   - error: error jika insert gagal
func (r *MarketRepository) SaveCandles(ctx context.Context, candles []models.MarketCandle) error {
	if len(candles) == 0 {
		return nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO market_candles (id, symbol, interval, open, high, low, close, volume, candle_time, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (symbol, interval, candle_time)
		DO UPDATE SET open = EXCLUDED.open, high = EXCLUDED.high, low = EXCLUDED.low, close = EXCLUDED.close, volume = EXCLUDED.volume
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, c := range candles {
		_, err := stmt.ExecContext(ctx, c.ID, c.Symbol, c.Interval, c.Open, c.High, c.Low, c.Close, c.Volume, c.CandleTime, time.Now())
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetCandles mengambil candles dari database
// Nama Function: GetCandles
// Deskripsi: Mengambil candle data dari database untuk symbol dan interval tertentu.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk operasi database
//   - symbol: string — symbol trading
//   - interval: string — timeframe
//   - limit: int — jumlah candle maksimal
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryContext: dipanggil untuk SELECT query
// Output/Return Value:
//   - []models.MarketCandle: list candle
//   - error: error jika query gagal
func (r *MarketRepository) GetCandles(ctx context.Context, symbol, interval string, limit int) ([]models.MarketCandle, error) {
	if limit == 0 {
		limit = 100
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT id, symbol, interval, open, high, low, close, volume, candle_time, created_at
		FROM market_candles
		WHERE symbol = $1 AND interval = $2
		ORDER BY candle_time DESC
		LIMIT $3
	`, symbol, interval, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candles []models.MarketCandle
	for rows.Next() {
		var c models.MarketCandle
		err := rows.Scan(&c.ID, &c.Symbol, &c.Interval, &c.Open, &c.High, &c.Low, &c.Close, &c.Volume, &c.CandleTime, &c.CreatedAt)
		if err != nil {
			return nil, err
		}
		candles = append(candles, c)
	}

	return candles, rows.Err()
}

// SaveSnapshot menyimpan market snapshot ke database
// Nama Function: SaveSnapshot
// Deskripsi: Menyimpan market snapshot (harga terakhir) ke database.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk operasi database
//   - snapshot: *models.MarketSnapshot — snapshot untuk disimpan
// Function yang Dipanggil/Dikonsumsi:
//   - db.ExecContext: dipanggil untuk INSERT/UPDATE
// Output/Return Value:
//   - error: error jika insert gagal
func (r *MarketRepository) SaveSnapshot(ctx context.Context, snapshot *models.MarketSnapshot) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO market_snapshots (id, symbol, last_price, volume_24h, timestamp)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (symbol) DO UPDATE SET
			last_price = EXCLUDED.last_price,
			volume_24h = EXCLUDED.volume_24h,
			timestamp = EXCLUDED.timestamp
	`, snapshot.ID, snapshot.Symbol, snapshot.LastPrice, snapshot.Volume24h, snapshot.Timestamp)

	return err
}

// GetLatestSnapshot mengambil snapshot terakhir untuk symbol
// Nama Function: GetLatestSnapshot
// Deskripsi: Mengambil snapshot terakhir dari database untuk symbol tertentu.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk operasi database
//   - symbol: string — symbol trading
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryRowContext: dipanggil untuk SELECT query
// Output/Return Value:
//   - *models.MarketSnapshot: snapshot terakhir
//   - error: error jika query gagal atau tidak ada data
func (r *MarketRepository) GetLatestSnapshot(ctx context.Context, symbol string) (*models.MarketSnapshot, error) {
	var s models.MarketSnapshot
	err := r.db.QueryRowContext(ctx, `
		SELECT id, symbol, last_price, volume_24h, timestamp
		FROM market_snapshots
		WHERE symbol = $1
		ORDER BY timestamp DESC
		LIMIT 1
	`, symbol).Scan(&s.ID, &s.Symbol, &s.LastPrice, &s.Volume24h, &s.Timestamp)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &s, err
}

// SaveSignal menyimpan signal history ke database
// Nama Function: SaveSignal
// Deskripsi: Menyimpan signal history setelah kalkulasi indikator.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk operasi database
//   - signal: *models.SignalHistory — signal untuk disimpan
// Function yang Dipanggil/Dikonsumsi:
//   - db.ExecContext: dipanggil untuk INSERT
// Output/Return Value:
//   - error: error jika insert gagal
func (r *MarketRepository) SaveSignal(ctx context.Context, signal *models.SignalHistory) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO signal_history (id, symbol, rsi, macd, volume_score, trend_score, momentum_score, signal_strength, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, signal.ID, signal.Symbol, signal.RSI, signal.MACD, signal.VolumeScore, signal.TrendScore, signal.MomentumScore, signal.SignalStrength, time.Now())

	return err
}

// GetRecentSignals mengambil recent signals untuk symbol
// Nama Function: GetRecentSignals
// Deskripsi: Mengambil signal history terakhir untuk symbol.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk operasi database
//   - symbol: string — symbol trading
//   - limit: int — jumlah signal maksimal
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryContext: dipanggil untuk SELECT query
// Output/Return Value:
//   - []models.SignalHistory: list signal
//   - error: error jika query gagal
func (r *MarketRepository) GetRecentSignals(ctx context.Context, symbol string, limit int) ([]models.SignalHistory, error) {
	if limit == 0 {
		limit = 50
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT id, symbol, rsi, macd, volume_score, trend_score, momentum_score, signal_strength, created_at
		FROM signal_history
		WHERE symbol = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, symbol, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var signals []models.SignalHistory
	for rows.Next() {
		var s models.SignalHistory
		err := rows.Scan(&s.ID, &s.Symbol, &s.RSI, &s.MACD, &s.VolumeScore, &s.TrendScore, &s.MomentumScore, &s.SignalStrength, &s.CreatedAt)
		if err != nil {
			return nil, err
		}
		signals = append(signals, s)
	}

	return signals, rows.Err()
}
