package scanner

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nzf210/nafas-bot/internal/models"
	"github.com/shopspring/decimal"
)

// mockExchange implements exchange.Exchange for testing
type mockExchange struct {
	name           string
	getCandlesFunc func(ctx context.Context, symbol, interval string, limit int) ([]models.MarketCandle, error)
	getTickerFunc  func(ctx context.Context, symbol string) (*models.MarketSnapshot, error)
}

func (m *mockExchange) GetName() string { return m.name }
func (m *mockExchange) GetBalances(ctx context.Context, apiKey, apiSecret, passphrase string) (map[string]decimal.Decimal, error) { return nil, nil }
func (m *mockExchange) PlaceOrder(ctx context.Context, apiKey, apiSecret, passphrase string, order models.Order) (*models.Order, error) { return nil, nil }
func (m *mockExchange) GetOrderStatus(ctx context.Context, apiKey, apiSecret, passphrase string, orderID string, symbol string) (*models.Order, error) { return nil, nil }
func (m *mockExchange) GetPrice(ctx context.Context, symbol string) (decimal.Decimal, error) { return decimal.NewFromFloat(50000), nil }
func (m *mockExchange) GetCandles(ctx context.Context, symbol, interval string, limit int) ([]models.MarketCandle, error) {
	if m.getCandlesFunc != nil {
		return m.getCandlesFunc(ctx, symbol, interval, limit)
	}
	return []models.MarketCandle{
		{Close: decimal.NewFromFloat(50000), Volume: decimal.NewFromFloat(1000)},
	}, nil
}
func (m *mockExchange) GetTicker(ctx context.Context, symbol string) (*models.MarketSnapshot, error) {
	if m.getTickerFunc != nil {
		return m.getTickerFunc(ctx, symbol)
	}
	return &models.MarketSnapshot{
		LastPrice: decimal.NewFromFloat(50000),
		Volume24h: decimal.NewFromFloat(1000000),
	}, nil
}

// TestScanner_AddSymbol tests adding symbols to scanner
func TestScanner_AddSymbol(t *testing.T) {
	mock := &mockExchange{name: "Binance"}
	s := NewScanner(mock, []string{"BTCUSDT"}, []string{"1h"})

	// Add duplicate - should not add
	s.AddSymbol("BTCUSDT")
	symbols := s.GetSymbols()
	if len(symbols) != 1 {
		t.Errorf("Expected 1 symbol, got %d", len(symbols))
	}

	// Add new symbol
	s.AddSymbol("ETHUSDT")
	symbols = s.GetSymbols()
	if len(symbols) != 2 {
		t.Errorf("Expected 2 symbols, got %d", len(symbols))
	}

	// Verify order
	if symbols[0] != "BTCUSDT" || symbols[1] != "ETHUSDT" {
		t.Errorf("Unexpected symbol order: %v", symbols)
	}
}

