package service

import (
	"strings"
	"testing"

	"notaion2api/backend/internal/config"
	"notaion2api/backend/internal/dto"
	"notaion2api/backend/internal/model"
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
