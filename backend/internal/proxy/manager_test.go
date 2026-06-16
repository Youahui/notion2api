package proxy

import (
	"testing"

	"notaion2api/backend/internal/config"
	"notaion2api/backend/internal/model"
)

func TestPolicyForAccountInheritsGlobalProxy(t *testing.T) {
	policy := PolicyForAccount(config.ProxyConfig{
		Mode: "http",
		URL:  "http://127.0.0.1:7890",
	}, model.Account{Email: "alice@example.com"})

	if policy.Mode != ModeHTTP {
		t.Fatalf("expected http mode, got %q", policy.Mode)
	}
	if got := policy.ProxyURLForScheme("https"); got != "http://127.0.0.1:7890" {
		t.Fatalf("expected inherited proxy url, got %q", got)
	}
}

func TestPolicyForAccountCanDisableGlobalProxy(t *testing.T) {
	policy := PolicyForAccount(config.ProxyConfig{
		Mode: "http",
		URL:  "http://127.0.0.1:7890",
	}, model.Account{Email: "alice@example.com", ProxyMode: "off"})

	if policy.Mode != ModeOff {
		t.Fatalf("expected off mode, got %q", policy.Mode)
	}
}

func TestPolicyForAccountPrefersAccountSchemeURLs(t *testing.T) {
	policy := PolicyForAccount(config.ProxyConfig{Mode: "off"}, model.Account{
		Email:         "alice@example.com",
		ProxyMode:     "http",
		ProxyURL:      "http://default-proxy:8080",
		ProxyHTTPSURL: "http://https-proxy:8080",
	})

	if got := policy.ProxyURLForScheme("http"); got != "http://default-proxy:8080" {
		t.Fatalf("expected default proxy for http, got %q", got)
	}
	if got := policy.ProxyURLForScheme("https"); got != "http://https-proxy:8080" {
		t.Fatalf("expected https proxy override, got %q", got)
	}
}

func TestManagerSeparatesClientsByAccountAndProxy(t *testing.T) {
	manager := NewManager(30)
	cfg := config.ProxyConfig{Mode: "http", URL: "http://127.0.0.1:7890"}
	alice := model.Account{ID: 1, Email: "alice@example.com", AuthToken: "token-a"}
	bob := model.Account{ID: 2, Email: "bob@example.com", AuthToken: "token-b"}

	aliceClient, err := manager.ClientForAccount(alice, cfg)
	if err != nil {
		t.Fatal(err)
	}
	aliceAgain, err := manager.ClientForAccount(alice, cfg)
	if err != nil {
		t.Fatal(err)
	}
	bobClient, err := manager.ClientForAccount(bob, cfg)
	if err != nil {
		t.Fatal(err)
	}
	aliceOtherProxy, err := manager.ClientForAccount(model.Account{
		ID:        alice.ID,
		Email:     alice.Email,
		AuthToken: alice.AuthToken,
		ProxyMode: "http",
		ProxyURL:  "http://127.0.0.1:8899",
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}

	if aliceClient != aliceAgain {
		t.Fatalf("expected same client for same account and proxy")
	}
	if aliceClient == bobClient {
		t.Fatalf("expected different clients for different accounts")
	}
	if aliceClient == aliceOtherProxy {
		t.Fatalf("expected different clients when account proxy changes")
	}
}

func TestParseURLAddsFallbackScheme(t *testing.T) {
	parsed, err := ParseURL("127.0.0.1:1080", ModeSOCKS5)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != ModeSOCKS5 {
		t.Fatalf("expected socks5 scheme, got %q", parsed.Scheme)
	}
}
