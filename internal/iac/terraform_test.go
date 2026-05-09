package iac

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestParsePlan_SimpleCreate(t *testing.T) {
	p, err := ParsePlanFile("testdata/plan_simple_create.json")
	if err != nil {
		t.Fatalf("ParsePlanFile: %v", err)
	}

	if p.FormatVersion != "1.2" {
		t.Errorf("FormatVersion = %q, want 1.2", p.FormatVersion)
	}
	if p.TerraformVersion != "1.6.0" {
		t.Errorf("TerraformVersion = %q, want 1.6.0", p.TerraformVersion)
	}
	if len(p.ResourceChanges) != 1 {
		t.Fatalf("len(ResourceChanges) = %d, want 1", len(p.ResourceChanges))
	}

	rc := p.ResourceChanges[0]
	if rc.Address != "aws_instance.web" {
		t.Errorf("Address = %q, want aws_instance.web", rc.Address)
	}
	if rc.Type != "aws_instance" {
		t.Errorf("Type = %q, want aws_instance", rc.Type)
	}
	if rc.Action() != ActionCreate {
		t.Errorf("Action() = %q, want %q", rc.Action(), ActionCreate)
	}
	if !rc.IsManaged() {
		t.Error("IsManaged() = false, want true")
	}
	if rc.Change.Before != nil {
		t.Errorf("Before = %v, want nil for create", rc.Change.Before)
	}
	if got := rc.Change.After["instance_type"]; got != "t3.large" {
		t.Errorf("After[instance_type] = %v, want t3.large", got)
	}
}

// TestParsePlan_Mixed verifies that a plan with multiple kinds of changes
// produces the right Action for each, including the data-source read which
// IsManaged() must filter out.
func TestParsePlan_Mixed(t *testing.T) {
	p, err := ParsePlanFile("testdata/plan_mixed.json")
	if err != nil {
		t.Fatalf("ParsePlanFile: %v", err)
	}

	got := map[string]Action{}
	managed := map[string]bool{}
	for _, rc := range p.ResourceChanges {
		got[rc.Address] = rc.Action()
		managed[rc.Address] = rc.IsManaged()
	}

	wantActions := map[string]Action{
		"aws_instance.api":     ActionCreate,
		"aws_ebs_volume.cache": ActionDelete,
		"aws_db_instance.main": ActionUpdate,
		"data.aws_ami.ubuntu":  ActionRead,
	}
	for addr, want := range wantActions {
		if got[addr] != want {
			t.Errorf("Action(%s) = %q, want %q", addr, got[addr], want)
		}
	}

	wantManaged := map[string]bool{
		"aws_instance.api":     true,
		"aws_ebs_volume.cache": true,
		"aws_db_instance.main": true,
		"data.aws_ami.ubuntu":  false,
	}
	for addr, want := range wantManaged {
		if managed[addr] != want {
			t.Errorf("IsManaged(%s) = %v, want %v", addr, managed[addr], want)
		}
	}
}

func TestParsePlan_Replace(t *testing.T) {
	p, err := ParsePlanFile("testdata/plan_replace.json")
	if err != nil {
		t.Fatalf("ParsePlanFile: %v", err)
	}
	if got := p.ResourceChanges[0].Action(); got != ActionReplace {
		t.Errorf("Action() = %q, want %q (delete+create should collapse to replace)",
			got, ActionReplace)
	}
}

// TestParsePlan_ReplaceReversed verifies that ["create","delete"] (the
// create_before_destroy lifecycle order) also collapses to ActionReplace,
// not just the default ["delete","create"] order.
func TestParsePlan_ReplaceReversed(t *testing.T) {
	in := `{
		"format_version": "1.2",
		"resource_changes": [{
			"address": "aws_instance.x",
			"mode": "managed", "type": "aws_instance", "name": "x",
			"change": { "actions": ["create","delete"], "before": null, "after": null }
		}]
	}`
	p, err := ParsePlan(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParsePlan: %v", err)
	}
	if got := p.ResourceChanges[0].Action(); got != ActionReplace {
		t.Errorf("Action() = %q, want %q", got, ActionReplace)
	}
}

