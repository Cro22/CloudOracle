package aws

import (
	"fmt"
	"strings"
	"testing"
)

func TestExtractRDS_HappyPath(t *testing.T) {
	attrs := map[string]interface{}{
		"engine":            "postgres",
		"engine_version":    "15.4",
		"instance_class":    "db.r5.large",
		"allocated_storage": float64(500),
		"storage_type":      "io1",
		"iops":              float64(10000),
		"multi_az":          true,
		// Unknown future field — must be ignored.
		"new_attr": "ignored",
	}
	got, err := ExtractRDS(attrs)
	if err != nil {
		t.Fatalf("ExtractRDS: %v", err)
	}
	want := RDSAttributes{
		Engine:           "postgres",
		EngineVersion:    "15.4",
		InstanceClass:    "db.r5.large",
		AllocatedStorage: 500,
		StorageType:      "io1",
		Iops:             10000,
		MultiAZ:          true,
	}
	if *got != want {
		t.Errorf("got %+v\nwant %+v", *got, want)
	}
}

func TestExtractRDS_OnlyRequiredFields(t *testing.T) {
	attrs := map[string]interface{}{
		"engine":            "mysql",
		"instance_class":    "db.t3.medium",
		"allocated_storage": float64(20),
	}
	got, err := ExtractRDS(attrs)
	if err != nil {
		t.Fatalf("ExtractRDS: %v", err)
	}
	if got.StorageType != "gp2" {
		t.Errorf("StorageType = %q, want %q (default)", got.StorageType, "gp2")
	}
	if got.MultiAZ {
		t.Error("MultiAZ = true, want false (default)")
	}
	if got.Iops != 0 {
		t.Errorf("Iops = %d, want 0", got.Iops)
	}
}

// TestExtractRDS_AuroraEngines verifies that aurora-postgresql / aurora-mysql
// are accepted as engine values. aws_db_instance covers Aurora *read replicas*
// (the cluster head is a separate resource type), so these must work.
func TestExtractRDS_AuroraEngines(t *testing.T) {
	for _, engine := range []string{"aurora-postgresql", "aurora-mysql"} {
		t.Run(engine, func(t *testing.T) {
			attrs := map[string]interface{}{
				"engine":            engine,
				"instance_class":    "db.r6g.large",
				"allocated_storage": float64(0),
			}
			got, err := ExtractRDS(attrs)
			if err != nil {
				t.Fatalf("ExtractRDS(%s): %v", engine, err)
			}
			if got.Engine != engine {
				t.Errorf("Engine = %q, want %q", got.Engine, engine)
			}
		})
	}
}

func TestExtractRDS_MissingRequired(t *testing.T) {
	cases := []struct {
		name  string
		attrs map[string]interface{}
		want  string
	}{
		{
			"no engine",
			map[string]interface{}{"instance_class": "db.t3.micro", "allocated_storage": float64(20)},
			`aws_db_instance: missing required attribute "engine"`,
		},
		{
			"no instance_class",
			map[string]interface{}{"engine": "postgres", "allocated_storage": float64(20)},
			`aws_db_instance: missing required attribute "instance_class"`,
		},
		{
			"no allocated_storage",
			map[string]interface{}{"engine": "postgres", "instance_class": "db.t3.micro"},
			`aws_db_instance: missing required attribute "allocated_storage"`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ExtractRDS(c.attrs)
			if err == nil {
				t.Fatal("expected error")
			}
			if err.Error() != c.want {
				t.Errorf("error = %q\nwant %q", err.Error(), c.want)
			}
		})
	}
}

func TestExtractRDS_WrongTypes(t *testing.T) {
	base := map[string]interface{}{
		"engine":            "postgres",
		"instance_class":    "db.t3.micro",
		"allocated_storage": float64(20),
	}
	cases := []struct {
		name string
		key  string
		val  interface{}
	}{
		{"engine as int", "engine", 42},
		{"instance_class as bool", "instance_class", true},
		{"engine_version as int", "engine_version", 15},
		{"storage_type as int", "storage_type", 7},
		{"allocated_storage as string", "allocated_storage", "20"},
		{"multi_az as string", "multi_az", "true"},
		{"iops fractional", "iops", 3.14},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			attrs := make(map[string]interface{}, len(base)+1)
			for k, v := range base {
				attrs[k] = v
			}
			attrs[c.key] = c.val

			_, err := ExtractRDS(attrs)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), c.key) {
				t.Errorf("error should mention %q: %v", c.key, err)
			}
		})
	}
}

func TestExtractRDS_NilAndEmpty(t *testing.T) {
	for _, attrs := range []map[string]interface{}{nil, {}} {
		_, err := ExtractRDS(attrs)
		if err == nil {
			t.Fatal("expected error for empty/nil attrs")
		}
		if err.Error() != "aws_db_instance: empty attributes" {
			t.Errorf("unexpected message: %q", err.Error())
		}
	}
}

func ExampleExtractRDS() {
	attrs := map[string]interface{}{
		"engine":            "postgres",
		"instance_class":    "db.t3.medium",
		"allocated_storage": float64(100),
		"multi_az":          true,
	}
	out, err := ExtractRDS(attrs)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s %s, %dGB, multi_az=%v\n",
		out.Engine, out.InstanceClass, out.AllocatedStorage, out.MultiAZ)
	// Output: postgres db.t3.medium, 100GB, multi_az=true
}
