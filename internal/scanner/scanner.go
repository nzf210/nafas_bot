// ============================================================
// MODULE: scanner
// Deskripsi: Market data ingestion - OHLCV, ticker, orderbook
// ============================================================

package scanner

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/nzf210/nafas-bot/internal/exchange"
	"github.com/nzf210/nafas-bot/internal/logger"
	"github.com/nzf210/nafas-bot/internal/models"
	"github.com/shopspring/decimal"
)

// Scanner handles market data ingestion
// Nama Function: Scanner
// Deskripsi: Struct utama untuk market data scanning.
// Parameter/Value Input:
//   - exchange: exchange.Exchange — client exchange (Binance/OKX)
//   - symbols: []string — list symbol yang akan di-scan
//   - intervals: []string — list timeframe (1m, 5m, 1h, 4h, 1d)
//   - logger: *logger.Logger — logger instance
type Scanner struct {
	mu        sync.RWMutex
	exchange  exchange.Exchange
	symbols   []string
	intervals []string
	logger    *logger.Logger
}

// NewScanner creates a new market scanner
// Nama Function: NewScanner
// Deskripsi: Membuat instance scanner baru.
// Parameter/Value Input:
//   - exch: exchange.Exchange — exchange client
//   - symbols: []string — list symbol untuk di-scan
//   - intervals: []string — list timeframe
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung, hanya inisialisasi
// Output/Return Value:
//   - *Scanner: pointer ke scanner instance
func NewScanner(exch exchange.Exchange, symbols, intervals []string) *Scanner {
	return &Scanner{
		exchange:  exch,
		symbols:   symbols,
		intervals: intervals,
		logger:    logger.Default().WithField("module", "scanner"),
	}
}

// MarketData represents aggregated market data for analysis
// Nama Function: MarketData
// Deskripsi: Struct untuk data market yang sudah diagregasi.
// Parameter/Value Input:
//   - Symbol: string — symbol trading
//   - Interval: string — timeframe
//   - Candles: []models.MarketCandle — data candle
//   - LatestPrice: decimal.Decimal — harga terakhir
//   - Volume24h: decimal.Decimal — volume 24 jam
// Function yang Dipanggil/Dikonsumsi:
//   - FetchCandles: dipanggil untuk populate Candles
//   - FetchTicker: dipanggil untuk populate LatestPrice dan Volume24h
// Output/Return Value:
//   - MarketData: struct data market untuk analisis
type MarketData struct {
	Symbol      string
	Interval    string
	Candles     []models.MarketCandle
	LatestPrice decimal.Decimal
	Volume24h   decimal.Decimal
	Timestamp   time.Time
}

// FetchCandles fetches OHLCV data for a symbol
// Nama Function: FetchCandles
// Deskripsi: Mengambil data candle OHLCV dari exchange.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk HTTP request
//   - symbol: string — symbol trading (contoh: BTCUSDT)
//   - interval: string — timeframe (1m, 5m, 15m, 1h, 4h, 1d)
//   - limit: int — jumlah candle yang diambil (default 100)
// Function yang Dipanggil/Dikonsumsi:
//   - exchange.GetCandles: dipanggil untuk fetch data dari exchange
// Output/Return Value:
//   - []models.MarketCandle: list candle data
//   - error: error jika fetch gagal
func (s *Scanner) FetchCandles(ctx context.Context, symbol, interval string, limit int) ([]models.MarketCandle, error) {
	if limit == 0 {
		limit = 100
	}

	s.logger.Infof("Fetching candles for %s %s (limit: %d)", symbol, interval, limit)

	candles, err := s.exchange.GetCandles(ctx, symbol, interval, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch candles: %w", err)
	}

	s.logger.Infof("Fetched %d candles for %s %s", len(candles), symbol, interval)
	return candles, nil
}

