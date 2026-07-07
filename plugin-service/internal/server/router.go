package server

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

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
	mux.HandleFunc("GET /launch", authHandler.Launch)
	registerImageGenerationFrontend(mux)
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
		disablePluginFrontendCache(w)
		body, err := os.ReadFile(indexPath)
		if err != nil {
			http.Error(w, "plugin frontend not found", http.StatusNotFound)
			return
		}
		html := string(body)
		html = strings.ReplaceAll(html, "/plugins/image-generation/assets/app.js", "/plugins/image-generation/assets/app.js?v="+imageGenerationAssetVersion(assetRoot, "app.js"))
		html = strings.ReplaceAll(html, "/plugins/image-generation/assets/app.css", "/plugins/image-generation/assets/app.css?v="+imageGenerationAssetVersion(assetRoot, "app.css"))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
	})

	assets := http.StripPrefix("/plugins/image-generation/assets/", http.FileServer(http.Dir(assetRoot)))
	mux.Handle("GET /plugins/image-generation/assets/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		disablePluginFrontendCache(w)
		assets.ServeHTTP(w, r)
	}))
}

func disablePluginFrontendCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

func imageGenerationAssetVersion(assetRoot string, name string) string {
	info, err := os.Stat(filepath.Join(assetRoot, name))
	if err != nil {
		return "0"
	}
	return strconv.FormatInt(info.ModTime().UnixNano(), 10)
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
