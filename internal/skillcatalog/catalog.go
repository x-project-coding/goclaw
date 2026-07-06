// Package skillcatalog is the single source of truth for the 42bucks
// skill-service operation catalog. It maps a fully-qualified operation id
// ("manage-view.set") to the HTTP call that reaches the corresponding
// skill-service endpoint.
//
// Two consumers share this catalog so an operation is described in exactly one
// place:
//
//   - internal/tools.CallSkillServiceTool — the native goclaw tool used during a
//     chat turn (server-side, mints the workspace token from the run context).
//   - cmd/skill — the static `skill` CLI baked into the goclaw image, used from a
//     skill's bash (and code-job sandboxes) where there is no tool loop; it reads
//     the pre-injected SKILL_RUNTIME_TOKEN + identity env vars instead.
//
// The package intentionally depends only on the standard library (os/sort/
// strings) so the CLI stays a small static binary (CGO_ENABLED=0) that runs on
// both the Alpine host-exec image and the Debian sandbox image.
package skillcatalog

import (
	"os"
	"sort"
	"strings"
)

// Operation describes one callable skill-service endpoint.
type Operation struct {
	// ID is the fully-qualified operation the caller selects, e.g. "manage-view.set".
	ID string
	// Skill is the owning skill slug — sent as X-Skill-Slug and (Phase 2) the
	// per-skill gating key.
	Skill string
	// Method is the HTTP verb.
	Method string
	// Path is the service-relative path under /api/skill-services, with {name}
	// placeholders filled from `input` at call time, e.g. "/media-forge/job/{id}".
	Path string
	// PathParams names the {placeholders} in Path, pulled out of `input`.
	PathParams []string
	// Summary is a one-line purpose shown to the model in the tool/CLI description.
	Summary string
	// InputHint lists the expected `input` fields (compact), shown to the model
	// and echoed in "unknown operation" errors so a weak model self-corrects.
	InputHint string
	// Async marks operations that enqueue work and return an id to poll.
	Async bool
	// PollWith names the operation that polls this one's result (async only).
	PollWith string
}

