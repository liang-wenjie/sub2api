package server

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/config"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/handler"
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
	principalMiddleware := hostprincipal.NewMiddleware()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", app.Health)
	registry.RegisterRoutes(mux, pluginregistry.RouteDeps{
		Config:  cfg,
		Auth:    principalMiddleware,
		History: history,
	})
	return app.WithCommonHeaders(mux)
}
