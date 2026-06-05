// ============================================================
// MODULE: auth
// Deskripsi: Telegram authentication dan user verification
// ============================================================

package auth

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/nzf210/nafas-bot/internal/logger"
	"github.com/nzf210/nafas-bot/internal/models"
)

// Nama Function: NewService
// Deskripsi: Membuat instance auth service baru.
// Parameter/Value Input:
//   - db: *sql.DB — koneksi database
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung, hanya inisialisasi
// Output/Return Value:
//   - *Service: pointer ke auth service
func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// Service handles Telegram authentication
type Service struct {
	db *sql.DB
}

// Nama Function: Authenticate
// Deskripsi: Authenticate user berdasarkan telegram_id, buat user baru jika belum ada.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk database operation
//   - telegramID: int64 — telegram user ID dari update
//   - username: string — telegram username (nullable)
//   - firstName: string — telegram first name
//   - lastName: string — telegram last name (nullable)
// Function yang Dipanggil/Dikonsumsi:
//   - GetOrCreateUser: dipanggil untuk get atau create user di database
// Output/Return Value:
//   - *models.User: pointer ke user yang sudah login
//   - error: error jika authentication gagal
func (s *Service) Authenticate(ctx context.Context, telegramID int64, username, firstName, lastName string) (*models.User, error) {
	return s.GetOrCreateUser(ctx, telegramID, username, firstName, lastName)
}

// GetOrCreateUser retrieves or creates a user
// Nama Function: GetOrCreateUser
// Deskripsi: Mengambil user berdasarkan telegram_id, buat baru jika tidak ada.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk database operation
//   - telegramID: int64 — telegram user ID
//   - username, firstName, lastName: string — profil data
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryRowContext: dipanggil untuk check existing user
//   - db.ExecContext: dipanggil untuk insert user baru
// Output/Return Value:
//   - *models.User: user yang ada atau baru dibuat
//   - error: error database
func (s *Service) GetOrCreateUser(ctx context.Context, telegramID int64, username, firstName, lastName string) (*models.User, error) {
	// Try to get existing user
	user, err := s.GetByTelegramID(ctx, telegramID)
	if err == nil {
		return user, nil
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	// Create new user
	return s.CreateUser(ctx, telegramID, username, firstName, lastName)
}

// CreateUser creates a new user
// Nama Function: CreateUser
// Deskripsi: Membuat user baru di database.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk database operation
//   - telegramID: int64 — telegram user ID
//   - username, firstName, lastName: string — profil data
// Function yang Dipanggil/Dikonsumsi:
//   - db.ExecContext: dipanggil untuk insert ke tabel users
//   - LogInfo: dipanggil untuk log pembuatan user baru
// Output/Return Value:
//   - *models.User: user yang baru dibuat
//   - error: error database
func (s *Service) CreateUser(ctx context.Context, telegramID int64, username, firstName, lastName string) (*models.User, error) {
	query := `
		INSERT INTO users (telegram_id, username, first_name, last_name, status, wch_balance)
		VALUES ($1, $2, $3, $4, 'active', 0)
		RETURNING id, telegram_id, username, first_name, last_name, status, wch_balance, created_at, updated_at
	`
	var user models.User
	var usernameOpt, firstNameOpt, lastNameOpt sql.NullString

	err := s.db.QueryRowContext(ctx, query, telegramID, nullString(username), nullString(firstName), nullString(lastName)).Scan(
		&user.ID,
		&user.TelegramID,
		&usernameOpt,
		&firstNameOpt,
		&lastNameOpt,
		&user.Status,
		&user.WCHBalance,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	user.Username = nullStringToPtr(usernameOpt)
	user.FirstName = nullStringToPtr(firstNameOpt)
	user.LastName = nullStringToPtr(lastNameOpt)

	logger.Default().WithField("module", "auth").Infof("Created new user: %d", telegramID)
	return &user, nil
}

// GetByTelegramID retrieves user by telegram ID
// Nama Function: GetByTelegramID
// Deskripsi: Mengambil user berdasarkan telegram_id.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk database operation
//   - telegramID: int64 — telegram user ID
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryRowContext: dipanggil untuk SELECT dari tabel users
// Output/Return Value:
//   - *models.User: user yang ditemukan
//   - error: sql.ErrNoRows jika tidak ditemukan, error lain jika query gagal
func (s *Service) GetByTelegramID(ctx context.Context, telegramID int64) (*models.User, error) {
	query := `
		SELECT id, telegram_id, username, first_name, last_name, status, wch_balance, created_at, updated_at
		FROM users
		WHERE telegram_id = $1
	`
	var user models.User
	var username, firstName, lastName sql.NullString

	err := s.db.QueryRowContext(ctx, query, telegramID).Scan(
		&user.ID,
		&user.TelegramID,
		&username,
		&firstName,
		&lastName,
		&user.Status,
		&user.WCHBalance,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	user.Username = nullStringToPtr(username)
	user.FirstName = nullStringToPtr(firstName)
	user.LastName = nullStringToPtr(lastName)
	return &user, nil
}

func nullString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nullStringToPtr(ns sql.NullString) *string {
	if ns.Valid {
		return &ns.String
	}
	return nil
}