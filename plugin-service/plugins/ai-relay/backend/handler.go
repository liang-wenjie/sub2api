package backend

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	hostprincipal "github.com/Wei-Shaw/sub2api/plugin-service/internal/host/principal"
	"github.com/Wei-Shaw/sub2api/plugin-service/internal/model"
)

const (
	relayRequestTimeout  = 180 * time.Second
	defaultRoutePageSize = 20
	maxRoutePageSize     = 100
)

type routeListPagination struct {
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

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
	mux.HandleFunc("POST /plugins/ai-relay/{platform}/{slug}/v1/images/generations", handler.Relay)
	mux.HandleFunc("POST /plugins/ai-relay/{platform}/{slug}/v1/images/edits", handler.Edit)
	mux.HandleFunc("GET /plugins/ai-relay/{platform}/{slug}/v1/models", handler.ProxyModels)
	mux.HandleFunc("POST /plugins/ai-relay/{platform}/{slug}/v1/chat/completions", handler.ProxyChatCompletions)
	if auth == nil {
		return
	}
	mux.HandleFunc("GET /plugins/ai-relay/api/routes", auth.RequirePlugin("ai-relay", handler.ListRoutes))
	mux.HandleFunc("GET /plugins/ai-relay/api/platforms", auth.RequirePlugin("ai-relay", handler.ListPlatforms))
	mux.HandleFunc("POST /plugins/ai-relay/api/routes", auth.RequirePlugin("ai-relay", handler.CreateRoute))
	mux.HandleFunc("PUT /plugins/ai-relay/api/routes/{platform}/{slug}", auth.RequirePlugin("ai-relay", handler.UpsertRoute))
	mux.HandleFunc("DELETE /plugins/ai-relay/api/routes/{platform}/{slug}", auth.RequirePlugin("ai-relay", handler.DeleteRoute))
	mux.HandleFunc("DELETE /plugins/ai-relay/api/routes", auth.RequirePlugin("ai-relay", handler.DeleteRoutes))
}

type OpenAIImageEditRequest struct {
	Model          string
	Prompt         string
	N              int
	ResponseFormat string
	Size           string
	Images         []string
}

func (h *RelayHandler) Edit(w http.ResponseWriter, r *http.Request) {
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(authorization), "bearer ") || strings.TrimSpace(authorization[7:]) == "" {
		writeOpenAIError(w, http.StatusUnauthorized, "authentication_error", "Bearer authorization is required")
		return
	}
	config, found, err := h.routes.Get(r.Context(), r.PathValue("platform"), r.PathValue("slug"))
	if err != nil {
		writeOpenAIError(w, http.StatusInternalServerError, "server_error", "failed to load relay route")
		return
	}
	if !found {
		writeOpenAIError(w, http.StatusNotFound, "not_found_error", "relay route not found")
		return
	}
	adapter, found := h.adapters.Get(config.Platform)
	if !found {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "unsupported relay platform")
		return
	}
	request, err := decodeOpenAIImageEditRequest(r)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	payload, err := adapter.BuildEditRequest(config, request)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	result, err := h.generatePayload(r.Context(), adapter, config, payload, request.N, authorization)
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "upstream_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *RelayHandler) ListPlatforms(w http.ResponseWriter, _ *http.Request, principal model.CurrentPrincipal) {
	if !isAdmin(principal) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "administrator access is required"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": h.adapters.Platforms()})
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
	if !found {
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
	if request.N < 1 {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "n must be at least 1")
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
	Platform string `json:"platform"`
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	BaseURL  string `json:"base_url"`
}

func (h *RelayHandler) CreateRoute(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	if !isAdmin(principal) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "administrator access is required"})
		return
	}
	input, err := decodeRouteConfigInput(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid route configuration"})
		return
	}
	if _, ok := h.adapters.Get(input.Platform); !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported relay platform"})
		return
	}
	config, err := h.routes.Upsert(r.Context(), routeConfigFromInput(input.Platform, input.Slug, input))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, config)
}

