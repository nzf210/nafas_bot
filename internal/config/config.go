// ============================================================
// MODULE: config
// Deskripsi: Konfigurasi aplikasi dari environment variables dan file
// ============================================================

package config

import (
	"fmt"
	"os"
	"strconv"
)

// Nama Function: Load
// Deskripsi: Memuat semua konfigurasi aplikasi dari environment variables.
// Parameter/Value Input:
//   - Tidak ada parameter input langsung, membaca dari os.Environ()
//
// Function yang Dipanggil/Dikonsumsi:
//   - os.Getenv: dipanggil untuk membaca setiap environment variable
//   - strconv.Atoi: dipanggil untuk konversi string ke int
//   - time.ParseDuration: dipanggil untuk parsing duration string
//
// Output/Return Value:
//   - *Config: pointer ke struct Config berisi semua konfigurasi
//   - error: error jika konfigurasi wajib tidak ditemukan
func Load() (*Config, error) {
	c := &Config{}

	// App
	c.AppEnv = getEnv("APP_ENV", "development")
	c.AppPort = getEnv("APP_PORT", "8080")

	// Database
	c.DBHost = getEnv("DB_HOST", "localhost")
	c.DBPort, _ = strconv.Atoi(getEnv("DB_PORT", "5432"))
	c.DBUser = getEnv("DB_USER", "nafas")
	c.DBPassword = getEnv("DB_PASSWORD", "")
	c.DBName = getEnv("DB_NAME", "nafas_db")
	c.DBSSLMode = getEnv("DB_SSLMODE", "disable")
	c.DBMaxConns, _ = strconv.Atoi(getEnv("DB_MAX_CONNS", "25"))
	c.DBMaxIdleConns, _ = strconv.Atoi(getEnv("DB_MAX_IDLE_CONNS", "5"))

	// Redis
	c.RedisHost = getEnv("REDIS_HOST", "localhost")
	c.RedisPort, _ = strconv.Atoi(getEnv("REDIS_PORT", "6379"))
	c.RedisPassword = getEnv("REDIS_PASSWORD", "")
	c.RedisDB, _ = strconv.Atoi(getEnv("REDIS_DB", "0"))

	// Telegram
	c.TelegramBotToken = getEnv("TELEGRAM_BOT_TOKEN", "")
	c.TelegramWebhookURL = getEnv("TELEGRAM_WEBHOOK_URL", "")

	// Encryption
	c.EncryptionKey = getEnv("ENCRYPTION_KEY", "")
	if c.EncryptionKey == "" {
		return nil, fmt.Errorf("ENCRYPTION_KEY is required")
	}
	if len(c.EncryptionKey) != 32 {
		return nil, fmt.Errorf("ENCRYPTION_KEY must be 32 bytes (32 characters)")
	}

	// LLM
	c.LLMProviderURL = getEnv("LLM_PROVIDER_URL", "https://api.openai.com/v1")
	c.LLMAPIKey = getEnv("LLM_API_KEY", "")
	c.LLMModel = getEnv("LLM_MODEL", "gpt-4o")
	c.LLMBaseURL = getEnv("LLM_BASE_URL", "") // Custom LLM base URL (Ollama, LM Studio, dll)

	// TradingAgents
	c.TradingAgentsURL = getEnv("TRADING_AGENTS_URL", "http://localhost:8000")

	// Log
	c.LogLevel = getEnv("LOG_LEVEL", "info")

	// Trading defaults
	c.DefaultMaxRiskPerTrade, _ = strconv.ParseFloat(getEnv("DEFAULT_MAX_RISK_PER_TRADE", "1.0"), 64)
	c.DefaultMaxAllocationPerTrade, _ = strconv.ParseFloat(getEnv("DEFAULT_MAX_ALLOCATION_PER_TRADE", "10.0"), 64)
	c.DefaultDailyLossLimit, _ = strconv.ParseFloat(getEnv("DEFAULT_DAILY_LOSS_LIMIT", "5.0"), 64)
	c.DefaultMaxOpenPositions, _ = strconv.Atoi(getEnv("DEFAULT_MAX_OPEN_POSITIONS", "3"))
	c.MaxPairsPerUser, _ = strconv.Atoi(getEnv("MAX_PAIRS_PER_USER", "200"))

	// AI Trading Thresholds
	c.MinConfidenceThreshold, _ = strconv.ParseFloat(getEnv("MIN_CONFIDENCE_THRESHOLD", "70.0"), 64)
	c.MaxRiskLevel = getEnv("MAX_RISK_LEVEL", "medium")

	// Pre-AI Filters
	c.MinVolatilityPercent, _ = strconv.ParseFloat(getEnv("MIN_VOLATILITY_PERCENT", "0.5"), 64)
	c.MinVolume24h, _ = strconv.ParseFloat(getEnv("MIN_VOLUME_24H", "100000"), 64)

	return c, nil
}

// Config holds all configuration
type Config struct {
	// App
	AppEnv  string
	AppPort string

	// Database
	DBHost         string
	DBPort         int
	DBUser         string
	DBPassword     string
	DBName         string
	DBSSLMode      string
	DBMaxConns     int
	DBMaxIdleConns int

	// Redis
	RedisHost     string
	RedisPort     int
	RedisPassword string
	RedisDB       int

	// Telegram
	TelegramBotToken   string
	TelegramWebhookURL string

	// Encryption
	EncryptionKey string

	// LLM
	LLMProviderURL string
	LLMAPIKey      string
	LLMModel       string
	LLMBaseURL     string // Custom LLM base URL (Ollama, LM Studio, dll)

	// TradingAgents
	TradingAgentsURL string

	// Log
	LogLevel string

	// Trading defaults
	DefaultMaxRiskPerTrade       float64
	DefaultMaxAllocationPerTrade float64
	DefaultDailyLossLimit        float64
	DefaultMaxOpenPositions      int
	MaxPairsPerUser              int

	// AI Trading Thresholds
	MinConfidenceThreshold float64
	MaxRiskLevel           string

	// Pre-AI Filters
	MinVolatilityPercent float64
	MinVolume24h         float64
}

// DSN returns PostgreSQL connection string
func (c *Config) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName, c.DBSSLMode,
	)
}

// RedisAddr returns Redis address
func (c *Config) RedisAddr() string {
	return fmt.Sprintf("%s:%d", c.RedisHost, c.RedisPort)
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
