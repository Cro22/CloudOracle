package aws

import (
	"fmt"
	"strings"
	"testing"
)

func TestExtractRDSClusterInstance_HappyPath(t *testing.T) {
	attrs := map[string]interface{}{
		"cluster_identifier": "billing-cluster",
		"instance_class":     "db.r6g.xlarge",
		"engine":             "aurora-postgresql",
		"engine_version":     "15.4",
		"new_provider_field": "ignored",
	}
	got, err := ExtractRDSClusterInstance(attrs)
	if err != nil {
		t.Fatalf("ExtractRDSClusterInstance: %v", err)
	}
	want := RDSClusterInstanceAttributes{
		ClusterIdentifier: "billing-cluster",
		InstanceClass:     "db.r6g.xlarge",
		Engine:            "aurora-postgresql",
		EngineVersion:     "15.4",
	}
	if *got != want {
		t.Errorf("got %+v\nwant %+v", *got, want)
	}
}

func TestExtractRDSClusterInstance_OnlyRequiredFields(t *testing.T) {
	got, err := ExtractRDSClusterInstance(map[string]interface{}{
		"cluster_identifier": "c1",
		"instance_class":     "db.t3.medium",
		"engine":             "aurora-mysql",
	})
	if err != nil {
		t.Fatalf("ExtractRDSClusterInstance: %v", err)
	}
	if got.EngineVersion != "" {
		t.Errorf("EngineVersion = %q, want empty (optional, absent)", got.EngineVersion)
	}
}

// TestExtractRDSClusterInstance_AcceptsLegacyAuroraEngine confirms that
// "aurora" (the legacy MySQL 5.6 form) is accepted alongside the modern
// aurora-mysql/aurora-postgresql variants. The extractor doesn't validate
// against any catalog — that's the pricing engine's job.
func TestExtractRDSClusterInstance_AcceptsLegacyAuroraEngine(t *testing.T) {
	for _, engine := range []string{"aurora", "aurora-mysql", "aurora-postgresql"} {
		t.Run(engine, func(t *testing.T) {
			got, err := ExtractRDSClusterInstance(map[string]interface{}{
				"cluster_identifier": "c1",
				"instance_class":     "db.r5.large",
				"engine":             engine,
			})
			if err != nil {
				t.Fatalf("engine=%s: %v", engine, err)
			}
			if got.Engine != engine {
				t.Errorf("Engine = %q, want %q", got.Engine, engine)
			}
		})
	}
}

func TestExtractRDSClusterInstance_MissingRequired(t *testing.T) {
	cases := []struct {
		name  string
		attrs map[string]interface{}
		want  string
	}{
		{
			"no cluster_identifier",
			map[string]interface{}{"instance_class": "db.t3.medium", "engine": "aurora-mysql"},
			`aws_rds_cluster_instance: missing required attribute "cluster_identifier"`,
		},
		{
			"no instance_class",
			map[string]interface{}{"cluster_identifier": "c1", "engine": "aurora-mysql"},
			`aws_rds_cluster_instance: missing required attribute "instance_class"`,
		},
		{
			"no engine",
			map[string]interface{}{"cluster_identifier": "c1", "instance_class": "db.t3.medium"},
			`aws_rds_cluster_instance: missing required attribute "engine"`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ExtractRDSClusterInstance(c.attrs)
			if err == nil {
				t.Fatal("expected error")
			}
			if err.Error() != c.want {
				t.Errorf("error = %q\nwant %q", err.Error(), c.want)
			}
		})
	}
}

func TestExtractRDSClusterInstance_WrongTypes(t *testing.T) {
	base := map[string]interface{}{
		"cluster_identifier": "c1",
		"instance_class":     "db.t3.medium",
		"engine":             "aurora-mysql",
	}
	cases := []struct {
		name string
		key  string
		val  interface{}
	}{
		{"cluster_identifier as int", "cluster_identifier", 42},
		{"instance_class as bool", "instance_class", true},
		{"engine as int", "engine", 7},
		{"engine_version as bool", "engine_version", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			attrs := make(map[string]interface{}, len(base)+1)
			for k, v := range base {
				attrs[k] = v
			}
			attrs[c.key] = c.val

			_, err := ExtractRDSClusterInstance(attrs)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), c.key) {
				t.Errorf("error should mention %q: %v", c.key, err)
			}
		})
	}
}

func TestExtractRDSClusterInstance_NilAndEmpty(t *testing.T) {
	for _, attrs := range []map[string]interface{}{nil, {}} {
		_, err := ExtractRDSClusterInstance(attrs)
		if err == nil {
			t.Fatal("expected error")
		}
		if err.Error() != "aws_rds_cluster_instance: empty attributes" {
			t.Errorf("unexpected message: %q", err.Error())
		}
	}
}

func ExampleExtractRDSClusterInstance() {
	attrs := map[string]interface{}{
		"cluster_identifier": "billing-cluster",
		"instance_class":     "db.r6g.large",
		"engine":             "aurora-postgresql",
	}
	out, err := ExtractRDSClusterInstance(attrs)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s -> %s (%s)\n", out.ClusterIdentifier, out.InstanceClass, out.Engine)
	// Output: billing-cluster -> db.r6g.large (aurora-postgresql)
}
