package service

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"notaion2api/backend/internal/dto"
)

func TestPromptAndHiddenFromResponsesInputParsesMessageItems(t *testing.T) {
	input := []any{
		map[string]any{
			"role": "system",
			"content": []any{
				map[string]any{"type": "input_text", "text": "Return JSON only."},
			},
		},
		map[string]any{
			"type": "message",
			"role": "user",
			"content": []any{
				map[string]any{"type": "input_text", "text": "Say hello."},
			},
		},
	}

	prompt, hiddenPrompt, hiddenMessages := promptAndHiddenFromResponsesInput(input)
	if prompt != "user: Say hello." {
		t.Fatalf("unexpected prompt %q", prompt)
	}
	if !strings.Contains(hiddenPrompt, "[system]\nReturn JSON only.") {
		t.Fatalf("unexpected hidden prompt %q", hiddenPrompt)
	}
	if len(hiddenMessages) != 1 || hiddenMessages[0].Role != "system" || hiddenMessages[0].Content != "Return JSON only." {
		t.Fatalf("unexpected hidden messages %#v", hiddenMessages)
	}
}

func TestBuildResponsesPayloadMatchesResponsesShape(t *testing.T) {
	payload := buildResponsesPayload(
		"gpt-test",
		"connected",
		dto.UsageDTO{PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5, Estimated: true},
		"resp_test",
		"msg_test",
		123,
		map[string]any{
			"previous_response_id": "resp_prev",
			"metadata":             map[string]any{"k": "v"},
		},
		map[string]any{"thread_id": "thread-1"},
	)

	if payload["id"] != "resp_test" || payload["object"] != "response" || payload["status"] != "completed" {
		t.Fatalf("unexpected response identity/status: %#v", payload)
	}
	if payload["output_text"] != "connected" {
		t.Fatalf("expected output_text, got %#v", payload["output_text"])
	}
	if payload["previous_response_id"] != "resp_prev" {
		t.Fatalf("expected previous_response_id, got %#v", payload["previous_response_id"])
	}
	if payload["parallel_tool_calls"] != true || payload["tool_choice"] != "auto" {
		t.Fatalf("missing default tool fields: %#v", payload)
	}
	output := payload["output"].([]any)
	item := output[0].(map[string]any)
	if item["id"] != "msg_test" || item["status"] != "completed" {
		t.Fatalf("unexpected output item %#v", item)
	}
	content := item["content"].([]any)
	part := content[0].(map[string]any)
	if part["type"] != "output_text" || part["text"] != "connected" {
		t.Fatalf("unexpected content part %#v", part)
	}
	usage := payload["usage"].(map[string]any)
	if usage["input_tokens"] != int64(3) || usage["output_tokens"] != int64(2) || usage["total_tokens"] != int64(5) {
		t.Fatalf("unexpected usage %#v", usage)
	}
}

func TestResponsesPromptPartsFromBodyIncludesProtocolEnvelope(t *testing.T) {
	body := map[string]any{
		"model": "gpt-test",
		"input": "更新 README.md",
		"tools": []any{
			map[string]any{
				"type":        "custom",
				"name":        "apply_patch",
				"description": "Apply a patch to local files.",
			},
		},
		"tool_choice":         map[string]any{"type": "custom", "name": "apply_patch"},
		"parallel_tool_calls": false,
		"reasoning":           map[string]any{"effort": "medium"},
	}

	prompt, hiddenPrompt, _ := responsesPromptPartsFromBody(body)
	if !strings.Contains(prompt, "更新 README.md") {
		t.Fatalf("expected visible prompt to contain user input, got %q", prompt)
	}
	for _, want := range []string{"openai_request_controls", "reasoning preferences", "file or document edit", "concrete text patch block"} {
		if !strings.Contains(hiddenPrompt, want) {
			t.Fatalf("expected hidden prompt to contain %q, got %q", want, hiddenPrompt)
		}
	}
	for _, forbidden := range []string{`"tools"`, `"tool_choice"`, `"parallel_tool_calls"`, "exec_command", "apply_patch"} {
		if strings.Contains(hiddenPrompt, forbidden) {
			t.Fatalf("hidden prompt should not expose raw protocol/tool data %q: %q", forbidden, hiddenPrompt)
		}
	}
}

func TestResponsesNotionVisiblePromptRemovesRuntimeToolMarkers(t *testing.T) {
	prompt := strings.Join([]string{
		"user: 更新 README.md",
		"<environment_context>\n  <cwd>/Users/dashuju/dev/notion2api</cwd>\n  <filesystem>workspace_roots</filesystem>\n</environment_context>",
		`<tool_call name="exec_command" call_id="call_123">`,
		`{"cmd":"sed -n '1,120p' README.md"}`,
		`</tool_call>`,
		`<tool_result call_id="call_123">`,
		"# README\nResponses section",
		`</tool_result>`,
	}, "\n")

	notionPrompt := responsesNotionVisiblePrompt(prompt)
	for _, want := range []string{"user: 更新 README.md", "<context_result>", "# README", "Responses section"} {
		if !strings.Contains(notionPrompt, want) {
			t.Fatalf("expected notion prompt to contain %q, got %q", want, notionPrompt)
		}
	}
	for _, forbidden := range []string{"environment_context", "workspace_roots", "tool_call", "exec_command", "call_123"} {
		if strings.Contains(notionPrompt, forbidden) {
			t.Fatalf("notion prompt should not contain runtime marker %q: %q", forbidden, notionPrompt)
		}
	}
}

func TestResponsesNotionVisiblePromptPrependsSafeSystemSections(t *testing.T) {
	prompt := "user: Say hello."
	notionPrompt := responsesNotionVisiblePrompt(prompt, []hiddenPromptMessage{
		{Role: "system", Content: "Return JSON only."},
		{Role: "developer", Content: "You are Codex. Use apply_patch and exec_command with the local filesystem sandbox."},
	})

	for _, want := range []string{"[system]\nReturn JSON only.", "user: Say hello."} {
		if !strings.Contains(notionPrompt, want) {
			t.Fatalf("expected notion prompt to contain %q, got %q", want, notionPrompt)
		}
	}
	for _, forbidden := range []string{"You are Codex", "apply_patch", "exec_command", "local filesystem sandbox"} {
		if strings.Contains(notionPrompt, forbidden) {
			t.Fatalf("notion prompt should not contain unsafe instruction %q: %q", forbidden, notionPrompt)
		}
	}
}

