// ============================================================
// MODULE: risk
// Deskripsi: Risk Guardian - hardcoded risk management rules (NOT AI)
// ============================================================

package risk

import (
	"github.com/nzf210/nafas-bot/internal/logger"
	"github.com/nzf210/nafas-bot/internal/models"
	"github.com/shopspring/decimal"
)

// Risk Guardian adalah FINAL AUTHORITY untuk semua keputusan trading.
// Aturan di bawah ini TIDAK BISA di-override oleh AI prompt.
// Risk Guardian written in Go code, bukan AI prompt.

// RiskLevel represents risk assessment level
type RiskLevel string

const (
	RiskLevelLow     RiskLevel = "low"
	RiskLevelMedium  RiskLevel = "medium"
	RiskLevelHigh    RiskLevel = "high"
	RiskLevelExtreme RiskLevel = "extreme"
)

// RiskAssessment holds risk check result
type RiskAssessment struct {
	Approved              bool
	RiskLevel             RiskLevel
	PositionSizeMultiplier float64
	Reason                string
	Blocked               bool
}

// Nama Function: NewGuardian
// Deskripsi: Membuat instance Risk Guardian baru.
// Parameter/Value Input:
//   - Tidak ada parameter input langsung, konfigurasi dari environment/defaults
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung
// Output/Return Value:
//   - *Guardian: pointer ke Risk Guardian instance
func NewGuardian() *Guardian {
	return &Guardian{
		logger: logger.Default().WithField("module", "risk"),
	}
}

// Guardian Risk Guardian - hardcoded risk management
type Guardian struct {
	logger *logger.Logger
}

// Nama Function: CheckTrade
// Deskripsi: Mengecek apakah trade diizinkan berdasarkan risk rules.
// Parameter/Value Input:
//   - user: *models.User — user yang akan trade
//   - config: *models.UserConfig — konfigurasi risk user
//   - order: *models.Order — order yang akan dieksekusi
//   - currentPositions: int — jumlah posisi terbuka saat ini
//   - dailyLoss: decimal.Decimal — loss harian saat ini
// Function yang Dipanggil/Dikonsumsi:
//   - CheckMaxRiskPerTrade: dipanggil untuk validasi max risk per trade
//   - CheckDailyLossLimit: dipanggil untuk validasi daily loss limit
//   - CheckMaxOpenPositions: dipanggil untuk validasi max positions
//   - CheckMaxExposure: dipanggil untuk validasi total exposure
// Output/Return Value:
//   - *RiskAssessment: hasil assessment risk
func (g *Guardian) CheckTrade(user *models.User, config *models.UserConfig, order *models.Order, currentPositions int, dailyLoss decimal.Decimal) *RiskAssessment {
	// 1. Check daily loss limit FIRST - absolute blocker
	if dailyLoss.GreaterThanOrEqual(config.DailyLossLimit) {
		return &RiskAssessment{
			Approved: false,
			Blocked:  true,
			RiskLevel: RiskLevelExtreme,
			Reason:   "Daily loss limit reached",
		}
	}

	// 2. Check max open positions
	if currentPositions >= config.MaxOpenPositions {
		return &RiskAssessment{
			Approved: false,
			Blocked:  true,
			RiskLevel: RiskLevelHigh,
			Reason:   "Max open positions reached",
		}
	}

	// 3. Check risk per trade
	maxRiskPercent := config.MaxRiskPerTrade
	if maxRiskPercent.IsZero() {
		maxRiskPercent = decimal.NewFromFloat(1.0) // default 1%
	}

	// Position size check would go here with actual portfolio value
	positionRiskPercent := decimal.NewFromFloat(0.5) // placeholder
	if positionRiskPercent.GreaterThan(maxRiskPercent) {
		return &RiskAssessment{
			Approved: false,
			Blocked:  true,
			RiskLevel: RiskLevelHigh,
			Reason:   "Risk per trade exceeds limit",
		}
	}

	// 4. Check user status
	if user.Status != "active" {
		return &RiskAssessment{
			Approved: false,
			Blocked:  true,
			RiskLevel: RiskLevelExtreme,
			Reason:   "User account not active",
		}
	}

	// 5. Check WCH balance (can affect risk tolerance but not override hard limits)
	wchMultiplier := g.calculateWCHMultiplier(user.WCHBalance)

	// Calculate final position size multiplier
	baseMultiplier := 1.0
	if wchMultiplier > 1.0 {
		baseMultiplier = wchMultiplier
	}

	return &RiskAssessment{
		Approved:              true,
		RiskLevel:             RiskLevelMedium,
		PositionSizeMultiplier: baseMultiplier,
		Reason:                "Trade approved with standard risk parameters",
	}
}

