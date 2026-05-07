package config

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// allConfigEnvVars returns every env var Load looks at. Tests use this to
// scrub the environment so default-value tests don't pick up dev shell state.
func allConfigEnvVars() []string {
	return []string{
		"DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME",
		"CLOUDORACLE_PROVIDER", "AWS_REGION", "AWS_PROFILE",
		"GOOGLE_CLOUD_PROJECT", "AZURE_SUBSCRIPTION_ID",
		"SYNTHETIC_COUNT", "SYNTHETIC_ACCOUNT",
		"LLM_PROVIDER", "GEMINI_API_KEY", "ANTHROPIC_API_KEY", "OPENAI_API_KEY", "LLM_TIMEOUT",
		"CLOUD_SERVICE_TIMEOUT", "LOG_LEVEL", "LOG_FORMAT",
	}
}

func clearAll(t *testing.T) {
	t.Helper()
	for _, k := range allConfigEnvVars() {
		os.Unsetenv(k)
	}
}

func TestLoad_AllDefaults(t *testing.T) {
	clearAll(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load with all defaults should succeed, got: %v", err)
	}

	checks := map[string]any{
		"DB.Host":            cfg.DB.Host,
		"DB.Port":            cfg.DB.Port,
		"DB.User":            cfg.DB.User,
		"Cloud.Provider":     cfg.Cloud.Provider,
		"Cloud.AWSRegion":    cfg.Cloud.AWSRegion,
		"Cloud.AWSProfile":   cfg.Cloud.AWSProfile,
		"Cloud.SyntheticCnt": cfg.Cloud.SyntheticCount,
		"ServiceTimeout":     cfg.ServiceTimeout,
		"LLM.RequestTimeout": cfg.LLM.RequestTimeout,
		"LogLevel":           cfg.LogLevel,
		"LogFormat":          cfg.LogFormat,
	}
	expect := map[string]any{
		"DB.Host":            "localhost",
		"DB.Port":            "5432",
		"DB.User":            "oracle",
		"Cloud.Provider":     "synthetic",
		"Cloud.AWSRegion":    "us-east-2",
		"Cloud.AWSProfile":   "cloudoracle",
		"Cloud.SyntheticCnt": 100,
		"ServiceTimeout":     30 * time.Second,
		"LLM.RequestTimeout": 30 * time.Second,
		"LogLevel":           "info",
		"LogFormat":          "text",
	}
	for k, want := range expect {
		if got := checks[k]; got != want {
			t.Errorf("%s = %v, want %v", k, got, want)
		}
	}
}

func TestLoad_CustomValues(t *testing.T) {
	clearAll(t)
	t.Setenv("DB_HOST", "myhost")
	t.Setenv("DB_PORT", "5433")
	t.Setenv("DB_USER", "admin")
	t.Setenv("CLOUDORACLE_PROVIDER", "aws")
	t.Setenv("AWS_REGION", "eu-west-1")
	t.Setenv("CLOUD_SERVICE_TIMEOUT", "45s")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_FORMAT", "json")
	t.Setenv("SYNTHETIC_COUNT", "250")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DB.Host != "myhost" || cfg.DB.Port != "5433" || cfg.DB.User != "admin" {
		t.Errorf("DB fields not picked up: %+v", cfg.DB)
	}
	if cfg.Cloud.Provider != "aws" || cfg.Cloud.AWSRegion != "eu-west-1" {
		t.Errorf("Cloud fields not picked up: %+v", cfg.Cloud)
	}
	if cfg.Cloud.SyntheticCount != 250 {
		t.Errorf("SyntheticCount = %d, want 250", cfg.Cloud.SyntheticCount)
	}
	if cfg.ServiceTimeout != 45*time.Second {
		t.Errorf("ServiceTimeout = %v, want 45s", cfg.ServiceTimeout)
	}
	if cfg.LogLevel != "debug" || cfg.LogFormat != "json" {
		t.Errorf("Log fields: level=%s format=%s", cfg.LogLevel, cfg.LogFormat)
	}
}

