// ============================================================
// MODULE: ai
// Deskripsi: TradingAgents client untuk multi-agent trading analysis
// ============================================================

package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nzf210/nafas-bot/internal/logger"
	"github.com/shopspring/decimal"
)

// CoordinatorDecision represents AI Coordinator output
// Used by both TradingAgentsClient and for orchestrator compatibility
// Nama Function: CoordinatorDecision
// Deskripsi: Struct output dari AI Coordinator.
// Parameter/Value Input:
//   - MarketContext: MarketContext — context market saat ini
//   - TopPairs: []string — ranked pairs untuk trading
//   - TradeDecision: string — keputusan (buy, sell, hold, skip)
//   - RiskAssessment: RiskAssessment — assessment risk dari AI
//   - ExecutionPlan: ExecutionPlan — rencana eksekusi
//   - Confidence: decimal.Decimal — confidence score
// Function yang Dipanggil/Dikonsumsi:
//   - TradingAgentsClient.Decide: dipanggil untuk generate decision
// Output/Return Value:
//   - CoordinatorDecision: struct decision lengkap
type CoordinatorDecision struct {
	MarketContext   MarketContext   `json:"market_context"`
	TopPairs        []string `json:"top_pairs"`
	TradeDecision   string         `json:"trade_decision"`
	RiskAssessment  RiskAssessment `json:"risk_assessment"`
	ExecutionPlan  ExecutionPlan  `json:"execution_plan"`
	Confidence decimal.Decimal `json:"confidence"`
}

// MarketContext represents current market conditions
// Nama Function: MarketContext
// Deskripsi: Struct context pasar saat ini.
// Parameter/Value Input:
//   - Regime: string — bull, bear, atau crab
//   - OverallSentiment: string — sentiment keseluruhan
// Function yang Dipanggil/Dikonsumsi:
//   - TradingAgentsClient.Analyze: dipanggil untuk generate context
// Output/Return Value:
//   - MarketContext: struct context pasar
type MarketContext struct {
	Regime           string `json:"regime"`
	OverallSentiment string `json:"overall_sentiment"`
}

// RiskAssessment represents AI risk assessment
// Nama Function: RiskAssessment
// Deskripsi: Struct assessment risk dari AI.
// Parameter/Value Input:
//   - Level: string — low, medium, high, extreme
// Function yang Dipanggil/Dikonsumsi:
//   - risk_guardian.CheckTrade: dipanggil untuk validasi final
// Output/Return Value:
//   - RiskAssessment: struct assessment
type RiskAssessment struct {
	Level string `json:"level"`
}

// ExecutionPlan represents trade execution plan
// Nama Function: ExecutionPlan
// Deskripsi: Struct rencana eksekusi trade.
// Parameter/Value Input:
//   - OrderType: string — market, limit, atau twap
//   - StopLoss: decimal.Decimal — stop loss percentage
//   - TakeProfitLevels: []TakeProfitLevel — level take profit
// Function yang Dipanggil/Dikonsumsi:
//   - execution.PlaceOrder: dipanggil untuk eksekusi plan
// Output/Return Value:
//   - ExecutionPlan: struct plan eksekusi
type ExecutionPlan struct {
	OrderType        string `json:"order_type"`
	StopLoss         decimal.Decimal   `json:"stop_loss"`
	TakeProfitLevels []TakeProfitLevel `json:"take_profit_levels"`
}

// TakeProfitLevel represents a take profit level
// Nama Function: TakeProfitLevel
// Deskripsi: Struct level take profit.
// Parameter/Value Input:
//   - TargetPercent: float64 — target percentage dari entry
//   - QuantityPercent: float64 — percentage of position to close
// Function yang Dipanggil/Dikonsumsi:
//   - execution.PlaceOrder: dipanggil untuk setiap level TP
// Output/Return Value:
//   - TakeProfitLevel: struct level TP
type TakeProfitLevel struct {
	TargetPercent   float64 `json:"target_percent"`
	QuantityPercent float64 `json:"quantity_percent"`
}

// TradingAgentsClient adalah client untuk TradingAgents API
// Support custom LLM base URL untuk Ollama, LM Studio, dll
// Nama Function: TradingAgentsClient
// Deskripsi: Client untuk TradingAgents API dengan custom LLM base URL support.
// Parameter/Value Input:
//   - baseURL: string — TradingAgents service URL
//   - apiKey: string — LLM API key
//   - model: string — model name
//   - llmBaseURL: string — custom LLM base URL (optional, untuk Ollama/LM Studio)
// Function yang Dipanggil/Dikonsumsi:
//   - Analyze: dipanggil untuk analisis ticker
//   - httpClient.Do: dipanggil untuk HTTP request
// Output/Return Value:
//   - TradingAgentsClient: struct client
type TradingAgentsClient struct {
	baseURL    string
	apiKey     string
	model      string
	llmBaseURL string // Custom LLM endpoint (Ollama, LM Studio, dll)
	httpClient *http.Client
}

