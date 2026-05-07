package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DB             DBConfig
	Cloud          CloudConfig
	LLM            LLMConfig
	ServiceTimeout time.Duration
	LogLevel       string
	LogFormat      string
}

type DBConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Database string
}

type CloudConfig struct {
	Provider       string
	AWSRegion      string
	AWSProfile     string
	GCPProject     string
	AzureSubID     string
	SyntheticCount int
	SyntheticAcct  string
}

type LLMConfig struct {
	Provider       string
	GeminiAPIKey   string
	ClaudeAPIKey   string
	OpenAIAPIKey   string
	RequestTimeout time.Duration
	MaxRetries     int
	BaseDelay      time.Duration
	MaxDelay       time.Duration
}

const (
	providerSynthetic = "synthetic"
	providerAWS       = "aws"
	providerGCP       = "gcp"
	providerAzure     = "azure"
)

var (
	validCloudProviders = []string{providerSynthetic, providerAWS, providerGCP, providerAzure}
	validLLMProviders   = []string{"gemini", "claude", "openai"}
	validLogLevels      = []string{"debug", "info", "warn", "error"}
	validLogFormats     = []string{"text", "json"}
)

// ValidationError aggregates every config problem encountered during Load
// so the operator sees the full picture at once instead of fixing one var,
// running again, fixing the next, etc.
type ValidationError struct {
	Issues []string
}

func (e *ValidationError) Error() string {
	if len(e.Issues) == 1 {
		return "config: " + e.Issues[0]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "config: %d problems:\n", len(e.Issues))
	for _, s := range e.Issues {
		fmt.Fprintf(&b, "  - %s\n", s)
	}
	return strings.TrimRight(b.String(), "\n")
}

// Load reads every env var once, validates everything, and returns a
// fully-populated Config. If any var is invalid or a required cross-field
// rule fails (e.g. provider=gcp without GOOGLE_CLOUD_PROJECT), it returns
// a *ValidationError listing every problem at once.
func Load() (Config, error) {
	v := newValidator()

	cfg := Config{
		DB: DBConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     v.requirePort("DB_PORT", "5432"),
			User:     getEnv("DB_USER", "oracle"),
			Password: getEnv("DB_PASSWORD", "oracle_dev"),
			Database: getEnv("DB_NAME", "cloudoracle"),
		},
		Cloud: CloudConfig{
			Provider:       v.requireEnum("CLOUDORACLE_PROVIDER", providerSynthetic, validCloudProviders),
			AWSRegion:      getEnv("AWS_REGION", "us-east-2"),
			AWSProfile:     getEnv("AWS_PROFILE", "cloudoracle"),
			GCPProject:     os.Getenv("GOOGLE_CLOUD_PROJECT"),
			AzureSubID:     os.Getenv("AZURE_SUBSCRIPTION_ID"),
			SyntheticCount: v.requirePositiveInt("SYNTHETIC_COUNT", 100),
			SyntheticAcct:  getEnv("SYNTHETIC_ACCOUNT", "synthetic-account"),
		},
		LLM: LLMConfig{
			Provider:       v.optionalEnum("LLM_PROVIDER", validLLMProviders),
			GeminiAPIKey:   os.Getenv("GEMINI_API_KEY"),
			ClaudeAPIKey:   os.Getenv("ANTHROPIC_API_KEY"),
			OpenAIAPIKey:   os.Getenv("OPENAI_API_KEY"),
			RequestTimeout: v.requirePositiveDuration("LLM_TIMEOUT", 30*time.Second),
			MaxRetries:     v.requireNonNegativeInt("LLM_MAX_RETRIES", 3),
			BaseDelay:      v.requirePositiveDuration("LLM_BASE_DELAY", 500*time.Millisecond),
			MaxDelay:       v.requirePositiveDuration("LLM_MAX_DELAY", 30*time.Second),
		},
		ServiceTimeout: v.requirePositiveDuration("CLOUD_SERVICE_TIMEOUT", 30*time.Second),
		LogLevel:       v.requireEnum("LOG_LEVEL", "info", validLogLevels),
		LogFormat:      v.requireEnum("LOG_FORMAT", "text", validLogFormats),
	}

	v.crossFieldChecks(&cfg)

	if len(v.issues) > 0 {
		return Config{}, &ValidationError{Issues: v.issues}
	}
	return cfg, nil
}

