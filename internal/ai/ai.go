// ============================================================
// MODULE: ai
// Deskripsi: AI Coordinator dan multi-agent orchestration
// ============================================================

package ai

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nzf210/nafas-bot/internal/logger"
	"github.com/nzf210/nafas-bot/internal/models"
	"github.com/shopspring/decimal"
)

// Coordinator orchestrates AI agents
// Nama Function: Coordinator
// Deskripsi: AI Coordinator - orchestrator utama untuk multi-agent system.
// Parameter/Value Input:
//   - db: *sql.DB — koneksi database untuk store decisions
//   - apiKey: string — LLM API key
//   - apiURL: string — LLM provider URL
//   - model: string — model name
//   - logger: *logger.Logger — logger instance
type Coordinator struct {
	db      *sql.DB
	apiKey  string
	apiURL  string
	model   string
	logger  *logger.Logger
	httpClient *http.Client
}

// NewCoordinator creates a new AI coordinator
// Nama Function: NewCoordinator
// Deskripsi: Membuat instance AI Coordinator baru.
// Parameter/Value Input:
//   - db: *sql.DB — koneksi database
//   - apiKey: string — LLM API key
//   - apiURL: string — LLM provider URL
//   - model: string — model name
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung, hanya inisialisasi
// Output/Return Value:
//   - *Coordinator: pointer ke coordinator
func NewCoordinator(db *sql.DB, apiKey, apiURL, model string) *Coordinator {
	return &Coordinator{
		db:      db,
		apiKey:  apiKey,
		apiURL:  apiURL,
		model:   model,
		logger:  logger.Default().WithField("module", "ai/coordinator"),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// CoordinatorDecision represents AI Coordinator output
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
//   - CallLLM: dipanggil untuk generate decision
// Output/Return Value:
//   - CoordinatorDecision: struct decision lengkap
type CoordinatorDecision struct {
	MarketContext   MarketContext   `json:"market_context"`
	TopPairs        []string        `json:"top_pairs"`
	TradeDecision   string          `json:"trade_decision"`
	RiskAssessment  RiskAssessment  `json:"risk_assessment"`
	ExecutionPlan   ExecutionPlan   `json:"execution_plan"`
	Confidence      decimal.Decimal `json:"confidence"`
}

// MarketContext represents current market conditions
// Nama Function: MarketContext
// Deskripsi: Struct context pasar saat ini.
// Parameter/Value Input:
//   - Regime: string — bull, bear, atau crab
//   - BTCTrend: string — trend BTC (bullish, bearish, neutral)
//   - ETHTrend: string — trend ETH
//   - BTCDominance: decimal.Decimal — BTC dominance percentage
//   - FearGreedIndex: int — fear & greed index
//   - OverallSentiment: string — sentiment keseluruhan
// Function yang Dipanggil/Dikonsumsi:
//   - marketAnalyst.Analyze: dipanggil untuk generate context
// Output/Return Value:
//   - MarketContext: struct context pasar
type MarketContext struct {
	Regime           string          `json:"regime"`
	BTCTrend         string          `json:"btc_trend"`
	ETHTrend         string          `json:"eth_trend"`
	BTCDominance     decimal.Decimal `json:"btc_dominance"`
	FearGreedIndex   int             `json:"fear_greed_index"`
	OverallSentiment string          `json:"overall_sentiment"`
}

// RiskAssessment represents AI risk assessment
// Nama Function: RiskAssessment
// Deskripsi: Struct assessment risk dari AI.
// Parameter/Value Input:
//   - Level: string — low, medium, high, extreme
//   - PositionSizeMultiplier: float64 — multiplier untuk position size
//   - KeyRisks: []string — list risk utama
//   - Recommendations: []string — rekomendasi risk management
// Function yang Dipanggil/Dikonsumsi:
//   - risk_guardian.CheckTrade: dipanggil untuk validasi final
// Output/Return Value:
//   - RiskAssessment: struct assessment
type RiskAssessment struct {
	Level                  string    `json:"level"`
	PositionSizeMultiplier float64   `json:"position_size_multiplier"`
	KeyRisks               []string  `json:"key_risks"`
	Recommendations        []string  `json:"recommendations"`
}

// ExecutionPlan represents trade execution plan
// Nama Function: ExecutionPlan
// Deskripsi: Struct rencana eksekusi trade.
// Parameter/Value Input:
//   - OrderType: string — market, limit, atau twap
//   - EntryPrice: decimal.Decimal — target harga masuk
//   - StopLoss: decimal.Decimal — stop loss price
//   - TakeProfitLevels: []TakeProfitLevel — level take profit
//   - MaxSlippage: decimal.Decimal — max slippage yang diterima
// Function yang Dipanggil/Dikonsumsi:
//   - execution.PlaceOrder: dipanggil untuk eksekusi plan
// Output/Return Value:
//   - ExecutionPlan: struct plan eksekusi
type ExecutionPlan struct {
	OrderType        string             `json:"order_type"`
	EntryPrice       decimal.Decimal    `json:"entry_price"`
	StopLoss         decimal.Decimal    `json:"stop_loss"`
	TakeProfitLevels  []TakeProfitLevel  `json:"take_profit_levels"`
	MaxSlippage      decimal.Decimal    `json:"max_slippage"`
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

// Decide generates AI decision for a symbol
// Nama Function: Decide
// Deskripsi: Menghasilkan keputusan AI untuk symbol tertentu.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk HTTP request
//   - symbol: string — symbol trading
//   - marketData: map[string]interface{} — data market untuk analisis
//   - systemPrompt: string — system prompt untuk AI
// Function yang Dipanggil/Dikonsumsi:
//   - CallLLM: dipanggil untuk generate decision dari AI
//   - LogDecision: dipanggil untuk store decision ke database
// Output/Return Value:
//   - *CoordinatorDecision: keputusan AI
//   - error: error jika decision gagal
func (c *Coordinator) Decide(ctx context.Context, symbol string, marketData map[string]interface{}, systemPrompt string) (*CoordinatorDecision, error) {
	// Prepare input context
	inputContext := map[string]interface{}{
		"symbol":     symbol,
		"market_data": marketData,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}
	inputJSON, _ := json.Marshal(inputContext)

	// Call LLM
	prompt := c.buildPrompt(symbol, marketData, systemPrompt)
	response, err := c.CallLLM(ctx, systemPrompt, prompt)
	if err != nil {
		return nil, fmt.Errorf("failed to call LLM: %w", err)
	}

	// Parse response
	var decision CoordinatorDecision
	if err := json.Unmarshal([]byte(response), &decision); err != nil {
		// Try to extract JSON from response
		decision = c.parseFlexibleResponse(response, symbol)
	}

	// Log decision
	c.logDecision(ctx, symbol, inputJSON, decision)

	return &decision, nil
}

// CallLLM calls the LLM API
// Nama Function: CallLLM
// Deskripsi: Memanggil LLM API untuk generate text.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk HTTP request
//   - systemPrompt: string — system prompt
//   - userPrompt: string — user prompt dengan data
// Function yang Dipanggil/Dikonsumsi:
//   - httpClient.Do: dipanggil untuk kirim request ke LLM API
//   - json.Marshal/Unmarshal: dipanggil untuk serialize/deserialize request
// Output/Return Value:
//   - string: response text dari LLM
//   - error: error jika call gagal
func (c *Coordinator) CallLLM(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	requestBody := map[string]interface{}{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature": 0.3, // Low temperature for trading decisions
		"response_format": map[string]string{"type": "json_object"},
	}
	requestJSON, _ := json.Marshal(requestBody)

	url := c.apiURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(requestJSON))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call LLM: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("LLM API error: %s", string(body))
	}

	var llmResponse struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&llmResponse); err != nil {
		return "", fmt.Errorf("failed to decode LLM response: %w", err)
	}

	if len(llmResponse.Choices) == 0 {
		return "", fmt.Errorf("no choices in LLM response")
	}

	return llmResponse.Choices[0].Message.Content, nil
}

