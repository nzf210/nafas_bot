// ============================================================
// MODULE: exchange
// Deskripsi: Exchange API wrapper untuk Binance dan OKX
// ============================================================

package exchange

import (
	// "bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/nzf210/nafas-bot/internal/logger"
	"github.com/nzf210/nafas-bot/internal/models"
	"github.com/shopspring/decimal"
)

// Tambah method ini di BinanceClient
func (c *BinanceClient) LoadSymbolFilters(ctx context.Context) error {
	url := fmt.Sprintf("%s/api/v3/exchangeInfo", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var info struct {
		Symbols []struct {
			Symbol  string `json:"symbol"`
			Filters []struct {
				FilterType string `json:"filterType"`
				StepSize    string `json:"stepSize"`
				MinQty      string `json:"minQty"`
				MaxQty      string `json:"maxQty"`
				MinNotional string `json:"minNotional"`
			} `json:"filters"`
		} `json:"symbols"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return err
	}

	c.symbolFilters = make(map[string]SymbolFilter)
	for _, s := range info.Symbols {
		var filter SymbolFilter
		for _, f := range s.Filters {
			switch f.FilterType {
			case "LOT_SIZE":
				step, _ := decimal.NewFromString(f.StepSize)
				minQty, _ := decimal.NewFromString(f.MinQty)
				maxQty, _ := decimal.NewFromString(f.MaxQty)
				filter.StepSize = step
				filter.MinQty = minQty
				filter.MaxQty = maxQty
			case "NOTIONAL":
				minNotional, _ := decimal.NewFromString(f.MinNotional)
				filter.MinNotional = minNotional
			}
		}
		c.symbolFilters[s.Symbol] = filter
	}

	c.logger.Infof("Loaded filters for %d symbols (LOT_SIZE + NOTIONAL)", len(c.symbolFilters))
	return nil
}

// Expose fixQuantity sebagai public agar orchestrator bisa pakai
func (c *BinanceClient) FixQuantity(symbol string, qty decimal.Decimal) decimal.Decimal {
	return c.fixQuantity(symbol, qty)
}

// Exchange interface for all exchange implementations
// Nama Function: Exchange
// Deskripsi: Interface untuk semua implementasi exchange.
// Parameter/Value Input:
//   - Context dan parameter sesuai method masing-masing
//
// Function yang Dipanggil/Dikonsumsi:
//   - GetName: mengambil nama exchange
//   - GetBalances: mengambil semua balance
//   - PlaceOrder: menempatkan order
//   - GetOrderStatus: mengambil status order
//   - GetCandles: mengambil data candle OHLCV
//
// Output/Return Value:
//   - Interface dengan semua method exchange
type Exchange interface {
	GetName() string
	GetBalances(ctx context.Context, apiKey, apiSecret string) (map[string]decimal.Decimal, error)
	PlaceOrder(ctx context.Context, apiKey, apiSecret string, order models.Order) (*models.Order, error)
	GetOrderStatus(ctx context.Context, apiKey, apiSecret string, orderID string) (*models.Order, error)
	GetPrice(ctx context.Context, symbol string) (decimal.Decimal, error)
	GetTicker(ctx context.Context, symbol string) (*models.MarketSnapshot, error)
	GetCandles(ctx context.Context, symbol, interval string, limit int) ([]models.MarketCandle, error)
}

// NewBinance creates Binance exchange client
// Nama Function: NewBinance
// Deskripsi: Membuat instance Binance exchange client baru.
// Parameter/Value Input:
//   - Tidak ada parameter langsung, menggunakan endpoint tetap
//
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung, hanya inisialisasi
//
// Output/Return Value:
//   - *BinanceClient: pointer ke Binance client
func NewBinance() *BinanceClient {
	return &BinanceClient{
		baseURL:    "https://api.binance.com",
		logger:     logger.Default().WithField("module", "exchange/binance"),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// BinanceClient Binance exchange implementation
type BinanceClient struct {
	baseURL       string
	logger        *logger.Logger
	httpClient    *http.Client
	symbolFilters map[string]SymbolFilter
}

// BinanceTickerResponse represents Binance ticker API response
type BinanceTickerResponse struct {
	Symbol      string `json:"symbol"`
	Price       string `json:"lastPrice"`
	Volume      string `json:"volume"`
	QuoteVolume string `json:"quoteVolume"`
}

// BinanceBalanceResponse represents balance info
type BinanceBalanceResponse struct {
	Asset  string `json:"asset"`
	Free   string `json:"free"`
	Locked string `json:"locked"`
}

// GetName returns exchange name
// Nama Function: GetName
// Deskripsi: Mengambil nama exchange.
// Parameter/Value Input:
//   - Tidak ada parameter
//
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung
//
// Output/Return Value:
//   - string: nama exchange ("binance")
func (c *BinanceClient) GetName() string {
	return "binance"
}

// GetBalances retrieves all account balances
// Nama Function: GetBalances
// Deskripsi: Mengambil semua balance akun dari Binance.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk HTTP request
//   - apiKey: string — API key dari user
//   - apiSecret: string — API secret dari user (untuk signing)
//
// Function yang Dipanggil/Dikonsumsi:
//   - c.signRequest: dipanggil untuk sign request dengan HMAC
//   - httpClient.Do: dipanggil untuk execute request ke Binance API
//   - json.Unmarshal: dipanggil untuk parse response
//
// Output/Return Value:
//   - map[string]decimal.Decimal: map asset ke balance
//   - error: error jika request gagal
func (c *BinanceClient) GetBalances(ctx context.Context, apiKey, apiSecret string) (map[string]decimal.Decimal, error) {
	timestamp := time.Now().UnixMilli()
	params := fmt.Sprintf("timestamp=%d&recvWindow=5000", timestamp)

	signature := c.signRequest(params, apiSecret)
	url := fmt.Sprintf("%s/api/v3/account?%s&signature=%s", c.baseURL, params, signature)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("X-MBX-APIKEY", apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch balances: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("binance API error: %s", string(body))
	}

	var account struct {
		Balances []BinanceBalanceResponse `json:"balances"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&account); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	balances := make(map[string]decimal.Decimal)
	for _, b := range account.Balances {
		free, _ := decimal.NewFromString(b.Free)
		locked, _ := decimal.NewFromString(b.Locked)
		total := free.Add(locked)
		if total.GreaterThan(decimal.Zero) {
			balances[b.Asset] = total
		}
	}

	return balances, nil
}

// PlaceOrder places a new order
// Nama Function: PlaceOrder
// Deskripsi: Menempatkan order baru ke Binance.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk HTTP request
//   - apiKey, apiSecret: string — kredensial user
//   - order: models.Order — detail order (symbol, side, quantity, price)
//
// Function yang Dipanggil/Dikonsumsi:
//   - c.signRequest: dipanggil untuk sign request
//   - httpClient.Do: dipanggil untuk kirim order ke Binance
//   - json.NewEncoder: dipanggil untuk encode body request
//
// Output/Return Value:
//   - *models.Order: order dengan exchange_order_id populated
//   - error: error jika order gagal

// SymbolFilter represents trading rules for a symbol loaded from Binance exchange info
// Nama Function: SymbolFilter
// Deskripsi: Struct untuk menyimpan filter trading rules per symbol dari Binance.
// Parameter/Value Input:
//   - StepSize: decimal.Decimal — step size untuk quantity precision
//   - MinQty: decimal.Decimal — minimum quantity yang diizinkan
//   - MaxQty: decimal.Decimal — maximum quantity yang diizinkan
//   - MinNotional: decimal.Decimal — minimum order value (quantity * price) dalam quote currency
type SymbolFilter struct {
	StepSize    decimal.Decimal
	MinQty      decimal.Decimal
	MaxQty      decimal.Decimal
	MinNotional decimal.Decimal
}

func (c *BinanceClient) validatePrecision(symbol string, qty decimal.Decimal) error {
	f := c.getLotSize(symbol)

	if f.StepSize.IsZero() {
		return nil
	}

	expected := qty.Div(f.StepSize).Floor().Mul(f.StepSize)

	if !qty.Equal(expected) {
		return fmt.Errorf("invalid precision for %s", symbol)
	}

	return nil
}

func (c *BinanceClient) PlaceOrder(ctx context.Context, apiKey, apiSecret string, order models.Order) (*models.Order, error) {
	timestamp := time.Now().UnixMilli()

	qty := c.fixQuantity(order.Symbol, order.Quantity)
	// 🔒 VALIDATION GATE (WAJIB CHECK ERROR)
	if err := c.validatePrecision(order.Symbol, qty); err != nil {
		return nil, fmt.Errorf("precision validation failed: %w", err)
	}

	params := map[string]string{
		"symbol":     order.Symbol,
		"side":       order.Side,
		"type":       order.OrderType,
		"quantity":   qty.String(),
		"timestamp":  fmt.Sprintf("%d", timestamp),
		"recvWindow": "5000",
	}

	if order.OrderType == "LIMIT" || order.OrderType == "STOP_LOSS_LIMIT" || order.OrderType == "TAKE_PROFIT_LIMIT" {
		params["price"] = order.Price.String()
		params["timeInForce"] = "GTC"
	}

	// Handle stop loss and take profit orders
	if order.OrderType == "STOP_LOSS_LIMIT" || order.OrderType == "TAKE_PROFIT_LIMIT" {
		// For SL/TP orders, price is the trigger price (stopPrice)
		// We use the Price field as stopPrice
		params["stopPrice"] = order.Price.String()
	}

	var keys []string
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var queryParts []string
	for _, k := range keys {
		queryParts = append(queryParts, fmt.Sprintf("%s=%s", k, params[k]))
	}
	queryString := strings.Join(queryParts, "&")

	signature := c.signRequest(queryString, apiSecret)
	url := fmt.Sprintf("%s/api/v3/order?%s&signature=%s", c.baseURL, queryString, signature)

	// body, _ := json.Marshal(params)
	// req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))

	req, err := http.NewRequestWithContext(
		ctx,
		"POST",
		url,
		nil,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-MBX-APIKEY", apiKey)
	req.Header.Set("Content-Type", "application/json")

	// Tambahan
	req.Header.Set(
		"Content-Type",
		"application/x-www-form-urlencoded",
	)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to place order: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("binance order error: %s", string(respBody))
	}

	var binanceResp struct {
		OrderID     int64  `json:"orderId"`
		Symbol      string `json:"symbol"`
		ExecutedQty string `json:"executedQty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&binanceResp); err != nil {
		return nil, fmt.Errorf("failed to decode order response: %w", err)
	}

	c.logger.WithField("symbol", order.Symbol).
		WithField("side", order.Side).
		WithField("type", order.OrderType).
		WithField("quantity", order.Quantity.String()).
		WithField("query", queryString).
		Debug("BINANCE ORDER REQUEST")

	order.ExchangeOrderID = fmtPtr(fmt.Sprintf("%d", binanceResp.OrderID))
	order.Status = "pending"
	return &order, nil
}

func (c *BinanceClient) fixQuantity(symbol string, qty decimal.Decimal) decimal.Decimal {
	f := c.getLotSize(symbol)
	c.logger.Infof("LOT_SIZE DEBUG %s step=%s min=%s max=%s",
		symbol,
		f.StepSize.String(),
		f.MinQty.String(),
		f.MaxQty.String(),
	)

	// fallback kalau belum ada rule
	if f.StepSize.IsZero() {
		return qty.Truncate(8)
	}

	// kalau qty terlalu kecil
	if qty.LessThan(f.MinQty) && !f.MinQty.IsZero() {
		return f.MinQty
	}

	// kalau terlalu besar
	if qty.GreaterThan(f.MaxQty) && !f.MaxQty.IsZero() {
		return f.MaxQty
	}

	// snap ke stepSize (floor biar aman dari reject Binance)
	qty = qty.Div(f.StepSize).Floor().Mul(f.StepSize)

	// safety: hindari 0 setelah pembulatan
	if qty.IsZero() {
		return f.MinQty
	}

	return qty
}

func (c *BinanceClient) getLotSize(symbol string) SymbolFilter {
	return c.symbolFilters[symbol]
}

// GetMinNotional returns the minimum notional value for a symbol
// Nama Function: GetMinNotional
// Deskripsi: Mengambil minimum notional value (minimum order value) untuk sebuah symbol.
// Parameter/Value Input:
//   - symbol: string — symbol trading (contoh: BTCUSDT)
//
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung, hanya akses map
//
// Output/Return Value:
//   - decimal.Decimal: minimum notional dalam quote currency (e.g., 5 USDT)
func (c *BinanceClient) GetMinNotional(symbol string) decimal.Decimal {
	if f, ok := c.symbolFilters[symbol]; ok {
		return f.MinNotional
	}
	return decimal.Zero
}

// GetOrderStatus retrieves order status from Binance
// Nama Function: GetOrderStatus
// Deskripsi: Mengambil status order dari Binance.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk HTTP request
//   - apiKey, apiSecret: string — kredensial user
//   - orderID: string — exchange order ID
//
// Function yang Dipanggil/Dikonsumsi:
//   - c.signRequest: dipanggil untuk sign request
//   - httpClient.Do: dipanggil untuk fetch order status
//
// Output/Return Value:
//   - *models.Order: order dengan status updated
//   - error: error jika fetch gagal
func (c *BinanceClient) GetOrderStatus(ctx context.Context, apiKey, apiSecret string, orderID string) (*models.Order, error) {
	timestamp := time.Now().UnixMilli()
	params := fmt.Sprintf("orderId=%s&timestamp=%d&recvWindow=5000", orderID, timestamp)
	signature := c.signRequest(params, apiSecret)

	url := fmt.Sprintf("%s/api/v3/order?%s&signature=%s", c.baseURL, params, signature)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-MBX-APIKEY", apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("binance get order error")
	}

	var binanceOrder struct {
		OrderID     int64  `json:"orderId"`
		Symbol      string `json:"symbol"`
		Side        string `json:"side"`
		Type        string `json:"type"`
		Price       string `json:"price"`
		OrigQty     string `json:"origQty"`
		ExecutedQty string `json:"executedQty"`
		Status      string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&binanceOrder); err != nil {
		return nil, err
	}

	return &models.Order{
		ExchangeOrderID:  fmtPtr(fmt.Sprintf("%d", binanceOrder.OrderID)),
		Symbol:           binanceOrder.Symbol,
		Side:             binanceOrder.Side,
		OrderType:        binanceOrder.Type,
		ExecutedQuantity: parseDecimal(binanceOrder.ExecutedQty),
		Status:           mapBinanceStatus(binanceOrder.Status),
	}, nil
}

// GetPrice retrieves current price for symbol
// Nama Function: GetPrice
// Deskripsi: Mengambil harga terakhir untuk symbol.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk HTTP request
//   - symbol: string — symbol trading (contoh: BTCUSDT)
//
// Function yang Dipanggil/Dikonsumsi:
//   - httpClient.Do: dipanggil untuk fetch dari Binance ticker API
//
// Output/Return Value:
//   - decimal.Decimal: harga terakhir
//   - error: error jika fetch gagal
func (c *BinanceClient) GetPrice(ctx context.Context, symbol string) (decimal.Decimal, error) {
	url := fmt.Sprintf("%s/api/v3/ticker/price?symbol=%s", c.baseURL, symbol)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return decimal.Decimal{}, err
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return decimal.Decimal{}, err
	}
	defer resp.Body.Close()

	var ticker struct {
		Price string `json:"price"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ticker); err != nil {
		return decimal.Decimal{}, err
	}

	return decimal.NewFromString(ticker.Price)
}

// GetTicker retrieves full ticker data
// Nama Function: GetTicker
// Deskripsi: Mengambil data ticker lengkap (harga + volume).
// Parameter/Value Input:
//   - ctx: context.Context — context untuk HTTP request
//   - symbol: string — symbol trading
//
// Function yang Dipanggil/Dikonsumsi:
//   - httpClient.Do: dipanggil untuk fetch dari 24hr ticker API
//
// Output/Return Value:
//   - *models.MarketSnapshot: snapshot harga dan volume
//   - error: error jika fetch gagal
func (c *BinanceClient) GetTicker(ctx context.Context, symbol string) (*models.MarketSnapshot, error) {
	url := fmt.Sprintf("%s/api/v3/ticker/24hr?symbol=%s", c.baseURL, symbol)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var ticker BinanceTickerResponse
	if err := json.NewDecoder(resp.Body).Decode(&ticker); err != nil {
		return nil, err
	}

	price, _ := decimal.NewFromString(ticker.Price)
	volume, _ := decimal.NewFromString(ticker.QuoteVolume)

	return &models.MarketSnapshot{
		Symbol:    ticker.Symbol,
		LastPrice: price,
		Volume24h: volume,
	}, nil
}

// GetCandles fetches OHLCV klines data from Binance
// Nama Function: GetCandles
// Deskripsi: Mengambil data candle OHLCV dari Binance klines API.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk HTTP request
//   - symbol: string — symbol trading (contoh: BTCUSDT)
//   - interval: string — timeframe (1m, 5m, 15m, 1h, 4h, 1d, 1w)
//   - limit: int — jumlah candle yang diambil (default 100, max 1000)
//
// Function yang Dipanggil/Dikonsumsi:
//   - httpClient.Do: dipanggil untuk fetch dari Binance klines API
//   - json.Unmarshal: dipanggil untuk parse response array
//
// Output/Return Value:
//   - []models.MarketCandle: list candle data
//   - error: error jika fetch gagal
func (c *BinanceClient) GetCandles(ctx context.Context, symbol, interval string, limit int) ([]models.MarketCandle, error) {
	if limit == 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	url := fmt.Sprintf("%s/api/v3/klines?symbol=%s&interval=%s&limit=%d", c.baseURL, symbol, interval, limit)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch candles: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("binance klines error: %s", string(body))
	}

	var rawCandles [][]any
	if err := json.NewDecoder(resp.Body).Decode(&rawCandles); err != nil {
		return nil, fmt.Errorf("failed to decode klines: %w", err)
	}

	candles := make([]models.MarketCandle, 0, len(rawCandles))
	for _, raw := range rawCandles {
		if len(raw) < 7 {
			continue
		}

		openTime := parseTimestamp(raw[0])
		open, _ := decimal.NewFromString(toString(raw[1]))
		high, _ := decimal.NewFromString(toString(raw[2]))
		low, _ := decimal.NewFromString(toString(raw[3]))
		close, _ := decimal.NewFromString(toString(raw[4]))
		volume, _ := decimal.NewFromString(toString(raw[5]))

		candles = append(candles, models.MarketCandle{
			Symbol:     symbol,
			Interval:   interval,
			Open:       open,
			High:       high,
			Low:        low,
			Close:      close,
			Volume:     volume,
			CandleTime: openTime,
		})
	}

	return candles, nil
}

// toString converts any to string safely
func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// parseTimestamp converts Binance timestamp (ms) to time.Time
func parseTimestamp(v any) time.Time {
	switch val := v.(type) {
	case float64:
		return time.UnixMilli(int64(val))
	case int64:
		return time.UnixMilli(val)
	}
	return time.Time{}
}

// signRequest creates HMAC-SHA256 signature
func (c *BinanceClient) signRequest(queryString, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(queryString))
	return hex.EncodeToString(mac.Sum(nil))
}

func parseDecimal(s string) decimal.Decimal {
	d, _ := decimal.NewFromString(s)
	return d
}

func fmtPtr(s string) *string {
	return &s
}

func mapBinanceStatus(status string) string {
	statusMap := map[string]string{
		"NEW":              "pending",
		"PARTIALLY_FILLED": "partial",
		"FILLED":           "filled",
		"CANCELED":         "cancelled",
		"REJECTED":         "rejected",
		"EXPIRED":          "expired",
	}
	if mapped, ok := statusMap[status]; ok {
		return mapped
	}
	return status
}
