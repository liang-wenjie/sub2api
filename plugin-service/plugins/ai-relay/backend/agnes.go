package backend

import (
	"encoding/json"
	"fmt"
	"strings"
)

const defaultAgnesImageBaseURL = "https://apihub.agnes-ai.com/v1"

type AgnesAdapter struct{}

type AgnesImageRequest struct {
	Model        string         `json:"model"`
	Prompt       string         `json:"prompt"`
	Size         string         `json:"size"`
	Ratio        string         `json:"ratio,omitempty"`
	ReturnBase64 bool           `json:"return_base64,omitempty"`
	ExtraBody    AgnesExtraBody `json:"extra_body,omitempty"`
}

type AgnesExtraBody struct {
	ResponseFormat string   `json:"response_format,omitempty"`
	Image          []string `json:"image,omitempty"`
}

func NewAgnesAdapter() *AgnesAdapter {
	return &AgnesAdapter{}
}

func (*AgnesAdapter) Platform() string {
	return "agnes"
}

func (*AgnesAdapter) Descriptor() PlatformDescriptor {
	return PlatformDescriptor{Key: "agnes", DisplayName: "Agnes", Operation: "images/generations", Protocol: "agnes-image"}
}

func (*AgnesAdapter) Endpoint(config RouteConfig) string {
	return agnesBaseURL(config) + "/images/generations"
}

func (*AgnesAdapter) ModelsEndpoint(config RouteConfig) string {
	return agnesBaseURL(config) + "/models"
}

func (*AgnesAdapter) ChatCompletionsEndpoint(config RouteConfig) string {
	return agnesBaseURL(config) + "/chat/completions"
}

func agnesBaseURL(config RouteConfig) string {
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultAgnesImageBaseURL
	}
	return baseURL
}

func (*AgnesAdapter) BuildRequest(config RouteConfig, request OpenAIImageRequest) (AgnesImageRequest, error) {
	return buildAgnesImageRequest(config, request.Model, request.Prompt, request.Size, request.ResponseFormat, nil)
}

func (*AgnesAdapter) BuildEditRequest(config RouteConfig, request OpenAIImageEditRequest) (AgnesImageRequest, error) {
	if len(request.Images) == 0 {
		return AgnesImageRequest{}, fmt.Errorf("image is required")
	}
	return buildAgnesImageRequest(config, request.Model, request.Prompt, request.Size, request.ResponseFormat, request.Images)
}

func buildAgnesImageRequest(_ RouteConfig, modelValue, promptValue, sizeValue, responseFormat string, images []string) (AgnesImageRequest, error) {
	prompt := strings.TrimSpace(promptValue)
	if prompt == "" {
		return AgnesImageRequest{}, fmt.Errorf("prompt is required")
	}
	model := strings.TrimSpace(modelValue)
	if model == "" {
		return AgnesImageRequest{}, fmt.Errorf("model is required")
	}
	size, ratio, err := agnesSizeAndRatio(sizeValue)
	if err != nil {
		return AgnesImageRequest{}, err
	}
	outgoing := AgnesImageRequest{Model: model, Prompt: prompt, Size: size, Ratio: ratio}
	if len(images) > 0 {
		outgoing.ExtraBody.Image = images
	}
	switch strings.TrimSpace(responseFormat) {
	case "", "url":
		outgoing.ExtraBody.ResponseFormat = "url"
	case "b64_json":
		outgoing.ReturnBase64 = true
		outgoing.ExtraBody.ResponseFormat = "b64_json"
	default:
		return AgnesImageRequest{}, fmt.Errorf("unsupported response_format %q", responseFormat)
	}
	return outgoing, nil
}

func (*AgnesAdapter) ParseResponse(body []byte) (OpenAIImageResponse, error) {
	var response OpenAIImageResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return OpenAIImageResponse{}, fmt.Errorf("invalid Agnes image response: %w", err)
	}
	if len(response.Data) == 0 {
		return OpenAIImageResponse{}, fmt.Errorf("Agnes image response has no data")
	}
	return response, nil
}

func agnesSizeAndRatio(value string) (string, string, error) {
	switch strings.TrimSpace(value) {
	case "", "1024x1024", "auto":
		return "1K", "1:1", nil
	case "1536x1024":
		return "1K", "3:2", nil
	case "1024x1536":
		return "1K", "2:3", nil
	default:
		return "", "", fmt.Errorf("unsupported size %q", value)
	}
}
