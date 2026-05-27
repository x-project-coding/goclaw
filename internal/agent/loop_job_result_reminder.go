package agent

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// Session-metadata keys recording the freshest code-job result link(s). Written
// by the code-job announce handler (cmd.handleCodeAnnounce) and read here.
const (
	MetaLatestJobResultLinks = "latest_job_result_links"
	MetaLatestJobResultAt    = "latest_job_result_at"
)

// latestJobResultReminderWindow bounds how long after a job completes we keep
// reminding the agent of its link — long enough to cover active iteration,
// short enough to not nag once the conversation has moved on.
const latestJobResultReminderWindow = 30 * time.Minute

// injectLatestJobResultReminder prefixes the trailing user message with a
// [System] note naming the most recent code-job result link, so the agent
// references the current version instead of grabbing a stale link from earlier
// in the chat (a chat accumulates one result link per build, and a weaker model
// will sometimes reuse an old, now-broken one).
//
// Mirrors injectTeamTaskReminders: the note is merged INTO the trailing user
// message rather than added as a separate turn, so role alternation stays valid
// (proxy providers reject a trailing assistant message). Other fields of the
// user message (ID, media) are preserved.
func (l *Loop) injectLatestJobResultReminder(ctx context.Context, req *RunRequest, messages []providers.Message) []providers.Message {
	if l.sessions == nil || req == nil || req.SessionKey == "" || len(messages) == 0 {
		return messages
	}
	if messages[len(messages)-1].Role != "user" {
		return messages
	}

	meta := l.sessions.GetSessionMetadata(ctx, req.SessionKey)
	links := strings.TrimSpace(meta[MetaLatestJobResultLinks])
	if links == "" {
		return messages
	}
	// Freshness gate: stop reminding once the result is older than the window.
	if ts, err := strconv.ParseInt(meta[MetaLatestJobResultAt], 10, 64); err == nil {
		if time.Since(time.Unix(ts, 0)) > latestJobResultReminderWindow {
			return messages
		}
	}

	reminder := "[System] The current version of what you built/published is at: " + links +
		" — this is the latest result from your background code jobs. If you share or reference a link, use this one; any earlier links in this conversation are outdated."

	last := messages[len(messages)-1]
	last.Content = reminder + "\n\n" + last.Content
	messages[len(messages)-1] = last
	return messages
}
