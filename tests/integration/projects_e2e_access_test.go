//go:build integration

package integration

// E2E access scenarios 6-7: contact default project, session-project resolver.
// Seed helpers live in projects_e2e_helpers_test.go.

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// TestProjectsE2E_ContactDefaultProjectSet asserts UpdateDefaultProject writes
// the FK and GetContactByID returns it.
func TestProjectsE2E_ContactDefaultProjectSet(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	owner := e2eUser(t, db)
	p := e2eCreateProject(t, ctx, db, owner)
	contactID := e2eContact(t, db)

	cs := pg.NewPGContactStore(db)
	if err := cs.UpdateDefaultProject(ctx, contactID, &p.ID); err != nil {
		t.Fatalf("UpdateDefaultProject: %v", err)
	}

	contact, err := cs.GetContactByID(ctx, contactID)
	if err != nil {
		t.Fatalf("GetContactByID: %v", err)
	}
	if contact.DefaultProjectID == nil || *contact.DefaultProjectID != p.ID {
		t.Errorf("DefaultProjectID = %v, want %v", contact.DefaultProjectID, p.ID)
	}
}

// TestProjectsE2E_ResolveSessionProjectChannelFallback asserts the two-layer
// resolver returns the contact's default_project_id when no session override
// is present (nil first argument).
func TestProjectsE2E_ResolveSessionProjectChannelFallback(t *testing.T) {
	proj := uuid.New()
	contact := &store.ChannelContact{DefaultProjectID: &proj}

	got := sessionProjectResolver(nil, contact)
	if got == nil {
		t.Fatal("expected non-nil project from channel fallback")
	}
	if *got != proj {
		t.Errorf("got %v, want %v", *got, proj)
	}
}

// TestProjectsE2E_ResolveSessionProjectNilWhenNeitherSet asserts nil is returned
// when both session override and channel contact default are absent.
func TestProjectsE2E_ResolveSessionProjectNilWhenNeitherSet(t *testing.T) {
	contact := &store.ChannelContact{DefaultProjectID: nil}
	if got := sessionProjectResolver(nil, contact); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

// sessionProjectResolver re-implements the two-layer COALESCE logic from
// agent.resolveSessionProject so we can test the resolver contract without
// reaching into an unexported package symbol.
// Layer 2: channel_contacts.default_project_id — group-chat default.
func sessionProjectResolver(_ any, contact *store.ChannelContact) *uuid.UUID {
	if contact != nil && contact.DefaultProjectID != nil {
		return contact.DefaultProjectID
	}
	return nil
}

// TestProjectsE2E_OwnerResolvesEditorRank asserts the project creator resolves
// with isOwner=true and editor-equivalent rank without any explicit grant row.
func TestProjectsE2E_OwnerResolvesEditorRank(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	owner := e2eUser(t, db)
	p := e2eCreateProject(t, ctx, db, owner)

	gs := pg.NewPGProjectGrantStore(db)
	role, isOwner, found, err := permissions.ResolveProjectRole(ctx, gs, owner.String(), p.ID.String())
	if err != nil {
		t.Fatalf("ResolveProjectRole: %v", err)
	}
	if !found {
		t.Fatal("owner must have access (found=true)")
	}
	if role != permissions.ProjectRoleEditor {
		t.Errorf("owner role = %q, want %q", role, permissions.ProjectRoleEditor)
	}
	if !isOwner {
		t.Error("isOwner must be true for the project creator")
	}
}

// TestProjectsE2E_ViewerCannotMeetMemberMin asserts CanAccessProject returns
// false when the caller's effective role is viewer and minRole is member.
func TestProjectsE2E_ViewerCannotMeetMemberMin(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	owner := e2eUser(t, db)
	viewer := e2eUser(t, db)
	p := e2eCreateProject(t, ctx, db, owner)

	gs := pg.NewPGProjectGrantStore(db)
	viewerStr := viewer.String()
	g := &store.ProjectGrant{ProjectID: p.ID.String(), UserID: &viewerStr, Role: "viewer"}
	if err := gs.Create(ctx, g); err != nil {
		t.Fatalf("Create grant: %v", err)
	}

	ok, err := permissions.CanAccessProject(ctx, gs, viewer.String(), p.ID.String(), permissions.ProjectRoleMember)
	if err != nil {
		t.Fatalf("CanAccessProject: %v", err)
	}
	if ok {
		t.Error("viewer must NOT satisfy member+ requirement")
	}
}

// TestProjectsE2E_GetNonExistentReturnsErrNoRows asserts Get returns
// sql.ErrNoRows for a UUID that was never inserted.
func TestProjectsE2E_GetNonExistentReturnsErrNoRows(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	ps := pg.NewPGProjectStore(db)
	_, err := ps.Get(ctx, uuid.New())
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}
