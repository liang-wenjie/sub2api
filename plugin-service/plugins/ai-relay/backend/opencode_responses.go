package backend

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func responsesRequestToChatCompletions(body []byte) ([]byte, error) {
	var request map[string]any
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, fmt.Errorf("invalid Responses request: %w", err)
	}
	messages, err := responsesInputMessages(request["input"])
	if err != nil {
		return nil, err
	}
	if instructions, ok := request["instructions"].(string); ok && strings.TrimSpace(instructions) != "" {
		messages = append([]any{map[string]any{"role": "system", "content": strings.TrimSpace(instructions)}}, messages...)
	}
	payload := map[string]any{"messages": messages}
	for _, key := range []string{"model", "stream", "temperature", "top_p", "frequency_penalty", "presence_penalty", "stop", "parallel_tool_calls"} {
		if value, ok := request[key]; ok {
			payload[key] = value
		}
	}
	if value, ok := request["max_output_tokens"]; ok {
		payload["max_tokens"] = value
	} else if value, ok := request["max_tokens"]; ok {
		payload["max_tokens"] = value
	}
	if stream, _ := request["stream"].(bool); stream {
		payload["stream_options"] = map[string]any{"include_usage": true}
	}
	if tools, ok := request["tools"].([]any); ok {
		converted := make([]any, 0, len(tools))
		for _, raw := range tools {
			tool, ok := raw.(map[string]any)
			if !ok || tool["type"] != "function" {
				continue
			}
			function := map[string]any{}
			for _, key := range []string{"name", "description", "parameters", "strict"} {
				if value, exists := tool[key]; exists {
					function[key] = value
				}
			}
			if name, _ := function["name"].(string); strings.TrimSpace(name) == "" {
				return nil, fmt.Errorf("function tool name is required")
			}
			converted = append(converted, map[string]any{"type": "function", "function": function})
		}
		if len(converted) > 0 {
			payload["tools"] = converted
			if choice := chatToolChoice(request["tool_choice"]); choice != nil {
				payload["tool_choice"] = choice
			}
		}
	}
	return json.Marshal(payload)
}

func chatToolChoice(value any) any {
	if choice, ok := value.(string); ok {
		switch choice {
		case "auto", "none", "required":
			return choice
		}
		return nil
	}
	choice, ok := value.(map[string]any)
	if !ok || choice["type"] != "function" {
		return nil
	}
	function, ok := choice["function"].(map[string]any)
	if !ok {
		return nil
	}
	name, ok := function["name"].(string)
	if !ok || strings.TrimSpace(name) == "" {
		return nil
	}
	return map[string]any{"type": "function", "function": map[string]any{"name": name}}
}

func responsesInputMessages(input any) ([]any, error) {
	if text, ok := input.(string); ok {
		return []any{map[string]any{"role": "user", "content": text}}, nil
	}
	items, ok := input.([]any)
	if !ok {
		return nil, fmt.Errorf("Responses input must be a string or array")
	}
	messages := make([]any, 0, len(items))
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Responses input item must be an object")
		}
		itemType, _ := item["type"].(string)
		switch itemType {
		case "function_call_output":
			callID, _ := item["call_id"].(string)
			if callID == "" {
				return nil, fmt.Errorf("function_call_output call_id is required")
			}
			messages = append(messages, map[string]any{"role": "tool", "tool_call_id": callID, "content": responsesText(item["output"])})
		case "function_call":
			callID, _ := item["call_id"].(string)
			name, _ := item["name"].(string)
			if callID == "" || name == "" {
				return nil, fmt.Errorf("function_call name and call_id are required")
			}
			arguments, _ := item["arguments"].(string)
			messages = append(messages, map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": callID, "type": "function", "function": map[string]any{"name": name, "arguments": arguments}}}})
		default:
			role, _ := item["role"].(string)
			role = normalizeChatRole(role)
			messages = append(messages, map[string]any{"role": role, "content": responsesText(item["content"])})
		}
	}
	return messages, nil
}

