package backend

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
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
	clients  RelayClientProvider
}

func RegisterRoutes(mux *http.ServeMux, auth *hostprincipal.Middleware, handler *RelayHandler) {
	if mux == nil || handler == nil {
		return
	}
	mux.HandleFunc("POST /plugins/ai-relay/agnes/{slug}", handler.Relay)
	mux.HandleFunc("POST /plugins/ai-relay/agnes/{slug}/v1/images/generations", handler.Relay)
	mux.HandleFunc("POST /plugins/ai-relay/agnes/{slug}/v1/images/edits", handler.Edit)
	mux.HandleFunc("GET /plugins/ai-relay/agnes/{slug}/v1/models", handler.ProxyModels)
	mux.HandleFunc("POST /plugins/ai-relay/agnes/{slug}/v1/chat/completions", handler.ProxyChatCompletions)
	mux.HandleFunc("POST /plugins/ai-relay/agnes/{slug}/v1/responses", handler.ProxyResponses)
	mux.HandleFunc("POST /plugins/ai-relay/agnes/{slug}/v1/responses/compact", handler.ProxyResponsesCompact)
	mux.HandleFunc("/plugins/ai-relay/openai/", handler.ProxyOpenAIPath)
	mux.HandleFunc("/plugins/ai-relay/opencode/", handler.ProxyOpenAIPath)
	if auth == nil {
		return
	}
	mux.HandleFunc("GET /plugins/ai-relay/api/routes", auth.RequirePlugin("ai-relay", handler.ListRoutes))
	mux.HandleFunc("GET /plugins/ai-relay/api/platforms", auth.RequirePlugin("ai-relay", handler.ListPlatforms))
	mux.HandleFunc("GET /plugins/ai-relay/api/runtime", auth.RequirePlugin("ai-relay", handler.Runtime))
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
	config, found, err := h.routes.Get(r.Context(), relayPlatform(r), r.PathValue("slug"))
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
	if adapter.Descriptor().Protocol == transparentProtocol {
		h.proxyOpenAIEndpointForProtocol(w, r, "images/edits", transparentProtocol)
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
	client, err := h.clientForRequest(r)
	if err != nil {
		writeProxyClientError(w, err)
		return
	}
	result, err := h.generatePayload(r.Context(), client, adapter, config, payload, request.N, authorization)
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

func (h *RelayHandler) Runtime(w http.ResponseWriter, _ *http.Request, principal model.CurrentPrincipal) {
	if !isAdmin(principal) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "administrator access is required"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"base_url": resolvePublicBaseURL()})
}

func NewRelayHandler(routes RouteRepository, adapters *AdapterRegistry, client *http.Client) *RelayHandler {
	if client == nil {
		client = http.DefaultClient
	}
	return NewRelayHandlerWithClientProvider(routes, adapters, NewProxyClientProvider(client, unavailableProxyResolver{}))
}

