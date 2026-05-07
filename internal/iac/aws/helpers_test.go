package aws

import (
	"reflect"
	"strings"
	"testing"
)

func TestGetString(t *testing.T) {
	cases := []struct {
		name     string
		attrs    map[string]interface{}
		key      string
		want     string
		wantPres bool
		wantErr  bool
	}{
		{"happy", map[string]interface{}{"k": "value"}, "k", "value", true, false},
		{"missing", map[string]interface{}{"other": "x"}, "k", "", false, false},
		{"null value treated as missing", map[string]interface{}{"k": nil}, "k", "", false, false},
		{"empty string is present", map[string]interface{}{"k": ""}, "k", "", true, false},
		{"wrong type int", map[string]interface{}{"k": 42}, "k", "", false, true},
		{"wrong type bool", map[string]interface{}{"k": true}, "k", "", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, pres, err := getString(c.attrs, c.key)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, c.wantErr)
			}
			if got != c.want {
				t.Errorf("value = %q, want %q", got, c.want)
			}
			if pres != c.wantPres {
				t.Errorf("present = %v, want %v", pres, c.wantPres)
			}
		})
	}
}

func TestGetInt(t *testing.T) {
	cases := []struct {
		name     string
		attrs    map[string]interface{}
		want     int
		wantPres bool
		wantErr  bool
	}{
		{"float64 whole number (JSON default)", map[string]interface{}{"k": float64(42)}, 42, true, false},
		{"int direct (programmatic map)", map[string]interface{}{"k": 7}, 7, true, false},
		{"zero is present", map[string]interface{}{"k": float64(0)}, 0, true, false},
		{"missing", map[string]interface{}{}, 0, false, false},
		{"null value treated as missing", map[string]interface{}{"k": nil}, 0, false, false},
		{"fractional float64 is error", map[string]interface{}{"k": 3.5}, 0, false, true},
		{"string is wrong type", map[string]interface{}{"k": "42"}, 0, false, true},
		{"bool is wrong type", map[string]interface{}{"k": true}, 0, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, pres, err := getInt(c.attrs, "k")
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, c.wantErr)
			}
			if got != c.want {
				t.Errorf("value = %d, want %d", got, c.want)
			}
			if pres != c.wantPres {
				t.Errorf("present = %v, want %v", pres, c.wantPres)
			}
		})
	}
}

func TestGetBool(t *testing.T) {
	cases := []struct {
		name     string
		val      interface{}
		want     bool
		wantPres bool
		wantErr  bool
	}{
		{"true", true, true, true, false},
		{"false is present", false, false, true, false},
		{"missing", nil, false, false, false},
		{"string 'true' is wrong type", "true", false, false, true},
		{"int 1 is wrong type", 1, false, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			attrs := map[string]interface{}{}
			if c.val != nil || c.name == "missing" {
				if c.name == "missing" {
					// Leave the key absent.
				} else {
					attrs["k"] = c.val
				}
			}
			got, pres, err := getBool(attrs, "k")
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, c.wantErr)
			}
			if got != c.want {
				t.Errorf("value = %v, want %v", got, c.want)
			}
			if pres != c.wantPres {
				t.Errorf("present = %v, want %v", pres, c.wantPres)
			}
		})
	}
}

