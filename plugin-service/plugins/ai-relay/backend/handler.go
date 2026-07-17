package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	hostprincipal "github.com/Wei-Shaw/sub2api/plugin-service/internal/host/principal"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
)

const relayRequestTimeout = 180 * time.Second

type RelayHandler struct {
	routes   RouteRepository
	adapters *AdapterRegistry
	client   *http.Client
}

func RegisterRoutes(mux *http.ServeMux, auth *hostprincipal.Middleware, handler *RelayHandler) {
	if mux == nil || handler == nil {
		return
	}
	mux.HandleFunc("POST /plugins/ai-relay/{platform}/{slug}", handler.Relay)
	if auth == nil {
		return
	}
	mux.HandleFunc("GET /plugins/ai-relay/api/routes", auth.RequirePlugin("ai-relay", handler.ListRoutes))
	mux.HandleFunc("PUT /plugins/ai-relay/api/routes/{platform}/{slug}", auth.RequirePlugin("ai-relay", handler.UpsertRoute))
	mux.HandleFunc("DELETE /plugins/ai-relay/api/routes/{platform}/{slug}", auth.RequirePlugin("ai-relay", handler.DeleteRoute))
}

func NewRelayHandler(routes RouteRepository, adapters *AdapterRegistry, client *http.Client) *RelayHandler {
	if client == nil {
		client = http.DefaultClient
	}
	if adapters == nil {
		adapters = NewDefaultAdapterRegistry()
	}
	return &RelayHandler{routes: routes, adapters: adapters, client: client}
}

func (h *RelayHandler) Relay(w http.ResponseWriter, r *http.Request) {
	if r == nil || r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "invalid_request_error", "only POST is supported")
		return
	}
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(authorization), "bearer ") || strings.TrimSpace(authorization[7:]) == "" {
		writeOpenAIError(w, http.StatusUnauthorized, "authentication_error", "Bearer authorization is required")
		return
	}
	platform, slug, ok := relayRouteParts(r.URL.Path)
	if !ok {
		writeOpenAIError(w, http.StatusNotFound, "not_found_error", "relay route not found")
		return
	}
	if h.routes == nil {
		writeOpenAIError(w, http.StatusServiceUnavailable, "server_error", "relay routes are unavailable")
		return
	}
	config, found, err := h.routes.Get(r.Context(), platform, slug)
	if err != nil {
		writeOpenAIError(w, http.StatusInternalServerError, "server_error", "failed to load relay route")
		return
	}
	if !found || !config.Enabled {
		writeOpenAIError(w, http.StatusNotFound, "not_found_error", "relay route not found")
		return
	}
	adapter, found := h.adapters.Get(platform)
	if !found {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "unsupported relay platform")
		return
	}

	request, err := decodeOpenAIImageRequest(r.Body)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	if request.N == 0 {
		request.N = 1
	}
	if request.N < 1 || request.N > config.MaxN {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "n exceeds the configured route limit")
		return
	}

	result, err := h.generate(r.Context(), adapter, config, request, authorization)
	if err != nil {
		status := http.StatusBadGateway
		if err == context.DeadlineExceeded {
			status = http.StatusGatewayTimeout
		}
		writeOpenAIError(w, status, "upstream_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type routeConfigInput struct {
	BaseURL      string            `json:"base_url"`
	DefaultModel string            `json:"default_model"`
	ModelMap     map[string]string `json:"model_map"`
	QualityMap   map[string]string `json:"quality_map"`
	MaxN         int               `json:"max_n"`
	Enabled      bool              `json:"enabled"`
}

func (h *RelayHandler) ListRoutes(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	if !isAdmin(principal) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "administrator access is required"})
		return
	}
	routes, err := h.routes.List(r.Context(), r.URL.Query().Get("platform"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list relay routes"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": routes})
}

func (h *RelayHandler) UpsertRoute(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	if !isAdmin(principal) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "administrator access is required"})
		return
	}
	var input routeConfigInput
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid route configuration"})
		return
	}
	config, err := h.routes.Upsert(r.Context(), RouteConfig{
		Platform:     r.PathValue("platform"),
		Slug:         r.PathValue("slug"),
		BaseURL:      input.BaseURL,
		DefaultModel: input.DefaultModel,
		ModelMap:     input.ModelMap,
		QualityMap:   input.QualityMap,
		MaxN:         input.MaxN,
		Enabled:      input.Enabled,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, config)
}

func (h *RelayHandler) DeleteRoute(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	if !isAdmin(principal) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "administrator access is required"})
		return
	}
	if err := h.routes.Delete(r.Context(), r.PathValue("platform"), r.PathValue("slug")); err != nil {
		status := http.StatusInternalServerError
		if err == ErrRouteNotFound {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func isAdmin(principal model.CurrentPrincipal) bool {
	return strings.EqualFold(strings.TrimSpace(principal.Role), model.RoleAdmin)
}

func (h *RelayHandler) generate(ctx context.Context, adapter ImageAdapter, config RouteConfig, request OpenAIImageRequest, authorization string) (OpenAIImageResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, relayRequestTimeout)
	defer cancel()
	outgoing, err := adapter.BuildRequest(config, request)
	if err != nil {
		return OpenAIImageResponse{}, err
	}
	payload, err := json.Marshal(outgoing)
	if err != nil {
		return OpenAIImageResponse{}, err
	}
	result := OpenAIImageResponse{Data: make([]OpenAIImageData, 0, request.N)}
	for index := 0; index < request.N; index++ {
		upstreamRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, adapter.Endpoint(config), strings.NewReader(string(payload)))
		if err != nil {
			return OpenAIImageResponse{}, fmt.Errorf("build upstream request: %w", err)
		}
		upstreamRequest.Header.Set("Authorization", authorization)
		upstreamRequest.Header.Set("Content-Type", "application/json")
		response, err := h.client.Do(upstreamRequest)
		if err != nil {
			return OpenAIImageResponse{}, err
		}
		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if readErr != nil {
			return OpenAIImageResponse{}, fmt.Errorf("read upstream response: %w", readErr)
		}
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			return OpenAIImageResponse{}, fmt.Errorf("upstream returned status %d", response.StatusCode)
		}
		parsed, err := adapter.ParseResponse(body)
		if err != nil {
			return OpenAIImageResponse{}, err
		}
		if result.Created == 0 {
			result.Created = parsed.Created
		}
		result.Data = append(result.Data, parsed.Data...)
	}
	return result, nil
}

func decodeOpenAIImageRequest(body io.Reader) (OpenAIImageRequest, error) {
	decoder := json.NewDecoder(io.LimitReader(body, 1<<20))
	decoder.DisallowUnknownFields()
	var request OpenAIImageRequest
	if err := decoder.Decode(&request); err != nil {
		return OpenAIImageRequest{}, fmt.Errorf("invalid image request: %w", err)
	}
	if decoder.More() {
		return OpenAIImageRequest{}, fmt.Errorf("invalid image request")
	}
	return request, nil
}

func relayRouteParts(path string) (string, string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 4 || parts[0] != "plugins" || parts[1] != "ai-relay" {
		return "", "", false
	}
	return parts[2], parts[3], true
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeOpenAIError(w http.ResponseWriter, status int, kind, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{
		"message": message,
		"type":    kind,
		"code":    status,
	}})
}