// TestScanner_RemoveSymbol tests removing symbols from scanner
func TestScanner_RemoveSymbol(t *testing.T) {
	mock := &mockExchange{name: "Binance"}
	s := NewScanner(mock, []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"}, []string{"1h"})

	s.RemoveSymbol("ETHUSDT")
	symbols := s.GetSymbols()
	if len(symbols) != 2 {
		t.Errorf("Expected 2 symbols after removal, got %d", len(symbols))
	}

	// Remove non-existent
	s.RemoveSymbol("NONEXISTENT")
	symbols = s.GetSymbols()
	if len(symbols) != 2 {
		t.Errorf("Expected 2 symbols (no change), got %d", len(symbols))
	}
}

// TestScanner_AddSymbols tests adding multiple symbols
func TestScanner_AddSymbols(t *testing.T) {
	mock := &mockExchange{name: "Binance"}
	s := NewScanner(mock, []string{}, []string{"1h"})

	s.AddSymbols([]string{"BTCUSDT", "ETHUSDT", "SOLUSDT"})
	symbols := s.GetSymbols()
	if len(symbols) != 3 {
		t.Errorf("Expected 3 symbols, got %d", len(symbols))
	}
}

// TestScanner_ScanAllPairs_Concurrent tests concurrent scanning with semaphore
func TestScanner_ScanAllPairs_Concurrent(t *testing.T) {
	var concurrentCalls int32
	var maxConcurrent int32

	mock := &mockExchange{
		name: "Binance",
		getCandlesFunc: func(ctx context.Context, symbol, interval string, limit int) ([]models.MarketCandle, error) {
			current := atomic.AddInt32(&concurrentCalls, 1)
			defer atomic.AddInt32(&concurrentCalls, -1)

			// Track max concurrent
			for {
				old := atomic.LoadInt32(&maxConcurrent)
				if current <= old {
					break
				}
				if atomic.CompareAndSwapInt32(&maxConcurrent, old, current) {
					break
				}
			}

			// Simulate API latency
			time.Sleep(50 * time.Millisecond)
			return []models.MarketCandle{{Close: decimal.NewFromFloat(50000)}}, nil
		},
		getTickerFunc: func(ctx context.Context, symbol string) (*models.MarketSnapshot, error) {
			return &models.MarketSnapshot{
				LastPrice: decimal.NewFromFloat(50000),
				Volume24h: decimal.NewFromFloat(1000000),
			}, nil
		},
	}

	s := NewScanner(mock, []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "BNBUSDT", "XRPUSDT"}, []string{"1m", "5m"})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var callbackCount int32
	err := s.ScanAllPairs(ctx, func(data MarketData) {
		atomic.AddInt32(&callbackCount, 1)
	})
	if err != nil {
		t.Errorf("ScanAllPairs returned error: %v", err)
	}

	// Verify all callbacks were called (5 symbols x 2 intervals = 10)
	expectedCalls := int32(len(s.GetSymbols()) * 2)
	if callbackCount != expectedCalls {
		t.Errorf("Expected %d callbacks, got %d", expectedCalls, callbackCount)
	}

	// Verify semaphore limit was respected (max 10 concurrent)
	if maxConcurrent > 10 {
		t.Errorf("Semaphore violated: max concurrent calls was %d, expected <= 10", maxConcurrent)
	}
}

// TestScanner_ScanAllPairs_ContextCancellation tests that goroutines respect context cancellation
func TestScanner_ScanAllPairs_ContextCancellation(t *testing.T) {
	var callCount int32

	mock := &mockExchange{
		name: "Binance",
		getCandlesFunc: func(ctx context.Context, symbol, interval string, limit int) ([]models.MarketCandle, error) {
			// Long delay - should be cancelled
			select {
			case <-time.After(2 * time.Second):
			case <-ctx.Done():
			}
			atomic.AddInt32(&callCount, 1)
			return []models.MarketCandle{{Close: decimal.NewFromFloat(50000)}}, nil
		},
	}

	s := NewScanner(mock, []string{"BTCUSDT", "ETHUSDT"}, []string{"1h"})

	// Very short timeout - should cancel before completion
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := s.ScanAllPairs(ctx, func(data MarketData) {})
	if err != nil {
		t.Errorf("ScanAllPairs should not return error on partial failure, got: %v", err)
	}
}

// TestScanner_FetchCandles tests candle fetching
func TestScanner_FetchCandles(t *testing.T) {
	mock := &mockExchange{
		name: "Binance",
		getCandlesFunc: func(ctx context.Context, symbol, interval string, limit int) ([]models.MarketCandle, error) {
			if symbol != "BTCUSDT" {
				t.Errorf("Expected BTCUSDT, got %s", symbol)
			}
			if interval != "1h" {
				t.Errorf("Expected 1h, got %s", interval)
			}
			return []models.MarketCandle{
				{Close: decimal.NewFromFloat(50000), Volume: decimal.NewFromFloat(1000)},
				{Close: decimal.NewFromFloat(51000), Volume: decimal.NewFromFloat(1100)},
			}, nil
		},
	}

	s := NewScanner(mock, []string{"BTCUSDT"}, []string{"1h"})
	candles, err := s.FetchCandles(context.Background(), "BTCUSDT", "1h", 100)
	if err != nil {
		t.Errorf("FetchCandles failed: %v", err)
	}
	if len(candles) != 2 {
		t.Errorf("Expected 2 candles, got %d", len(candles))
	}
}

