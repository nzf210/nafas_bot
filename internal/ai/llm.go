// ============================================================
// MODULE: ai
// Deskripsi: Direct LLM client — replacement untuk TradingAgents
// Prinsip diadopsi dari Meridian (nzf210/meridian):
// - Direct LLM call via OpenAI SDK (tanpa service terpisah)
// - Support OpenRouter, Ollama, LM Studio via custom base URL
// - Provider fallback + retry logic
// ============================================================

package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nzf210/nafas-bot/internal/logger"
	"github.com/shopspring/decimal"
)

// LLMClient adalah direct LLM client (replacement untuk TradingAgentsClient)
// Support OpenAI-compatible providers: OpenAI, OpenRouter, Ollama, LM Studio
// Nama Function: LLMClient
// Deskripsi: Direct LLM client dengan OpenAI-compatible API support.
// Parameter/Value Input:
//   - baseURL: string — LLM provider base URL (default: OpenRouter)
//   - apiKey: string — API key untuk provider
//   - model: string — model name (e.g., gpt-4o, openrouter/anthropic/claude-3.5-sonnet)
// Function yang Dipanggil/Dikonsumsi:
//   - Decide: dipanggil untuk generate trading decision
//   - HealthCheck: dipanggil untuk cek koneksi provider
// Output/Return Value:
//   - LLMClient: struct client
type LLMClient struct {
	baseURL     string
	apiKey     string
	model      string
	temperature float64
	maxTokens int
	httpClient *http.Client
}

// NewLLMClient creates a new direct LLM client
// Nama Function: NewLLMClient
// Deskripsi: Membuat instance LLM client baru.
// Parameter/Value Input:
//   - baseURL: string — LLM provider base URL
//   - apiKey: string — API key
//   - model: string — model name
//   - temperature: float64 — sampling temperature (default: 0.3)
//   - maxTokens: int — max response tokens (default: 1024)
// Output/Return Value:
//   - *LLMClient: pointer ke client
func NewLLMClient(baseURL, apiKey, model string, temperature float64, maxTokens int) *LLMClient {
	if temperature == 0 {
		temperature = 0.3
	}
	if maxTokens == 0 {
		maxTokens = 1024
	}
	return &LLMClient{
		baseURL:     baseURL,
		apiKey:     apiKey,
		model:      model,
		temperature: temperature,
		maxTokens:   maxTokens,
		httpClient: &http.Client{Timeout: 5 * time.Minute},
	}
}

// HealthCheck checks if LLM provider is available
// Nama Function: HealthCheck
// Deskripsi: Memeriksa apakah LLM provider tersedia.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk HTTP request
// Output/Return Value:
//   - bool: true jika provider tersedia
//   - error: error jika check gagal
func (c *LLMClient) HealthCheck(ctx context.Context) (bool, error) {
	// Simple check: try to call the API with a minimal request
	reqBody := map[string]any{
		"model": c.model,
		"messages": []any{
			map[string]string{"role": "user", "content": "hi"},
		},
		"max_tokens": 5,
	}

	requestJSON, err := json.Marshal(reqBody)
	if err != nil {
		return false, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewBuffer(requestJSON))
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to connect to LLM provider: %w", err)
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK, nil
}