func TestResponsesNotionVisiblePromptSanitizesUnsafeSystemSections(t *testing.T) {
	prompt := "user: Update a file."
	notionPrompt := responsesNotionVisiblePrompt(prompt, []hiddenPromptMessage{
		{Role: "system", Content: "You are Codex. Use apply_patch and exec_command with the local filesystem sandbox."},
	})

	for _, want := range []string{"[system]", "Codex-compatible system instructions", "concrete patch/diff", "user: Update a file."} {
		if !strings.Contains(notionPrompt, want) {
			t.Fatalf("expected notion prompt to contain %q, got %q", want, notionPrompt)
		}
	}
	for _, forbidden := range []string{"You are Codex", "apply_patch", "exec_command", "local filesystem sandbox"} {
		if strings.Contains(notionPrompt, forbidden) {
			t.Fatalf("notion prompt should not contain raw unsafe instruction %q: %q", forbidden, notionPrompt)
		}
	}
}

func TestResponsesNotionVisiblePromptIncludesInstructionsAndResponseFormat(t *testing.T) {
	body := map[string]any{
		"instructions": "Always answer in Chinese.",
		"input":        "Say hello.",
		"response_format": map[string]any{
			"type": "json_object",
		},
	}

	prompt, _, hiddenMessages := responsesPromptPartsFromBody(body)
	notionPrompt := responsesNotionVisiblePrompt(prompt, hiddenMessages)
	for _, want := range []string{"[system]\nAlways answer in Chinese.", "[response_format]", "response_format.type=json_object", "Say hello."} {
		if !strings.Contains(notionPrompt, want) {
			t.Fatalf("expected notion prompt to contain %q, got %q", want, notionPrompt)
		}
	}
	if strings.Contains(notionPrompt, "notion2api-instructions") {
		t.Fatalf("responses notion prompt should use visible sections, got %q", notionPrompt)
	}
}

func TestResponsesNotionVisiblePromptNormalizesRepeatedRolePrefixes(t *testing.T) {
	notionPrompt := responsesNotionVisiblePrompt("user: user: user: [config.docker.yaml](notion2api/backend/configs/config.docker.yaml) 改回9998")
	if strings.Contains(notionPrompt, "user: user:") {
		t.Fatalf("prompt should not duplicate user label: %q", notionPrompt)
	}
	if !strings.HasPrefix(notionPrompt, "user: [config.docker.yaml]") {
		t.Fatalf("unexpected normalized prompt: %q", notionPrompt)
	}
}

func TestResponsesPromptPartsFromBodyAppendsCurrentUserRequestForMultiTurnInput(t *testing.T) {
	body := map[string]any{
		"input": []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "[config.docker.yaml](notion2api/backend/configs/config.docker.yaml) 帮我尝试将 server prot 改为9999"},
				},
			},
			map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "output_text", "text": "修改已完成。"},
				},
			},
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "改回9998"},
				},
			},
		},
	}

	prompt, _, _ := responsesPromptPartsFromBody(body)
	if !strings.Contains(prompt, "[current_user_request]") {
		t.Fatalf("expected current request section, got %q", prompt)
	}
	if !strings.Contains(prompt, "Treat this as the active request for this turn:\nuser: 改回9998") {
		t.Fatalf("expected latest user request to be explicit, got %q", prompt)
	}
	if !strings.HasSuffix(prompt, "user: 改回9998") {
		t.Fatalf("current request should be last, got %q", prompt)
	}
	if strings.Contains(prompt, "帮我尝试将 server prot 改为9999") || strings.Contains(prompt, "修改已完成") {
		t.Fatalf("prompt should not keep completed historical turn, got %q", prompt)
	}
}

func TestResponsesPromptPartsFromBodySkipsCodexCompactionContext(t *testing.T) {
	body := map[string]any{
		"input": []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "Another language model started to solve this problem and produced a summary of its thinking process.\n\n## Handoff Summary\n\n### 当前进展 / 已做决策\n- 旧任务已经完成。\n"},
				},
			},
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "帮我更新 [docker-compose.yml](notion2api/docker-compose.yml) 的 server port 为 9991"},
				},
			},
		},
	}

	prompt, _, _ := responsesPromptPartsFromBody(body)
	for _, forbidden := range []string{"Another language model", "Handoff Summary", "旧任务已经完成"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("prompt should drop internal compaction context %q: %q", forbidden, prompt)
		}
	}
	if !strings.Contains(prompt, "server port 为 9991") {
		t.Fatalf("prompt should keep latest real user request, got %q", prompt)
	}
}

func TestMaybeBuildCodexCompactionResultHandlesLocally(t *testing.T) {
	body := map[string]any{
		"input": []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "帮我更新 [docker-compose.yml](notion2api/docker-compose.yml) 的 server port 为 9991"},
				},
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_read",
				"output":  "services:\n  backend:\n    ports:\n      - \"${N2A_PORT:-9998}:9998\"\n",
			},
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "You are performing a COPOINT COMPACTION. Create a handoff summary for another LLM that will resume the task."},
				},
			},
		},
	}

	result, ok := maybeBuildCodexCompactionResult("gpt-test", body)
	if !ok {
		t.Fatal("expected compaction request to be handled locally")
	}
	text := result.Payload["output_text"].(string)
	for _, want := range []string{"Handoff Summary", "server port 为 9991", "不要重复"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected local summary to contain %q, got %q", want, text)
		}
	}
	if trace := result.Payload["notion_trace"].(map[string]any); trace["local_reason"] != "codex_compaction" {
		t.Fatalf("expected local compaction trace, got %#v", trace)
	}
}

