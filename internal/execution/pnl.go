// ============================================================
// MODULE: execution/pnl
// Deskripsi: Realized P&L tracking per closed position
// ============================================================

package execution

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/nzf210/nafas-bot/internal/logger"
	"github.com/nzf210/nafas-bot/internal/models"
	"github.com/shopspring/decimal"
)

// RealizedPnL represents the realized profit or loss for a closed position.
// Nama Function: RealizedPnL
// Deskripsi: Struct yang menyimpan informasi realized P&L untuk satu posisi yang ditutup.
// Parameter/Value Input:
//   - UserID: models.UUID — user yang trade
//   - Symbol: string — pair trading (misal SOLUSDT)
//   - BuyOrderID: models.UUID — ID order beli
//   - SellOrderID: models.UUID — ID order jual
//   - EntryPrice: decimal.Decimal — harga rata-rata beli
//   - ExitPrice: decimal.Decimal — harga jual
//   - Quantity: decimal.Decimal — jumlah yang ditradingkan
//   - GrossPnL: decimal.Decimal — P&L kotor sebelum fee
//   - NetPnL: decimal.Decimal — P&L bersih setelah fee
//   - PnLPercent: decimal.Decimal — P&L dalam persen
//   - IsProfit: bool — apakah trade menghasilkan profit
//   - ClosedAt: time.Time — timestamp posisi ditutup
// Output/Return Value:
//   - RealizedPnL: struct P&L yang terealisasi
type RealizedPnL struct {
	ID          models.UUID     `json:"id"`
	UserID      models.UUID     `json:"user_id"`
	Symbol      string          `json:"symbol"`
	BuyOrderID  models.UUID     `json:"buy_order_id"`
	SellOrderID models.UUID     `json:"sell_order_id"`
	EntryPrice  decimal.Decimal `json:"entry_price"`
	ExitPrice   decimal.Decimal `json:"exit_price"`
	Quantity    decimal.Decimal `json:"quantity"`
	GrossPnL    decimal.Decimal `json:"gross_pnl"`
	NetPnL      decimal.Decimal `json:"net_pnl"`
	PnLPercent  decimal.Decimal `json:"pnl_percent"`
	IsProfit    bool            `json:"is_profit"`
	ClosedAt    time.Time       `json:"closed_at"`
}

// PnLTracker tracks realized P&L for closed positions.
// Nama Function: PnLTracker
// Deskripsi: Service untuk tracking Realized P&L setiap posisi yang ditutup.
//   Menyimpan P&L ke tabel realized_pnl dan menyediakan query untuk reporting.
type PnLTracker struct {
	db     *sql.DB
	logger *logger.Logger
}

// NewPnLTracker creates a new P&L tracker.
// Nama Function: NewPnLTracker
// Deskripsi: Membuat instance PnLTracker baru.
// Parameter/Value Input:
//   - db: *sql.DB — koneksi database
// Output/Return Value:
//   - *PnLTracker: pointer ke tracker instance
func NewPnLTracker(db *sql.DB) *PnLTracker {
	return &PnLTracker{
		db:     db,
		logger: logger.Default().WithField("module", "execution/pnl"),
	}
}

