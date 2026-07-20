package backend

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"
)

const defaultOpenCodeBaseURL = "https://opencode.ai/zen"

type OpenCodeAdapter struct{}

func NewOpenCodeAdapter() *OpenCodeAdapter { return &OpenCodeAdapter{} }

func (*OpenCodeAdapter) Platform() string { return "opencode" }

func (*OpenCodeAdapter) Descriptor() PlatformDescriptor {
	return PlatformDescriptor{Key: "opencode", DisplayName: "OpenCode", Protocol: "opencode", DefaultBaseURL: defaultOpenCodeBaseURL}
}

func (*OpenCodeAdapter) NormalizeBaseURL(baseURL string) string {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return baseURL
	}
	if !strings.HasSuffix(strings.ToLower(strings.TrimRight(parsed.Path, "/")), "/v1") {
		parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1"
	}
	parsed.RawPath = ""
	return parsed.String()
}

func (adapter *OpenCodeAdapter) TransformRequestBody(endpoint string, body []byte) []byte {
	if canonicalRelayPath(endpoint) != "chat/completions" {
		return body
	}
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil || payload == nil {
		return body
	}
	if _, hasMessages := payload["messages"].([]any); hasMessages {
		return body
	}
	messages := make([]map[string]string, 0, 2)
	if system, ok := payload["system"].(string); ok && strings.TrimSpace(system) != "" {
		messages = append(messages, map[string]string{"role": "system", "content": strings.TrimSpace(system)})
	}
	partsText := opencodePartsText(payload["parts"])
	if partsText != "" {
		messages = append(messages, map[string]string{"role": "user", "content": partsText})
	} else if message, ok := payload["message"].(string); ok && strings.TrimSpace(message) != "" {
		messages = append(messages, map[string]string{"role": "user", "content": strings.TrimSpace(message)})
	}
	if len(messages) == 0 {
		return body
	}
	converted := map[string]any{"messages": messages}
	switch model := payload["model"].(type) {
	case string:
		if strings.TrimSpace(model) != "" {
			converted["model"] = strings.TrimSpace(model)
		}
	case map[string]any:
		for _, key := range []string{"modelID", "model_id", "id"} {
			if value, ok := model[key].(string); ok && strings.TrimSpace(value) != "" {
				converted["model"] = strings.TrimSpace(value)
				break
			}
		}
	}
	for _, key := range []string{"stream", "temperature", "top_p", "max_tokens", "stop"} {
		if value, ok := payload[key]; ok {
			converted[key] = value
		}
	}
	result, err := json.Marshal(converted)
	if err != nil {
		return body
	}
	return result
}

func opencodePartsText(value any) string {
	parts, ok := value.([]any)
	if !ok {
		return ""
	}
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		switch item := part.(type) {
		case string:
			if strings.TrimSpace(item) != "" {
				texts = append(texts, strings.TrimSpace(item))
			}
		case map[string]any:
			for _, key := range []string{"text", "content"} {
				if text, ok := item[key].(string); ok && strings.TrimSpace(text) != "" {
					texts = append(texts, strings.TrimSpace(text))
					break
				}
			}
		}
	}
	return strings.Join(texts, "\n")
}

func logOpenCodeResponsesBridge(stage string, chatBody []byte, statusCode int, responseBody []byte) {
	var payload map[string]any
	if json.Unmarshal(chatBody, &payload) != nil {
		return
	}
	roles := make([]string, 0)
	if messages, ok := payload["messages"].([]any); ok {
		for _, raw := range messages {
			if message, ok := raw.(map[string]any); ok {
				if role, ok := message["role"].(string); ok {
					roles = append(roles, role)
				}
			}
		}
	}
	toolCount := 0
	if tools, ok := payload["tools"].([]any); ok {
		toolCount = len(tools)
	}
	responsePreview := ""
	if statusCode >= http.StatusBadRequest {
		responsePreview = string(responseBody)
		if len(responsePreview) > 1000 {
			responsePreview = responsePreview[:1000]
		}
	}
	log.Printf("opencode_responses_bridge stage=%s status=%d model=%q stream=%v roles=%q tools=%d tool_choice_type=%T max_tokens=%v response=%q", stage, statusCode, payload["model"], payload["stream"], roles, toolCount, payload["tool_choice"], payload["max_tokens"], responsePreview)
}
