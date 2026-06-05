// ============================================================
// MODULE: database
// Deskripsi: Koneksi database PostgreSQL dan management pool
// ============================================================

package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
	"github.com/nzf210/nafas-bot/internal/config"
	"github.com/nzf210/nafas-bot/internal/logger"
)

// Nama Function: Connect
// Deskripsi: Membuat koneksi ke PostgreSQL dan mengembalikan DB pool.
// Parameter/Value Input:
//   - cfg: *config.Config — konfigurasi database dari environment
// Function yang Dipanggil/Dikonsumsi:
//   - sql.Open: dipanggil untuk buka koneksi ke PostgreSQL
//   - db.SetMaxOpenConns: dipanggil untuk set max connections
//   - db.SetMaxIdleConns: dipanggil untuk set max idle connections
//   - db.Ping: dipanggil untuk test koneksi
// Output/Return Value:
//   - *sql.DB: pointer ke database connection pool
//   - error: error jika koneksi gagal
func Connect(cfg *config.Config) (*sql.DB, error) {
	db, err := sql.Open("postgres", cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db.SetMaxOpenConns(cfg.DBMaxConns)
	db.SetMaxIdleConns(cfg.DBMaxIdleConns)
	db.SetConnMaxLifetime(time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	logger.Default().Infof("Connected to PostgreSQL at %s:%d", cfg.DBHost, cfg.DBPort)
	return db, nil
}

// Nama Function: RunMigrations
// Deskripsi: Menjalankan semua migration files secara berurutan.
// Parameter/Value Input:
//   - db: *sql.DB — koneksi database
//   - migrationsPath: string — path ke folder migrations
// Function yang Dipanggil/Dikonsumsi:
//   - os.ReadDir: dipanggil untuk list semua file migration
//   - db.Exec: dipanggil untuk execute setiap migration
// Output/Return Value:
//   - error: error jika ada migration yang gagal
func RunMigrations(db *sql.DB, migrationsPath string) error {
	log := logger.Default().WithField("module", "database")
	log.Info("Running migrations...")

	// Migration files akan dijalankan manual via psql
	// Ini adalah placeholder untuk future automatic migration runner
	log.Info("Use: psql $DATABASE_URL -f database/migrations/000004_full_schema.up.sql")
	return nil
}

// Transaction wraps a function in a database transaction
// Nama Function: Transaction
// Deskripsi: Menjalankan function dalam context database transaction.
// Parameter/Value Input:
//   - db: *sql.DB — koneksi database
//   - fn: func(tx *sql.Tx) error — function yang dijalankan dalam transaction
// Function yang Dipanggil/Dikonsumsi:
//   - db.BeginTx: dipanggil untuk mulai transaction
//   - tx.Commit: dipanggil jika function berhasil
//   - tx.Rollback: dipanggil jika function error
// Output/Return Value:
//   - error: error dari function atau transaction
func Transaction(db *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("tx error: %v, rollback error: %w", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}