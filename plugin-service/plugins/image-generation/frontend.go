package imagegeneration

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

func RegisterFrontend(mux *http.ServeMux) {
	webRoot := webRoot()
	assetRoot := filepath.Join(webRoot, "assets")
	indexPath := indexPath(webRoot)

	mux.HandleFunc("GET /plugins/image-generation", func(w http.ResponseWriter, r *http.Request) {
		disableFrontendCache(w)
		body, err := os.ReadFile(indexPath)
		if err != nil {
			http.Error(w, "plugin frontend not found", http.StatusNotFound)
			return
		}
		html := string(body)
		html = strings.ReplaceAll(html, "/plugins/image-generation/assets/app.js", "/plugins/image-generation/assets/app.js?v="+assetVersion(assetRoot, "app.js"))
		html = strings.ReplaceAll(html, "/plugins/image-generation/assets/app.css", "/plugins/image-generation/assets/app.css?v="+assetVersion(assetRoot, "app.css"))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
	})

	assets := http.StripPrefix("/plugins/image-generation/assets/", http.FileServer(http.Dir(assetRoot)))
	mux.Handle("GET /plugins/image-generation/assets/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		disableFrontendCache(w)
		assets.ServeHTTP(w, r)
	}))
}

func disableFrontendCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

func assetVersion(assetRoot string, name string) string {
	info, err := os.Stat(filepath.Join(assetRoot, name))
	if err != nil {
		return "0"
	}
	return strconv.FormatInt(info.ModTime().UnixNano(), 10)
}

func indexPath(webRoot string) string {
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

func webRoot() string {
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

	pluginRoot := filepath.Clean(filepath.Dir(currentFile))
	return filepath.Join(pluginRoot, "web")
}
