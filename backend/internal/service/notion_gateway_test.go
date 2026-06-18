package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

func TestExtractNotionTextFromRichTextRecordMap(t *testing.T) {
	want := "这个 `docker-compose.yml` 定义了一个服务。\n- 端口 `9998`\n数据目录。"
	recordMap := notionTestAgentRecordMapWithValue("thread-1", "assistant-1", []any{
		map[string]any{
			"type": "text",
			"content": []any{
				map[string]any{"plainText": "这个 "},
				map[string]any{"plain_text": "`docker-compose.yml`", "text": map[string]any{"content": "`docker-compose.yml`"}},
				map[string]any{"text": map[string]any{"content": " 定义了一个服务。\n"}},
				map[string]any{"rich_text": []any{
					map[string]any{"plainText": "- 端口 "},
					map[string]any{"plainText": "`9998`\n"},
				}},
				map[string]any{"plainText": "数据目录。"},
			},
		},
	}, true)
	raw := notionTestLines(t, map[string]any{"type": "record-map", "recordMap": recordMap})

	got, messageID := extractNotionText([]byte(raw), "thread-1")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if messageID != "assistant-1" {
		t.Fatalf("expected assistant message id, got %q", messageID)
	}
}

func TestExtractNotionTextFromMarkdownTypedRecordMap(t *testing.T) {
	want := "我读完了 `docker-compose.yml`。\n- 服务名：`notion2api`\n```yaml\ncommand: [\"./notion2api\"]\n```\n目录。"
	recordMap := notionTestAgentRecordMapWithValue("thread-1", "assistant-1", []any{
		map[string]any{"type": "text", "content": "我"},
		map[string]any{"type": "markdown", "content": "读完了 `docker-compose.yml`。\n"},
		map[string]any{"type": "bulleted-list", "content": "- 服务名：`notion2api`\n"},
		map[string]any{"type": "code", "content": "```yaml\ncommand: [\"./notion2api\"]\n```\n"},
		map[string]any{"type": "text", "content": "目录。"},
	}, true)
	raw := notionTestLines(t, map[string]any{"type": "record-map", "recordMap": recordMap})

	got, messageID := extractNotionText([]byte(raw), "thread-1")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if messageID != "assistant-1" {
		t.Fatalf("expected assistant message id, got %q", messageID)
	}
}

func TestExtractNotionTextFromMixedRichMarkdownRecordMap(t *testing.T) {
	want := "我读完了 `docker-compose.yml`。\n\n主要内容如下：\n- 服务名：`notion2api`\n- 端口：`9998`\n\n也就是说，这个 compose 文件用于启动服务并挂载数据目录。"
	recordMap := notionTestAgentRecordMapWithValue("thread-1", "assistant-1", []any{
		map[string]any{"type": "text", "content": []any{
			map[string]any{"plain_text": "我读完了 "},
			map[string]any{"plain_text": "`docker-compose.yml`", "text": map[string]any{"content": "`docker-compose.yml`"}},
			map[string]any{"plain_text": "。"},
		}},
		map[string]any{"type": "markdown", "content": "\n\n主要内容如下：\n"},
		map[string]any{"type": "bulleted-list-item", "content": []any{
			map[string]any{"plainText": "- 服务名："},
			map[string]any{"plainText": "`notion2api`\n"},
		}},
		map[string]any{"type": "bulleted-list-item", "content": []any{
			map[string]any{"plain_text": "- 端口："},
			map[string]any{"plain_text": "`9998`\n"},
		}},
		map[string]any{"type": "paragraph", "content": "\n也就是说，这个 compose 文件用于启动服务并挂载数据目录。"},
	}, true)
	raw := notionTestLines(t, map[string]any{"type": "record-map", "recordMap": recordMap})

	got, messageID := extractNotionText([]byte(raw), "thread-1")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if messageID != "assistant-1" {
		t.Fatalf("expected assistant message id, got %q", messageID)
	}
}