// Catalog — Phase 1 set. Keep sorted by ID for a stable enum/description.
var Catalog = []Operation{
	{
		ID: "deploy.static", Skill: "deploy", Method: "POST", Path: "/deploy/static",
		Summary:   "Publish a static file to the CDN and get a public immutable URL.",
		InputHint: "file:{file:string, data:string(base64 or utf-8), encoding?:'base64'|'utf-8', contentType?:string}, note?:string",
	},
	{
		ID: "manage-connections.catalog", Skill: "manage-connections", Method: "GET", Path: "/manage-connections/catalog",
		Summary:   "List the connectable app toolkits (name, auth mode, tool counts).",
		InputHint: "(no input)",
	},
	{
		ID: "manage-connections.list", Skill: "manage-connections", Method: "GET", Path: "/manage-connections/list",
		Summary:   "List this workspace's existing app connections and their status.",
		InputHint: "(no input)",
	},
	{
		ID: "manage-qa.run", Skill: "manage-qa", Method: "POST", Path: "/manage-qa/runs",
		Summary:   "Enqueue a QA run for a test or a project (async).",
		InputHint: "one of {testId:string} or {projectId:string}; trigger?:'UI'|'CLAUDE'|'API'|'SCHEDULE', triggeredBy?:string",
		Async:     true, PollWith: "manage-qa.run-status",
	},
	{
		ID: "manage-qa.run-status", Skill: "manage-qa", Method: "GET", Path: "/manage-qa/runs/{id}",
		PathParams: []string{"id"},
		Summary:    "Poll a QA run (terminal statuses: PASSED, FAILED, BLOCKED, ERRORED).",
		InputHint:  "id:string (the run id from manage-qa.run)",
	},
	{
		ID: "manage-skills.catalog", Skill: "manage-skills", Method: "GET", Path: "/manage-skills/catalog",
		Summary:   "List the workspace skill catalog and each employee's skills.",
		InputHint: "(no input)",
	},
	{
		ID: "manage-skills.connect", Skill: "manage-skills", Method: "POST", Path: "/manage-skills/connect",
		Summary:   "Attach an available skill to yourself (or a teammate via agentKey).",
		InputHint: "slug:string; agentKey?:string (omit for yourself)",
	},
	{
		ID: "manage-skills.disconnect", Skill: "manage-skills", Method: "POST", Path: "/manage-skills/disconnect",
		Summary:   "Detach a skill you added.",
		InputHint: "slug:string; agentKey?:string",
	},
	{
		ID: "manage-skills.duplicate", Skill: "manage-skills", Method: "POST", Path: "/manage-skills/duplicate",
		Summary:   "Fork a platform/brand skill into an editable workspace copy.",
		InputHint: "slug:string; newSlug?:string, name?:string, description?:string, connectToSelf?:bool, agentKey?:string",
	},
	{
		ID: "manage-skills.publish", Skill: "manage-skills", Method: "POST", Path: "/manage-skills/publish",
		Summary:   "Author or update a workspace skill from a files map (upsert by slug).",
		InputHint: "slug:string, files:{path:contents} (must include SKILL.md); name?:string, description?:string, connectToSelf?:bool, agentKey?:string",
	},
	{
		ID: "manage-view.set", Skill: "manage-view", Method: "POST", Path: "/manage-view/set",
		Summary:   "Set the chat view hints (prompt pills, placeholder, templates, browser pane).",
		InputHint: "sessionKey:string, hints:{pills?, placeholder?, templates?, browser?}",
	},
	{
		ID: "media-forge.image", Skill: "media-forge", Method: "POST", Path: "/media-forge/image",
		Summary:   "Generate an image and get its URL (synchronous).",
		InputHint: "prompt:string; tier?:'default'|'premium'|'budget', aspect_ratio?:string, count?:1..4, maxCostUsd?:number",
	},
	{
		ID: "media-forge.job", Skill: "media-forge", Method: "GET", Path: "/media-forge/job/{id}",
		PathParams: []string{"id"},
		Summary:    "Poll a media-forge async job (terminal statuses: DONE, FAILED).",
		InputHint:  "id:string (the job id)",
	},
	{
		ID: "research.search", Skill: "research", Method: "POST", Path: "/research/search",
		Summary:   "Web search grounded via the research provider (metered).",
		InputHint: "query:string; numResults?:1..100, type?:'auto'|'neural'|'keyword'|'fast', maxCostUsd?:0..5",
	},
}

// byID indexes the catalog for O(1) lookup.
var byID = func() map[string]Operation {
	m := make(map[string]Operation, len(Catalog))
	for _, op := range Catalog {
		m[op.ID] = op
	}
	return m
}()

// OperationIDs returns the sorted list of operation ids (the tool enum).
func OperationIDs() []string {
	ids := make([]string, 0, len(Catalog))
	for _, op := range Catalog {
		ids = append(ids, op.ID)
	}
	sort.Strings(ids)
	return ids
}

// Lookup resolves an operation id.
func Lookup(id string) (Operation, bool) {
	op, ok := byID[id]
	return op, ok
}

// Description renders the per-operation reference block embedded in the tool/CLI
// help, so the caller sees every operation's purpose + input shape in one place
// (weak models call more reliably from an inline reference than from a separate
// discovery round-trip).
func Description() string {
	ids := OperationIDs()
	var b strings.Builder
	for _, id := range ids {
		op := byID[id]
		b.WriteString("- ")
		b.WriteString(op.ID)
		b.WriteString(" — ")
		b.WriteString(op.Summary)
		b.WriteString(" [input: ")
		b.WriteString(op.InputHint)
		b.WriteString("]")
		if op.Async {
			b.WriteString(" (async → poll with ")
			b.WriteString(op.PollWith)
			b.WriteString(")")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// BaseURL is the x-api origin the skill-services live under. Read from the
// X_API_BASE_URL env var (already set on the goclaw runtime to
// https://api.42bucks.com); the default matches prod so a missing var is safe.
func BaseURL() string {
	if v := strings.TrimRight(os.Getenv("X_API_BASE_URL"), "/"); v != "" {
		return v
	}
	return "https://api.42bucks.com"
}