func normalizeChatRole(role string) string {
	switch role {
	case "developer":
		return "system"
	case "system", "user", "assistant", "tool":
		return role
	default:
		return "user"
	}
}

func responsesText(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	parts, ok := value.([]any)
	if !ok {
		return ""
	}
	texts := make([]string, 0, len(parts))
	for _, raw := range parts {
		if part, ok := raw.(map[string]any); ok {
			if text, ok := part["text"].(string); ok {
				texts = append(texts, text)
			}
		}
	}
	return strings.Join(texts, "\n")
}

func chatCompletionToResponses(body []byte) ([]byte, error) {
	var chat map[string]any
	if err := json.Unmarshal(body, &chat); err != nil {
		return nil, fmt.Errorf("invalid Chat Completions response: %w", err)
	}
	responseID := strings.ReplaceAll(stringValue(chat["id"]), "chatcmpl", "resp")
	if responseID == "" {
		responseID = "resp_" + fmt.Sprint(time.Now().UnixNano())
	}
	output := []any{}
	if choices, ok := chat["choices"].([]any); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			if message, ok := choice["message"].(map[string]any); ok {
				if content := responsesText(message["content"]); content != "" {
					output = append(output, responseMessage(responseID, content))
				}
				if calls, ok := message["tool_calls"].([]any); ok {
					for _, raw := range calls {
						if call, ok := raw.(map[string]any); ok {
							function, _ := call["function"].(map[string]any)
							output = append(output, map[string]any{"id": "fc_" + stringValue(call["id"]), "type": "function_call", "status": "completed", "call_id": stringValue(call["id"]), "name": stringValue(function["name"]), "arguments": stringValue(function["arguments"])})
						}
					}
				}
			}
		}
	}
	result := map[string]any{"id": responseID, "object": "response", "created_at": intValue(chat["created"]), "status": "completed", "model": stringValue(chat["model"]), "output": output}
	if usage, ok := chat["usage"].(map[string]any); ok {
		result["usage"] = map[string]any{"input_tokens": usage["prompt_tokens"], "output_tokens": usage["completion_tokens"], "total_tokens": usage["total_tokens"]}
	}
	return json.Marshal(result)
}

func responseMessage(responseID, text string) map[string]any {
	return map[string]any{"id": "msg_" + responseID, "type": "message", "status": "completed", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}}}
}
func stringValue(value any) string { result, _ := value.(string); return result }
func intValue(value any) int64 {
	switch value := value.(type) {
	case float64:
		return int64(value)
	case int64:
		return value
	default:
		return time.Now().Unix()
	}
}

