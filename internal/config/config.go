// ============================================================
// MODULE: config
// Deskripsi: Konfigurasi aplikasi dari environment variables dan file
// ============================================================

package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
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
	c :=&Config{}

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
	c.LLMBaseURL = getEnv("LLM_BASE_URL", c.LLMProviderURL) // Custom LLM base URL, fallback to provider URL

	// TradingAgents
	c.TradingAgentsURL = getEnv("TRADING_AGENTS_URL", "http://localhost:8000")

	// Log
	c.LogLevel = getEnv("LOG_LEVEL", "info")

	// Trading Mode - check if explicitly set via env
	c.TradingMode = getEnv("TRADING_MODE", "moderate")

	// Track which trading params are explicitly set via env
	envOverrides := map[string]bool{
		"DEFAULT_MAX_RISK_PER_TRADE":    os.Getenv("DEFAULT_MAX_RISK_PER_TRADE") != "",
		"DEFAULT_MAX_ALLOCATION_PER_TRADE": os.Getenv("DEFAULT_MAX_ALLOCATION_PER_TRADE") != "",
		"DEFAULT_DAILY_LOSS_LIMIT":      os.Getenv("DEFAULT_DAILY_LOSS_LIMIT") != "",
		"DEFAULT_MAX_OPEN_POSITIONS":   os.Getenv("DEFAULT_MAX_OPEN_POSITIONS") != "",
		"MIN_CONFIDENCE_THRESHOLD":     os.Getenv("MIN_CONFIDENCE_THRESHOLD") != "",
		"MAX_RISK_LEVEL":                os.Getenv("MAX_RISK_LEVEL") != "",
		"MIN_VOLATILITY_PERCENT":       os.Getenv("MIN_VOLATILITY_PERCENT") != "",
		"MIN_VOLUME_24H":               os.Getenv("MIN_VOLUME_24H") != "",
	}

	// Apply preset first (default values)
	if preset, ok := GetPreset(c.TradingMode); ok {
		c.ApplyPreset(preset, envOverrides)
	}

	// Scanner properties
	c.ScannerCycleInterval, _ = time.ParseDuration(getEnv("SCANNER_CYCLE_INTERVAL", "7m"))

	// AI Decision Cache TTL (max TTL per symbol before AI re-analysis)
	// Default4 hours — dapat di-set jadi 1h, 4h, 6h, atau sesuai kebutuhan
	c.AIDecisionCacheMaxTTL, _ = time.ParseDuration(getEnv("AI_DECISION_CACHE_MAX_TTL", "4h"))

	// Max pairs per user
	c.MaxPairsPerUser, _ = strconv.Atoi(getEnv("MAX_PAIRS_PER_USER", "100"))

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

	// Scanner properties
	ScannerCycleInterval time.Duration

	// AI Decision Cache TTL (max per symbol, adaptive TTL based on confidence)
	AIDecisionCacheMaxTTL time.Duration

	// AI Trading Thresholds
	MinConfidenceThreshold float64
	MaxRiskLevel           string

	// Pre-AI Filters
	MinVolatilityPercent float64
	MinVolume24h         float64

	// Trading Mode Preset
	TradingMode string // "conservative", "moderate", "aggressive"
}

// TradingModePreset defines preset configurations for different trading modes
type TradingModePreset struct {
	Name                     string
	DefaultMaxRiskPerTrade  float64
	DefaultMaxAllocationPerTrade float64
	DefaultDailyLossLimit   float64
	DefaultMaxOpenPositions int
	MinConfidenceThreshold  float64
	MaxRiskLevel            string
	MinVolatilityPercent    float64
	MinVolume24h            float64
}