func TestPromptAndHiddenFromResponsesInputAvoidsDuplicateAndBlankUserLabels(t *testing.T) {
	input := []any{
		map[string]any{"type": "message", "role": "user", "content": []any{
			map[string]any{"type": "input_text", "text": "user:"},
		}},
		map[string]any{"type": "message", "role": "user", "content": []any{
			map[string]any{"type": "input_text", "text": "user: user: user: [config.docker.yaml](notion2api/backend/configs/config.docker.yaml) 帮我改端口"},
		}},
	}

	prompt, hiddenPrompt, hiddenMessages := promptAndHiddenFromResponsesInput(input)
	if hiddenPrompt != "" || len(hiddenMessages) != 0 {
		t.Fatalf("unexpected hidden prompt/messages: %q %#v", hiddenPrompt, hiddenMessages)
	}
	if strings.Contains(prompt, "user: user:") {
		t.Fatalf("prompt should not duplicate user label: %q", prompt)
	}
	if strings.Contains("\n"+prompt+"\n", "\nuser:\n") {
		t.Fatalf("prompt should not include blank user label: %q", prompt)
	}
	if prompt != "user: [config.docker.yaml](notion2api/backend/configs/config.docker.yaml) 帮我改端口" {
		t.Fatalf("unexpected prompt %q", prompt)
	}
}

func TestPromptAndHiddenFromResponsesInputIncludesInlineFileData(t *testing.T) {
	fileData := "data:text/plain;base64," + base64.StdEncoding.EncodeToString([]byte("package main\n\nfunc main() {}\n"))
	input := []any{
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "input_text", "text": "Review this file."},
				map[string]any{"type": "input_file", "filename": "main.go", "file_data": fileData},
			},
		},
	}

	prompt, hiddenPrompt, hiddenMessages := promptAndHiddenFromResponsesInput(input)
	if hiddenPrompt != "" || len(hiddenMessages) != 0 {
		t.Fatalf("unexpected hidden prompt/messages: %q %#v", hiddenPrompt, hiddenMessages)
	}
	for _, want := range []string{"Review this file.", `<file name="main.go">`, "package main", "func main() {}"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected prompt to contain %q, got %q", want, prompt)
		}
	}
	if strings.Contains(prompt, base64.StdEncoding.EncodeToString([]byte("package main\n\nfunc main() {}\n"))) {
		t.Fatalf("prompt leaked raw base64: %q", prompt)
	}
}

func TestResponsesAttachmentUploadsFromBodyExtractsInputImageData(t *testing.T) {
	imageData := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte("jpeg-bytes"))
	body := map[string]any{
		"input": []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "Describe this image."},
					map[string]any{"type": "input_image", "filename": "image.jpg", "image_url": map[string]any{"url": imageData}},
				},
			},
		},
	}

	uploads := responsesAttachmentUploadsFromBody(body)
	if len(uploads) != 1 {
		t.Fatalf("expected one upload, got %#v", uploads)
	}
	upload := uploads[0]
	if upload.Name != "image.jpg" || upload.ContentType != "image/jpeg" || string(upload.Data) != "jpeg-bytes" {
		t.Fatalf("unexpected upload %#v", upload)
	}
	prompt := promptFromResponsesInput(body["input"])
	if strings.Contains(prompt, imageData) {
		t.Fatalf("prompt leaked image data URL: %q", prompt)
	}
	if !strings.Contains(prompt, "inline image data") {
		t.Fatalf("expected prompt to retain an inline image reference, got %q", prompt)
	}
}

func TestMaybeBuildCodexFileToolCallResultUsesExecCommand(t *testing.T) {
	body := map[string]any{
		"input": []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "帮我查看文件 [Dockerfile](https://app.notion.com/Dockerfile) ,告诉我怎么部署的"},
				},
			},
		},
		"tools": []any{
			map[string]any{"type": "function", "name": "exec_command"},
		},
	}

	result, ok := maybeBuildCodexFileToolCallResult("gpt-test", body)
	if !ok {
		t.Fatal("expected codex file reference to produce a tool call")
	}
	output := result.Payload["output"].([]any)
	item := output[0].(map[string]any)
	defer pendingResponseContexts.Delete("resp:" + result.Payload["id"].(string))
	defer pendingResponseContexts.Delete("call:" + item["call_id"].(string))
	if item["type"] != "function_call" || item["name"] != "exec_command" {
		t.Fatalf("unexpected function call item %#v", item)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(item["arguments"].(string)), &args); err != nil {
		t.Fatalf("invalid arguments json: %v", err)
	}
	if !strings.Contains(args["cmd"].(string), "Dockerfile") {
		t.Fatalf("expected read command to reference Dockerfile, got %#v", args)
	}
	if result.Payload["output_text"] != "" {
		t.Fatalf("function call response should not include text output: %#v", result.Payload["output_text"])
	}
}

func TestMaybeBuildCodexFileToolCallResultAllowsNewReadAfterToolOutput(t *testing.T) {
	body := map[string]any{
		"input": []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "[README.md](notion2api/README.md) 帮我修改说明"},
				},
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_read",
				"output":  "# README\n",
			},
			map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "output_text", "text": "README 已处理。"},
				},
			},
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "帮我查看 [Dockerfile](notion2api/Dockerfile)"},
				},
			},
		},
		"tools": []any{
			map[string]any{"type": "function", "name": "exec_command"},
		},
	}

	result, ok := maybeBuildCodexFileToolCallResult("gpt-test", body)
	if !ok {
		t.Fatal("expected new user file request after tool output to produce a tool call")
	}
	output := result.Payload["output"].([]any)
	item := output[0].(map[string]any)
	defer pendingResponseContexts.Delete("resp:" + result.Payload["id"].(string))
	defer pendingResponseContexts.Delete("call:" + item["call_id"].(string))
	var args map[string]any
	if err := json.Unmarshal([]byte(item["arguments"].(string)), &args); err != nil {
		t.Fatalf("invalid arguments json: %v", err)
	}
	cmd := args["cmd"].(string)
	if !strings.Contains(cmd, "notion2api/Dockerfile") {
		t.Fatalf("expected latest Dockerfile request, got %q", cmd)
	}
	if strings.Contains(cmd, "README.md") {
		t.Fatalf("should not reuse historical README request, got %q", cmd)
	}
}

