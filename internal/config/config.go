package config

import (
	"fmt"
	"os"
	"strconv"
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
}

func Load() Config {
	return Config{
		DB: DBConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     getEnv("DB_PORT", "5432"),
			User:     getEnv("DB_USER", "oracle"),
			Password: getEnv("DB_PASSWORD", "oracle_dev"),
			Database: getEnv("DB_NAME", "cloudoracle"),
		},
		Cloud: CloudConfig{
			Provider:       getEnv("CLOUDORACLE_PROVIDER", ""),
			AWSRegion:      getEnv("AWS_REGION", "us-east-2"),
			AWSProfile:     getEnv("AWS_PROFILE", "cloudoracle"),
			GCPProject:     os.Getenv("GOOGLE_CLOUD_PROJECT"),
			AzureSubID:     os.Getenv("AZURE_SUBSCRIPTION_ID"),
			SyntheticCount: getEnvInt("SYNTHETIC_COUNT", 100),
			SyntheticAcct:  getEnv("SYNTHETIC_ACCOUNT", "synthetic-account"),
		},
		LLM: LLMConfig{
			Provider:       os.Getenv("LLM_PROVIDER"),
			GeminiAPIKey:   os.Getenv("GEMINI_API_KEY"),
			ClaudeAPIKey:   os.Getenv("ANTHROPIC_API_KEY"),
			OpenAIAPIKey:   os.Getenv("OPENAI_API_KEY"),
			RequestTimeout: getEnvDuration("LLM_TIMEOUT", 30*time.Second),
		},
		ServiceTimeout: getEnvDuration("CLOUD_SERVICE_TIMEOUT", 30*time.Second),
		LogLevel:       getEnv("LOG_LEVEL", "info"),
		LogFormat:      getEnv("LOG_FORMAT", "text"),
	}
}

func (c Config) DSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		c.DB.User, c.DB.Password, c.DB.Host, c.DB.Port, c.DB.Database,
	)
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getEnvDuration(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
