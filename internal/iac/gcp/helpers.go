package gcp

import (
	"fmt"
	"math"
	"strings"
)

// These helpers mirror the ones in internal/iac/aws/helpers.go. They are
// duplicated rather than shared for the same reason internal/diff duplicates
// weakestConfidence: they are a few lines each, generic, and importing across
// the aws↔gcp extractor packages would couple two otherwise-independent
// dialects to one package's unexported internals. If a third provider appears
// this is the moment to hoist them into a shared internal/iac/attrs package.

// getString returns (value, present, error) for a string attribute. JSON null
// and absent both read as "not present" so the caller can apply its own default.
func getString(attrs map[string]interface{}, key string) (string, bool, error) {
	raw, ok := attrs[key]
	if !ok || raw == nil {
		return "", false, nil
	}
	s, ok := raw.(string)
	if !ok {
		return "", false, fmt.Errorf("attribute %q: want string, got %T", key, raw)
	}
	return s, true, nil
}

// getInt returns (value, present, error) for an integer attribute. JSON numbers
// decode to float64, so whole-valued float64 is accepted; a fractional value is
// an error (the caller asked for an int, not a rounding).
func getInt(attrs map[string]interface{}, key string) (int, bool, error) {
	raw, ok := attrs[key]
	if !ok || raw == nil {
		return 0, false, nil
	}
	switch v := raw.(type) {
	case int:
		return v, true, nil
	case float64:
		if math.Trunc(v) != v {
			return 0, false, fmt.Errorf("attribute %q: want integer, got fractional %g", key, v)
		}
		return int(v), true, nil
	default:
		return 0, false, fmt.Errorf("attribute %q: want integer, got %T", key, raw)
	}
}

// getBool returns (value, present, error) for a strict JSON bool attribute.
func getBool(attrs map[string]interface{}, key string) (bool, bool, error) {
	raw, ok := attrs[key]
	if !ok || raw == nil {
		return false, false, nil
	}
	b, ok := raw.(bool)
	if !ok {
		return false, false, fmt.Errorf("attribute %q: want bool, got %T", key, raw)
	}
	return b, true, nil
}

// getNestedFirst returns the first element of a list-of-maps attribute — the
// shape Terraform plans use for nested blocks (boot_disk, scheduling, ...) even
// when only one is allowed. (nil, false, nil) for absent/null/empty-list.
func getNestedFirst(attrs map[string]interface{}, key string) (map[string]interface{}, bool, error) {
	raw, ok := attrs[key]
	if !ok || raw == nil {
		return nil, false, nil
	}
	list, ok := raw.([]interface{})
	if !ok {
		return nil, false, fmt.Errorf("attribute %q: want list, got %T", key, raw)
	}
	if len(list) == 0 {
		return nil, false, nil
	}
	first, ok := list[0].(map[string]interface{})
	if !ok {
		return nil, false, fmt.Errorf("attribute %q[0]: want object, got %T", key, list[0])
	}
	return first, true, nil
}

// lastPathSegment returns the final "/"-separated segment of s, or s unchanged
// when it has no slash. GCP self-links arrive either as short names
// ("e2-medium", "us-central1-a") or full URLs
// ("projects/p/zones/us-central1-a/machineTypes/e2-medium"); we only ever want
// the trailing name.
func lastPathSegment(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// regionFromZone strips the trailing "-<letter>" from a GCP zone to get its
// region ("us-central1-a" → "us-central1"). Returns "" when s doesn't look like
// a zone (no hyphen), leaving the caller to fall back to the plan-wide region.
func regionFromZone(zone string) string {
	i := strings.LastIndex(zone, "-")
	if i <= 0 {
		return ""
	}
	return zone[:i]
}

// getStringList returns (values, present, error) for a list-of-strings attribute.
// Absent/null/empty-list read as (nil, false, nil) so the caller applies its own
// default. Mirrors the aws helper of the same name.
func getStringList(attrs map[string]interface{}, key string) ([]string, bool, error) {
	raw, ok := attrs[key]
	if !ok || raw == nil {
		return nil, false, nil
	}
	list, ok := raw.([]interface{})
	if !ok {
		return nil, false, fmt.Errorf("attribute %q: want list, got %T", key, raw)
	}
	if len(list) == 0 {
		return nil, false, nil
	}
	out := make([]string, len(list))
	for i, v := range list {
		s, ok := v.(string)
		if !ok {
			return nil, false, fmt.Errorf("attribute %q[%d]: want string, got %T", key, i, v)
		}
		out[i] = s
	}
	return out, true, nil
}

func errEmptyAttrs(typ string) error {
	return fmt.Errorf("%s: empty attributes", typ)
}

func errMissingRequired(typ, key string) error {
	return fmt.Errorf("%s: missing required attribute %q", typ, key)
}

func wrapAttr(typ string, err error) error {
	return fmt.Errorf("%s: %w", typ, err)
}
