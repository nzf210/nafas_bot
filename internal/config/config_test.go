// ============================================================
// MODULE: config
// Deskripsi: Unit tests untuk config package
// ============================================================

package config

import (
	"os"
	"testing"
)

func TestLoad_ValidConfig(t *testing.T) {
	os.Setenv("APP_ENV", "development")
	os.Setenv("APP_PORT", "8080")
	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("ENCRYPTION_KEY", "01234567890123456789012345678901") // 32 bytes

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AppEnv != "development" {
		t.Errorf("AppEnv = %v, want %v", cfg.AppEnv, "development")
	}
	if cfg.AppPort != "8080" {
		t.Errorf("AppPort = %v, want %v", cfg.AppPort, "8080")
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %v, want %v", cfg.LogLevel, "debug")
	}
}

func TestLoad_MissingEncryptionKey(t *testing.T) {
	os.Setenv("APP_ENV", "development")
	os.Setenv("APP_PORT", "8080")
	os.Unsetenv("ENCRYPTION_KEY")

	_, err := Load()
	if err == nil {
		t.Error("Load() expected error for missing ENCRYPTION_KEY")
	}
}

func TestLoad_InvalidEncryptionKeyLength(t *testing.T) {
	os.Setenv("APP_ENV", "development")
	os.Setenv("APP_PORT", "8080")
	os.Setenv("ENCRYPTION_KEY", "tooshort")

	_, err := Load()
	if err == nil {
		t.Error("Load() expected error for invalid ENCRYPTION_KEY length")
	}
}

func TestLoad_Defaults(t *testing.T) {
	os.Setenv("APP_ENV", "test")
	os.Setenv("ENCRYPTION_KEY", "01234567890123456789012345678901")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AppPort != "8080" {
		t.Errorf("AppPort default = %v, want %v", cfg.AppPort, "8080")
	}
	if cfg.LLMModel != "gpt-4o" {
		t.Errorf("LLMModel default = %v, want %v", cfg.LLMModel, "gpt-4o")
	}
	if cfg.DefaultMaxOpenPositions != 3 {
		t.Errorf("DefaultMaxOpenPositions default = %v, want %v", cfg.DefaultMaxOpenPositions, 3)
	}
}

func TestDSN(t *testing.T) {
	os.Setenv("APP_ENV", "development")
	os.Setenv("DB_HOST", "localhost")
	os.Setenv("DB_PORT", "5432")
	os.Setenv("DB_USER", "user")
	os.Setenv("DB_PASSWORD", "pass")
	os.Setenv("DB_NAME", "testdb")
	os.Setenv("ENCRYPTION_KEY", "01234567890123456789012345678901")

	cfg, _ := Load()
	dsn := cfg.DSN()

	expected := "host=localhost port=5432 user=user password=pass dbname=testdb sslmode=disable"
	if dsn != expected {
		t.Errorf("DSN() = %v, want %v", dsn, expected)
	}
}

func TestRedisAddr(t *testing.T) {
	os.Setenv("APP_ENV", "development")
	os.Setenv("REDIS_HOST", "redis.example.com")
	os.Setenv("REDIS_PORT", "6380")
	os.Setenv("ENCRYPTION_KEY", "01234567890123456789012345678901")

	cfg, _ := Load()
	addr := cfg.RedisAddr()

	expected := "redis.example.com:6380"
	if addr != expected {
		t.Errorf("RedisAddr() = %v, want %v", addr, expected)
	}
}

func TestGetEnv(t *testing.T) {
	os.Setenv("TEST_VAR", "test_value")
	result := getEnv("TEST_VAR", "default")
	if result != "test_value" {
		t.Errorf("getEnv() = %v, want %v", result, "test_value")
	}

	os.Unsetenv("TEST_VAR")
	result = getEnv("TEST_VAR", "default")
	if result != "default" {
		t.Errorf("getEnv() with unset = %v, want %v", result, "default")
	}
}