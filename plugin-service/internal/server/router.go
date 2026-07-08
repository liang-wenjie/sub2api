package server

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/config"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/handler"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/host/auth"
	hostprincipal "github.com/Wei-Shaw/sub2api/plugin-service/internal/host/principal"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/pluginregistry"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/repository"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/service"
	"github.com/Wei-Shaw/sub2api/plugin-service/plugins"
)

func NewRouter(cfg config.Config) http.Handler {
	historyRepo := repository.NewHistoryRepository()

	history := service.NewHistoryService(historyRepo)
	registry := pluginregistry.New()
	if err := plugins.RegisterAll(registry); err != nil {
		panic(err)
	}

	app := handler.NewApp(handler.AppDeps{
		Config: cfg,
	})
	authHandler := auth.NewHandler(auth.HandlerDeps{
		Registry: registry,
	})
	principalMiddleware := hostprincipal.NewMiddleware()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", app.Health)
	mux.HandleFunc("GET /launch", authHandler.Launch)
	mux.HandleFunc("GET /api/plugins", authHandler.ListPlugins)
	mux.HandleFunc("GET /api/plugins/{key}", authHandler.GetPlugin)
	mux.HandleFunc("GET /api/me", principalMiddleware.Require(authHandler.Me))
	registry.RegisterRoutes(mux, pluginregistry.RouteDeps{
		Config:  cfg,
		Auth:    principalMiddleware,
		History: history,
	})
	return app.WithCommonHeaders(mux)
}
