package proxy

import (
	"net/http"
	"sync/atomic"
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

func TestManagerEvictsStaleClientsForSameAccount(t *testing.T) {
	manager := NewManager(30)
	cfg := config.ProxyConfig{Mode: "http", URL: "http://127.0.0.1:7890"}
	account := model.Account{ID: 1, Email: "alice@example.com", AuthToken: "token-a"}

	first, err := manager.ClientForAccount(account, cfg)
	if err != nil {
		t.Fatal(err)
	}

	account.AuthToken = "token-b"
	second, err := manager.ClientForAccount(account, cfg)
	if err != nil {
		t.Fatal(err)
	}

	if first == second {
		t.Fatalf("expected a new client after auth changes")
	}
	if got := len(manager.clients); got != 1 {
		t.Fatalf("expected stale clients to be evicted, got %d cached clients", got)
	}
}

func TestManagerCloseClosesIdleConnectionsAndClearsCache(t *testing.T) {
	manager := &Manager{
		timeout: 30,
		clients: map[string]*http.Client{
			"one": {Transport: &stubCloseIdleTransport{}},
			"two": {Transport: &stubCloseIdleTransport{}},
		},
	}

	transportOne := manager.clients["one"].Transport.(*stubCloseIdleTransport)
	transportTwo := manager.clients["two"].Transport.(*stubCloseIdleTransport)

	manager.Close()

	if got := transportOne.closeCalls.Load(); got != 1 {
		t.Fatalf("expected first transport CloseIdleConnections once, got %d", got)
	}
	if got := transportTwo.closeCalls.Load(); got != 1 {
		t.Fatalf("expected second transport CloseIdleConnections once, got %d", got)
	}
	if got := len(manager.clients); got != 0 {
		t.Fatalf("expected cache to be cleared, got %d entries", got)
	}
}

type stubCloseIdleTransport struct {
	closeCalls atomic.Int32
}

func (s *stubCloseIdleTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, nil
}

func (s *stubCloseIdleTransport) CloseIdleConnections() {
	s.closeCalls.Add(1)
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
