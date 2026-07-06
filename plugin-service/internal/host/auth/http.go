package auth

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/config"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/host/httpx"
	hostsession "github.com/Wei-Shaw/sub2api/plugin-service/internal/host/session"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/pluginregistry"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/service"
)

const (
	CanonicalPluginKey   = "image-generation"
	CanonicalPluginName  = "Image Generation"
	CanonicalPluginEntry = "/app"
)

type HandlerDeps struct {
	Config   config.Config
	Tickets  *service.TicketService
	Sessions *service.SessionService
	Registry *pluginregistry.Registry
}

type Handler struct {
	cfg      config.Config
	tickets  *service.TicketService
	sessions *service.SessionService
	registry *pluginregistry.Registry
}

func NewHandler(deps HandlerDeps) *Handler {
	return &Handler{
		cfg:      deps.Config,
		tickets:  deps.Tickets,
		sessions: deps.Sessions,
		registry: deps.Registry,
	}
}

func (h *Handler) Launch(w http.ResponseWriter, r *http.Request) {
	ticket := r.URL.Query().Get("ticket")
	claims, err := h.tickets.VerifyTicket(ticket)
	if err != nil {
		httpx.WriteError(w, http.StatusUnauthorized, "invalid or expired launch ticket")
		return
	}

	resolvedPluginKey, ok := h.resolvePluginKey(claims.Plugin)
	if !ok {
		httpx.WriteError(w, http.StatusForbidden, "ticket is not valid for this plugin")
		return
	}
	claims.Plugin = resolvedPluginKey

	currentSession, err := h.sessions.CreateFromLaunchClaims(r.Context(), *claims)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to create plugin session")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     hostsession.CookieName,
		Value:    currentSession.ID,
		Path:     "/",
		Expires:  currentSession.ExpiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, normalizeRedirectPath(r.URL.Query().Get("path")), http.StatusFound)
}

func (h *Handler) DevLogin(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.DevLoginEnabled {
		httpx.WriteError(w, http.StatusNotFound, "dev login is disabled")
		return
	}

	userID := int64(parsePositiveInt(r.URL.Query().Get("user_id"), 1))
	role := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("role")))
	if role != model.RoleAdmin {
		role = model.RoleUser
	}

	email := strings.TrimSpace(r.URL.Query().Get("email"))
	if email == "" {
		email = "dev@example.com"
	}
	username := strings.TrimSpace(r.URL.Query().Get("username"))
	if username == "" {
		username = "dev-user"
	}

	currentSession, err := h.sessions.CreateFromLaunchClaims(r.Context(), model.LaunchClaims{
		UserID:   userID,
		Role:     role,
		Email:    email,
		Username: username,
		Plugin:   h.defaultPluginKey(),
	})
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to create plugin session")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     hostsession.CookieName,
		Value:    currentSession.ID,
		Path:     "/",
		Expires:  currentSession.ExpiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, normalizeRedirectPath(r.URL.Query().Get("path")), http.StatusFound)
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

func normalizeRedirectPath(raw string) string {
	if raw == "" {
		return "/app"
	}
	if strings.HasPrefix(raw, "//") {
		return "/app"
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.IsAbs() {
		return "/app"
	}
	if !strings.HasPrefix(raw, "/") {
		return "/app"
	}
	return raw
}

func parsePositiveInt(raw string, fallback int) int {
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
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

func DefaultPluginMetadata() pluginregistry.Metadata {
	return pluginregistry.Metadata{
		Key:              CanonicalPluginKey,
		Name:             CanonicalPluginName,
		Description:      "Generate images from the host plugin service",
		Enabled:          true,
		FrontendMode:     pluginregistry.FrontendModeHosted,
		DefaultEntryPath: CanonicalPluginEntry,
	}
}

func (h *Handler) defaultPluginKey() string {
	if _, ok := h.registry.Get(CanonicalPluginKey); ok {
		return CanonicalPluginKey
	}
	for _, metadata := range h.registry.List() {
		if strings.TrimSpace(metadata.Key) != "" {
			return metadata.Key
		}
	}
	return h.cfg.PluginKey
}

func (h *Handler) resolvePluginKey(raw string) (string, bool) {
	key := strings.TrimSpace(raw)
	if key == "" {
		return "", false
	}
	if _, ok := h.registry.Get(key); ok {
		return key, true
	}
	if key == h.cfg.PluginKey {
		return h.defaultPluginKey(), true
	}
	return "", false
}
