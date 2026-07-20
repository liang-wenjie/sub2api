package backend

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const customToolInputDescription = "The raw input for this tool, passed through verbatim."

type responsesBridgeContext struct {
	customTools   map[string]bool
	declaredTools map[string]bool
}

func newResponsesBridgeContext() responsesBridgeContext {
	return responsesBridgeContext{
		customTools:   map[string]bool{},
		declaredTools: map[string]bool{},
	}
}

func customToolParameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input": map[string]any{"type": "string", "description": customToolInputDescription},
		},
		"required": []any{"input"},
	}
}

func wrapCustomToolInput(input string) string {
	encoded, _ := json.Marshal(map[string]string{"input": input})
	return string(encoded)
}

func responsesRequestToChatCompletions(body []byte) ([]byte, error) {
	converted, _, err := responsesRequestToChatCompletionsWithContext(body)
	return converted, err
}

func responsesRequestToChatCompletionsWithContext(body []byte) ([]byte, responsesBridgeContext, error) {
	context := newResponsesBridgeContext()
	var request map[string]any
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, context, fmt.Errorf("invalid Responses request: %w", err)
	}
	messages, err := responsesInputMessages(request["input"])
	if err != nil {
		return nil, context, err
	}
	if instructions, ok := request["instructions"].(string); ok && strings.TrimSpace(instructions) != "" {
		messages = append([]any{map[string]any{"role": "system", "content": strings.TrimSpace(instructions)}}, messages...)
	}
	addOpenCodeMessageIDs(messages)
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
	if reasoning, ok := request["reasoning"].(map[string]any); ok {
		if effort, ok := reasoning["effort"].(string); ok && strings.TrimSpace(effort) != "" {
			payload["reasoning_effort"] = strings.TrimSpace(effort)
		}
	}
	if stream, _ := request["stream"].(bool); stream {
		payload["stream_options"] = map[string]any{"include_usage": true}
	}
	if tools, ok := request["tools"].([]any); ok {
		converted, names, err := normalizeChatTools(tools, &context)
		if err != nil {
			return nil, context, err
		}
		if len(converted) > 0 {
			payload["tools"] = converted
			if choice := chatToolChoiceForNames(request["tool_choice"], names); choice != nil {
				payload["tool_choice"] = choice
			}
		}
	}
	converted, err := json.Marshal(payload)
	return converted, context, err
}

func addOpenCodeMessageIDs(messages []any) {
	for index, raw := range messages {
		message, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if message["role"] == "tool" {
			callID := stringValue(message["tool_call_id"])
			if callID != "" {
				message["id"] = "tool_" + callID
				continue
			}
			message["id"] = "tool"
			continue
		}
		message["id"] = fmt.Sprintf("msg_%d", index)
	}
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
	if !ok {
		return nil
	}
	if choice["type"] == "function" {
		function, _ := choice["function"].(map[string]any)
		name := stringValue(function["name"])
		if name == "" {
			name = stringValue(choice["name"])
		}
		if !isValidChatFunctionName(name) {
			return nil
		}
		return map[string]any{"type": "function", "function": map[string]any{"name": name}}
	}
	name := normalizeChatFunctionName(stringValue(choice["type"]))
	if !isValidChatFunctionName(name) {
		return nil
	}
	return map[string]any{"type": "function", "function": map[string]any{"name": name}}
}

func chatToolChoiceForNames(value any, names map[string]bool) any {
	choice := chatToolChoice(value)
	if choice == nil {
		return nil
	}
	if object, ok := choice.(map[string]any); ok {
		if function, ok := object["function"].(map[string]any); ok && !names[stringValue(function["name"])] {
			return nil
		}
	}
	return choice
}

