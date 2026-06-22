package dto

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ResponsesRequest is intentionally permissive for Codex/OpenAI Responses API.
// Codex may send fields this bridge does not use; handlers should ignore
// unknown fields instead of rejecting the request.
type ResponsesRequest struct {
	Model              string          `json:"model"`
	Input              json.RawMessage `json:"input"`
	Instructions       string          `json:"instructions,omitempty"`
	Stream             bool            `json:"stream,omitempty"`
	PreviousResponseID string          `json:"previous_response_id,omitempty"`
	Tools              json.RawMessage `json:"tools,omitempty"`
	ToolChoice          any             `json:"tool_choice,omitempty"`
	ParallelToolCalls   *bool           `json:"parallel_tool_calls,omitempty"`
	Reasoning           json.RawMessage `json:"reasoning,omitempty"`
	Text                json.RawMessage `json:"text,omitempty"`
	Metadata            map[string]any  `json:"metadata,omitempty"`
	Temperature         *float64        `json:"temperature,omitempty"`
	TopP                *float64        `json:"top_p,omitempty"`
	MaxOutputTokens     *int            `json:"max_output_tokens,omitempty"`
	MaxTokens           *int            `json:"max_tokens,omitempty"`
}

type ResponseObject struct {
	ID         string               `json:"id"`
	Object     string               `json:"object"`
	CreatedAt  int64                `json:"created_at"`
	Status     string               `json:"status"`
	Model      string               `json:"model"`
	Output     []ResponseOutputItem `json:"output"`
	OutputText string               `json:"output_text,omitempty"`
	Usage      *ResponseUsage       `json:"usage,omitempty"`
	Metadata   map[string]any       `json:"metadata,omitempty"`
}

type ResponseOutputItem struct {
	ID      string                `json:"id"`
	Type    string                `json:"type"`
	Status  string                `json:"status,omitempty"`
	Role    string                `json:"role,omitempty"`
	Content []ResponseContentPart `json:"content,omitempty"`

	// function_call fields
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type ResponseContentPart struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	Annotations []any  `json:"annotations"`
}

type ResponseUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type OpenAIErrorResponse struct {
	Error OpenAIError `json:"error"`
}

type OpenAIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   any    `json:"param"`
	Code    string `json:"code,omitempty"`
}

func NewOpenAIError(message, typ string, param any, code string) OpenAIErrorResponse {
	if typ == "" {
		typ = "invalid_request_error"
	}
	return OpenAIErrorResponse{
		Error: OpenAIError{
			Message: message,
			Type:    typ,
			Param:   param,
			Code:    code,
		},
	}
}

func NewTextResponse(id, model, text string, usage *ResponseUsage) ResponseObject {
	now := time.Now().Unix()
	if id == "" {
		id = fmt.Sprintf("resp_%d", time.Now().UnixNano())
	}
	if model == "" {
		model = "auto"
	}

	part := ResponseContentPart{
		Type:        "output_text",
		Text:        text,
		Annotations: []any{},
	}

	item := ResponseOutputItem{
		ID:      fmt.Sprintf("msg_%d", time.Now().UnixNano()),
		Type:    "message",
		Status:  "completed",
		Role:    "assistant",
		Content: []ResponseContentPart{part},
	}

	return ResponseObject{
		ID:         id,
		Object:     "response",
		CreatedAt:  now,
		Status:     "completed",
		Model:      model,
		Output:     []ResponseOutputItem{item},
		OutputText: text,
		Usage:      usage,
	}
}

