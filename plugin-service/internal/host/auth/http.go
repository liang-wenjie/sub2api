package auth

import (
	"net/http"
	"net/url"
	"strings"

	hostprincipal "github.com/Wei-Shaw/sub2api/plugin-service/internal/host/principal"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/host/httpx"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/pluginregistry"
)

type HandlerDeps struct {
	Registry *pluginregistry.Registry
}

type Handler struct {
	registry *pluginregistry.Registry
}

func NewHandler(deps HandlerDeps) *Handler {
	return &Handler{
		registry: deps.Registry,
	}
}

func (h *Handler) Launch(w http.ResponseWriter, r *http.Request) {
	pluginKey := strings.TrimSpace(r.URL.Query().Get("plugin"))
	if pluginKey == "" {
		pluginKey = h.defaultPluginKey()
	}
	resolvedPluginKey, ok := h.resolvePluginKey(pluginKey)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "plugin not found")
		return
	}

	if _, err := hostprincipal.LoadCurrentPrincipal(r, resolvedPluginKey); err != nil {
		httpx.WriteError(w, http.StatusUnauthorized, err.Error())
		return
	}

	redirectPath := normalizeRedirectPath(r.URL.Query().Get("path"), h.defaultPluginEntry(resolvedPluginKey))
	http.Redirect(w, r, appendLaunchQueryParams(redirectPath, r), http.StatusFound)
}

func (h *Handler) Me(w http.ResponseWriter, _ *http.Request, principal model.CurrentPrincipal) {
	httpx.WriteJSON(w, http.StatusOK, principal)
}

func (h *Handler) ListPlugins(w http.ResponseWriter, _ *http.Request) {
	registered := h.registry.List()
	items := make([]model.PluginMetadata, 0, len(registered))
	for _, metadata := range registered {
		items = append(items, toPluginMetadata(metadata))
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"items": items,
	})
}

func (h *Handler) GetPlugin(w http.ResponseWriter, r *http.Request) {
	plugin, ok := h.registry.Get(r.PathValue("key"))
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "plugin not found")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toPluginMetadata(plugin.Metadata()))
}

func normalizeRedirectPath(raw string, fallback string) string {
	if strings.TrimSpace(fallback) == "" {
		fallback = "/"
	}
	if raw == "" {
		return fallback
	}
	if strings.HasPrefix(raw, "//") {
		return fallback
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.IsAbs() {
		return fallback
	}
	if !strings.HasPrefix(raw, "/") {
		return fallback
	}
	return raw
}

func appendLaunchQueryParams(target string, r *http.Request) string {
	if r == nil {
		return target
	}

	parsed, err := url.Parse(target)
	if err != nil {
		return target
	}

	query := parsed.Query()
	for _, key := range []string{"user_id", "token", "session", "theme", "lang", "ui_mode", "src_host", "src_url"} {
		if strings.TrimSpace(query.Get(key)) != "" {
			continue
		}
		if value := strings.TrimSpace(r.URL.Query().Get(key)); value != "" {
			query.Set(key, value)
		}
	}

	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func toPluginMetadata(metadata pluginregistry.Metadata) model.PluginMetadata {
	return model.PluginMetadata{
		Key:              metadata.Key,
		Name:             metadata.Name,
		Description:      metadata.Description,
		Enabled:          metadata.Enabled,
		FrontendMode:     string(metadata.FrontendMode),
		DefaultEntryPath: metadata.DefaultEntryPath,
		RemoteEntryURL:   metadata.RemoteEntryURL,
	}
}

func (h *Handler) defaultPluginKey() string {
	for _, metadata := range h.registry.List() {
		if strings.TrimSpace(metadata.Key) != "" {
			return metadata.Key
		}
	}
	return ""
}

func (h *Handler) defaultPluginEntry(pluginKey string) string {
	if plugin, ok := h.registry.Get(pluginKey); ok {
		entry := strings.TrimSpace(plugin.Metadata().DefaultEntryPath)
		if entry != "" {
			return entry
		}
	}

	for _, metadata := range h.registry.List() {
		entry := strings.TrimSpace(metadata.DefaultEntryPath)
		if entry != "" {
			return entry
		}
	}

	return "/"
}

func (h *Handler) resolvePluginKey(raw string) (string, bool) {
	key := strings.TrimSpace(raw)
	if key == "" {
		return "", false
	}
	if _, ok := h.registry.Get(key); ok {
		return key, true
	}
	return "", false
}