func chatCompletionSSEToResponses(stream []byte) []byte {
	var output bytes.Buffer
	state := responseStreamState{calls: map[int]*responseToolCall{}}
	scanner := bufio.NewScanner(bytes.NewReader(stream))
	scanner.Buffer(make([]byte, 4096), 4<<20)
	var data []string
	flush := func() {
		if len(data) == 0 {
			return
		}
		raw := strings.Join(data, "\n")
		data = nil
		if raw == "[DONE]" {
			state.finish(&output)
			return
		}
		var payload map[string]any
		if json.Unmarshal([]byte(raw), &payload) == nil {
			state.consume(payload, &output)
		}
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	flush()
	state.finish(&output)
	return output.Bytes()
}

type responseToolCall struct {
	callID, name, arguments string
	outputIndex             int
}
type responseStreamState struct {
	responseID, model string
	created           bool
	textStarted       bool
	text              string
	completed         bool
	nextIndex         int
	calls             map[int]*responseToolCall
}

func (s *responseStreamState) emit(w *bytes.Buffer, event string, payload map[string]any) {
	encoded, _ := json.Marshal(payload)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, encoded)
}
func (s *responseStreamState) start(payload map[string]any, w *bytes.Buffer) {
	if s.created {
		return
	}
	s.responseID = strings.ReplaceAll(stringValue(payload["id"]), "chatcmpl", "resp")
	if s.responseID == "" {
		s.responseID = "resp_" + fmt.Sprint(time.Now().UnixNano())
	}
	s.model = stringValue(payload["model"])
	s.created = true
	s.emit(w, "response.created", map[string]any{"type": "response.created", "response": map[string]any{"id": s.responseID, "object": "response", "status": "in_progress", "model": s.model, "output": []any{}}})
}
func (s *responseStreamState) consume(payload map[string]any, w *bytes.Buffer) {
	s.start(payload, w)
	choices, _ := payload["choices"].([]any)
	if len(choices) == 0 {
		return
	}
	choice, _ := choices[0].(map[string]any)
	delta, _ := choice["delta"].(map[string]any)
	if text := responsesText(delta["content"]); text != "" {
		if !s.textStarted {
			s.textStarted = true
			s.emit(w, "response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": s.nextIndex, "item": map[string]any{"id": "msg_" + s.responseID, "type": "message", "status": "in_progress", "role": "assistant", "content": []any{}}})
			s.emit(w, "response.content_part.added", map[string]any{"type": "response.content_part.added", "output_index": s.nextIndex, "content_index": 0, "item_id": "msg_" + s.responseID, "part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}}})
			s.nextIndex++
		}
		s.text += text
		s.emit(w, "response.output_text.delta", map[string]any{"type": "response.output_text.delta", "output_index": 0, "content_index": 0, "item_id": "msg_" + s.responseID, "delta": text})
	}
	if calls, ok := delta["tool_calls"].([]any); ok {
		for _, raw := range calls {
			call, _ := raw.(map[string]any)
			index := int(intValue(call["index"]))
			item := s.calls[index]
			if item == nil {
				item = &responseToolCall{callID: stringValue(call["id"]), outputIndex: s.nextIndex}
				s.calls[index] = item
				s.nextIndex++
				s.emit(w, "response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": item.outputIndex, "item": map[string]any{"id": "fc_" + item.callID, "type": "function_call", "status": "in_progress", "call_id": item.callID, "name": "", "arguments": ""}})
			}
			function, _ := call["function"].(map[string]any)
			if name := stringValue(function["name"]); name != "" {
				item.name += name
			}
			if args := stringValue(function["arguments"]); args != "" {
				item.arguments += args
				s.emit(w, "response.function_call_arguments.delta", map[string]any{"type": "response.function_call_arguments.delta", "output_index": item.outputIndex, "item_id": "fc_" + item.callID, "delta": args})
			}
		}
	}
}
func (s *responseStreamState) finish(w *bytes.Buffer) {
	if !s.created || s.completed {
		return
	}
	s.completed = true
	for _, call := range s.calls {
		s.emit(w, "response.function_call_arguments.done", map[string]any{"type": "response.function_call_arguments.done", "output_index": call.outputIndex, "item_id": "fc_" + call.callID, "arguments": call.arguments})
		s.emit(w, "response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": call.outputIndex, "item": map[string]any{"id": "fc_" + call.callID, "type": "function_call", "status": "completed", "call_id": call.callID, "name": call.name, "arguments": call.arguments}})
	}
	if s.textStarted {
		message := responseMessage(s.responseID, s.text)
		s.emit(w, "response.output_text.done", map[string]any{"type": "response.output_text.done", "output_index": 0, "content_index": 0, "item_id": "msg_" + s.responseID, "text": s.text})
		s.emit(w, "response.content_part.done", map[string]any{"type": "response.content_part.done", "output_index": 0, "content_index": 0, "item_id": "msg_" + s.responseID, "part": message["content"].([]any)[0]})
		s.emit(w, "response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": 0, "item": message})
	}
	s.emit(w, "response.completed", map[string]any{"type": "response.completed", "response": map[string]any{"id": s.responseID, "object": "response", "status": "completed", "model": s.model}})
	w.WriteString("data: [DONE]\n\n")
}
