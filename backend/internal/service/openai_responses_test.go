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