func normalizeChatTools(tools []any, context *responsesBridgeContext) ([]any, map[string]bool, error) {
	converted := make([]any, 0, len(tools))
	names := map[string]bool{}
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		toolType := stringValue(tool["type"])
		if toolType == "custom" {
			name := strings.TrimSpace(stringValue(tool["name"]))
			if !isValidChatFunctionName(name) || names[name] {
				continue
			}
			names[name] = true
			if context != nil {
				context.customTools[name] = true
				context.declaredTools[name] = true
			}
			clean := map[string]any{"name": name, "parameters": customToolParameters()}
			if description, exists := tool["description"]; exists {
				clean["description"] = description
			}
			converted = append(converted, map[string]any{"type": "function", "function": clean})
			continue
		}
		function := tool
		if toolType == "function" {
			if nested, ok := tool["function"].(map[string]any); ok {
				function = nested
			}
		} else {
			name := normalizeChatFunctionName(toolType)
			if name == "" {
				continue
			}
			function = tool
			function["name"] = name
		}
		name, _ := function["name"].(string)
		name = strings.TrimSpace(name)
		if !isValidChatFunctionName(name) {
			continue
		}
		if names[name] {
			continue
		}
		names[name] = true
		if context != nil {
			context.declaredTools[name] = true
		}
		clean := map[string]any{"name": name}
		for _, key := range []string{"description", "parameters", "strict"} {
			if value, exists := function[key]; exists {
				clean[key] = value
			}
		}
		converted = append(converted, map[string]any{"type": "function", "function": clean})
	}
	return converted, names, nil
}

func isValidChatFunctionName(value string) bool {
	if len(value) < 1 || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_' || char == '-') {
			return false
		}
	}
	return true
}

