package service

import (
	"context"
	"testing"
	"time"

	"notaion2api/backend/internal/config"
	"notaion2api/backend/internal/model"
	"notaion2api/backend/internal/repository"
)

func TestNewStickySessionStoreDefaultsToLocalCache(t *testing.T) {
	store := NewStickySessionStore(config.Default().Sticky)
	if store == nil {
		t.Fatal("expected default sticky store")
	}
	if _, ok := store.(*memoryStickySessionStore); !ok {
		t.Fatalf("expected default sticky store to use local cache, got %T", store)
	}
}

func TestMemoryStickySessionStoreBindsAndExpires(t *testing.T) {
	store := newMemoryStickySessionStore(50 * time.Millisecond)
	ctx := context.Background()

	err := store.Set(ctx, StickyThreadBinding{
		ThreadID:     "Thread-1",
		AccountID:    42,
		AccountEmail: "owner@example.com",
		SpaceID:      "space-1",
	}, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("set sticky binding: %v", err)
	}

	got, ok, err := store.Get(ctx, "thread-1")
	if err != nil {
		t.Fatalf("get sticky binding: %v", err)
	}
	if !ok {
		t.Fatal("expected sticky binding hit")
	}
	if got.AccountID != 42 || got.AccountEmail != "owner@example.com" || got.SpaceID != "space-1" {
		t.Fatalf("unexpected sticky binding: %+v", got)
	}

	time.Sleep(80 * time.Millisecond)
	_, ok, err = store.Get(ctx, "thread-1")
	if err != nil {
		t.Fatalf("get expired sticky binding: %v", err)
	}
	if ok {
		t.Fatal("expected sticky binding to expire")
	}
}

func TestMemoryStickySessionStoreAliasPreservesThreadID(t *testing.T) {
	store := newMemoryStickySessionStore(time.Minute)
	ctx := context.Background()

	err := store.Set(ctx, StickyThreadBinding{
		StickyKey:    "client:k7:header_session_id:sess-1",
		ThreadID:     "notion-thread-1",
		AccountID:    42,
		AccountEmail: "owner@example.com",
	}, time.Minute)
	if err != nil {
		t.Fatalf("set sticky alias: %v", err)
	}

	got, ok, err := store.Get(ctx, "client:k7:header_session_id:sess-1")
	if err != nil {
		t.Fatalf("get sticky alias: %v", err)
	}
	if !ok {
		t.Fatal("expected sticky alias hit")
	}
	if got.ThreadID != "notion-thread-1" || got.StickyKey != "client:k7:header_session_id:sess-1" {
		t.Fatalf("sticky alias lost thread/key: %+v", got)
	}
}

func TestOpenAIServiceClearStickyThreadBindingsDeletesThreadAndAliases(t *testing.T) {
	sticky := newMemoryStickySessionStore(time.Minute)
	ctx := context.Background()
	alias := clientStickyKey(7, "prompt_cache_key", "cache-1")
	for _, binding := range []StickyThreadBinding{
		{ThreadID: "notion-thread-1", AccountID: 42},
		{StickyKey: alias.Key, ThreadID: "notion-thread-1", AccountID: 42},
	} {
		if err := sticky.Set(ctx, binding, time.Minute); err != nil {
			t.Fatalf("set sticky binding: %v", err)
		}
	}

	svc := &OpenAIService{sticky: sticky}
	svc.clearStickyThreadBindings(ctx, "notion-thread-1", []stickySessionKey{alias})

	if _, ok, err := sticky.Get(ctx, "notion-thread-1"); err != nil || ok {
		t.Fatalf("expected thread binding to be cleared, ok=%v err=%v", ok, err)
	}
	if _, ok, err := sticky.Get(ctx, alias.Key); err != nil || ok {
		t.Fatalf("expected alias binding to be cleared, ok=%v err=%v", ok, err)
	}
}

func TestOpenAIServiceSelectAccountForClientSessionBinding(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Database.DSN = ":memory:"
	store, err := repository.OpenDatabase(cfg.Database)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	account := model.Account{Email: "owner@example.com", Enabled: true, Status: "ready"}
	if err := store.CreateAccount(ctx, &account); err != nil {
		t.Fatalf("create account: %v", err)
	}
	sticky := newMemoryStickySessionStore(time.Minute)
	sessionKey := clientStickyKey(7, "header_session_id", "sess-1")
	if err := sticky.Set(ctx, StickyThreadBinding{
		StickyKey:    sessionKey.Key,
		ThreadID:     "notion-thread-1",
		AccountID:    account.ID,
		AccountEmail: account.Email,
	}, time.Minute); err != nil {
		t.Fatalf("set sticky alias: %v", err)
	}
	svc := &OpenAIService{cfg: cfg, accounts: NewAccountService(store, cfg), sticky: sticky}

	selection, err := svc.selectAccountForStickyKeys(ctx, "", "", []stickySessionKey{sessionKey})
	if err != nil {
		t.Fatalf("select account: %v", err)
	}
	if !selection.StickyHit || selection.Source != "header_session_id" {
		t.Fatalf("expected sticky hit, got %+v", selection)
	}
	if selection.Account.ID != account.ID || selection.ThreadID != "notion-thread-1" {
		t.Fatalf("unexpected sticky selection: %+v", selection)
	}
}

func TestResponseStickySessionKeysPrioritizePreviousResponseID(t *testing.T) {
	keys := responseStickySessionKeys(map[string]any{
		"previous_response_id": "resp_prev",
		"conversation_id":      "conv-body",
		"metadata":             map[string]any{"session_id": "meta-session"},
	}, OpenAIRequestOptions{
		APIKeyID:             7,
		HeaderSessionID:      "sess-header",
		HeaderConversationID: "conv-header",
	})
	if len(keys) < 4 {
		t.Fatalf("expected several sticky keys, got %+v", keys)
	}
	if keys[0].Key != "response:k7:resp_prev" || keys[0].Source != "previous_response_id" {
		t.Fatalf("previous_response_id should be first, got %+v", keys[0])
	}
	if keys[1].Key != "client:k7:header_session_id:sess-header" {
		t.Fatalf("header session key should be isolated by api key, got %+v", keys[1])
	}
}

func TestParseRedisStickyURL(t *testing.T) {
	addr, username, password, db, err := parseRedisStickyURL("redis://user:secret@localhost:6380/2")
	if err != nil {
		t.Fatalf("parse redis url: %v", err)
	}
	if addr != "localhost:6380" || username != "user" || password != "secret" || db != 2 {
		t.Fatalf("unexpected parsed redis url: addr=%q username=%q password=%q db=%d", addr, username, password, db)
	}
}
