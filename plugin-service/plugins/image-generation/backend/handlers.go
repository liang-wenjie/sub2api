package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"

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
	mux.HandleFunc("GET "+apiBasePath+"/conversations", authMiddleware.RequirePlugin(imagemanifest.Key, handler.ListConversations))
	mux.HandleFunc("GET "+apiBasePath+"/conversations/{id}/messages", authMiddleware.RequirePlugin(imagemanifest.Key, handler.ListConversationMessages))
	mux.HandleFunc("DELETE "+apiBasePath+"/conversations/{id}", authMiddleware.RequirePlugin(imagemanifest.Key, handler.DeleteConversation))
	mux.HandleFunc("GET "+apiBasePath+"/history/{id}", authMiddleware.RequirePlugin(imagemanifest.Key, handler.GetHistory))
	mux.HandleFunc("DELETE "+apiBasePath+"/history/{id}", authMiddleware.RequirePlugin(imagemanifest.Key, handler.DeleteHistory))
	mux.HandleFunc("POST "+apiBasePath+"/history/{id}/retry", authMiddleware.RequirePlugin(imagemanifest.Key, handler.RetryHistory))
	mux.HandleFunc("GET "+apiBasePath+"/history/{id}/status", authMiddleware.RequirePlugin(imagemanifest.Key, handler.StatusHistory))
	mux.HandleFunc("POST "+apiBasePath+"/history/{id}/cancel", authMiddleware.RequirePlugin(imagemanifest.Key, handler.CancelHistory))
	mux.HandleFunc("GET "+apiBasePath+"/assets/{history_id}/{index}", authMiddleware.RequirePlugin(imagemanifest.Key, handler.GetAsset))
	mux.HandleFunc("GET "+apiBasePath+"/assets/{history_id}/{kind}/{index}", authMiddleware.RequirePlugin(imagemanifest.Key, handler.GetAsset))
	mux.HandleFunc("GET "+apiBasePath+"/assets/{history_id}/{kind}/{index}/{variant}", authMiddleware.RequirePlugin(imagemanifest.Key, handler.GetAsset))
}

func (h *Handler) GetAsset(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	record, err := h.history.Get(r.Context(), principal, r.PathValue("history_id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil || index < 0 {
		httpx.WriteError(w, http.StatusBadRequest, "invalid image index")
		return
	}
	kind := r.PathValue("kind")
	preview := r.PathValue("variant") == "preview"
	var key string
	switch kind {
	case "", "result":
		images := imageMapsValue(record.Result["images"])
		if index < len(images) {
			field := "object_key"
			if preview {
				field = "preview_object_key"
			}
			key = stringValue(images[index][field])
		}
	case "reference":
		references := referenceImagesValue(record.Request["reference_images"])
		if index < len(references) {
			key = references[index].StorageKey
			if preview {
				key = references[index].PreviewStorageKey
			}
		}
	default:
		httpx.WriteError(w, http.StatusBadRequest, "invalid image asset kind")
		return
	}
	if key == "" {
		httpx.WriteError(w, http.StatusNotFound, "image asset not found")
		return
	}
	object, err := h.generation.GetMedia(r.Context(), key)
	if err != nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "image storage unavailable")
		return
	}
	defer object.Body.Close()
	w.Header().Set("Content-Type", object.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(object.Size, 10))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.URL.Query().Get("download") == "1" && !preview {
		extension := ".img"
		if values, _ := mime.ExtensionsByType(object.ContentType); len(values) > 0 {
			extension = values[0]
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="generated-image-%d%s"`, index+1, extension))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, object.Body)
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
		if errors.Is(err, ErrPromptRequired) || errors.Is(err, ErrProviderKeyRequired) || errors.Is(err, ErrImageModelUnsupported) {
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

func (h *Handler) ListConversations(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	query, err := parseCursorQuery(r.URL.Query(), "cursor")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	items, err := h.history.ListConversations(r.Context(), principal, query)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to list conversations")
		return
	}
	next := ""
	if len(items) == query.Limit {
		last := items[len(items)-1]
		next = encodeCursor(last.UpdatedAt, last.ID)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
}

func (h *Handler) ListConversationMessages(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	query, err := parseCursorQuery(r.URL.Query(), "before")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	items, err := h.history.ListConversationMessages(r.Context(), principal, r.PathValue("id"), query)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to list conversation messages")
		return
	}
	next := ""
	if len(items) == query.Limit {
		last := items[len(items)-1]
		next = encodeCursor(last.CreatedAt, last.ID)
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": sanitizeHistoryRecords(items), "next_cursor": next})
}

func (h *Handler) DeleteConversation(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	query := model.CursorQuery{Limit: 100}
	for {
		items, err := h.history.ListConversationMessages(r.Context(), principal, r.PathValue("id"), query)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		if len(items) == 0 {
			break
		}
		for index := range items {
			if err := h.history.Delete(r.Context(), principal, items[index].ID); err != nil {
				writeServiceError(w, err)
				return
			}
			h.generation.DeleteMedia(r.Context(), &items[index])
		}
		if len(items) < query.Limit {
			break
		}
		last := items[len(items)-1]
		query.BeforeTime, query.BeforeID = last.CreatedAt, last.ID
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetHistory(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	record, err := h.history.Get(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, sanitizeHistoryRecord(record))
}

func (h *Handler) DeleteHistory(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	record, err := h.history.Get(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if err := h.history.Delete(r.Context(), principal, record.ID); err != nil {
		writeServiceError(w, err)
		return
	}
	h.generation.DeleteMedia(r.Context(), record)
	w.WriteHeader(http.StatusNoContent)
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
	record, err := h.generation.Cancel(r.Context(), principal, resolveMainServiceBaseURL(r), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, sanitizeHistoryRecord(record))
}

func (h *Handler) StatusHistory(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	record, err := h.generation.Status(r.Context(), principal, resolveMainServiceBaseURL(r), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, sanitizeHistoryRecord(record))
}

func resolveMainServiceBaseURL(r *http.Request) string {
	if baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("PLUGIN_MAIN_SERVICE_BASE_URL")), "/"); baseURL != "" {
		return baseURL
	}
	return httpx.ResolveRequestBaseURL(r)
}
