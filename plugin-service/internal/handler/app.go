package handler

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/config"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/repository"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/service"
)

const sessionCookieName = "plugin_session"

type principalContextKey struct{}

type AppDeps struct {
	Config     config.Config
	Tickets    *service.TicketService
	Sessions   *service.SessionService
	History    *service.HistoryService
	Generation *service.GenerationService
}

type App struct {
	cfg        config.Config
	tickets    *service.TicketService
	sessions   *service.SessionService
	history    *service.HistoryService
	generation *service.GenerationService
}

func NewApp(deps AppDeps) *App {
	return &App{
		cfg:        deps.Config,
		tickets:    deps.Tickets,
		sessions:   deps.Sessions,
		history:    deps.History,
		generation: deps.Generation,
	}
}

func (a *App) WithCommonHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		if a.cfg.MainSiteOrigin != "" {
			w.Header().Set("Content-Security-Policy", "frame-ancestors "+a.cfg.MainSiteOrigin)
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"plugin": a.cfg.PluginKey,
	})
}

func (a *App) AppPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Plugin Service</title>
  <style>
    :root { color-scheme: light; }
    body { margin: 0; font-family: "Segoe UI", "PingFang SC", sans-serif; background: #f5f7fb; color: #18202a; }
    main { max-width: 960px; margin: 0 auto; padding: 40px 20px 72px; }
    h1 { margin: 0 0 8px; font-size: 28px; }
    p { line-height: 1.6; color: #4d5a6a; }
    .panel { background: #fff; border: 1px solid #d9e0ea; border-radius: 8px; padding: 20px; margin-top: 20px; }
    .row { display: grid; grid-template-columns: 180px 1fr; gap: 8px 16px; }
    .k { color: #607085; font-size: 13px; text-transform: uppercase; letter-spacing: .04em; }
    .v { font-family: Consolas, "Courier New", monospace; font-size: 14px; color: #18202a; word-break: break-all; }
    .links a { display: inline-block; margin: 0 12px 12px 0; color: #0f62fe; text-decoration: none; }
    .links a:hover { text-decoration: underline; }
    code { background: #eef2f7; border-radius: 4px; padding: 2px 6px; }
  </style>
</head>
<body>
  <main>
    <h1>Plugin Service Ready</h1>
    <p>这个页面说明插件服务本身已经可访问。先完成登录，再调用生成和历史接口。</p>
    <section class="panel">
      <div class="row">
        <div class="k">Plugin</div>
        <div class="v">` + html.EscapeString(a.cfg.PluginKey) + `</div>
        <div class="k">History</div>
        <div class="v">` + html.EscapeString(strconv.FormatBool(a.cfg.HistoryEnabled)) + `</div>
        <div class="k">Dev Login</div>
        <div class="v">` + html.EscapeString(strconv.FormatBool(a.cfg.DevLoginEnabled)) + `</div>
      </div>
    </section>
    <section class="panel">
      <p>开发联调时先访问 <code>/dev/login?user_id=7&role=admin&email=admin@example.com&username=dev-admin&path=/app</code>。</p>
      <div class="links">
        <a href="/api/me" target="_blank" rel="noreferrer">/api/me</a>
        <a href="/api/config" target="_blank" rel="noreferrer">/api/config</a>
        <a href="/api/history" target="_blank" rel="noreferrer">/api/history</a>
        <a href="/healthz" target="_blank" rel="noreferrer">/healthz</a>
      </div>
    </section>
  </main>
</body>
</html>`))
}

func (a *App) Launch(w http.ResponseWriter, r *http.Request) {
	ticket := r.URL.Query().Get("ticket")
	claims, err := a.tickets.VerifyTicket(ticket)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid or expired launch ticket")
		return
	}
	if claims.Plugin != a.cfg.PluginKey {
		writeError(w, http.StatusForbidden, "ticket is not valid for this plugin")
		return
	}
	session, err := a.sessions.CreateFromLaunchClaims(r.Context(), *claims)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create plugin session")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    session.ID,
		Path:     "/",
		Expires:  session.ExpiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	target := normalizeRedirectPath(r.URL.Query().Get("path"))
	http.Redirect(w, r, target, http.StatusFound)
}

func (a *App) DevLogin(w http.ResponseWriter, r *http.Request) {
	if !a.cfg.DevLoginEnabled {
		writeError(w, http.StatusNotFound, "dev login is disabled")
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
	session, err := a.sessions.CreateFromLaunchClaims(r.Context(), model.LaunchClaims{
		UserID:   userID,
		Role:     role,
		Email:    email,
		Username: username,
		Plugin:   a.cfg.PluginKey,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create plugin session")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    session.ID,
		Path:     "/",
		Expires:  session.ExpiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	target := normalizeRedirectPath(r.URL.Query().Get("path"))
	http.Redirect(w, r, target, http.StatusFound)
}

func (a *App) RequireSession(next func(http.ResponseWriter, *http.Request, model.CurrentPrincipal)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || strings.TrimSpace(cookie.Value) == "" {
			writeError(w, http.StatusUnauthorized, "plugin session is required")
			return
		}
		session, ok := a.sessions.Get(r.Context(), cookie.Value)
		if !ok {
			writeError(w, http.StatusUnauthorized, "plugin session expired")
			return
		}
		ctx := context.WithValue(r.Context(), principalContextKey{}, session.Principal)
		next(w, r.WithContext(ctx), session.Principal)
	}
}

func (a *App) Me(w http.ResponseWriter, _ *http.Request, principal model.CurrentPrincipal) {
	writeJSON(w, http.StatusOK, principal)
}

func (a *App) Config(w http.ResponseWriter, _ *http.Request, principal model.CurrentPrincipal) {
	writeJSON(w, http.StatusOK, map[string]any{
		"plugin_key":      a.cfg.PluginKey,
		"history_enabled": a.cfg.HistoryEnabled,
		"user_id":         principal.UserID,
		"role":            principal.Role,
	})
}

func (a *App) Generate(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	var req model.GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	resp, err := a.generation.Generate(r.Context(), principal, req)
	if err != nil {
		if errors.Is(err, service.ErrPromptRequired) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "generation failed")
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (a *App) ListHistory(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	records, err := a.history.List(r.Context(), principal, parseHistoryQuery(r.URL.Query()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list history")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": records,
	})
}

func (a *App) GetHistory(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	record, err := a.history.Get(r.Context(), principal, r.PathValue("id"))
	writeRecordOrError(w, record, err)
}

func (a *App) RetryHistory(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	resp, err := a.generation.Retry(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (a *App) CancelHistory(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	record, err := a.generation.Cancel(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func normalizeRedirectPath(raw string) string {
	if raw == "" {
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

func parseHistoryQuery(values url.Values) model.HistoryQuery {
	return model.HistoryQuery{
		Page:     parsePositiveInt(values.Get("page"), 1),
		PageSize: parsePositiveInt(values.Get("page_size"), 20),
	}
}

func parsePositiveInt(raw string, fallback int) int {
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func writeRecordOrError(w http.ResponseWriter, record *model.HistoryRecord, err error) {
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, http.StatusNotFound, "history record not found")
	case errors.Is(err, service.ErrHistoryForbidden):
		writeError(w, http.StatusForbidden, "history record is not accessible")
	default:
		writeError(w, http.StatusConflict, err.Error())
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error":     message,
		"status":    status,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}
