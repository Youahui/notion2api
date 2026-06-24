package service

import (
	"context"
	"path/filepath"
	"testing"

	"notion2api/backend/internal/config"
	"notion2api/backend/internal/dto"
	"notion2api/backend/internal/model"
	"notion2api/backend/internal/repository"
)

func TestAccountSelectionStaysInsideGroup(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Database.Driver = "sqlite"
	cfg.Database.DSN = filepath.Join(t.TempDir(), "test.sqlite")
	store, err := repository.OpenDatabase(cfg.Database)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	groupA := model.AccountGroup{Name: "group-a", Enabled: true}
	groupB := model.AccountGroup{Name: "group-b", Enabled: true}
	if err := store.CreateAccountGroup(ctx, &groupA); err != nil {
		t.Fatalf("create group A: %v", err)
	}
	if err := store.CreateAccountGroup(ctx, &groupB); err != nil {
		t.Fatalf("create group B: %v", err)
	}
	accountA := model.Account{Email: "a@example.com", Enabled: true, Status: "ready", Priority: 100}
	accountB := model.Account{Email: "b@example.com", Enabled: true, Status: "ready", Priority: 1}
	if err := store.CreateAccount(ctx, &accountA); err != nil {
		t.Fatalf("create account A: %v", err)
	}
	if err := store.CreateAccount(ctx, &accountB); err != nil {
		t.Fatalf("create account B: %v", err)
	}
	if err := store.ReplaceAccountGroupMembers(ctx, groupA.ID, []model.AccountGroupMember{{AccountID: accountA.ID, Enabled: true, Priority: 100}}); err != nil {
		t.Fatalf("bind account A: %v", err)
	}
	if err := store.ReplaceAccountGroupMembers(ctx, groupB.ID, []model.AccountGroupMember{{AccountID: accountB.ID, Enabled: true, Priority: 1}}); err != nil {
		t.Fatalf("bind account B: %v", err)
	}

	accounts := NewAccountService(store, cfg)
	selected, err := accounts.SelectWithScope(ctx, AccountSelectionOptions{GroupID: &groupB.ID})
	if err != nil {
		t.Fatalf("select account: %v", err)
	}
	if selected.ID != accountB.ID {
		t.Fatalf("selected account = %d, want %d", selected.ID, accountB.ID)
	}

	_, err = accounts.SelectWithScope(ctx, AccountSelectionOptions{GroupID: &groupB.ID, PreferredEmail: accountA.Email})
	if err == nil {
		t.Fatal("expected preferred account outside group to fail")
	}
}

func TestAccountSelectionSkipsFailedAccountWithoutGroup(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Database.Driver = "sqlite"
	cfg.Database.DSN = filepath.Join(t.TempDir(), "test.sqlite")
	store, err := repository.OpenDatabase(cfg.Database)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	accountA := model.Account{Email: "a@example.com", Enabled: true, Active: true, Status: "ready", Priority: 100}
	accountB := model.Account{Email: "b@example.com", Enabled: true, Status: "ready", Priority: 1}
	if err := store.CreateAccount(ctx, &accountA); err != nil {
		t.Fatalf("create account A: %v", err)
	}
	if err := store.CreateAccount(ctx, &accountB); err != nil {
		t.Fatalf("create account B: %v", err)
	}

	accounts := NewAccountService(store, cfg)
	selected, err := accounts.SelectWithScope(ctx, AccountSelectionOptions{FailedAccountIDs: map[uint]struct{}{accountA.ID: {}}})
	if err != nil {
		t.Fatalf("select account: %v", err)
	}
	if selected.ID != accountB.ID {
		t.Fatalf("selected account = %d, want %d", selected.ID, accountB.ID)
	}
}

func TestReplaceGroupMembersPersistsDisabledMember(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.Database.Driver = "sqlite"
	cfg.Database.DSN = filepath.Join(t.TempDir(), "test.sqlite")
	store, err := repository.OpenDatabase(cfg.Database)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	group := model.AccountGroup{Name: "group-a", Enabled: true}
	if err := store.CreateAccountGroup(ctx, &group); err != nil {
		t.Fatalf("create group: %v", err)
	}
	account := model.Account{Email: "a@example.com", Enabled: true, Status: "ready"}
	if err := store.CreateAccount(ctx, &account); err != nil {
		t.Fatalf("create account: %v", err)
	}

	accounts := NewAccountService(store, cfg)
	rows, err := accounts.ReplaceGroupMembers(ctx, group.ID, dto.UpdateGroupMembersRequest{
		Members: []dto.UpdateGroupMemberRequest{{
			AccountID: account.ID,
			Enabled:   false,
			Priority:  3,
		}},
	})
	if err != nil {
		t.Fatalf("replace group members: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("member count = %d, want 1", len(rows))
	}
	if rows[0].Enabled {
		t.Fatal("member enabled persisted as true, want false")
	}
}