func (c Config) DSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		c.DB.User, c.DB.Password, c.DB.Host, c.DB.Port, c.DB.Database,
	)
}

// IsValidationError reports whether err is a *ValidationError. Lets main.go
// branch on "config problem" without importing the concrete type everywhere.
func IsValidationError(err error) bool {
	var ve *ValidationError
	return errors.As(err, &ve)
}

type validator struct {
	issues []string
}

func newValidator() *validator { return &validator{} }

func (v *validator) errorf(format string, args ...any) {
	v.issues = append(v.issues, fmt.Sprintf(format, args...))
}

func (v *validator) requirePort(key, def string) string {
	raw, set := os.LookupEnv(key)
	if !set || raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		v.errorf("%s=%q is not a valid port number", key, raw)
		return def
	}
	if n < 1 || n > 65535 {
		v.errorf("%s=%d out of range (must be 1..65535)", key, n)
		return def
	}
	return raw
}

func (v *validator) requireEnum(key, def string, allowed []string) string {
	raw, set := os.LookupEnv(key)
	if !set || raw == "" {
		return def
	}
	for _, a := range allowed {
		if raw == a {
			return raw
		}
	}
	v.errorf("%s=%q is not one of {%s}", key, raw, strings.Join(allowed, ", "))
	return def
}

func (v *validator) optionalEnum(key string, allowed []string) string {
	raw, set := os.LookupEnv(key)
	if !set || raw == "" {
		return ""
	}
	for _, a := range allowed {
		if raw == a {
			return raw
		}
	}
	v.errorf("%s=%q is not one of {%s}", key, raw, strings.Join(allowed, ", "))
	return ""
}

func (v *validator) requirePositiveInt(key string, def int) int {
	raw, set := os.LookupEnv(key)
	if !set || raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		v.errorf("%s=%q is not a valid integer", key, raw)
		return def
	}
	if n < 1 {
		v.errorf("%s=%d must be >= 1", key, n)
		return def
	}
	return n
}

// requireNonNegativeInt is the >= 0 sibling of requirePositiveInt — used for
// values like LLM_MAX_RETRIES where 0 is a legal "disable" setting.
func (v *validator) requireNonNegativeInt(key string, def int) int {
	raw, set := os.LookupEnv(key)
	if !set || raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		v.errorf("%s=%q is not a valid integer", key, raw)
		return def
	}
	if n < 0 {
		v.errorf("%s=%d must be >= 0", key, n)
		return def
	}
	return n
}

func (v *validator) requirePositiveDuration(key string, def time.Duration) time.Duration {
	raw, set := os.LookupEnv(key)
	if !set || raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		v.errorf("%s=%q is not a valid Go duration (e.g. 30s, 5m)", key, raw)
		return def
	}
	if d <= 0 {
		v.errorf("%s=%v must be greater than zero", key, d)
		return def
	}
	return d
}

// crossFieldChecks runs the validations that depend on more than one var,
// after all primitive parsing has completed.
func (v *validator) crossFieldChecks(cfg *Config) {
	switch cfg.Cloud.Provider {
	case providerGCP:
		if cfg.Cloud.GCPProject == "" {
			v.errorf("GOOGLE_CLOUD_PROJECT is required when CLOUDORACLE_PROVIDER=gcp")
		}
	case providerAzure:
		if cfg.Cloud.AzureSubID == "" {
			v.errorf("AZURE_SUBSCRIPTION_ID is required when CLOUDORACLE_PROVIDER=azure")
		}
	}

	switch cfg.LLM.Provider {
	case "gemini":
		if cfg.LLM.GeminiAPIKey == "" {
			v.errorf("GEMINI_API_KEY is required when LLM_PROVIDER=gemini")
		}
	case "claude":
		if cfg.LLM.ClaudeAPIKey == "" {
			v.errorf("ANTHROPIC_API_KEY is required when LLM_PROVIDER=claude")
		}
	case "openai":
		if cfg.LLM.OpenAIAPIKey == "" {
			v.errorf("OPENAI_API_KEY is required when LLM_PROVIDER=openai")
		}
	}
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}
