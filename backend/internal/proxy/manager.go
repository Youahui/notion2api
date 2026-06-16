package proxy

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"notaion2api/backend/internal/config"
	"notaion2api/backend/internal/model"
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
	m.clients[key] = client
	return client, nil
}

func accountClientKey(account model.Account, policy Policy) string {
	accountKey := strings.TrimSpace(strings.ToLower(account.Email))
	if account.ID > 0 {
		accountKey = strconv.FormatUint(uint64(account.ID), 10)
	}
	hash := sha256.Sum256([]byte(strings.TrimSpace(account.AuthToken) + "\x00" + strings.TrimSpace(account.Cookie)))
	return fmt.Sprintf("account:%s|proxy:%s|auth:%x", accountKey, policy.CacheKey(), hash[:8])
}
