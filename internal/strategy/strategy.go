// ============================================================
// MODULE: strategy
// Deskripsi: Strategy definitions dan execution engine
// ============================================================

package strategy

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nzf210/nafas-bot/internal/logger"
	"github.com/nzf210/nafas-bot/internal/models"
)

// StrategyEngine handles strategy execution
// Nama Function: StrategyEngine
// Deskripsi: Engine utama untuk menjalankan strategi trading.
// Parameter/Value Input:
//   - db: *sql.DB — koneksi database untuk store run history
//   - logger: *logger.Logger — logger instance
type StrategyEngine struct {
	db     *sql.DB
	logger *logger.Logger
}

// NewStrategyEngine creates a new strategy engine
// Nama Function: NewStrategyEngine
// Deskripsi: Membuat instance strategy engine baru.
// Parameter/Value Input:
//   - db: *sql.DB — koneksi database
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung, hanya inisialisasi
// Output/Return Value:
//   - *StrategyEngine: pointer ke strategy engine
func NewStrategyEngine(db *sql.DB) *StrategyEngine {
	return &StrategyEngine{
		db:     db,
		logger: logger.Default().WithField("module", "strategy"),
	}
}

// StrategyParameters holds strategy configuration
// Nama Function: StrategyParameters
// Deskripsi: Struct untuk parameter konfigurasi strategi.
// Parameter/Value Input:
//   - MinSignalStrength: float64 — min signal strength untuk trigger (default 65)
//   - MaxPositionSizeBTC: float64 — max position size dalam BTC
//   - StopLossPercent: float64 — stop loss percentage
//   - TakeProfitTargets: []float64 — take profit targets (%)
//   - Timeframes: []string — timeframes yang di-scan
//   - PriorityAssets: []string — asset yang diutamakan
// Function yang Dipanggil/Dikonsumsi:
//   - json.Unmarshal: dipanggil untuk parse dari JSONB
// Output/Return Value:
//   - StrategyParameters: struct parameter
type StrategyParameters struct {
	MinSignalStrength   float64   `json:"min_signal_strength"`
	MaxPositionSizeBTC  float64   `json:"max_position_size_btc"`
	StopLossPercent     float64   `json:"stop_loss_percent"`
	TakeProfitTargets   []float64 `json:"take_profit_targets"`
	Timeframes          []string  `json:"timeframes"`
	PriorityAssets      []string  `json:"priority_assets"`
}

// RunStrategy executes a strategy for a symbol
// Nama Function: RunStrategy
// Deskripsi: Menjalankan strategi untuk symbol tertentu.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk database operation
//   - strategyID: models.UUID — ID strategi yang dijalankan
//   - symbol: string — symbol trading
//   - decisionSource: string — sumber keputusan (ai, signal, manual)
//   - params: StrategyParameters — parameter strategi
// Function yang Dipanggil/Dikonsumsi:
//   - db.ExecContext: dipanggil untuk insert strategy_run
//   - strategy logic: dipanggil untuk evaluate signal
//   - db.ExecContext: dipanggil untuk update run status
// Output/Return Value:
//   - *models.StrategyRun: record execution strategy
//   - error: error jika execution gagal
func (e *StrategyEngine) RunStrategy(ctx context.Context, strategyID models.UUID, symbol, decisionSource string, params StrategyParameters) (*models.StrategyRun, error) {
	// Create strategy run record
	run := &models.StrategyRun{
		StrategyID:     strategyID,
		Symbol:         symbol,
		DecisionSource: decisionSource,
		Status:         "running",
		StartedAt:      time.Now(),
	}

	query := `
		INSERT INTO strategy_runs (strategy_id, symbol, decision_source, status, started_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at
	`
	err := e.db.QueryRowContext(ctx, query,
		run.StrategyID, run.Symbol, run.DecisionSource, run.Status, run.StartedAt,
	).Scan(&run.ID, &run.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create strategy run: %w", err)
	}

	e.logger.Infof("Started strategy run %s for %s", run.ID, symbol)

	// Execute strategy logic here
	// This would integrate with AI Coordinator and Scanner

	// Mark as completed
	now := time.Now()
	_, err = e.db.ExecContext(ctx,
		"UPDATE strategy_runs SET status = 'completed', completed_at = $1 WHERE id = $2",
		now, run.ID,
	)
	if err != nil {
		e.logger.Warnf("Failed to update strategy run status: %v", err)
	}
	run.Status = "completed"
	run.CompletedAt = &now

	return run, nil
}

// GetActiveStrategies retrieves all enabled strategies
// Nama Function: GetActiveStrategies
// Deskripsi: Mengambil semua strategi yang enabled dari database.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk database operation
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryContext: dipanggil untuk SELECT dari tabel strategies
// Output/Return Value:
//   - []models.Strategy: list strategi aktif
//   - error: error jika query gagal
func (e *StrategyEngine) GetActiveStrategies(ctx context.Context) ([]models.Strategy, error) {
	query := `
		SELECT id, name, description, parameters, enabled, created_at, updated_at
		FROM strategies
		WHERE enabled = true
	`
	rows, err := e.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get strategies: %w", err)
	}
	defer rows.Close()

	var strategies []models.Strategy
	for rows.Next() {
		var s models.Strategy
		var desc sql.NullString
		if err := rows.Scan(&s.ID, &s.Name, &desc, &s.Parameters, &s.Enabled, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan strategy: %w", err)
		}
		s.Description = nullStringToPtr(desc)
		strategies = append(strategies, s)
	}

	return strategies, nil
}

// ParseParameters parses JSON parameters to StrategyParameters
// Nama Function: ParseParameters
// Deskripsi: Parse JSONB parameters ke struct StrategyParameters.
// Parameter/Value Input:
//   - data: json.RawMessage — JSON raw dari database
// Function yang Dipanggil/Dikonsumsi:
//   - json.Unmarshal: dipanggil untuk parse JSON ke struct
// Output/Return Value:
//   - *StrategyParameters: struct parameter yang sudah di-parse
//   - error: error jika JSON invalid
func ParseParameters(data json.RawMessage) (*StrategyParameters, error) {
	var params StrategyParameters
	if err := json.Unmarshal(data, &params); err != nil {
		return nil, fmt.Errorf("failed to parse strategy parameters: %w", err)
	}

	// Set defaults
	if params.MinSignalStrength == 0 {
		params.MinSignalStrength = 65
	}
	if params.MaxPositionSizeBTC == 0 {
		params.MaxPositionSizeBTC = 0.01
	}
	if params.StopLossPercent == 0 {
		params.StopLossPercent = 2.0
	}
	if len(params.TakeProfitTargets) == 0 {
		params.TakeProfitTargets = []float64{3, 5, 8}
	}

	return &params, nil
}

func nullStringToPtr(ns sql.NullString) *string {
	if ns.Valid {
		return &ns.String
	}
	return nil
}