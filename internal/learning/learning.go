// ============================================================
// MODULE: learning
// Deskripsi: Learning Engine untuk menyimpan dan recall memory
// ============================================================

package learning

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nzf210/nafas-bot/internal/logger"
	"github.com/nzf210/nafas-bot/internal/models"
	"github.com/shopspring/decimal"
)

// Service handles AI learning and memory
// Nama Function: Service
// Deskripsi: Service utama untuk learning engine.
// Parameter/Value Input:
//   - db: *sql.DB — koneksi database
//   - logger: *logger.Logger — logger instance
type Service struct {
	db     *sql.DB
	logger *logger.Logger
}

// NewService creates a new learning service
// Nama Function: NewService
// Deskripsi: Membuat instance learning service baru.
// Parameter/Value Input:
//   - db: *sql.DB — koneksi database
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung, hanya inisialisasi
// Output/Return Value:
//   - *Service: pointer ke learning service
func NewService(db *sql.DB) *Service {
	return &Service{
		db:     db,
		logger: logger.Default().WithField("module", "learning"),
	}
}

// StoreMemory stores a new memory
// Nama Function: StoreMemory
// Deskripsi: Menyimpan memory baru ke database.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk database operation
//   - memoryType: string — tipe memory (pattern, lesson, market_event)
//   - content: string — konten memory
//   - importance: float64 — skor kepentingan (0-1)
//   - metadata: map[string]interface{} — metadata tambahan
// Function yang Dipanggil/Dikonsumsi:
//   - db.ExecContext: dipanggil untuk INSERT ke tabel ai_memory
//   - json.Marshal: dipanggil untuk serialize metadata
// Output/Return Value:
//   - *models.AIMemory: memory yang sudah disimpan
//   - error: error jika storage gagal
func (s *Service) StoreMemory(ctx context.Context, memoryType, content string, importance float64, metadata map[string]interface{}) (*models.AIMemory, error) {
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	query := `
		INSERT INTO ai_memory (memory_type, content, importance_score, metadata)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at
	`
	var memory models.AIMemory
	err = s.db.QueryRowContext(ctx, query, memoryType, content, importance, metadataJSON).Scan(
		&memory.ID, &memory.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to store memory: %w", err)
	}

	memory.MemoryType = memoryType
	memory.Content = content
	memory.ImportanceScore = decimalFromFloat(importance)
	memory.Metadata = metadataJSON

	s.logger.Infof("Stored memory: type=%s, importance=%.2f", memoryType, importance)
	return &memory, nil
}

// RecallMemories retrieves relevant memories
// Nama Function: RecallMemories
// Deskripsi: Mengambil memories yang relevan berdasarkan tipe dan importance.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk database operation
//   - memoryType: string — filter berdasarkan tipe (kosongkan untuk semua)
//   - limit: int — maksimal jumlah memories yang diambil
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryContext: dipanggil untuk SELECT dari tabel ai_memory
//   - json.Unmarshal: dipanggil untuk parse metadata
// Output/Return Value:
//   - []models.AIMemory: list memories yang ditemukan
//   - error: error jika query gagal
func (s *Service) RecallMemories(ctx context.Context, memoryType string, limit int) ([]models.AIMemory, error) {
	if limit == 0 {
		limit = 50
	}

	query := `
		SELECT id, memory_type, content, importance_score, metadata, created_at
		FROM ai_memory
		WHERE ($1 = '' OR memory_type = $1)
		ORDER BY importance_score DESC, created_at DESC
		LIMIT $2
	`
	rows, err := s.db.QueryContext(ctx, query, memoryType, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to recall memories: %w", err)
	}
	defer rows.Close()

	var memories []models.AIMemory
	for rows.Next() {
		var m models.AIMemory
		if err := rows.Scan(&m.ID, &m.MemoryType, &m.Content, &m.ImportanceScore, &m.Metadata, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan memory: %w", err)
		}
		memories = append(memories, m)
	}

	return memories, nil
}

// LogDecision logs an AI decision
// Nama Function: LogDecision
// Deskripsi: Menyimpan log keputusan AI ke database.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk database operation
//   - decision: *models.AIDecision — keputusan AI yang akan dilog
// Function yang Dipanggil/Dikonsumsi:
//   - db.ExecContext: dipanggil untuk INSERT ke tabel ai_decisions
// Output/Return Value:
//   - *models.AIDecision: decision dengan ID populated
//   - error: error jika logging gagal
func (s *Service) LogDecision(ctx context.Context, decision *models.AIDecision) (*models.AIDecision, error) {
	query := `
		INSERT INTO ai_decisions (strategy_run_id, provider_id, symbol, decision, confidence, reasoning, prompt_version, input_context, output_context)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, created_at
	`
	err := s.db.QueryRowContext(ctx, query,
		decision.StrategyRunID,
		decision.ProviderID,
		decision.Symbol,
		decision.Decision,
		decision.Confidence,
		nullStringPtr(decision.Reasoning),
		nullStringPtr(decision.PromptVersion),
		decision.InputContext,
		decision.OutputContext,
	).Scan(&decision.ID, &decision.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to log decision: %w", err)
	}

	s.logger.Infof("Logged AI decision: %s %s (confidence: %s)", decision.Symbol, decision.Decision, decision.Confidence)
	return decision, nil
}

