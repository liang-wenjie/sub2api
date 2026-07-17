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
	ResponseFormat string `json:"response_format,omitempty"`
}

func NewAgnesAdapter() *AgnesAdapter {
	return &AgnesAdapter{}
}

func (*AgnesAdapter) Platform() string {
	return "agnes"
}

func (*AgnesAdapter) Endpoint(config RouteConfig) string {
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultAgnesImageBaseURL
	}
	return baseURL + "/images/generations"
}

func (*AgnesAdapter) BuildRequest(config RouteConfig, request OpenAIImageRequest) (AgnesImageRequest, error) {
	prompt := strings.TrimSpace(request.Prompt)
	if prompt == "" {
		return AgnesImageRequest{}, fmt.Errorf("prompt is required")
	}
	model := strings.TrimSpace(config.DefaultModel)
	if mapped := strings.TrimSpace(config.ModelMap[strings.TrimSpace(request.Model)]); mapped != "" {
		model = mapped
	}
	if model == "" {
		return AgnesImageRequest{}, fmt.Errorf("default model is required")
	}
	size, ratio, err := agnesSizeAndRatio(request.Size)
	if err != nil {
		return AgnesImageRequest{}, err
	}
	if tier := strings.TrimSpace(config.QualityMap[strings.TrimSpace(request.Quality)]); tier != "" {
		size = tier
	}

	outgoing := AgnesImageRequest{Model: model, Prompt: prompt, Size: size, Ratio: ratio}
	switch strings.TrimSpace(request.ResponseFormat) {
	case "", "url":
		outgoing.ExtraBody.ResponseFormat = "url"
	case "b64_json":
		outgoing.ReturnBase64 = true
	default:
		return AgnesImageRequest{}, fmt.Errorf("unsupported response_format %q", request.ResponseFormat)
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
