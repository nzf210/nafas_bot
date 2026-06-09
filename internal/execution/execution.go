// ============================================================
// MODULE: execution
// Deskripsi: Order execution module - Market, Limit, TWAP orders
// ============================================================

package execution

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nzf210/nafas-bot/internal/exchange"
	"github.com/nzf210/nafas-bot/internal/logger"
	"github.com/nzf210/nafas-bot/internal/models"
	"github.com/shopspring/decimal"
)

// Executor handles order execution
// Nama Function: Executor
// Deskripsi: Struct utama untuk eksekusi order ke exchange.
// Parameter/Value Input:
//   - db: *sql.DB — koneksi database untuk logging
//   - exchange: exchange.Exchange — exchange client
//   - logger: *logger.Logger — logger instance
type Executor struct {
	db *sql.DB
	exchange exchange.Exchange
	logger   *logger.Logger
}

// NewExecutor creates a new order executor
// Nama Function: NewExecutor
// Deskripsi: Membuat instance executor baru.
// Parameter/Value Input:
//   - db: *sql.DB — koneksi database
//   - exch: exchange.Exchange — exchange client
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung, hanya inisialisasi
// Output/Return Value:
//   - *Executor: pointer ke executor
func NewExecutor(db *sql.DB, exch exchange.Exchange) *Executor {
	return&Executor{
		db:       db,
		exchange: exch,
		logger:   logger.Default().WithField("module", "execution"),
	}
}

// ExecutionPlan represents planned order execution
// Nama Function: ExecutionPlan
// Deskripsi: Struct rencana eksekusi order.
// Parameter/Value Input:
//   - Symbol: string — symbol trading
//   - Side: string — buy atau sell
//   - OrderType: string — market, limit, atau twap
//   - Quantity: decimal.Decimal — jumlah yang ingin diperdagangkan
//   - Price: decimal.Decimal — harga limit (untuk limit order)
//   - StopLoss: decimal.Decimal — stop loss price
//   - TakeProfitLevels: []TakeProfitLevel — level take profit
//   - MaxSlippage: decimal.Decimal — max slippage yang diterima
// Function yang Dipanggil/Dikonsumsi:
//   - Execute: dipanggil untuk eksekusi plan
// Output/Return Value:
//   - ExecutionPlan: struct plan eksekusi
type ExecutionPlan struct {
	Symbol           string
	Side             string
	OrderType        string
	Quantity         decimal.Decimal
	Price            decimal.Decimal
	StopLoss         decimal.Decimal
	TakeProfitLevels []TakeProfitLevel
	MaxSlippage      decimal.Decimal
}

// TakeProfitLevel represents a take profit level
// Nama Function: TakeProfitLevel
// Deskripsi: Struct level take profit.
// Parameter/Value Input:
//   - TargetPercent: float64 — target percentage dari entry
//   - QuantityPercent: float64 — percentage of position to close
// Function yang Dipanggil/Dikonsumsi:
//   - Execute: dipanggil untuk setiap level TP
// Output/Return Value:
//   - TakeProfitLevel: struct level TP
type TakeProfitLevel struct {
	TargetPercent   float64
	QuantityPercent float64
}

// ExecutionResult represents result of order execution
// Nama Function: ExecutionResult
// Deskripsi: Struct hasil eksekusi order.
// Parameter/Value Input:
//   - Order: *models.Order — order yang dieksekusi
//   - Fills: []models.TradeExecution — list fills
//   - ExecutionPrice: decimal.Decimal — harga rata-rata eksekusi
//   - Slippage: decimal.Decimal — slippage yang terjadi
//   - Error: error — error jika eksekusi gagal
// Function yang Dipanggil/Dikonsumsi:
//   - ExecuteMarket: dipanggil untuk eksekusi market order
// Output/Return Value:
//   - ExecutionResult: struct hasil eksekusi
type ExecutionResult struct {
	Order           *models.Order
	Fills           []models.TradeExecution
	ExecutionPrice  decimal.Decimal
	Slippage        decimal.Decimal
	Error           error
}

