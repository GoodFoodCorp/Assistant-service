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
	"goodfood/assistant-service/internal/adapter/settingsstore"
	"goodfood/assistant-service/internal/adapter/userclient"
	"goodfood/assistant-service/internal/application"
	"goodfood/assistant-service/internal/config"
	"goodfood/assistant-service/internal/domain"
)

func main() {
	log := newLogger()

	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("invalid configuration")
	}

	// Admins can change the provider/model/key from the back-office; what they
	// save overrides the environment, which stays the default.
	factory := func(s application.AISettings) (domain.LLMProvider, error) {
		p, _, err := llmclient.NewProvider(llmclient.Settings{
			Provider: s.Provider, BaseURL: s.BaseURL, APIKey: s.APIKey, Model: s.Model,
			Timeout: time.Duration(s.TimeoutSeconds) * time.Second,
		})
		return p, err
	}
	llm := llmclient.NewSwitchable(llmclient.NewFakeProvider())
	aiConfig := application.NewAIConfig(application.AISettings{
		Provider: cfg.AIProvider, BaseURL: cfg.AIBaseURL, APIKey: cfg.AIAPIKey, Model: cfg.AIModel,
		TimeoutSeconds: int(cfg.AITimeout / time.Second),
	}, settingsstore.NewFile(cfg.AISettingsFile), llm, factory)
	if err := aiConfig.Init(); err != nil {
		log.Fatal().Err(err).Msg("invalid AI configuration")
	}
	if v, err := aiConfig.View(application.Actor{RoleSlugs: []string{"admin"}}); err == nil {
		if v.Provider == "" || v.Provider == "demo" {
			log.Warn().Msg("no AI provider configured — running with the offline demo FakeProvider")
		} else {
			log.Info().Str("ai_provider", v.Provider).Str("ai_model", v.Model).Str("source", v.Source).Msg("AI provider configured")
		}
	}

	uc := application.NewUseCases(
		llm,
		orderclient.New(cfg.OrderServiceURL),
		menuclient.New(cfg.MenuServiceURL),
		paymentclient.New(cfg.PaymentServiceURL),
		userclient.New(cfg.UserServiceURL),
		restaurantclient.New(cfg.FranchiseURL),
	).WithDeliveryChecks(restaurantclient.New(cfg.FranchiseURL)).
		WithHistorySigner(application.NewHistorySigner(cfg.HistoryKey))

	router := httpadapter.NewRouter(httpadapter.NewChatHandler(uc), httpadapter.NewAdminHandler(aiConfig),
		httpadapter.JWTConfig{Secret: cfg.JWTSecret, Issuer: cfg.JWTIssuer, Audience: cfg.JWTAudience},
		httpadapter.NewRateLimiter(cfg.ChatPerMinute, cfg.ChatBurst), log)

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
