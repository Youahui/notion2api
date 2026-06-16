package service

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"notaion2api/backend/internal/config"
)

type StickyThreadBinding struct {
	ThreadID     string    `json:"thread_id"`
	AccountID    uint      `json:"account_id"`
	AccountEmail string    `json:"account_email"`
	SpaceID      string    `json:"space_id"`
	SpaceViewID  string    `json:"space_view_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type StickySessionStore interface {
	Get(ctx context.Context, threadID string) (StickyThreadBinding, bool, error)
	Set(ctx context.Context, binding StickyThreadBinding, ttl time.Duration) error
	Delete(ctx context.Context, threadID string) error
}

func NewStickySessionStore(cfg config.StickyConfig) StickySessionStore {
	if !cfg.Enabled {
		return nil
	}
	ttl := stickyTTL(cfg)
	stores := []StickySessionStore{}
	if cfg.LocalEnabled {
		stores = append(stores, newMemoryStickySessionStore(ttl))
	}
	if redisURL := strings.TrimSpace(cfg.RedisURL); redisURL != "" {
		redisStore, err := newRedisStickySessionStore(redisURL, cfg.RedisKeyPrefix, time.Duration(cfg.RedisTimeoutSeconds*float64(time.Second)))
		if err != nil {
			slog.Warn("sticky redis disabled", "error", err)
		} else {
			stores = append(stores, redisStore)
		}
	}
	switch len(stores) {
	case 0:
		return nil
	case 1:
		return stores[0]
	default:
		return &tieredStickySessionStore{stores: stores, ttl: ttl}
	}
}

func stickyTTL(cfg config.StickyConfig) time.Duration {
	seconds := cfg.TTLSeconds
	if seconds <= 0 {
		seconds = 604800
	}
	return time.Duration(seconds) * time.Second
}

type tieredStickySessionStore struct {
	stores []StickySessionStore
	ttl    time.Duration
}

func (s *tieredStickySessionStore) Get(ctx context.Context, threadID string) (StickyThreadBinding, bool, error) {
	var lastErr error
	for idx, store := range s.stores {
		binding, ok, err := store.Get(ctx, threadID)
		if err != nil {
			lastErr = err
			continue
		}
		if !ok {
			continue
		}
		for backfill := 0; backfill < idx; backfill++ {
			_ = s.stores[backfill].Set(ctx, binding, s.ttl)
		}
		return binding, true, nil
	}
	return StickyThreadBinding{}, false, lastErr
}

func (s *tieredStickySessionStore) Set(ctx context.Context, binding StickyThreadBinding, ttl time.Duration) error {
	var lastErr error
	succeeded := false
	for _, store := range s.stores {
		if err := store.Set(ctx, binding, ttl); err != nil {
			lastErr = err
			continue
		}
		succeeded = true
	}
	if succeeded {
		return nil
	}
	return lastErr
}

func (s *tieredStickySessionStore) Delete(ctx context.Context, threadID string) error {
	var lastErr error
	succeeded := false
	for _, store := range s.stores {
		if err := store.Delete(ctx, threadID); err != nil {
			lastErr = err
			continue
		}
		succeeded = true
	}
	if succeeded {
		return nil
	}
	return lastErr
}

type memoryStickySessionStore struct {
	mu    sync.RWMutex
	items map[string]StickyThreadBinding
	ttl   time.Duration
}

func newMemoryStickySessionStore(ttl time.Duration) *memoryStickySessionStore {
	return &memoryStickySessionStore{items: make(map[string]StickyThreadBinding), ttl: ttl}
}

func (s *memoryStickySessionStore) Get(_ context.Context, threadID string) (StickyThreadBinding, bool, error) {
	key := stickyThreadKey(threadID)
	if key == "" {
		return StickyThreadBinding{}, false, nil
	}
	now := time.Now()
	s.mu.RLock()
	binding, ok := s.items[key]
	s.mu.RUnlock()
	if !ok {
		return StickyThreadBinding{}, false, nil
	}
	if !binding.ExpiresAt.IsZero() && now.After(binding.ExpiresAt) {
		s.mu.Lock()
		delete(s.items, key)
		s.mu.Unlock()
		return StickyThreadBinding{}, false, nil
	}
	return binding, true, nil
}

func (s *memoryStickySessionStore) Set(_ context.Context, binding StickyThreadBinding, ttl time.Duration) error {
	key := stickyThreadKey(firstNonEmpty(binding.ThreadID))
	if key == "" || binding.AccountID == 0 {
		return nil
	}
	now := time.Now()
	if ttl <= 0 {
		ttl = s.ttl
	}
	if binding.CreatedAt.IsZero() {
		binding.CreatedAt = now
	}
	binding.ThreadID = key
	binding.UpdatedAt = now
	binding.ExpiresAt = now.Add(ttl)
	s.mu.Lock()
	s.items[key] = binding
	s.mu.Unlock()
	return nil
}

func (s *memoryStickySessionStore) Delete(_ context.Context, threadID string) error {
	key := stickyThreadKey(threadID)
	if key == "" {
		return nil
	}
	s.mu.Lock()
	delete(s.items, key)
	s.mu.Unlock()
	return nil
}

type redisStickySessionStore struct {
	addr     string
	username string
	password string
	db       int
	prefix   string
	timeout  time.Duration
}

func newRedisStickySessionStore(rawURL string, prefix string, timeout time.Duration) (*redisStickySessionStore, error) {
	addr, username, password, db, err := parseRedisStickyURL(rawURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(prefix) == "" {
		prefix = "n2a:sticky:thread:"
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &redisStickySessionStore{
		addr:     addr,
		username: username,
		password: password,
		db:       db,
		prefix:   prefix,
		timeout:  timeout,
	}, nil
}

func (s *redisStickySessionStore) Get(ctx context.Context, threadID string) (StickyThreadBinding, bool, error) {
	key := stickyThreadKey(threadID)
	if key == "" {
		return StickyThreadBinding{}, false, nil
	}
	reply, err := s.do(ctx, "GET", s.key(key))
	if errors.Is(err, errRedisNil) {
		return StickyThreadBinding{}, false, nil
	}
	if err != nil {
		return StickyThreadBinding{}, false, err
	}
	raw, _ := reply.(string)
	if strings.TrimSpace(raw) == "" {
		return StickyThreadBinding{}, false, nil
	}
	var binding StickyThreadBinding
	if err := json.Unmarshal([]byte(raw), &binding); err != nil {
		return StickyThreadBinding{}, false, err
	}
	if !binding.ExpiresAt.IsZero() && time.Now().After(binding.ExpiresAt) {
		_ = s.Delete(ctx, key)
		return StickyThreadBinding{}, false, nil
	}
	return binding, true, nil
}

func (s *redisStickySessionStore) Set(ctx context.Context, binding StickyThreadBinding, ttl time.Duration) error {
	key := stickyThreadKey(binding.ThreadID)
	if key == "" || binding.AccountID == 0 {
		return nil
	}
	if ttl <= 0 {
		ttl = 604800 * time.Second
	}
	now := time.Now()
	if binding.CreatedAt.IsZero() {
		binding.CreatedAt = now
	}
	binding.ThreadID = key
	binding.UpdatedAt = now
	binding.ExpiresAt = now.Add(ttl)
	raw, err := json.Marshal(binding)
	if err != nil {
		return err
	}
	expireSeconds := int(ttl.Seconds())
	if expireSeconds <= 0 {
		expireSeconds = 1
	}
	_, err = s.do(ctx, "SET", s.key(key), string(raw), "EX", strconv.Itoa(expireSeconds))
	return err
}

func (s *redisStickySessionStore) Delete(ctx context.Context, threadID string) error {
	key := stickyThreadKey(threadID)
	if key == "" {
		return nil
	}
	_, err := s.do(ctx, "DEL", s.key(key))
	return err
}

func (s *redisStickySessionStore) key(threadID string) string {
	sum := sha256.Sum256([]byte(stickyThreadKey(threadID)))
	return s.prefix + hex.EncodeToString(sum[:])
}

var errRedisNil = errors.New("redis nil")

func (s *redisStickySessionStore) do(ctx context.Context, args ...string) (any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	dialer := net.Dialer{Timeout: s.timeout}
	conn, err := dialer.DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(redisDeadline(ctx, s.timeout))
	reader := bufio.NewReader(conn)
	if s.password != "" {
		authArgs := []string{"AUTH"}
		if s.username != "" {
			authArgs = append(authArgs, s.username)
		}
		authArgs = append(authArgs, s.password)
		if _, err := redisRoundTrip(conn, reader, authArgs...); err != nil {
			return nil, err
		}
	}
	if s.db > 0 {
		if _, err := redisRoundTrip(conn, reader, "SELECT", strconv.Itoa(s.db)); err != nil {
			return nil, err
		}
	}
	return redisRoundTrip(conn, reader, args...)
}

func redisRoundTrip(w io.Writer, reader *bufio.Reader, args ...string) (any, error) {
	if err := writeRedisCommand(w, args...); err != nil {
		return nil, err
	}
	return readRedisReply(reader)
}

func writeRedisCommand(w io.Writer, args ...string) error {
	if _, err := fmt.Fprintf(w, "*%d\r\n", len(args)); err != nil {
		return err
	}
	for _, arg := range args {
		if _, err := fmt.Fprintf(w, "$%d\r\n%s\r\n", len(arg), arg); err != nil {
			return err
		}
	}
	return nil
}

func readRedisReply(reader *bufio.Reader) (any, error) {
	prefix, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	line, err := readRedisLine(reader)
	if err != nil {
		return nil, err
	}
	switch prefix {
	case '+':
		return line, nil
	case '-':
		return nil, fmt.Errorf("redis error: %s", line)
	case ':':
		return strconv.ParseInt(line, 10, 64)
	case '$':
		size, err := strconv.Atoi(line)
		if err != nil {
			return nil, err
		}
		if size < 0 {
			return nil, errRedisNil
		}
		buf := make([]byte, size+2)
		if _, err := io.ReadFull(reader, buf); err != nil {
			return nil, err
		}
		return string(buf[:size]), nil
	case '*':
		size, err := strconv.Atoi(line)
		if err != nil {
			return nil, err
		}
		if size < 0 {
			return nil, errRedisNil
		}
		out := make([]any, 0, size)
		for i := 0; i < size; i++ {
			item, err := readRedisReply(reader)
			if err != nil {
				return nil, err
			}
			out = append(out, item)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported redis reply prefix %q", string(prefix))
	}
}

func readRedisLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
}

func redisDeadline(ctx context.Context, timeout time.Duration) time.Time {
	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		return ctxDeadline
	}
	return deadline
}

func parseRedisStickyURL(raw string) (addr string, username string, password string, db int, err error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return "", "", "", 0, fmt.Errorf("redis_url is empty")
	}
	if !strings.Contains(clean, "://") {
		return addDefaultRedisPort(clean), "", "", 0, nil
	}
	parsed, err := url.Parse(clean)
	if err != nil {
		return "", "", "", 0, err
	}
	if parsed.Scheme != "redis" {
		return "", "", "", 0, fmt.Errorf("unsupported redis scheme %q", parsed.Scheme)
	}
	addr = addDefaultRedisPort(parsed.Host)
	if parsed.User != nil {
		username = parsed.User.Username()
		password, _ = parsed.User.Password()
	}
	if path := strings.Trim(strings.TrimSpace(parsed.Path), "/"); path != "" {
		db, err = strconv.Atoi(path)
		if err != nil {
			return "", "", "", 0, fmt.Errorf("invalid redis db %q: %w", path, err)
		}
	}
	return addr, username, password, db, nil
}

func addDefaultRedisPort(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "127.0.0.1:6379"
	}
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr
	}
	return addr + ":6379"
}

func stickyThreadKey(threadID string) string {
	return strings.ToLower(strings.TrimSpace(threadID))
}