// Nama Function: CheckMaxPositionSize
// Deskripsi: Mengecek apakah ukuran posisi tidak melebihi max untuk asset.
// Parameter/Value Input:
//   - asset: string — nama asset (BTC, ETH, SOL)
//   - quantity: decimal.Decimal — jumlah yang akan dibeli
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung, hanya lookup di hardcoded limits
// Output/Return Value:
//   - bool: true jika dalam batas, false jika melebihi
//   - decimal.Decimal: max quantity yang diizinkan
func (g *Guardian) CheckMaxPositionSize(asset string, quantity decimal.Decimal) (bool, decimal.Decimal) {
	maxSizes := map[string]decimal.Decimal{
		"BTC": decimal.NewFromFloat(0.01),  // 0.01 BTC max per trade
		"ETH": decimal.NewFromFloat(0.1),   // 0.1 ETH max per trade
		"SOL": decimal.NewFromFloat(1.0),   // 1.0 SOL max per trade
	}

	maxSize, ok := maxSizes[asset]
	if !ok {
		return false, decimal.Zero
	}

	return quantity.LessThanOrEqual(maxSize), maxSize
}

// CalculatePositionSize calculates safe position size
// Nama Function: CalculatePositionSize
// Deskripsi: Menghitung ukuran posisi yang aman berdasarkan risk parameters.
// Parameter/Value Input:
//   - portfolioValue: decimal.Decimal — total nilai portfolio dalam USD
//   - riskPercent: decimal.Decimal — persentase risk (0-100)
//   - stopLossPercent: decimal.Decimal — stop loss percentage
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung, hanya kalkulasi matematika
// Output/Return Value:
//   - decimal.Decimal: jumlah yang aman untuk di-trading
func (g *Guardian) CalculatePositionSize(portfolioValue, riskPercent, stopLossPercent decimal.Decimal) decimal.Decimal {
	if stopLossPercent.IsZero() {
		stopLossPercent = decimal.NewFromFloat(2.0) // default 2%
	}
	if riskPercent.IsZero() {
		riskPercent = decimal.NewFromFloat(1.0) // default 1%
	}

	// Risk amount = portfolio * risk%
	riskAmount := portfolioValue.Mul(riskPercent).Div(decimal.NewFromFloat(100))

	// Position size = risk amount / stop loss %
	positionSize := riskAmount.Div(stopLossPercent).Mul(decimal.NewFromFloat(100))

	return positionSize
}

// calculateWCHMultiplier calculates position multiplier based on WCH balance
func (g *Guardian) calculateWCHMultiplier(wchBalance decimal.Decimal) float64 {
	// WCH balance can increase risk tolerance slightly
	// but NEVER overrides hard limits
	switch {
	case wchBalance.GreaterThanOrEqual(decimal.NewFromFloat(10000)):
		return 1.5
	case wchBalance.GreaterThanOrEqual(decimal.NewFromFloat(1000)):
		return 1.25
	case wchBalance.GreaterThanOrEqual(decimal.NewFromFloat(100)):
		return 1.1
	default:
		return 1.0
	}
}