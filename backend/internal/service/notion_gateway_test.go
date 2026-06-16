package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"notaion2api/backend/internal/config"
	"notaion2api/backend/internal/model"
)

func TestExtractNotionTextFromAgentInferenceEvent(t *testing.T) {
	want := `{"titles":[{"title":"[FULL] a long title","reason":"valid json"}]}`
	raw := notionTestLines(t, map[string]any{
		"type": "agent-inference",
		"id":   "assistant-1",
		"value": []any{
			map[string]any{"type": "text", "content": want},
		},
		"finishedAt": "2026-06-16T08:42:29.000Z",
	})

	got, messageID := extractNotionText([]byte(raw), "thread-1")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if messageID != "assistant-1" {
		t.Fatalf("expected assistant message id, got %q", messageID)
	}
}

func TestNotionTranscriptStateDetectsTerminalAgentInference(t *testing.T) {
	want := `{"ok":true,"message":"connected"}`
	state := &notionTranscriptState{ActiveAgentIndex: -1}
	text, messageID := state.handlePayload(map[string]any{
		"type": "agent-inference",
		"id":   "assistant-1",
		"value": []any{
			map[string]any{"type": "text", "content": want},
		},
		"finishedAt": "2026-06-16T08:42:29.000Z",
	}, "thread-1")
	if text != want {
		t.Fatalf("expected %q, got %q", want, text)
	}
	if messageID != "assistant-1" {
		t.Fatalf("expected assistant message id, got %q", messageID)
	}
	if !state.hasTerminalAnswer() {
		t.Fatal("expected terminal answer")
	}
}

func TestExtractNotionTextFromPatchStream(t *testing.T) {
	want := `{"titles":[{"title":"[FULL] a long title","reason":"valid json"}]}`
	raw := notionTestLines(t,
		map[string]any{
			"type": "patch-start",
			"data": map[string]any{
				"s": []any{
					map[string]any{"id": "user-1", "type": "user", "value": []any{[]any{"user prompt must not be returned"}}},
					map[string]any{"id": "assistant-1", "type": "agent-inference", "value": []any{map[string]any{"type": "text", "content": ""}}},
				},
			},
		},
		map[string]any{
			"type": "patch",
			"v": []any{
				map[string]any{"o": "x", "p": "/s/1/value/0/content", "v": `{"titles":`},
			},
		},
		map[string]any{
			"type": "patch",
			"v": []any{
				map[string]any{"o": "x", "p": "/s/1/value/0/content", "v": `[{"title":"[FULL] a long title","reason":"valid json"}]}`},
			},
		},
		map[string]any{
			"type": "patch",
			"v": []any{
				map[string]any{"o": "a", "p": "/s/1/finishedAt", "v": "2026-06-16T08:42:29.000Z"},
			},
		},
	)

	got, messageID := extractNotionText([]byte(raw), "thread-1")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if messageID != "assistant-1" {
		t.Fatalf("expected assistant message id, got %q", messageID)
	}
}

func TestExtractNotionTextFromRecordMap(t *testing.T) {
	want := `{"titles":[{"title":"[FULL] a long title","reason":"valid json"}]}`
	recordMap := map[string]any{
		"thread": map[string]any{
			"thread-1": map[string]any{
				"value": map[string]any{
					"value": map[string]any{
						"messages": []any{"user-1", "assistant-1"},
					},
				},
			},
		},
		"thread_message": map[string]any{
			"assistant-1": map[string]any{
				"value": map[string]any{
					"value": map[string]any{
						"step": map[string]any{
							"type": "agent-inference",
							"value": []any{
								map[string]any{"type": "text", "content": want},
							},
						},
						"data": map[string]any{"completed": true},
					},
				},
			},
		},
	}
	raw := notionTestLines(t, map[string]any{"type": "record-map", "recordMap": recordMap})

	got, messageID := extractNotionText([]byte(raw), "thread-1")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if messageID != "assistant-1" {
		t.Fatalf("expected assistant message id, got %q", messageID)
	}
}