func (h *RelayHandler) ListRoutes(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	if !isAdmin(principal) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "administrator access is required"})
		return
	}
	query := RouteQuery{Platform: r.URL.Query().Get("platform"), Search: r.URL.Query().Get("search")}
	routes, err := h.routes.List(r.Context(), query)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list relay routes"})
		return
	}
	page, pageSize := routeListPage(r)
	total := len(routes)
	totalPages := (total + pageSize - 1) / pageSize
	if totalPages == 0 {
		totalPages = 1
	}
	start := (page - 1) * pageSize
	if start >= total {
		routes = []RouteConfig{}
	} else {
		end := start + pageSize
		if end > total {
			end = total
		}
		routes = routes[start:end]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": routes,
		"pagination": routeListPagination{
			Page:       page,
			PageSize:   pageSize,
			Total:      total,
			TotalPages: totalPages,
		},
	})
}

func routeListPage(r *http.Request) (int, int) {
	page := parsePositiveQueryInt(r, "page", 1, 1_000_000)
	pageSize := parsePositiveQueryInt(r, "page_size", defaultRoutePageSize, maxRoutePageSize)
	return page, pageSize
}

func parsePositiveQueryInt(r *http.Request, key string, fallback, maximum int) int {
	value, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get(key)))
	if err != nil || value < 1 {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}

func (h *RelayHandler) UpsertRoute(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	if !isAdmin(principal) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "administrator access is required"})
		return
	}
	input, err := decodeRouteConfigInput(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid route configuration"})
		return
	}
	if _, ok := h.adapters.Get(r.PathValue("platform")); !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported relay platform"})
		return
	}
	config, err := h.routes.Upsert(r.Context(), routeConfigFromInput(r.PathValue("platform"), r.PathValue("slug"), input))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, config)
}

func decodeRouteConfigInput(body io.Reader) (routeConfigInput, error) {
	var input routeConfigInput
	decoder := json.NewDecoder(io.LimitReader(body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return routeConfigInput{}, err
	}
	return input, nil
}

func routeConfigFromInput(platform, slug string, input routeConfigInput) RouteConfig {
	return RouteConfig{Platform: platform, Slug: slug, Name: input.Name, BaseURL: input.BaseURL}
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

func (h *RelayHandler) DeleteRoutes(w http.ResponseWriter, r *http.Request, principal model.CurrentPrincipal) {
	if !isAdmin(principal) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "administrator access is required"})
		return
	}
	var input struct {
		Items []RouteReference `json:"items"`
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || len(input.Items) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "at least one relay route is required"})
		return
	}
	if err := h.routes.DeleteMany(r.Context(), input.Items); err != nil {
		status := http.StatusInternalServerError
		if err == ErrInvalidRouteConfig || err == ErrRouteNotFound {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"deleted": len(input.Items)})
}

func isAdmin(principal model.CurrentPrincipal) bool {
	return strings.EqualFold(strings.TrimSpace(principal.Role), model.RoleAdmin)
}

func (h *RelayHandler) generate(ctx context.Context, adapter ImageAdapter, config RouteConfig, request OpenAIImageRequest, authorization string) (OpenAIImageResponse, error) {
	outgoing, err := adapter.BuildRequest(config, request)
	if err != nil {
		return OpenAIImageResponse{}, err
	}
	return h.generatePayload(ctx, adapter, config, outgoing, request.N, authorization)
}