func TestMaybeBuildCodexFileToolCallResultSkipsImmediatelyAfterToolOutput(t *testing.T) {
	body := map[string]any{
		"input": []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "帮我查看 [Dockerfile](Dockerfile)"},
				},
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_read",
				"output":  "FROM alpine\n",
			},
		},
		"tools": []any{
			map[string]any{"type": "function", "name": "exec_command"},
		},
	}

	if _, ok := maybeBuildCodexFileToolCallResult("gpt-test", body); ok {
		t.Fatal("tool output follow-up should be sent to Notion for answer synthesis, not produce another file read")
	}
}

func TestMaybeBuildCodexFileToolCallResultSkipsRepeatedReadWhenUserTextTrailsToolOutput(t *testing.T) {
	body := map[string]any{
		"input": []any{
			map[string]any{
				"type":      "function_call",
				"call_id":   "call_read",
				"name":      "exec_command",
				"arguments": `{"cmd":"sed -n '1,400p' Dockerfile","yield_time_ms":10000,"max_output_tokens":20000}`,
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_read",
				"output":  "FROM alpine\n",
			},
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "帮我查看 [Dockerfile](Dockerfile)"},
				},
			},
		},
		"tools": []any{
			map[string]any{"type": "function", "name": "exec_command"},
		},
	}

	if _, ok := maybeBuildCodexFileToolCallResult("gpt-test", body); ok {
		t.Fatal("already-read file should not produce another file-read tool call")
	}
}

func TestMaybeBuildCodexFileToolCallResultDoesNotReuseOldFileWhenLatestUserHasNoFile(t *testing.T) {
	body := map[string]any{
		"input": []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "[README.md](notion2api/README.md) 帮我总结"},
				},
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_read",
				"output":  "# README\n",
			},
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "继续说明风险"},
				},
			},
		},
		"tools": []any{
			map[string]any{"type": "function", "name": "exec_command"},
		},
	}

	if _, ok := maybeBuildCodexFileToolCallResult("gpt-test", body); ok {
		t.Fatal("latest user message without a file should not reuse historical file refs")
	}
}

func TestCodexFileRefsFromTextRejectsSentenceFragments(t *testing.T) {
	files := codexFileRefsFromText("请读取 docker-compose.yml。The model. should not read work. or 1. as files.")
	if len(files) != 1 || files[0] != "docker-compose.yml" {
		t.Fatalf("expected only docker-compose.yml, got %#v", files)
	}
}

func TestCodexFileRefsFromTextSupportsAtMentions(t *testing.T) {
	files := codexFileRefsFromText("修改 @README.md，然后检查 @notion2api/backend/configs/config.docker.yaml")
	want := []string{"README.md", "notion2api/backend/configs/config.docker.yaml"}
	if len(files) != len(want) {
		t.Fatalf("expected at mentions %#v, got %#v", want, files)
	}
	for idx := range want {
		if files[idx] != want[idx] {
			t.Fatalf("expected at mentions %#v, got %#v", want, files)
		}
	}
}

func TestMaybeBuildCodexFileToolCallResultUsesAtMentionAndNamespacedExecCommand(t *testing.T) {
	body := map[string]any{
		"input": "修改 @README.md",
		"tools": []any{
			map[string]any{"type": "function", "name": "functions.exec_command"},
		},
	}

	result, ok := maybeBuildCodexFileToolCallResult("gpt-test", body)
	if !ok {
		t.Fatal("expected @ file reference to produce a namespaced tool call")
	}
	output := result.Payload["output"].([]any)
	item := output[0].(map[string]any)
	defer pendingResponseContexts.Delete("resp:" + result.Payload["id"].(string))
	defer pendingResponseContexts.Delete("call:" + item["call_id"].(string))
	if item["name"] != "functions.exec_command" {
		t.Fatalf("expected namespaced exec tool, got %#v", item)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(item["arguments"].(string)), &args); err != nil {
		t.Fatalf("invalid arguments json: %v", err)
	}
	cmd := args["cmd"].(string)
	if !strings.Contains(cmd, "README.md") || strings.Contains(cmd, "@README.md") {
		t.Fatalf("expected @ mention to be normalized in read command, got %q", cmd)
	}
}

func TestMaybeBuildCodexFileToolCallResultHonorsToolChoiceNone(t *testing.T) {
	body := map[string]any{
		"input":       "帮我查看 [Dockerfile](Dockerfile)",
		"tool_choice": "none",
		"tools": []any{
			map[string]any{"type": "function", "name": "exec_command"},
		},
	}

	if _, ok := maybeBuildCodexFileToolCallResult("gpt-test", body); ok {
		t.Fatal("tool_choice=none should disable local file tool conversion")
	}
}

func TestMaybeBuildCodexFileToolCallResultHonorsNamedToolChoice(t *testing.T) {
	body := map[string]any{
		"input": "帮我查看 [Dockerfile](Dockerfile)",
		"tool_choice": map[string]any{
			"type": "function",
			"name": "exec_command",
		},
		"tools": []any{
			map[string]any{"type": "function", "name": "connections.fs.readFiles"},
			map[string]any{"type": "function", "name": "exec_command"},
		},
	}

	result, ok := maybeBuildCodexFileToolCallResult("gpt-test", body)
	if !ok {
		t.Fatal("expected named tool_choice to allow exec_command")
	}
	item := result.Payload["output"].([]any)[0].(map[string]any)
	defer pendingResponseContexts.Delete("resp:" + result.Payload["id"].(string))
	defer pendingResponseContexts.Delete("call:" + item["call_id"].(string))
	if item["name"] != "exec_command" {
		t.Fatalf("expected exec_command, got %#v", item)
	}
}