func normalizeChatFunctionName(value string) string {
	var builder strings.Builder
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_' || char == '-' {
			builder.WriteRune(char)
		} else if builder.Len() > 0 {
			builder.WriteByte('_')
		}
	}
	return strings.Trim(builder.String(), "_")[:minInt(len(strings.Trim(builder.String(), "_")), 64)]
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func normalizeChatToolArguments(value any) string {
	if text, ok := value.(string); ok {
		var parsed any
		if json.Unmarshal([]byte(text), &parsed) == nil {
			encoded, _ := json.Marshal(parsed)
			return string(encoded)
		}
		return text
	}
	if value == nil {
		return ""
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
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
	pendingCalls := map[string]any{}
	pendingCallOrder := make([]string, 0)
	pendingOutputs := map[string]any{}
	pendingOutputOrder := make([]string, 0)
	pendingReasoning := ""
	seenCalls := map[string]bool{}
	seenOutputs := map[string]bool{}
	flushPending := func() {
		matched := make([]any, 0, len(pendingCallOrder))
		emitted := map[string]bool{}
		for _, callID := range pendingCallOrder {
			if _, exists := pendingOutputs[callID]; exists {
				matched = append(matched, pendingCalls[callID])
				emitted[callID] = true
			}
		}
		if len(matched) > 0 {
			assistant := map[string]any{"role": "assistant", "content": nil, "tool_calls": matched}
			if pendingReasoning != "" {
				assistant["reasoning_content"] = pendingReasoning
			}
			messages = append(messages, assistant)
			for _, callID := range pendingCallOrder {
				if emitted[callID] {
					messages = append(messages, pendingOutputs[callID])
				}
			}
		} else if pendingReasoning != "" {
			messages = append(messages, map[string]any{"role": "assistant", "content": "", "reasoning_content": pendingReasoning})
		}
		for _, callID := range pendingOutputOrder {
			if !emitted[callID] {
				if output, exists := pendingOutputs[callID]; exists {
					messages = append(messages, map[string]any{"role": "user", "content": "Tool output for " + callID + ":\n" + stringValue(output.(map[string]any)["content"])})
				}
			}
		}
		pendingCalls = map[string]any{}
		pendingCallOrder = nil
		pendingOutputs = map[string]any{}
		pendingOutputOrder = nil
		pendingReasoning = ""
	}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Responses input item must be an object")
		}
		itemType, _ := item["type"].(string)
		switch itemType {
		case "function_call_output", "custom_tool_call_output":
			callID, _ := item["call_id"].(string)
			if callID == "" {
				return nil, fmt.Errorf("function_call_output call_id is required")
			}
			if seenOutputs[callID] {
				continue
			}
			seenOutputs[callID] = true
			pendingOutputs[callID] = map[string]any{"role": "tool", "tool_call_id": callID, "content": responsesText(item["output"])}
			pendingOutputOrder = append(pendingOutputOrder, callID)
		case "function_call", "custom_tool_call":
			callID, _ := item["call_id"].(string)
			name, _ := item["name"].(string)
			if callID == "" || name == "" {
				return nil, fmt.Errorf("function_call name and call_id are required")
			}
			if seenCalls[callID] {
				continue
			}
			seenCalls[callID] = true
			if !isValidChatFunctionName(name) {
				continue
			}
			arguments := normalizeChatToolArguments(item["arguments"])
			if itemType == "custom_tool_call" {
				arguments = wrapCustomToolInput(stringValue(item["input"]))
			}
			pendingCalls[callID] = map[string]any{"id": callID, "type": "function", "function": map[string]any{"name": name, "arguments": arguments}}
			pendingCallOrder = append(pendingCallOrder, callID)
		case "reasoning":
			pendingReasoning += responsesReasoningText(item)
		default:
			if toolCalls, ok := item["tool_calls"].([]any); ok && normalizeChatRole(stringValue(item["role"])) == "assistant" {
				for _, rawCall := range toolCalls {
					call, ok := rawCall.(map[string]any)
					if !ok {
						continue
					}
					callID := stringValue(call["id"])
					function, _ := call["function"].(map[string]any)
					if callID == "" || seenCalls[callID] {
						continue
					}
					seenCalls[callID] = true
					name := stringValue(function["name"])
					if !isValidChatFunctionName(name) {
						continue
					}
					pendingCalls[callID] = map[string]any{"id": callID, "type": "function", "function": map[string]any{"name": name, "arguments": normalizeChatToolArguments(function["arguments"])}}
					pendingCallOrder = append(pendingCallOrder, callID)
				}
				pendingReasoning += stringValue(item["reasoning_content"])
				continue
			}
			if normalizeChatRole(stringValue(item["role"])) == "tool" {
				callID := stringValue(item["tool_call_id"])
				if callID != "" && !seenOutputs[callID] {
					seenOutputs[callID] = true
					pendingOutputs[callID] = map[string]any{"role": "tool", "tool_call_id": callID, "content": responsesText(item["content"])}
					pendingOutputOrder = append(pendingOutputOrder, callID)
				}
				continue
			}
			if normalizeChatRole(stringValue(item["role"])) == "assistant" && stringValue(item["content"]) == "" && stringValue(item["reasoning_content"]) != "" {
				pendingReasoning += stringValue(item["reasoning_content"])
				continue
			}
			flushPending()
			role, _ := item["role"].(string)
			role = normalizeChatRole(role)
			content := responsesContent(item["content"])
			if content != "" {
				message := map[string]any{"role": role, "content": content}
				if role == "assistant" {
					if reasoning := stringValue(item["reasoning_content"]); reasoning != "" {
						message["reasoning_content"] = reasoning
					}
				}
				messages = append(messages, message)
			}
		}
	}
	flushPending()
	return messages, nil
}

