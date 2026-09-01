package azure

import "fmt"

// These helpers mirror the ones in internal/iac/aws/helpers.go and
// internal/iac/gcp/helpers.go. They are duplicated a third time rather than
// shared for the reason those files give: a few generic lines each, and
// importing across the provider extractor packages would couple otherwise-
// independent dialects.
//
// ponytail: azure is the third provider, which is the hoist trigger the gcp
// helpers comment named ("If a third provider appears this is the moment to
// hoist them into a shared internal/iac/attrs package"). Left duplicated to
// keep this feature's diff small; hoisting all three is a separate refactor.

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
		if v != float64(int(v)) {
			return 0, false, fmt.Errorf("attribute %q: want integer, got fractional %g", key, v)
		}
		return int(v), true, nil
	default:
		return 0, false, fmt.Errorf("attribute %q: want integer, got %T", key, raw)
	}
}

// getNestedFirst returns the first element of a list-of-maps attribute — the
// shape Terraform plans use for nested blocks (os_disk, ...) even when only one
// is allowed. (nil, false, nil) for absent/null/empty-list.
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

func errEmptyAttrs(typ string) error {
	return fmt.Errorf("%s: empty attributes", typ)
}

func errMissingRequired(typ, key string) error {
	return fmt.Errorf("%s: missing required attribute %q", typ, key)
}

func wrapAttr(typ string, err error) error {
	return fmt.Errorf("%s: %w", typ, err)
}