func TestMaybeBuildCodexFileToolCallResultSupportsParallelExecCommands(t *testing.T) {
	body := map[string]any{
		"input":               "帮我查看 [Dockerfile](Dockerfile) 和 [README.md](README.md)",
		"parallel_tool_calls": true,
		"tools": []any{
			map[string]any{"type": "function", "name": "exec_command"},
		},
	}

	result, ok := maybeBuildCodexFileToolCallResult("gpt-test", body)
	if !ok {
		t.Fatal("expected file refs to produce tool calls")
	}
	output := result.Payload["output"].([]any)
	if len(output) != 2 {
		t.Fatalf("expected two parallel tool calls, got %#v", output)
	}
	defer pendingResponseContexts.Delete("resp:" + result.Payload["id"].(string))
	for _, raw := range output {
		item := raw.(map[string]any)
		defer pendingResponseContexts.Delete("call:" + item["call_id"].(string))
		if item["name"] != "exec_command" {
			t.Fatalf("expected exec_command item, got %#v", item)
		}
	}
	if !strings.Contains(output[0].(map[string]any)["arguments"].(string), "Dockerfile") || !strings.Contains(output[1].(map[string]any)["arguments"].(string), "README.md") {
		t.Fatalf("expected both file paths in tool calls, got %#v", output)
	}
}

func TestMaybeBuildCodexFileToolCallResultLimitsWhenParallelDisabled(t *testing.T) {
	body := map[string]any{
		"input":               "帮我查看 [Dockerfile](Dockerfile) 和 [README.md](README.md)",
		"parallel_tool_calls": false,
		"tools": []any{
			map[string]any{"type": "function", "name": "exec_command"},
		},
	}

	result, ok := maybeBuildCodexFileToolCallResult("gpt-test", body)
	if !ok {
		t.Fatal("expected first file ref to produce a tool call")
	}
	output := result.Payload["output"].([]any)
	if len(output) != 1 {
		t.Fatalf("parallel_tool_calls=false should emit one tool call, got %#v", output)
	}
	item := output[0].(map[string]any)
	defer pendingResponseContexts.Delete("resp:" + result.Payload["id"].(string))
	defer pendingResponseContexts.Delete("call:" + item["call_id"].(string))
	if strings.Contains(item["arguments"].(string), "README.md") {
		t.Fatalf("expected only first file in arguments, got %#v", item)
	}
}

func TestMaybeBuildCodexFileToolCallResultUsesPlainFilename(t *testing.T) {
	body := map[string]any{
		"input": []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "帮我查看 Dockerfile 的内容"},
				},
			},
		},
		"tools": []any{
			map[string]any{"type": "function", "name": "connections.fs.readFiles"},
		},
	}

	result, ok := maybeBuildCodexFileToolCallResult("gpt-test", body)
	if !ok {
		t.Fatal("expected plain Dockerfile reference to produce a tool call")
	}
	output := result.Payload["output"].([]any)
	item := output[0].(map[string]any)
	defer pendingResponseContexts.Delete("resp:" + result.Payload["id"].(string))
	defer pendingResponseContexts.Delete("call:" + item["call_id"].(string))
	if item["type"] != "function_call" || item["name"] != "connections.fs.readFiles" {
		t.Fatalf("unexpected function call item %#v", item)
	}
	var args map[string][]string
	if err := json.Unmarshal([]byte(item["arguments"].(string)), &args); err != nil {
		t.Fatalf("invalid arguments json: %v", err)
	}
	if len(args["files"]) != 1 || args["files"][0] != "Dockerfile" {
		t.Fatalf("expected Dockerfile read args, got %#v", args)
	}
}

func TestMaybeBuildCodexAssistantTextToolCallResultConvertsHallucinatedCall(t *testing.T) {
	body := map[string]any{
		"input": []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "帮我查看 Dockerfile 的内容"},
				},
			},
		},
		"tools": []any{
			map[string]any{"type": "function", "name": "connections.fs.readFiles"},
		},
	}
	answer := `我function":"connections.fs.readFiles","args":{"files":["Dockerfile"]}}`

	result, ok := maybeBuildCodexAssistantTextToolCallResult("gpt-test", body, answer, map[string]any{"thread_id": "thread-1"})
	if !ok {
		t.Fatal("expected assistant text function call to be converted")
	}
	output := result.Payload["output"].([]any)
	item := output[0].(map[string]any)
	defer pendingResponseContexts.Delete("resp:" + result.Payload["id"].(string))
	defer pendingResponseContexts.Delete("call:" + item["call_id"].(string))
	if item["type"] != "function_call" || item["name"] != "connections.fs.readFiles" {
		t.Fatalf("unexpected function call item %#v", item)
	}
	var args map[string][]string
	if err := json.Unmarshal([]byte(item["arguments"].(string)), &args); err != nil {
		t.Fatalf("invalid arguments json: %v", err)
	}
	if len(args["files"]) != 1 || args["files"][0] != "Dockerfile" {
		t.Fatalf("expected Dockerfile read args, got %#v", args)
	}
	if result.Payload["output_text"] != "" {
		t.Fatalf("function call response should not include assistant text: %#v", result.Payload["output_text"])
	}
}

func TestMaybeBuildCodexAssistantTextToolCallResultConvertsApplyPatch(t *testing.T) {
	body := map[string]any{
		"input": []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "帮我修改 [README.md](README.md)"},
				},
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_read",
				"output":  "old content",
			},
		},
		"tools": []any{
			map[string]any{"type": "custom", "name": "apply_patch"},
		},
	}
	answer := "我不能直接写文件，可以应用这个补丁：\n```diff\n*** Begin Patch\n*** Update File: README.md\n@@\n-old\n+new\n*** End Patch\n```"

	result, ok := maybeBuildCodexAssistantTextToolCallResult("gpt-test", body, answer, map[string]any{"thread_id": "thread-1"})
	if !ok {
		t.Fatal("expected assistant patch text to be converted")
	}
	output := result.Payload["output"].([]any)
	item := output[0].(map[string]any)
	defer pendingResponseContexts.Delete("resp:" + result.Payload["id"].(string))
	defer pendingResponseContexts.Delete("call:" + item["call_id"].(string))
	if item["type"] != "custom_tool_call" || item["name"] != "apply_patch" {
		t.Fatalf("unexpected custom tool call item %#v", item)
	}
	input := item["input"].(string)
	for _, want := range []string{"*** Begin Patch", "*** Update File: README.md", "-old", "+new", "*** End Patch"} {
		if !strings.Contains(input, want) {
			t.Fatalf("expected input to contain %q, got %q", want, input)
		}
	}
	if result.Payload["output_text"] != "" {
		t.Fatalf("custom tool call response should not include assistant text: %#v", result.Payload["output_text"])
	}
}