// RecordClosedPosition calculates and records Realized P&L when a position is closed.
// Nama Function: RecordClosedPosition
// Deskripsi: Menghitung dan menyimpan Realized P&L saat posisi ditutup (SELL order filled).
//   Mencari BUY order terkait untuk menghitung entry price rata-rata.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk database operation
//   - userID: models.UUID — user ID
//   - sellOrder: *models.Order — SELL order yang baru filled
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryRowContext: mencari average buy price untuk symbol
//   - db.ExecContext: menyimpan realized P&L ke tabel
// Output/Return Value:
//   - *RealizedPnL: realized P&L yang sudah dihitung dan disimpan
//   - error: error jika operasi gagal
func (t *PnLTracker) RecordClosedPosition(ctx context.Context, userID models.UUID, sellOrder *models.Order) (*RealizedPnL, error) {
	if sellOrder == nil {
		return nil, fmt.Errorf("sell order cannot be nil")
	}
	if sellOrder.ExecutedQuantity.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("sell order has zero executed quantity")
	}

	// Find the most recent filled BUY order for this symbol (FIFO matching)
	var buyOrderID models.UUID
	var avgBuyPrice decimal.Decimal
	var buyQty decimal.Decimal

	err := t.db.QueryRowContext(ctx, `
		SELECT id, price, executed_quantity
		FROM orders
		WHERE user_id = $1 AND symbol = $2
		  AND UPPER(side) = 'BUY' AND UPPER(status) = 'FILLED'
		ORDER BY created_at DESC
		LIMIT 1
	`, userID, sellOrder.Symbol).Scan(&buyOrderID, &avgBuyPrice, &buyQty)

	if err == sql.ErrNoRows {
		// No matching buy order found — could be first sell or data gap
		t.logger.WithField("symbol", sellOrder.Symbol).
			WithField("user_id", userID).
			Warn("PNL: No matching BUY order found for SELL — cannot compute P&L")
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to find buy order: %w", err)
	}

	// If we found avg buy price via query, compute P&L
	if avgBuyPrice.LessThanOrEqual(decimal.Zero) {
		// Fallback: compute weighted avg from all BUY orders for this symbol
		err = t.db.QueryRowContext(ctx, `
			SELECT
				SUM(price * executed_quantity) / NULLIF(SUM(executed_quantity), 0) AS avg_price
			FROM orders
			WHERE user_id = $1 AND symbol = $2
			  AND UPPER(side) = 'BUY' AND UPPER(status) = 'FILLED'
		`, userID, sellOrder.Symbol).Scan(&avgBuyPrice)
		if err != nil || avgBuyPrice.LessThanOrEqual(decimal.Zero) {
			t.logger.WithField("symbol", sellOrder.Symbol).Warn("PNL: Cannot compute avg buy price")
			return nil, nil
		}
	}

	// Suppress unused variable warning — buyQty captured for future FIFO expansion
	_ = buyQty

	exitPrice := sellOrder.Price
	qty := sellOrder.ExecutedQuantity

	// Gross P&L = (exitPrice - entryPrice) * quantity
	grossPnL := exitPrice.Sub(avgBuyPrice).Mul(qty)

	// Estimate fee: assume 0.1% trading fee on both sides
	feeRate := decimal.NewFromFloat(0.001)
	totalFee := exitPrice.Mul(qty).Mul(feeRate).Add(avgBuyPrice.Mul(qty).Mul(feeRate))

	netPnL := grossPnL.Sub(totalFee)

	// P&L percent relative to entry value
	entryValue := avgBuyPrice.Mul(qty)
	var pnlPercent decimal.Decimal
	if entryValue.GreaterThan(decimal.Zero) {
		pnlPercent = netPnL.Div(entryValue).Mul(decimal.NewFromFloat(100))
	}

	pnl := &RealizedPnL{
		UserID:      userID,
		Symbol:      sellOrder.Symbol,
		BuyOrderID:  buyOrderID,
		SellOrderID: sellOrder.ID,
		EntryPrice:  avgBuyPrice,
		ExitPrice:   exitPrice,
		Quantity:    qty,
		GrossPnL:    grossPnL,
		NetPnL:      netPnL,
		PnLPercent:  pnlPercent,
		IsProfit:    netPnL.GreaterThan(decimal.Zero),
		ClosedAt:    time.Now(),
	}

	// Auto-ensure table exists and insert P&L record
	// Uses INSERT ... ON CONFLICT DO NOTHING to handle idempotency
	_, err = t.db.ExecContext(ctx, `
		INSERT INTO realized_pnl
			(user_id, symbol, buy_order_id, sell_order_id, entry_price, exit_price,
			 quantity, gross_pnl, net_pnl, pnl_percent, is_profit, closed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (sell_order_id) DO NOTHING
	`,
		pnl.UserID, pnl.Symbol, pnl.BuyOrderID, pnl.SellOrderID,
		pnl.EntryPrice, pnl.ExitPrice, pnl.Quantity,
		pnl.GrossPnL, pnl.NetPnL, pnl.PnLPercent, pnl.IsProfit, pnl.ClosedAt,
	)
	if err != nil {
		t.logger.WithError(err).WithField("symbol", sellOrder.Symbol).Warn("PNL: Failed to persist realized P&L (table may not exist yet — will create)")
		// Try to create table and retry
		if createErr := t.ensureTable(ctx); createErr == nil {
			_, err = t.db.ExecContext(ctx, `
				INSERT INTO realized_pnl
					(user_id, symbol, buy_order_id, sell_order_id, entry_price, exit_price,
					 quantity, gross_pnl, net_pnl, pnl_percent, is_profit, closed_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
				ON CONFLICT (sell_order_id) DO NOTHING
			`,
				pnl.UserID, pnl.Symbol, pnl.BuyOrderID, pnl.SellOrderID,
				pnl.EntryPrice, pnl.ExitPrice, pnl.Quantity,
				pnl.GrossPnL, pnl.NetPnL, pnl.PnLPercent, pnl.IsProfit, pnl.ClosedAt,
			)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to persist realized P&L: %w", err)
		}
	}

	t.logger.WithField("symbol", sellOrder.Symbol).
		WithField("net_pnl", netPnL.StringFixed(8)).
		WithField("pnl_percent", pnlPercent.StringFixed(2)).
		WithField("is_profit", pnl.IsProfit).
		Info("PNL: Realized P&L recorded")

	return pnl, nil
}