// LogFeedback logs trade feedback for learning
// Nama Function: LogFeedback
// Deskripsi: Menyimpan feedback dari hasil trade untuk pembelajaran AI.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk database operation
//   - decisionID: models.UUID — ID keputusan yang feedback
//   - btcBefore, btcAfter: decimal.Decimal — BTC sebelum dan sesudah trade
//   - success: bool — apakah trade berhasil
// Function yang Dipanggil/Dikonsumsi:
//   - db.ExecContext: dipanggil untuk INSERT ke tabel ai_feedback
//   - StoreMemory: dipanggil untuk store pattern jika trade berhasil/gagal
// Output/Return Value:
//   - *models.AIFeedback: feedback yang sudah disimpan
//   - error: error jika logging gagal
func (s *Service) LogFeedback(ctx context.Context, decisionID models.UUID, btcBefore, btcAfter decimal.Decimal, success bool) (*models.AIFeedback, error) {
	btcDelta := btcAfter.Sub(btcBefore)

	query := `
		INSERT INTO ai_feedback (decision_id, btc_before, btc_after, btc_delta, success)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at
	`
	var feedback models.AIFeedback
	err := s.db.QueryRowContext(ctx, query, decisionID, btcBefore, btcAfter, btcDelta, success).Scan(
		&feedback.ID, &feedback.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to log feedback: %w", err)
	}

	feedback.DecisionID = decisionID
	feedback.BTCBefore = btcBefore
	feedback.BTCAfter = btcAfter
	feedback.BTCDelta = btcDelta
	feedback.Success = success

	// Learn from feedback
	s.learnFromFeedback(ctx, feedback)

	s.logger.Infof("Logged feedback: delta=%s, success=%v", btcDelta, success)
	return &feedback, nil
}

// learnFromFeedback creates memory from feedback
func (s *Service) learnFromFeedback(ctx context.Context, feedback models.AIFeedback) {
	importance := 0.5
	if feedback.Success {
		importance = 0.7
	} else {
		importance = 0.9 // More important to remember failures
	}

	content := fmt.Sprintf("Trade result: delta=%s, success=%v", feedback.BTCDelta, feedback.Success)
	metadata := map[string]interface{}{
		"source": "feedback",
		"delta":  feedback.BTCDelta.String(),
	}

	// Store pattern memory
	if feedback.BTCDelta.Abs().GreaterThan(decimal.NewFromFloat(0.001)) {
		s.StoreMemory(ctx, "pattern", content, importance, metadata)
	}
}

// GenerateDailyReport generates daily report for user
// Nama Function: GenerateDailyReport
// Deskripsi: Menghasilkan laporan harian untuk user.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk database operation
//   - userID: models.UUID — ID user
//   - btcStart: decimal.Decimal — BTC balance di awal hari
//   - btcEnd: decimal.Decimal — BTC balance di akhir hari
// Function yang Dipanggil/Dikonsumsi:
//   - db.ExecContext: dipanggil untuk INSERT ke tabel daily_reports
// Output/Return Value:
//   - *models.DailyReport: laporan harian
//   - error: error jika generation gagal
func (s *Service) GenerateDailyReport(ctx context.Context, userID models.UUID, btcStart, btcEnd decimal.Decimal) (*models.DailyReport, error) {
	btcGrowth := btcEnd.Sub(btcStart)
	reportDate := time.Now().Truncate(24 * time.Hour)

	// Count trades today
	var tradeCount int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM orders WHERE user_id = $1 AND created_at >= $2`,
		userID, reportDate,
	).Scan(&tradeCount)
	if err != nil {
		return nil, fmt.Errorf("failed to count trades: %w", err)
	}

	// Calculate win rate
	var winRate decimal.Decimal
	rows, err := s.db.QueryContext(ctx,
		`SELECT success FROM ai_feedback
		 JOIN ai_decisions ON ai_feedback.decision_id = ai_decisions.id
		 WHERE ai_decisions.created_at >= $1`, reportDate,
	)
	if err == nil {
		var total, wins int
		for rows.Next() {
			var success bool
			rows.Scan(&success)
			total++
			if success {
				wins++
			}
		}
		rows.Close()
		if total > 0 {
			winRate = decimal.NewFromInt(int64(wins)).Div(decimal.NewFromInt(int64(total))).Mul(decimal.NewFromFloat(100))
		}
	}

	query := `
		INSERT INTO daily_reports (user_id, btc_start, btc_end, btc_growth, trade_count, win_rate, report_date)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at
	`
	var report models.DailyReport
	err = s.db.QueryRowContext(ctx, query,
		userID, btcStart, btcEnd, btcGrowth, tradeCount, winRate, reportDate,
	).Scan(&report.ID, &report.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to generate report: %w", err)
	}

	report.UserID = userID
	report.BTCStart = btcStart
	report.BTCEnd = btcEnd
	report.BTCGrowth = btcGrowth
	report.TradeCount = tradeCount
	report.WinRate = winRate
	report.ReportDate = reportDate

	return &report, nil
}

func nullStringPtr(s *string) interface{} {
	if s == nil {
		return nil
	}
	return *s
}

func decimalFromFloat(f float64) decimal.Decimal {
	return decimal.NewFromFloat(f)
}