// NewTradingAgentsClient creates a new TradingAgents client
// Nama Function: NewTradingAgentsClient
// Deskripsi: Membuat instance TradingAgents client baru.
// Parameter/Value Input:
//   - baseURL: string — TradingAgents service URL
//   - apiKey: string — LLM API key
//   - model: string — model name
//   - llmBaseURL: string — custom LLM base URL (optional)
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung, hanya inisialisasi
// Output/Return Value:
//   - *TradingAgentsClient: pointer ke client
func NewTradingAgentsClient(baseURL, apiKey, model, llmBaseURL string) *TradingAgentsClient {
	return &TradingAgentsClient{
		baseURL:    baseURL,
		apiKey:     apiKey,
		model:      model,
		llmBaseURL: llmBaseURL,
		httpClient: &http.Client{Timeout: 120 * time.Second}, // Timeout 2 min untuk multi-agent analysis
	}
}

// TARequest adalah request body untuk TradingAgents API
// Nama Function: TARequest
// Deskripsi: Struct request untuk TradingAgents analyze endpoint.
// Parameter/Value Input:
//   - Ticker: string — ticker symbol (e.g., BTC-USD)
//   - Date: string — analysis date (YYYY-MM-DD)
//   - LLMConfig: LLMConfig — optional LLM configuration
// Function yang Dipanggil/Dikonsumsi:
//   - json.Marshal: dipanggil untuk serialize request
// Output/Return Value:
//   - TARequest: struct request
type TARequest struct {
	Ticker    string     `json:"ticker"`
	Date      string     `json:"date"`
	LLMConfig LLMConfig  `json:"llm_config,omitempty"`
}

// LLMConfig adalah konfigurasi untuk custom LLM provider
// Nama Function: LLMConfig
// Deskripsi: Struct konfigurasi LLM untuk custom provider.
// Parameter/Value Input:
//   - BaseURL: string — base URL LLM provider
//   - APIKey: string — API key (optional untuk Ollama)
//   - Model: string — model name
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada
// Output/Return Value:
//   - LLMConfig: struct config
type LLMConfig struct {
	BaseURL string `json:"base_url,omitempty"`
	APIKey string `json:"api_key,omitempty"`
	Model  string `json:"model,omitempty"`
}

// TAResponse adalah response dari TradingAgents API
// Nama Function: TAResponse
// Deskripsi: Struct response dari TradingAgents analyze endpoint.
// Parameter/Value Input:
//   - Action: string — BUY/SELL/HOLD
//   - Confidence: float64 — confidence score (0-100)
//   - Reasoning: string — AI reasoning
//   - Allocation: float64 — recommended allocation percentage
//   - StopLoss: float64 — stop loss percentage
//   - TakeProfitTargets: []float64 — take profit levels
// Function yang Dipanggil/Dikonsumsi:
//   - json.Unmarshal: dipanggil untuk parse response
// Output/Return Value:
//   - TAResponse: struct response
type TAResponse struct {
	Action             string   `json:"action"`
	Confidence         float64  `json:"confidence"`
	Reasoning          string   `json:"reasoning"`
	Allocation         float64  `json:"allocation"`
	StopLoss           float64  `json:"stop_loss"`
	TakeProfitTargets  []float64 `json:"take_profit_targets"`
}

