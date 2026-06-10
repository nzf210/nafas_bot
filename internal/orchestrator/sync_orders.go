package orchestrator

import (
	"context"
	"time"

	"github.com/nzf210/nafas-bot/internal/execution"
	"github.com/nzf210/nafas-bot/internal/models"
	"github.com/shopspring/decimal"
)

// SyncPendingOrders checks all pending, submitted, or partial orders
// and syncs their status with the exchange.
// Nama Function: SyncPendingOrders
// Deskripsi: Melakukan sinkronisasi status semua order yang masih pending/submitted/partial
//   dengan exchange. Juga meng-update price (harga eksekusi) dan menjalankan logika
//   OCO sederhana: jika SL/TP terpicu (filled), cancel counterpart orders untuk symbol
//   yang sama agar tidak terjadi double execution.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk operasi database dan exchange
// Function yang Dipanggil/Dikonsumsi:
//   - GetUserContext: ambil API key user untuk query exchange
//   - exchange.GetOrderStatus: polling status order dari exchange
//   - exchange.CancelOrder: cancel counterpart order (OCO behavior)
//   - db.ExecContext: update status, price, executed_quantity di DB
// Output/Return Value:
//   - Tidak ada return value langsung
func (o *Orchestrator) SyncPendingOrders(ctx context.Context) {
	o.logger.Info("Starting pending orders sync")

	// Get all orders that need sync (include order_type for OCO logic)
	rows, err := o.db.QueryContext(ctx, `
		SELECT id, user_id, exchange, exchange_order_id, symbol, order_type, UPPER(side)
		FROM orders 
		WHERE status IN ('pending', 'submitted', 'partial')
		AND exchange_order_id IS NOT NULL
	`)
	if err != nil {
		o.logger.WithError(err).Error("Failed to query pending orders")
		return
	}
	defer rows.Close()

	type pendingOrder struct {
		ID              models.UUID
		UserID          models.UUID
		Exchange        string
		ExchangeOrderID string
		Symbol          string
		OrderType       string
		Side            string
	}
	var orders []pendingOrder
	for rows.Next() {
		var po pendingOrder
		if err := rows.Scan(&po.ID, &po.UserID, &po.Exchange, &po.ExchangeOrderID, &po.Symbol, &po.OrderType, &po.Side); err != nil {
			o.logger.WithError(err).Warn("Failed to scan pending order row")
			continue
		}
		orders = append(orders, po)
	}

	if len(orders) == 0 {
		return
	}

	o.logger.Infof("Found %d pending orders to sync", len(orders))

	// Track which symbols had an SL or TP filled (for OCO cancel)
	var filledProtectives []filledProtectiveOrder

	for _, order := range orders {
		// Need UserContext to get API keys
		user := &models.User{ID: order.UserID}
		userCtx, err := GetUserContext(ctx, o.db, user)
		if err != nil || userCtx == nil {
			o.logger.WithError(err).WithField("user_id", order.UserID).Warn("Failed to get user context for sync")
			continue
		}

		// Sync with exchange using per-user exchange client (multi-exchange support)
		userExch := o.getUserExchange(order.UserID.String(), userCtx.Exchange)
		updatedOrder, err := userExch.GetOrderStatus(ctx, userCtx.APIKey, userCtx.APISecret, userCtx.Passphrase, order.ExchangeOrderID, order.Symbol)
		if err != nil {
			o.logger.WithError(err).WithField("order_id", order.ID).Warn("Failed to fetch order status from exchange")
			continue
		}

		if updatedOrder != nil && updatedOrder.Status != "" {
			// Fix #9: Update price alongside status and executed_quantity
			price := updatedOrder.Price
			if price.LessThanOrEqual(decimal.Zero) {
				// Fallback: compute from executed qty if exchange didn't return price
				price = decimal.Zero
			}

			_, err = o.db.ExecContext(ctx, `
				UPDATE orders 
				SET status = $1, executed_quantity = $2, price = $3, updated_at = $4
				WHERE id = $5
			`, updatedOrder.Status, updatedOrder.ExecutedQuantity, price, time.Now(), order.ID)

			if err != nil {
				o.logger.WithError(err).WithField("order_id", order.ID).Warn("Failed to update order status in DB")
			} else {
				o.logger.WithField("order_id", order.ID).
					WithField("new_status", updatedOrder.Status).
					WithField("price", price.String()).
					Info("Successfully synced order status")
			}

			// OCO logic: if a protective order (SL/TP) just filled, mark for counterpart cancel
			if updatedOrder.Status == "filled" &&
				(order.OrderType == "STOP_LOSS_LIMIT" || order.OrderType == "TAKE_PROFIT_LIMIT") {
				filledProtectives = append(filledProtectives, filledProtectiveOrder{
					UserID:    order.UserID,
					Symbol:    order.Symbol,
					OrderType: order.OrderType,
				})
			}

			// P&L TRACKING: Record realized P&L when a SELL order fills
			if updatedOrder.Status == "filled" && order.Side == "SELL" {
				pnlCtx, pnlCancel := context.WithTimeout(ctx, 5*time.Second)
				pnlTracker := execution.NewPnLTracker(o.db)
				_, pnlErr := pnlTracker.RecordClosedPosition(pnlCtx, order.UserID, updatedOrder)
				if pnlErr != nil {
					o.logger.WithError(pnlErr).WithField("symbol", order.Symbol).
						Warn("PNL: Failed to record P&L for filled SELL order")
				}
				pnlCancel()
			}
		}
	}

	// OCO: Cancel counterpart orders for symbols where SL or TP was filled
	o.cancelCounterpartOrders(ctx, filledProtectives)
}