func NewRelayHandlerWithClientProvider(routes RouteRepository, adapters *AdapterRegistry, clients RelayClientProvider) *RelayHandler {
	if adapters == nil {
		adapters = NewDefaultAdapterRegistry()
	}
	if clients == nil {
		clients = NewProxyClientProvider(http.DefaultClient, unavailableProxyResolver{})
	}
	return &RelayHandler{routes: routes, adapters: adapters, clients: clients}
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
	if adapter.Descriptor().Protocol == transparentProtocol {
		h.proxyOpenAIEndpointForProtocol(w, r, "images/generations", transparentProtocol)
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

	client, err := h.clientForRequest(r)
	if err != nil {
		writeProxyClientError(w, err)
		return
	}
	result, err := h.generate(r.Context(), client, adapter, config, request, authorization)
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
	Platform     string            `json:"platform"`
	Slug         string            `json:"slug"`
	Name         string            `json:"name"`
	BaseURL      string            `json:"base_url"`
	PathMappings map[string]string `json:"path_mappings"`
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
	return RouteConfig{
		Platform: platform, Slug: slug, Name: input.Name, BaseURL: input.BaseURL,
		PathMappings: input.PathMappings,
	}
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
	if err := decoder.Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid route configuration"})
		return
	}
	if len(input.Items) == 0 {
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

func (h *RelayHandler) generate(ctx context.Context, client *http.Client, adapter ImageAdapter, config RouteConfig, request OpenAIImageRequest, authorization string) (OpenAIImageResponse, error) {
	outgoing, err := adapter.BuildRequest(config, request)
	if err != nil {
		return OpenAIImageResponse{}, err
	}
	return h.generatePayload(ctx, client, adapter, config, outgoing, request.N, authorization)
}

func (h *RelayHandler) generatePayload(ctx context.Context, client *http.Client, adapter ImageAdapter, config RouteConfig, outgoing AgnesImageRequest, count int, authorization string) (OpenAIImageResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, relayRequestTimeout)
	defer cancel()
	payload, err := json.Marshal(outgoing)
	if err != nil {
		return OpenAIImageResponse{}, err
	}
	result := OpenAIImageResponse{Data: make([]OpenAIImageData, 0, count)}
	for index := 0; index < count; index++ {
		upstreamURL, err := ResolveRouteEndpointURL(config, adapter.Descriptor().Operation)
		if err != nil {
			return OpenAIImageResponse{}, fmt.Errorf("resolve upstream URL: %w", err)
		}
		upstreamRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, strings.NewReader(string(payload)))
		if err != nil {
			return OpenAIImageResponse{}, fmt.Errorf("build upstream request: %w", err)
		}
		upstreamRequest.Header.Set("Authorization", authorization)
		upstreamRequest.Header.Set("Content-Type", "application/json")
		response, err := client.Do(upstreamRequest)
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

func relayPlatform(r *http.Request) string {
	if platform := strings.TrimSpace(r.PathValue("platform")); platform != "" {
		return platform
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) >= 3 && parts[0] == "plugins" && parts[1] == "ai-relay" {
		return parts[2]
	}
	return ""
}

func (h *RelayHandler) ProxyModels(w http.ResponseWriter, r *http.Request) {
	h.proxyOpenAIEndpoint(w, r, "models")
}

func (h *RelayHandler) ProxyChatCompletions(w http.ResponseWriter, r *http.Request) {
	h.proxyOpenAIEndpoint(w, r, "chat/completions")
}

func (h *RelayHandler) ProxyResponses(w http.ResponseWriter, r *http.Request) {
	h.proxyOpenAIEndpoint(w, r, "responses")
}

func (h *RelayHandler) ProxyResponsesCompact(w http.ResponseWriter, r *http.Request) {
	h.proxyOpenAIEndpoint(w, r, "responses/compact")
}

func (h *RelayHandler) proxyOpenAIEndpoint(w http.ResponseWriter, r *http.Request, endpoint string) {
	h.proxyOpenAIEndpointForProtocol(w, r, endpoint, "")
}

func (h *RelayHandler) ProxyOpenAIPath(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 6 || parts[0] != "plugins" || parts[1] != "ai-relay" || parts[4] != "v1" {
		writeOpenAIError(w, http.StatusNotFound, "not_found_error", "relay endpoint not found")
		return
	}
	endpoint := strings.Join(parts[5:], "/")
	r.SetPathValue("platform", parts[2])
	r.SetPathValue("slug", parts[3])
	h.proxyOpenAIEndpointForProtocol(w, r, endpoint, "")
}

func (h *RelayHandler) proxyOpenAIEndpointForProtocol(w http.ResponseWriter, r *http.Request, endpoint, requiredProtocol string) {
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(authorization), "bearer ") || strings.TrimSpace(authorization[7:]) == "" {
		writeOpenAIError(w, http.StatusUnauthorized, "authentication_error", "Bearer authorization is required")
		return
	}
	config, found, err := h.routes.Get(r.Context(), relayPlatform(r), r.PathValue("slug"))
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
	if requiredProtocol != "" && adapter.Descriptor().Protocol != requiredProtocol {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "relay platform does not support transparent forwarding")
		return
	}
	if transparentAdapter, ok := adapter.(TransparentAdapter); ok {
		config.BaseURL = transparentAdapter.NormalizeBaseURL(config.BaseURL)
	}
	upstreamURL, err := ResolveRouteEndpointURL(config, endpoint)
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "upstream_error", "failed to resolve upstream URL")
		return
	}
	parsedUpstreamURL, err := url.Parse(upstreamURL)
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "upstream_error", "failed to resolve upstream URL")
		return
	}
	parsedUpstreamURL.RawQuery = r.URL.RawQuery
	var requestBody io.Reader = r.Body
	if transparentAdapter, ok := adapter.(TransparentAdapter); ok && r.Body != nil && adapter.Descriptor().Protocol == "opencode" && canonicalRelayPath(endpoint) == "chat/completions" {
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "failed to read request body")
			return
		}
		requestBody = strings.NewReader(string(transparentAdapter.TransformRequestBody(endpoint, body)))
	}
	upstreamRequest, err := http.NewRequestWithContext(r.Context(), r.Method, parsedUpstreamURL.String(), requestBody)
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "upstream_error", "failed to build upstream request")
		return
	}
	copyEndToEndHeaders(upstreamRequest.Header, r.Header)
	upstreamRequest.Header.Set("Authorization", authorization)
	client, err := h.clientForRequest(r)
	if err != nil {
		writeProxyClientError(w, err)
		return
	}
	response, err := client.Do(upstreamRequest)
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "upstream_error", err.Error())
		return
	}
	defer response.Body.Close()
	copyEndToEndHeaders(w.Header(), response.Header)
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}

var hopByHopHeaders = map[string]struct{}{
	"Connection": {}, "Proxy-Connection": {}, "Keep-Alive": {}, "Transfer-Encoding": {},
	"TE": {}, "Trailer": {}, "Upgrade": {},
}

func copyEndToEndHeaders(destination, source http.Header) {
	for key, values := range source {
		canonicalKey := http.CanonicalHeaderKey(key)
		if _, excluded := hopByHopHeaders[canonicalKey]; excluded || strings.EqualFold(canonicalKey, ProxyIDHeader) {
			continue
		}
		destination.Del(canonicalKey)
		for _, value := range values {
			destination.Add(canonicalKey, value)
		}
	}
}

func (h *RelayHandler) clientForRequest(r *http.Request) (*http.Client, error) {
	if h == nil || h.clients == nil {
		return nil, ErrProxyStorageUnavailable
	}
	proxyID := ""
	ctx := context.Background()
	if r != nil {
		proxyID = r.Header.Get(ProxyIDHeader)
		ctx = r.Context()
	}
	return h.clients.ClientFor(ctx, proxyID)
}

func writeProxyClientError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrInvalidProxyID) {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "invalid account proxy context")
		return
	}
	writeOpenAIError(w, http.StatusBadGateway, "upstream_error", "account proxy is unavailable")
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