func TestMaybeBuildCodexAssistantTextToolCallResultNormalizesApplyPatchHunkHeader(t *testing.T) {
	body := map[string]any{
		"input": "帮我更新 README.md",
		"tools": []any{
			map[string]any{"type": "custom", "name": "apply_patch"},
		},
	}
	answer := "*** Begin Patch\n*** Update File: README.md\n@@ -1,2 +1,2 @@\n-old\n+new\n*** End Patch"

	result, ok := maybeBuildCodexAssistantTextToolCallResult("gpt-test", body, answer, nil)
	if !ok {
		t.Fatal("expected assistant patch text to be converted")
	}
	item := result.Payload["output"].([]any)[0].(map[string]any)
	defer pendingResponseContexts.Delete("resp:" + result.Payload["id"].(string))
	defer pendingResponseContexts.Delete("call:" + item["call_id"].(string))
	input := item["input"].(string)
	if strings.Contains(input, "@@ -1,2 +1,2 @@") {
		t.Fatalf("standard unified hunk header should be normalized for Codex apply_patch: %q", input)
	}
	for _, want := range []string{"*** Begin Patch", "*** Update File: README.md", "@@\n", "-old", "+new", "*** End Patch"} {
		if !strings.Contains(input, want) {
			t.Fatalf("expected normalized patch to contain %q, got %q", want, input)
		}
	}
}

func TestMaybeBuildCodexAssistantTextToolCallResultConvertsNamespacedApplyPatch(t *testing.T) {
	body := map[string]any{
		"input": "修改 @README.md",
		"tools": []any{
			map[string]any{"type": "custom", "name": "functions.apply_patch"},
		},
		"tool_choice": map[string]any{"type": "custom", "name": "apply_patch"},
	}
	answer := "*** Begin Patch\n*** Update File: README.md\n@@\n-old\n+new\n*** End Patch"

	result, ok := maybeBuildCodexAssistantTextToolCallResult("gpt-test", body, answer, nil)
	if !ok {
		t.Fatal("expected namespaced apply_patch text to be converted")
	}
	item := result.Payload["output"].([]any)[0].(map[string]any)
	defer pendingResponseContexts.Delete("resp:" + result.Payload["id"].(string))
	defer pendingResponseContexts.Delete("call:" + item["call_id"].(string))
	if item["type"] != "custom_tool_call" || item["name"] != "functions.apply_patch" {
		t.Fatalf("expected namespaced custom tool call, got %#v", item)
	}
	if !responsesInputHasApplyPatchToolCall([]any{item}) {
		t.Fatalf("expected namespaced apply_patch call to be detected")
	}
}

func TestMaybeBuildCodexDeterministicEditToolCallResultConvertsYamlPortChange(t *testing.T) {
	body := map[string]any{
		"input": []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "user: [config.docker.yaml](notion2api/backend/configs/config.docker.yaml) 帮我尝试将 server prot 改为9999"},
				},
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_read",
				"output":  "server:\n  host: 0.0.0.0\n  port: 9998\n\nsecurity:\n  admin_token_ttl_hours: 24\n",
			},
		},
		"tools": []any{
			map[string]any{"type": "custom", "name": "apply_patch"},
		},
	}

	result, ok := maybeBuildCodexDeterministicEditToolCallResult("gpt-test", body)
	if !ok {
		t.Fatal("expected deterministic YAML port edit to produce apply_patch")
	}
	item := result.Payload["output"].([]any)[0].(map[string]any)
	defer pendingResponseContexts.Delete("resp:" + result.Payload["id"].(string))
	defer pendingResponseContexts.Delete("call:" + item["call_id"].(string))
	if item["type"] != "custom_tool_call" || item["name"] != "apply_patch" {
		t.Fatalf("unexpected custom tool call item %#v", item)
	}
	input := item["input"].(string)
	for _, want := range []string{
		"*** Begin Patch",
		"*** Update File: notion2api/backend/configs/config.docker.yaml",
		" server:",
		"   host: 0.0.0.0",
		"-  port: 9998",
		"+  port: 9999",
		"*** End Patch",
	} {
		if !strings.Contains(input, want) {
			t.Fatalf("expected patch to contain %q, got %q", want, input)
		}
	}
}

func TestMaybeBuildCodexDeterministicEditToolCallResultAllowsNewEditAfterApplyPatchOutput(t *testing.T) {
	body := map[string]any{
		"input": []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "user: [config.docker.yaml](notion2api/backend/configs/config.docker.yaml) 帮我尝试将 server prot 改为9999"},
				},
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_read",
				"output":  "server:\n  host: 0.0.0.0\n  port: 9999\n\nsecurity:\n  admin_token_ttl_hours: 24\n",
			},
			map[string]any{
				"type":    "custom_tool_call",
				"call_id": "call_patch",
				"name":    "apply_patch",
				"input":   "*** Begin Patch\n*** End Patch\n",
			},
			map[string]any{
				"type":    "custom_tool_call_output",
				"call_id": "call_patch",
				"output":  "Success. Updated the following files:\nM notion2api/backend/configs/config.docker.yaml",
			},
			map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "output_text", "text": "修改已完成：notion2api/backend/configs/config.docker.yaml"},
				},
			},
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "改回9998"},
				},
			},
		},
		"tools": []any{
			map[string]any{"type": "custom", "name": "apply_patch"},
		},
	}

	result, ok := maybeBuildCodexDeterministicEditToolCallResult("gpt-test", body)
	if !ok {
		t.Fatal("expected follow-up edit after apply_patch output to produce apply_patch")
	}
	item := result.Payload["output"].([]any)[0].(map[string]any)
	defer pendingResponseContexts.Delete("resp:" + result.Payload["id"].(string))
	defer pendingResponseContexts.Delete("call:" + item["call_id"].(string))
	input := item["input"].(string)
	for _, want := range []string{"*** Update File: notion2api/backend/configs/config.docker.yaml", "-  port: 9999", "+  port: 9998"} {
		if !strings.Contains(input, want) {
			t.Fatalf("expected follow-up patch to contain %q, got %q", want, input)
		}
	}
}