// filledProtectiveOrder represents a SL/TP order that just transitioned to filled.
type filledProtectiveOrder struct {
	UserID    models.UUID
	Symbol    string
	OrderType string
}

// cancelCounterpartOrders cancels the counterpart protective orders when one side triggers.
// Nama Function: cancelCounterpartOrders
// Deskripsi: Implementasi OCO sederhana. Jika SL terpicu → cancel semua pending TP untuk
//   symbol yang sama (dan sebaliknya). Mencegah double execution.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk operasi
//   - filled: list of SL/TP orders yang baru saja filled
// Function yang Dipanggil/Dikonsumsi:
//   - exchange.CancelOrder: cancel order di exchange
//   - db.ExecContext: update status order di DB menjadi 'cancelled'
// Output/Return Value:
//   - Tidak ada return value langsung
func (o *Orchestrator) cancelCounterpartOrders(ctx context.Context, filled []filledProtectiveOrder) {
	if len(filled) == 0 {
		return
	}

	for _, f := range filled {
		// Determine counterpart type
		var counterType string
		if f.OrderType == "STOP_LOSS_LIMIT" {
			counterType = "TAKE_PROFIT_LIMIT"
		} else {
			counterType = "STOP_LOSS_LIMIT"
		}

		// Find counterpart orders still pending
		rows, err := o.db.QueryContext(ctx, `
			SELECT id, exchange_order_id FROM orders
			WHERE user_id = $1 AND symbol = $2 AND order_type = $3
			AND status IN ('pending', 'submitted', 'partial')
			AND exchange_order_id IS NOT NULL
		`, f.UserID, f.Symbol, counterType)
		if err != nil {
			continue
		}

		type counterOrder struct {
			ID              models.UUID
			ExchangeOrderID string
		}
		var counters []counterOrder
		for rows.Next() {
			var co counterOrder
			if err := rows.Scan(&co.ID, &co.ExchangeOrderID); err == nil {
				counters = append(counters, co)
			}
		}
		rows.Close()

		if len(counters) == 0 {
			continue
		}

		// Get user context for cancel
		user := &models.User{ID: f.UserID}
		userCtx, err := GetUserContext(ctx, o.db, user)
		if err != nil || userCtx == nil {
			continue
		}

		// Cancel each counterpart using per-user exchange client (multi-exchange support)
		type orderCanceller interface {
			CancelOrder(ctx context.Context, apiKey, apiSecret, passphrase, orderID, symbol string) error
		}
		userExch := o.getUserExchange(f.UserID.String(), userCtx.Exchange)
		canceller, ok := userExch.(orderCanceller)
		if !ok {
			// Exchange doesn't support cancel — just mark cancelled in DB
			for _, co := range counters {
				o.db.ExecContext(ctx, `UPDATE orders SET status = 'cancelled', updated_at = $1 WHERE id = $2`, time.Now(), co.ID)
			}
			continue
		}

		for _, co := range counters {
			err := canceller.CancelOrder(ctx, userCtx.APIKey, userCtx.APISecret, userCtx.Passphrase, co.ExchangeOrderID, f.Symbol)
			if err != nil {
				o.logger.WithError(err).WithField("order_id", co.ID).Warn("Failed to cancel counterpart order on exchange")
			}
			// Mark cancelled in DB regardless (exchange might have already cancelled)
			o.db.ExecContext(ctx, `UPDATE orders SET status = 'cancelled', updated_at = $1 WHERE id = $2`, time.Now(), co.ID)
			o.logger.WithField("order_id", co.ID).
				WithField("symbol", f.Symbol).
				WithField("cancelled_type", counterType).
				Info("OCO: Cancelled counterpart protective order")
		}
	}
}
