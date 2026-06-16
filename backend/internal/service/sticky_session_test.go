package service

import (
	"context"
	"testing"
	"time"

	"notaion2api/backend/internal/config"
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

func TestParseRedisStickyURL(t *testing.T) {
	addr, username, password, db, err := parseRedisStickyURL("redis://user:secret@localhost:6380/2")
	if err != nil {
		t.Fatalf("parse redis url: %v", err)
	}
	if addr != "localhost:6380" || username != "user" || password != "secret" || db != 2 {
		t.Fatalf("unexpected parsed redis url: addr=%q username=%q password=%q db=%d", addr, username, password, db)
	}
}
