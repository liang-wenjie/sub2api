package server

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/config"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/handler"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/repository"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/service"
)

func NewRouter(cfg config.Config) http.Handler {
	sessionRepo := repository.NewSessionRepository()
	historyRepo := repository.NewHistoryRepository()

	tickets := service.NewTicketService(cfg.LaunchSharedSecret)
	sessions := service.NewSessionService(sessionRepo, cfg.SessionTTL)
	history := service.NewHistoryService(historyRepo)
	generation := service.NewGenerationService(history, service.GenerationServiceOptions{
		ProviderBaseURL: cfg.ImageProviderBaseURL,
	})

	app := handler.NewApp(handler.AppDeps{
		Config:     cfg,
		Tickets:    tickets,
		Sessions:   sessions,
		History:    history,
		Generation: generation,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", app.Health)
	mux.HandleFunc("GET /app", app.AppPage)
	mux.HandleFunc("GET /launch", app.Launch)
	mux.HandleFunc("GET /dev/login", app.DevLogin)
	mux.HandleFunc("GET /api/me", app.RequireSession(app.Me))
	mux.HandleFunc("GET /api/config", app.RequireSession(app.Config))
	mux.HandleFunc("POST /api/generate", app.RequireSession(app.Generate))
	mux.HandleFunc("GET /api/creations", app.RequireSession(app.ListCreations))
	mux.HandleFunc("GET /api/history", app.RequireSession(app.ListHistory))
	mux.HandleFunc("GET /api/history/{id}", app.RequireSession(app.GetHistory))
	mux.HandleFunc("POST /api/history/{id}/retry", app.RequireSession(app.RetryHistory))
	mux.HandleFunc("POST /api/history/{id}/cancel", app.RequireSession(app.CancelHistory))
	return app.WithCommonHeaders(mux)
}
