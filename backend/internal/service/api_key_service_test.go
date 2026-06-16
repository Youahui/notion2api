package service

import (
	"context"
	"path/filepath"
	"testing"

	"notaion2api/backend/internal/config"
	"notaion2api/backend/internal/dto"
	"notaion2api/backend/internal/repository"
)

func TestLogsHydratesAPIKeyDisplayFields(t *testing.T) {
	ctx := context.Background()
	store, err := repository.OpenDatabase(config.DatabaseConfig{
		Driver: "sqlite",
		DSN:    filepath.Join(t.TempDir(), "test.sqlite"),
	})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	service := NewAPIKeyService(store, "test")
	created, err := service.Create(ctx, 1, dto.CreateAPIKeyRequest{Name: "Primary Key"})
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}
	if err := service.RecordUsage(ctx, UsageRecord{
		APIKeyID:     created.ID,
		Method:       "POST",
		Route:        "/v1/chat/completions",
		StatusCode:   200,
		LatencyMs:    123,
		Model:        "gpt-5.5",
		TotalTokens:  42,
		ErrorSummary: "",
	}); err != nil {
		t.Fatalf("record usage: %v", err)
	}

	logs, err := service.Logs(ctx, 100)
	if err != nil {
		t.Fatalf("list logs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("logs length = %d, want 1", len(logs))
	}
	got := logs[0]
	if got.APIKeyID == nil || *got.APIKeyID != created.ID {
		t.Fatalf("api_key_id = %v, want %d", got.APIKeyID, created.ID)
	}
	if got.APIKeyName != "Primary Key" {
		t.Fatalf("api_key_name = %q, want Primary Key", got.APIKeyName)
	}
	wantMasked := created.KeyPrefix + "..." + created.KeySuffix
	if got.APIKeyMasked != wantMasked {
		t.Fatalf("api_key_masked = %q, want %q", got.APIKeyMasked, wantMasked)
	}
}
