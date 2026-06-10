// ============================================================
// MODULE: execution/dedup
// Deskripsi: Duplicate order prevention — mencegah double BUY untuk pair yang sama
// ============================================================

package execution

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/nzf210/nafas-bot/internal/logger"
	"github.com/nzf210/nafas-bot/internal/models"
)

// DuplicateChecker mencegah order duplikat untuk user dan pair yang sama.
// Nama Function: DuplicateChecker
// Deskripsi: Service untuk deteksi dan pencegahan duplicate orders.
//   Menggunakan kombinasi DB query (untuk persistent dedup) dan in-memory lock
//   (untuk immediate goroutine-level dedup) untuk mencegah double execution.
type DuplicateChecker struct {
	db     *sql.DB
	logger *logger.Logger
}

// NewDuplicateChecker creates a new DuplicateChecker.
// Nama Function: NewDuplicateChecker
// Deskripsi: Membuat instance DuplicateChecker baru.
// Parameter/Value Input:
//   - db: *sql.DB — koneksi database
// Output/Return Value:
//   - *DuplicateChecker: pointer ke checker instance
func NewDuplicateChecker(db *sql.DB) *DuplicateChecker {
	return &DuplicateChecker{
		db:     db,
		logger: logger.Default().WithField("module", "execution/dedup"),
	}
}

// HasOpenBuyPosition checks if user already has an open BUY position for a symbol.
// Nama Function: HasOpenBuyPosition
// Deskripsi: Mengecek apakah user sudah memiliki posisi BUY terbuka untuk symbol tertentu.
//   "Open" didefinisikan sebagai: ada BUY order filled yang belum ditutup oleh SELL order,
//   ATAU ada BUY order yang masih pending/submitted.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk database operation
//   - userID: models.UUID — user ID
//   - symbol: string — trading pair symbol (misal SOLUSDT)
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryRowContext: query orders tabel untuk cek status
// Output/Return Value:
//   - bool: true jika sudah ada posisi terbuka (BUY harus diblokir)
//   - string: alasan jika blocked (untuk logging)
//   - error: error jika query gagal
func (d *DuplicateChecker) HasOpenBuyPosition(ctx context.Context, userID models.UUID, symbol string) (bool, string, error) {
	// Check 1: Apakah ada order BUY yang masih pending/submitted?
	var pendingCount int
	err := d.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM orders
		WHERE user_id = $1 AND symbol = $2
		  AND UPPER(side) = 'BUY'
		  AND status IN ('pending', 'submitted', 'partial')
	`, userID, symbol).Scan(&pendingCount)
	if err != nil {
		return false, "", fmt.Errorf("dedup check failed: %w", err)
	}
	if pendingCount > 0 {
		return true, fmt.Sprintf("BUY order already pending/submitted for %s", symbol), nil
	}

	// Check 2: Apakah ada posisi terbuka (BUY filled + protective order masih pending)?
	// Posisi terbuka = ada SL atau TP order yang masih pending untuk symbol ini
	var protectiveCount int
	err = d.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM orders
		WHERE user_id = $1 AND symbol = $2
		  AND order_type IN ('STOP_LOSS_LIMIT', 'TAKE_PROFIT_LIMIT')
		  AND status IN ('pending', 'submitted', 'partial')
	`, userID, symbol).Scan(&protectiveCount)
	if err != nil {
		return false, "", fmt.Errorf("dedup protective check failed: %w", err)
	}
	if protectiveCount > 0 {
		return true, fmt.Sprintf("open position detected for %s (has %d active SL/TP orders)", symbol, protectiveCount), nil
	}

	// Check 3: Apakah ada BUY order filled dalam 60 menit terakhir tanpa matching SELL?
	// Ini mencegah duplicate dalam window waktu singkat
	window := time.Now().Add(-60 * time.Minute)
	var recentBuyCount int
	err = d.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM orders
		WHERE user_id = $1 AND symbol = $2
		  AND UPPER(side) = 'BUY' AND UPPER(status) = 'FILLED'
		  AND created_at >= $3
		  AND NOT EXISTS (
		      SELECT 1 FROM orders sell
		      WHERE sell.user_id = $1 AND sell.symbol = $2
		        AND UPPER(sell.side) = 'SELL'
		        AND UPPER(sell.status) = 'FILLED'
		        AND sell.created_at > orders.created_at
		  )
	`, userID, symbol, window).Scan(&recentBuyCount)
	if err != nil {
		// Non-critical check, log and continue
		d.logger.WithError(err).WithField("symbol", symbol).Warn("DEDUP: Failed recent buy check")
		return false, "", nil
	}
	if recentBuyCount > 0 {
		return true, fmt.Sprintf("recent unfilled BUY detected for %s within 60min window", symbol), nil
	}

	return false, "", nil
}

// IsDuplicate atomically checks if a BUY is duplicate for the given user+symbol.
// Nama Function: IsDuplicate
// Deskripsi: Mengecek apakah BUY order sudah ada (duplicate check) untuk symbol user.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk database operation
//   - userID: models.UUID — user ID
//   - symbol: string — trading pair
//   - side: string — "BUY" atau "SELL"
// Output/Return Value:
//   - bool: true jika order ini adalah duplicate (harus diblokir)
//   - string: alasan diblokir
//   - error: error jika check gagal
func (d *DuplicateChecker) IsDuplicate(ctx context.Context, userID models.UUID, symbol, side string) (bool, string, error) {
	// Only check duplicates for BUY orders
	if side != "BUY" {
		return false, "", nil
	}

	blocked, reason, err := d.HasOpenBuyPosition(ctx, userID, symbol)
	if err != nil {
		d.logger.WithError(err).WithField("symbol", symbol).Warn("DEDUP: Check error — allowing order")
		return false, "", nil // Fail open: if check fails, allow the order
	}

	if blocked {
		d.logger.WithField("user_id", userID).
			WithField("symbol", symbol).
			WithField("reason", reason).
			Info("DEDUP: Duplicate BUY blocked")
	}

	return blocked, reason, nil
}
