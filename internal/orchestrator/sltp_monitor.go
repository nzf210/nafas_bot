// ============================================================
// MODULE: orchestrator/sltp_monitor
// Deskripsi: SL/TP Monitoring Service — background job untuk memantau
//            dan mengelola Stop Loss / Take Profit orders
// ============================================================

package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/nzf210/nafas-bot/internal/exchange"
	"github.com/nzf210/nafas-bot/internal/models"
	"github.com/shopspring/decimal"
)

// SLTPMonitor adalah background service yang memantau semua SL/TP orders aktif.
// Berjalan secara periodik dan mengambil tindakan ketika:
//   - SL terpicu → cancel TP yang masih pending (OCO behavior)
//   - TP terpicu → cancel SL yang masih pending (OCO behavior)
//   - Order stale / tidak aktif → deteksi dan log
//
// Nama Function: SLTPMonitor
// Deskripsi: Background service untuk monitoring SL/TP orders.
//   Menggunakan polling interval untuk check status semua active protective orders.
type SLTPMonitor struct {
	orchestrator *Orchestrator
	interval     time.Duration
	stopCh       chan struct{}
	wg           sync.WaitGroup
}

// NewSLTPMonitor creates a new SL/TP monitoring service.
// Nama Function: NewSLTPMonitor
// Deskripsi: Membuat instance SLTPMonitor baru.
// Parameter/Value Input:
//   - orch: *Orchestrator — orchestrator parent (akses ke DB dan exchange)
//   - interval: time.Duration — polling interval (misal 30 detik)
//
// Output/Return Value:
//   - *SLTPMonitor: pointer ke monitor instance
func NewSLTPMonitor(orch *Orchestrator, interval time.Duration) *SLTPMonitor {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &SLTPMonitor{
		orchestrator: orch,
		interval:     interval,
		stopCh:       make(chan struct{}),
	}
}

// Start starts the background SL/TP monitoring goroutine.
// Nama Function: Start
// Deskripsi: Memulai background goroutine untuk monitoring SL/TP orders.
//
//	Berjalan sampai Stop() dipanggil atau context cancelled.
//
// Parameter/Value Input:
//   - ctx: context.Context — context untuk cancellation
//
// Output/Return Value:
//   - Tidak ada return value langsung
func (m *SLTPMonitor) Start(ctx context.Context) {
	m.orchestrator.logger.Infof("SL/TP Monitor: Starting with interval %v", m.interval)
	m.wg.Add(1)
	go m.run(ctx)
}

// Stop gracefully stops the SL/TP monitor.
// Nama Function: Stop
// Deskripsi: Menghentikan SL/TP monitor secara graceful.
// Output/Return Value:
//   - Tidak ada return value langsung
func (m *SLTPMonitor) Stop() {
	m.orchestrator.logger.Info("SL/TP Monitor: Stopping...")
	close(m.stopCh)
	m.wg.Wait()
	m.orchestrator.logger.Info("SL/TP Monitor: Stopped")
}

// run is the main background loop.
func (m *SLTPMonitor) run(ctx context.Context) {
	defer m.wg.Done()

	// Run immediately on start
	m.checkSLTPOrders(ctx)

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.orchestrator.logger.Info("SL/TP Monitor: Context cancelled")
			return
		case <-m.stopCh:
			m.orchestrator.logger.Info("SL/TP Monitor: Stop signal received")
			return
		case <-ticker.C:
			m.checkSLTPOrders(ctx)
		}
	}
}

// activeSLTP represents an active SL or TP order from the database.
type activeSLTP struct {
	ID              models.UUID
	UserID          models.UUID
	Exchange        string
	Symbol          string
	OrderType       string // STOP_LOSS_LIMIT or TAKE_PROFIT_LIMIT
	ExchangeOrderID string
	Price           decimal.Decimal
	Quantity        decimal.Decimal
	Status          string
	CreatedAt       time.Time
}