// loadInvalid is a helper for tests that expect Load to fail. It clears the
// environment, sets the bad var, and asserts the error contains the substring.
func loadInvalid(t *testing.T, key, value, wantSubstring string) {
	t.Helper()
	clearAll(t)
	t.Setenv(key, value)

	_, err := Load()
	if err == nil {
		t.Fatalf("expected validation error for %s=%q, got nil", key, value)
	}
	if !IsValidationError(err) {
		t.Errorf("expected *ValidationError, got %T", err)
	}
	if !strings.Contains(err.Error(), wantSubstring) {
		t.Errorf("error = %q\nwant substring = %q", err.Error(), wantSubstring)
	}
}

func TestLoad_InvalidPort_NotNumeric(t *testing.T) {
	loadInvalid(t, "DB_PORT", "abc", "DB_PORT")
}

func TestLoad_InvalidPort_OutOfRange(t *testing.T) {
	loadInvalid(t, "DB_PORT", "70000", "out of range")
}

func TestLoad_InvalidPort_Zero(t *testing.T) {
	loadInvalid(t, "DB_PORT", "0", "out of range")
}

func TestLoad_InvalidCloudProvider(t *testing.T) {
	loadInvalid(t, "CLOUDORACLE_PROVIDER", "azur", "not one of")
}

func TestLoad_InvalidLLMProvider(t *testing.T) {
	loadInvalid(t, "LLM_PROVIDER", "claude4", "not one of")
}

func TestLoad_InvalidLogLevel(t *testing.T) {
	loadInvalid(t, "LOG_LEVEL", "verbose", "LOG_LEVEL")
}

func TestLoad_InvalidLogFormat(t *testing.T) {
	loadInvalid(t, "LOG_FORMAT", "yaml", "LOG_FORMAT")
}

func TestLoad_InvalidSyntheticCount(t *testing.T) {
	loadInvalid(t, "SYNTHETIC_COUNT", "notanumber", "SYNTHETIC_COUNT")
}

func TestLoad_NegativeSyntheticCount(t *testing.T) {
	loadInvalid(t, "SYNTHETIC_COUNT", "-5", ">= 1")
}

func TestLoad_InvalidServiceTimeout(t *testing.T) {
	loadInvalid(t, "CLOUD_SERVICE_TIMEOUT", "30", "valid Go duration")
}

func TestLoad_ZeroServiceTimeout(t *testing.T) {
	loadInvalid(t, "CLOUD_SERVICE_TIMEOUT", "0s", "greater than zero")
}

func TestLoad_InvalidLLMTimeout(t *testing.T) {
	loadInvalid(t, "LLM_TIMEOUT", "5sec", "valid Go duration")
}

// TestLoad_GCPProviderRequiresProject covers the cross-field rule:
// provider=gcp without GOOGLE_CLOUD_PROJECT must fail.
func TestLoad_GCPProviderRequiresProject(t *testing.T) {
	clearAll(t)
	t.Setenv("CLOUDORACLE_PROVIDER", "gcp")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when provider=gcp without project")
	}
	if !strings.Contains(err.Error(), "GOOGLE_CLOUD_PROJECT") {
		t.Errorf("error should mention GOOGLE_CLOUD_PROJECT: %v", err)
	}
}

func TestLoad_GCPProviderWithProject_OK(t *testing.T) {
	clearAll(t)
	t.Setenv("CLOUDORACLE_PROVIDER", "gcp")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "my-project")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Cloud.GCPProject != "my-project" {
		t.Errorf("GCPProject = %q, want my-project", cfg.Cloud.GCPProject)
	}
}

func TestLoad_AzureProviderRequiresSubscription(t *testing.T) {
	clearAll(t)
	t.Setenv("CLOUDORACLE_PROVIDER", "azure")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when provider=azure without subscription")
	}
	if !strings.Contains(err.Error(), "AZURE_SUBSCRIPTION_ID") {
		t.Errorf("error should mention AZURE_SUBSCRIPTION_ID: %v", err)
	}
}