func responsesReasoningText(item map[string]any) string {
	if text := responsesText(item["content"]); text != "" {
		return text
	}
	return responsesText(item["summary"])
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
		if value == nil {
			return ""
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return ""
		}
		return string(encoded)
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

func responsesContent(value any) any {
	if text, ok := value.(string); ok {
		return text
	}
	parts, ok := value.([]any)
	if !ok {
		return responsesText(value)
	}
	content := make([]any, 0, len(parts))
	for _, raw := range parts {
		part, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := part["type"].(string)
		switch typ {
		case "input_text", "output_text", "text":
			if text, ok := part["text"].(string); ok {
				content = append(content, map[string]any{"type": "text", "text": text})
			}
		case "input_image":
			imageURL := part["image_url"]
			if image, ok := imageURL.(map[string]any); ok {
				if url, ok := image["url"].(string); ok {
					content = append(content, map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}})
				}
			} else if url, ok := imageURL.(string); ok {
				content = append(content, map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}})
			}
		}
	}
	if len(content) == 0 {
		return ""
	}
	return content
}

func extractCustomToolInput(arguments string) string {
	var wrapped map[string]any
	if json.Unmarshal([]byte(arguments), &wrapped) == nil {
		if input, ok := wrapped["input"].(string); ok {
			return input
		}
	}
	return arguments
}

func chatCompletionToResponses(body []byte) ([]byte, error) {
	return chatCompletionToResponsesWithContext(body, newResponsesBridgeContext())
}

func chatCompletionToResponsesWithContext(body []byte, context responsesBridgeContext) ([]byte, error) {
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
				if reasoning := chatReasoningText(message["reasoning_content"]); reasoning != "" {
					output = append(output, responseReasoning(responseID, reasoning))
				}
				if content := chatContentText(message["content"]); content != "" {
					output = append(output, responseMessage(responseID, content))
				}
				if calls, ok := message["tool_calls"].([]any); ok {
					for _, raw := range calls {
						if call, ok := raw.(map[string]any); ok {
							function, _ := call["function"].(map[string]any)
							callID := stringValue(call["id"])
							if callID == "" {
								callID = "call_" + fmt.Sprint(time.Now().UnixNano())
							}
							name := stringValue(function["name"])
							arguments := stringValue(function["arguments"])
							if context.customTools[name] {
								output = append(output, map[string]any{"id": functionItemID(callID), "type": "custom_tool_call", "status": "completed", "call_id": callID, "name": name, "input": extractCustomToolInput(arguments)})
								continue
							}
							output = append(output, map[string]any{"id": functionItemID(callID), "type": "function_call", "status": "completed", "call_id": callID, "name": name, "arguments": arguments})
						}
					}
				}
			}
		}
	}
	result := map[string]any{"id": responseID, "object": "response", "created_at": intValue(chat["created"]), "status": "completed", "model": stringValue(chat["model"]), "output": output}
	if usage, ok := chat["usage"].(map[string]any); ok {
		convertedUsage := map[string]any{"input_tokens": usage["prompt_tokens"], "output_tokens": usage["completion_tokens"], "total_tokens": usage["total_tokens"]}
		if details, ok := usage["prompt_tokens_details"].(map[string]any); ok {
			convertedUsage["input_tokens_details"] = details
		}
		if details, ok := usage["completion_tokens_details"].(map[string]any); ok {
			convertedUsage["output_tokens_details"] = details
		}
		result["usage"] = convertedUsage
	}
	return json.Marshal(result)
}

