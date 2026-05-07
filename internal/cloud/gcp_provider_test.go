package cloud

import (
	"context"
	"errors"
	"testing"
	"time"

	computepb "cloud.google.com/go/compute/apiv1/computepb"
	functionspb "cloud.google.com/go/functions/apiv2/functionspb"
	sqladmin "google.golang.org/api/sqladmin/v1beta4"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeGCPInstances struct {
	out []*computepb.Instance
	err error
}

func (f *fakeGCPInstances) listInstances(context.Context) ([]*computepb.Instance, error) {
	return f.out, f.err
}

type fakeGCPDisks struct {
	out []*computepb.Disk
	err error
}

func (f *fakeGCPDisks) listDisks(context.Context) ([]*computepb.Disk, error) {
	return f.out, f.err
}

type fakeGCPSQL struct {
	out []*sqladmin.DatabaseInstance
	err error
}

func (f *fakeGCPSQL) listSQLInstances(context.Context) ([]*sqladmin.DatabaseInstance, error) {
	return f.out, f.err
}

type fakeGCPFunctions struct {
	out []*functionspb.Function
	err error
}

func (f *fakeGCPFunctions) listFunctions(context.Context) ([]*functionspb.Function, error) {
	return f.out, f.err
}

func newTestGCPProvider() *GCPProvider {
	return &GCPProvider{
		projectID:      "test-project",
		serviceTimeout: 5 * time.Second,
	}
}

// TestGCPFetchComputeInstances_MapsZoneToRegion verifica el mapeo no obvio
// zone -> region: "us-central1-a" debe convertirse en "us-central1". Es un
// detalle facil de romper si alguien cambia extractRegionFromZone.
func TestGCPFetchComputeInstances_MapsZoneToRegion(t *testing.T) {
	zone := "https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a"
	machineType := "https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/machineTypes/n2-standard-4"
	created := "2026-02-15T10:00:00Z"
	name := "vm-prod-1"

	p := newTestGCPProvider()
	p.instances = &fakeGCPInstances{
		out: []*computepb.Instance{{
			Name:              &name,
			Zone:              &zone,
			MachineType:       &machineType,
			CreationTimestamp: &created,
			Labels:            map[string]string{"env": "prod"},
		}},
	}

	got, err := p.fetchComputeInstances(context.Background())
	if err != nil {
		t.Fatalf("fetchComputeInstances: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Region != "us-central1" {
		t.Errorf("Region = %q, want %q", got[0].Region, "us-central1")
	}
	if got[0].ResourceType != "n2-standard-4" {
		t.Errorf("ResourceType = %q, want n2-standard-4", got[0].ResourceType)
	}
	if got[0].Tags["env"] != "prod" {
		t.Errorf("Tags[env] = %q, want prod", got[0].Tags["env"])
	}
	if got[0].Service != "compute" {
		t.Errorf("Service = %q, want compute", got[0].Service)
	}
}

func TestGCPFetchComputeInstances_Error(t *testing.T) {
	p := newTestGCPProvider()
	p.instances = &fakeGCPInstances{err: errors.New("Compute API down")}

	_, err := p.fetchComputeInstances(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGCPFetchCloudSQL_Mapping(t *testing.T) {
	p := newTestGCPProvider()
	p.sql = &fakeGCPSQL{
		out: []*sqladmin.DatabaseInstance{{
			Name:       "db-prod",
			Region:     "us-east1",
			CreateTime: "2025-11-01T00:00:00Z",
			Settings: &sqladmin.Settings{
				Tier:       "db-n1-standard-2",
				UserLabels: map[string]string{"team": "data"},
			},
		}},
	}

	got, err := p.fetchCloudSQLInstances(context.Background())
	if err != nil {
		t.Fatalf("fetchCloudSQLInstances: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	r := got[0]
	if r.Service != "cloudsql" || r.ResourceType != "db-n1-standard-2" || r.Region != "us-east1" {
		t.Errorf("got = {service:%s type:%s region:%s}, want cloudsql/db-n1-standard-2/us-east1",
			r.Service, r.ResourceType, r.Region)
	}
	if r.Tags["team"] != "data" {
		t.Errorf("Tags[team] = %q, want data", r.Tags["team"])
	}
}

// TestGCPFetchCloudSQL_NilSettings cubre el caso en el que la API devuelve
// una instancia sin Settings (proxima al borrado). El mapeador no debe paniquear.
func TestGCPFetchCloudSQL_NilSettings(t *testing.T) {
	p := newTestGCPProvider()
	p.sql = &fakeGCPSQL{
		out: []*sqladmin.DatabaseInstance{{
			Name:     "db-no-settings",
			Region:   "europe-west1",
			Settings: nil,
		}},
	}
	got, err := p.fetchCloudSQLInstances(context.Background())
	if err != nil {
		t.Fatalf("fetchCloudSQLInstances: %v", err)
	}
	if len(got) != 1 || got[0].ResourceType != "" || got[0].Tags != nil {
		t.Errorf("nil Settings not handled: %+v", got)
	}
}

func TestGCPFetchPersistentDisks_Mapping(t *testing.T) {
	zone := "projects/test/zones/europe-west1-b"
	diskType := "projects/test/zones/europe-west1-b/diskTypes/pd-ssd"
	created := "2026-01-01T00:00:00Z"
	name := "disk-orphan"

	p := newTestGCPProvider()
	p.disks = &fakeGCPDisks{
		out: []*computepb.Disk{{
			Name:              &name,
			Zone:              &zone,
			Type:              &diskType,
			CreationTimestamp: &created,
		}},
	}

	got, err := p.fetchPersistentDisks(context.Background())
	if err != nil {
		t.Fatalf("fetchPersistentDisks: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Region != "europe-west1" || got[0].ResourceType != "pd-ssd" {
		t.Errorf("got = {region:%s type:%s}, want europe-west1/pd-ssd", got[0].Region, got[0].ResourceType)
	}
}

func TestGCPFetchCloudFunctions_Mapping(t *testing.T) {
	name := "projects/test-project/locations/us-central1/functions/fn-1"
	runtime := "nodejs20"

	p := newTestGCPProvider()
	p.functions = &fakeGCPFunctions{
		out: []*functionspb.Function{{
			Name: name,
			BuildConfig: &functionspb.BuildConfig{
				Runtime: runtime,
			},
			UpdateTime: timestamppb.New(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)),
			Labels:     map[string]string{"owner": "devx"},
		}},
	}

	got, err := p.fetchCloudFunctions(context.Background())
	if err != nil {
		t.Fatalf("fetchCloudFunctions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	r := got[0]
	if r.ID != "fn-1" || r.Region != "us-central1" || r.ResourceType != "nodejs20" {
		t.Errorf("got = {id:%s region:%s runtime:%s}, want fn-1/us-central1/nodejs20",
			r.ID, r.Region, r.ResourceType)
	}
}

// TestGCPFetchResources_GracefulDegradation verifica que cuando un servicio
// (Cloud SQL aqui) falla, los demas todavia entregan recursos.
func TestGCPFetchResources_GracefulDegradation(t *testing.T) {
	zone := "projects/test/zones/us-central1-a"
	machineType := "projects/test/zones/us-central1-a/machineTypes/e2-small"
	created := "2026-04-01T00:00:00Z"
	vmName := "vm-ok"

	p := newTestGCPProvider()
	p.instances = &fakeGCPInstances{
		out: []*computepb.Instance{{
			Name:              &vmName,
			Zone:              &zone,
			MachineType:       &machineType,
			CreationTimestamp: &created,
		}},
	}
	p.disks = &fakeGCPDisks{out: nil}
	p.sql = &fakeGCPSQL{err: errors.New("SQL Admin API quota exceeded")}
	p.functions = &fakeGCPFunctions{out: nil}

	got, err := p.FetchResources(context.Background())
	if err != nil {
		t.Fatalf("FetchResources: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (only Compute should land)", len(got))
	}
	if got[0].Service != "compute" {
		t.Errorf("got service = %q, want compute", got[0].Service)
	}
}

func TestExtractRegionFromZone(t *testing.T) {
	cases := map[string]string{
		"us-central1-a":  "us-central1",
		"europe-west1-b": "europe-west1",
		"asia-east2-c":   "asia-east2",
		"":               "",
		"noseparator":    "noseparator",
	}
	for in, want := range cases {
		if got := extractRegionFromZone(in); got != want {
			t.Errorf("extractRegionFromZone(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExtractFromResourceName(t *testing.T) {
	name := "projects/p1/locations/us-east1/functions/fn"
	if got := extractFromResourceName(name, "locations"); got != "us-east1" {
		t.Errorf("locations -> %q, want us-east1", got)
	}
	if got := extractFromResourceName(name, "functions"); got != "fn" {
		t.Errorf("functions -> %q, want fn", got)
	}
	if got := extractFromResourceName(name, "missing"); got != "" {
		t.Errorf("missing -> %q, want empty string", got)
	}
}
