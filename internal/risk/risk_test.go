// ============================================================
// MODULE: risk
// Deskripsi: Unit tests untuk risk guardian
// ============================================================

package risk

import (
	"testing"

	"github.com/google/uuid"
	"github.com/nzf210/nafas-bot/internal/models"
	"github.com/shopspring/decimal"
)

func newTestUser(status string, wchBalance decimal.Decimal) *models.User {
	return &models.User{
		ID:         uuid.New(),
		TelegramID: 123456,
		Status:     status,
		WCHBalance: wchBalance,
	}
}

func newTestConfig(dailyLossLimit, maxRiskPerTrade decimal.Decimal, maxOpenPositions int) *models.UserConfig {
	return &models.UserConfig{
		ID:               uuid.New(),
		UserID:           uuid.New(),
		DailyLossLimit:   dailyLossLimit,
		MaxRiskPerTrade:  maxRiskPerTrade,
		MaxOpenPositions: maxOpenPositions,
	}
}

func newTestOrder(symbol string) *models.Order {
	return &models.Order{
		ID:       uuid.New(),
		Symbol:   symbol,
		Side:     "buy",
		Quantity: decimal.NewFromFloat(0.005),
	}
}

func TestCheckTrade_Approved(t *testing.T) {
	g := NewGuardian()

	user := newTestUser("active", decimal.Zero)
	config := newTestConfig(decimal.NewFromFloat(5.0), decimal.NewFromFloat(2.0), 5)
	order := newTestOrder("BTCUSDT")

	result := g.CheckTrade(user, config, order, 2, decimal.Zero)
	if !result.Approved {
		t.Errorf("Expected trade to be approved, got rejected: %v", result.Reason)
	}
}

func TestCheckTrade_DailyLossLimitExceeded(t *testing.T) {
	g := NewGuardian()

	user := newTestUser("active", decimal.Zero)
	config := newTestConfig(decimal.NewFromFloat(2.0), decimal.NewFromFloat(2.0), 5)
	order := newTestOrder("BTCUSDT")

	result := g.CheckTrade(user, config, order, 2, decimal.NewFromFloat(3.0))
	if result.Approved {
		t.Error("Expected trade to be rejected due to daily loss limit")
	}
	if !result.Blocked {
		t.Error("Expected trade to be blocked, not just rejected")
	}
}

func TestCheckTrade_MaxPositionsReached(t *testing.T) {
	g := NewGuardian()

	user := newTestUser("active", decimal.Zero)
	config := newTestConfig(decimal.NewFromFloat(5.0), decimal.NewFromFloat(2.0), 3)
	order := newTestOrder("ETHUSDT")

	result := g.CheckTrade(user, config, order, 3, decimal.Zero)
	if result.Approved {
		t.Error("Expected trade to be rejected due to max positions")
	}
	if !result.Blocked {
		t.Error("Expected trade to be blocked, not just rejected")
	}
}

func TestCheckTrade_InactiveUser(t *testing.T) {
	g := NewGuardian()

	user := newTestUser("suspended", decimal.Zero)
	config := newTestConfig(decimal.NewFromFloat(5.0), decimal.NewFromFloat(2.0), 5)
	order := newTestOrder("BTCUSDT")

	result := g.CheckTrade(user, config, order, 1, decimal.Zero)
	if result.Approved {
		t.Error("Expected trade to be rejected due to inactive user")
	}
	if !result.Blocked {
		t.Error("Expected trade to be blocked, not just rejected")
	}
}

func TestCheckTrade_WCHMultiplier(t *testing.T) {
	g := NewGuardian()

	user := newTestUser("active", decimal.NewFromFloat(1000))
	config := newTestConfig(decimal.NewFromFloat(5.0), decimal.NewFromFloat(2.0), 5)
	order := newTestOrder("BTCUSDT")

	result := g.CheckTrade(user, config, order, 1, decimal.Zero)
	if !result.Approved {
		t.Errorf("Expected trade to be approved, got: %v", result.Reason)
	}
	if result.PositionSizeMultiplier < 1.0 {
		t.Errorf("Expected PositionSizeMultiplier >= 1.0, got %v", result.PositionSizeMultiplier)
	}
}

