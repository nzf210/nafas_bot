// ============================================================
// MODULE: database/user_repository
// Deskripsi: Repository untuk operasi CRUD pada user data dan API keys
// ============================================================

package database

import (
	"context"
	"database/sql"
	"time"

	"github.com/nzf210/nafas-bot/internal/models"
)

// UserRepository handles user data persistence
// Nama Function: UserRepository
// Deskripsi: Struct repository untuk operasi CRUD pada user data.
// Parameter/Value Input:
//   - db: *sql.DB — koneksi database
type UserRepository struct {
	db *sql.DB
}

// NewUserRepository creates a new user repository
// Nama Function: NewUserRepository
// Deskripsi: Membuat instance user repository baru.
// Parameter/Value Input:
//   - db: *sql.DB — koneksi database
// Function yang Dipanggil/Dikonsumsi:
//   - Tidak ada function langsung, hanya inisialisasi
// Output/Return Value:
//   - *UserRepository: pointer ke repository
func NewUserRepository(db *sql.DB) *UserRepository {
	return&UserRepository{db: db}
}

// GetByTelegramID mengambil user berdasarkan telegram ID
// Nama Function: GetByTelegramID
// Deskripsi: Mengambil data user dari database berdasarkan telegram_id.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk operasi database
//   - telegramID: int64 — telegram ID unik user
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryRowContext: dipanggil untuk SELECT query
// Output/Return Value:
//   - *models.User: pointer ke user jika ditemukan
//   - error: error jika query gagal atau user tidak ada
func (r *UserRepository) GetByTelegramID(ctx context.Context, telegramID int64) (*models.User, error) {
	var u models.User
	err := r.db.QueryRowContext(ctx, `
		SELECT id, telegram_id, username, first_name, last_name, status, wch_balance, created_at, updated_at
		FROM users
		WHERE telegram_id = $1
	`, telegramID).Scan(&u.ID, &u.TelegramID, &u.Username, &u.FirstName, &u.LastName, &u.Status, &u.WCHBalance, &u.CreatedAt, &u.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &u, err
}

// Create membuat user baru
// Nama Function: Create
// Deskripsi: Membuat user baru di database.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk operasi database
//   - user: *models.User — data user untuk dibuat
// Function yang Dipanggil/Dikonsumsi:
//   - db.ExecContext: dipanggil untuk INSERT
// Output/Return Value:
//   - error: error jika insert gagal
func (r *UserRepository) Create(ctx context.Context, user *models.User) error {
	user.CreatedAt = time.Now()
	user.UpdatedAt = time.Now()

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO users (id, telegram_id, username, first_name, last_name, status, wch_balance, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, user.ID, user.TelegramID, user.Username, user.FirstName, user.LastName, user.Status, user.WCHBalance, user.CreatedAt, user.UpdatedAt)

	return err
}

// Update mengupdate data user
// Nama Function: Update
// Deskripsi: Mengupdate data user di database.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk operasi database
//   - user: *models.User — data user untuk diupdate
// Function yang Dipanggil/Dikonsumsi:
//   - db.ExecContext: dipanggil untuk UPDATE
// Output/Return Value:
//   - error: error jika update gagal
func (r *UserRepository) Update(ctx context.Context, user *models.User) error {
	user.UpdatedAt = time.Now()

	_, err := r.db.ExecContext(ctx, `
		UPDATE users SET
			username = $1, first_name = $2, last_name = $3, status = $4, wch_balance = $5, updated_at = $6
		WHERE id = $7
	`, user.Username, user.FirstName, user.LastName, user.Status, user.WCHBalance, user.UpdatedAt, user.ID)

	return err
}

// GetConfig mengambil user config
// Nama Function: GetConfig
// Deskripsi: Mengambil konfigurasi user dari database.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk operasi database
//   - userID: models.UUID — user ID
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryRowContext: dipanggil untuk SELECT query
// Output/Return Value:
//   - *models.UserConfig: pointer ke config jika ditemukan
//   - error: error jika query gagal
func (r *UserRepository) GetConfig(ctx context.Context, userID models.UUID) (*models.UserConfig, error) {
	var c models.UserConfig
	err := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, max_risk_per_trade, daily_loss_limit, max_open_positions,
			   notify_on_trade, notify_on_error, daily_report_time, auto_trade_enabled, created_at, updated_at
		FROM user_configs
		WHERE user_id = $1
	`, userID).Scan(&c.ID, &c.UserID, &c.MaxRiskPerTrade, &c.DailyLossLimit, &c.MaxOpenPositions,
		&c.NotifyOnTrade, &c.NotifyOnError, &c.DailyReportTime, &c.AutoTradeEnabled, &c.CreatedAt, &c.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &c, err
}

// SaveConfig menyimpan atau mengupdate user config
// Nama Function: SaveConfig
// Deskripsi: Menyimpan atau mengupdate konfigurasi user.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk operasi database
//   - config: *models.UserConfig — config untuk disimpan
// Function yang Dipanggil/Dikonsumsi:
//   - db.ExecContext: dipanggil untuk INSERT ON CONFLICT UPDATE
// Output/Return Value:
//   - error: error jika insert gagal
func (r *UserRepository) SaveConfig(ctx context.Context, config *models.UserConfig) error {
	config.CreatedAt = time.Now()
	config.UpdatedAt = time.Now()

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO user_configs (id, user_id, max_risk_per_trade, daily_loss_limit, max_open_positions,
								  notify_on_trade, notify_on_error, daily_report_time, auto_trade_enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (user_id) DO UPDATE SET
			max_risk_per_trade = EXCLUDED.max_risk_per_trade,
			daily_loss_limit = EXCLUDED.daily_loss_limit,
			max_open_positions = EXCLUDED.max_open_positions,
			notify_on_trade = EXCLUDED.notify_on_trade,
			notify_on_error = EXCLUDED.notify_on_error,
			daily_report_time = EXCLUDED.daily_report_time,
			auto_trade_enabled = EXCLUDED.auto_trade_enabled,
			updated_at = EXCLUDED.updated_at
	`, config.ID, config.UserID, config.MaxRiskPerTrade, config.DailyLossLimit, config.MaxOpenPositions,
		config.NotifyOnTrade, config.NotifyOnError, config.DailyReportTime, config.AutoTradeEnabled, config.CreatedAt, config.UpdatedAt)

	return err
}

