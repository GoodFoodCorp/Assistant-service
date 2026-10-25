package main

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"

	httpadapter "goodfood/assistant-service/internal/adapter/http"
	"goodfood/assistant-service/internal/adapter/llmclient"
	"goodfood/assistant-service/internal/adapter/menuclient"
	"goodfood/assistant-service/internal/adapter/orderclient"
	"goodfood/assistant-service/internal/adapter/paymentclient"
	"goodfood/assistant-service/internal/adapter/restaurantclient"
	"goodfood/assistant-service/internal/adapter/userclient"
	"goodfood/assistant-service/internal/application"
	"goodfood/assistant-service/internal/config"
)

func main() {
	log := newLogger()

	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("invalid configuration")
	}

	llm, desc, err := llmclient.NewProvider(llmclient.Settings{
		Provider: cfg.AIProvider, BaseURL: cfg.AIBaseURL, APIKey: cfg.AIAPIKey,
		Model: cfg.AIModel, Timeout: cfg.AITimeout,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("invalid AI configuration")
	}
	if desc.Provider == "demo" {
		log.Warn().Msg("no AI provider configured — running with the offline demo FakeProvider")
	} else {
		log.Info().Str("ai_provider", desc.Provider).Str("ai_base_url", desc.BaseURL).Str("ai_model", desc.Model).Msg("AI provider configured")
	}

	uc := application.NewUseCases(
		llm,
		orderclient.New(cfg.OrderServiceURL),
		menuclient.New(cfg.MenuServiceURL),
		paymentclient.New(cfg.PaymentServiceURL),
		userclient.New(cfg.UserServiceURL),
		restaurantclient.New(cfg.FranchiseURL),
	)

	router := httpadapter.NewRouter(httpadapter.NewChatHandler(uc), cfg.JWTSecret, log)

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: router, ReadHeaderTimeout: 5 * time.Second}
	log.Info().Str("port", cfg.Port).Msg("assistant-service started")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal().Err(err).Msg("server stopped")
	}
}

func newLogger() zerolog.Logger {
	level, err := zerolog.ParseLevel(strings.ToLower(os.Getenv("LOG_LEVEL")))
	if err != nil || level == zerolog.NoLevel {
		level = zerolog.InfoLevel
	}
	return zerolog.New(os.Stdout).Level(level).With().Timestamp().Str("service", "assistant-service").Logger()
}