// FetchTicker fetches current ticker data
// Nama Function: FetchTicker
// Deskripsi: Mengambil data ticker terakhir (harga + volume) untuk symbol.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk HTTP request
//   - symbol: string — symbol trading
// Function yang Dipanggil/Dikonsumsi:
//   - exchange.GetTicker: dipanggil untuk fetch dari exchange
// Output/Return Value:
//   - *models.MarketSnapshot: snapshot harga dan volume
//   - error: error jika fetch gagal
func (s *Scanner) FetchTicker(ctx context.Context, symbol string) (*models.MarketSnapshot, error) {
	ticker, err := s.exchange.GetTicker(ctx, symbol)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch ticker for %s: %w", symbol, err)
	}
	return ticker, nil
}

// ScanAllPairs scans all configured symbols
// Nama Function: ScanAllPairs
// Deskripsi: Scan semua symbol dan interval secara paralel menggunakan goroutine.
//   Batas concurrent HTTP calls: 10 (semaphore pattern).
//   FetchCandles dan FetchTicker dipanggil concurrently per symbol.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk scan operation (harus punya timeout)
//   - callback: func(MarketData) — function yang dipanggil untuk setiap data
// Function yang Dipanggil/Dikonsumsi:
//   - FetchCandles: dipanggil concurrently per symbol x interval
//   - FetchTicker: dipanggil concurrently per symbol
// Output/Return Value:
//   - error: error jika semua scan gagal, nil jika ada yang berhasil
// Catatan: Context harus punya timeout untuk avoid goroutine leak.
//   Callback dipanggil secara concurrent — caller harus handle thread-safety.
func (s *Scanner) ScanAllPairs(ctx context.Context, callback func(MarketData)) error {
	const maxConcurrent = 10
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup

	s.mu.RLock()
	symbols := make([]string, len(s.symbols))
	copy(symbols, s.symbols)
	intervals := s.intervals
	s.mu.RUnlock()

	errCh := make(chan error, len(symbols)*len(intervals))

	for _, symbol := range symbols {
		for _, interval := range intervals {
			wg.Add(1)
			go func(sym, intv string) {
				defer wg.Done()

				sem <- struct{}{}
				defer func() { <-sem }()

				candles, err := s.FetchCandles(ctx, sym, intv, 100)
				if err != nil {
					s.logger.Warnf("Failed to fetch candles for %s %s: %v", sym, intv, err)
					errCh <- err
					return
				}

				ticker, err := s.FetchTicker(ctx, sym)
				if err != nil {
					s.logger.Warnf("Failed to fetch ticker for %s: %v", sym, err)
					errCh <- err
					return
				}

				callback(MarketData{
					Symbol:      sym,
					Interval:    intv,
					Candles:     candles,
					LatestPrice: ticker.LastPrice,
					Volume24h:   ticker.Volume24h,
					Timestamp:   time.Now(),
				})
			}(symbol, interval)
		}
	}

	wg.Wait()
	close(errCh)

	var errors []error
	for err := range errCh {
		errors = append(errors, err)
	}
	if len(errors) > 0 && len(errors) == len(symbols)*len(intervals) {
		return fmt.Errorf("all scan operations failed: %v", errors)
	}
	return nil
}

// AddSymbol adds a new symbol to the scanner
// Nama Function: AddSymbol
// Deskripsi: Menambahkan symbol baru ke dalam list scan jika belum ada.
// Parameter/Value Input:
//   - symbol: string — symbol trading baru (contoh: "BNBUSDT")
func (s *Scanner) AddSymbol(symbol string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if slices.Contains(s.symbols, symbol) {
		return
	}
	s.symbols = append(s.symbols, symbol)
	s.logger.Infof("Added new symbol to scanner: %s", symbol)
}

// AddSymbols adds multiple symbols to the scanner
// Nama Function: AddSymbols
func (s *Scanner) AddSymbols(symbols []string) {
	for _, sym := range symbols {
		s.AddSymbol(sym)
	}
}

// RemoveSymbol removes a symbol from the scanner
// Nama Function: RemoveSymbol
// Deskripsi: Menghapus symbol dari list scan.
func (s *Scanner) RemoveSymbol(symbol string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sym := range s.symbols {
		if sym == symbol {
			s.symbols = append(s.symbols[:i], s.symbols[i+1:]...)
			s.logger.Infof("Removed symbol from scanner: %s", symbol)
			return
		}
	}
}