func TestGetNestedFirst(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		attrs := map[string]interface{}{
			"block": []interface{}{
				map[string]interface{}{"size": float64(100)},
				map[string]interface{}{"size": float64(200)},
			},
		}
		got, pres, err := getNestedFirst(attrs, "block")
		if err != nil || !pres {
			t.Fatalf("err = %v, pres = %v", err, pres)
		}
		if got["size"] != float64(100) {
			t.Errorf("first[size] = %v, want 100 (must take FIRST element)", got["size"])
		}
	})

	t.Run("missing key", func(t *testing.T) {
		_, pres, err := getNestedFirst(map[string]interface{}{}, "block")
		if err != nil || pres {
			t.Errorf("missing key: err=%v pres=%v, want nil/false", err, pres)
		}
	})

	t.Run("null value", func(t *testing.T) {
		_, pres, err := getNestedFirst(map[string]interface{}{"block": nil}, "block")
		if err != nil || pres {
			t.Errorf("null value: err=%v pres=%v, want nil/false", err, pres)
		}
	})

	t.Run("empty list", func(t *testing.T) {
		_, pres, err := getNestedFirst(map[string]interface{}{"block": []interface{}{}}, "block")
		if err != nil || pres {
			t.Errorf("empty list: err=%v pres=%v, want nil/false", err, pres)
		}
	})

	t.Run("not a list", func(t *testing.T) {
		_, _, err := getNestedFirst(map[string]interface{}{"block": "not a list"}, "block")
		if err == nil {
			t.Fatal("expected error for string-typed value")
		}
		if !strings.Contains(err.Error(), "want list") {
			t.Errorf("error should say 'want list': %v", err)
		}
	})

	t.Run("first element not a map", func(t *testing.T) {
		attrs := map[string]interface{}{"block": []interface{}{"string-not-map"}}
		_, _, err := getNestedFirst(attrs, "block")
		if err == nil {
			t.Fatal("expected error for non-map first element")
		}
		if !strings.Contains(err.Error(), "want object") {
			t.Errorf("error should say 'want object': %v", err)
		}
	})
}

func TestGetStringList(t *testing.T) {
	t.Run("multi element", func(t *testing.T) {
		attrs := map[string]interface{}{
			"k": []interface{}{"a", "b", "c"},
		}
		got, pres, err := getStringList(attrs, "k")
		if err != nil || !pres {
			t.Fatalf("err=%v pres=%v", err, pres)
		}
		if !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
			t.Errorf("got %v", got)
		}
	})

	t.Run("single element", func(t *testing.T) {
		attrs := map[string]interface{}{"k": []interface{}{"only"}}
		got, _, err := getStringList(attrs, "k")
		if err != nil || len(got) != 1 || got[0] != "only" {
			t.Errorf("got=%v err=%v", got, err)
		}
	})

	t.Run("missing", func(t *testing.T) {
		_, pres, err := getStringList(map[string]interface{}{}, "k")
		if err != nil || pres {
			t.Errorf("err=%v pres=%v", err, pres)
		}
	})

	t.Run("null value", func(t *testing.T) {
		_, pres, err := getStringList(map[string]interface{}{"k": nil}, "k")
		if err != nil || pres {
			t.Errorf("err=%v pres=%v", err, pres)
		}
	})

	t.Run("empty list reported as not present", func(t *testing.T) {
		_, pres, err := getStringList(map[string]interface{}{"k": []interface{}{}}, "k")
		if err != nil || pres {
			t.Errorf("err=%v pres=%v, want nil/false for empty list", err, pres)
		}
	})

	t.Run("not a list", func(t *testing.T) {
		_, _, err := getStringList(map[string]interface{}{"k": "string-value"}, "k")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "want list") {
			t.Errorf("error should say 'want list': %v", err)
		}
	})

	t.Run("non-string element", func(t *testing.T) {
		attrs := map[string]interface{}{"k": []interface{}{"ok", 42}}
		_, _, err := getStringList(attrs, "k")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), `"k"[1]`) {
			t.Errorf("error should reference index 1: %v", err)
		}
	})
}

func TestErrorHelpers(t *testing.T) {
	if got := errEmptyAttrs("aws_instance").Error(); got != `aws_instance: empty attributes` {
		t.Errorf("errEmptyAttrs format unexpected: %q", got)
	}
	if got := errMissingRequired("aws_db_instance", "engine").Error(); got != `aws_db_instance: missing required attribute "engine"` {
		t.Errorf("errMissingRequired format unexpected: %q", got)
	}
}
