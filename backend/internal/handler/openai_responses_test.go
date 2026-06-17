package handler

import (
	"strings"
	"testing"

	"notaion2api/backend/internal/service"

	"github.com/gin-gonic/gin"
	"net/http/httptest"
)

func TestStreamResponseEmitsResponsesLifecycleEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	result := service.CompletionResult{
		Model: "gpt-test",
		Text:  "connected",
		Payload: map[string]any{
			"id":           "resp_test",
			"object":       "response",
			"created_at":   int64(123),
			"status":       "completed",
			"model":        "gpt-test",
			"output_text":  "connected",
			"completed_at": int64(123),
			"output": []any{
				map[string]any{
					"id":     "msg_test",
					"type":   "message",
					"status": "completed",
					"role":   "assistant",
					"content": []any{
						map[string]any{"type": "output_text", "text": "connected", "annotations": []any{}},
					},
				},
			},
		},
	}

	(&Handler{}).streamResponse(ctx, result)
	body := recorder.Body.String()
	for _, event := range []string{
		"response.created",
		"response.in_progress",
		"response.output_item.added",
		"response.content_part.added",
		"response.output_text.delta",
		"response.output_text.done",
		"response.content_part.done",
		"response.output_item.done",
		"response.completed",
	} {
		if !strings.Contains(body, "event: "+event+"\n") {
			t.Fatalf("missing event %s in stream:\n%s", event, body)
		}
	}
	if !strings.Contains(body, `"sequence_number":0`) || !strings.Contains(body, `"sequence_number":8`) {
		t.Fatalf("expected sequence numbers in stream:\n%s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("expected done sentinel in stream:\n%s", body)
	}
}

func TestStreamResponseEmitsFunctionCallEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	result := service.CompletionResult{
		Model: "gpt-test",
		Payload: map[string]any{
			"id":           "resp_test",
			"object":       "response",
			"created_at":   int64(123),
			"status":       "completed",
			"model":        "gpt-test",
			"output_text":  "",
			"completed_at": int64(123),
			"output": []any{
				map[string]any{
					"id":        "fc_test",
					"type":      "function_call",
					"status":    "completed",
					"call_id":   "call_test",
					"name":      "exec_command",
					"arguments": `{"cmd":"sed -n '1,400p' 'Dockerfile'"}`,
				},
			},
		},
	}

	(&Handler{}).streamResponse(ctx, result)
	body := recorder.Body.String()
	for _, event := range []string{
		"response.created",
		"response.in_progress",
		"response.output_item.added",
		"response.function_call_arguments.delta",
		"response.function_call_arguments.done",
		"response.output_item.done",
		"response.completed",
	} {
		if !strings.Contains(body, "event: "+event+"\n") {
			t.Fatalf("missing event %s in stream:\n%s", event, body)
		}
	}
	if strings.Contains(body, "response.content_part.added") || strings.Contains(body, "response.output_text.delta") {
		t.Fatalf("function call stream should not emit text content events:\n%s", body)
	}
	if !strings.Contains(body, `"name":"exec_command"`) || !strings.Contains(body, `"sequence_number":6`) {
		t.Fatalf("expected function call payload and sequence numbers in stream:\n%s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("expected done sentinel in stream:\n%s", body)
	}
}