func responseMessage(responseID, text string) map[string]any {
	return map[string]any{"id": "msg_" + responseID, "type": "message", "status": "completed", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}}}
}
func responseReasoning(responseID, text string) map[string]any {
	return map[string]any{"id": "rs_" + responseID, "type": "reasoning", "status": "completed", "summary": []any{}, "content": []any{map[string]any{"type": "reasoning_text", "text": text}}}
}
func functionItemID(callID string) string {
	if strings.HasPrefix(callID, "call_") {
		return "fc_" + strings.TrimPrefix(callID, "call_")
	}
	return "fc_" + callID
}
func chatReasoningText(value any) string {
	return chatContentText(value)
}
func chatContentText(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	if parts, ok := value.([]any); ok {
		texts := make([]string, 0, len(parts))
		for _, part := range parts {
			texts = append(texts, chatContentText(part))
		}
		return strings.Join(texts, "")
	}
	if part, ok := value.(map[string]any); ok {
		if text, ok := part["text"]; ok {
			return chatContentText(text)
		}
		if content, ok := part["content"]; ok {
			return chatContentText(content)
		}
	}
	return ""
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
	createdAt         int64
	created           bool
	textStarted       bool
	text              string
	textIndex         int
	reasoningStarted  bool
	reasoning         string
	reasoningIndex    int
	completed         bool
	nextIndex         int
	usage             map[string]any
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
	s.createdAt = intValue(payload["created"])
	s.textIndex = -1
	s.reasoningIndex = -1
	s.created = true
	s.emit(w, "response.created", map[string]any{"type": "response.created", "response": map[string]any{"id": s.responseID, "object": "response", "created_at": s.createdAt, "status": "in_progress", "model": s.model, "output": []any{}}})
}
func (s *responseStreamState) consume(payload map[string]any, w *bytes.Buffer) {
	s.start(payload, w)
	if usage, ok := payload["usage"].(map[string]any); ok {
		s.usage = usage
	}
	choices, _ := payload["choices"].([]any)
	if len(choices) == 0 {
		return
	}
	choice, _ := choices[0].(map[string]any)
	delta, _ := choice["delta"].(map[string]any)
	if reasoning := chatReasoningText(delta["reasoning_content"]); reasoning != "" {
		if !s.reasoningStarted {
			s.reasoningStarted = true
			s.reasoningIndex = s.nextIndex
			s.nextIndex++
			s.emit(w, "response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": s.reasoningIndex, "item": map[string]any{"id": "rs_" + s.responseID, "type": "reasoning", "status": "in_progress", "summary": []any{}, "content": []any{}}})
		}
		s.reasoning += reasoning
		s.emit(w, "response.reasoning_text.delta", map[string]any{"type": "response.reasoning_text.delta", "output_index": s.reasoningIndex, "content_index": 0, "item_id": "rs_" + s.responseID, "delta": reasoning})
	}
	if text := responsesText(delta["content"]); text != "" {
		if !s.textStarted {
			s.textStarted = true
			s.textIndex = s.nextIndex
			s.emit(w, "response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": s.textIndex, "item": map[string]any{"id": "msg_" + s.responseID, "type": "message", "status": "in_progress", "role": "assistant", "content": []any{}}})
			s.emit(w, "response.content_part.added", map[string]any{"type": "response.content_part.added", "output_index": s.textIndex, "content_index": 0, "item_id": "msg_" + s.responseID, "part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}}})
			s.nextIndex++
		}
		s.text += text
		s.emit(w, "response.output_text.delta", map[string]any{"type": "response.output_text.delta", "output_index": s.textIndex, "content_index": 0, "item_id": "msg_" + s.responseID, "delta": text})
	}
	if calls, ok := delta["tool_calls"].([]any); ok {
		for _, raw := range calls {
			call, _ := raw.(map[string]any)
			index := numericIndex(call["index"], len(s.calls))
			item := s.calls[index]
			if item == nil {
				callID := stringValue(call["id"])
				if callID == "" {
					callID = "call_" + fmt.Sprint(time.Now().UnixNano())
				}
				item = &responseToolCall{callID: callID, outputIndex: s.nextIndex}
				s.calls[index] = item
				s.nextIndex++
				s.emit(w, "response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": item.outputIndex, "item": map[string]any{"id": functionItemID(item.callID), "type": "function_call", "status": "in_progress", "call_id": item.callID, "name": "", "arguments": ""}})
			}
			function, _ := call["function"].(map[string]any)
			if name := stringValue(function["name"]); name != "" {
				item.name += name
			}
			if args := stringValue(function["arguments"]); args != "" {
				item.arguments += args
				s.emit(w, "response.function_call_arguments.delta", map[string]any{"type": "response.function_call_arguments.delta", "output_index": item.outputIndex, "item_id": functionItemID(item.callID), "delta": args})
			}
		}
	}
}
func (s *responseStreamState) finish(w *bytes.Buffer) {
	if !s.created || s.completed {
		return
	}
	s.completed = true
	if s.reasoningStarted {
		s.emit(w, "response.reasoning_text.done", map[string]any{"type": "response.reasoning_text.done", "output_index": s.reasoningIndex, "content_index": 0, "item_id": "rs_" + s.responseID, "text": s.reasoning})
		s.emit(w, "response.content_part.done", map[string]any{"type": "response.content_part.done", "output_index": s.reasoningIndex, "content_index": 0, "item_id": "rs_" + s.responseID, "part": map[string]any{"type": "reasoning_text", "text": s.reasoning}})
		s.emit(w, "response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": s.reasoningIndex, "item": responseReasoning(s.responseID, s.reasoning)})
	}
	callList := make([]*responseToolCall, 0, len(s.calls))
	for _, call := range s.calls {
		callList = append(callList, call)
	}
	sort.Slice(callList, func(i, j int) bool { return callList[i].outputIndex < callList[j].outputIndex })
	for _, call := range callList {
		itemID := functionItemID(call.callID)
		s.emit(w, "response.function_call_arguments.done", map[string]any{"type": "response.function_call_arguments.done", "output_index": call.outputIndex, "item_id": itemID, "arguments": call.arguments})
		s.emit(w, "response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": call.outputIndex, "item": map[string]any{"id": itemID, "type": "function_call", "status": "completed", "call_id": call.callID, "name": call.name, "arguments": call.arguments}})
	}
	if s.textStarted {
		message := responseMessage(s.responseID, s.text)
		s.emit(w, "response.output_text.done", map[string]any{"type": "response.output_text.done", "output_index": s.textIndex, "content_index": 0, "item_id": "msg_" + s.responseID, "text": s.text})
		s.emit(w, "response.content_part.done", map[string]any{"type": "response.content_part.done", "output_index": s.textIndex, "content_index": 0, "item_id": "msg_" + s.responseID, "part": message["content"].([]any)[0]})
		s.emit(w, "response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": s.textIndex, "item": message})
	}
	output := make([]any, 0, len(callList)+2)
	if s.reasoningStarted {
		output = append(output, responseReasoning(s.responseID, s.reasoning))
	}
	if s.textStarted {
		output = append(output, responseMessage(s.responseID, s.text))
	}
	for _, call := range callList {
		output = append(output, map[string]any{"id": functionItemID(call.callID), "type": "function_call", "status": "completed", "call_id": call.callID, "name": call.name, "arguments": call.arguments})
	}
	sort.SliceStable(output, func(i, j int) bool { return outputItemIndex(output[i], s) < outputItemIndex(output[j], s) })
	response := map[string]any{"id": s.responseID, "object": "response", "created_at": s.createdAt, "status": "completed", "model": s.model, "output": output}
	if s.usage != nil {
		response["usage"] = map[string]any{"input_tokens": s.usage["prompt_tokens"], "output_tokens": s.usage["completion_tokens"], "total_tokens": s.usage["total_tokens"]}
	}
	s.emit(w, "response.completed", map[string]any{"type": "response.completed", "response": response})
	w.WriteString("data: [DONE]\n\n")
}

func numericIndex(value any, fallback int) int {
	switch number := value.(type) {
	case float64:
		return int(number)
	case int:
		return number
	default:
		return fallback
	}
}

func outputItemIndex(value any, state *responseStreamState) int {
	item, _ := value.(map[string]any)
	switch item["type"] {
	case "reasoning":
		return state.reasoningIndex
	case "message":
		return state.textIndex
	default:
		callID := stringValue(item["call_id"])
		for _, call := range state.calls {
			if call.callID == callID {
				return call.outputIndex
			}
		}
		return state.nextIndex
	}
}
