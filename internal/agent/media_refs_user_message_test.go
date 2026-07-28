package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/pipeline"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// ─── 42bucks fork patch: user messages persist media_refs ─────────────────
//
// enrichInputMedia persists uploaded media and returns the current-turn refs,
// but makeFlushMessages used to build the persisted user message from
// req.Message alone — so session history had only the literal <media:*> tags
// and no media_refs, and the chat UI could never render the attachment again
// after a reload (it fell back to a filename placeholder chip).

func TestMakeFlushMessages_UserMessageCarriesMediaRefs(t *testing.T) {
	rec := &recordingSessionStore{}
	l := &Loop{sessions: rec}
	req := &RunRequest{
		MessageID:        "msg-1",
		MessageCreatedAt: time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
		Message:          "<media:image>\n\nWhat is on this picture?",
		SenderID:         "u1",
	}

	flush := l.makeFlushMessages(req)
	// The context stage (makeEnrichMedia) runs before the first flush and
	// captures the persisted refs onto the request.
	refs := []providers.MediaRef{
		{ID: "m1", MimeType: "image/jpeg", Kind: "image", Path: "/ws/.uploads/shot-abc123.jpg"},
		{ID: "m2", MimeType: "application/pdf", Kind: "document", Path: "/ws/.uploads/spec-def456.pdf"},
	}
	req.PersistedMediaRefs = refs

	if err := flush(context.Background(), "sess-1", nil); err != nil {
		t.Fatalf("flush returned error: %v", err)
	}
	if len(rec.added) != 1 {
		t.Fatalf("AddMessage called %d times, want 1", len(rec.added))
	}
	got := rec.added[0]
	if got.Role != "user" || got.Content != req.Message {
		t.Errorf("user message identity wrong: %+v", got)
	}
	if len(got.MediaRefs) != 2 {
		t.Fatalf("user message MediaRefs = %+v, want the 2 captured refs", got.MediaRefs)
	}
	if got.MediaRefs[0] != refs[0] || got.MediaRefs[1] != refs[1] {
		t.Errorf("MediaRefs = %+v, want %+v", got.MediaRefs, refs)
	}
}

func TestMakeFlushMessages_NoMediaLeavesRefsEmpty(t *testing.T) {
	rec := &recordingSessionStore{}
	l := &Loop{sessions: rec}
	req := &RunRequest{Message: "plain text"}

	flush := l.makeFlushMessages(req)
	if err := flush(context.Background(), "sess-1", nil); err != nil {
		t.Fatalf("flush returned error: %v", err)
	}
	if len(rec.added) != 1 {
		t.Fatalf("AddMessage called %d times, want 1", len(rec.added))
	}
	if len(rec.added[0].MediaRefs) != 0 {
		t.Errorf("MediaRefs = %+v, want empty for text-only message", rec.added[0].MediaRefs)
	}
}

func TestMakeEnrichMedia_CapturesPersistedRefsOnRequest(t *testing.T) {
	ws := t.TempDir()
	src := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	l := &Loop{}
	req := &RunRequest{
		SessionKey: "sess-1",
		Message:    "<media:document name=\"notes.txt\">\n\nsee file",
		Media:      []bus.MediaFile{{Path: src, MimeType: "text/plain", Filename: "notes.txt"}},
	}
	state := &pipeline.RunState{
		Messages: pipeline.NewMessageBuffer(providers.Message{Role: "system", Content: "sys"}),
	}
	state.Messages.SetHistory([]providers.Message{{Role: "user", Content: req.Message}})

	ctx := tools.WithToolWorkspace(context.Background(), ws)
	if err := l.makeEnrichMedia(req)(ctx, state); err != nil {
		t.Fatalf("enrichMedia returned error: %v", err)
	}

	if len(req.PersistedMediaRefs) != 1 {
		t.Fatalf("PersistedMediaRefs = %+v, want exactly 1 captured ref", req.PersistedMediaRefs)
	}
	ref := req.PersistedMediaRefs[0]
	if ref.Kind != "document" || ref.MimeType != "text/plain" {
		t.Errorf("ref = %+v, want persisted text document", ref)
	}
	if ref.Path == "" {
		t.Fatal("ref.Path empty, want persisted .uploads path")
	}
	if _, err := os.Stat(ref.Path); err != nil {
		t.Errorf("persisted file missing at %s: %v", ref.Path, err)
	}
}