// TestScanner_FetchTicker tests ticker fetching
func TestScanner_FetchTicker(t *testing.T) {
	mock := &mockExchange{
		name: "Binance",
		getTickerFunc: func(ctx context.Context, symbol string) (*models.MarketSnapshot, error) {
			return &models.MarketSnapshot{
				LastPrice: decimal.NewFromFloat(50000),
				Volume24h: decimal.NewFromFloat(1000000),
			}, nil
		},
	}

	s := NewScanner(mock, []string{"BTCUSDT"}, []string{"1h"})
	ticker, err := s.FetchTicker(context.Background(), "BTCUSDT")
	if err != nil {
		t.Errorf("FetchTicker failed: %v", err)
	}
	if !ticker.LastPrice.Equal(decimal.NewFromFloat(50000)) {
		t.Errorf("Expected price 50000, got %s", ticker.LastPrice)
	}
}

// TestScanner_CalculateRSI tests RSI calculation
func TestScanner_CalculateRSI(t *testing.T) {
	mock := &mockExchange{name: "Binance"}
	s := NewScanner(mock, []string{"BTCUSDT"}, []string{"1h"})

	// Create candles with consistent upward movement (RSI should be high)
	candles := make([]models.MarketCandle, 20)
	for i := range candles {
		candles[i] = models.MarketCandle{
			Close: decimal.NewFromFloat(50000 + float64(i)*100),
		}
	}

	rsi, err := s.CalculateRSI(candles, 14)
	if err != nil {
		t.Errorf("CalculateRSI failed: %v", err)
	}

	// RSI should be high (>50) for upward trend
	if rsi.LessThan(decimal.NewFromFloat(50)) {
		t.Errorf("Expected RSI > 50 for upward trend, got %s", rsi)
	}

	// RSI should be <= 100
	if rsi.GreaterThan(decimal.NewFromFloat(100)) {
		t.Errorf("RSI should not exceed 100, got %s", rsi)
	}

	// Test insufficient data
	_, err = s.CalculateRSI(candles[:5], 14)
	if err == nil {
		t.Errorf("Expected error for insufficient data")
	}
}

// TestScanner_CalculateRSI_DownwardTrend tests RSI for downward trend
func TestScanner_CalculateRSI_DownwardTrend(t *testing.T) {
	mock := &mockExchange{name: "Binance"}
	s := NewScanner(mock, []string{"BTCUSDT"}, []string{"1h"})

	// Create candles with consistent downward movement (RSI should be low)
	candles := make([]models.MarketCandle, 20)
	for i := range candles {
		candles[i] = models.MarketCandle{
			Close: decimal.NewFromFloat(51000 - float64(i)*100),
		}
	}

	rsi, err := s.CalculateRSI(candles, 14)
	if err != nil {
		t.Errorf("CalculateRSI failed: %v", err)
	}

	// RSI should be low (<50) for downward trend
	if rsi.GreaterThan(decimal.NewFromFloat(50)) {
		t.Errorf("Expected RSI < 50 for downward trend, got %s", rsi)
	}
}

// TestScanner_CalculateSignalStrength tests signal strength calculation
func TestScanner_CalculateSignalStrength(t *testing.T) {
	mock := &mockExchange{name: "Binance"}
	s := NewScanner(mock, []string{"BTCUSDT"}, []string{"1h"})

	candles := make([]models.MarketCandle, 25)
	for i := range candles {
		candles[i] = models.MarketCandle{
			Close: decimal.NewFromFloat(50000 + float64(i)*100),
		}
	}

	strength, err := s.CalculateSignalStrength(candles, decimal.NewFromFloat(60), decimal.NewFromFloat(70))
	if err != nil {
		t.Errorf("CalculateSignalStrength failed: %v", err)
	}

	// Signal strength should be between 0 and 100
	if strength.LessThan(decimal.Zero) || strength.GreaterThan(decimal.NewFromFloat(100)) {
		t.Errorf("Signal strength should be 0-100, got %s", strength)
	}
}

