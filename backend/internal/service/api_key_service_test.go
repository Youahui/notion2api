package service

import (
	"context"
	"path/filepath"
	"testing"

	"notion2api/backend/internal/config"
	"notion2api/backend/internal/dto"
	"notion2api/backend/internal/repository"
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
	if created.Secret == "" {
		t.Fatal("expected created secret to be returned once")
	}
	if created.APIKey != created.Secret {
		t.Fatalf("created api_key = %q, want returned secret", created.APIKey)
	}
	stored, err := store.GetAPIKey(ctx, created.ID)
	if err != nil {
		t.Fatalf("get stored api key: %v", err)
	}
	if stored.APIKey != created.Secret {
		t.Fatalf("stored api key plaintext = %q, want %q", stored.APIKey, created.Secret)
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

func TestUsageHydratesAPIKeyDisplayFields(t *testing.T) {
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
	created, err := service.Create(ctx, 1, dto.CreateAPIKeyRequest{Name: "Usage Key"})
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}
	if err := service.RecordUsage(ctx, UsageRecord{
		APIKeyID:         created.ID,
		Method:           "POST",
		Route:            "/v1/chat/completions",
		StatusCode:       200,
		Model:            "gpt-5.5",
		PromptTokens:     10,
		CompletionTokens: 15,
		TotalTokens:      25,
		Estimated:        true,
	}); err != nil {
		t.Fatalf("record usage: %v", err)
	}

	rows, err := service.Usage(ctx, repository.UsageFilter{Limit: 100})
	if err != nil {
		t.Fatalf("list usage: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("usage length = %d, want 1", len(rows))
	}
	got := rows[0]
	if got.APIKeyID != created.ID {
		t.Fatalf("api_key_id = %d, want %d", got.APIKeyID, created.ID)
	}
	if got.APIKeyName != "Usage Key" {
		t.Fatalf("api_key_name = %q, want Usage Key", got.APIKeyName)
	}
	if got.APIKey != "" {
		t.Fatalf("api_key = %q, want empty", got.APIKey)
	}
	wantMasked := created.KeyPrefix + "..." + created.KeySuffix
	if got.APIKeyMasked != wantMasked {
		t.Fatalf("api_key_masked = %q, want %q", got.APIKeyMasked, wantMasked)
	}
}

func TestListHydratesAPIKeyDisplayFields(t *testing.T) {
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
	created, err := service.Create(ctx, 1, dto.CreateAPIKeyRequest{Name: "List Key"})
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}
	rows, err := service.List(ctx)
	if err != nil {
		t.Fatalf("list api keys: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("api key length = %d, want 1", len(rows))
	}
	got := rows[0]
	if got.APIKey != created.Secret {
		t.Fatalf("api_key = %q, want %q", got.APIKey, created.Secret)
	}
	wantMasked := created.KeyPrefix + "..." + created.KeySuffix
	if got.APIKeyMasked != wantMasked {
		t.Fatalf("api_key_masked = %q, want %q", got.APIKeyMasked, wantMasked)
	}
}
