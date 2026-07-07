package backend

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/config"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/host/httpx"
	hostsession "github.com/Wei-Shaw/sub2api/plugin-service/internal/host/session"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/service"
	imagemanifest "github.com/Wei-Shaw/sub2api/plugin-service/plugins/image-generation/manifest"
)

const apiBasePath = "/api/plugins/" + imagemanifest.Key

type HandlerDeps struct {
	Config     config.Config
	PluginKey  string
	History    *service.HistoryService
	Generation *service.GenerationService
}

type Handler struct {
	cfg        config.Config
	pluginKey  string
	history    *service.HistoryService
	generation *service.GenerationService
}

func NewHandler(deps HandlerDeps) *Handler {
	pluginKey := deps.PluginKey
	if pluginKey == "" {
		pluginKey = imagemanifest.Key
	}

	return &Handler{
		cfg:        deps.Config,
		pluginKey:  pluginKey,
		history:    deps.History,
		generation: deps.Generation,
	}
}

func RegisterRoutes(mux *http.ServeMux, sessionMiddleware *hostsession.Middleware, handler *Handler) {
	mux.HandleFunc("GET "+apiBasePath+"/me", sessionMiddleware.Require(handler.Me))
	mux.HandleFunc("GET "+apiBasePath+"/config", sessionMiddleware.Require(handler.Config))
	mux.HandleFunc("POST "+apiBasePath+"/generate", sessionMiddleware.Require(handler.Generate))
	mux.HandleFunc("GET "+apiBasePath+"/creations", sessionMiddleware.Require(handler.ListCreations))
	mux.HandleFunc("GET "+apiBasePath+"/history", sessionMiddleware.Require(handler.ListHistory))
	mux.HandleFunc("GET "+apiBasePath+"/history/{id}", sessionMiddleware.Require(handler.GetHistory))
	mux.HandleFunc("POST "+apiBasePath+"/history/{id}/retry", sessionMiddleware.Require(handler.RetryHistory))
	mux.HandleFunc("POST "+apiBasePath+"/history/{id}/cancel", sessionMiddleware.Require(handler.CancelHistory))
}

func (h *Handler) Config(w http.ResponseWriter, _ *http.Request, principal model.CurrentPrincipal) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"plugin_key":      h.pluginKey,
		"history_enabled": h.cfg.HistoryEnabled,
		"user_id":         principal.UserID,
		"role":            principal.Role,
	})
}

func (h *Handler) Me(w http.ResponseWriter, _ *http.Request, principal model.CurrentPrincipal) {
	httpx.WriteJSON(w, http.StatusOK, principal)
}

func (h *Handler) Generate(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	var req model.GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	resp, err := h.generation.Generate(r.Context(), principal, resolveMainServiceBaseURL(r, h.cfg), req)
	if err != nil {
		if errors.Is(err, service.ErrPromptRequired) || errors.Is(err, service.ErrProviderKeyRequired) {
			httpx.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "generation failed")
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, resp)
}

func (h *Handler) ListCreations(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	records, err := h.generation.ListCreations(r.Context(), principal, parseHistoryQuery(r.URL.Query()))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to list creations")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"items": records,
	})
}

func (h *Handler) ListHistory(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	records, err := h.history.List(r.Context(), principal, parseHistoryQuery(r.URL.Query()))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to list history")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"items": sanitizeHistoryRecords(records),
	})
}

func (h *Handler) GetHistory(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	record, err := h.history.Get(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, sanitizeHistoryRecord(record))
}

func (h *Handler) RetryHistory(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	resp, err := h.generation.Retry(r.Context(), principal, resolveMainServiceBaseURL(r, h.cfg), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, resp)
}

func (h *Handler) CancelHistory(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	record, err := h.generation.Cancel(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, sanitizeHistoryRecord(record))
}

func resolveMainServiceBaseURL(r *http.Request, cfg config.Config) string {
	return httpx.ResolveRequestBaseURL(r)
}