// ExecuteMarket executes a market order
// Nama Function: ExecuteMarket
// Deskripsi: Mengeksekusi market order ke exchange.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk HTTP request
//   - userID: models.UUID — user ID untuk logging
//   - plan: ExecutionPlan — rencana eksekusi
//   - apiKey: string — API key (terenkripsi, perlu decrypt)
//   - apiSecret: string — API secret (terenkripsi, perlu decrypt)
// Function yang Dipanggil/Dikonsumsi:
//   - exchange.PlaceOrder: dipanggil untuk kirim order ke exchange
//   - logOrder: dipanggil untuk logging ke database
// Output/Return Value:
//   - *ExecutionResult: hasil eksekusi
//   - error: error jika eksekusi gagal
func (e *Executor) ExecuteMarket(ctx context.Context, userID models.UUID, plan ExecutionPlan, apiKey, apiSecret string) (*ExecutionResult, error) {
	e.logger.WithField("symbol", plan.Symbol).
		WithField("side", plan.Side).
		WithField("quantity", plan.Quantity.String()).
		WithField("order_type", plan.OrderType).
		Info("EXEC: Placing market order to exchange")

	order := models.Order{
		ID:        uuid.New(),
		UserID:    userID,
		Exchange:  e.exchange.GetName(),
		Symbol:    plan.Symbol,
		Side:      plan.Side,
		OrderType: "MARKET",
		Quantity:  plan.Quantity,
		Status:    "pending",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	e.logger.WithField("symbol", plan.Symbol).
		WithField("order_id", order.ID.String()).
		Debug("EXEC: Order created, sending to exchange")

	filled, err := e.exchange.PlaceOrder(ctx, apiKey, apiSecret, order)
	if err != nil {
		e.logger.WithField("symbol", plan.Symbol).
			WithField("order_id", order.ID.String()).
			WithError(err).
			Error("EXEC FAILED: Exchange rejected order")
		return &ExecutionResult{Order: &order, Error: err}, err
	}

	result := &ExecutionResult{
		Order: filled,
	}

	// Calculate execution price from fills
	if filled.ExecutedQuantity.GreaterThan(decimal.Zero) {
		result.ExecutionPrice = filled.Price
	}

	e.logOrder(ctx, filled)
	e.logger.WithField("symbol", plan.Symbol).
		WithField("order_id", order.ID.String()).
		WithField("exchange_order_id", ptrStr(filled.ExchangeOrderID)).
		WithField("executed_qty", filled.ExecutedQuantity.String()).
		WithField("price", filled.Price.String()).
		WithField("status", filled.Status).
		Info("EXEC SUCCESS: Order placed successfully")

	return result, nil
}

// ExecuteLimit executes a limit order
// Nama Function: ExecuteLimit
// Deskripsi: Mengeksekusi limit order ke exchange.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk HTTP request
//   - userID: models.UUID — user ID untuk logging
//   - plan: ExecutionPlan — rencana eksekusi dengan Price terisi
//   - apiKey: string — API key
//   - apiSecret: string — API secret
// Function yang Dipanggil/Dikonsumsi:
//   - exchange.PlaceOrder: dipanggil untuk kirim order ke exchange
//   - logOrder: dipanggil untuk logging ke database
// Output/Return Value:
//   - *ExecutionResult: hasil eksekusi
//   - error: error jika eksekusi gagal
func (e *Executor) ExecuteLimit(ctx context.Context, userID models.UUID, plan ExecutionPlan, apiKey, apiSecret string) (*ExecutionResult, error) {
	if plan.Price.IsZero() {
		return nil, fmt.Errorf("limit order requires price")
	}

	e.logger.Infof("Executing limit order: %s %s %s @ %s", plan.Side, plan.Quantity.String(), plan.Symbol, plan.Price.String())

	order := models.Order{
		ID:        uuid.New(),
		UserID:    userID,
		Exchange:  e.exchange.GetName(),
		Symbol:    plan.Symbol,
		Side:      plan.Side,
		OrderType: "LIMIT",
		Quantity:  plan.Quantity,
		Price:     plan.Price,
		Status:    "pending",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	filled, err := e.exchange.PlaceOrder(ctx, apiKey, apiSecret, order)
	if err != nil {
		e.logger.Errorf("Limit order failed: %v", err)
		return &ExecutionResult{Order: &order, Error: err}, err
	}

	e.logOrder(ctx, filled)
	e.logger.Infof("Limit order placed: %s", ptrStr(filled.ExchangeOrderID))

	return &ExecutionResult{Order: filled}, nil
}

// ExecuteTWAP executes a TWAP (Time-Weighted Average Price) order
// Nama Function: ExecuteTWAP
// Deskripsi: Mengeksekusi TWAP order dengan membagi menjadi multiple smaller orders.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk HTTP request
//   - userID: models.UUID — user ID untuk logging
//   - plan: ExecutionPlan — rencana eksekusi
//   - apiKey: string — API key
//   - apiSecret: string — API secret
//   - duration: time.Duration — durasi total TWAP
//   - sliceInterval: time.Duration — interval antar slice
// Function yang Dipanggil/Dikonsumsi:
//   - ExecuteLimit: dipanggil untuk setiap slice TWAP
//   - exchange.GetPrice: dipanggil untuk harga rata-rata
// Output/Return Value:
//   - *ExecutionResult: hasil eksekusi dengan semua fills
//   - error: error jika eksekusi gagal
func (e *Executor) ExecuteTWAP(ctx context.Context, userID models.UUID, plan ExecutionPlan, apiKey, apiSecret string, duration, sliceInterval time.Duration) (*ExecutionResult, error) {
	if plan.Quantity.IsZero() {
		return nil, fmt.Errorf("TWAP order requires quantity")
	}

	// Calculate number of slices
	numSlices := int(duration / sliceInterval)
	if numSlices < 1 {
		numSlices = 1
	}

	sliceQty := plan.Quantity.Div(decimal.NewFromInt(int64(numSlices)))
	e.logger.Infof("Executing TWAP: %d slices over %v, %s per slice", numSlices, duration, sliceQty.String())

	var allFills []models.TradeExecution
	var totalExecuted decimal.Decimal
	var avgPrice decimal.Decimal

	ticker := time.NewTicker(sliceInterval)
	defer ticker.Stop()

	for i := 0; i < numSlices; i++ {
		select {
		case <-ctx.Done():
			return &ExecutionResult{Fills: allFills}, ctx.Err()
		case <-ticker.C:
			// Get current market price for reference
			currentPrice, err := e.exchange.GetPrice(ctx, plan.Symbol)
			if err != nil {
				e.logger.Warnf("Failed to get current price: %v", err)
				continue
			}

			// Execute slice as limit order near market price
			slicePlan := ExecutionPlan{
				Symbol:    plan.Symbol,
				Side:      plan.Side,
				OrderType: "LIMIT",
				Quantity:  sliceQty,
				Price:     currentPrice,
			}

			result, err := e.ExecuteLimit(ctx, userID, slicePlan, apiKey, apiSecret)
			if err != nil {
				e.logger.Warnf("TWAP slice %d failed: %v", i+1, err)
				continue
			}

			if result.Order != nil && result.Order.ExecutedQuantity.GreaterThan(decimal.Zero) {
				totalExecuted = totalExecuted.Add(result.Order.ExecutedQuantity)
				avgPrice = avgPrice.Add(result.Order.Price.Mul(result.Order.ExecutedQuantity))
				allFills = append(allFills, models.TradeExecution{
					ID:       uuid.New(),
					OrderID:  result.Order.ID,
					Price:    result.Order.Price,
					Quantity: result.Order.ExecutedQuantity,
					ExecutedAt: time.Now(),
				})
			}
		}
	}

	// Calculate average execution price
	if totalExecuted.GreaterThan(decimal.Zero) {
		avgPrice = avgPrice.Div(totalExecuted)
	}

	return &ExecutionResult{
		Fills:          allFills,
		ExecutionPrice: avgPrice,
	}, nil
}

// MonitorOrder monitors an order until filled or cancelled
// Nama Function: MonitorOrder
// Deskripsi: Memonitor order sampai filled atau cancelled.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk HTTP request
//   - orderID: string — exchange order ID
//   - apiKey: string — API key
//   - apiSecret: string — API secret
//   - timeout: time.Duration — max waktu monitoring
// Function yang Dipanggil/Dikonsumsi:
//   - exchange.GetOrderStatus: dipanggil secara polling
// Output/Return Value:
//   - *models.Order: order final status
//   - error: error jika monitoring gagal atau timeout
func (e *Executor) MonitorOrder(ctx context.Context, orderID string, apiKey, apiSecret string, timeout time.Duration) (*models.Order, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			order, err := e.exchange.GetOrderStatus(ctx, apiKey, apiSecret, orderID)
			if err != nil {
				return nil, err
			}

			if order.Status == "filled" || order.Status == "cancelled" || order.Status == "rejected" {
				return order, nil
			}
		}
	}
}

// logOrder logs order to database
func (e *Executor) logOrder(ctx context.Context, order *models.Order) {
	_, err := e.db.ExecContext(ctx, `
		INSERT INTO orders (id, user_id, exchange, symbol, side, order_type, price, quantity, executed_quantity, status, exchange_order_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (id) DO UPDATE SET
			status = EXCLUDED.status,
			executed_quantity = EXCLUDED.executed_quantity,
			updated_at = EXCLUDED.updated_at
	`, order.ID, order.UserID, order.Exchange, order.Symbol, order.Side, order.OrderType, order.Price, order.Quantity, order.ExecutedQuantity, order.Status, order.ExchangeOrderID, order.CreatedAt, order.UpdatedAt)

	if err != nil {
		e.logger.Warnf("Failed to log order: %v", err)
	}
}

// ptrStr safely converts *string to string
func ptrStr(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
