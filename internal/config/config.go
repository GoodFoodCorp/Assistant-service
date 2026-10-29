package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port              string
	JWTSecret         string
	JWTIssuer         string
	JWTAudience       string
	AIProvider        string
	AIBaseURL         string
	AIAPIKey          string
	AIModel           string
	AITimeout         time.Duration
	AISettingsFile    string
	OrderServiceURL   string
	MenuServiceURL    string
	PaymentServiceURL string
	UserServiceURL    string
	FranchiseURL      string
	LogLevel          string
}

// Load reads typed configuration from environment variables only.
// AIProvider and AIBaseURL are deliberately allowed to be empty:
// llmclient.NewProvider falls back to a FakeProvider so the chat stays
// demoable without any AI configured. AIModel is left empty when unset so the
// chosen provider's own default model applies.
func Load() (*Config, error) {
	cfg := &Config{
		Port:              getEnv("PORT", "8093"),
		JWTSecret:         os.Getenv("JWT_SECRET"),
		JWTIssuer:         getEnv("JWT_ISSUER", "goodfood-auth"),
		JWTAudience:       getEnv("JWT_AUDIENCE", "goodfood-api"),
		AIProvider:        os.Getenv("AI_PROVIDER"),
		AIBaseURL:         getEnv("AI_BASE_URL", ""),
		AIAPIKey:          os.Getenv("AI_API_KEY"),
		AIModel:           os.Getenv("AI_MODEL"),
		AISettingsFile:    getEnv("AI_SETTINGS_FILE", "/data/ai-settings.json"),
		OrderServiceURL:   getEnv("ORDER_SERVICE_URL", "http://order-service:8082"),
		MenuServiceURL:    getEnv("MENU_SERVICE_URL", "http://menu-service:8085"),
		PaymentServiceURL: getEnv("PAYMENT_SERVICE_URL", "http://payment-service:8086"),
		UserServiceURL:    getEnv("USER_SERVICE_URL", "http://user-service:8087"),
		FranchiseURL:      getEnv("FRANCHISE_SERVICE_URL", "http://franchise-service:8089"),
		LogLevel:          getEnv("LOG_LEVEL", "info"),
	}
	timeout, err := parseTimeoutSeconds(os.Getenv("AI_TIMEOUT_SECONDS"))
	if err != nil {
		return nil, err
	}
	cfg.AITimeout = timeout
	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// parseTimeoutSeconds returns 0 (meaning "use the default") when unset. A local
// model may need much longer than a cloud one to answer after a cold start.
func parseTimeoutSeconds(raw string) (time.Duration, error) {
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("AI_TIMEOUT_SECONDS must be a positive integer, got %q", raw)
	}
	return time.Duration(n) * time.Second, nil
}