func TestCheckMaxPositionSize_BTC(t *testing.T) {
	g := NewGuardian()

	tests := []struct {
		name     string
		quantity decimal.Decimal
		want     bool
	}{
		{"exact limit", decimal.NewFromFloat(0.01), true},
		{"below limit", decimal.NewFromFloat(0.005), true},
		{"above limit", decimal.NewFromFloat(0.02), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, maxSize := g.CheckMaxPositionSize("BTC", tt.quantity)
			if ok != tt.want {
				t.Errorf("CheckMaxPositionSize(BTC, %v) = %v, want %v", tt.quantity, ok, tt.want)
			}
			expectedMax := decimal.NewFromFloat(0.01)
			if !maxSize.Equal(expectedMax) {
				t.Errorf("CheckMaxPositionSize(BTC, _) maxSize = %v, want %v", maxSize, expectedMax)
			}
		})
	}
}

func TestCheckMaxPositionSize_ETH(t *testing.T) {
	g := NewGuardian()

	tests := []struct {
		name     string
		quantity decimal.Decimal
		want     bool
	}{
		{"exact limit", decimal.NewFromFloat(0.1), true},
		{"below limit", decimal.NewFromFloat(0.05), true},
		{"above limit", decimal.NewFromFloat(0.2), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, _ := g.CheckMaxPositionSize("ETH", tt.quantity)
			if ok != tt.want {
				t.Errorf("CheckMaxPositionSize(ETH, %v) = %v, want %v", tt.quantity, ok, tt.want)
			}
		})
	}
}

func TestCheckMaxPositionSize_SOL(t *testing.T) {
	g := NewGuardian()

	tests := []struct {
		name     string
		quantity decimal.Decimal
		want     bool
	}{
		{"exact limit", decimal.NewFromFloat(1.0), true},
		{"below limit", decimal.NewFromFloat(0.5), true},
		{"above limit", decimal.NewFromFloat(2.0), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, _ := g.CheckMaxPositionSize("SOL", tt.quantity)
			if ok != tt.want {
				t.Errorf("CheckMaxPositionSize(SOL, %v) = %v, want %v", tt.quantity, ok, tt.want)
			}
		})
	}
}

func TestCheckMaxPositionSize_UnknownAsset(t *testing.T) {
	g := NewGuardian()

	ok, maxSize := g.CheckMaxPositionSize("UNKNOWN", decimal.NewFromFloat(100))
	if ok {
		t.Error("Expected false for unknown asset")
	}
	if !maxSize.IsZero() {
		t.Errorf("Expected zero maxSize for unknown asset, got %v", maxSize)
	}
}

func TestCalculatePositionSize(t *testing.T) {
	g := NewGuardian()

	tests := []struct {
		name            string
		portfolioValue   decimal.Decimal
		riskPercent     decimal.Decimal
		stopLossPercent decimal.Decimal
	}{
		{"standard", decimal.NewFromFloat(10000), decimal.NewFromFloat(1.0), decimal.NewFromFloat(2.0)},
		{"zero stop loss uses default", decimal.NewFromFloat(10000), decimal.NewFromFloat(1.0), decimal.Zero},
		{"zero risk uses default", decimal.NewFromFloat(10000), decimal.Zero, decimal.NewFromFloat(2.0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size := g.CalculatePositionSize(tt.portfolioValue, tt.riskPercent, tt.stopLossPercent)
			if size.LessThanOrEqual(decimal.Zero) {
				t.Errorf("CalculatePositionSize() returned non-positive: %v", size)
			}
		})
	}
}

func TestRiskAssessment_Fields(t *testing.T) {
	assessment := &RiskAssessment{
		Approved:              true,
		RiskLevel:             RiskLevelMedium,
		PositionSizeMultiplier: 1.25,
		Reason:                "Test approval",
		Blocked:               false,
	}

	if !assessment.Approved {
		t.Error("Expected Approved to be true")
	}
	if assessment.RiskLevel != RiskLevelMedium {
		t.Errorf("Expected RiskLevel Medium, got %v", assessment.RiskLevel)
	}
	if assessment.Blocked {
		t.Error("Expected Blocked to be false")
	}
}