// GetSymbols returns a copy of the current symbols list
// Nama Function: GetSymbols
func (s *Scanner) GetSymbols() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	symbols := make([]string, len(s.symbols))
	copy(symbols, s.symbols)
	return symbols
}

// CalculateRSI calculates Relative Strength Index
// Nama Function: CalculateRSI
// Deskripsi: Menghitung RSI dari data candle.
// Parameter/Value Input:
//   - candles: []models.MarketCandle — data candle
//   - period: int — RSI period (default 14)
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung, kalkulasi matematika RSI
// Output/Return Value:
//   - decimal.Decimal: nilai RSI (0-100)
//   - error: error jika data tidak cukup
func (s *Scanner) CalculateRSI(candles []models.MarketCandle, period int) (decimal.Decimal, error) {
	if len(candles) < period+1 {
		return decimal.Zero, fmt.Errorf("insufficient data for RSI calculation")
	}

	if period == 0 {
		period = 14
	}

	var gains, losses = decimal.Zero, decimal.Zero
	for i := len(candles) - period; i < len(candles); i++ {
		change := candles[i].Close.Sub(candles[i-1].Close)
		if change.GreaterThan(decimal.Zero) {
			gains = gains.Add(change)
		} else {
			losses = losses.Add(change.Abs())
		}
	}

	avgGain := gains.Div(decimal.NewFromInt(int64(period)))
	avgLoss := losses.Div(decimal.NewFromInt(int64(period)))

	if avgLoss.IsZero() {
		return decimal.NewFromFloat(100), nil
	}

	rs := avgGain.Div(avgLoss)
	rsi := decimal.NewFromFloat(100).Sub(decimal.NewFromFloat(100).Div(rs.Add(decimal.NewFromFloat(1))))

	return rsi, nil
}

// CalculateSignalStrength calculates overall signal strength
// Nama Function: CalculateSignalStrength
// Deskripsi: Menghitung kekuatan sinyal dari multiple indicators.
// Parameter/Value Input:
//   - candles: []models.MarketCandle — data candle
//   - rsi: decimal.Decimal — RSI value
//   - volumeScore: decimal.Decimal — volume score (0-100)
// Function yang Dipanggil/Dikonsumsi:
//   - CalculateRSI: dipanggil untuk kalkulasi RSI
//   - Tidak ada function lain, weighted average calculation
// Output/Return Value:
//   - decimal.Decimal: signal strength (0-100)
//   - error: error jika kalkulasi gagal
func (s *Scanner) CalculateSignalStrength(candles []models.MarketCandle, rsi, volumeScore decimal.Decimal) (decimal.Decimal, error) {
	// Calculate trend score from recent candles
	trendScore := decimal.NewFromFloat(50)
	if len(candles) >= 20 {
		recentAvg := candles[len(candles)-5].Close
		olderAvg := candles[len(candles)-20].Close
		if olderAvg.IsZero() {
			olderAvg = decimal.NewFromFloat(1)
		}
		trendChange := recentAvg.Sub(olderAvg).Div(olderAvg).Mul(decimal.NewFromFloat(100))
		trendScore = trendChange.Add(decimal.NewFromFloat(50))
		if trendScore.LessThan(decimal.Zero) {
			trendScore = decimal.Zero
		}
		if trendScore.GreaterThan(decimal.NewFromFloat(100)) {
			trendScore = decimal.NewFromFloat(100)
		}
	}

	// Weighted average: RSI (30%) + Volume (30%) + Trend (40%)
	rsiScore := rsi
	signalStrength := rsiScore.Mul(decimal.NewFromFloat(0.3)).
		Add(volumeScore.Mul(decimal.NewFromFloat(0.3))).
		Add(trendScore.Mul(decimal.NewFromFloat(0.4)))

	return signalStrength, nil
}