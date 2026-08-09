package pipeline

import "testing"

func TestNormalizeReplyText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, in, want string
	}{
		{"plain", "Hello world", "Hello world"},
		{"trim", "  Hello world \n", "Hello world"},
		{"collapse whitespace", "Hello\n\n  world\tagain", "Hello world again"},
		{"strip bold", "**Hello** world", "Hello world"},
		{"strip underscore emphasis", "__Hello__ _world_", "Hello world"},
		{"strip mixed emphasis", "*Hey!* I'm **Roman**.", "Hey! I'm Roman."},
		{"empty", "", ""},
		{"whitespace only", " \n\t ", ""},
		{"emphasis only", "**__**", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeReplyText(tc.in); got != tc.want {
				t.Errorf("NormalizeReplyText(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsDuplicateReplyText(t *testing.T) {
	t.Parallel()
	const greeting = "Hey! I'm Roman. What would you like to build today?"
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"exact match", greeting, greeting, true},
		{"markdown bold variant", "**Hey! I'm Roman.** What would you like to build today?", greeting, true},
		{"whitespace variant", "Hey! I'm Roman.\n\nWhat would you like to build today?", greeting, true},
		{"different text", "Let me check that for you.", greeting, false},
		{"prefix is not a duplicate", "Hey! I'm Roman.", greeting, false},
		{"superset is not a duplicate", greeting + " Anyway, done!", greeting, false},
		{"both empty", "", "", false},
		{"empty vs text", "", greeting, false},
		{"text vs empty", greeting, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsDuplicateReplyText(tc.a, tc.b); got != tc.want {
				t.Errorf("IsDuplicateReplyText(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func pushAll(d *StreamDedup, chunks []string) (emitted []string) {
	for _, c := range chunks {
		if out := d.Push(c); out != "" {
			emitted = append(emitted, out)
		}
	}
	return emitted
}

func TestStreamDedup_FullRepeatSuppressed(t *testing.T) {
	t.Parallel()
	prev := "Hey! I'm Roman. What would you like to build today?"
	d := NewStreamDedup(prev)
	emitted := pushAll(d, []string{"Hey! I'm ", "Roman. What would ", "you like to build today?"})
	if len(emitted) != 0 {
		t.Fatalf("chunks emitted while replaying previous turn: %q", emitted)
	}
	flush, suppressed := d.Finish()
	if flush != "" || !suppressed {
		t.Fatalf("Finish() = (%q, %v), want (\"\", true)", flush, suppressed)
	}
}

func TestStreamDedup_MarkdownVariantSuppressed(t *testing.T) {
	t.Parallel()
	d := NewStreamDedup("Hey! I'm Roman. What would you like to build today?")
	emitted := pushAll(d, []string{"**Hey! I'm Roman.**", " What would you like", " to build today?"})
	if len(emitted) != 0 {
		t.Fatalf("chunks emitted while replaying previous turn: %q", emitted)
	}
	if flush, suppressed := d.Finish(); flush != "" || !suppressed {
		t.Fatalf("Finish() = (%q, %v), want (\"\", true)", flush, suppressed)
	}
}

func TestStreamDedup_DivergenceFlushesHeldText(t *testing.T) {
	t.Parallel()
	d := NewStreamDedup("Hey! I'm Roman. What would you like to build today?")
	if out := d.Push("Hey! I'm "); out != "" {
		t.Fatalf("prefix chunk emitted early: %q", out)
	}
	// Diverges here — everything held so far must flush in one piece.
	if out := d.Push("done with the setup."); out != "Hey! I'm done with the setup." {
		t.Fatalf("divergence flush = %q, want full held text", out)
	}
	// Subsequent chunks pass through untouched.
	if out := d.Push(" Next steps:"); out != " Next steps:" {
		t.Fatalf("post-divergence chunk = %q, want pass-through", out)
	}
	if flush, suppressed := d.Finish(); flush != "" || suppressed {
		t.Fatalf("Finish() = (%q, %v), want (\"\", false)", flush, suppressed)
	}
}

func TestStreamDedup_ShorterPrefixFlushedOnFinish(t *testing.T) {
	t.Parallel()
	d := NewStreamDedup("Hey! I'm Roman. What would you like to build today?")
	if out := d.Push("Hey! I'm Roman."); out != "" {
		t.Fatalf("prefix chunk emitted early: %q", out)
	}
	flush, suppressed := d.Finish()
	if flush != "Hey! I'm Roman." || suppressed {
		t.Fatalf("Finish() = (%q, %v), want held text flushed, not suppressed", flush, suppressed)
	}
}

func TestStreamDedup_NoPreviousText_PassesThrough(t *testing.T) {
	t.Parallel()
	d := NewStreamDedup("")
	if out := d.Push("Hello"); out != "Hello" {
		t.Fatalf("Push = %q, want pass-through", out)
	}
	if flush, suppressed := d.Finish(); flush != "" || suppressed {
		t.Fatalf("Finish() = (%q, %v), want (\"\", false)", flush, suppressed)
	}
}

func TestStreamDedup_PushAfterFinishPassesThrough(t *testing.T) {
	t.Parallel()
	d := NewStreamDedup("previous reply")
	pushAll(d, []string{"previous ", "reply"})
	if _, suppressed := d.Finish(); !suppressed {
		t.Fatal("expected full-repeat suppression")
	}
	// A guarded retry reuses the same chunk callback — retry output must stream.
	if out := d.Push("fresh retry output"); out != "fresh retry output" {
		t.Fatalf("post-Finish Push = %q, want pass-through", out)
	}
}
