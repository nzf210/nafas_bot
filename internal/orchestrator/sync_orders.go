package orchestrator

import (
	"context"
	"time"

	"github.com/nzf210/nafas-bot/internal/models"
)

// SyncPendingOrders checks all pending, submitted, or partial orders
// and syncs their status with the exchange.
func (o *Orchestrator) SyncPendingOrders(ctx context.Context) {
	o.logger.Info("Starting pending orders sync")

	// Get all orders that need sync
	rows, err := o.db.QueryContext(ctx, `
		SELECT id, user_id, exchange, exchange_order_id, symbol
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
	}
	var orders []pendingOrder
	for rows.Next() {
		var po pendingOrder
		if err := rows.Scan(&po.ID, &po.UserID, &po.Exchange, &po.ExchangeOrderID, &po.Symbol); err != nil {
			o.logger.WithError(err).Warn("Failed to scan pending order row")
			continue
		}
		orders = append(orders, po)
	}

	if len(orders) == 0 {
		return
	}

	o.logger.Infof("Found %d pending orders to sync", len(orders))

	for _, order := range orders {
		// Need UserContext to get API keys
		user := &models.User{ID: order.UserID}
		userCtx, err := GetUserContext(ctx, o.db, user)
		if err != nil || userCtx == nil {
			o.logger.WithError(err).WithField("user_id", order.UserID).Warn("Failed to get user context for sync")
			continue
		}

		// Sync with exchange
		updatedOrder, err := o.exchange.GetOrderStatus(ctx, userCtx.APIKey, userCtx.APISecret, order.ExchangeOrderID, order.Symbol)
		if err != nil {
			o.logger.WithError(err).WithField("order_id", order.ID).Warn("Failed to fetch order status from exchange")
			continue
		}

		if updatedOrder != nil && updatedOrder.Status != "" {
			// Update the database
			_, err = o.db.ExecContext(ctx, `
				UPDATE orders 
				SET status = $1, executed_quantity = $2, updated_at = $3
				WHERE id = $4
			`, updatedOrder.Status, updatedOrder.ExecutedQuantity, time.Now(), order.ID)

			if err != nil {
				o.logger.WithError(err).WithField("order_id", order.ID).Warn("Failed to update order status in DB")
			} else {
				o.logger.WithField("order_id", order.ID).WithField("new_status", updatedOrder.Status).Info("Successfully synced order status")
			}
		}
	}
}
