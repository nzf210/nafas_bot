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
)

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
	return&TradingAgentsClient{
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
