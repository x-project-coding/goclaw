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
// The catalog is served from an atomic snapshot (see [current]). init loads the
// EMBEDDED catalog.json as the floor; a runtime loader (internal/skillcatalog/
// reload, wired only into the gateway) may hot-swap a fresher catalog fetched
// from x-api via [Load]. Every accessor reads the live snapshot per call, so a
// swap is visible immediately with no reader locking. If x-api is unreachable or
// serves an invalid catalog, the embedded floor stays in place — the catalog
// never becomes empty.
//
// The package intentionally depends only on the standard library (embed/
// encoding-json/errors/fmt/os/sort/strings/sync-atomic) so the CLI stays a small
// static binary (CGO_ENABLED=0) that runs on both the Alpine host-exec image and
// the Debian sandbox image.
package skillcatalog

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync/atomic"
)

// DefaultCatalogPath is where the runtime loader persists the latest catalog and
// where the `skill` CLI looks for a hot-swapped override (see cmd/skill). It is
// the same path the gateway exec-env injection points GOCLAW_SKILL_CATALOG at.
const DefaultCatalogPath = "/app/data/skill-catalog.json"

// embeddedVersion tags the compile-time floor catalog. It is deliberately not a
// content hash of the embed: x-api serves its own sha256 version, and a distinct
// sentinel keeps boot logs unambiguous ("old=embedded new=<sha>") and guarantees
// the first runtime fetch is never mistaken for a 304 no-op.
const embeddedVersion = "embedded"

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

// catalogJSON is the embedded floor catalog. It is GENERATED from x-api's route
// schemas — do not edit catalog.json by hand. To regenerate:
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

// state is an immutable snapshot of the catalog: the operation slice, its id
// index, and the version identifying it. Accessors read the current *state
// through an atomic pointer so [Load] can hot-swap the whole catalog without
// locking readers. A snapshot is never mutated after it is stored.
type state struct {
	ops     []Operation
	byID    map[string]Operation
	version string
}

// current holds the live snapshot. Seeded by init from the embedded catalog.json
// (the floor) and swapped in by Load.
var current atomic.Pointer[state]

// newState builds a snapshot from an already-validated operation slice.
func newState(ops []Operation, version string) *state {
	m := make(map[string]Operation, len(ops))
	for _, op := range ops {
		m[op.ID] = op
	}
	return &state{ops: ops, byID: m, version: version}
}

// parseCatalog unmarshals a catalog operation array and validates it: non-empty,
// every operation has ID/Skill/Method/Path, and ids are unique. It returns the
// parsed operations or an error (never a partial result).
func parseCatalog(jsonBytes []byte) ([]Operation, error) {
	var ops []Operation
	if err := json.Unmarshal(jsonBytes, &ops); err != nil {
		return nil, fmt.Errorf("skillcatalog: invalid catalog JSON: %w", err)
	}
	if len(ops) == 0 {
		return nil, errors.New("skillcatalog: catalog is empty")
	}
	seen := make(map[string]bool, len(ops))
	for i, op := range ops {
		if op.ID == "" || op.Skill == "" || op.Method == "" || op.Path == "" {
			return nil, fmt.Errorf("skillcatalog: operation %d missing required field (id/skill/method/path)", i)
		}
		if seen[op.ID] {
			return nil, fmt.Errorf("skillcatalog: duplicate operation id %q", op.ID)
		}
		seen[op.ID] = true
	}
	return ops, nil
}

func init() {
	ops, err := parseCatalog(catalogJSON)
	if err != nil {
		panic("skillcatalog: embedded catalog.json is invalid: " + err.Error())
	}
	current.Store(newState(ops, embeddedVersion))
}

// Load parses jsonBytes as a []Operation, validates it (non-empty, every op has
// ID/Skill/Method/Path, ids unique), and atomically swaps it in as the live
// catalog tagged with version. On any parse/validation error the current
// snapshot is left untouched and the error is returned, so a bad fetch can never
// blank the catalog.
func Load(jsonBytes []byte, version string) error {
	ops, err := parseCatalog(jsonBytes)
	if err != nil {
		return err
	}
	current.Store(newState(ops, version))
	return nil
}

// Version returns the version string identifying the live catalog snapshot
// ("embedded" until a runtime Load succeeds, then x-api's sha256 hex).
func Version() string {
	return current.Load().version
}

// Catalog returns the live operation snapshot. The returned slice is shared with
// the current snapshot and MUST NOT be mutated.
func Catalog() []Operation {
	return current.Load().ops
}

// OperationIDs returns the sorted list of operation ids (the tool enum).
func OperationIDs() []string {
	return operationIDs(current.Load(), nil)
}

// OperationIDsFor returns the sorted operation ids whose owning skill slug is in
// allowed. A nil map means unrestricted (the full catalog); a non-nil map gates
// strictly, so an empty map yields no operations.
func OperationIDsFor(allowed map[string]bool) []string {
	return operationIDs(current.Load(), allowed)
}

// operationIDs filters+sorts ids from a single snapshot so callers that also need
// the operations (DescriptionFor) see a consistent view even across a swap.
func operationIDs(st *state, allowed map[string]bool) []string {
	ids := make([]string, 0, len(st.ops))
	for _, op := range st.ops {
		if allowed != nil && !allowed[op.Skill] {
			continue
		}
		ids = append(ids, op.ID)
	}
	sort.Strings(ids)
	return ids
}

// Lookup resolves an operation id against the live snapshot.
func Lookup(id string) (Operation, bool) {
	op, ok := current.Load().byID[id]
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
	st := current.Load()
	ids := operationIDs(st, allowed)
	var b strings.Builder
	for _, id := range ids {
		op := st.byID[id]
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
