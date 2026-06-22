package proxy

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"notion2api/backend/internal/config"
	"notion2api/backend/internal/model"
)

type Manager struct {
	mu      sync.RWMutex
	timeout time.Duration
	clients map[string]*http.Client
}

func NewManager(timeoutSeconds int) *Manager {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 180
	}
	return &Manager{
		timeout: time.Duration(timeoutSeconds) * time.Second,
		clients: make(map[string]*http.Client),
	}
}

func (m *Manager) ClientForAccount(account model.Account, cfg config.ProxyConfig) (*http.Client, error) {
	if m == nil {
		return newHTTPClient(PolicyForAccount(cfg, account), 180*time.Second)
	}
	policy := PolicyForAccount(cfg, account)
	key := accountClientKey(account, policy)
	prefix := accountClientPrefix(account)

	m.mu.RLock()
	client := m.clients[key]
	m.mu.RUnlock()
	if client != nil {
		return client, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if client = m.clients[key]; client != nil {
		return client, nil
	}
	client, err := newHTTPClient(policy, m.timeout)
	if err != nil {
		return nil, err
	}
	for staleKey, staleClient := range m.clients {
		if staleKey == key || !strings.HasPrefix(staleKey, prefix) {
			continue
		}
		closeIdleConnections(staleClient)
		delete(m.clients, staleKey)
	}
	m.clients[key] = client
	return client, nil
}

func (m *Manager) Close() {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for key, client := range m.clients {
		closeIdleConnections(client)
		delete(m.clients, key)
	}
}

func closeIdleConnections(client *http.Client) {
	if client == nil {
		return
	}
	transport, ok := client.Transport.(interface{ CloseIdleConnections() })
	if ok {
		transport.CloseIdleConnections()
	}
}

func accountClientPrefix(account model.Account) string {
	return fmt.Sprintf("account:%s|", accountClientIdentity(account))
}

func accountClientIdentity(account model.Account) string {
	accountKey := strings.TrimSpace(strings.ToLower(account.Email))
	if account.ID > 0 {
		accountKey = strconv.FormatUint(uint64(account.ID), 10)
	}
	return accountKey
}

func accountClientKey(account model.Account, policy Policy) string {
	accountKey := accountClientIdentity(account)
	hash := sha256.Sum256([]byte(strings.TrimSpace(account.AuthToken) + "\x00" + strings.TrimSpace(account.Cookie)))
	return fmt.Sprintf("account:%s|proxy:%s|auth:%x", accountKey, policy.CacheKey(), hash[:8])
}
