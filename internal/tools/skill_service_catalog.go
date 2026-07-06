package tools

import "github.com/nextlevelbuilder/goclaw/internal/skillcatalog"

// The skill-service operation catalog now lives in internal/skillcatalog so the
// native call_skill_service tool (this package) and the static `skill` CLI
// (cmd/skill) share one source of truth. These thin aliases keep the existing
// tool code and tests referencing the original package-local names unchanged.

// skillOperation is the catalog operation type.
type skillOperation = skillcatalog.Operation

// skillServiceCatalog is the operation catalog (Phase 1 set).
var skillServiceCatalog = skillcatalog.Catalog

// catalogOperationIDs returns the sorted list of operation ids (the tool enum).
func catalogOperationIDs() []string { return skillcatalog.OperationIDs() }

// catalogLookup resolves an operation id.
func catalogLookup(id string) (skillOperation, bool) { return skillcatalog.Lookup(id) }

// catalogDescription renders the per-operation reference block for the tool.
func catalogDescription() string { return skillcatalog.Description() }

// skillServiceBaseURL is the x-api origin the skill-services live under.
func skillServiceBaseURL() string { return skillcatalog.BaseURL() }
