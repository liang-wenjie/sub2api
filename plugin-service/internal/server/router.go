package server

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/config"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/handler"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/host/auth"
	hostsession "github.com/Wei-Shaw/sub2api/plugin-service/internal/host/session"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/pluginregistry"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/repository"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/service"
	imagebackend "github.com/Wei-Shaw/sub2api/plugin-service/plugins/image-generation/backend"
	imagemanifest "github.com/Wei-Shaw/sub2api/plugin-service/plugins/image-generation/manifest"
)

func NewRouter(cfg config.Config) http.Handler {
	sessionRepo := repository.NewSessionRepository()
	historyRepo := repository.NewHistoryRepository()

	sessions := service.NewSessionService(sessionRepo, cfg.SessionTTL)
	history := service.NewHistoryService(historyRepo)
	generation := service.NewGenerationService(history, service.GenerationServiceOptions{})
	registry := pluginregistry.New()
	if err := registry.Register(imagemanifest.Plugin()); err != nil {
		panic(err)
	}

	app := handler.NewApp(handler.AppDeps{
		Config: cfg,
	})
	imageHandler := imagebackend.NewHandler(imagebackend.HandlerDeps{
		Config:     cfg,
		PluginKey:  imagemanifest.Key,
		History:    history,
		Generation: generation,
	})
	authHandler := auth.NewHandler(auth.HandlerDeps{
		Config:   cfg,
		Sessions: sessions,
		Registry: registry,
	})
	sessionMiddleware := hostsession.NewMiddleware(sessions)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", app.Health)
	registerImageGenerationFrontend(mux)
	mux.HandleFunc("GET /launch", authHandler.Launch)
	mux.HandleFunc("GET /dev/login", authHandler.DevLogin)
	mux.HandleFunc("GET /api/plugins", authHandler.ListPlugins)
	mux.HandleFunc("GET /api/plugins/{key}", authHandler.GetPlugin)
	mux.HandleFunc("GET /api/me", sessionMiddleware.Require(authHandler.Me))
	imagebackend.RegisterRoutes(mux, sessionMiddleware, imageHandler)
	return app.WithCommonHeaders(mux)
}

func registerImageGenerationFrontend(mux *http.ServeMux) {
	webRoot := imageGenerationWebRoot()
	assetRoot := filepath.Join(webRoot, "assets")
	indexPath := imageGenerationIndexPath(webRoot)

	mux.HandleFunc("GET /plugins/image-generation", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, indexPath)
	})

	mux.Handle("GET /plugins/image-generation/assets/", http.StripPrefix("/plugins/image-generation/assets/", http.FileServer(http.Dir(assetRoot))))

	mux.HandleFunc("GET /app", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/plugins/image-generation", http.StatusFound)
	})
}

func imageGenerationIndexPath(webRoot string) string {
	for _, name := range []string{
		"index.html",
		"plugin-image-generation.html",
		filepath.Join("plugin-image-generation", "index.html"),
	} {
		candidate := filepath.Join(webRoot, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return filepath.Join(webRoot, "index.html")
}

func imageGenerationWebRoot() string {
	for _, candidate := range []string{
		filepath.Join("plugins", "image-generation", "web"),
		filepath.Join("plugin-service", "plugins", "image-generation", "web"),
	} {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return filepath.Join("plugins", "image-generation", "web")
	}

	pluginServiceRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	return filepath.Join(pluginServiceRoot, "plugins", "image-generation", "web")
}
