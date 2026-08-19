package agent

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// metaSessionStore is a nopSessionStore that serves fixed session metadata,
// so the reminder can be exercised without a real store.
type metaSessionStore struct {
	nopSessionStore
	meta map[string]string
}

func (m *metaSessionStore) GetSessionMetadata(_ context.Context, _ string) map[string]string {
	return m.meta
}

func TestParseJobOutputPaths(t *testing.T) {
	if got := ParseJobOutputPaths(""); got != nil {
		t.Fatalf("empty → nil, got %v", got)
	}
	got := ParseJobOutputPaths(" /a/b \n\n/c/d\n")
	if len(got) != 2 || got[0] != "/a/b" || got[1] != "/c/d" {
		t.Fatalf("unexpected parse: %v", got)
	}
}

func TestMergeJobOutputPaths(t *testing.T) {
	// Newest first, deduped, capped.
	if got := MergeJobOutputPaths("/old", ""); got != "/old" {
		t.Fatalf("empty newest must leave existing unchanged, got %q", got)
	}
	got := MergeJobOutputPaths("/b\n/a", "/c")
	if got != "/c\n/b\n/a" {
		t.Fatalf("expected newest-first merge, got %q", got)
	}
	got = MergeJobOutputPaths("/c\n/b\n/a", "/b")
	if got != "/b\n/c\n/a" {
		t.Fatalf("re-announced path must move to the front without duplicating, got %q", got)
	}
	var many []string
	for i := 0; i < MaxRememberedJobOutputPaths+3; i++ {
		many = append(many, "/p"+strconv.Itoa(i))
	}
	got = MergeJobOutputPaths(strings.Join(many, "\n"), "/new")
	parts := ParseJobOutputPaths(got)
	if len(parts) != MaxRememberedJobOutputPaths {
		t.Fatalf("expected cap %d, got %d: %v", MaxRememberedJobOutputPaths, len(parts), parts)
	}
	if parts[0] != "/new" {
		t.Fatalf("newest must be first, got %v", parts)
	}
}

func TestFilterJobOutputPathsUnderRoot(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "app", "workspace", "tenants", "acme")
	inside := filepath.Join(root, "jordan", "user-1")
	outsideTenant := filepath.Join(string(filepath.Separator), "app", "workspace", "tenants", "other", "jordan", "user-1")
	traversal := filepath.Join(root, "..", "other", "x")

	got := filterJobOutputPathsUnderRoot([]string{inside, outsideTenant, traversal, "relative/path", "", root}, root)
	if len(got) != 1 || got[0] != inside {
		t.Fatalf("only the in-tenant absolute path must survive, got %v", got)
	}
	if got := filterJobOutputPathsUnderRoot([]string{inside}, ""); got != nil {
		t.Fatalf("empty root must disable the widen, got %v", got)
	}
	if got := filterJobOutputPathsUnderRoot([]string{inside}, "relative"); got != nil {
		t.Fatalf("relative root must disable the widen, got %v", got)
	}
}

func TestInjectLatestJobOutputPathReminder(t *testing.T) {
	const dir = "/app/workspace/tenants/acme/jordan/user-1"
	fresh := strconv.FormatInt(time.Now().Unix(), 10)
	stale := strconv.FormatInt(time.Now().Add(-2*latestJobOutputPathReminderWindow).Unix(), 10)

	msgs := func() []providers.Message {
		return []providers.Message{
			{Role: "assistant", Content: "earlier"},
			{Role: "user", ID: "u-1", Content: "where is the file?"},
		}
	}
	req := &RunRequest{SessionKey: "agent:jordan:ws:direct:chat-1"}

	t.Run("names the newest path in-band, merged into the trailing user message", func(t *testing.T) {
		l := &Loop{sessions: &metaSessionStore{meta: map[string]string{
			MetaLatestJobOutputPaths:  dir + "\n/app/workspace/tenants/acme/eva/system_workflow_acme",
			MetaLatestJobOutputPathAt: fresh,
		}}}
		out := l.injectLatestJobOutputPathReminder(context.Background(), req, msgs())
		if len(out) != 2 {
			t.Fatalf("must not add a turn, got %d messages", len(out))
		}
		last := out[1]
		if last.Role != "user" || last.ID != "u-1" {
			t.Fatalf("trailing user message identity must be preserved: %+v", last)
		}
		if !strings.HasPrefix(last.Content, "[System] Your background job's output is at: "+dir) {
			t.Fatalf("expected reminder naming %q, got %q", dir, last.Content)
		}
		if !strings.HasSuffix(last.Content, "where is the file?") {
			t.Fatalf("original user text must be kept, got %q", last.Content)
		}
		if strings.Contains(last.Content, "system_workflow_acme") {
			t.Fatalf("only the newest path is named in-band, got %q", last.Content)
		}
	})

	t.Run("no metadata → untouched", func(t *testing.T) {
		l := &Loop{sessions: &metaSessionStore{meta: map[string]string{}}}
		out := l.injectLatestJobOutputPathReminder(context.Background(), req, msgs())
		if out[1].Content != "where is the file?" {
			t.Fatalf("expected untouched, got %q", out[1].Content)
		}
	})

	t.Run("stale completion → untouched", func(t *testing.T) {
		l := &Loop{sessions: &metaSessionStore{meta: map[string]string{
			MetaLatestJobOutputPaths:  dir,
			MetaLatestJobOutputPathAt: stale,
		}}}
		out := l.injectLatestJobOutputPathReminder(context.Background(), req, msgs())
		if out[1].Content != "where is the file?" {
			t.Fatalf("expected untouched after the freshness window, got %q", out[1].Content)
		}
	})

	t.Run("trailing assistant message → untouched (role alternation)", func(t *testing.T) {
		l := &Loop{sessions: &metaSessionStore{meta: map[string]string{
			MetaLatestJobOutputPaths:  dir,
			MetaLatestJobOutputPathAt: fresh,
		}}}
		in := []providers.Message{{Role: "user", Content: "hi"}, {Role: "assistant", Content: "done"}}
		out := l.injectLatestJobOutputPathReminder(context.Background(), req, in)
		if out[1].Content != "done" || out[0].Content != "hi" {
			t.Fatalf("expected untouched, got %+v", out)
		}
	})

	t.Run("nil sessions / empty session key → untouched", func(t *testing.T) {
		l := &Loop{}
		out := l.injectLatestJobOutputPathReminder(context.Background(), req, msgs())
		if out[1].Content != "where is the file?" {
			t.Fatalf("expected untouched, got %q", out[1].Content)
		}
		l = &Loop{sessions: &metaSessionStore{meta: map[string]string{MetaLatestJobOutputPaths: dir}}}
		out = l.injectLatestJobOutputPathReminder(context.Background(), &RunRequest{}, msgs())
		if out[1].Content != "where is the file?" {
			t.Fatalf("expected untouched, got %q", out[1].Content)
		}
	})
}