// checkSLTPOrders polls the DB for active SL/TP orders and checks their status.
// Nama Function: checkSLTPOrders
// Deskripsi: Memeriksa semua SL/TP orders yang aktif dan meng-update statusnya.
//   Jika SL atau TP terpicu (filled), otomatis cancel counterpart orders (OCO).
//   Juga mendeteksi orders stale yang belum dibuat di exchange.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk operasi DB dan exchange
//
// Output/Return Value:
//   - Tidak ada return value langsung
func (m *SLTPMonitor) checkSLTPOrders(ctx context.Context) {
	o := m.orchestrator

	// Query semua active SL/TP orders dari DB
	rows, err := o.db.QueryContext(ctx, `
		SELECT id, user_id, exchange, symbol, order_type, 
		       COALESCE(exchange_order_id, ''), price, quantity, status, created_at
		FROM orders
		WHERE order_type IN ('STOP_LOSS_LIMIT', 'TAKE_PROFIT_LIMIT')
		  AND status IN ('pending', 'submitted', 'partial')
		ORDER BY user_id, symbol, created_at
	`)
	if err != nil {
		o.logger.WithError(err).Warn("SL/TP Monitor: Failed to query active orders")
		return
	}
	defer rows.Close()

	var activeOrders []activeSLTP
	for rows.Next() {
		var order activeSLTP
		if err := rows.Scan(
			&order.ID, &order.UserID, &order.Exchange, &order.Symbol,
			&order.OrderType, &order.ExchangeOrderID, &order.Price,
			&order.Quantity, &order.Status, &order.CreatedAt,
		); err != nil {
			o.logger.WithError(err).Warn("SL/TP Monitor: Failed to scan order row")
			continue
		}
		activeOrders = append(activeOrders, order)
	}

	if len(activeOrders) == 0 {
		return
	}

	o.logger.Infof("SL/TP Monitor: Checking %d active SL/TP orders", len(activeOrders))

	// Process each order
	var filledOrders []filledProtectiveOrder
	var staleOrders []activeSLTP

	for _, order := range activeOrders {
		// Skip orders without exchange_order_id (not yet submitted to exchange)
		if order.ExchangeOrderID == "" {
			// Check if order is stale (created > 5 minutes ago without being submitted)
			if time.Since(order.CreatedAt) > 5*time.Minute {
				staleOrders = append(staleOrders, order)
			}
			continue
		}

		// Get user context for API credentials
		user := &models.User{ID: order.UserID}
		userCtx, err := GetUserContext(ctx, o.db, user)
		if err != nil || userCtx == nil {
			o.logger.WithError(err).WithField("user_id", order.UserID).
				Warn("SL/TP Monitor: Failed to get user context")
			continue
		}

		// Get per-user exchange client
		userExch := o.getUserExchangeByName(order.UserID.String(), order.Exchange)
		if userExch == nil {
			o.logger.WithField("exchange", order.Exchange).Warn("SL/TP Monitor: Exchange not available")
			continue
		}

		// Poll order status from exchange
		updatedOrder, err := userExch.GetOrderStatus(ctx, userCtx.APIKey, userCtx.APISecret, userCtx.Passphrase,
			order.ExchangeOrderID, order.Symbol)
		if err != nil {
			o.logger.WithError(err).
				WithField("order_id", order.ID).
				WithField("exchange_order_id", order.ExchangeOrderID).
				Warn("SL/TP Monitor: Failed to get order status from exchange")
			continue
		}

		if updatedOrder == nil || updatedOrder.Status == order.Status {
			continue // No change
		}

		// Update order status in DB
		_, err = o.db.ExecContext(ctx, `
			UPDATE orders 
			SET status = $1, executed_quantity = $2, price = $3, updated_at = $4
			WHERE id = $5
		`, updatedOrder.Status, updatedOrder.ExecutedQuantity, updatedOrder.Price, time.Now(), order.ID)
		if err != nil {
			o.logger.WithError(err).WithField("order_id", order.ID).
				Warn("SL/TP Monitor: Failed to update order status in DB")
			continue
		}

		o.logger.WithField("order_id", order.ID).
			WithField("symbol", order.Symbol).
			WithField("type", order.OrderType).
			WithField("old_status", order.Status).
			WithField("new_status", updatedOrder.Status).
			Info("SL/TP Monitor: Order status updated")

		// Track filled protective orders for OCO handling
		if updatedOrder.Status == "filled" {
			filledOrders = append(filledOrders, filledProtectiveOrder{
				UserID:    order.UserID,
				Symbol:    order.Symbol,
				OrderType: order.OrderType,
			})

			o.logger.WithField("symbol", order.Symbol).
				WithField("type", order.OrderType).
				WithField("price", updatedOrder.Price.String()).
				Info("SL/TP Monitor: Protective order filled — triggering OCO cancel")
		}
	}

	// Process OCO cancellations for filled orders
	if len(filledOrders) > 0 {
		o.cancelCounterpartOrders(ctx, filledOrders)
	}

	// Handle stale orders
	if len(staleOrders) > 0 {
		m.handleStaleOrders(ctx, staleOrders)
	}
}

