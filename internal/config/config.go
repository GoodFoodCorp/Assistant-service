package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port              string
	JWTSecret         string
	AIBaseURL         string
	AIAPIKey          string
	AIModel           string
	OrderServiceURL   string
	MenuServiceURL    string
	PaymentServiceURL string
	UserServiceURL    string
	LogLevel          string
}

// Load reads typed configuration from environment variables only.
// AIBaseURL is deliberately allowed to be empty: llmclient.New falls back to
// a FakeProvider so the chat stays demoable without any AI configured.
func Load() (*Config, error) {
	cfg := &Config{
		Port:              getEnv("PORT", "8093"),
		JWTSecret:         os.Getenv("JWT_SECRET"),
		AIBaseURL:         getEnv("AI_BASE_URL", ""),
		AIAPIKey:          os.Getenv("AI_API_KEY"),
		AIModel:           getEnv("AI_MODEL", "gpt-4o-mini"),
		OrderServiceURL:   getEnv("ORDER_SERVICE_URL", "http://order-service:8082"),
		MenuServiceURL:    getEnv("MENU_SERVICE_URL", "http://menu-service:8085"),
		PaymentServiceURL: getEnv("PAYMENT_SERVICE_URL", "http://payment-service:8086"),
		UserServiceURL:    getEnv("USER_SERVICE_URL", "http://user-service:8087"),
		LogLevel:          getEnv("LOG_LEVEL", "info"),
	}
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