func TestExtractNotionTextFromSnakeCaseRecordMap(t *testing.T) {
	want := `{"ok":true,"message":"connected"}`
	recordMap := map[string]any{
		"thread": map[string]any{
			"thread-1": map[string]any{
				"value": map[string]any{
					"value": map[string]any{
						"messages": []any{"config-1", "context-1", "user-1", "assistant-1"},
					},
				},
			},
		},
		"thread_message": map[string]any{
			"assistant-1": map[string]any{
				"value": map[string]any{
					"value": map[string]any{
						"step": map[string]any{
							"type": "agent-inference",
							"value": []any{
								map[string]any{"type": "text", "content": want},
							},
						},
						"data": map[string]any{"completed": true},
					},
				},
			},
		},
	}
	raw := notionTestLines(t,
		map[string]any{"type": "patch-start", "data": map[string]any{"s": []any{}}},
		map[string]any{"type": "record-map", "record_map": recordMap},
	)

	got, messageID := extractNotionText([]byte(raw), "thread-1")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if messageID != "assistant-1" {
		t.Fatalf("expected assistant message id, got %q", messageID)
	}
}

func TestExtractNotionTextFromAssistantRoleRecordMap(t *testing.T) {
	want := `{"ok":true,"message":"connected"}`
	recordMap := map[string]any{
		"thread": map[string]any{
			"thread-1": map[string]any{
				"value": map[string]any{
					"value": map[string]any{
						"messages": []any{"user-1", "assistant-1"},
					},
				},
			},
		},
		"thread_message": map[string]any{
			"assistant-1": map[string]any{
				"value": map[string]any{
					"value": map[string]any{
						"role":    "assistant",
						"content": want,
						"data":    map[string]any{"completed": true},
					},
				},
			},
		},
	}
	raw := notionTestLines(t, map[string]any{"type": "record-map", "recordMap": recordMap})

	got, messageID := extractNotionText([]byte(raw), "thread-1")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if messageID != "assistant-1" {
		t.Fatalf("expected assistant message id, got %q", messageID)
	}
}

func TestInferenceErrorFromRecordMap(t *testing.T) {
	recordMap := map[string]any{
		"thread": map[string]any{
			"thread-1": map[string]any{
				"value": map[string]any{
					"value": map[string]any{
						"messages": []any{"user-1", "error-1"},
					},
				},
			},
		},
		"thread_message": map[string]any{
			"error-1": map[string]any{
				"value": map[string]any{
					"value": map[string]any{
						"step": map[string]any{
							"type":        "error",
							"message":     "model unavailable",
							"subType":     "inference_failed",
							"traceId":     "trace-1",
							"isRetryable": true,
						},
						"data": map[string]any{"completed": true},
					},
				},
			},
		},
	}
	raw := notionTestLines(t, map[string]any{"type": "record-map", "recordMap": recordMap})

	err := inferenceErrorFromRawNotionResponse(raw, "thread-1")
	stepErr, ok := err.(*NotionInferenceStepError)
	if !ok {
		t.Fatalf("expected NotionInferenceStepError, got %#v", err)
	}
	if stepErr.Message != "model unavailable" {
		t.Fatalf("expected error message, got %q", stepErr.Message)
	}
	if stepErr.SubType != "inference_failed" {
		t.Fatalf("expected subtype, got %q", stepErr.SubType)
	}
	if stepErr.TraceID != "trace-1" {
		t.Fatalf("expected trace id, got %q", stepErr.TraceID)
	}
	if !stepErr.Retryable {
		t.Fatal("expected retryable")
	}
}

