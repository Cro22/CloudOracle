package aws

import (
	"fmt"
	"math"
)

// The helpers in this file are intentionally unexported. They exist to keep
// each extractor (ec2.go / rds.go / ebs.go) small and to centralize three
// concerns that would otherwise be scattered:
//
//  1. The (value, present, error) tri-state for optional attributes — it's
//     not enough to return zero+error because callers often need to apply
//     a default *only* when the attribute is missing, vs. when it errored.
//  2. The float64-as-int quirk introduced by encoding/json (every JSON
//     number decodes to float64 by default).
//  3. Consistent error messages across resource types so the diff engine
//     in the next milestone can match on them if it needs to.

// getString returns the string at key.
//
//   - (s,    true,  nil) — key is present and is a string.
//   - ("",   false, nil) — key is missing or its value is JSON null.
//   - ("",   false, err) — key is present but the value is the wrong type.
//
// JSON null is treated as "missing" rather than "wrong type" because
// Terraform plans encode unset string attributes as null (not empty string),
// and the calling extractor wants to apply its own default in that case.
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

// getInt returns the int at key.
//
// JSON numbers unmarshal as float64 by default, so this accepts both
// float64 (whole-number values only — fractional input is an error) and
// int (in case the caller built the map programmatically). Anything else
// is a type mismatch.
func getInt(attrs map[string]interface{}, key string) (int, bool, error) {
	raw, ok := attrs[key]
	if !ok || raw == nil {
		return 0, false, nil
	}
	switch v := raw.(type) {
	case int:
		return v, true, nil
	case float64:
		// JSON numbers are float64. Reject anything with a fractional part —
		// the caller asked for an integer and a value like 3.5 means the
		// upstream data is inconsistent, not "round it for me".
		if math.Trunc(v) != v {
			return 0, false, fmt.Errorf("attribute %q: want integer, got fractional %g", key, v)
		}
		return int(v), true, nil
	default:
		return 0, false, fmt.Errorf("attribute %q: want integer, got %T", key, raw)
	}
}

// getBool returns the bool at key. Strict — JSON true/false only, never
// "true"/"false" strings or 0/1 integers.
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

// getNestedFirst returns the first element of a list-of-maps attribute as
// a map. This is the shape Terraform plans use for nested blocks like
// root_block_device — even when HCL only allows one block, the JSON plan
// always wraps it in an array.
//
// Returns (nil, false, nil) when the key is absent, JSON null, or an empty
// list. Returns (nil, false, err) when the value isn't a list, or its
// first element isn't a map.
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

// getStringList returns the value at key as a []string slice.
//
// JSON encodes string arrays as []interface{} of string entries, so each
// element gets a type assertion. A non-string element is an error rather
// than a silent skip — partial slices would lose data the caller likely
// needs (Lambda's `architectures`, security_group lists, etc.).
//
// Empty lists are reported as (nil, false, nil) — callers that distinguish
// "explicitly empty" from "absent" don't currently exist; aligning with
// the missing/null behavior keeps the helper predictable.
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

// errEmptyAttrs is the canonical error every extractor returns when its
// input map is nil or empty. Centralized so the message stays uniform.
func errEmptyAttrs(typ string) error {
	return fmt.Errorf("%s: empty attributes", typ)
}

// errMissingRequired produces a uniform message for required attributes
// that are absent or null.
func errMissingRequired(typ, key string) error {
	return fmt.Errorf("%s: missing required attribute %q", typ, key)
}

// wrapAttr wraps a helper-level error with the resource-type prefix so
// every error in the package starts with `aws_<type>: …`.
func wrapAttr(typ string, err error) error {
	return fmt.Errorf("%s: %w", typ, err)
}
