//go:build e2e

// Package stores_test exercises the v4 store layer end-to-end against real PG18.
// PR-05A scope: users + user_sessions + skill_versions + curator_runs + user_hook_budget.
package stores_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/tests/e2e/helpers"
)

// TestUsersCRUD exercises Create/Get/GetByEmail/List/Update/Delete.
// Covers the partial unique index `users_only_one_root` indirectly by NOT writing
// a second root — concurrent-root coverage lives with the auth bootstrap tests.
func TestUsersCRUD(t *testing.T) {
	helpers.ResetDB(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	s := pg.NewPGUsersStore(helpers.MustDB(t))

	email := helpers.RandEmail("crud")
	u := &store.User{
		Email:        email,
		DisplayName:  ptrStr("CRUD User"),
		PasswordHash: "argon2id$placeholder$opaque",
		Role:         "member",
		Status:       "active",
	}
	if err := s.Create(ctx, u); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if u.ID == uuid.Nil {
		t.Fatalf("Create: ID not populated")
	}

	got, err := s.Get(ctx, u.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Email != email {
		t.Fatalf("Get email mismatch: %q vs %q", got.Email, email)
	}
	if got.Role != "member" {
		t.Fatalf("Get role mismatch: %q", got.Role)
	}

	byEmail, err := s.GetByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetByEmail: %v", err)
	}
	if byEmail.ID != u.ID {
		t.Fatalf("GetByEmail id mismatch")
	}

	beforeUpdate := got.UpdatedAt
	time.Sleep(5 * time.Millisecond) // ensure now() advances past beforeUpdate
	if err := s.Update(ctx, u.ID, map[string]any{
		"display_name": "Updated Name",
		"status":       "suspended",
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ = s.Get(ctx, u.ID)
	if got.DisplayName == nil || *got.DisplayName != "Updated Name" {
		t.Fatalf("Update display_name not persisted: got %v", got.DisplayName)
	}
	if got.Status != "suspended" {
		t.Fatalf("Update status not persisted: %q", got.Status)
	}
	if !got.UpdatedAt.After(beforeUpdate) {
		t.Fatalf("Update did not advance updated_at: before=%v after=%v",
			beforeUpdate, got.UpdatedAt)
	}

	list, err := s.List(ctx, 100, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, x := range list {
		if x.ID == u.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("List did not contain created user")
	}

	if err := s.Delete(ctx, u.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, u.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get after Delete: want ErrNotFound, got %v", err)
	}
}

// TestUsersEmailUnique verifies UNIQUE(email) constraint surfaces as an error.
func TestUsersEmailUnique(t *testing.T) {
	helpers.ResetDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	s := pg.NewPGUsersStore(helpers.MustDB(t))
	email := helpers.RandEmail("uniq")
	first := &store.User{
		Email:        email,
		PasswordHash: "argon2id$x",
		Role:         "member",
		Status:       "active",
	}
	if err := s.Create(ctx, first); err != nil {
		t.Fatalf("Create first: %v", err)
	}
	dup := &store.User{
		Email:        email,
		PasswordHash: "argon2id$y",
		Role:         "member",
		Status:       "active",
	}
	if err := s.Create(ctx, dup); err == nil {
		t.Fatalf("Create dup: want UNIQUE error, got nil")
	}
}

func ptrStr(s string) *string { return &s }