func TestInferenceErrorFromPremiumFeaturePatchStart(t *testing.T) {
	raw := notionTestLines(t, map[string]any{
		"type": "patch-start",
		"data": map[string]any{
			"s": []any{
				map[string]any{
					"id":   "premium-1",
					"type": "premium-feature-unavailable",
					"featureAvailability": map[string]any{
						"limit": map[string]any{
							"type":    "cumulative",
							"current": float64(132),
							"total":   float64(75),
						},
					},
				},
			},
		},
	})

	err := inferenceErrorFromRawNotionResponse(raw, "thread-1")
	stepErr, ok := err.(*NotionInferenceStepError)
	if !ok {
		t.Fatalf("expected NotionInferenceStepError, got %#v", err)
	}
	if stepErr.SubType != "premium-feature-unavailable" {
		t.Fatalf("expected premium subtype, got %q", stepErr.SubType)
	}
	if stepErr.MessageID != "premium-1" {
		t.Fatalf("expected message id, got %q", stepErr.MessageID)
	}
	if !strings.Contains(stepErr.Message, "limit_type=cumulative") || !strings.Contains(stepErr.Message, "current=132 total=75") {
		t.Fatalf("unexpected message %q", stepErr.Message)
	}
}

func TestExtractNotionTextFromMarkdownChatEvent(t *testing.T) {
	want := `{"ok":true,"message":"connected"}`
	raw := notionTestLines(t, map[string]any{
		"type":  "markdown-chat",
		"id":    "assistant-1",
		"value": want,
	})

	got, messageID := extractNotionText([]byte(raw), "thread-1")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if messageID != "assistant-1" {
		t.Fatalf("expected assistant message id, got %q", messageID)
	}
}

func TestExtractNotionTextFromStepValueStringPatch(t *testing.T) {
	want := `{"ok":true,"message":"connected"}`
	raw := notionTestLines(t,
		map[string]any{
			"type": "patch-start",
			"data": map[string]any{
				"s": []any{
					map[string]any{"id": "user-1", "type": "user", "value": []any{[]any{"user prompt must not be returned"}}},
					map[string]any{"id": "assistant-1", "type": "markdown-chat", "value": ""},
				},
			},
		},
		map[string]any{
			"type": "patch",
			"v": []any{
				map[string]any{"o": "x", "p": "/s/1/value", "v": `{"ok":`},
			},
		},
		map[string]any{
			"type": "patch",
			"v": []any{
				map[string]any{"o": "x", "p": "/s/1/value", "v": `true,"message":"connected"}`},
			},
		},
	)

	got, messageID := extractNotionText([]byte(raw), "thread-1")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if messageID != "assistant-1" {
		t.Fatalf("expected assistant message id, got %q", messageID)
	}
}

func TestExtractNotionTextIgnoresUserStepValueStringPatch(t *testing.T) {
	raw := notionTestLines(t,
		map[string]any{
			"type": "patch-start",
			"data": map[string]any{
				"s": []any{
					map[string]any{"id": "user-1", "type": "user", "value": []any{[]any{"original user prompt"}}},
				},
			},
		},
		map[string]any{
			"type": "patch",
			"v": []any{
				map[string]any{"o": "x", "p": "/s/0/value", "v": `{"ok":true,"message":"this is user text"}`},
			},
		},
	)

	got, messageID := extractNotionText([]byte(raw), "thread-1")
	if got != "" || messageID != "" {
		t.Fatalf("expected empty assistant text, got text=%q message_id=%q", got, messageID)
	}
}

func TestExtractNotionTextDoesNotReturnUserPatchStart(t *testing.T) {
	raw := notionTestLines(t, map[string]any{
		"type": "patch-start",
		"data": map[string]any{
			"s": []any{
				map[string]any{"id": "user-1", "type": "user", "value": []any{[]any{"user prompt must not be returned"}}},
			},
		},
	})

	got, messageID := extractNotionText([]byte(raw), "thread-1")
	if got != "" || messageID != "" {
		t.Fatalf("expected empty assistant text, got text=%q message_id=%q", got, messageID)
	}
}

