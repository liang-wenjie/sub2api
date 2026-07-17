package backend

import "strings"

type OpenAIImageRequest struct {
	Model             string `json:"model"`
	Prompt            string `json:"prompt"`
	Background        string `json:"background,omitempty"`
	Moderation        string `json:"moderation,omitempty"`
	N                 int    `json:"n,omitempty"`
	OutputCompression *int   `json:"output_compression,omitempty"`
	OutputFormat      string `json:"output_format,omitempty"`
	Quality           string `json:"quality,omitempty"`
	ResponseFormat    string `json:"response_format,omitempty"`
	Size              string `json:"size,omitempty"`
	Style             string `json:"style,omitempty"`
	User              string `json:"user,omitempty"`
}

type OpenAIImageResponse struct {
	Created int64             `json:"created"`
	Data    []OpenAIImageData `json:"data"`
}

type OpenAIImageData struct {
	URL           string `json:"url,omitempty"`
	B64JSON       string `json:"b64_json,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

type ImageAdapter interface {
	Platform() string
	Endpoint(RouteConfig) string
	BuildRequest(RouteConfig, OpenAIImageRequest) (AgnesImageRequest, error)
	ParseResponse([]byte) (OpenAIImageResponse, error)
}

type AdapterRegistry struct {
	adapters map[string]ImageAdapter
}

func NewDefaultAdapterRegistry() *AdapterRegistry {
	registry := &AdapterRegistry{adapters: make(map[string]ImageAdapter)}
	registry.Register(NewAgnesAdapter())
	return registry
}

func (r *AdapterRegistry) Register(adapter ImageAdapter) {
	if r == nil || adapter == nil {
		return
	}
	r.adapters[strings.ToLower(strings.TrimSpace(adapter.Platform()))] = adapter
}

func (r *AdapterRegistry) Get(platform string) (ImageAdapter, bool) {
	if r == nil {
		return nil, false
	}
	adapter, ok := r.adapters[strings.ToLower(strings.TrimSpace(platform))]
	return adapter, ok
}