func (h *RelayHandler) generatePayload(ctx context.Context, adapter ImageAdapter, config RouteConfig, outgoing AgnesImageRequest, count int, authorization string) (OpenAIImageResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, relayRequestTimeout)
	defer cancel()
	payload, err := json.Marshal(outgoing)
	if err != nil {
		return OpenAIImageResponse{}, err
	}
	result := OpenAIImageResponse{Data: make([]OpenAIImageData, 0, count)}
	for index := 0; index < count; index++ {
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

func decodeOpenAIImageEditRequest(r *http.Request) (OpenAIImageEditRequest, error) {
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		return OpenAIImageEditRequest{}, fmt.Errorf("invalid multipart image edit request: %w", err)
	}
	count := 1
	if raw := strings.TrimSpace(r.FormValue("n")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			return OpenAIImageEditRequest{}, fmt.Errorf("n must be at least 1")
		}
		count = parsed
	}
	files := r.MultipartForm.File["image"]
	if len(files) == 0 {
		return OpenAIImageEditRequest{}, fmt.Errorf("image is required")
	}
	images := make([]string, 0, len(files))
	for _, file := range files {
		content, err := file.Open()
		if err != nil {
			return OpenAIImageEditRequest{}, fmt.Errorf("read image: %w", err)
		}
		data, readErr := io.ReadAll(io.LimitReader(content, 10<<20))
		_ = content.Close()
		if readErr != nil {
			return OpenAIImageEditRequest{}, fmt.Errorf("read image: %w", readErr)
		}
		if len(data) == 0 {
			return OpenAIImageEditRequest{}, fmt.Errorf("image is empty")
		}
		mediaType := file.Header.Get("Content-Type")
		if mediaType == "" || mediaType == "application/octet-stream" {
			mediaType = mime.TypeByExtension(filepath.Ext(file.Filename))
		}
		if mediaType == "" {
			mediaType = "application/octet-stream"
		}
		images = append(images, "data:"+mediaType+";base64,"+base64.StdEncoding.EncodeToString(data))
	}
	return OpenAIImageEditRequest{
		Model:          r.FormValue("model"),
		Prompt:         r.FormValue("prompt"),
		N:              count,
		ResponseFormat: r.FormValue("response_format"),
		Size:           r.FormValue("size"),
		Images:         images,
	}, nil
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
	if len(parts) != 4 && len(parts) != 7 || len(parts) >= 2 && (parts[0] != "plugins" || parts[1] != "ai-relay") {
		return "", "", false
	}
	if len(parts) == 7 && (parts[4] != "v1" || parts[5] != "images" || parts[6] != "generations") {
		return "", "", false
	}
	return parts[2], parts[3], true
}

func (h *RelayHandler) ProxyModels(w http.ResponseWriter, r *http.Request) {
	h.proxyOpenAIEndpoint(w, r, func(adapter ImageAdapter, config RouteConfig) string {
		return adapter.ModelsEndpoint(config)
	})
}

func (h *RelayHandler) ProxyChatCompletions(w http.ResponseWriter, r *http.Request) {
	h.proxyOpenAIEndpoint(w, r, func(adapter ImageAdapter, config RouteConfig) string {
		return adapter.ChatCompletionsEndpoint(config)
	})
}

func (h *RelayHandler) proxyOpenAIEndpoint(w http.ResponseWriter, r *http.Request, resolveEndpoint func(ImageAdapter, RouteConfig) string) {
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(authorization), "bearer ") || strings.TrimSpace(authorization[7:]) == "" {
		writeOpenAIError(w, http.StatusUnauthorized, "authentication_error", "Bearer authorization is required")
		return
	}
	config, found, err := h.routes.Get(r.Context(), r.PathValue("platform"), r.PathValue("slug"))
	if err != nil {
		writeOpenAIError(w, http.StatusInternalServerError, "server_error", "failed to load relay route")
		return
	}
	if !found {
		writeOpenAIError(w, http.StatusNotFound, "not_found_error", "relay route not found")
		return
	}
	adapter, found := h.adapters.Get(config.Platform)
	if !found {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "unsupported relay platform")
		return
	}
	upstreamRequest, err := http.NewRequestWithContext(r.Context(), r.Method, resolveEndpoint(adapter, config), r.Body)
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "upstream_error", "failed to build upstream request")
		return
	}
	upstreamRequest.Header.Set("Authorization", authorization)
	upstreamRequest.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	response, err := h.client.Do(upstreamRequest)
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "upstream_error", err.Error())
		return
	}
	defer response.Body.Close()
	w.Header().Set("Content-Type", response.Header.Get("Content-Type"))
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
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
