package backend

import (
	"sort"
	"strings"
)

type PlatformDescriptor struct {
	Key         string `json:"key"`
	DisplayName string `json:"display_name"`
	Operation   string `json:"operation"`
	Protocol    string `json:"protocol"`
}

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
	Descriptor() PlatformDescriptor
	Endpoint(RouteConfig) string
	ModelsEndpoint(RouteConfig) string
	ChatCompletionsEndpoint(RouteConfig) string
	BuildRequest(RouteConfig, OpenAIImageRequest) (AgnesImageRequest, error)
	BuildEditRequest(RouteConfig, OpenAIImageEditRequest) (AgnesImageRequest, error)
	ParseResponse([]byte) (OpenAIImageResponse, error)
}

func (r *AdapterRegistry) Platforms() []PlatformDescriptor {
	if r == nil {
		return []PlatformDescriptor{}
	}
	platforms := make([]PlatformDescriptor, 0, len(r.adapters))
	for _, adapter := range r.adapters {
		platforms = append(platforms, adapter.Descriptor())
	}
	sort.Slice(platforms, func(i, j int) bool { return platforms[i].Key < platforms[j].Key })
	return platforms
}

type AdapterRegistry struct {
	adapters map[string]ImageAdapter
}

func NewDefaultAdapterRegistry() *AdapterRegistry {
	registry := &AdapterRegistry{adapters: make(map[string]ImageAdapter)}
	registry.Register(NewAgnesAdapter())
	registry.Register(NewOpenAIAdapter())
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
