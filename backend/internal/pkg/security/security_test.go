package security

import (
	"strings"
	"testing"
)

func TestGenerateAPIKeyUsesConfiguredPrefix(t *testing.T) {
	secret, prefix, suffix, err := GenerateAPIKey("custom")
	if err != nil {
		t.Fatalf("generate api key: %v", err)
	}
	if !strings.HasPrefix(secret, "custom_") {
		t.Fatalf("secret = %q, want custom_ prefix", secret)
	}
	wantPrefix, wantSuffix := APIKeyParts(secret)
	if prefix != wantPrefix {
		t.Fatalf("prefix = %q, want %q", prefix, wantPrefix)
	}
	if suffix != wantSuffix {
		t.Fatalf("suffix = %q, want %q", suffix, wantSuffix)
	}
}

func TestGenerateAPIKeyUsesDefaultPrefixWhenBlank(t *testing.T) {
	secret, _, _, err := GenerateAPIKey("")
	if err != nil {
		t.Fatalf("generate api key: %v", err)
	}
	if !strings.HasPrefix(secret, DefaultAPIKeyPrefix+"_") {
		t.Fatalf("secret = %q, want default prefix %q", secret, DefaultAPIKeyPrefix+"_")
	}
}
