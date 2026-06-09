// ============================================================
// MODULE: orchestrator
// Deskripsi: Balance sufficiency checker untuk pre-execution validation
// ============================================================

package orchestrator

import (
	"github.com/shopspring/decimal"
)

const (
	// BinanceFeeRate adalah fee rate untuk maker/taker (0.1%)
	// Digunakan untuk estimasi fee sebelum eksekusi order
	BinanceFeeRate = 0.001
)

// BalanceSufficiencyResult menyimpan hasil dari pengecekan balance.
// Nama Function: BalanceSufficiencyResult
// Deskripsi: Struct untuk menyimpan hasil validasi balance sufficiency.
// Parameter/Value Input:
//   - Sufficient: bool — apakah balance mencukupi untuk trade
//   - Required: decimal.Decimal — jumlah yang dibutuhkan (position + fees)
//   - Available: decimal.Decimal — balance user yang tersedia
//   - Deficit: decimal.Decimal — jumlah shortfall jika insufficient
//   - MinNotionalMet: bool — apakah order memenuhi minimum notional exchange
//   - Reason: string — alasan kenapa check gagal (human-readable)
//
// Output/Return Value:
//   - BalanceSufficiencyResult: struct hasil check
type BalanceSufficiencyResult struct {
	Sufficient    bool
	Required      decimal.Decimal
	Available     decimal.Decimal
	Deficit       decimal.Decimal
	MinNotionalMet bool
	Reason        string
}

// CheckBalanceSufficiency memvalidasi apakah user memiliki balance yang cukup untuk trade.
// Nama Function: CheckBalanceSufficiency
// Deskripsi: Memvalidasi apakah user memiliki balance yang cukup untuk trade.
//
//	Melakukan 2 validasi:
//	1. MIN_NOTIONAL: position_value >= min_notional (exchange requirement)
//	2. Balance: position_value + estimated_fees <= available_balance
//
// Parameter/Value Input:
//   - availableBalance: decimal.Decimal — balance user yang tersedia (quote currency)
//   - positionValueQuote: decimal.Decimal — nilai posisi dalam quote currency (qty * price)
//   - minNotional: decimal.Decimal — minimum order value dari exchange (e.g., 5 USDT)
//
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung
//
// Output/Return Value:
//   - *BalanceSufficiencyResult: pointer ke struct hasil dengan status dan detail check
//
// Catatan:
//   - Fee estimation menggunakan 0.1% (Binance standard fee)
//   - Jika minNotional = 0, skip MIN_NOTIONAL check
func CheckBalanceSufficiency(
	availableBalance decimal.Decimal,
	positionValueQuote decimal.Decimal,
	minNotional decimal.Decimal,
) *BalanceSufficiencyResult {
	result := &BalanceSufficiencyResult{
		Available: availableBalance,
		Required:  positionValueQuote,
	}

	// ============================================================
	// Check 1: MIN_NOTIONAL validation (exchange requirement)
	// ============================================================
	if minNotional.GreaterThan(decimal.Zero) {
		if positionValueQuote.LessThan(minNotional) {
			result.MinNotionalMet = false
			result.Sufficient = false
			result.Reason = "Order value below exchange minimum notional"
			return result
		}
		result.MinNotionalMet = true
	} else {
		// No minimum required from exchange
		result.MinNotionalMet = true
	}

	// ============================================================
	// Check 2: Balance sufficiency (user balance check)
	// ============================================================
	// Hitung estimated fee (0.1% untuk Binance taker/maker)
	estimatedFee := positionValueQuote.Mul(decimal.NewFromFloat(BinanceFeeRate))
	totalRequired := positionValueQuote.Add(estimatedFee)
	result.Required = totalRequired

	if availableBalance.LessThan(totalRequired) {
		result.Sufficient = false
		result.Deficit = totalRequired.Sub(availableBalance)
		result.Reason = "Insufficient balance"
		return result
	}

	result.Sufficient = true
	return result
}

// FormatBalanceCheckResult membuat human-readable string dari hasil check.
// Nama Function: FormatBalanceCheckResult
// Deskripsi: Membuat string yang readable untuk logging atau notifikasi.
// Parameter/Value Input:
//   - result: *BalanceSufficiencyResult — hasil dari CheckBalanceSufficiency
//   - symbol: string — symbol trading untuk context
//
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung
//
// Output/Return Value:
//   - string: formatted result untuk logging
func FormatBalanceCheckResult(result *BalanceSufficiencyResult, symbol string) string {
	if result.Sufficient {
		return "Balance check passed for " + symbol
	}

	return "Balance check FAILED for " + symbol + ": " + result.Reason +
		" | Available: " + result.Available.String() +
		" | Required: " + result.Required.String() +
		" | Deficit: " + result.Deficit.String()
}