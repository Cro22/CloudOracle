// Package iac parses infrastructure-as-code artifacts (Terraform plans today,
// other tools later) into a uniform shape that downstream cost-impact analysis
// can consume.
//
// This package owns *parsing and structural validation only*. Per-resource
// attribute extraction (instance_type for aws_instance, allocated_storage for
// aws_db_instance, etc.) is intentionally out of scope here — it lives in a
// later milestone so the parser doesn't have to be touched whenever a new
// resource type is supported.
package iac

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// Plan represents a parsed Terraform plan, i.e. the JSON document produced by
// `terraform show -json plan.tfplan`.
//
// We intentionally model only the fields downstream code reads. Real plans
// also include `prior_state`, `configuration`, `planned_values`, etc.;
// encoding/json silently ignores unknown fields, which is what we want for
// forward compatibility with future Terraform versions.
type Plan struct {
	FormatVersion    string           `json:"format_version"`
	TerraformVersion string           `json:"terraform_version"`
	ResourceChanges  []ResourceChange `json:"resource_changes"`
}

// Action is the canonical action type for a resource change.
//
// Note: ActionReplace is a *synthetic* action — Terraform itself reports
// replacements as a two-element actions slice, usually ["delete","create"]
// (the default lifecycle) or ["create","delete"] (when create_before_destroy
// is set). We collapse both into ActionReplace so downstream code (cost
// diffs, PR comments) can pattern-match on a single value.
type Action string

// Canonical action values. The string forms match Terraform's wire format
// 1:1 except for "replace", which Terraform doesn't emit directly.
const (
	ActionNoop    Action = "no-op"
	ActionCreate  Action = "create"
	ActionRead    Action = "read"
	ActionUpdate  Action = "update"
	ActionDelete  Action = "delete"
	ActionReplace Action = "replace"
)

// ResourceChange describes a single resource's planned change in a Terraform
// plan. One ResourceChange per address — there is exactly one entry per
// resource regardless of how many attributes change.
type ResourceChange struct {
	Address      string `json:"address"`
	Mode         string `json:"mode"` // "managed" or "data"
	Type         string `json:"type"` // e.g. "aws_instance"
	Name         string `json:"name"`
	ProviderName string `json:"provider_name"`
	Change       Change `json:"change"`
}

// Change holds the before/after state of a resource and the actions
// Terraform plans to take. Before is nil for a create; After is nil for
// a delete; both are populated for update and replace.
//
// Before and After are intentionally kept as map[string]interface{} rather
// than strongly-typed structs because the shape depends on the resource
// type (aws_instance vs aws_db_instance vs google_compute_instance, etc.).
// Per-type attribute extraction is the job of the next milestone — keeping
// the raw map here means new resource types can be supported later without
// touching this parser.
type Change struct {
	Actions []string               `json:"actions"`
	Before  map[string]interface{} `json:"before"`
	After   map[string]interface{} `json:"after"`
}

// Action returns the canonical Action for this resource change.
//
// Replacement detection: Terraform emits a two-element actions slice for
// resource replacements — ["delete","create"] under the default lifecycle,
// ["create","delete"] when create_before_destroy is set. Either order is
// reported as ActionReplace.
//
// For unknown action strings (e.g. a hypothetical future "import"), the
// raw value is returned as Action("…") rather than swallowed. Callers can
// compare against the known constants and treat anything else as unknown.
func (rc ResourceChange) Action() Action {
	a := rc.Change.Actions
	if len(a) == 2 {
		if (a[0] == "delete" && a[1] == "create") ||
			(a[0] == "create" && a[1] == "delete") {
			return ActionReplace
		}
	}
	if len(a) == 0 {
		// Defensive: Terraform always emits at least one action, but a
		// hand-crafted or truncated plan might not. Treat empty as no-op.
		return ActionNoop
	}
	return Action(a[0])
}

// IsManaged reports whether this is a managed resource (operator-controlled
// infrastructure) as opposed to a data source. Data sources are read-only
// lookups and never have a cost impact, so cost-diff code skips them.
func (rc ResourceChange) IsManaged() bool {
	return rc.Mode == "managed"
}

// ParsePlan parses a Terraform plan JSON from r.
//
// Returns an error if:
//   - the JSON is malformed
//   - the document is missing format_version (likely the wrong file:
//     a terraform.tfstate, a non-JSON `terraform show`, or unrelated JSON)
//
// resource_changes may be absent or null; both forms are accepted as a
// valid empty plan and surface as a Plan with nil ResourceChanges.
func ParsePlan(r io.Reader) (*Plan, error) {
	var p Plan
	if err := json.NewDecoder(r).Decode(&p); err != nil {
		return nil, fmt.Errorf("decoding terraform plan JSON: %w", err)
	}
	// format_version is the cheapest sanity check that this is actually a
	// `terraform show -json` document. Without it we'd silently accept
	// arbitrary JSON and surface confusing errors much later.
	if p.FormatVersion == "" {
		return nil, errors.New(
			"terraform plan: missing required field format_version " +
				"(was the file produced by `terraform show -json`?)",
		)
	}
	return &p, nil
}

// ParsePlanFile is a convenience wrapper around ParsePlan that opens the
// file at path and reads from it. Errors from os.Open are wrapped so
// callers can use errors.Is(err, os.ErrNotExist) to detect missing files.
func ParsePlanFile(path string) (*Plan, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening terraform plan file %q: %w", path, err)
	}
	defer f.Close()
	return ParsePlan(f)
}
