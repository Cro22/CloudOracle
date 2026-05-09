package aws

import (
	"fmt"
	"strings"
	"testing"
)

func TestExtractLambda_HappyPath(t *testing.T) {
	attrs := map[string]interface{}{
		"function_name":                     "billing-processor",
		"runtime":                           "python3.12",
		"memory_size":                       float64(2048),
		"timeout":                           float64(900),
		"architectures":                     []interface{}{"arm64"},
		"provisioned_concurrent_executions": float64(5),
		// Unknown field — must be ignored.
		"some_future_attr": "ignored",
	}
	got, err := ExtractLambda(attrs)
	if err != nil {
		t.Fatalf("ExtractLambda: %v", err)
	}
	want := LambdaAttributes{
		FunctionName:           "billing-processor",
		Runtime:                "python3.12",
		MemorySize:             2048,
		Timeout:                900,
		Architecture:           "arm64",
		ProvisionedConcurrency: 5,
	}
	if *got != want {
		t.Errorf("got %+v\nwant %+v", *got, want)
	}
}

func TestExtractLambda_OnlyRequiredFields(t *testing.T) {
	got, err := ExtractLambda(map[string]interface{}{
		"function_name": "minimal",
	})
	if err != nil {
		t.Fatalf("ExtractLambda: %v", err)
	}
	if got.MemorySize != 128 {
		t.Errorf("MemorySize = %d, want 128 (Lambda default)", got.MemorySize)
	}
	if got.Timeout != 3 {
		t.Errorf("Timeout = %d, want 3 (Lambda default)", got.Timeout)
	}
	if got.Architecture != "x86_64" {
		t.Errorf("Architecture = %q, want x86_64 (default)", got.Architecture)
	}
	if got.ProvisionedConcurrency != 0 {
		t.Errorf("ProvisionedConcurrency = %d, want 0", got.ProvisionedConcurrency)
	}
}

// TestExtractLambda_ArchitecturesVariants covers the three shapes the
// architectures field can arrive in: a single-element list (the canonical
// case), an empty list, and an absent field. The latter two must both
// produce the x86_64 default.
func TestExtractLambda_ArchitecturesVariants(t *testing.T) {
	cases := []struct {
		name  string
		val   interface{}
		want  string
		setIt bool
	}{
		{"single element x86_64", []interface{}{"x86_64"}, "x86_64", true},
		{"single element arm64", []interface{}{"arm64"}, "arm64", true},
		{"empty list defaults", []interface{}{}, "x86_64", true},
		{"absent defaults", nil, "x86_64", false},
		// Unknown architecture value passes through — validation belongs
		// to the pricing engine, not the extractor.
		{"unknown value passes through", []interface{}{"riscv"}, "riscv", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			attrs := map[string]interface{}{"function_name": "fn"}
			if c.setIt {
				attrs["architectures"] = c.val
			}
			got, err := ExtractLambda(attrs)
			if err != nil {
				t.Fatalf("ExtractLambda: %v", err)
			}
			if got.Architecture != c.want {
				t.Errorf("Architecture = %q, want %q", got.Architecture, c.want)
			}
		})
	}
}

func TestExtractLambda_MissingFunctionName(t *testing.T) {
	_, err := ExtractLambda(map[string]interface{}{
		"runtime": "python3.12",
	})
	if err == nil {
		t.Fatal("expected error for missing function_name")
	}
	want := `aws_lambda_function: missing required attribute "function_name"`
	if err.Error() != want {
		t.Errorf("error = %q\nwant %q", err.Error(), want)
	}
}

func TestExtractLambda_WrongTypes(t *testing.T) {
	cases := []struct {
		name  string
		attrs map[string]interface{}
		want  string
	}{
		{"function_name as int", map[string]interface{}{"function_name": 42}, "function_name"},
		{"runtime as bool", map[string]interface{}{
			"function_name": "fn", "runtime": true,
		}, "runtime"},
		{"memory_size as string", map[string]interface{}{
			"function_name": "fn", "memory_size": "1024",
		}, "memory_size"},
		{"timeout fractional", map[string]interface{}{
			"function_name": "fn", "timeout": 5.5,
		}, "timeout"},
		{"architectures not a list", map[string]interface{}{
			"function_name": "fn", "architectures": "arm64",
		}, "architectures"},
		{"architectures contains non-string", map[string]interface{}{
			"function_name": "fn", "architectures": []interface{}{42},
		}, "architectures"},
		{"provisioned_concurrent_executions as string", map[string]interface{}{
			"function_name": "fn", "provisioned_concurrent_executions": "5",
		}, "provisioned_concurrent_executions"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ExtractLambda(c.attrs)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error should mention %q: %v", c.want, err)
			}
			if !strings.HasPrefix(err.Error(), "aws_lambda_function") {
				t.Errorf("error should start with aws_lambda_function: %v", err)
			}
		})
	}
}

func TestExtractLambda_NilAndEmpty(t *testing.T) {
	for _, attrs := range []map[string]interface{}{nil, {}} {
		_, err := ExtractLambda(attrs)
		if err == nil {
			t.Fatal("expected error")
		}
		if err.Error() != "aws_lambda_function: empty attributes" {
			t.Errorf("unexpected message: %q", err.Error())
		}
	}
}

func ExampleExtractLambda() {
	attrs := map[string]interface{}{
		"function_name": "image-resize",
		"memory_size":   float64(1024),
		"architectures": []interface{}{"arm64"},
	}
	out, err := ExtractLambda(attrs)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s on %s, %dMB\n", out.FunctionName, out.Architecture, out.MemorySize)
	// Output: image-resize on arm64, 1024MB
}
