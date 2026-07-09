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
	_ "embed"
	"encoding/json"
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

// Catalog is loaded from the embedded catalog.json, which is GENERATED from
// x-api's route schemas — do not edit catalog.json by hand. To regenerate:
//
//	cd x-api && npm run catalog:dump     # verifies every op against the live
//	cp dist/skill-catalog.json .../internal/skillcatalog/catalog.json  # schemas
//
// The generator (x-api scripts/generate-skill-catalog.mjs) holds the curated
// operation allowlist + summaries; the mechanical parts (existence, method,
// path params, input hints) come from the TypeBox schemas, so a drifted route
// fails generation instead of shipping a broken operation.
//
//go:embed catalog.json
var catalogJSON []byte

// Catalog is the generated operation set.
var Catalog = func() []Operation {
	var ops []Operation
	if err := json.Unmarshal(catalogJSON, &ops); err != nil {
		panic("skillcatalog: embedded catalog.json is invalid: " + err.Error())
	}
	if len(ops) == 0 {
		panic("skillcatalog: embedded catalog.json is empty")
	}
	return ops
}()

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
	return OperationIDsFor(nil)
}

// OperationIDsFor returns the sorted operation ids whose owning skill slug is in
// allowed. A nil map means unrestricted (the full catalog); a non-nil map gates
// strictly, so an empty map yields no operations.
func OperationIDsFor(allowed map[string]bool) []string {
	ids := make([]string, 0, len(Catalog))
	for _, op := range Catalog {
		if allowed != nil && !allowed[op.Skill] {
			continue
		}
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
	return DescriptionFor(nil)
}

// DescriptionFor renders the reference block for the operations whose owning
// skill slug is in allowed, with the same nil-means-all semantics as
// OperationIDsFor. Used to prune the tool description to an agent's granted
// skills alongside the enum.
func DescriptionFor(allowed map[string]bool) string {
	ids := OperationIDsFor(allowed)
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

// HintHasField reports whether an operation's InputHint names `field` as an
// input, matching on a word boundary so "sessionKey" does not match inside
// "fromSessionKey". Hints are generated from the real route schemas, so a
// field appears in the hint iff the route accepts it. Shared by the native
// call_skill_service tool and the skill CLI for session-key auto-fill.
func HintHasField(hint, field string) bool {
	for i := 0; ; {
		j := strings.Index(hint[i:], field)
		if j < 0 {
			return false
		}
		j += i
		var before byte
		if j > 0 {
			before = hint[j-1]
		}
		isAlnum := (before >= 'a' && before <= 'z') || (before >= 'A' && before <= 'Z') || (before >= '0' && before <= '9')
		if !isAlnum {
			return true
		}
		i = j + len(field)
	}
}