func TestMaybeBuildCodexAssistantTextToolCallResultConvertsUnifiedDiffPatch(t *testing.T) {
	body := map[string]any{
		"input": "帮我更新 README.md",
		"tools": []any{
			map[string]any{"type": "custom", "name": "apply_patch"},
		},
	}
	answer := "```diff\ndiff --git a/README.md b/README.md\n--- a/README.md\n+++ b/README.md\n@@ -1,2 +1,2 @@\n-old\n+new\n```"

	result, ok := maybeBuildCodexAssistantTextToolCallResult("gpt-test", body, answer, nil)
	if !ok {
		t.Fatal("expected unified diff patch to be converted")
	}
	item := result.Payload["output"].([]any)[0].(map[string]any)
	defer pendingResponseContexts.Delete("resp:" + result.Payload["id"].(string))
	defer pendingResponseContexts.Delete("call:" + item["call_id"].(string))
	input := item["input"].(string)
	for _, want := range []string{"*** Begin Patch", "*** Update File: README.md", "@@\n", "-old", "+new", "*** End Patch"} {
		if !strings.Contains(input, want) {
			t.Fatalf("expected converted patch to contain %q, got %q", want, input)
		}
	}
	if strings.Contains(input, "@@ -1,2 +1,2 @@") {
		t.Fatalf("standard unified hunk header should be normalized for Codex apply_patch: %q", input)
	}
	if strings.Contains(input, "diff --git") || strings.Contains(input, "--- a/") || strings.Contains(input, "+++ b/") {
		t.Fatalf("converted patch should not keep unified diff headers: %q", input)
	}
}

func TestMaybeBuildCodexAssistantTextToolCallResultConvertsHeadingInsertionDiff(t *testing.T) {
	body := map[string]any{
		"input": "帮我修改 README.md，在 Responses 小节下面加说明",
		"tools": []any{
			map[string]any{"type": "custom", "name": "apply_patch"},
		},
	}
	answer := "我这边不能直接改你本地仓库文件，但可以给你最小补丁。建议把这段加在 `README.md` 的 `### Responses` 小节标题下面：\n```diff\n### Responses\n+\n+> 注意：当前暂未原生支持 OpenAI Responses 格式。如果客户端需要使用 Responses API，可以先通过 LiteLLM 将当前 Chat Completions 格式转换为 Responses 格式。\n```"

	result, ok := maybeBuildCodexAssistantTextToolCallResult("gpt-test", body, answer, nil)
	if !ok {
		t.Fatal("expected heading insertion diff to be converted")
	}
	item := result.Payload["output"].([]any)[0].(map[string]any)
	defer pendingResponseContexts.Delete("resp:" + result.Payload["id"].(string))
	defer pendingResponseContexts.Delete("call:" + item["call_id"].(string))
	input := item["input"].(string)
	for _, want := range []string{"*** Begin Patch", "*** Update File: README.md", " ### Responses", "+> 注意：当前暂未原生支持 OpenAI Responses 格式", "*** End Patch"} {
		if !strings.Contains(input, want) {
			t.Fatalf("expected converted patch to contain %q, got %q", want, input)
		}
	}
}

func TestMaybeBuildCodexAssistantTextToolCallResultConvertsMarkdownInsertionBlock(t *testing.T) {
	body := map[string]any{
		"input": "帮我更新 README.md 的 Responses 小节",
		"tools": []any{
			map[string]any{"type": "custom", "name": "apply_patch"},
		},
	}
	answer := "可以按下面这段加在 README 的 `### Responses` 小节标题下面：\n```md\n> 注意：当前 `/v1/responses` 只提供基础兼容入口，暂未完整支持 OpenAI Responses 的原生请求 / 响应格式。现阶段建议先使用 LiteLLM 将现有 Chat Completions 格式转换为 Responses 格式，再接入需要 Responses API 的客户端。\n```"

	result, ok := maybeBuildCodexAssistantTextToolCallResult("gpt-test", body, answer, nil)
	if !ok {
		t.Fatal("expected markdown insertion block to be converted")
	}
	item := result.Payload["output"].([]any)[0].(map[string]any)
	defer pendingResponseContexts.Delete("resp:" + result.Payload["id"].(string))
	defer pendingResponseContexts.Delete("call:" + item["call_id"].(string))
	input := item["input"].(string)
	for _, want := range []string{"*** Begin Patch", "*** Update File: README.md", " ### Responses", "+> 注意：当前 `/v1/responses`", "*** End Patch"} {
		if !strings.Contains(input, want) {
			t.Fatalf("expected converted patch to contain %q, got %q", want, input)
		}
	}
}

func TestMaybeBuildCodexAssistantTextToolCallResultSkipsAfterApplyPatchOutput(t *testing.T) {
	body := map[string]any{
		"input": []any{
			map[string]any{"type": "custom_tool_call_output", "call_id": "call_patch", "output": "Success"},
		},
		"tools": []any{
			map[string]any{"type": "custom", "name": "apply_patch"},
		},
	}
	answer := "*** Begin Patch\n*** Update File: README.md\n@@\n-old\n+new\n*** End Patch"

	if _, ok := maybeBuildCodexAssistantTextToolCallResult("gpt-test", body, answer, nil); ok {
		t.Fatal("apply_patch output follow-up should not produce another apply_patch call")
	}
}

func TestMaybeBuildCodexApplyPatchCompletionResultSkipsNotionAfterSuccess(t *testing.T) {
	body := map[string]any{
		"input": []any{
			map[string]any{
				"type":    "custom_tool_call",
				"call_id": "call_patch",
				"name":    "apply_patch",
				"input":   "*** Begin Patch\n*** Update File: notion2api/backend/configs/config.docker.yaml\n@@\n-  port: 9998\n+  port: 9999\n*** End Patch\n",
			},
			map[string]any{
				"type":    "custom_tool_call_output",
				"call_id": "call_patch",
				"output":  "Success. Updated the following files:\nM notion2api/backend/configs/config.docker.yaml",
			},
		},
		"tools": []any{
			map[string]any{"type": "custom", "name": "apply_patch"},
		},
	}

	result, ok := maybeBuildCodexApplyPatchCompletionResult("gpt-test", body)
	if !ok {
		t.Fatal("expected apply_patch output to produce local completion")
	}
	output := result.Payload["output"].([]any)
	if len(output) != 1 {
		t.Fatalf("expected one output message, got %#v", output)
	}
	item := output[0].(map[string]any)
	if item["type"] != "message" {
		t.Fatalf("expected final message item, got %#v", item)
	}
	if strings.Contains(result.Payload["output_text"].(string), "This action is not currently available") {
		t.Fatalf("local completion should not contain notion failure text: %#v", result.Payload["output_text"])
	}
	if !strings.Contains(result.Payload["output_text"].(string), "notion2api/backend/configs/config.docker.yaml") {
		t.Fatalf("expected final text to include updated file, got %#v", result.Payload["output_text"])
	}
}