// buildPrompt constructs the prompt for AI
func (c *Coordinator) buildPrompt(symbol string, marketData map[string]interface{}, basePrompt string) string {
	dataJSON, _ := json.Marshal(marketData)
	return fmt.Sprintf(`Analyze the following market data for %s and provide a trading decision.

Market Data:
%s

Output a JSON object with the following structure:
{
  "market_context": {
    "regime": "bull|bear|crab",
    "btc_trend": "bullish|bearish|neutral",
    "eth_trend": "bullish|bearish|neutral",
    "btc_dominance": 52.5,
    "fear_greed_index": 65,
    "overall_sentiment": "string"
  },
  "top_pairs": ["BTCUSDT", "ETHUSDT"],
  "trade_decision": "buy|sell|hold|skip",
  "risk_assessment": {
    "level": "low|medium|high|extreme",
    "position_size_multiplier": 1.0,
    "key_risks": ["risk1", "risk2"],
    "recommendations": ["rec1", "rec2"]
  },
  "execution_plan": {
    "order_type": "market|limit|twap",
    "entry_price": 0,
    "stop_loss": 0,
    "take_profit_levels": [
      {"target_percent": 3, "quantity_percent": 33},
      {"target_percent": 5, "quantity_percent": 33},
      {"target_percent": 8, "quantity_percent": 34}
    ],
    "max_slippage": 0.5
  },
  "confidence": 75
}`, symbol, string(dataJSON))
}

// parseFlexibleResponse parses response when strict JSON fails
func (c *Coordinator) parseFlexibleResponse(response, symbol string) CoordinatorDecision {
	// Default to hold with low confidence if parsing fails
	return CoordinatorDecision{
		MarketContext: MarketContext{
			Regime:           "unknown",
			OverallSentiment: "unknown",
		},
		TopPairs:      []string{},
		TradeDecision: "skip",
		RiskAssessment: RiskAssessment{
			Level: "medium",
		},
		Confidence: decimal.NewFromFloat(0),
	}
}

// logDecision stores decision to database
func (c *Coordinator) logDecision(ctx context.Context, symbol string, inputJSON []byte, decision CoordinatorDecision) {
	outputJSON, _ := json.Marshal(decision)

	// Get provider ID (placeholder)
	var providerID models.UUID

	query := `
		INSERT INTO ai_decisions (provider_id, symbol, decision, confidence, input_context, output_context)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := c.db.ExecContext(ctx, query, providerID, symbol, decision.TradeDecision, decision.Confidence, inputJSON, outputJSON)
	if err != nil {
		c.logger.Warnf("Failed to log decision: %v", err)
	}
}