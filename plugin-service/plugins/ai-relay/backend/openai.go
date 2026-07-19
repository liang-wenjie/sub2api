package backend

import "fmt"

const transparentProtocol = "transparent"

type OpenAIAdapter struct{}

func NewOpenAIAdapter() *OpenAIAdapter {
	return &OpenAIAdapter{}
}

func (*OpenAIAdapter) Platform() string {
	return "openai"
}

func (*OpenAIAdapter) Descriptor() PlatformDescriptor {
	return PlatformDescriptor{Key: "openai", DisplayName: "OpenAI", Protocol: transparentProtocol, DefaultBaseURL: "https://api.openai.com/v1"}
}

func (*OpenAIAdapter) NormalizeBaseURL(baseURL string) string { return baseURL }

func (*OpenAIAdapter) TransformRequestBody(_ string, body []byte) []byte { return body }

func (*OpenAIAdapter) Endpoint(config RouteConfig) string {
	return config.BaseURL
}

func (*OpenAIAdapter) ModelsEndpoint(config RouteConfig) string {
	return config.BaseURL
}

func (*OpenAIAdapter) ChatCompletionsEndpoint(config RouteConfig) string {
	return config.BaseURL
}

func (*OpenAIAdapter) BuildRequest(RouteConfig, OpenAIImageRequest) (AgnesImageRequest, error) {
	return AgnesImageRequest{}, fmt.Errorf("OpenAI image conversion is not supported")
}

func (*OpenAIAdapter) BuildEditRequest(RouteConfig, OpenAIImageEditRequest) (AgnesImageRequest, error) {
	return AgnesImageRequest{}, fmt.Errorf("OpenAI image conversion is not supported")
}

func (*OpenAIAdapter) ParseResponse([]byte) (OpenAIImageResponse, error) {
	return OpenAIImageResponse{}, fmt.Errorf("OpenAI image conversion is not supported")
}