// TestParsePlan_EmptyPlan covers all three forms Terraform may use for a
// plan with no changes: explicit empty array, explicit null, and the field
// omitted entirely. None of them should be rejected.
func TestParsePlan_EmptyPlan(t *testing.T) {
	p, err := ParsePlanFile("testdata/plan_empty.json")
	if err != nil {
		t.Fatalf("ParsePlanFile: %v", err)
	}
	if len(p.ResourceChanges) != 0 {
		t.Errorf("len(ResourceChanges) = %d, want 0", len(p.ResourceChanges))
	}

	cases := []struct {
		name string
		json string
	}{
		{"null array", `{"format_version": "1.2", "resource_changes": null}`},
		{"omitted field", `{"format_version": "1.2"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			plan, err := ParsePlan(strings.NewReader(c.json))
			if err != nil {
				t.Fatalf("ParsePlan: %v", err)
			}
			if len(plan.ResourceChanges) != 0 {
				t.Errorf("len(ResourceChanges) = %d, want 0", len(plan.ResourceChanges))
			}
		})
	}
}

func TestParsePlan_InvalidJSON(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"garbage", "not json at all"},
		{"unterminated object", "{"},
		{"empty input", ""},
		{"truncated mid-string", `{"format_version": "1.`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParsePlan(strings.NewReader(c.in))
			if err == nil {
				t.Errorf("expected error for %q, got nil", c.in)
				return
			}
			if !strings.Contains(err.Error(), "decoding terraform plan JSON") {
				t.Errorf("error should mention decoding context: %v", err)
			}
		})
	}
}

func TestParsePlan_MissingFormatVersion(t *testing.T) {
	in := `{"terraform_version": "1.6.0", "resource_changes": []}`
	_, err := ParsePlan(strings.NewReader(in))
	if err == nil {
		t.Fatal("expected error for missing format_version, got nil")
	}
	if !strings.Contains(err.Error(), "format_version") {
		t.Errorf("error should mention format_version: %v", err)
	}
}

func TestIsManaged(t *testing.T) {
	cases := []struct {
		mode string
		want bool
	}{
		{"managed", true},
		{"data", false},
		{"", false},
		{"unknown", false},
	}
	for _, c := range cases {
		got := ResourceChange{Mode: c.mode}.IsManaged()
		if got != c.want {
			t.Errorf("Mode=%q: IsManaged() = %v, want %v", c.mode, got, c.want)
		}
	}
}

// TestAction_EdgeCases covers Action() branches not exercised by the
// fixtures: empty actions slice (defensive no-op fallback), unknown action
// strings (passed through), and a non-replace two-element slice.
func TestAction_EdgeCases(t *testing.T) {
	cases := []struct {
		name    string
		actions []string
		want    Action
	}{
		{"empty", []string{}, ActionNoop},
		{"nil", nil, ActionNoop},
		{"unknown action passes through", []string{"import"}, Action("import")},
		{"two non-replace actions does not collapse", []string{"create", "update"}, ActionCreate},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rc := ResourceChange{Change: Change{Actions: c.actions}}
			if got := rc.Action(); got != c.want {
				t.Errorf("Action() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestParsePlanFile(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		p, err := ParsePlanFile("testdata/plan_simple_create.json")
		if err != nil {
			t.Fatalf("ParsePlanFile: %v", err)
		}
		if p == nil {
			t.Fatal("got nil plan")
		}
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := ParsePlanFile("testdata/this-does-not-exist.json")
		if err == nil {
			t.Fatal("expected error opening missing file")
		}
		// The error chain must preserve os.ErrNotExist so callers can
		// distinguish "missing file" from other failure modes.
		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("error should wrap os.ErrNotExist: %v", err)
		}
	})
}
