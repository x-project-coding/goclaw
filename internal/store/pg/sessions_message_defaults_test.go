package pg

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestAddMessageStampsIDAndPreservesSenderMetadata(t *testing.T) {
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	key := "agent:test:ws:group:chat-1"
	session := &store.SessionData{Key: key, Messages: []providers.Message{}}
	s := &PGSessionStore{cache: map[string]*store.SessionData{sessionCacheKey(ctx, key): session}}

	s.AddMessage(ctx, key, providers.Message{
		Role:       "user",
		Content:    "hello",
		SenderID:   "user-1",
		SenderName: "Alice",
	})

	if len(session.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1", len(session.Messages))
	}
	msg := session.Messages[0]
	if msg.ID == "" {
		t.Fatal("Message.ID is empty")
	}
	if _, err := uuid.Parse(msg.ID); err != nil {
		t.Fatalf("Message.ID is not a UUID: %v", err)
	}
	if msg.CreatedAt == nil {
		t.Fatal("CreatedAt is nil")
	}
	if msg.SenderID != "user-1" {
		t.Fatalf("SenderID = %q, want user-1", msg.SenderID)
	}
	if msg.SenderName != "Alice" {
		t.Fatalf("SenderName = %q, want Alice", msg.SenderName)
	}
}

func TestAddMessagePreservesExistingID(t *testing.T) {
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	key := "agent:test:ws:group:chat-1"
	session := &store.SessionData{Key: key, Messages: []providers.Message{}}
	s := &PGSessionStore{cache: map[string]*store.SessionData{sessionCacheKey(ctx, key): session}}
	existingID := uuid.NewString()

	s.AddMessage(ctx, key, providers.Message{ID: existingID, Role: "user", Content: "hello"})

	if got := session.Messages[0].ID; got != existingID {
		t.Fatalf("Message.ID = %q, want existing id %q", got, existingID)
	}
}