// PresetConfigs contains all available trading mode presets
var PresetConfigs = map[string]TradingModePreset{
	"conservative": {
		Name:                     "conservative",
		DefaultMaxRiskPerTrade:   0.5, // 0.5% risk per trade
		DefaultMaxAllocationPerTrade: 5.0,       // 5% max allocation
		DefaultDailyLossLimit:   3.0,           // 3% daily loss limit
		DefaultMaxOpenPositions: 2, // Max 2 open positions
		MinConfidenceThreshold:  90,            // High confidence required
		MaxRiskLevel:            "low",          // Only low risk
		MinVolatilityPercent:    1.0,           // Only volatile coins
		MinVolume24h:            500000, // High volume requirement
	},
	"moderate": {
		Name:                     "moderate",
		DefaultMaxRiskPerTrade:   1.0,           // 1% risk per trade
		DefaultMaxAllocationPerTrade: 10.0,      // 10% max allocation
		DefaultDailyLossLimit:   5.0,           // 5% daily loss limit
		DefaultMaxOpenPositions: 3,             // Max 3 open positions
		MinConfidenceThreshold:  85,            // Standard confidence
		MaxRiskLevel:            "medium",      // Allow medium risk
		MinVolatilityPercent:    0.5,           // Standard volatility
		MinVolume24h:            100000,        // Standard volume
	},
	"aggressive": {
		Name:                     "aggressive",
		DefaultMaxRiskPerTrade:   2.0,           // 2% risk per trade
		DefaultMaxAllocationPerTrade: 20.0,      // 20% max allocation
		DefaultDailyLossLimit:   8.0,           // 8% daily loss limit
		DefaultMaxOpenPositions: 5,             // Max 5 open positions
		MinConfidenceThreshold:  70,            // Lower confidence threshold
		MaxRiskLevel:            "high",        // Allow high risk
		MinVolatilityPercent:    0.2,           // Lower volatility filter
		MinVolume24h:            50000,         // Lower volume requirement
	},
}

// GetPreset returns the preset configuration for the given mode
// Nama Function: GetPreset
// Deskripsi: Mengambil preset configuration berdasarkan mode trading.
// Parameter/Value Input:
//   - mode: string — mode trading ("conservative", "moderate", "aggressive")
// Output/Return Value:
//   - TradingModePreset: preset configuration
//   - bool: true jika preset ditemukan, false jika tidak
func GetPreset(mode string) (TradingModePreset, bool) {
	preset, ok := PresetConfigs[mode]
	return preset, ok
}

// ApplyPreset applies preset values to Config (only if not explicitly set via env)
// Nama Function: ApplyPreset
// Deskripsi: Mengaplikasikan preset values ke Config.
// Hanya mengaplikasikan jika environment variable belum di-set.
// Parameter/Value Input:
//   - preset: TradingModePreset — preset yang akan di-applied
//   - envOverrides: map[string]bool — map yang menandakan env var sudah di-set
// Output/Return Value:
//   - Tidak ada return value, modify Config in-place
func (c *Config) ApplyPreset(preset TradingModePreset, envOverrides map[string]bool) {
	if !envOverrides["DEFAULT_MAX_RISK_PER_TRADE"] {
		c.DefaultMaxRiskPerTrade = preset.DefaultMaxRiskPerTrade
	}
	if !envOverrides["DEFAULT_MAX_ALLOCATION_PER_TRADE"] {
		c.DefaultMaxAllocationPerTrade = preset.DefaultMaxAllocationPerTrade
	}
	if !envOverrides["DEFAULT_DAILY_LOSS_LIMIT"] {
		c.DefaultDailyLossLimit = preset.DefaultDailyLossLimit
	}
	if !envOverrides["DEFAULT_MAX_OPEN_POSITIONS"] {
		c.DefaultMaxOpenPositions = preset.DefaultMaxOpenPositions
	}
	if !envOverrides["MIN_CONFIDENCE_THRESHOLD"] {
		c.MinConfidenceThreshold = preset.MinConfidenceThreshold
	}
	if !envOverrides["MAX_RISK_LEVEL"] {
		c.MaxRiskLevel = preset.MaxRiskLevel
	}
	if !envOverrides["MIN_VOLATILITY_PERCENT"] {
		c.MinVolatilityPercent = preset.MinVolatilityPercent
	}
	if !envOverrides["MIN_VOLUME_24H"] {
		c.MinVolume24h = preset.MinVolume24h
	}
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