func TestLoad_LLMProviderRequiresMatchingKey(t *testing.T) {
	cases := []struct {
		provider string
		envKey   string
	}{
		{"gemini", "GEMINI_API_KEY"},
		{"claude", "ANTHROPIC_API_KEY"},
		{"openai", "OPENAI_API_KEY"},
	}
	for _, c := range cases {
		t.Run(c.provider, func(t *testing.T) {
			clearAll(t)
			t.Setenv("LLM_PROVIDER", c.provider)

			_, err := Load()
			if err == nil {
				t.Fatalf("expected error when LLM_PROVIDER=%s without key", c.provider)
			}
			if !strings.Contains(err.Error(), c.envKey) {
				t.Errorf("error should mention %s: %v", c.envKey, err)
			}
		})
	}
}

func TestLoad_LLMProviderWithKey_OK(t *testing.T) {
	clearAll(t)
	t.Setenv("LLM_PROVIDER", "claude")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-123")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LLM.Provider != "claude" || cfg.LLM.ClaudeAPIKey != "sk-ant-123" {
		t.Errorf("LLM not picked up: %+v", cfg.LLM)
	}
}

// TestLoad_AccumulatesAllErrors verifies the key behavior pediste explicitly:
// if 3 vars are wrong, all 3 show up in one error message — not just the first.
func TestLoad_AccumulatesAllErrors(t *testing.T) {
	clearAll(t)
	t.Setenv("DB_PORT", "abc")
	t.Setenv("LOG_LEVEL", "loud")
	t.Setenv("CLOUD_SERVICE_TIMEOUT", "ten")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{"DB_PORT", "LOG_LEVEL", "CLOUD_SERVICE_TIMEOUT"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error should mention %s, got: %s", want, msg)
		}
	}
	// And the message should look like a list, not a one-liner.
	if !strings.Contains(msg, "problems:") {
		t.Errorf("multi-error message should say 'problems:', got: %s", msg)
	}
}

// TestLoad_EmptyEnvFallsBackToDefaults: empty string is treated like "unset".
// Important so that scripts that do `unset DB_PORT` and `DB_PORT=` behave the same.
func TestLoad_EmptyEnvFallsBackToDefaults(t *testing.T) {
	clearAll(t)
	t.Setenv("DB_PORT", "")
	t.Setenv("LOG_LEVEL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DB.Port != "5432" || cfg.LogLevel != "info" {
		t.Errorf("empty env should fall back to defaults: port=%s level=%s",
			cfg.DB.Port, cfg.LogLevel)
	}
}

func TestValidationError_SinglePlural(t *testing.T) {
	single := &ValidationError{Issues: []string{"DB_PORT bad"}}
	if !strings.HasPrefix(single.Error(), "config: DB_PORT bad") {
		t.Errorf("single-issue format unexpected: %s", single.Error())
	}
	if strings.Contains(single.Error(), "problems:") {
		t.Errorf("single issue should not say 'problems:': %s", single.Error())
	}

	multi := &ValidationError{Issues: []string{"a", "b"}}
	if !strings.Contains(multi.Error(), "2 problems:") {
		t.Errorf("multi-issue format should announce count: %s", multi.Error())
	}
}

func TestIsValidationError(t *testing.T) {
	if !IsValidationError(&ValidationError{Issues: []string{"x"}}) {
		t.Error("IsValidationError should return true for ValidationError")
	}
	if IsValidationError(errors.New("plain error")) {
		t.Error("IsValidationError should return false for non-validation errors")
	}
	if IsValidationError(nil) {
		t.Error("IsValidationError(nil) should be false")
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

func TestGetEnv_DefaultBehavior(t *testing.T) {
	t.Setenv("TEST_KEY_VALUE", "myvalue")
	if v := getEnv("TEST_KEY_VALUE", "default"); v != "myvalue" {
		t.Errorf("with value: got %s, want myvalue", v)
	}

	t.Setenv("TEST_KEY_EMPTY", "")
	if v := getEnv("TEST_KEY_EMPTY", "fallback"); v != "fallback" {
		t.Errorf("empty: got %s, want fallback", v)
	}

	os.Unsetenv("TEST_KEY_NONEXISTENT")
	if v := getEnv("TEST_KEY_NONEXISTENT", "fallback"); v != "fallback" {
		t.Errorf("unset: got %s, want fallback", v)
	}
}