// Decide generates a trading decision using direct LLM call
// Uses compact prompt format to minimize token usage (~50% reduction).
// Nama Function: Decide
// Deskripsi: Generate trading decision via direct LLM call.
// Convert response ke CoordinatorDecision untuk kompatibilitas dengan orchestrator.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk HTTP request
//   - symbol: string — trading symbol (e.g., BTCUSDT)
//   - marketData: map[string]any — market data dari scanner
//   - systemPrompt: string — system prompt (unused, prompt built internally)
// Function yang Dipanggil/Dikonsumsi:
//   - buildPromptCompact: dipanggil untuk build compact prompt (token-optimized)
//   - callLLM: dipanggil untuk kirim request ke LLM provider
//   - parseLLMResponse: dipanggil untuk parse response
// Output/Return Value:
//   - *CoordinatorDecision: keputusan trading dalam format CoordinatorDecision
//   - error: error jika decision gagal
func (c *LLMClient) Decide(ctx context.Context, symbol string, marketData map[string]any, systemPrompt string) (*CoordinatorDecision, error) {
	log := logger.Default().WithField("module", "ai/llm")

	// Build compact prompt to minimize token usage
	prompt := c.buildPromptCompact(symbol, marketData)

	// Call LLM with retry logic
	response, err := c.callLLM(ctx, prompt)
	if err != nil {
		log.WithError(err).Error("LLM call failed")
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	// Parse response to CoordinatorDecision
	decision, err := c.parseLLMResponse(symbol, response)
	if err != nil {
		log.WithError(err).WithField("response", response).Error("Failed to parse LLM response")
		return nil, fmt.Errorf("failed to parse LLM response: %w", err)
	}

	log.WithField("symbol", symbol).
		WithField("decision", decision.TradeDecision).
		WithField("confidence", decision.Confidence.String()).
		Info("LLM decision generated")

	return decision, nil
}

// buildPromptCompact constructs a compact analysis prompt to minimize token usage.
// Only last 10 candles are sent, and field names are shortened.
func (c *LLMClient) buildPromptCompact(symbol string, marketData map[string]any) string {
	price := getStringField(marketData, "latest_price", "N/A")
	volume := getStringField(marketData, "volume_24h", "N/A")
	rsi := getStringField(marketData, "rsi", "N/A")
	trend := getStringField(marketData, "trend", "N/A")

	// Compress candles to only last 10
	candlesStr := compressCandlesToString(getCandles(marketData), 10)

	return fmt.Sprintf(`NAFAS %s|P=%s V=%s RSI=%s T=%s|C:%s|JSON:{"a":"","c":0,"r":"","sl":0,"tp":[0],"rl":""}`,
		symbol, price, volume, rsi, trend, candlesStr)
}

// compressCandlesToString compresses candles to a compact string format.
// Only last N candles are included to minimize token usage.
// Uses scientific notation for large numbers to reduce token usage.
func compressCandlesToString(candles any, limit int) string {
	list, ok := candles.([]any)
	if !ok || len(list) == 0 {
		return "[]"
	}

	// Take only last N candles
	start := 0
	if len(list) > limit {
		start = len(list) - limit
	}

	var parts []string
	for i := start; i < len(list); i++ {
		if candle, ok := list[i].(map[string]any); ok {
			o := compressNumber(getMapString(candle, "open", "0"))
			h := compressNumber(getMapString(candle, "high", "0"))
			l := compressNumber(getMapString(candle, "low", "0"))
			c := compressNumber(getMapString(candle, "close", "0"))
			v := compressNumber(getMapString(candle, "volume", "0"))
			parts = append(parts, fmt.Sprintf("[%s,%s,%s,%s,%s]", o, h, l, c, v))
		}
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// compressNumber converts large numbers to compact/scientific notation
// e.g., "1000000" → "1M", "0.00005234" → "5.2e-5"
func compressNumber(s string) string {
	// Try to parse as float
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}

	// Large numbers: use K/M/B notation
	if f >= 1e9 {
		return fmt.Sprintf("%.1fB", f/1e9)
	}
	if f >= 1e6 {
		return fmt.Sprintf("%.1fM", f/1e6)
	}
	if f >= 1e3 {
		return fmt.Sprintf("%.1fK", f/1e3)
	}

	// Small numbers: use scientific notation
	if f > 0 && f < 1e-4 {
		return fmt.Sprintf("%.1e", f)
	}

	// Regular: limit decimal places
	return fmt.Sprintf("%.4g", f)
}

// getMapString safely extracts a string from a map
func getMapString(m map[string]any, key, fallback string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return fallback
}

// getCandles extracts candles from market data map
func getCandles(m map[string]any) any {
	if v, ok := m["candles"]; ok {
		return v
	}
	return nil
}

// callLLM sends request to LLM provider with retry logic
// Nama Function: callLLM
// Deskripsi: Mengirim request ke LLM provider dengan retry logic.
// Retry up to 3 times pada transient errors (502, 503, 529).
// Parameter/Value Input:
//   - ctx: context.Context — context untuk HTTP request
//   - prompt: string — user prompt
// Output/Return Value:
//   - string: LLM response text
//   - error: error jika semua retry gagal
func (c *LLMClient) callLLM(ctx context.Context, prompt string) (string, error) {
	requestBody := map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"temperature": c.temperature,
		"max_tokens":  256, // Optimized: response is simple JSON, no need for 1024+
	}

	requestJSON, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Retry logic (up to 3 attempts)
	var lastErr error
	for attempt := range 3 {
		if attempt > 0 {
			wait := time.Duration(attempt*5000) * time.Millisecond
			time.Sleep(wait)
		}

		req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewBuffer(requestJSON))
		if err != nil {
			lastErr = fmt.Errorf("failed to create request: %w", err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		if c.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("failed to call LLM: %w", err)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("failed to read response: %w", err)
			continue
		}

		// Check for transient errors
		if resp.StatusCode == 502 || resp.StatusCode == 503 || resp.StatusCode == 529 {
			lastErr = fmt.Errorf("transient error: %d", resp.StatusCode)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("LLM API error (status %d): %s", resp.StatusCode, string(body))
		}

		// Parse response
		var apiResp chatCompletionResponse
		if err := json.Unmarshal(body, &apiResp); err != nil {
			return "", fmt.Errorf("failed to parse response: %w", err)
		}

		if len(apiResp.Choices) == 0 {
			return "", fmt.Errorf("no choices in LLM response")
		}

		return apiResp.Choices[0].Message.Content, nil
	}

	return "", fmt.Errorf("LLM call failed after 3 attempts: %w", lastErr)
}

// chatCompletionResponse adalah response dari OpenAI-compatible chat completions API
type chatCompletionResponse struct {
	ID      string `json:"id"`
	Object string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int `json:"index"`
		Message      struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// parseLLMResponse converts LLM text response to CoordinatorDecision
// Nama Function: parseLLMResponse
// Deskripsi: Parse response text LLM ke CoordinatorDecision format.
// Extract JSON dari response, handle markdown code blocks jika ada.
// Parameter/Value Input:
//   - symbol: string — trading symbol
//   - response: string — raw LLM response
// Output/Return Value:
//   - *CoordinatorDecision: parsed decision
//   - error: error jika parse gagal
func (c *LLMClient) parseLLMResponse(symbol, response string) (*CoordinatorDecision, error) {
	// Extract JSON from response (handle markdown code blocks)
	jsonStr := extractJSON(response)
	if jsonStr == "" {
		// Fallback: assume it's a HOLD decision if we can't parse
		return &CoordinatorDecision{
			MarketContext: MarketContext{
				Regime:           "unknown",
				OverallSentiment: "Could not parse LLM response",
			},
			TopPairs:      []string{symbol},
			TradeDecision: "HOLD",
			RiskAssessment: RiskAssessment{
				Level: "medium",
			},
			Confidence: decimal.NewFromFloat(50),
		}, nil
	}

	var parsed parsedLLMResponse
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		// Fallback: try to extract key fields with regex
		return c.parseWithRegex(symbol, response)
	}

	// Convert to CoordinatorDecision
	decision := &CoordinatorDecision{
		MarketContext: MarketContext{
			Regime:           getRegimeFromAction(parsed.Action),
			OverallSentiment: parsed.Reasoning,
		},
		TopPairs:      []string{symbol},
		TradeDecision: parsed.Action,
		RiskAssessment: RiskAssessment{
			Level: parsed.RiskLevel,
		},
		Confidence: decimal.NewFromFloat(parsed.Confidence),
	}

	// Set execution plan
	if parsed.StopLoss > 0 {
		decision.ExecutionPlan = ExecutionPlan{
			OrderType: "market",
			StopLoss:  decimal.NewFromFloat(parsed.StopLoss),
		}
	}

	// Set take profit levels
	for i, tp := range parsed.TakeProfitTargets {
		percent := float64((i + 1) * 25)
		decision.ExecutionPlan.TakeProfitLevels = append(decision.ExecutionPlan.TakeProfitLevels, TakeProfitLevel{
			TargetPercent:   percent,
			QuantityPercent: tp,
		})
	}

	return decision, nil
}

// parsedLLMResponse adalah intermediate struct untuk parse JSON response
type parsedLLMResponse struct {
	Action            string   `json:"action"`
	Confidence        float64  `json:"confidence"`
	Reasoning         string   `json:"reasoning"`
	StopLoss          float64  `json:"stop_loss"`
	TakeProfitTargets []float64 `json:"take_profit_targets"`
	RiskLevel         string   `json:"risk_level"`
}

// parseWithRegex extracts decision fields using regex when JSON parse fails
func (c *LLMClient) parseWithRegex(symbol, response string) (*CoordinatorDecision, error) {
	decision := &CoordinatorDecision{
		TopPairs:      []string{symbol},
		TradeDecision: "HOLD",
		RiskAssessment: RiskAssessment{
			Level: "medium",
		},
		Confidence: decimal.NewFromFloat(50),
	}

	// Extract action
	actionRe := regexp.MustCompile(`"action"\s*:\s*"?(BUY|SELL|HOLD)`)
	if matches := actionRe.FindStringSubmatch(response); len(matches) > 1 {
		decision.TradeDecision = matches[1]
	}

	// Extract confidence
	confRe := regexp.MustCompile(`"confidence"\s*:\s*(\d+\.?\d*)`)
	if matches := confRe.FindStringSubmatch(response); len(matches) > 1 {
		if conf, err := strconv.ParseFloat(matches[1], 64); err == nil {
			decision.Confidence = decimal.NewFromFloat(conf)
		}
	}

	// Extract stop loss
	slRe := regexp.MustCompile(`"stop_loss"\s*:\s*(\d+\.?\d*)`)
	if matches := slRe.FindStringSubmatch(response); len(matches) > 1 {
		if sl, err := strconv.ParseFloat(matches[1], 64); err == nil && sl > 0 {
			decision.ExecutionPlan = ExecutionPlan{
				OrderType: "market",
				StopLoss:  decimal.NewFromFloat(sl),
			}
		}
	}

	// Extract risk level
	riskRe := regexp.MustCompile(`"risk_level"\s*:\s*"?(low|medium|high|extreme)`)
	if matches := riskRe.FindStringSubmatch(response); len(matches) > 1 {
		decision.RiskAssessment.Level = matches[1]
	}

	// Extract reasoning
	reasonRe := regexp.MustCompile(`"reasoning"\s*:\s*"([^"]+)"`)
	if matches := reasonRe.FindStringSubmatch(response); len(matches) > 1 {
		decision.MarketContext.OverallSentiment = matches[1]
	}

	return decision, nil
}

// extractJSON extracts JSON from text that may contain markdown code blocks
func extractJSON(text string) string {
	// Try to find JSON in markdown code blocks
	codeBlockRe := regexp.MustCompile("```(?:json)?\\s*([\\s\\S]+?)```")
	if matches := codeBlockRe.FindStringSubmatch(text); len(matches) > 1 {
		return matches[1]
	}

	// Try to find raw JSON object
	jsonRe := regexp.MustCompile(`\{[\s\S]*\}`)
	if matches := jsonRe.FindString(text); matches != "" {
		return matches
	}

	return ""
}

// getStringField safely extracts a string field from map
func getStringField(m map[string]any, key, fallback string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return fallback
}

// AIClient adalah interface untuk AI decision client
// Bisa diimplementasi oleh LLMClient (direct call), MultiLLMClient (main+fallback), atau TradingAgentsClient (service call)
type AIClient interface {
	Decide(ctx context.Context, symbol string, marketData map[string]any, systemPrompt string) (*CoordinatorDecision, error)
	HealthCheck(ctx context.Context) (bool, error)
}
