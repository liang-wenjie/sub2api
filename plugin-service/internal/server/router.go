package server

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/config"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/handler"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/host/auth"
	hostsession "github.com/Wei-Shaw/sub2api/plugin-service/internal/host/session"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/pluginregistry"
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
	registry := pluginregistry.New()
	_ = registry.Register(pluginregistry.StaticPlugin{
		Meta: auth.DefaultPluginMetadata(),
	})

	app := handler.NewApp(handler.AppDeps{
		Config:     cfg,
		History:    history,
		Generation: generation,
	})
	authHandler := auth.NewHandler(auth.HandlerDeps{
		Config:   cfg,
		Tickets:  tickets,
		Sessions: sessions,
		Registry: registry,
	})
	sessionMiddleware := hostsession.NewMiddleware(sessions)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", app.Health)
	mux.HandleFunc("GET /app", app.AppPage)
	mux.HandleFunc("GET /launch", authHandler.Launch)
	mux.HandleFunc("GET /dev/login", authHandler.DevLogin)
	mux.HandleFunc("GET /api/plugins", authHandler.ListPlugins)
	mux.HandleFunc("GET /api/plugins/{key}", authHandler.GetPlugin)
	mux.HandleFunc("GET /api/me", sessionMiddleware.Require(authHandler.Me))
	mux.HandleFunc("GET /api/config", sessionMiddleware.Require(app.Config))
	mux.HandleFunc("POST /api/generate", sessionMiddleware.Require(app.Generate))
	mux.HandleFunc("GET /api/creations", sessionMiddleware.Require(app.ListCreations))
	mux.HandleFunc("GET /api/history", sessionMiddleware.Require(app.ListHistory))
	mux.HandleFunc("GET /api/history/{id}", sessionMiddleware.Require(app.GetHistory))
	mux.HandleFunc("POST /api/history/{id}/retry", sessionMiddleware.Require(app.RetryHistory))
	mux.HandleFunc("POST /api/history/{id}/cancel", sessionMiddleware.Require(app.CancelHistory))
	return app.WithCommonHeaders(mux)
}