// handleStaleOrders logs and optionally cancels stale SL/TP orders.
// Nama Function: handleStaleOrders
// Deskripsi: Menangani SL/TP orders yang stale (belum memiliki exchange_order_id
//   setelah 5 menit). Order ini mungkin gagal dibuat di exchange.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk operasi
//   - staleOrders: []activeSLTP — list orders stale
//
// Output/Return Value:
//   - Tidak ada return value langsung
func (m *SLTPMonitor) handleStaleOrders(ctx context.Context, staleOrders []activeSLTP) {
	o := m.orchestrator

	for _, order := range staleOrders {
		o.logger.WithField("order_id", order.ID).
			WithField("symbol", order.Symbol).
			WithField("type", order.OrderType).
			WithField("age", time.Since(order.CreatedAt).Round(time.Second).String()).
			Warn("SL/TP Monitor: Stale order detected (no exchange_order_id after 5min)")

		// Mark stale orders as failed in DB so they don't accumulate
		_, err := o.db.ExecContext(ctx, `
			UPDATE orders 
			SET status = 'failed', updated_at = $1
			WHERE id = $2 AND exchange_order_id IS NULL
			  AND status IN ('pending', 'submitted')
		`, time.Now(), order.ID)
		if err != nil {
			o.logger.WithError(err).WithField("order_id", order.ID).
				Warn("SL/TP Monitor: Failed to mark stale order as failed")
		} else {
			o.logger.WithField("order_id", order.ID).
				WithField("symbol", order.Symbol).
				Info("SL/TP Monitor: Stale order marked as failed")
		}
	}
}

// getUserExchangeByName returns the exchange client by name string.
// Nama Function: getUserExchangeByName
// Deskripsi: Mengambil exchange client berdasarkan nama exchange.
//   Wrapper untuk getUserExchange yang menggunakan string exchange name langsung.
// Parameter/Value Input:
//   - userID: string — user ID
//   - exchangeName: string — nama exchange
//
// Output/Return Value:
//   - exchange.Exchange: exchange client, atau nil jika tidak tersedia
func (o *Orchestrator) getUserExchangeByName(userID, exchangeName string) exchange.Exchange {
	return o.getUserExchange(userID, exchangeName)
}

// GetSLTPSummary returns a summary of active SL/TP orders for a user.
// Nama Function: GetSLTPSummary
// Deskripsi: Mengambil ringkasan SL/TP orders aktif untuk satu user.
//   Berguna untuk Telegram bot reporting.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk database operation
//   - userID: models.UUID — user ID
//
// Output/Return Value:
//   - map[string]SLTPPositionSummary: map symbol ke summary
//   - error: error jika query gagal
func (o *Orchestrator) GetSLTPSummary(ctx context.Context, userID models.UUID) (map[string]*SLTPPositionSummary, error) {
	rows, err := o.db.QueryContext(ctx, `
		SELECT symbol, order_type, price, quantity, status, created_at
		FROM orders
		WHERE user_id = $1
		  AND order_type IN ('STOP_LOSS_LIMIT', 'TAKE_PROFIT_LIMIT')
		  AND status IN ('pending', 'submitted', 'partial')
		ORDER BY symbol, order_type
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query SLTP summary: %w", err)
	}
	defer rows.Close()

	summary := make(map[string]*SLTPPositionSummary)
	for rows.Next() {
		var symbol, orderType, status string
		var price, qty decimal.Decimal
		var createdAt time.Time

		if err := rows.Scan(&symbol, &orderType, &price, &qty, &status, &createdAt); err != nil {
			continue
		}

		if _, exists := summary[symbol]; !exists {
			summary[symbol] = &SLTPPositionSummary{Symbol: symbol}
		}

		pos := summary[symbol]
		switch orderType {
		case "STOP_LOSS_LIMIT":
			pos.StopLossPrice = price
			pos.StopLossStatus = status
		case "TAKE_PROFIT_LIMIT":
			pos.TakeProfitPrice = price
			pos.TakeProfitStatus = status
		}
	}

	return summary, rows.Err()
}

// SLTPPositionSummary summarizes SL/TP for a single symbol position.
// Nama Function: SLTPPositionSummary
// Deskripsi: Struct ringkasan SL/TP untuk satu posisi trading.
type SLTPPositionSummary struct {
	Symbol          string
	StopLossPrice   decimal.Decimal
	StopLossStatus  string
	TakeProfitPrice decimal.Decimal
	TakeProfitStatus string
}
