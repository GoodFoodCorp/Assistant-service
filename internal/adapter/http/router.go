package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
)

func NewRouter(handler *ChatHandler, admin *AdminHandler, jwtCfg JWTConfig, limiter *RateLimiter, log zerolog.Logger) http.Handler {
	r := chi.NewRouter()

	r.Use(chimw.Recoverer)
	r.Use(RequestID)
	r.Use(Logger(log))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	r.Route("/api/chat", func(r chi.Router) {
		r.Use(Auth(jwtCfg))
		r.With(limiter.Limit).Post("/messages", handler.SendMessage)
		r.Route("/admin/settings", func(r chi.Router) {
			r.Get("/", admin.Get)
			r.Put("/", admin.Put)
			r.Delete("/", admin.Delete)
			r.Post("/test", admin.Test)
		})
	})

	return r
}
