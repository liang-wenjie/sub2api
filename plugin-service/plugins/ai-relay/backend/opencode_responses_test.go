package backend

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestResponsesRequestToChatCompletionsPreservesToolLoop(t *testing.T) {
	converted, err := responsesRequestToChatCompletions([]byte(`{
		"model":"deepseek-v4-flash-free",
		"stream":true,
		"input":[
			{"role":"user","content":"read file"},
			{"type":"function_call_output","call_id":"call_1","output":"contents"}
		],
		"tools":[{"type":"function","name":"read","description":"Read a file","parameters":{"type":"object"}}]
	}`))
	if err != nil {
		t.Fatal(err)
	}

	var payload struct {
		Model    string `json:"model"`
		Stream   bool   `json:"stream"`
		Messages []struct {
			Role       string `json:"role"`
			Content    string `json:"content"`
			ToolCallID string `json:"tool_call_id"`
		} `json:"messages"`
		Tools []struct {
			Type     string `json:"type"`
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(converted, &payload); err != nil {
		t.Fatalf("converted body is invalid JSON: %v", err)
	}
	if payload.Model != "deepseek-v4-flash-free" || !payload.Stream {
		t.Fatalf("model/stream = %q/%v", payload.Model, payload.Stream)
	}
	if len(payload.Messages) != 2 || payload.Messages[0].Role != "user" || payload.Messages[0].Content != "read file" {
		t.Fatalf("messages = %#v", payload.Messages)
	}
	if payload.Messages[1].Role != "tool" || payload.Messages[1].ToolCallID != "call_1" || payload.Messages[1].Content != "contents" {
		t.Fatalf("tool message = %#v", payload.Messages[1])
	}
	if len(payload.Tools) != 1 || payload.Tools[0].Type != "function" || payload.Tools[0].Function.Name != "read" {
		t.Fatalf("tools = %#v", payload.Tools)
	}
}

func TestResponsesRequestToChatCompletionsNormalizesCodexControls(t *testing.T) {
	converted, err := responsesRequestToChatCompletions([]byte(`{
		"model":"deepseek-v4-flash-free","stream":true,"instructions":"system rules",
		"max_output_tokens":321,"tool_choice":"auto","input":"hello",
		"tools":[{"type":"function","name":"read","parameters":{"type":"object"}}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(converted, &payload); err != nil {
		t.Fatal(err)
	}
	messages := payload["messages"].([]any)
	if messages[0].(map[string]any)["role"] != "system" || messages[0].(map[string]any)["content"] != "system rules" {
		t.Fatalf("messages = %#v", messages)
	}
	if payload["max_tokens"] != float64(321) || payload["tool_choice"] != "auto" {
		t.Fatalf("controls = %#v", payload)
	}
	streamOptions, ok := payload["stream_options"].(map[string]any)
	if !ok || streamOptions["include_usage"] != true {
		t.Fatalf("stream_options = %#v", payload["stream_options"])
	}
}

func TestResponsesRequestToChatCompletionsNormalizesDeveloperRole(t *testing.T) {
	converted, err := responsesRequestToChatCompletions([]byte(`{
		"model":"deepseek-v4-flash-free",
		"input":[{"role":"developer","content":"Follow repository rules."},{"role":"user","content":"hello"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Messages []struct {
			Role string `json:"role"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(converted, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Messages) != 2 || payload.Messages[0].Role != "system" || payload.Messages[1].Role != "user" {
		t.Fatalf("messages = %#v", payload.Messages)
	}
}

func TestChatCompletionToResponsesReturnsFunctionCall(t *testing.T) {
	result, err := chatCompletionToResponses([]byte(`{
		"id":"chatcmpl_1","created":1,"model":"deepseek-v4-flash-free",
		"choices":[{"message":{"role":"assistant","tool_calls":[{
			"id":"call_1","type":"function","function":{"name":"read","arguments":"{}"}
		}]}}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		ID     string `json:"id"`
		Output []struct {
			Type   string `json:"type"`
			CallID string `json:"call_id"`
			Name   string `json:"name"`
		} `json:"output"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ID != "resp_1" || len(payload.Output) != 1 || payload.Output[0].Type != "function_call" || payload.Output[0].CallID != "call_1" || payload.Output[0].Name != "read" {
		t.Fatalf("response = %#v", payload)
	}
}

func TestChatCompletionSSEToResponsesEmitsTextAndToolCallLifecycle(t *testing.T) {
	stream := "data: {\"id\":\"chatcmpl_1\",\"model\":\"deepseek\",\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_1\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"read\",\"arguments\":\"{}\"}}]}}]}\n\n" +
		"data: [DONE]\n\n"
	converted := chatCompletionSSEToResponses([]byte(stream))
	for _, event := range []string{"response.created", "response.output_text.delta", "response.function_call_arguments.delta", "response.function_call_arguments.done", "response.completed"} {
		if !bytes.Contains(converted, []byte("event: "+event)) {
			t.Fatalf("missing %s in %s", event, converted)
		}
	}
	if !bytes.HasSuffix(converted, []byte("data: [DONE]\n\n")) {
		t.Fatalf("missing done marker: %s", converted)
	}
	if bytes.Count(converted, []byte("event: response.completed")) != 1 || bytes.Count(converted, []byte("data: [DONE]")) != 1 {
		t.Fatalf("completion emitted more than once: %s", converted)
	}
	if !bytes.Contains(converted, []byte("event: response.content_part.done")) || !bytes.Contains(converted, []byte(`"text":"Hi"`)) {
		t.Fatalf("missing completed text lifecycle: %s", converted)
	}
}