// Analyze calls TradingAgents API untuk analisis ticker
// Nama Function: Analyze
// Deskripsi: Memanggil TradingAgents API untuk analisis ticker.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk HTTP request
//   - ticker: string — ticker symbol (e.g., BTC-USD)
//   - date: string — analysis date (YYYY-MM-DD)
// Function yang Dipanggil/Dikonsumsi:
//   - httpClient.Do: dipanggil untuk kirim request ke TradingAgents
//   - json.Marshal/Unmarshal: dipanggil untuk serialize/deserialize
// Output/Return Value:
//   - *TAResponse: hasil analisis dari TradingAgents
//   - error: error jika request gagal
func (c *TradingAgentsClient) Analyze(ctx context.Context, ticker, date string) (*TAResponse, error) {
	logger := logger.Default().WithField("module", "ai/tradingagents")

	// Build request
	reqBody := TARequest{
		Ticker: ticker,
		Date:   date,
	}

	// Add custom LLM config if base URL is set
	if c.llmBaseURL != "" {
		reqBody.LLMConfig = LLMConfig{
			BaseURL: c.llmBaseURL,
			APIKey: c.apiKey,
			Model:  c.model,
		}
	}

	requestJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	url := c.baseURL + "/analyze"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(requestJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Call TradingAgents
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call TradingAgents: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		logger.WithField("status", resp.StatusCode).WithField("body", string(body)).Error("TradingAgents API error")
		return nil, fmt.Errorf("TradingAgents API error: %s", string(body))
	}

	var taResp TAResponse
	if err := json.NewDecoder(resp.Body).Decode(&taResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	logger.WithField("ticker", ticker).WithField("action", taResp.Action).
		WithField("confidence", taResp.Confidence).Info("TradingAgents analysis completed")

	return &taResp, nil
}

// HealthCheck checks if TradingAgents service is available
// Nama Function: HealthCheck
// Deskripsi: Memeriksa apakah TradingAgents service tersedia.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk HTTP request
// Function yang Dipanggil/Dikonsumsi:
//   - httpClient.Do: dipanggil untuk health check
// Output/Return Value:
//   - bool: true jika service tersedia
//   - error: error jika check gagal
func (c *TradingAgentsClient) HealthCheck(ctx context.Context) (bool, error) {
	url := c.baseURL + "/health"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK, nil
}

// Decide adalah wrapper untuk Analyze yang menghasilkan CoordinatorDecision.
// Ini membuat TradingAgentsClient bisa digunakan sebagai drop-in replacement untuk Coordinator.
// Nama Function: Decide
// Deskripsi: Wrapper untuk Analyze yang menghasilkan CoordinatorDecision yang kompatibel.
// Convert response dari TradingAgents ke format CoordinatorDecision untuk orchestrator.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk HTTP request
//   - symbol: string — symbol trading (e.g., BTCUSDT)
//   - marketData: map[string]interface{} — data market (tidak digunakan langsung oleh TradingAgents)
//   - systemPrompt: string — system prompt (tidak digunakan, sudah ada di TradingAgents)
// Function yang Dipanggil/Dikonsumsi:
//   - Analyze: dipanggil untuk analisis ticker via TradingAgents
//   - convertToCoordinatorDecision: dipanggil untuk convert response
// Output/Return Value:
//   - *CoordinatorDecision: keputusan AI dalam format CoordinatorDecision
//   - error: error jika analisis gagal
func (c *TradingAgentsClient) Decide(ctx context.Context, symbol string, marketData map[string]interface{}, systemPrompt string) (*CoordinatorDecision, error) {
	// Convert symbol dari Binance format (BTCUSDT) ke TradingAgents format (BTC-USD)
	ticker := convertSymbolToTradingAgents(symbol)

	// Get current date
	date := time.Now().UTC().Format("2006-01-02")

	// Call TradingAgents
	taResp, err := c.Analyze(ctx, ticker, date)
	if err != nil {
		return nil, fmt.Errorf("TradingAgents analysis failed: %w", err)
	}

	// Convert to CoordinatorDecision
	return c.convertToCoordinatorDecision(symbol, taResp), nil
}

// convertToCoordinatorDecision converts TAResponse to CoordinatorDecision
func (c *TradingAgentsClient) convertToCoordinatorDecision(symbol string, taResp *TAResponse) *CoordinatorDecision {
	decision := CoordinatorDecision{
		MarketContext: MarketContext{
			Regime:           getRegimeFromAction(taResp.Action),
			OverallSentiment: taResp.Reasoning,
		},
		TopPairs:      []string{symbol},
		TradeDecision: taResp.Action,
		RiskAssessment: RiskAssessment{
			Level: getRiskLevel(taResp.Confidence),
		},
		Confidence: decimal.NewFromFloat(taResp.Confidence),
	}

	// Set execution plan
	if taResp.StopLoss > 0 {
		decision.ExecutionPlan = ExecutionPlan{
			OrderType: "market",
			StopLoss:  decimal.NewFromFloat(taResp.StopLoss),
		}
	}

	// Set take profit levels
	for i, tp := range taResp.TakeProfitTargets {
		percent := float64((i + 1) * 25) // Distribute across TP targets
		decision.ExecutionPlan.TakeProfitLevels = append(decision.ExecutionPlan.TakeProfitLevels, TakeProfitLevel{
			TargetPercent:   percent,
			QuantityPercent: tp,
		})
	}

	return &decision
}

// convertSymbolToTradingAgents converts Binance symbol format to TradingAgents format
// BTCUSDT -> BTC-USD, ETHUSDT -> ETH-USD
func convertSymbolToTradingAgents(symbol string) string {
	if len(symbol) > 4 && symbol[len(symbol)-4:] == "USDT" {
		return symbol[:len(symbol)-4] + "-USD"
	}
	if len(symbol) > 3 && symbol[len(symbol)-3:] == "BTC" {
		return symbol[:len(symbol)-3] + "-BTC"
	}
	if len(symbol) > 3 && symbol[len(symbol)-3:] == "ETH" {
		return symbol[:len(symbol)-3] + "-ETH"
	}
	if len(symbol) > 3 && symbol[len(symbol)-3:] == "BNB" {
		return symbol[:len(symbol)-3] + "-BNB"
	}
	return symbol
}

// getRegimeFromAction determines market regime from action
func getRegimeFromAction(action string) string {
	switch action {
	case "BUY":
		return "bull"
	case "SELL":
		return "bear"
	default:
		return "crab"
	}
}

// getRiskLevel determines risk level from confidence
func getRiskLevel(confidence float64) string {
	if confidence >= 80 {
		return "low"
	} else if confidence >= 60 {
		return "medium"
	} else if confidence >= 40 {
		return "high"
	}
	return "extreme"
}
