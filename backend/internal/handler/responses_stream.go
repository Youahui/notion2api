package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"notion2api/backend/internal/dto"
)

type responsesStreamEvent struct {
	Type         string                   `json:"type"`
	Response     *dto.ResponseObject      `json:"response,omitempty"`
	Item         *dto.ResponseOutputItem  `json:"item,omitempty"`
	OutputIndex  int                      `json:"output_index,omitempty"`
	ContentIndex int                      `json:"content_index,omitempty"`
	Part         *dto.ResponseContentPart `json:"part,omitempty"`
	Delta        string                   `json:"delta,omitempty"`
	Text         string                   `json:"text,omitempty"`
	Error        any                      `json:"error,omitempty"`
}

// writeResponsesTextStream writes OpenAI Responses API compatible SSE events.
// Do not reuse chat.completion.chunk for /v1/responses stream=true.
func writeResponsesTextStream(c *gin.Context, model string, chunks <-chan string, usage *dto.ResponseUsage) {
	respID := fmt.Sprintf("resp_%d", time.Now().UnixNano())
	msgID := fmt.Sprintf("msg_%d", time.Now().UnixNano())
	createdAt := time.Now().Unix()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	baseResp := dto.ResponseObject{
		ID:        respID,
		Object:    "response",
		CreatedAt: createdAt,
		Status:    "in_progress",
		Model:     model,
		Output:    []dto.ResponseOutputItem{},
	}

	writeResponsesSSE(c, "response.created", responsesStreamEvent{
		Type:     "response.created",
		Response: &baseResp,
	})
	writeResponsesSSE(c, "response.in_progress", responsesStreamEvent{
		Type:     "response.in_progress",
		Response: &baseResp,
	})

	item := dto.ResponseOutputItem{
		ID:      msgID,
		Type:    "message",
		Status:  "in_progress",
		Role:    "assistant",
		Content: []dto.ResponseContentPart{},
	}

	writeResponsesSSE(c, "response.output_item.added", responsesStreamEvent{
		Type:        "response.output_item.added",
		OutputIndex: 0,
		Item:        &item,
	})

	part := dto.ResponseContentPart{
		Type:        "output_text",
		Text:        "",
		Annotations: []any{},
	}

	writeResponsesSSE(c, "response.content_part.added", responsesStreamEvent{
		Type:         "response.content_part.added",
		OutputIndex:  0,
		ContentIndex: 0,
		Part:         &part,
	})

	var full strings.Builder
	for chunk := range chunks {
		if chunk == "" {
			continue
		}
		full.WriteString(chunk)
		writeResponsesSSE(c, "response.output_text.delta", responsesStreamEvent{
			Type:         "response.output_text.delta",
			OutputIndex:  0,
			ContentIndex: 0,
			Delta:        chunk,
		})
	}

	text := full.String()
	writeResponsesSSE(c, "response.output_text.done", responsesStreamEvent{
		Type:         "response.output_text.done",
		OutputIndex:  0,
		ContentIndex: 0,
		Text:         text,
	})

	donePart := dto.ResponseContentPart{
		Type:        "output_text",
		Text:        text,
		Annotations: []any{},
	}

	writeResponsesSSE(c, "response.content_part.done", responsesStreamEvent{
		Type:         "response.content_part.done",
		OutputIndex:  0,
		ContentIndex: 0,
		Part:         &donePart,
	})

	doneItem := dto.ResponseOutputItem{
		ID:     msgID,
		Type:   "message",
		Status: "completed",
		Role:   "assistant",
		Content: []dto.ResponseContentPart{
			donePart,
		},
	}

	writeResponsesSSE(c, "response.output_item.done", responsesStreamEvent{
		Type:        "response.output_item.done",
		OutputIndex: 0,
		Item:        &doneItem,
	})

	finalResp := dto.ResponseObject{
		ID:         respID,
		Object:     "response",
		CreatedAt:  createdAt,
		Status:     "completed",
		Model:      model,
		OutputText: text,
		Usage:      usage,
		Output: []dto.ResponseOutputItem{
			doneItem,
		},
	}

	writeResponsesSSE(c, "response.completed", responsesStreamEvent{
		Type:     "response.completed",
		Response: &finalResp,
	})

	_, _ = c.Writer.Write([]byte("data: [DONE]\n\n"))
	c.Writer.Flush()
}

func writeResponsesErrorStream(c *gin.Context, status int, errResp dto.OpenAIErrorResponse) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(status)

	writeResponsesSSE(c, "error", responsesStreamEvent{
		Type:  "error",
		Error: errResp.Error,
	})
	_, _ = c.Writer.Write([]byte("data: [DONE]\n\n"))
	c.Writer.Flush()
}

func writeResponsesSSE(c *gin.Context, event string, payload any) {
	b, err := json.Marshal(payload)
	if err != nil {
		b = []byte(`{"type":"error","error":{"message":"failed to marshal SSE payload","type":"server_error","param":null,"code":"marshal_error"}}`)
		event = "error"
	}
	_, _ = c.Writer.Write([]byte("event: " + event + "\n"))
	_, _ = c.Writer.Write([]byte("data: "))
	_, _ = c.Writer.Write(b)
	_, _ = c.Writer.Write([]byte("\n\n"))
	c.Writer.Flush()
}
