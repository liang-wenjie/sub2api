package backend

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestResponsesRequestToChatCompletionsWrapsCustomToolInput(t *testing.T) {
	body, context, err := responsesRequestToChatCompletionsWithContext([]byte(`{
		"model":"deepseek-v4-flash-free",
		"input":"update the file",
		"tools":[{"type":"custom","name":"apply_patch","description":"Apply a patch"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if !context.customTools["apply_patch"] || !context.declaredTools["apply_patch"] {
		t.Fatalf("context = %#v", context)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	tools := payload["tools"].([]any)
	function := tools[0].(map[string]any)["function"].(map[string]any)
	if function["name"] != "apply_patch" {
		t.Fatalf("function = %#v", function)
	}
	parameters := function["parameters"].(map[string]any)
	required := parameters["required"].([]any)
	if len(required) != 1 || required[0] != "input" {
		t.Fatalf("parameters = %#v", parameters)
	}
}

func TestResponsesRequestToChatCompletionsPreservesCustomToolLoop(t *testing.T) {
	body, _, err := responsesRequestToChatCompletionsWithContext([]byte(`{
		"model":"deepseek-v4-flash-free",
		"input":[
			{"type":"custom_tool_call","call_id":"call_1","name":"apply_patch","input":"*** Begin Patch\n*** End Patch"},
			{"type":"custom_tool_call_output","call_id":"call_1","output":"Done!"}
		],
		"tools":[{"type":"custom","name":"apply_patch"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	messages := payload["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	assistant := messages[0].(map[string]any)
	toolCalls := assistant["tool_calls"].([]any)
	function := toolCalls[0].(map[string]any)["function"].(map[string]any)
	if function["name"] != "apply_patch" || function["arguments"] != `{"input":"*** Begin Patch\n*** End Patch"}` {
		t.Fatalf("function = %#v", function)
	}
	tool := messages[1].(map[string]any)
	if tool["role"] != "tool" || tool["tool_call_id"] != "call_1" || tool["content"] != "Done!" {
		t.Fatalf("tool message = %#v", tool)
	}
}

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
	if payload.Messages[1].Role != "user" || payload.Messages[1].ToolCallID != "" || payload.Messages[1].Content != "Tool output for call_1:\ncontents" {
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

func TestResponsesRequestToChatCompletionsPreservesReasoningAndImageParts(t *testing.T) {
	converted, err := responsesRequestToChatCompletions([]byte(`{
		"model":"deepseek-v4-flash-free","stream":true,
		"reasoning":{"effort":"high"},
		"input":[{"role":"user","content":[
			{"type":"input_text","text":"Describe this image"},
			{"type":"input_image","image_url":{"url":"data:image/png;base64,AAA"}}
		]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(converted, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort = %#v", payload["reasoning_effort"])
	}
	messages := payload["messages"].([]any)
	content := messages[0].(map[string]any)["content"].([]any)
	if len(content) != 2 || content[0].(map[string]any)["type"] != "text" || content[1].(map[string]any)["type"] != "image_url" {
		t.Fatalf("content = %#v", content)
	}
}

func TestResponsesRequestToChatCompletionsGroupsParallelCallsAndFiltersIncomplete(t *testing.T) {
	converted, err := responsesRequestToChatCompletions([]byte(`{
		"model":"deepseek-v4-flash-free",
		"input":[
			{"role":"user","content":"run two commands"},
			{"type":"function_call","call_id":"call_one","name":"read","arguments":"{}"},
			{"type":"function_call","call_id":"call_missing","name":"read","arguments":"{\"missing\":true}"},
			{"type":"function_call_output","call_id":"call_one","output":"README.md"},
			{"type":"function_call_output","call_id":"call_one","output":"README.md"}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(converted, &payload); err != nil {
		t.Fatal(err)
	}
	messages := payload["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("messages = %#v", messages)
	}
	assistant := messages[1].(map[string]any)
	calls := assistant["tool_calls"].([]any)
	if len(calls) != 1 || calls[0].(map[string]any)["id"] != "call_one" {
		t.Fatalf("tool calls = %#v", calls)
	}
	if messages[2].(map[string]any)["role"] != "tool" || messages[2].(map[string]any)["content"] != "README.md" {
		t.Fatalf("tool output = %#v", messages[2])
	}
}

func TestResponsesRequestToChatCompletionsAddsOpenCodeMessageIDs(t *testing.T) {
	converted, err := responsesRequestToChatCompletions([]byte(`{
		"model":"deepseek-v4-flash-free",
		"input":[
			{"role":"user","content":"run command"},
			{"type":"function_call","call_id":"call_1","name":"read","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_1","output":"README.md"}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(converted, &payload); err != nil {
		t.Fatal(err)
	}
	messages := payload["messages"].([]any)
	if messages[0].(map[string]any)["id"] != "msg_0" {
		t.Fatalf("user message id = %#v", messages[0])
	}
	if messages[1].(map[string]any)["id"] != "msg_1" {
		t.Fatalf("assistant message id = %#v", messages[1])
	}
	if messages[2].(map[string]any)["id"] != "tool_call_1" {
		t.Fatalf("tool message id = %#v", messages[2])
	}
}

func TestResponsesRequestToChatCompletionsMergesReasoningIntoToolTurn(t *testing.T) {
	converted, err := responsesRequestToChatCompletions([]byte(`{
		"model":"deepseek-v4-flash-free",
		"input":[
			{"role":"user","content":"inspect repository"},
			{"type":"reasoning","content":[{"type":"reasoning_text","text":"Need directory"}]},
			{"type":"function_call","call_id":"call_1","name":"read","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_1","output":"README.md"}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(converted, &payload); err != nil {
		t.Fatal(err)
	}
	messages := payload["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("messages = %#v", messages)
	}
	assistant := messages[1].(map[string]any)
	if assistant["role"] != "assistant" || assistant["reasoning_content"] != "Need directory" {
		t.Fatalf("assistant = %#v", assistant)
	}
	if messages[2].(map[string]any)["role"] != "tool" {
		t.Fatalf("tool message = %#v", messages[2])
	}
}

func TestResponsesRequestToChatCompletionsNormalizesReferenceToolShapes(t *testing.T) {
	converted, err := responsesRequestToChatCompletions([]byte(`{
		"model":"deepseek-v4-flash-free",
		"input":[{"role":"user","content":"use tools"}],
		"tools":[
			{"type":"tool_search","description":"Search tools","parameters":{"type":"object"}},
			{"type":"function","name":"bad.name","parameters":{"type":"object"}},
			{"type":"function","name":"read","parameters":{"type":"object"}}
		],
		"tool_choice":{"type":"function","function":{"name":"read"}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(converted, &payload); err != nil {
		t.Fatal(err)
	}
	tools := payload["tools"].([]any)
	if len(tools) != 2 || tools[0].(map[string]any)["function"].(map[string]any)["name"] != "tool_search" || tools[1].(map[string]any)["function"].(map[string]any)["name"] != "read" {
		t.Fatalf("tools = %#v", tools)
	}
	if payload["tool_choice"].(map[string]any)["function"].(map[string]any)["name"] != "read" {
		t.Fatalf("tool_choice = %#v", payload["tool_choice"])
	}
}

func TestResponsesRequestToChatCompletionsNormalizesObjectToolArguments(t *testing.T) {
	converted, err := responsesRequestToChatCompletions([]byte(`{
		"model":"deepseek-v4-flash-free",
		"input":[
			{"type":"function_call","call_id":"call_1","name":"read","arguments":{"path":"README.md"}},
			{"type":"function_call_output","call_id":"call_1","output":"ok"}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(converted, &payload); err != nil {
		t.Fatal(err)
	}
	arguments := payload["messages"].([]any)[0].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["arguments"]
	if arguments != `{"path":"README.md"}` {
		t.Fatalf("arguments = %#v", arguments)
	}
}

func TestResponsesRequestToChatCompletionsSerializesStructuredToolOutput(t *testing.T) {
	converted, err := responsesRequestToChatCompletions([]byte(`{
		"model":"deepseek-v4-flash-free",
		"input":[
			{"type":"function_call","call_id":"call_1","name":"read","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_1","output":{"path":"README.md","ok":true}}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(converted, &payload); err != nil {
		t.Fatal(err)
	}
	tool := payload["messages"].([]any)[1].(map[string]any)
	var content map[string]any
	if err := json.Unmarshal([]byte(tool["content"].(string)), &content); err != nil {
		t.Fatalf("tool content is not JSON: %#v", tool["content"])
	}
	if content["path"] != "README.md" || content["ok"] != true {
		t.Fatalf("tool content = %#v", content)
	}
}

func TestResponsesRequestToChatCompletionsPreservesAssistantReasoningContent(t *testing.T) {
	converted, err := responsesRequestToChatCompletions([]byte(`{
		"model":"deepseek-v4-flash-free",
		"input":[{"role":"assistant","content":"answer","reasoning_content":"think first"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(converted, &payload); err != nil {
		t.Fatal(err)
	}
	assistant := payload["messages"].([]any)[0].(map[string]any)
	if assistant["reasoning_content"] != "think first" || assistant["content"] != "answer" {
		t.Fatalf("assistant = %#v", assistant)
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

func TestChatCompletionToResponsesRestoresCustomToolCall(t *testing.T) {
	context := newResponsesBridgeContext()
	context.customTools["apply_patch"] = true
	context.declaredTools["apply_patch"] = true
	result, err := chatCompletionToResponsesWithContext([]byte(`{
		"id":"chatcmpl_1","created":1,"model":"deepseek",
		"choices":[{"message":{"role":"assistant","tool_calls":[{
			"id":"call_1","type":"function","function":{
				"name":"apply_patch",
				"arguments":"{\"input\":\"*** Begin Patch\\n*** End Patch\"}"
			}
		}]}}]
	}`), context)
	if err != nil {
		t.Fatal(err)
	}

	var payload map[string]any
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatal(err)
	}
	item := payload["output"].([]any)[0].(map[string]any)
	if item["type"] != "custom_tool_call" || item["name"] != "apply_patch" || item["input"] != "*** Begin Patch\n*** End Patch" {
		t.Fatalf("item = %#v", item)
	}
	if _, exists := item["arguments"]; exists {
		t.Fatalf("custom item must not contain arguments: %#v", item)
	}
}

func TestExtractCustomToolInputFallsBackToRawArguments(t *testing.T) {
	arguments := `{"wrong":true}`
	if got := extractCustomToolInput(arguments); got != arguments {
		t.Fatalf("got %q", got)
	}
}

func TestChatCompletionToResponsesPreservesReasoningAndUsageDetails(t *testing.T) {
	result, err := chatCompletionToResponses([]byte(`{
		"id":"chatcmpl_reasoning","created":1,"model":"deepseek",
		"choices":[{"message":{"role":"assistant","content":"answer","reasoning_content":"think","tool_calls":[
			{"id":"call_1","type":"function","function":{"name":"read","arguments":"{}"}}
		]}}],
		"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7,"prompt_tokens_details":{"cached_tokens":1}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(result, &payload); err != nil {
		t.Fatal(err)
	}
	output := payload["output"].([]any)
	if len(output) != 3 || output[0].(map[string]any)["type"] != "reasoning" || output[1].(map[string]any)["type"] != "message" || output[2].(map[string]any)["id"] != "fc_1" {
		t.Fatalf("output = %#v", output)
	}
	usage := payload["usage"].(map[string]any)
	if usage["input_tokens"] != float64(3) || usage["output_tokens"] != float64(4) || usage["total_tokens"] != float64(7) {
		t.Fatalf("usage = %#v", usage)
	}
	if _, ok := usage["input_tokens_details"]; !ok {
		t.Fatalf("usage details missing: %#v", usage)
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

func TestChatCompletionSSEToResponsesKeepsIndexesAndReasoning(t *testing.T) {
	stream := "data: {\"id\":\"chatcmpl_2\",\"created\":1,\"model\":\"deepseek\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"read\",\"arguments\":\"{}\"}}]}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_2\",\"model\":\"deepseek\",\"choices\":[{\"delta\":{\"reasoning_content\":\"think\"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_2\",\"model\":\"deepseek\",\"choices\":[{\"delta\":{\"content\":\"answer\"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_2\",\"model\":\"deepseek\",\"choices\":[{\"finish_reason\":\"stop\",\"delta\":{}}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":3,\"total_tokens\":5}}\n\n" +
		"data: [DONE]\n\n"
	converted := chatCompletionSSEToResponses([]byte(stream))
	if !bytes.Contains(converted, []byte("event: response.reasoning_text.delta")) || !bytes.Contains(converted, []byte("event: response.reasoning_text.done")) {
		t.Fatalf("missing reasoning lifecycle: %s", converted)
	}
	if !bytes.Contains(converted, []byte("event: response.output_text.delta")) || !bytes.Contains(converted, []byte(`"delta":"answer"`)) {
		t.Fatalf("text delta missing: %s", converted)
	}
	if !bytes.Contains(converted, []byte(`"input_tokens":2`)) || !bytes.Contains(converted, []byte(`"output_tokens":3`)) {
		t.Fatalf("usage missing from completion: %s", converted)
	}
}
