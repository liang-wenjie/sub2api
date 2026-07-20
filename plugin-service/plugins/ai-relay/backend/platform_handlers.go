package backend

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func (adapter *OpenAIAdapter) Handle(ctx context.Context, request PlatformRequest) (PlatformResponse, error) {
	return forwardPlatformRequest(ctx, request, request.Route, request.Endpoint, request.Method, request.Body)
}

func (adapter *OpenCodeAdapter) Handle(ctx context.Context, request PlatformRequest) (PlatformResponse, error) {
	endpoint := canonicalRelayPath(request.Endpoint)
	if endpoint == "responses" {
		body, bridgeContext, err := responsesRequestToChatCompletionsWithContext(request.Body)
		if err != nil {
			return PlatformResponse{StatusCode: http.StatusBadRequest}, err
		}
		logOpenCodeResponsesBridge("request", body, 0, nil)
		config := request.Route
		config.BaseURL = adapter.NormalizeBaseURL(config.BaseURL)
		response, err := forwardPlatformRequest(ctx, request, config, "chat/completions", http.MethodPost, body)
		logOpenCodeResponsesBridge("response", body, response.StatusCode, response.Body)
		if err != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
			return response, err
		}
		if strings.Contains(strings.ToLower(response.Headers.Get("Content-Type")), "text/event-stream") {
			response.Headers.Set("Content-Type", "text/event-stream")
			response.Body = chatCompletionSSEToResponsesWithContext(response.Body, bridgeContext)
			return response, nil
		}
		response.Body, err = chatCompletionToResponsesWithContext(response.Body, bridgeContext)
		if err != nil {
			return PlatformResponse{StatusCode: http.StatusBadGateway}, err
		}
		response.Headers.Set("Content-Type", "application/json")
		return response, nil
	}
	body := request.Body
	if endpoint == "chat/completions" {
		body = adapter.TransformRequestBody(endpoint, body)
	}
	config := request.Route
	config.BaseURL = adapter.NormalizeBaseURL(config.BaseURL)
	return forwardPlatformRequest(ctx, request, config, endpoint, request.Method, body)
}

func forwardPlatformRequest(ctx context.Context, request PlatformRequest, config RouteConfig, endpoint, method string, body []byte) (PlatformResponse, error) {
	upstreamURL, err := ResolveRouteEndpointURL(config, endpoint)
	if err != nil {
		return PlatformResponse{StatusCode: http.StatusBadGateway}, err
	}
	parsedURL, err := url.Parse(upstreamURL)
	if err != nil {
		return PlatformResponse{StatusCode: http.StatusBadGateway}, err
	}
	parsedURL.RawQuery = request.Query
	upstreamRequest, err := http.NewRequestWithContext(ctx, method, parsedURL.String(), bytes.NewReader(body))
	if err != nil {
		return PlatformResponse{StatusCode: http.StatusBadGateway}, err
	}
	copyEndToEndHeaders(upstreamRequest.Header, request.Headers)
	response, err := request.Client.Do(upstreamRequest)
	if err != nil {
		return PlatformResponse{StatusCode: http.StatusBadGateway}, err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return PlatformResponse{StatusCode: http.StatusBadGateway}, err
	}
	return PlatformResponse{StatusCode: response.StatusCode, Headers: response.Header.Clone(), Body: responseBody}, nil
}
