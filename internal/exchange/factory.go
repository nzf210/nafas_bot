package exchange

import (
	"fmt"
	"strings"
	"sync"

	"github.com/nzf210/nafas-bot/internal/logger"
)

// ExchangeRegistry maps exchange names to their constructors
type ExchangeRegistry map[string]func() Exchange

// ExchangeFactory creates and caches exchange clients per user
// Nama Function: ExchangeFactory
// Deskripsi: Factory untuk membuat dan cache exchange client per user. Menggunakan registry pattern
//   untuk mendukung multiple exchange (Binance, OKX, dll).
//
// Field:
//   - mu: sync.RWMutex untuk thread-safe access ke clients map
//   - clients: map cache "userID:exchangeName" → Exchange client
//   - registry: map exchange name → constructor function
//   - logger: logger untuk tracking
//
// Usage:
//   factory := NewExchangeFactory()
//   client, err := factory.GetExchange("user-uuid", "binance")
type ExchangeFactory struct {
	mu       sync.RWMutex
	clients  map[string]Exchange
	registry ExchangeRegistry
	logger   *logger.Logger
}

// NewExchangeFactory creates a new factory with default exchanges registered
// Nama Function: NewExchangeFactory
// Deskripsi: Membuat factory baru dengan default exchanges (binance, okx) sudah teregistrasi.
// Output/Return Value:
//   - *ExchangeFactory: pointer ke factory instance
func NewExchangeFactory() *ExchangeFactory {
	f := &ExchangeFactory{
		clients:  make(map[string]Exchange),
		registry: make(ExchangeRegistry),
		logger:   logger.Default().WithField("module", "exchange/factory"),
	}

	// Register default exchanges
	f.registry["binance"] = func() Exchange { return NewBinance() }
	f.registry["okx"] = func() Exchange { return NewOKX() }

	f.logger.Info("Exchange factory initialized with exchanges: binance, okx")
	return f
}

// GetExchange returns a cached exchange client for a user
// Nama Function: GetExchange
// Deskripsi: Mengambil atau membuat exchange client untuk user tertentu. Menggunakan cache
//   untuk avoid membuat instance baru jika sudah ada.
//
// Parameter/Value Input:
//   - userID: string — user ID untuk cache key
//   - exchangeName: string — nama exchange ("binance", "okx")
//
// Output/Return Value:
//   - Exchange: interface exchange client
//   - error: error jika exchange tidak supported
//
// Catatan: Cache key format "userID:exchangeName", contoh "uuid-123:binance"
func (f *ExchangeFactory) GetExchange(userID, exchangeName string) (Exchange, error) {
	cacheKey := userID + ":" + strings.ToLower(exchangeName)

	// Quick read lock check
	f.mu.RLock()
	if client, ok := f.clients[cacheKey]; ok {
		f.mu.RUnlock()
		return client, nil
	}
	f.mu.RUnlock()

	// Acquire write lock for creation
	f.mu.Lock()
	defer f.mu.Unlock()

	// Double-check after acquiring write lock (another goroutine might have created it)
	if client, ok := f.clients[cacheKey]; ok {
		return client, nil
	}

	// Create new client
	constructor, ok := f.registry[strings.ToLower(exchangeName)]
	if !ok {
		return nil, fmt.Errorf("unsupported exchange: %s", exchangeName)
	}

	client := constructor()
	f.clients[cacheKey] = client
	f.logger.Infof("Created new %s client for user %s", exchangeName, userID)

	return client, nil
}

// ClearUserCache removes all cached clients for a user
// Nama Function: ClearUserCache
// Deskripsi: Menghapus semua cached client untuk user tertentu. Dipanggil ketika
//   user mengganti API key agar client dibuat ulang dengan credentials baru.
//
// Parameter/Value Input:
//   - userID: string — user ID yang akan di-clear dari cache
func (f *ExchangeFactory) ClearUserCache(userID string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	prefix := userID + ":"
	for key := range f.clients {
		if strings.HasPrefix(key, prefix) {
			delete(f.clients, key)
		}
	}
	f.logger.Infof("Cleared exchange cache for user %s", userID)
}

// RegisterExchange registers a new exchange in the factory
// Nama Function: RegisterExchange
// Deskripsi: Meregistrasikan exchange baru ke factory. Berguna untuk plugin-style
//   extension atau testing dengan mock exchanges.
//
// Parameter/Value Input:
//   - name: string — nama exchange ("bybit", "kucoin", dll)
//   - constructor: func() Exchange — function yang membuat instance exchange
func (f *ExchangeFactory) RegisterExchange(name string, constructor func() Exchange) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registry[strings.ToLower(name)] = constructor
	f.logger.Infof("Registered exchange: %s", name)
}

// GetSupportedExchanges returns list of supported exchange names
// Nama Function: GetSupportedExchanges
// Deskripsi: Mengembalikan daftar nama exchange yang didukung.
//
// Output/Return Value:
//   - []string: slice nama exchange yang tersedia
func (f *ExchangeFactory) GetSupportedExchanges() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()

	exchanges := make([]string, 0, len(f.registry))
	for name := range f.registry {
		exchanges = append(exchanges, name)
	}
	return exchanges
}