// GetAPIKey mengambil API key untuk user dan exchange
// Nama Function: GetAPIKey
// Deskripsi: Mengambil API key terenkripsi untuk user dan exchange tertentu.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk operasi database
//   - userID: models.UUID — user ID
//   - exchange: string — nama exchange (binance, okx)
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryRowContext: dipanggil untuk SELECT query
// Output/Return Value:
//   - *models.APIKey: pointer ke API key jika ditemukan
//   - error: error jika query gagal
func (r *UserRepository) GetAPIKey(ctx context.Context, userID models.UUID, exchange string) (*models.APIKey, error) {
	var k models.APIKey
	err := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, exchange, encrypted_api_key, encrypted_api_secret, encrypted_passphrase, is_active, created_at, updated_at
		FROM api_keys
		WHERE user_id = $1 AND exchange = $2 AND is_active = true
	`, userID, exchange).Scan(&k.ID, &k.UserID, &k.Exchange, &k.EncryptedAPIKey, &k.EncryptedAPISecret, &k.EncryptedPassphrase, &k.IsActive, &k.CreatedAt, &k.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &k, err
}

// SaveAPIKey menyimpan atau mengupdate API key
// Nama Function: SaveAPIKey
// Deskripsi: Menyimpan atau mengupdate API key terenkripsi untuk user.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk operasi database
//   - apiKey: *models.APIKey — API key untuk disimpan
// Function yang Dipanggil/Dikonsumsi:
//   - db.ExecContext: dipanggil untuk INSERT ON CONFLICT UPDATE
// Output/Return Value:
//   - error: error jika insert gagal
func (r *UserRepository) SaveAPIKey(ctx context.Context, apiKey *models.APIKey) error {
	apiKey.CreatedAt = time.Now()
	apiKey.UpdatedAt = time.Now()

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO api_keys (id, user_id, exchange, encrypted_api_key, encrypted_api_secret, encrypted_passphrase, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (user_id, exchange) DO UPDATE SET
			encrypted_api_key = EXCLUDED.encrypted_api_key,
			encrypted_api_secret = EXCLUDED.encrypted_api_secret,
			encrypted_passphrase = EXCLUDED.encrypted_passphrase,
			is_active = EXCLUDED.is_active,
			updated_at = EXCLUDED.updated_at
	`, apiKey.ID, apiKey.UserID, apiKey.Exchange, apiKey.EncryptedAPIKey, apiKey.EncryptedAPISecret, apiKey.EncryptedPassphrase, apiKey.IsActive, apiKey.CreatedAt, apiKey.UpdatedAt)

	return err
}

// GetInventory mengambil asset inventory untuk user
// Nama Function: GetInventory
// Deskripsi: Mengambil semua asset inventory untuk user.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk operasi database
//   - userID: models.UUID — user ID
// Function yang Dipanggil/Dikonsumsi:
//   - db.QueryContext: dipanggil untuk SELECT query
// Output/Return Value:
//   - []models.AssetInventory: list inventory
//   - error: error jika query gagal
func (r *UserRepository) GetInventory(ctx context.Context, userID models.UUID) ([]models.AssetInventory, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, asset, balance, locked_balance, btc_equivalent, updated_at
		FROM asset_inventory
		WHERE user_id = $1
		ORDER BY asset
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var inv []models.AssetInventory
	for rows.Next() {
		var i models.AssetInventory
		err := rows.Scan(&i.ID, &i.UserID, &i.Asset, &i.Balance, &i.LockedBalance, &i.BTCEquivalent, &i.UpdatedAt)
		if err != nil {
			return nil, err
		}
		inv = append(inv, i)
	}

	return inv, rows.Err()
}

// UpdateInventory mengupdate asset inventory
// Nama Function: UpdateInventory
// Deskripsi: Mengupdate atau membuat asset inventory untuk user.
// Parameter/Value Input:
//   - ctx: context.Context — context untuk operasi database
//   - inv: *models.AssetInventory — inventory untuk diupdate
// Function yang Dipanggil/Dikonsumsi:
//   - db.ExecContext: dipanggil untuk INSERT ON CONFLICT UPDATE
// Output/Return Value:
//   - error: error jika update gagal
func (r *UserRepository) UpdateInventory(ctx context.Context, inv *models.AssetInventory) error {
	inv.UpdatedAt = time.Now()

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO asset_inventory (id, user_id, asset, balance, locked_balance, btc_equivalent, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (user_id, asset) DO UPDATE SET
			balance = EXCLUDED.balance,
			locked_balance = EXCLUDED.locked_balance,
			btc_equivalent = EXCLUDED.btc_equivalent,
			updated_at = EXCLUDED.updated_at
	`, inv.ID, inv.UserID, inv.Asset, inv.Balance, inv.LockedBalance, inv.BTCEquivalent, inv.UpdatedAt)

	return err
}
