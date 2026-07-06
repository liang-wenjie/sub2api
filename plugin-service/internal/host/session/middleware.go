package session

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/host/httpx"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/service"
)

const CookieName = "plugin_session"

type Middleware struct {
	sessions *service.SessionService
}

func NewMiddleware(sessions *service.SessionService) *Middleware {
	return &Middleware{sessions: sessions}
}

func (m *Middleware) Require(next func(http.ResponseWriter, *http.Request, model.CurrentPrincipal)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(CookieName)
		if err != nil || strings.TrimSpace(cookie.Value) == "" {
			httpx.WriteError(w, http.StatusUnauthorized, "plugin session is required")
			return
		}

		currentSession, ok := m.sessions.Get(r.Context(), cookie.Value)
		if !ok {
			http.SetCookie(w, &http.Cookie{
				Name:     CookieName,
				Value:    "",
				Path:     "/",
				MaxAge:   -1,
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
			httpx.WriteError(w, http.StatusUnauthorized, "plugin session expired")
			return
		}

		next(w, r, currentSession.Principal)
	}
}
