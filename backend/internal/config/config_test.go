package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSecurityAPIKeyPrefix(t *testing.T) {
	t.Setenv("N2A_API_KEY_PREFIX", "")

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("security:\n  api_key_prefix: \"custom_\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Security.APIKeyPrefix != "custom" {
		t.Fatalf("api key prefix = %q, want custom", cfg.Security.APIKeyPrefix)
	}
}

func TestLoadSecurityAPIKeyPrefixFromEnv(t *testing.T) {
	t.Setenv("N2A_API_KEY_PREFIX", "envkey")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Security.APIKeyPrefix != "envkey" {
		t.Fatalf("api key prefix = %q, want envkey", cfg.Security.APIKeyPrefix)
	}
}
