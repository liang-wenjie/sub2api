package backend

import (
	"encoding/json"
	"testing"
)

func TestOpenCodeAdapterMetadataAndBaseURL(t *testing.T) {
	adapter := NewOpenCodeAdapter()
	if got := adapter.Platform(); got != "opencode" {
		t.Fatalf("Platform() = %q", got)
	}
	if got := adapter.Descriptor(); got != (PlatformDescriptor{Key: "opencode", DisplayName: "OpenCode", Protocol: "opencode", DefaultBaseURL: "https://opencode.ai/zen"}) {
		t.Fatalf("Descriptor() = %#v", got)
	}
	for _, test := range []struct{ base, want string }{
		{"https://opencode.ai/zen", "https://opencode.ai/zen/v1"},
		{"https://opencode.ai/zen/", "https://opencode.ai/zen/v1"},
		{"https://opencode.ai/zen/v1", "https://opencode.ai/zen/v1"},
	} {
		if got := adapter.NormalizeBaseURL(test.base); got != test.want {
			t.Errorf("NormalizeBaseURL(%q) = %q, want %q", test.base, got, test.want)
		}
	}
}

func TestOpenCodeAdapterConvertsNativeChatPayload(t *testing.T) {
	body := []byte(`{"model":{"providerID":"openai","modelID":"gpt-5"},"system":"Be concise.","parts":[{"type":"text","text":"Hello"},{"type":"text","text":"world"}],"stream":true,"temperature":0.2}`)
	converted := NewOpenCodeAdapter().TransformRequestBody("chat/completions", body)
	var payload map[string]any
	if err := json.Unmarshal(converted, &payload); err != nil {
		t.Fatalf("converted body is invalid JSON: %v", err)
	}
	if payload["model"] != "gpt-5" || payload["stream"] != true || payload["temperature"] != 0.2 {
		t.Fatalf("payload = %#v", payload)
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 2 || messages[0].(map[string]any)["role"] != "system" || messages[1].(map[string]any)["content"] != "Hello\nworld" {
		t.Fatalf("messages = %#v", payload["messages"])
	}
}

func TestOpenCodeAdapterKeepsOpenAIBodiesAndInvalidBodies(t *testing.T) {
	adapter := NewOpenCodeAdapter()
	openAI := []byte(`{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`)
	if got := adapter.TransformRequestBody("chat/completions", openAI); string(got) != string(openAI) {
		t.Fatalf("OpenAI body changed to %s", got)
	}
	invalid := []byte("not-json")
	if got := adapter.TransformRequestBody("chat/completions", invalid); string(got) != string(invalid) {
		t.Fatalf("invalid body changed to %s", got)
	}
	if got := adapter.TransformRequestBody("responses", []byte(`{"parts":[{"text":"hi"}]}`)); string(got) != `{"parts":[{"text":"hi"}]}` {
		t.Fatalf("non-chat body changed to %s", got)
	}
}

func TestOpenCodeAdapterUsesMessageFallbackAndRelativeMappings(t *testing.T) {
	adapter := NewOpenCodeAdapter()
	converted := adapter.TransformRequestBody("chat/completions", []byte(`{"model":"gpt-5","message":" hello "}`))
	var payload struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(converted, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Model != "gpt-5" || len(payload.Messages) != 1 || payload.Messages[0].Content != "hello" {
		t.Fatalf("payload = %#v", payload)
	}

	config := RouteConfig{
		BaseURL:      adapter.NormalizeBaseURL("https://opencode.ai/zen"),
		PathMappings: map[string]string{"v1/chat/completions": "custom/chat"},
	}
	endpoint, err := ResolveRouteEndpointURL(config, "chat/completions")
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != "https://opencode.ai/zen/v1/custom/chat" {
		t.Fatalf("mapped endpoint = %q", endpoint)
	}
}
