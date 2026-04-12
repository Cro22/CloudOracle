package db

import (
	"os"
	"testing"
)

func TestLoadConfigFromEnv_Defaults(t *testing.T) {
	// Clear any env vars that might be set
	for _, key := range []string{"DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME"} {
		os.Unsetenv(key)
	}

	cfg := LoadConfigFromEnv()

	if cfg.Host != "localhost" {
		t.Errorf("expected Host localhost, got %s", cfg.Host)
	}
	if cfg.Port != "5432" {
		t.Errorf("expected Port 5432, got %s", cfg.Port)
	}
	if cfg.User != "oracle" {
		t.Errorf("expected User oracle, got %s", cfg.User)
	}
	if cfg.Password != "oracle_dev" {
		t.Errorf("expected Password oracle_dev, got %s", cfg.Password)
	}
	if cfg.Database != "cloudoracle" {
		t.Errorf("expected Database cloudoracle, got %s", cfg.Database)
	}
}

func TestLoadConfigFromEnv_CustomValues(t *testing.T) {
	t.Setenv("DB_HOST", "myhost")
	t.Setenv("DB_PORT", "5433")
	t.Setenv("DB_USER", "admin")
	t.Setenv("DB_PASSWORD", "secret")
	t.Setenv("DB_NAME", "testdb")

	cfg := LoadConfigFromEnv()

	if cfg.Host != "myhost" {
		t.Errorf("expected Host myhost, got %s", cfg.Host)
	}
	if cfg.Port != "5433" {
		t.Errorf("expected Port 5433, got %s", cfg.Port)
	}
	if cfg.User != "admin" {
		t.Errorf("expected User admin, got %s", cfg.User)
	}
	if cfg.Password != "secret" {
		t.Errorf("expected Password secret, got %s", cfg.Password)
	}
	if cfg.Database != "testdb" {
		t.Errorf("expected Database testdb, got %s", cfg.Database)
	}
}

func TestGetEnv_ReturnsValue(t *testing.T) {
	t.Setenv("TEST_KEY_123", "myvalue")
	result := getEnv("TEST_KEY_123", "default")
	if result != "myvalue" {
		t.Errorf("expected myvalue, got %s", result)
	}
}

func TestGetEnv_ReturnsDefault(t *testing.T) {
	os.Unsetenv("TEST_KEY_NONEXISTENT")
	result := getEnv("TEST_KEY_NONEXISTENT", "fallback")
	if result != "fallback" {
		t.Errorf("expected fallback, got %s", result)
	}
}

func TestGetEnv_EmptyValueReturnsEmpty(t *testing.T) {
	t.Setenv("TEST_KEY_EMPTY", "")
	result := getEnv("TEST_KEY_EMPTY", "default")
	// LookupEnv returns exist=true for empty values
	if result != "" {
		t.Errorf("expected empty string, got %s", result)
	}
}