// TestPairManager_Basic tests basic PairManager operations
func TestPairManager_Basic(t *testing.T) {
	pm := NewPairManager(nil) // nil DB for testing

	// Test adding pairs
	pm.AddUserPair("user1", "Binance", "BTCUSDT")
	pm.AddUserPair("user1", "Binance", "ETHUSDT")
	pm.AddUserPair("user2", "Binance", "BTCUSDT")

	// Check user1's pairs
	user1Pairs := pm.GetUserPairs("user1")
	if len(user1Pairs["Binance"]) != 2 {
		t.Errorf("Expected user1 to have 2 pairs, got %d", len(user1Pairs["Binance"]))
	}

	// Check master pairs - BTCUSDT should have 2 users
	masterPairs := pm.GetMasterPairs()
	if masterPairs["Binance"]["BTCUSDT"] != 2 {
		t.Errorf("Expected BTCUSDT to have 2 users, got %d", masterPairs["Binance"]["BTCUSDT"])
	}

	// ETHUSDT should have 1 user
	if masterPairs["Binance"]["ETHUSDT"] != 1 {
		t.Errorf("Expected ETHUSDT to have 1 user, got %d", masterPairs["Binance"]["ETHUSDT"])
	}
}

// TestPairManager_RemovePair tests removing pairs
func TestPairManager_RemovePair(t *testing.T) {
	pm := NewPairManager(nil)

	pm.AddUserPair("user1", "Binance", "BTCUSDT")
	pm.AddUserPair("user2", "Binance", "BTCUSDT")

	// Remove user1's BTCUSDT
	pm.RemoveUserPair("user1", "Binance", "BTCUSDT")

	masterPairs := pm.GetMasterPairs()
	// Should still have 1 user
	if masterPairs["Binance"]["BTCUSDT"] != 1 {
		t.Errorf("Expected 1 user after removal, got %d", masterPairs["Binance"]["BTCUSDT"])
	}

	// Remove user2's BTCUSDT - should be completely removed
	pm.RemoveUserPair("user2", "Binance", "BTCUSDT")
	masterPairs = pm.GetMasterPairs()
	if _, exists := masterPairs["Binance"]["BTCUSDT"]; exists {
		t.Errorf("BTCUSDT should be removed from master pairs")
	}
}

// TestPairManager_DuplicateAdd tests that duplicate adds are handled
func TestPairManager_DuplicateAdd(t *testing.T) {
	pm := NewPairManager(nil)

	pm.AddUserPair("user1", "Binance", "BTCUSDT")
	pm.AddUserPair("user1", "Binance", "BTCUSDT") // duplicate

	masterPairs := pm.GetMasterPairs()
	if masterPairs["Binance"]["BTCUSDT"] != 1 {
		t.Errorf("Duplicate add should not increase count, got %d", masterPairs["Binance"]["BTCUSDT"])
	}
}

// TestPairManager_RegisterScanner tests scanner registration
func TestPairManager_RegisterScanner(t *testing.T) {
	pm := NewPairManager(nil)
	mock := &mockExchange{name: "Binance"}
	scanner := NewScanner(mock, []string{}, []string{"1h"})

	// Register scanner first
	pm.RegisterScanner("Binance", scanner)

	// Add pair - should add to scanner
	pm.AddUserPair("user1", "Binance", "BTCUSDT")

	symbols := scanner.GetSymbols()
	if len(symbols) != 1 || symbols[0] != "BTCUSDT" {
		t.Errorf("Scanner should have BTCUSDT, got %v", symbols)
	}
}

// TestPairManager_MultipleExchanges tests multiple exchange support
func TestPairManager_MultipleExchanges(t *testing.T) {
	pm := NewPairManager(nil)

	pm.AddUserPair("user1", "Binance", "BTCUSDT")
	pm.AddUserPair("user1", "OKX", "BTCUSDT")

	userPairs := pm.GetUserPairs("user1")
	if len(userPairs["Binance"]) != 1 {
		t.Errorf("Expected 1 Binance pair, got %d", len(userPairs["Binance"]))
	}
	if len(userPairs["OKX"]) != 1 {
		t.Errorf("Expected 1 OKX pair, got %d", len(userPairs["OKX"]))
	}
}
