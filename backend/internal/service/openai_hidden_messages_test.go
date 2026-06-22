package service

import (
	"encoding/json"
	"strings"
	"testing"

	"notion2api/backend/internal/config"
	"notion2api/backend/internal/dto"
	"notion2api/backend/internal/model"
)

func TestNormalizeChatMessagesExtractsSystemAndDeveloper(t *testing.T) {
	prompt, hiddenPrompt, hiddenMessages := normalizeChatMessages([]dto.ChatMessage{
		{Role: "system", Content: "Return JSON only."},
		{Role: "developer", Content: "Use terse keys."},
		{Role: "user", Content: "Give me an example."},
	})

	if strings.Contains(prompt, "Return JSON only") || strings.Contains(prompt, "Use terse keys") {
		t.Fatalf("hidden instructions leaked into visible prompt: %q", prompt)
	}
	if !strings.Contains(prompt, "user: Give me an example.") {
		t.Fatalf("expected user prompt, got %q", prompt)
	}
	if !strings.Contains(hiddenPrompt, "[system]\nReturn JSON only.") || !strings.Contains(hiddenPrompt, "[developer]\nUse terse keys.") {
		t.Fatalf("expected hidden prompt sections, got %q", hiddenPrompt)
	}
	if len(hiddenMessages) != 2 {
		t.Fatalf("expected 2 hidden messages, got %d", len(hiddenMessages))
	}
	if hiddenMessages[0].Role != "system" || hiddenMessages[1].Role != "developer" {
		t.Fatalf("unexpected hidden message roles: %#v", hiddenMessages)
	}
}

func TestNormalizeChatMessagesForNotionUsesCurrentTurnWhenContinuing(t *testing.T) {
	prompt, hiddenPrompt, hiddenMessages := normalizeChatMessagesForNotion([]dto.ChatMessage{
		{Role: "system", Content: "Return JSON only."},
		{Role: "user", Content: "第一轮"},
		{Role: "assistant", Content: "第一轮回答"},
		{Role: "user", Content: "第二轮"},
	}, true)

	if strings.Contains(prompt, "第一轮") || strings.Contains(prompt, "第一轮回答") {
		t.Fatalf("continuing thread should only send current turn, got %q", prompt)
	}
	if !strings.Contains(prompt, "user: 第二轮") {
		t.Fatalf("expected current user turn, got %q", prompt)
	}
	if !strings.Contains(hiddenPrompt, "[system]\nReturn JSON only.") || len(hiddenMessages) != 1 {
		t.Fatalf("expected hidden system instruction to be preserved, got %q %#v", hiddenPrompt, hiddenMessages)
	}
}

func TestMaybeBuildCodexChatFileToolCallResultUsesChatToolCalls(t *testing.T) {
	req := dto.ChatCompletionRequest{
		Model: "gpt-test",
		Messages: []dto.ChatMessage{
			{Role: "user", Content: "帮我查看 [Dockerfile](Dockerfile)"},
		},
		Tools: []any{
			map[string]any{"type": "function", "function": map[string]any{"name": "exec_command"}},
		},
	}

	result, ok := maybeBuildCodexChatFileToolCallResult("gpt-test", req)
	if !ok {
		t.Fatal("expected chat file request to produce tool_calls")
	}
	choices := result.Payload["choices"].([]map[string]any)
	message := choices[0]["message"].(map[string]any)
	toolCalls := message["tool_calls"].([]map[string]any)
	call := toolCalls[0]
	function := call["function"].(map[string]any)
	if function["name"] != "exec_command" {
		t.Fatalf("expected exec_command tool call, got %#v", function)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(function["arguments"].(string)), &args); err != nil {
		t.Fatalf("invalid tool arguments: %v", err)
	}
	if !strings.Contains(args["cmd"].(string), "Dockerfile") {
		t.Fatalf("expected Dockerfile command, got %#v", args)
	}
	if choices[0]["finish_reason"] != "tool_calls" {
		t.Fatalf("expected tool_calls finish reason, got %#v", choices[0])
	}
}

func TestChatStickySessionKeysMatchAnswerHistoryOnNextTurn(t *testing.T) {
	req := dto.ChatCompletionRequest{
		Model: "gpt-test",
		Messages: []dto.ChatMessage{
			{Role: "user", Content: "第一轮"},
		},
	}
	answerKeys := chatCompletionAnswerStickyKeys(req, "第一轮回答", OpenAIRequestOptions{APIKeyID: 7})
	if len(answerKeys) != 1 || answerKeys[0].Key == "" {
		t.Fatalf("expected answer history key, got %+v", answerKeys)
	}
	next := dto.ChatCompletionRequest{
		Model: "gpt-test",
		Messages: []dto.ChatMessage{
			{Role: "user", Content: "第一轮"},
			{Role: "assistant", Content: "第一轮回答"},
			{Role: "user", Content: "第二轮"},
		},
	}
	keys := chatStickySessionKeys(next, OpenAIRequestOptions{APIKeyID: 7})
	found := false
	for _, key := range keys {
		if key.Key == answerKeys[0].Key {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected next turn to include previous answer history key %q, got %+v", answerKeys[0].Key, keys)
	}
}

func TestBuildPayloadKeepsHiddenPromptOutOfTranscriptSteps(t *testing.T) {
	gateway := NewNotionGateway(config.Config{
		Features: config.FeatureConfig{PreserveSystemMessagesAsSteps: true},
	})
	payload, _ := gateway.BuildPayload(model.Account{
		Email:   "user@example.com",
		UserID:  "user-id",
		SpaceID: "space-id",
	}, NotionPromptRequest{
		Prompt:       "user: Give me an example.",
		HiddenPrompt: "[system]\nReturn JSON only.",
		HiddenMessages: []hiddenPromptMessage{
			{Role: "system", Content: "Return JSON only."},
		},
		PublicModel:  "auto",
		NotionModel:  "oatmeal-cookie",
		UseWebSearch: false,
	})

	transcript, ok := payload["transcript"].([]map[string]any)
	if !ok || len(transcript) != 3 {
		t.Fatalf("expected config/context/user transcript only, got %#v", payload["transcript"])
	}
	contextValue, ok := transcript[1]["value"].(map[string]any)
	if !ok {
		t.Fatalf("expected context value, got %#v", transcript[1]["value"])
	}
	if contextValue["instructions"] != "[system]\nReturn JSON only." {
		t.Fatalf("expected context instructions, got %#v", contextValue["instructions"])
	}
	if contextValue["runtimePromptHint"] != "[system]\nReturn JSON only." {
		t.Fatalf("expected runtimePromptHint, got %#v", contextValue["runtimePromptHint"])
	}
	if transcript[2]["type"] != "user" {
		t.Fatalf("expected user step after context, got %#v", transcript[2])
	}
}
