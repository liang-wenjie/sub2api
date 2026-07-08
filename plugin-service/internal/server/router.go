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
	"github.com/Wei-Shaw/sub2api/plugin-service/plugins"
)

func NewRouter(cfg config.Config) http.Handler {
	sessionRepo := repository.NewSessionRepository()
	historyRepo := repository.NewHistoryRepository()

	sessions := service.NewSessionService(sessionRepo, cfg.SessionTTL)
	history := service.NewHistoryService(historyRepo)
	registry := pluginregistry.New()
	if err := plugins.RegisterAll(registry); err != nil {
		panic(err)
	}

	app := handler.NewApp(handler.AppDeps{
		Config: cfg,
	})
	authHandler := auth.NewHandler(auth.HandlerDeps{
		Config:   cfg,
		Sessions: sessions,
		Registry: registry,
	})
	sessionMiddleware := hostsession.NewMiddleware(sessions)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", app.Health)
	mux.HandleFunc("GET /launch", authHandler.Launch)
	mux.HandleFunc("GET /dev/login", authHandler.DevLogin)
	mux.HandleFunc("GET /api/plugins", authHandler.ListPlugins)
	mux.HandleFunc("GET /api/plugins/{key}", authHandler.GetPlugin)
	mux.HandleFunc("GET /api/me", sessionMiddleware.Require(authHandler.Me))
	registry.RegisterRoutes(mux, pluginregistry.RouteDeps{
		Config:            cfg,
		SessionMiddleware: sessionMiddleware,
		History:           history,
	})
	return app.WithCommonHeaders(mux)
}
