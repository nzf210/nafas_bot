package scanner

import (
	"database/sql"
	"sync"

	"github.com/nzf210/nafas-bot/internal/logger"
)

// PairManager manages user-specific trading pairs and syncs them with the global Scanners
// Nama Struct: PairManager
// Deskripsi: Mengelola list pair per-user dan pair master per exchange, persisten di DB.
type PairManager struct {
	mu           sync.RWMutex
	db           *sql.DB
	logger       *logger.Logger
	scanners     map[string]*Scanner // exchangeName -> Scanner
	// userPairs menyimpan daftar pair yang di-track oleh setiap user (userID -> exchangeName -> map of symbols)
	userPairs    map[string]map[string]map[string]bool
	// masterPairs menyimpan referensi jumlah user yang memakai sebuah pair (exchangeName -> symbol -> count)
	masterPairs  map[string]map[string]int
}

// NewPairManager creates a new PairManager
// Nama Function: NewPairManager
func NewPairManager(db *sql.DB) *PairManager {
	return &PairManager{
		db:          db,
		logger:      logger.Default().WithField("module", "pair_manager"),
		scanners:    make(map[string]*Scanner),
		userPairs:   make(map[string]map[string]map[string]bool),
		masterPairs: make(map[string]map[string]int),
	}
}

// LoadFromDB loads all existing trading pairs from the database
// Nama Function: LoadFromDB
func (pm *PairManager) LoadFromDB() error {
	if pm.db == nil {
		return nil // test/mock without DB
	}

	rows, err := pm.db.Query("SELECT user_id, exchange, symbol FROM trading_pairs WHERE enabled = true")
	if err != nil {
		pm.logger.Errorf("Failed to load pairs from DB: %v", err)
		return err
	}
	defer rows.Close()

	pm.mu.Lock()
	defer pm.mu.Unlock()

	count := 0
	for rows.Next() {
		var userID, exchangeName, symbol string
		if err := rows.Scan(&userID, &exchangeName, &symbol); err != nil {
			pm.logger.Warnf("Error scanning pair row: %v", err)
			continue
		}

		if pm.userPairs[userID] == nil {
			pm.userPairs[userID] = make(map[string]map[string]bool)
		}
		if pm.userPairs[userID][exchangeName] == nil {
			pm.userPairs[userID][exchangeName] = make(map[string]bool)
		}
		pm.userPairs[userID][exchangeName][symbol] = true

		if pm.masterPairs[exchangeName] == nil {
			pm.masterPairs[exchangeName] = make(map[string]int)
		}
		pm.masterPairs[exchangeName][symbol]++
		count++
	}

	// Trigger scanners to add symbols
	for exchangeName, symbols := range pm.masterPairs {
		if scanner, exists := pm.scanners[exchangeName]; exists {
			for symbol := range symbols {
				scanner.AddSymbol(symbol)
			}
		}
	}

	pm.logger.Infof("Loaded %d pairs from database", count)
	return nil
}

// RegisterScanner mendaftarkan scanner untuk sebuah exchange
// Nama Function: RegisterScanner
func (pm *PairManager) RegisterScanner(exchangeName string, s *Scanner) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.scanners[exchangeName] = s
	
	// Tambahkan symbol yang sudah di-load dari DB ke scanner ini
	if symbols, exists := pm.masterPairs[exchangeName]; exists {
		for symbol := range symbols {
			s.AddSymbol(symbol)
		}
	}
}

// AddUserPair adds a symbol to a user's tracking list for a specific exchange.
// If it's a new symbol globally, it adds it to the exchange's scanner.
// Nama Function: AddUserPair
func (pm *PairManager) AddUserPair(userID string, exchangeName string, symbol string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.userPairs[userID] == nil {
		pm.userPairs[userID] = make(map[string]map[string]bool)
	}
	if pm.userPairs[userID][exchangeName] == nil {
		pm.userPairs[userID][exchangeName] = make(map[string]bool)
	}

	// Check if user already has this pair for this exchange
	if pm.userPairs[userID][exchangeName][symbol] {
		return
	}

	// Save to DB first if it's not the system user
	if pm.db != nil && userID != "system" {
		query := `
			INSERT INTO trading_pairs (user_id, exchange, symbol, base_asset, quote_asset, enabled)
			VALUES ($1, $2, $3, '', '', true)
			ON CONFLICT (user_id, exchange, symbol) DO UPDATE SET enabled = true
		`
		_, err := pm.db.Exec(query, userID, exchangeName, symbol)
		if err != nil {
			pm.logger.Errorf("Failed to save pair to DB for user %s: %v", userID, err)
			// Lanjut update memory agar tetep bisa dipakai (atau bisa return jika strict)
		}
	}

	// Add to user's list
	pm.userPairs[userID][exchangeName][symbol] = true

	// Ensure masterPairs map for exchange exists
	if pm.masterPairs[exchangeName] == nil {
		pm.masterPairs[exchangeName] = make(map[string]int)
	}

	// Update master list
	pm.masterPairs[exchangeName][symbol]++

	// Jika pair baru saja masuk ke master (reference == 1), tambahkan ke scanner
	if pm.masterPairs[exchangeName][symbol] == 1 {
		if scanner, exists := pm.scanners[exchangeName]; exists {
			scanner.AddSymbol(symbol)
		}
	}
}

// RemoveUserPair removes a symbol from a user's tracking list.
// If no other user is tracking it, it removes it from the exchange's scanner.
// Nama Function: RemoveUserPair
func (pm *PairManager) RemoveUserPair(userID string, exchangeName string, symbol string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.userPairs[userID] == nil || pm.userPairs[userID][exchangeName] == nil || !pm.userPairs[userID][exchangeName][symbol] {
		return // User tidak men-track pair ini
	}

	// Remove from DB first if it's not the system user
	if pm.db != nil && userID != "system" {
		query := `DELETE FROM trading_pairs WHERE user_id = $1 AND exchange = $2 AND symbol = $3`
		_, err := pm.db.Exec(query, userID, exchangeName, symbol)
		if err != nil {
			pm.logger.Errorf("Failed to delete pair from DB for user %s: %v", userID, err)
		}
	}

	// Remove from user's list
	delete(pm.userPairs[userID][exchangeName], symbol)

	// Update master list
	pm.masterPairs[exchangeName][symbol]--

	// Jika tidak ada user lain yang track pair ini, hapus dari scanner (master)
	if pm.masterPairs[exchangeName][symbol] == 0 {
		delete(pm.masterPairs[exchangeName], symbol)
		if scanner, exists := pm.scanners[exchangeName]; exists {
			scanner.RemoveSymbol(symbol)
		}
	}
}

// GetUserPairs returns the list of pairs tracked by a specific user per exchange
// Nama Function: GetUserPairs
// Return: map[exchangeName][]string
func (pm *PairManager) GetUserPairs(userID string) map[string][]string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	result := make(map[string][]string)
	if pm.userPairs[userID] != nil {
		for exchangeName, symbols := range pm.userPairs[userID] {
			var pairs []string
			for symbol := range symbols {
				pairs = append(pairs, symbol)
			}
			result[exchangeName] = pairs
		}
	}
	return result
}

// GetMasterPairs returns the list of all active pairs and how many users track them per exchange
// Nama Function: GetMasterPairs
// Return: map[exchangeName]map[symbol]count
func (pm *PairManager) GetMasterPairs() map[string]map[string]int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	result := make(map[string]map[string]int)
	for exchangeName, symbols := range pm.masterPairs {
		result[exchangeName] = make(map[string]int)
		for symbol, count := range symbols {
			result[exchangeName][symbol] = count
		}
	}
	return result
}