func TestLoadFinalAnswerOnceSyncsThreadMessages(t *testing.T) {
	const threadID = "thread-1"
	const messageID = "assistant-1"
	want := `{"ok":true,"message":"connected"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/syncRecordValuesSpaceInitial" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		requests := body["requests"].([]any)
		pointer := requests[0].(map[string]any)["pointer"].(map[string]any)
		switch pointer["table"] {
		case "thread":
			writeJSON(t, w, map[string]any{
				"recordMap": map[string]any{
					"thread": map[string]any{
						threadID: map[string]any{
							"value": map[string]any{
								"value": map[string]any{
									"messages": []any{"user-1", messageID},
								},
							},
						},
					},
				},
			})
		case "thread_message":
			writeJSON(t, w, map[string]any{
				"recordMap": map[string]any{
					"thread_message": map[string]any{
						messageID: map[string]any{
							"value": map[string]any{
								"value": map[string]any{
									"step": map[string]any{
										"type": "agent-inference",
										"value": []any{
											map[string]any{"type": "text", "content": want},
										},
									},
									"data": map[string]any{"completed": true},
								},
							},
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected table %#v", pointer["table"])
		}
	}))
	defer server.Close()

	gateway := NewNotionGateway(config.Config{
		Upstream: config.UpstreamConfig{BaseURL: server.URL, Origin: server.URL},
	})
	got, gotMessageID, err := gateway.loadFinalAnswerOnce(
		context.Background(),
		model.Account{SpaceID: "space-1", AuthToken: "token"},
		threadID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if gotMessageID != messageID {
		t.Fatalf("expected message id %q, got %q", messageID, gotMessageID)
	}
}

func TestLoadFinalAnswerFromMessageIDsUsesSnakeCaseRecordMap(t *testing.T) {
	const threadID = "thread-1"
	const messageID = "assistant-1"
	want := `{"ok":true,"message":"connected"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/syncRecordValuesSpaceInitial" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		writeJSON(t, w, map[string]any{
			"record_map": map[string]any{
				"thread_message": map[string]any{
					messageID: map[string]any{
						"value": map[string]any{
							"value": map[string]any{
								"step": map[string]any{
									"type": "agent-inference",
									"value": []any{
										map[string]any{"type": "text", "content": want},
									},
								},
								"data": map[string]any{"completed": true},
							},
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	gateway := NewNotionGateway(config.Config{
		Upstream: config.UpstreamConfig{BaseURL: server.URL, Origin: server.URL},
	})
	got, gotMessageID, err := gateway.loadFinalAnswerFromMessageIDs(
		context.Background(),
		model.Account{SpaceID: "space-1", AuthToken: "token"},
		threadID,
		[]string{messageID},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if gotMessageID != messageID {
		t.Fatalf("expected message id %q, got %q", messageID, gotMessageID)
	}
}

func TestListInferenceTranscriptsUsesSpaceWorkflowRequest(t *testing.T) {
	const spaceID = "64b3e3c4-c0fc-81b3-9009-0003cf74f0c5"
	const threadID = "33588b21-50f8-42be-9da9-31423724ae8f"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/getInferenceTranscriptsForUser" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("X-Notion-Space-ID"); got != spaceID {
			t.Fatalf("X-Notion-Space-ID = %q, want %q", got, spaceID)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		pointer := body["threadParentPointer"].(map[string]any)
		if pointer["table"] != "space" || pointer["id"] != spaceID || pointer["spaceId"] != spaceID {
			t.Fatalf("unexpected threadParentPointer: %#v", pointer)
		}
		if body["includeWorkflowThreads"] != true {
			t.Fatalf("includeWorkflowThreads = %#v, want true", body["includeWorkflowThreads"])
		}
		if body["includeWriterChats"] != false {
			t.Fatalf("includeWriterChats = %#v, want false", body["includeWriterChats"])
		}
		if body["limit"] != float64(50) {
			t.Fatalf("limit = %#v, want 50", body["limit"])
		}
		writeJSON(t, w, map[string]any{
			"transcripts": []map[string]any{{
				"id":                      threadID,
				"title":                   "模型服务连通性测试",
				"created_at":              float64(1781602942759),
				"updated_at":              float64(1781602947226),
				"created_by_display_name": "ahah Y",
				"type":                    "workflow",
			}},
			"threadIds":       []string{threadID},
			"unreadThreadIds": []string{threadID},
			"hasMore":         false,
			"recordMap": map[string]any{
				"thread": map[string]any{
					threadID: map[string]any{
						"spaceId": spaceID,
					},
				},
			},
		})
	}))
	defer server.Close()

	gateway := NewNotionGateway(config.Config{
		Upstream: config.UpstreamConfig{BaseURL: server.URL, Origin: server.URL},
	})
	got, err := gateway.ListInferenceTranscripts(
		context.Background(),
		model.Account{Email: "user@example.com", UserID: "user-1", SpaceID: spaceID, AuthToken: "token"},
		spaceID,
		50,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 transcript, got %d", len(got))
	}
	row := got[0]
	if row.ThreadID != threadID || row.ID != threadID {
		t.Fatalf("unexpected thread ids: id=%q thread_id=%q", row.ID, row.ThreadID)
	}
	if row.Title != "模型服务连通性测试" {
		t.Fatalf("title = %q", row.Title)
	}
	if row.Transport != "workflow" {
		t.Fatalf("transport = %q, want workflow", row.Transport)
	}
	if row.CreatedByDisplayName != "ahah Y" {
		t.Fatalf("created_by_display_name = %q", row.CreatedByDisplayName)
	}
	if row.SpaceID != spaceID {
		t.Fatalf("space_id = %q, want %q", row.SpaceID, spaceID)
	}
	if row.UpdatedAt == nil || !row.UpdatedAt.Equal(time.UnixMilli(1781602947226).UTC()) {
		t.Fatalf("updated_at = %v, want %v", row.UpdatedAt, time.UnixMilli(1781602947226).UTC())
	}
}

func TestRunPromptReturnsWhenUpstreamKeepsStreamOpenAfterFinalAnswer(t *testing.T) {
	const want = `{"ok":true,"message":"connected"}`
	const messageID = "assistant-1"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/runInferenceTranscript" {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		threadID, _ := body["threadId"].(string)
		if threadID == "" {
			t.Error("expected generated threadId")
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		_, _ = fmt.Fprint(w, notionTestLines(t, map[string]any{
			"type": "agent-inference",
			"id":   messageID,
			"value": []any{
				map[string]any{"type": "text", "content": want},
			},
			"finishedAt": "2026-06-16T08:42:29.000Z",
		}))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	defer server.Close()

	gateway := NewNotionGateway(config.Config{
		Upstream: config.UpstreamConfig{BaseURL: server.URL, Origin: server.URL, TimeoutSeconds: 10},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	got, err := gateway.RunPrompt(ctx, model.Account{
		Email:     "user@example.com",
		UserID:    "user-1",
		SpaceID:   "space-1",
		AuthToken: "token",
	}, NotionPromptRequest{
		Prompt:      `请返回 {"ok": true, "message": "connected"}。`,
		PublicModel: "gpt-5.5",
		NotionModel: "oatmeal-cookie",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != want {
		t.Fatalf("expected %q, got %q", want, got.Text)
	}
	if got.MessageID != messageID {
		t.Fatalf("expected message id %q, got %q", messageID, got.MessageID)
	}
	if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
		t.Fatalf("expected stream to return before context timeout, elapsed=%s", elapsed)
	}
}

func notionTestLines(t *testing.T, payloads ...map[string]any) string {
	t.Helper()
	lines := make([]string, 0, len(payloads))
	for _, payload := range payloads {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(encoded))
	}
	return strings.Join(lines, "\n") + "\n"
}

func writeJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatal(err)
	}
}
