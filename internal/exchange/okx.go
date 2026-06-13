package exchange

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nzf210/nafas-bot/internal/logger"
	"github.com/nzf210/nafas-bot/internal/models"
	"github.com/shopspring/decimal"
)

// OKXClient OKX exchange implementation
// Nama Function: OKXClient
// Deskripsi: Client untuk exchange OKX. Mengimplementasi interface Exchange.
//   Menggunakan OKX API v5 endpoint.
//
// Field:
//   - baseURL: string — base URL OKX API
//   - logger: *logger.Logger — logger
//   - httpClient: *http.Client — HTTP client dengan timeout
type OKXClient struct {
	baseURL    string
	logger     *logger.Logger
	httpClient *http.Client
}

// NewOKX creates OKX exchange client
// Nama Function: NewOKX
// Deskripsi: Membuat instance OKX client baru.
//
// Output/Return Value:
//   - *OKXClient: pointer ke client instance
func NewOKX() *OKXClient {
	return &OKXClient{
		baseURL:    "https://www.okx.com",
		logger:     logger.Default().WithField("module", "exchange/okx"),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// GetName returns exchange name
// Nama Function: GetName
// Deskripsi: Mengembalikan nama exchange "okx".
//
// Output/Return Value:
//   - string: "okx"
func (c *OKXClient) GetName() string {
	return "okx"
}

// signRequest generates OKX signature and adds headers
func (c *OKXClient) signRequest(req *http.Request, apiKey, apiSecret, passphrase, body string) error {
	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	message := timestamp + req.Method + req.URL.RequestURI() + body

	mac := hmac.New(sha256.New, []byte(apiSecret))
	mac.Write([]byte(message))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	req.Header.Set("OK-ACCESS-KEY", apiKey)
	req.Header.Set("OK-ACCESS-SIGN", signature)
	req.Header.Set("OK-ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("OK-ACCESS-PASSPHRASE", passphrase)
	req.Header.Set("Content-Type", "application/json")
	return nil
}

// GetBalances fetches account balances from OKX
// Nama Function: GetBalances
// Deskripsi: Mengambil semua balance dari account OKX menggunakan API endpoint
//   /api/v5/account/balance.
//
// Parameter/Value Input:
//   - ctx: context.Context — context untuk HTTP request
//   - apiKey: string — OKX API key
//   - apiSecret: string — OKX API secret
//
// Output/Return Value:
//   - map[string]decimal.Decimal: map asset → balance
//   - error: error jika request gagal
//
// Catatan: OKX butuh signature authentication untuk private endpoints
func (c *OKXClient) GetBalances(ctx context.Context, apiKey, apiSecret, passphrase string) (map[string]decimal.Decimal, error) {
	urlStr := fmt.Sprintf("%s/api/v5/account/balance", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return nil, err
	}

	if err := c.signRequest(req, apiKey, apiSecret, passphrase, ""); err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OKX GetBalances: HTTP %d - %s", resp.StatusCode, string(body))
	}

	var result struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			Details []struct {
				Ccy   string `json:"ccy"`
				Avail string `json:"availBal"`
			} `json:"details"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if result.Code != "0" {
		return nil, fmt.Errorf("OKX GetBalances API error: %s", result.Msg)
	}

	balances := make(map[string]decimal.Decimal)
	if len(result.Data) > 0 {
		for _, detail := range result.Data[0].Details {
			bal, _ := decimal.NewFromString(detail.Avail)
			balances[detail.Ccy] = bal
		}
	}
	return balances, nil
}

// PlaceOrder places a new order on OKX
// Nama Function: PlaceOrder
// Deskripsi: Membuat order baru di OKX.
//
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - apiKey: string — API key
//   - apiSecret: string — API secret
//   - order: models.Order — order details
//
// Output/Return Value:
//   - *models.Order: order dengan filled data
//   - error: error jika gagal
func (c *OKXClient) PlaceOrder(ctx context.Context, apiKey, apiSecret, passphrase string, order models.Order) (*models.Order, error) {
	urlStr := fmt.Sprintf("%s/api/v5/trade/order", c.baseURL)
	
	side := strings.ToLower(order.Side)
	
	ordType := "limit"
	if strings.Contains(strings.ToLower(order.OrderType), "market") {
		ordType = "market"
	}
	
	okxSymbol := c.convertSymbol(order.Symbol)
	
	reqBody := map[string]any{
		"instId":  okxSymbol,
		"tdMode":  "cash",
		"side":    side,
		"ordType": ordType,
		"sz":      order.Quantity.String(),
	}
	
	if ordType == "limit" && !order.Price.IsZero() {
		reqBody["px"] = order.Price.String()
	}

	bodyBytes, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST", urlStr, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, err
	}

	if err := c.signRequest(req, apiKey, apiSecret, passphrase, string(bodyBytes)); err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OKX PlaceOrder: HTTP %d - %s", resp.StatusCode, string(body))
	}

	var result struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			OrdId string `json:"ordId"`
			SMsg  string `json:"sMsg"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if result.Code != "0" {
		return nil, fmt.Errorf("OKX PlaceOrder API error: %s", result.Msg)
	}

	if len(result.Data) == 0 {
		return nil, fmt.Errorf("OKX PlaceOrder: empty data returned")
	}

	if result.Data[0].OrdId == "" {
		return nil, fmt.Errorf("OKX PlaceOrder failed: %s", result.Data[0].SMsg)
	}

	order.ExchangeOrderID = &result.Data[0].OrdId
	order.Status = "submitted"
	return &order, nil
}

// GetOrderStatus gets order status from OKX
// Nama Function: GetOrderStatus
// Deskripsi: Mengambil status order dari OKX.
//
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - apiKey: string — API key
//   - apiSecret: string — API secret
//   - orderID: string — exchange order ID
//   - symbol: string — trading pair symbol
//
// Output/Return Value:
//   - *models.Order: order dengan status terbaru
//   - error: error jika gagal
func (c *OKXClient) GetOrderStatus(ctx context.Context, apiKey, apiSecret, passphrase string, orderID string, symbol string) (*models.Order, error) {
	okxSymbol := c.convertSymbol(symbol)
	urlStr := fmt.Sprintf("%s/api/v5/trade/order?instId=%s&ordId=%s", c.baseURL, okxSymbol, orderID)
	
	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return nil, err
	}

	if err := c.signRequest(req, apiKey, apiSecret, passphrase, ""); err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("OKX GetOrderStatus: HTTP %d - %s", resp.StatusCode, string(body))
	}

	var result struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			State     string `json:"state"`
			AvgPx     string `json:"avgPx"`
			AccFillSz string `json:"accFillSz"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if result.Code != "0" {
		return nil, fmt.Errorf("OKX GetOrderStatus API error: %s", result.Msg)
	}

	if len(result.Data) == 0 {
		return nil, fmt.Errorf("OKX GetOrderStatus: order not found")
	}

	data := result.Data[0]
	
	status := "pending"
	switch data.State {
	case "filled":
		status = "filled"
	case "canceled":
		status = "cancelled"
	case "partially_filled":
		status = "partial"
	case "live":
		status = "pending"
	}

	avgPx, _ := decimal.NewFromString(data.AvgPx)
	fillSz, _ := decimal.NewFromString(data.AccFillSz)

	return &models.Order{
		ExchangeOrderID:  &orderID,
		Status:           status,
		Price:            avgPx,
		ExecutedQuantity: fillSz,
		Symbol:           symbol,
	}, nil
}

// CancelOrder cancels an existing order on OKX
func (c *OKXClient) CancelOrder(ctx context.Context, apiKey, apiSecret, passphrase string, orderID string, symbol string) error {
	urlStr := fmt.Sprintf("%s/api/v5/trade/cancel-order", c.baseURL)
	okxSymbol := c.convertSymbol(symbol)

	reqBody := map[string]any{
		"instId": okxSymbol,
		"ordId":  orderID,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST", urlStr, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return err
	}

	if err := c.signRequest(req, apiKey, apiSecret, passphrase, string(bodyBytes)); err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("OKX CancelOrder HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	if result.Code != "0" {
		return fmt.Errorf("OKX CancelOrder API error: %s", result.Msg)
	}

	return nil
}

// GetPrice gets current price for a symbol
// Nama Function: GetPrice
// Deskripsi: Mengambil harga terakhir untuk symbol. Menggunakan public endpoint
//   /api/v5/market/ticker yang tidak butuh authentication.
//
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - symbol: string — trading pair (contoh: "BTCUSDT", "ETHBTC")
//
// Output/Return Value:
//   - decimal.Decimal: harga terakhir
//   - error: error jika gagal fetch
//
// Catatan: OKX menggunakan format symbol berbeda - BTCUSDT menjadi BTC-USDT
func (c *OKXClient) GetPrice(ctx context.Context, symbol string) (decimal.Decimal, error) {
	// Convert symbol format: BTCUSDT → BTC-USDT
	okxSymbol := c.convertSymbol(symbol)

	url := fmt.Sprintf("%s/api/v5/market/ticker?instId=%s", c.baseURL, okxSymbol)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return decimal.Decimal{}, fmt.Errorf("OKX GetPrice: failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return decimal.Decimal{}, fmt.Errorf("OKX GetPrice: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return decimal.Decimal{}, fmt.Errorf("OKX GetPrice: HTTP %d - %s", resp.StatusCode, string(body))
	}

	var ticker OKXTickerResponse
	if err := json.NewDecoder(resp.Body).Decode(&ticker); err != nil {
		return decimal.Decimal{}, fmt.Errorf("OKX GetPrice: failed to decode response: %w", err)
	}

	if len(ticker.Data) == 0 {
		return decimal.Decimal{}, fmt.Errorf("OKX GetPrice: no data for symbol %s", symbol)
	}

	price, err := decimal.NewFromString(ticker.Data[0].Last)
	if err != nil {
		return decimal.Decimal{}, fmt.Errorf("OKX GetPrice: invalid price format: %s", ticker.Data[0].Last)
	}

	return price, nil
}

// GetTicker gets price and volume ticker
// Nama Function: GetTicker
// Deskripsi: Mengambil ticker lengkap (harga + volume 24h) dari OKX.
//
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - symbol: string — trading pair
//
// Output/Return Value:
//   - *models.MarketSnapshot: snapshot dengan last price, volume, dll
//   - error: error jika gagal
func (c *OKXClient) GetTicker(ctx context.Context, symbol string) (*models.MarketSnapshot, error) {
	// Convert symbol format
	okxSymbol := c.convertSymbol(symbol)

	url := fmt.Sprintf("%s/api/v5/market/ticker?instId=%s", c.baseURL, okxSymbol)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var ticker OKXTickerResponse
	if err := json.NewDecoder(resp.Body).Decode(&ticker); err != nil {
		return nil, err
	}

	if len(ticker.Data) == 0 {
		return nil, fmt.Errorf("no ticker data for %s", symbol)
	}

	data := ticker.Data[0]
	lastPrice, _ := decimal.NewFromString(data.Last)
	volume24h, _ := decimal.NewFromString(data.Vol24h)

	return &models.MarketSnapshot{
		Symbol:     symbol,
		LastPrice:  lastPrice,
		Volume24h:  volume24h,
	}, nil
}

// GetCandles fetches OHLCV candle data
// Nama Function: GetCandles
// Deskripsi: Mengambil historical candle data dari OKX.
//
// Parameter/Value Input:
//   - ctx: context.Context — context
//   - symbol: string — trading pair
//   - interval: string — candle interval ("1m", "5m", "1h", "4h", "1d")
//   - limit: int — jumlah candle (max 100)
//
// Output/Return Value:
//   - []models.MarketCandle: slice candle data
//   - error: error jika gagal
func (c *OKXClient) GetCandles(ctx context.Context, symbol, interval string, limit int) ([]models.MarketCandle, error) {
	// TODO: Implement OKX candles fetching
	return nil, fmt.Errorf("OKX GetCandles: not implemented yet")
}

// convertSymbol converts Binance-style symbol to OKX format
// Nama Function: convertSymbol
// Deskripsi: Konversi symbol dari format Binance (BTCUSDT) ke format OKX (BTC-USDT).
//
// Parameter/Value Input:
//   - symbol: string — symbol dalam format Binance
//
// Output/Return Value:
//   - string: symbol dalam format OKX
func (c *OKXClient) convertSymbol(symbol string) string {
	// Common quote assets
	quoteAssets := []string{"USDT", "BUSD", "USDC", "BTC", "ETH", "BNB"}

	for _, quote := range quoteAssets {
		if strings.HasSuffix(symbol, quote) {
			base := strings.TrimSuffix(symbol, quote)
			return base + "-" + quote
		}
	}

	// If no known quote, just return as-is
	return symbol
}

// OKX API Response structures
type OKXTickerResponse struct {
	Code string        `json:"code"`
	Msg  string        `json:"msg"`
	Data []OKXTickerData `json:"data"`
}

type OKXTickerData struct {
	InstID  string `json:"instId"`  // Instrument ID e.g. "BTC-USDT"
	Last    string `json:"last"`    // Last price
	Vol24h  string `json:"vol24h"` // Volume 24h
	AskPx   string `json:"askPx"`   // Best ask price
	BidPx   string `json:"bidPx"`   // Best bid price
	Open24h string `json:"open24h"` // Open price 24h
	High24h string `json:"high24h"` // High price 24h
	Low24h  string `json:"low24h"`  // Low price 24h
}