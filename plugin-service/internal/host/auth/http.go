package auth

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

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
	CanonicalPluginEntry = "/plugins/image-generation"
)

type HandlerDeps struct {
	Config   config.Config
	Sessions *service.SessionService
	Registry *pluginregistry.Registry
}

type Handler struct {
	cfg      config.Config
	sessions *service.SessionService
	registry *pluginregistry.Registry
}

func NewHandler(deps HandlerDeps) *Handler {
	return &Handler{
		cfg:      deps.Config,
		sessions: deps.Sessions,
		registry: deps.Registry,
	}
}

func (h *Handler) Launch(w http.ResponseWriter, r *http.Request) {
	claims, err := h.resolveLaunchClaimsFromMainSite(r)
	if err != nil {
		httpx.WriteError(w, http.StatusUnauthorized, err.Error())
		return
	}

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
	http.Redirect(w, r, normalizeRedirectPath(r.URL.Query().Get("path"), h.defaultPluginEntry(claims.Plugin)), http.StatusFound)
}

func (h *Handler) resolveLaunchClaimsFromMainSite(r *http.Request) (*model.LaunchClaims, error) {
	pluginKey := strings.TrimSpace(r.URL.Query().Get("plugin"))
	if pluginKey == "" {
		pluginKey = h.defaultPluginKey()
	}
	resolvedPluginKey, ok := h.resolvePluginKey(pluginKey)
	if !ok {
		return nil, errUnauthorized("plugin not found")
	}

	profile, err := h.fetchMainSiteProfile(r)
	if err != nil {
		return nil, err
	}

	role := model.RoleUser
	if strings.EqualFold(strings.TrimSpace(profile.Role), model.RoleAdmin) {
		role = model.RoleAdmin
	}

	return &model.LaunchClaims{
		UserID:   profile.ID,
		Role:     role,
		Email:    strings.TrimSpace(profile.Email),
		Username: strings.TrimSpace(profile.Username),
		Plugin:   resolvedPluginKey,
		IssuedAt: time.Now().UTC().Unix(),
	}, nil
}

type mainSiteProfileEnvelope struct {
	Code int                 `json:"code"`
	Data mainSiteProfileData `json:"data"`
}

type mainSiteProfileData struct {
	ID       int64  `json:"id"`
	Email    string `json:"email"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

func (h *Handler) fetchMainSiteProfile(r *http.Request) (*mainSiteProfileData, error) {
	mainSiteBase := strings.TrimRight(strings.TrimSpace(h.cfg.MainSiteOrigin), "/")
	if mainSiteBase == "" {
		return nil, errUnauthorized("main site origin is not configured")
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, mainSiteBase+"/api/v1/auth/me", nil)
	if err != nil {
		return nil, errUnauthorized("failed to build main site request")
	}

	if token := firstNonEmptyQuery(r, "token", "session"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if authHeader := strings.TrimSpace(r.Header.Get("Authorization")); authHeader != "" && req.Header.Get("Authorization") == "" {
		req.Header.Set("Authorization", authHeader)
	}
	if rawCookie := strings.TrimSpace(r.Header.Get("Cookie")); rawCookie != "" {
		req.Header.Set("Cookie", rawCookie)
	}
	if forwardedCookie := strings.TrimSpace(r.URL.Query().Get("cookie")); forwardedCookie != "" && req.Header.Get("Cookie") == "" {
		req.Header.Set("Cookie", forwardedCookie)
	}
	if acceptLanguage := strings.TrimSpace(r.Header.Get("Accept-Language")); acceptLanguage != "" {
		req.Header.Set("Accept-Language", acceptLanguage)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errUnauthorized("failed to load current user from main site")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errUnauthorized("failed to load current user from main site")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errUnauthorized("failed to read current user from main site")
	}

	var envelope mainSiteProfileEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, errUnauthorized("failed to parse current user from main site")
	}
	if envelope.Code != 0 || envelope.Data.ID <= 0 {
		return nil, errUnauthorized("main site authentication required")
	}
	return &envelope.Data, nil
}

func firstNonEmptyQuery(r *http.Request, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(r.URL.Query().Get(key)); value != "" {
			return value
		}
	}
	return ""
}

type unauthorizedError struct {
	message string
}

func (e unauthorizedError) Error() string {
	return e.message
}

func errUnauthorized(message string) error {
	return unauthorizedError{message: message}
}

func (h *Handler) DevLogin(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.DevLoginEnabled {
		httpx.WriteError(w, http.StatusNotFound, "dev login is disabled")
		return
	}

	pluginKey := strings.TrimSpace(r.URL.Query().Get("plugin"))
	if pluginKey == "" {
		pluginKey = h.defaultPluginKey()
	} else {
		resolvedPluginKey, ok := h.resolvePluginKey(pluginKey)
		if !ok {
			httpx.WriteError(w, http.StatusNotFound, "plugin not found")
			return
		}
		pluginKey = resolvedPluginKey
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
		Plugin:   pluginKey,
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
	http.Redirect(w, r, normalizeRedirectPath(r.URL.Query().Get("path"), h.defaultPluginEntry(pluginKey)), http.StatusFound)
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
		fallback = CanonicalPluginEntry
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

func (h *Handler) defaultPluginEntry(pluginKey string) string {
	if plugin, ok := h.registry.Get(pluginKey); ok {
		entry := strings.TrimSpace(plugin.Metadata().DefaultEntryPath)
		if entry != "" {
			return entry
		}
	}

	if pluginKey != CanonicalPluginKey {
		if plugin, ok := h.registry.Get(CanonicalPluginKey); ok {
			entry := strings.TrimSpace(plugin.Metadata().DefaultEntryPath)
			if entry != "" {
				return entry
			}
		}
	}

	return CanonicalPluginEntry
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