func NewFunctionCallResponse(id, model, callID, name, arguments string, usage *ResponseUsage) ResponseObject {
	now := time.Now().Unix()
	if id == "" {
		id = fmt.Sprintf("resp_%d", time.Now().UnixNano())
	}
	if callID == "" {
		callID = fmt.Sprintf("call_%d", time.Now().UnixNano())
	}
	if model == "" {
		model = "auto"
	}
	if arguments == "" {
		arguments = "{}"
	}

	item := ResponseOutputItem{
		ID:        fmt.Sprintf("fc_%d", time.Now().UnixNano()),
		Type:      "function_call",
		Status:    "completed",
		CallID:    callID,
		Name:      name,
		Arguments: arguments,
	}

	return ResponseObject{
		ID:        id,
		Object:    "response",
		CreatedAt: now,
		Status:    "completed",
		Model:     model,
		Output:    []ResponseOutputItem{item},
		Usage:     usage,
	}
}

// ResponsesInputToPrompt converts Codex/OpenAI Responses input variants into
// a plain prompt for the existing upstream path.
//
// Supported examples:
//   "hello"
//   [{"role":"user","content":"hello"}]
//   [{"role":"user","content":[{"type":"input_text","text":"hello"}]}]
//   [{"type":"function_call_output","call_id":"call_x","output":"..."}]
func ResponsesInputToPrompt(req ResponsesRequest) (string, error) {
	var parts []string

	if strings.TrimSpace(req.Instructions) != "" {
		parts = append(parts, "Instructions:\n"+strings.TrimSpace(req.Instructions))
	}

	input := strings.TrimSpace(string(req.Input))
	if input == "" || input == "null" {
		return strings.Join(parts, "\n\n"), nil
	}

	var s string
	if err := json.Unmarshal(req.Input, &s); err == nil {
		if strings.TrimSpace(s) != "" {
			parts = append(parts, strings.TrimSpace(s))
		}
		return strings.Join(parts, "\n\n"), nil
	}

	var arr []map[string]any
	if err := json.Unmarshal(req.Input, &arr); err != nil {
		return "", fmt.Errorf("unsupported responses input format")
	}

	for _, item := range arr {
		itemType, _ := item["type"].(string)
		role, _ := item["role"].(string)

		if itemType == "function_call_output" {
			callID, _ := item["call_id"].(string)
			output := stringifyAny(item["output"])
			if strings.TrimSpace(output) == "" {
				continue
			}
			if callID != "" {
				parts = append(parts, fmt.Sprintf("Tool result (%s):\n%s", callID, output))
			} else {
				parts = append(parts, "Tool result:\n"+output)
			}
			continue
		}

		contentText := responsesContentToText(item["content"])
		if strings.TrimSpace(contentText) == "" {
			continue
		}

		if role == "" {
			role = itemType
		}
		if role != "" {
			parts = append(parts, fmt.Sprintf("%s:\n%s", role, contentText))
		} else {
			parts = append(parts, contentText)
		}
	}

	return strings.Join(parts, "\n\n"), nil
}

func responsesContentToText(v any) string {
	switch c := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(c)
	case []any:
		var parts []string
		for _, part := range c {
			m, ok := part.(map[string]any)
			if !ok {
				txt := stringifyAny(part)
				if strings.TrimSpace(txt) != "" {
					parts = append(parts, strings.TrimSpace(txt))
				}
				continue
			}

			partType, _ := m["type"].(string)
			switch partType {
			case "input_text", "output_text", "text":
				if txt, _ := m["text"].(string); strings.TrimSpace(txt) != "" {
					parts = append(parts, strings.TrimSpace(txt))
				}
			case "input_image":
				parts = append(parts, "[input_image]")
			default:
				txt := stringifyAny(m)
				if strings.TrimSpace(txt) != "" {
					parts = append(parts, strings.TrimSpace(txt))
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return stringifyAny(v)
	}
}

func stringifyAny(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return fmt.Sprint(x)
		}
		return string(b)
	}
}

func NormalizeResponseUsage(inputTokens, outputTokens int) *ResponseUsage {
	if inputTokens < 0 {
		inputTokens = 0
	}
	if outputTokens < 0 {
		outputTokens = 0
	}
	return &ResponseUsage{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalTokens:  inputTokens + outputTokens,
	}
}