func TestExtractNotionTextFromNestedUnknownContentKeys(t *testing.T) {
	want := "我读完了 docker-compose.yml。\n目录。"
	recordMap := notionTestAgentRecordMapWithValue("thread-1", "assistant-1", []any{
		map[string]any{"type": "markdown-block", "children": []any{
			map[string]any{"plainText": "我读完了 "},
			map[string]any{"plainText": "docker-compose.yml。\n"},
		}},
		map[string]any{"type": "text", "content": "目录。"},
	}, true)
	raw := notionTestLines(t, map[string]any{"type": "record-map", "recordMap": recordMap})

	got, messageID := extractNotionText([]byte(raw), "thread-1")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if messageID != "assistant-1" {
		t.Fatalf("expected assistant message id, got %q", messageID)
	}
}

func TestConsumeNotionTextStreamWaitsForCompletedRecordMap(t *testing.T) {
	const threadID = "thread-1"
	const messageID = "assistant-1"
	partial := "partial answer"
	want := "complete answer"
	raw := notionTestLines(t,
		map[string]any{"type": "record-map", "recordMap": notionTestAgentRecordMap(threadID, messageID, partial, false)},
		map[string]any{"type": "record-map", "recordMap": notionTestAgentRecordMap(threadID, messageID, want, true)},
	)

	result, err := consumeNotionTextStream(io.NopCloser(strings.NewReader(raw)), threadID, 50*time.Millisecond, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != want {
		t.Fatalf("expected completed answer %q, got %q", want, result.Text)
	}
	if result.MessageID != messageID {
		t.Fatalf("expected message id %q, got %q", messageID, result.MessageID)
	}
	if !result.Completed {
		t.Fatal("expected completed stream result")
	}
}

func TestConsumeNotionTextStreamKeepsAnswerTextWhenPatchLaterShrinks(t *testing.T) {
	const threadID = "thread-1"
	const messageID = "assistant-1"
	want := "这个 `docker-compose.yml` 定义了服务。\n- 端口 `8787`\n端口。"
	raw := notionTestLines(t,
		map[string]any{"type": "patch-start", "data": map[string]any{"s": []any{}}},
		map[string]any{"type": "patch", "v": []any{
			map[string]any{"o": "a", "p": "/s/-", "v": map[string]any{"id": messageID, "type": "agent-inference", "value": []any{}}},
		}},
		map[string]any{"type": "patch", "v": []any{
			map[string]any{"o": "a", "p": "/s/0/value/-", "v": map[string]any{"type": "thinking", "content": ""}},
		}},
		map[string]any{"type": "patch", "v": []any{
			map[string]any{"o": "x", "p": "/s/0/value/0/content", "v": "internal reasoning must not be returned"},
		}},
		map[string]any{"type": "patch", "v": []any{
			map[string]any{"o": "a", "p": "/s/0/value/-", "v": map[string]any{"type": "text", "content": ""}},
		}},
		map[string]any{"type": "patch", "v": []any{
			map[string]any{"o": "x", "p": "/s/0/value/1/content", "v": "这个 `docker-compose.yml` 定义了服务。\n"},
		}},
		map[string]any{"type": "patch", "v": []any{
			map[string]any{"o": "x", "p": "/s/0/value/1/content", "v": "- 端口 `8787`\n"},
		}},
		map[string]any{"type": "patch", "v": []any{
			map[string]any{"o": "x", "p": "/s/0/value/1/content", "v": "端口。"},
		}},
		map[string]any{"type": "patch", "v": []any{
			map[string]any{"o": "a", "p": "/s/0/finishedAt", "v": "2026-06-16T08:42:29.000Z"},
		}},
	)

	result, err := consumeNotionTextStream(io.NopCloser(strings.NewReader(raw)), threadID, 50*time.Millisecond, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != want {
		t.Fatalf("expected %q, got %q", want, result.Text)
	}
	if strings.Contains(result.Text, "internal reasoning") {
		t.Fatalf("answer leaked thinking content: %q", result.Text)
	}
	if result.MessageID != messageID {
		t.Fatalf("expected message id %q, got %q", messageID, result.MessageID)
	}
	if !result.Completed {
		t.Fatal("expected completed stream result")
	}
}

func TestRunPromptRecoversCompletedAnswerAfterIncompleteStream(t *testing.T) {
	const messageID = "assistant-1"
	partial := "partial answer"
	want := "complete answer"
	var threadID string
	var sawSync bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/runInferenceTranscript":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				return
			}
			threadID = stringFromAny(body["threadId"])
			if threadID == "" {
				t.Fatal("expected generated thread id")
			}
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = fmt.Fprint(w, notionTestLines(t,
				map[string]any{"type": "record-map", "recordMap": notionTestAgentRecordMap(threadID, messageID, partial, false)},
			))
		case "/api/v3/syncRecordValuesSpaceInitial":
			sawSync = true
			writeJSON(t, w, map[string]any{
				"record_map": notionTestAgentRecordMap(threadID, messageID, want, true),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	gateway := NewNotionGateway(config.Config{
		Upstream: config.UpstreamConfig{BaseURL: server.URL, Origin: server.URL, TimeoutSeconds: 10, PollIntervalSeconds: 0.01, PollMaxRounds: 1},
	})
	got, err := gateway.RunPrompt(context.Background(), model.Account{
		Email:     "user@example.com",
		UserID:    "user-1",
		SpaceID:   "space-1",
		AuthToken: "token",
	}, NotionPromptRequest{
		Prompt:      "read file",
		PublicModel: "gpt-test",
		NotionModel: "oatmeal-cookie",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != want {
		t.Fatalf("expected recovered answer %q, got %q", want, got.Text)
	}
	if got.MessageID != messageID {
		t.Fatalf("expected message id %q, got %q", messageID, got.MessageID)
	}
	if !sawSync {
		t.Fatal("expected sync after incomplete stream")
	}
}

func TestRunPromptPrefersSyncedAnswerAfterCompletedStream(t *testing.T) {
	const messageID = "assistant-1"
	want := "这个 `docker-compose.yml` 定义了一个名为 `notion2api` 的服务，用来构建并运行 Notion2API 容器。\n\n主要内容如下：\n\n- **服务名**：`notion2api`\n- **端口映射**：宿主机 `8787` 到容器内 `8787`\n\n也就是说，这个 compose 文件用于启动服务并挂载数据目录。"
	var threadID string
	var syncedMessageIDs []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/runInferenceTranscript":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				return
			}
			threadID = stringFromAny(body["threadId"])
			if threadID == "" {
				t.Fatal("expected generated thread id")
			}
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = fmt.Fprint(w, notionTestLines(t,
				map[string]any{
					"type": "patch-start",
					"data": map[string]any{
						"s": []any{
							map[string]any{"id": "user-1", "type": "user", "value": []any{[]any{"read file"}}},
							map[string]any{"id": messageID, "type": "agent-inference", "value": []any{map[string]any{"type": "text", "content": "这个 "}}},
						},
					},
				},
				map[string]any{
					"type": "patch",
					"v": []any{
						map[string]any{"o": "x", "p": "/s/1/value/0/content", "v": "端口。"},
					},
				},
				map[string]any{
					"type": "patch",
					"v": []any{
						map[string]any{"o": "a", "p": "/s/1/finishedAt", "v": "2026-06-16T08:42:29.000Z"},
					},
				},
			))
		case "/api/v3/syncRecordValuesSpaceInitial":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				return
			}
			for _, rawRequest := range sliceFromAny(body["requests"]) {
				pointer := mapFromAny(mapFromAny(rawRequest)["pointer"])
				if stringFromAny(pointer["table"]) == "thread_message" {
					syncedMessageIDs = append(syncedMessageIDs, stringFromAny(pointer["id"]))
				}
			}
			writeJSON(t, w, map[string]any{
				"recordMap": notionTestAgentRecordMap(threadID, messageID, want, true),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	gateway := NewNotionGateway(config.Config{
		Upstream: config.UpstreamConfig{BaseURL: server.URL, Origin: server.URL, TimeoutSeconds: 10, PollIntervalSeconds: 0.01, PollMaxRounds: 1},
	})
	got, err := gateway.RunPrompt(context.Background(), model.Account{
		Email:     "user@example.com",
		UserID:    "user-1",
		SpaceID:   "space-1",
		AuthToken: "token",
	}, NotionPromptRequest{
		Prompt:      "read file",
		PublicModel: "gpt-test",
		NotionModel: "oatmeal-cookie",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != want {
		t.Fatalf("expected synced answer %q, got %q", want, got.Text)
	}
	if got.MessageID != messageID {
		t.Fatalf("expected message id %q, got %q", messageID, got.MessageID)
	}
	if len(syncedMessageIDs) != 1 || syncedMessageIDs[0] != messageID {
		t.Fatalf("expected exact message sync for %q, got %#v", messageID, syncedMessageIDs)
	}
}

func TestBuildPayloadUsesPartialTranscriptForExistingThread(t *testing.T) {
	gateway := NewNotionGateway(config.Default())
	payload, threadID := gateway.BuildPayload(model.Account{
		Email:   "user@example.com",
		UserID:  "user-id",
		SpaceID: "space-id",
	}, NotionPromptRequest{
		Prompt:           "user: 继续",
		UpstreamThreadID: "notion-thread-1",
	})

	if threadID != "notion-thread-1" || payload["threadId"] != "notion-thread-1" {
		t.Fatalf("expected existing thread id, got threadID=%q payload=%#v", threadID, payload["threadId"])
	}
	if payload["createThread"] != false || payload["generateTitle"] != false || payload["saveAllThreadOperations"] != false {
		t.Fatalf("existing thread should not create/title/save all operations: %#v", payload)
	}
	if payload["isPartialTranscript"] != true {
		t.Fatalf("existing thread should use partial transcript, got %#v", payload["isPartialTranscript"])
	}
	if _, ok := payload["threadParentPointer"]; ok {
		t.Fatalf("existing thread should not set thread parent pointer: %#v", payload["threadParentPointer"])
	}
	transcript, ok := payload["transcript"].([]map[string]any)
	if !ok || len(transcript) != 1 {
		t.Fatalf("expected one user transcript step, got %#v", payload["transcript"])
	}
	if transcript[0]["type"] != "user" {
		t.Fatalf("expected user step only, got %#v", transcript)
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

func TestMessageIDsFromRawNotionResponseExtractsAssistantPatchStartID(t *testing.T) {
	raw := notionTestLines(t, map[string]any{
		"type": "patch-start",
		"data": map[string]any{
			"s": []any{
				map[string]any{"id": "user-1", "type": "user", "value": []any{[]any{"user prompt"}}},
				map[string]any{"id": "assistant-1", "type": "agent-inference", "value": []any{map[string]any{"type": "text", "content": "answer"}}},
			},
		},
	})

	got := messageIDsFromRawNotionResponse(raw, "thread-1")
	if len(got) != 1 || got[0] != "assistant-1" {
		t.Fatalf("expected assistant message id only, got %#v", got)
	}
}

func TestMessageIDsFromRawNotionResponseIgnoresAgentToolSteps(t *testing.T) {
	raw := notionTestLines(t,
		map[string]any{
			"type": "patch",
			"v": []any{
				map[string]any{"o": "a", "p": "/s/-", "v": map[string]any{"id": "instruction-1", "type": "agent-instruction-state"}},
				map[string]any{"o": "a", "p": "/s/-", "v": map[string]any{"id": "tool-1", "type": "agent-tool-result"}},
				map[string]any{"o": "a", "p": "/s/-", "v": map[string]any{"id": "assistant-1", "type": "agent-inference"}},
			},
		},
	)

	got := messageIDsFromRawNotionResponse(raw, "thread-1")
	if len(got) != 1 || got[0] != "assistant-1" {
		t.Fatalf("expected only assistant message id, got %#v", got)
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

func TestRunPromptUploadsAssistantAttachmentBeforeInference(t *testing.T) {
	const want = "uploaded image received"
	const messageID = "assistant-attachment"
	const spaceID = "space-1"
	const userID = "user-1"

	var s3Server *httptest.Server
	var sawS3Upload bool
	var sawEnqueue bool
	var sawGetTasks bool
	var sawSignedURL bool
	var inferencePayload map[string]any

	notionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/getUploadFileUrlForAssistantChatTranscriptUpload":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				return
			}
			if body["name"] != "image.jpg" || body["contentType"] != "image/jpeg" {
				t.Fatalf("unexpected upload target request: %#v", body)
			}
			writeJSON(t, w, map[string]any{
				"url": s3Server.URL,
				"fields": map[string]any{
					"Content-Type":    "image/jpeg",
					"bucket":          "prod-files-secure",
					"key":             spaceID + "/file-id/image.jpg",
					"Policy":          "policy",
					"X-Amz-Signature": "signature",
				},
			})
		case "/api/v3/enqueueTask":
			sawEnqueue = true
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				return
			}
			if !strings.Contains(fmt.Sprint(body), "processAgentAttachment") || !strings.Contains(fmt.Sprint(body), "attachment:file-id:image.jpg") {
				t.Fatalf("unexpected enqueue payload: %#v", body)
			}
			writeJSON(t, w, map[string]any{"taskId": "task-1"})
		case "/api/v3/getTasks":
			sawGetTasks = true
			writeJSON(t, w, map[string]any{"tasks": []any{map[string]any{"id": "task-1", "state": "success"}}})
		case "/api/v3/getSignedFileUrls":
			sawSignedURL = true
			writeJSON(t, w, map[string]any{"signedUrls": []any{"https://file.notion.test/image.jpg"}})
		case "/api/v3/runInferenceTranscript":
			if err := json.NewDecoder(r.Body).Decode(&inferencePayload); err != nil {
				t.Error(err)
				return
			}
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = fmt.Fprint(w, notionTestLines(t, map[string]any{
				"type": "agent-inference",
				"id":   messageID,
				"value": []any{
					map[string]any{"type": "text", "content": want},
				},
				"finishedAt": "2026-06-16T08:42:29.000Z",
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer notionServer.Close()

	s3Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected S3 method %s", r.Method)
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		body := string(raw)
		if !strings.Contains(body, `name="file"; filename="image.jpg"`) || !strings.Contains(body, "jpeg-bytes") {
			t.Fatalf("unexpected S3 multipart body: %s", body)
		}
		sawS3Upload = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer s3Server.Close()

	gateway := NewNotionGateway(config.Config{
		Upstream: config.UpstreamConfig{BaseURL: notionServer.URL, Origin: notionServer.URL, TimeoutSeconds: 10, PollIntervalSeconds: 0.01, PollMaxRounds: 2},
	})
	got, err := gateway.RunPrompt(context.Background(), model.Account{
		Email:     "user@example.com",
		UserID:    userID,
		SpaceID:   spaceID,
		AuthToken: "token",
	}, NotionPromptRequest{
		Prompt:      "describe the image",
		PublicModel: "gpt-test",
		NotionModel: "oatmeal-cookie",
		Attachments: []NotionAttachmentUpload{
			{Name: "image.jpg", ContentType: "image/jpeg", Data: []byte("jpeg-bytes")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != want || got.MessageID != messageID {
		t.Fatalf("unexpected result: text=%q message_id=%q", got.Text, got.MessageID)
	}
	for name, ok := range map[string]bool{
		"s3 upload":  sawS3Upload,
		"enqueue":    sawEnqueue,
		"getTasks":   sawGetTasks,
		"signed URL": sawSignedURL,
	} {
		if !ok {
			t.Fatalf("expected %s call", name)
		}
	}
	transcript := inferencePayload["transcript"].([]any)
	foundAttachment := false
	for _, rawStep := range transcript {
		step := rawStep.(map[string]any)
		if step["type"] != "attachment" {
			continue
		}
		foundAttachment = true
		if step["fileUrl"] != "attachment:file-id:image.jpg" || step["signedUrl"] != "https://file.notion.test/image.jpg" {
			t.Fatalf("unexpected attachment step: %#v", step)
		}
	}
	if !foundAttachment {
		t.Fatalf("expected attachment step in transcript: %#v", transcript)
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

func notionTestAgentRecordMap(threadID string, messageID string, text string, completed bool) map[string]any {
	return notionTestAgentRecordMapWithValue(threadID, messageID, []any{
		map[string]any{"type": "text", "content": text},
	}, completed)
}

func notionTestAgentRecordMapWithValue(threadID string, messageID string, value []any, completed bool) map[string]any {
	return map[string]any{
		"thread": map[string]any{
			threadID: map[string]any{
				"value": map[string]any{
					"value": map[string]any{
						"messages": []any{"user-1", messageID},
					},
				},
			},
		},
		"thread_message": map[string]any{
			messageID: map[string]any{
				"value": map[string]any{
					"value": map[string]any{
						"step": map[string]any{
							"type":  "agent-inference",
							"value": value,
						},
						"data": map[string]any{"completed": completed},
					},
				},
			},
		},
	}
}
