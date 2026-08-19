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

// Session-metadata keys recording where this session's background jobs /
// delegates actually wrote their output. A code-runner job sandbox resolves its
// workspace as tenants/<tenant>/<executingAgentKey>/<userID> (see
// internal/http/skillcallback_verify_key.go resolveWorkspaceDir) — a sibling of,
// not the same as, the launching agent's own interactive workspace — so the job's
// self-reported "/workspace/…" path is only valid inside its own sandbox.
// Written by cmd.handleCodeAnnounce (from the resolved dir the skill-callback
// handler stamps on the completion message) and read by
// injectLatestJobOutputPathReminder here and by the loop context setup, which
// widens the READ-allowed prefixes with these paths (tools.WithToolJobOutputPaths).
//
// MetaLatestJobOutputPaths is a newline-separated list, newest first, capped at
// MaxRememberedJobOutputPaths entries; MetaLatestJobOutputPathAt is the unix
// timestamp of the newest entry.
const (
	MetaLatestJobOutputPaths  = "latest_job_output_paths"
	MetaLatestJobOutputPathAt = "latest_job_output_path_at"
)

// MaxRememberedJobOutputPaths bounds how many distinct job output directories a
// session keeps read access to (a chat that dispatches jobs to several
// employees accumulates one dir per (agentKey, userID) pair).
const MaxRememberedJobOutputPaths = 8

// latestJobOutputPathReminderWindow bounds how long after a job completes we keep
// naming its output path in-band — same rationale as latestJobResultReminderWindow.
const latestJobOutputPathReminderWindow = 30 * time.Minute

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
		" — this is the latest result from your background jobs. If you share or reference a link, use this one; any earlier links in this conversation are outdated."

	last := messages[len(messages)-1]
	last.Content = reminder + "\n\n" + last.Content
	messages[len(messages)-1] = last
	return messages
}

// ParseJobOutputPaths splits a MetaLatestJobOutputPaths value into its entries
// (newest first), dropping blanks. Shared by the reminder and the loop context
// setup so both read the metadata the same way handleCodeAnnounce writes it.
func ParseJobOutputPaths(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// MergeJobOutputPaths returns existing (a MetaLatestJobOutputPaths value) with
// newest prepended, deduplicated and capped at MaxRememberedJobOutputPaths, in
// the newline-joined form the metadata stores. An empty newest returns existing
// unchanged.
func MergeJobOutputPaths(existing, newest string) string {
	newest = strings.TrimSpace(newest)
	if newest == "" {
		return existing
	}
	merged := []string{newest}
	for _, p := range ParseJobOutputPaths(existing) {
		if p == newest {
			continue
		}
		merged = append(merged, p)
		if len(merged) >= MaxRememberedJobOutputPaths {
			break
		}
	}
	return strings.Join(merged, "\n")
}

// injectLatestJobOutputPathReminder prefixes the trailing user message with a
// [System] note naming the ABSOLUTE directory the session's most recent
// background job / delegate wrote its output to, so the agent reads it directly
// instead of trusting the job's sandbox-local "/workspace/…" path and burning
// iterations on a filesystem-wide find (the failure mode behind monitor issue
// cfqgrcaxz7ez7e2t6pdehjcwl). Sibling of injectLatestJobResultReminder: same
// metadata storage, same freshness window, same merge-into-trailing-user-message
// shape so role alternation stays valid.
func (l *Loop) injectLatestJobOutputPathReminder(ctx context.Context, req *RunRequest, messages []providers.Message) []providers.Message {
	if l.sessions == nil || req == nil || req.SessionKey == "" || len(messages) == 0 {
		return messages
	}
	if messages[len(messages)-1].Role != "user" {
		return messages
	}

	meta := l.sessions.GetSessionMetadata(ctx, req.SessionKey)
	paths := ParseJobOutputPaths(meta[MetaLatestJobOutputPaths])
	if len(paths) == 0 {
		return messages
	}
	// Freshness gate: stop reminding once the newest completion is older than
	// the window. Read access (loop context) is NOT gated — the files remain.
	if ts, err := strconv.ParseInt(meta[MetaLatestJobOutputPathAt], 10, 64); err == nil {
		if time.Since(time.Unix(ts, 0)) > latestJobOutputPathReminderWindow {
			return messages
		}
	}

	reminder := "[System] Your background job's output is at: " + paths[0] +
		" — that is the real absolute directory the job's /workspace was mounted at; read it directly with read_file/list_files (or ls/cat), no need to search the filesystem for it."

	last := messages[len(messages)-1]
	last.Content = reminder + "\n\n" + last.Content
	messages[len(messages)-1] = last
	return messages
}
