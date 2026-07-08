package backend

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/Wei-Shaw/sub2api/plugin-service/internal/host/httpx"
	hostprincipal "github.com/Wei-Shaw/sub2api/plugin-service/internal/host/principal"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/service"
	imagemanifest "github.com/Wei-Shaw/sub2api/plugin-service/plugins/image-generation/manifest"
)

const apiBasePath = "/plugins/" + imagemanifest.Key + "/api"

type HandlerDeps struct {
	PluginKey  string
	History    *service.HistoryService
	Generation *GenerationService
}

type Handler struct {
	pluginKey  string
	history    *service.HistoryService
	generation *GenerationService
}

func NewHandler(deps HandlerDeps) *Handler {
	pluginKey := deps.PluginKey
	if pluginKey == "" {
		pluginKey = imagemanifest.Key
	}

	return &Handler{
		pluginKey:  pluginKey,
		history:    deps.History,
		generation: deps.Generation,
	}
}

func RegisterRoutes(mux *http.ServeMux, authMiddleware *hostprincipal.Middleware, handler *Handler) {
	mux.HandleFunc("GET "+apiBasePath+"/me", authMiddleware.RequirePlugin(imagemanifest.Key, handler.Me))
	mux.HandleFunc("GET "+apiBasePath+"/config", authMiddleware.RequirePlugin(imagemanifest.Key, handler.Config))
	mux.HandleFunc("POST "+apiBasePath+"/generate", authMiddleware.RequirePlugin(imagemanifest.Key, handler.Generate))
	mux.HandleFunc("GET "+apiBasePath+"/creations", authMiddleware.RequirePlugin(imagemanifest.Key, handler.ListCreations))
	mux.HandleFunc("GET "+apiBasePath+"/history", authMiddleware.RequirePlugin(imagemanifest.Key, handler.ListHistory))
	mux.HandleFunc("GET "+apiBasePath+"/history/{id}", authMiddleware.RequirePlugin(imagemanifest.Key, handler.GetHistory))
	mux.HandleFunc("POST "+apiBasePath+"/history/{id}/retry", authMiddleware.RequirePlugin(imagemanifest.Key, handler.RetryHistory))
	mux.HandleFunc("POST "+apiBasePath+"/history/{id}/cancel", authMiddleware.RequirePlugin(imagemanifest.Key, handler.CancelHistory))
}

func (h *Handler) Config(w http.ResponseWriter, _ *http.Request, principal model.CurrentPrincipal) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"plugin_key":      h.pluginKey,
		"history_enabled": true,
		"user_id":         principal.UserID,
		"role":            principal.Role,
	})
}

func (h *Handler) Me(w http.ResponseWriter, _ *http.Request, principal model.CurrentPrincipal) {
	httpx.WriteJSON(w, http.StatusOK, principal)
}

func (h *Handler) Generate(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	var req GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	log.Printf(
		"[plugin-service] generate handler request user_id=%d plugin=%s model=%s size=%s prompt_len=%d reference_images=%d",
		principal.UserID,
		principal.Plugin,
		req.Model,
		req.Size,
		len(req.Prompt),
		len(req.ReferenceImages),
	)

	resp, err := h.generation.Generate(r.Context(), principal, resolveMainServiceBaseURL(r), req)
	if err != nil {
		if errors.Is(err, ErrPromptRequired) || errors.Is(err, ErrProviderKeyRequired) {
			httpx.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		var upstreamErr *UpstreamHTTPError
		if errors.As(err, &upstreamErr) {
			log.Printf("[plugin-service] generate handler upstream error user_id=%d status=%d err=%s", principal.UserID, upstreamErr.StatusCode, upstreamErr.Message)
			httpx.WriteError(w, upstreamErr.StatusCode, upstreamErr.Message)
			return
		}
		log.Printf("[plugin-service] generate handler internal error user_id=%d err=%v", principal.UserID, err)
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
	resp, err := h.generation.Retry(r.Context(), principal, resolveMainServiceBaseURL(r), r.PathValue("id"))
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

func resolveMainServiceBaseURL(r *http.Request) string {
	return httpx.ResolveRequestBaseURL(r)
}