// GetUserRealizedPnL retrieves realized P&L summary for a user.
// Nama Function: GetUserRealizedPnL
// Deskripsi: Mengambil summary realized P&L user untuk periode tertentu.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk database operation
//   - userID: models.UUID — user ID
//   - since: time.Time — mulai dari tanggal ini
// Output/Return Value:
//   - []RealizedPnL: list realized P&L
//   - error: error jika query gagal
func (t *PnLTracker) GetUserRealizedPnL(ctx context.Context, userID models.UUID, since time.Time) ([]RealizedPnL, error) {
	rows, err := t.db.QueryContext(ctx, `
		SELECT user_id, symbol, buy_order_id, sell_order_id,
			   entry_price, exit_price, quantity, gross_pnl, net_pnl,
			   pnl_percent, is_profit, closed_at
		FROM realized_pnl
		WHERE user_id = $1 AND closed_at >= $2
		ORDER BY closed_at DESC
	`, userID, since)
	if err != nil {
		return nil, fmt.Errorf("failed to query realized P&L: %w", err)
	}
	defer rows.Close()

	var results []RealizedPnL
	for rows.Next() {
		var p RealizedPnL
		if err := rows.Scan(
			&p.UserID, &p.Symbol, &p.BuyOrderID, &p.SellOrderID,
			&p.EntryPrice, &p.ExitPrice, &p.Quantity, &p.GrossPnL, &p.NetPnL,
			&p.PnLPercent, &p.IsProfit, &p.ClosedAt,
		); err != nil {
			continue
		}
		results = append(results, p)
	}
	return results, rows.Err()
}

// GetTotalRealizedPnL returns aggregate P&L for a user since a date.
// Nama Function: GetTotalRealizedPnL
// Deskripsi: Menghitung total Realized P&L user sejak tanggal tertentu.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk database operation
//   - userID: models.UUID — user ID
//   - since: time.Time — tanggal mulai (biasanya awal hari)
// Output/Return Value:
//   - decimal.Decimal: total net P&L
//   - error: error jika query gagal
func (t *PnLTracker) GetTotalRealizedPnL(ctx context.Context, userID models.UUID, since time.Time) (decimal.Decimal, error) {
	var total decimal.Decimal
	err := t.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(net_pnl), 0)
		FROM realized_pnl
		WHERE user_id = $1 AND closed_at >= $2
	`, userID, since).Scan(&total)
	if err != nil {
		return decimal.Zero, fmt.Errorf("failed to sum realized P&L: %w", err)
	}
	return total, nil
}

// ensureTable creates the realized_pnl table if it doesn't exist.
// This is a safety net — the table should ideally be created via DB migration.
func (t *PnLTracker) ensureTable(ctx context.Context) error {
	_, err := t.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS realized_pnl (
			id              UUID DEFAULT gen_random_uuid() PRIMARY KEY,
			user_id         UUID NOT NULL,
			symbol          VARCHAR(20) NOT NULL,
			buy_order_id    UUID NOT NULL,
			sell_order_id   UUID NOT NULL UNIQUE,
			entry_price     DECIMAL(30,10) NOT NULL,
			exit_price      DECIMAL(30,10) NOT NULL,
			quantity        DECIMAL(30,10) NOT NULL,
			gross_pnl       DECIMAL(30,10) NOT NULL,
			net_pnl         DECIMAL(30,10) NOT NULL,
			pnl_percent     DECIMAL(10,4) NOT NULL,
			is_profit       BOOLEAN NOT NULL DEFAULT false,
			closed_at       TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
		)
	`)
	return err
}