func TestMaybeBuildCodexApplyPatchCompletionResultSkipsWhenUserContinuesAfterToolOutput(t *testing.T) {
	body := map[string]any{
		"input": []any{
			map[string]any{"type": "custom_tool_call", "call_id": "call_patch", "name": "apply_patch", "input": "*** Begin Patch\n*** End Patch\n"},
			map[string]any{"type": "custom_tool_call_output", "call_id": "call_patch", "output": "Success"},
			map[string]any{"type": "message", "role": "user", "content": []any{
				map[string]any{"type": "input_text", "text": "改回9998"},
			}},
		},
		"tools": []any{
			map[string]any{"type": "custom", "name": "apply_patch"},
		},
	}

	if _, ok := maybeBuildCodexApplyPatchCompletionResult("gpt-test", body); ok {
		t.Fatal("new user input after apply_patch output should not be treated as patch completion")
	}
}

func TestMaybeBuildCodexFileToolCallResultSkipsTitleRequest(t *testing.T) {
	body := map[string]any{
		"input": "Generate a concise UI title.\n\nUser prompt:\n帮我查看文件 [Dockerfile](Dockerfile) ,告诉我怎么部署的",
		"text": map[string]any{
			"format": map[string]any{
				"name": "codex_output_schema",
				"schema": map[string]any{
					"properties": map[string]any{
						"title": map[string]any{"type": "string"},
					},
				},
			},
		},
		"tools": []any{
			map[string]any{"type": "function", "name": "exec_command"},
		},
	}

	if _, ok := maybeBuildCodexFileToolCallResult("gpt-test", body); ok {
		t.Fatal("title generation request should not produce a file-read tool call")
	}
}

func TestPromptAndHiddenFromResponsesInputIncludesFunctionCallOutput(t *testing.T) {
	input := []any{
		map[string]any{
			"type":    "function_call_output",
			"call_id": "call_123",
			"output": map[string]any{
				"stdout": "FROM alpine\nCMD [\"/app/server\"]\n",
			},
		},
	}

	prompt, hiddenPrompt, hiddenMessages := promptAndHiddenFromResponsesInput(input)
	if hiddenPrompt != "" || len(hiddenMessages) != 0 {
		t.Fatalf("unexpected hidden prompt/messages: %q %#v", hiddenPrompt, hiddenMessages)
	}
	for _, want := range []string{`<tool_result call_id="call_123">`, "FROM alpine", "CMD [\"/app/server\"]"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected prompt to contain %q, got %q", want, prompt)
		}
	}
}

func TestPromptAndHiddenFromResponsesInputIncludesToolCallItems(t *testing.T) {
	input := []any{
		map[string]any{
			"type":      "function_call",
			"call_id":   "call_123",
			"name":      "exec_command",
			"arguments": `{"cmd":"sed -n '1,400p' Dockerfile"}`,
		},
		map[string]any{
			"type":    "custom_tool_call",
			"call_id": "call_patch",
			"name":    "apply_patch",
			"input":   "*** Begin Patch\n*** End Patch\n",
		},
	}

	prompt, hiddenPrompt, hiddenMessages := promptAndHiddenFromResponsesInput(input)
	if hiddenPrompt != "" || len(hiddenMessages) != 0 {
		t.Fatalf("unexpected hidden prompt/messages: %q %#v", hiddenPrompt, hiddenMessages)
	}
	for _, want := range []string{`<tool_call name="exec_command" call_id="call_123">`, `"cmd":"sed -n '1,400p' Dockerfile"`, `<tool_call name="apply_patch" call_id="call_patch">`, "*** Begin Patch"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected prompt to contain %q, got %q", want, prompt)
		}
	}
}

func TestApplyResponsesOutputLimitMarksIncomplete(t *testing.T) {
	answer := strings.Repeat("token ", 80)
	limited, details := applyResponsesOutputLimit(answer, map[string]any{"max_output_tokens": 4})
	if details == nil || details["reason"] != "max_output_tokens" {
		t.Fatalf("expected max_output_tokens incomplete details, got %#v", details)
	}
	if len([]rune(limited)) >= len([]rune(answer)) {
		t.Fatalf("expected limited answer to be shorter")
	}
}

func TestResponsesPromptPartsFromBodyMergesPendingToolContext(t *testing.T) {
	initial := map[string]any{
		"input": []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "帮我查看文件 [Dockerfile](Dockerfile) ,告诉我怎么部署的"},
				},
			},
		},
		"tools": []any{
			map[string]any{"type": "function", "name": "exec_command"},
		},
	}
	result, ok := maybeBuildCodexFileToolCallResult("gpt-test", initial)
	if !ok {
		t.Fatal("expected initial request to produce a tool call")
	}
	responseID := result.Payload["id"].(string)
	item := result.Payload["output"].([]any)[0].(map[string]any)
	callID := item["call_id"].(string)

	followUp := map[string]any{
		"previous_response_id": responseID,
		"input": []any{
			map[string]any{
				"type":    "function_call_output",
				"call_id": callID,
				"output":  "FROM alpine\nEXPOSE 9998\n",
			},
		},
	}
	prompt, _, _ := responsesPromptPartsFromBody(followUp)
	for _, want := range []string{"帮我查看文件", "<tool_result", "FROM alpine", "EXPOSE 9998"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected merged prompt to contain %q, got %q", want, prompt)
		}
	}
}
