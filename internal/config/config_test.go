package config

import (
	"os"
	"testing"
	"time"
)

func clearEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		os.Unsetenv(k)
	}
}

func TestLoad_Defaults(t *testing.T) {
	clearEnv(t,
		"DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME",
		"CLOUDORACLE_PROVIDER", "AWS_REGION", "AWS_PROFILE",
		"GOOGLE_CLOUD_PROJECT", "AZURE_SUBSCRIPTION_ID",
		"LLM_PROVIDER", "GEMINI_API_KEY", "ANTHROPIC_API_KEY", "OPENAI_API_KEY",
		"CLOUD_SERVICE_TIMEOUT", "LLM_TIMEOUT", "LOG_LEVEL", "LOG_FORMAT",
	)

	cfg := Load()

	if cfg.DB.Host != "localhost" {
		t.Errorf("expected DB.Host localhost, got %s", cfg.DB.Host)
	}
	if cfg.DB.Port != "5432" {
		t.Errorf("expected DB.Port 5432, got %s", cfg.DB.Port)
	}
	if cfg.DB.Database != "cloudoracle" {
		t.Errorf("expected DB.Database cloudoracle, got %s", cfg.DB.Database)
	}
	if cfg.Cloud.AWSRegion != "us-east-2" {
		t.Errorf("expected AWSRegion us-east-2, got %s", cfg.Cloud.AWSRegion)
	}
	if cfg.ServiceTimeout != 30*time.Second {
		t.Errorf("expected ServiceTimeout 30s, got %v", cfg.ServiceTimeout)
	}
	if cfg.LLM.RequestTimeout != 30*time.Second {
		t.Errorf("expected LLM.RequestTimeout 30s, got %v", cfg.LLM.RequestTimeout)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("expected LogLevel info, got %s", cfg.LogLevel)
	}
}

func TestLoad_CustomValues(t *testing.T) {
	t.Setenv("DB_HOST", "myhost")
	t.Setenv("DB_PORT", "5433")
	t.Setenv("DB_USER", "admin")
	t.Setenv("DB_PASSWORD", "secret")
	t.Setenv("DB_NAME", "testdb")
	t.Setenv("CLOUDORACLE_PROVIDER", "aws")
	t.Setenv("AWS_REGION", "eu-west-1")
	t.Setenv("CLOUD_SERVICE_TIMEOUT", "45s")
	t.Setenv("LOG_LEVEL", "debug")

	cfg := Load()

	if cfg.DB.Host != "myhost" {
		t.Errorf("expected DB.Host myhost, got %s", cfg.DB.Host)
	}
	if cfg.DB.User != "admin" {
		t.Errorf("expected DB.User admin, got %s", cfg.DB.User)
	}
	if cfg.Cloud.Provider != "aws" {
		t.Errorf("expected Provider aws, got %s", cfg.Cloud.Provider)
	}
	if cfg.Cloud.AWSRegion != "eu-west-1" {
		t.Errorf("expected AWSRegion eu-west-1, got %s", cfg.Cloud.AWSRegion)
	}
	if cfg.ServiceTimeout != 45*time.Second {
		t.Errorf("expected ServiceTimeout 45s, got %v", cfg.ServiceTimeout)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("expected LogLevel debug, got %s", cfg.LogLevel)
	}
}

func TestGetEnv_ReturnsValue(t *testing.T) {
	t.Setenv("TEST_KEY_123", "myvalue")
	if v := getEnv("TEST_KEY_123", "default"); v != "myvalue" {
		t.Errorf("expected myvalue, got %s", v)
	}
}

func TestGetEnv_ReturnsDefaultOnEmpty(t *testing.T) {
	t.Setenv("TEST_KEY_EMPTY", "")
	if v := getEnv("TEST_KEY_EMPTY", "fallback"); v != "fallback" {
		t.Errorf("expected fallback for empty env, got %s", v)
	}
}

func TestGetEnv_ReturnsDefaultWhenUnset(t *testing.T) {
	os.Unsetenv("TEST_KEY_NONEXISTENT")
	if v := getEnv("TEST_KEY_NONEXISTENT", "fallback"); v != "fallback" {
		t.Errorf("expected fallback, got %s", v)
	}
}

func TestGetEnvDuration_Invalid(t *testing.T) {
	t.Setenv("TEST_DUR", "notaduration")
	if v := getEnvDuration("TEST_DUR", 10*time.Second); v != 10*time.Second {
		t.Errorf("expected fallback 10s, got %v", v)
	}
}

func TestDSN(t *testing.T) {
	cfg := Config{DB: DBConfig{
		Host: "h", Port: "5432", User: "u", Password: "p", Database: "d",
	}}
	want := "postgres://u:p@h:5432/d?sslmode=disable"
	if cfg.DSN() != want {
		t.Errorf("DSN mismatch: got %s, want %s", cfg.DSN(), want)
	}
}
