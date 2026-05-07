package cloud

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/appservice/armappservice/v4"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v6"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/sql/armsql/v2"
)

type fakeAzureVMs struct {
	out []*armcompute.VirtualMachine
	err error
}

func (f *fakeAzureVMs) listVMs(context.Context) ([]*armcompute.VirtualMachine, error) {
	return f.out, f.err
}

type fakeAzureDisks struct {
	out []*armcompute.Disk
	err error
}

func (f *fakeAzureDisks) listDisks(context.Context) ([]*armcompute.Disk, error) {
	return f.out, f.err
}

type fakeAzureSQL struct {
	out []*armsql.Database
	err error
}

func (f *fakeAzureSQL) listSQLDatabases(context.Context) ([]*armsql.Database, error) {
	return f.out, f.err
}

type fakeAzureWebApps struct {
	out []*armappservice.Site
	err error
}

func (f *fakeAzureWebApps) listWebApps(context.Context) ([]*armappservice.Site, error) {
	return f.out, f.err
}

func newTestAzureProvider() *AzureProvider {
	return &AzureProvider{
		subscriptionID: "00000000-0000-0000-0000-000000000000",
		serviceTimeout: 5 * time.Second,
	}
}

func TestAzureFetchVirtualMachines_Mapping(t *testing.T) {
	name := "vm-1"
	location := "eastus"
	created := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	vmSize := armcompute.VirtualMachineSizeTypesStandardD2SV3
	tagVal := "production"

	p := newTestAzureProvider()
	p.vms = &fakeAzureVMs{
		out: []*armcompute.VirtualMachine{{
			Name:     &name,
			Location: &location,
			Properties: &armcompute.VirtualMachineProperties{
				HardwareProfile: &armcompute.HardwareProfile{VMSize: &vmSize},
				TimeCreated:     &created,
			},
			Tags: map[string]*string{"env": &tagVal},
		}},
	}

	got, err := p.fetchVirtualMachines(context.Background())
	if err != nil {
		t.Fatalf("fetchVirtualMachines: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	r := got[0]
	if r.Service != "vm" || r.ResourceType != string(vmSize) || r.Region != "eastus" {
		t.Errorf("got = {service:%s type:%s region:%s}, want vm/%s/eastus",
			r.Service, r.ResourceType, r.Region, vmSize)
	}
	if r.Tags["env"] != "production" {
		t.Errorf("Tags[env] = %q, want production", r.Tags["env"])
	}
}

// TestAzureFetchVirtualMachines_NilHardwareProfile verifica que un VM con
// Properties.HardwareProfile == nil no paniquea — Azure puede devolver eso
// para VMs en estados de transicion.
func TestAzureFetchVirtualMachines_NilHardwareProfile(t *testing.T) {
	name := "vm-broken"
	location := "westus"

	p := newTestAzureProvider()
	p.vms = &fakeAzureVMs{
		out: []*armcompute.VirtualMachine{{
			Name:       &name,
			Location:   &location,
			Properties: nil,
		}},
	}

	got, err := p.fetchVirtualMachines(context.Background())
	if err != nil {
		t.Fatalf("fetchVirtualMachines: %v", err)
	}
	if len(got) != 1 || got[0].ResourceType != "" {
		t.Errorf("nil Properties not handled: %+v", got)
	}
}

func TestAzureFetchSQLDatabases_Mapping(t *testing.T) {
	name := "db-prod"
	location := "northeurope"
	skuName := "S1"
	created := time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC)

	p := newTestAzureProvider()
	p.sql = &fakeAzureSQL{
		out: []*armsql.Database{{
			Name:     &name,
			Location: &location,
			SKU:      &armsql.SKU{Name: &skuName},
			Properties: &armsql.DatabaseProperties{
				CreationDate: &created,
			},
		}},
	}

	got, err := p.fetchSQLDatabases(context.Background())
	if err != nil {
		t.Fatalf("fetchSQLDatabases: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Service != "sql" || got[0].ResourceType != "S1" || got[0].Region != "northeurope" {
		t.Errorf("got = %+v, want sql/S1/northeurope", got[0])
	}
}

func TestAzureFetchManagedDisks_Mapping(t *testing.T) {
	name := "disk-1"
	location := "eastus"
	skuName := armcompute.DiskStorageAccountTypesPremiumLRS

	p := newTestAzureProvider()
	p.disks = &fakeAzureDisks{
		out: []*armcompute.Disk{{
			Name:     &name,
			Location: &location,
			SKU:      &armcompute.DiskSKU{Name: &skuName},
		}},
	}

	got, err := p.fetchManagedDisks(context.Background())
	if err != nil {
		t.Fatalf("fetchManagedDisks: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].ResourceType != string(skuName) {
		t.Errorf("ResourceType = %q, want %q", got[0].ResourceType, skuName)
	}
}

// TestAzureFetchFunctionApps_FiltersOutWebApps verifica el filtrado clave
// del fetcher: el endpoint /sites devuelve Web Apps Y Function Apps mezclados,
// y solo nos interesan los functionapp. Si alguien rompe el filtro, este test
// se cae.
func TestAzureFetchFunctionApps_FiltersOutWebApps(t *testing.T) {
	fnName, fnKind, fnLoc := "fn-1", "functionapp", "eastus"
	webName, webKind, webLoc := "web-app", "app", "eastus"

	p := newTestAzureProvider()
	p.webApps = &fakeAzureWebApps{
		out: []*armappservice.Site{
			{Name: &fnName, Kind: &fnKind, Location: &fnLoc},
			{Name: &webName, Kind: &webKind, Location: &webLoc},
		},
	}

	got, err := p.fetchFunctionApps(context.Background())
	if err != nil {
		t.Fatalf("fetchFunctionApps: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (web app should be filtered out)", len(got))
	}
	if got[0].ID != "fn-1" {
		t.Errorf("got ID = %q, want fn-1", got[0].ID)
	}
}

func TestAzureFetchFunctionApps_LinuxKindMatches(t *testing.T) {
	// Azure devuelve Kind como "functionapp,linux" para function apps en Linux.
	// El filtro debe ser case-insensitive y un substring.
	name, kind, loc := "fn-linux", "functionapp,linux", "eastus"

	p := newTestAzureProvider()
	p.webApps = &fakeAzureWebApps{
		out: []*armappservice.Site{
			{Name: &name, Kind: &kind, Location: &loc},
		},
	}

	got, err := p.fetchFunctionApps(context.Background())
	if err != nil {
		t.Fatalf("fetchFunctionApps: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (linux variant should match)", len(got))
	}
}

// TestAzureFetchResources_GracefulDegradation: si Azure SQL falla,
// los demas servicios (VM, Disks, Functions) deben seguir surfaceando.
func TestAzureFetchResources_GracefulDegradation(t *testing.T) {
	vmName, loc := "vm-ok", "eastus"
	vmSize := armcompute.VirtualMachineSizeTypesStandardB2S

	p := newTestAzureProvider()
	p.vms = &fakeAzureVMs{
		out: []*armcompute.VirtualMachine{{
			Name:     &vmName,
			Location: &loc,
			Properties: &armcompute.VirtualMachineProperties{
				HardwareProfile: &armcompute.HardwareProfile{VMSize: &vmSize},
			},
		}},
	}
	p.disks = &fakeAzureDisks{out: nil}
	p.sql = &fakeAzureSQL{err: errors.New("Azure SQL listing failed")}
	p.webApps = &fakeAzureWebApps{out: nil}

	got, err := p.FetchResources(context.Background())
	if err != nil {
		t.Fatalf("FetchResources: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (only VM should land)", len(got))
	}
	if got[0].Service != "vm" {
		t.Errorf("got service = %q, want vm", got[0].Service)
	}
}

func TestExtractResourceGroup(t *testing.T) {
	id := "/subscriptions/abc/resourceGroups/my-rg/providers/Microsoft.Sql/servers/srv1"
	if got := extractResourceGroup(id); got != "my-rg" {
		t.Errorf("got %q, want my-rg", got)
	}
	// case-insensitive: la API a veces devuelve "resourcegroups" minusculas
	id2 := "/subscriptions/abc/resourcegroups/lowercased-rg/providers/Foo"
	if got := extractResourceGroup(id2); got != "lowercased-rg" {
		t.Errorf("case-insensitive: got %q, want lowercased-rg", got)
	}
	if got := extractResourceGroup("malformed"); got != "" {
		t.Errorf("malformed input: got %q, want empty", got)
	}
}

func TestConvertAzureTags_NilValuePointer(t *testing.T) {
	val := "value"
	tags := map[string]*string{
		"key1": &val,
		"key2": nil, // tag con valor nil — la API a veces devuelve eso
	}
	got := convertAzureTags(tags)
	if got["key1"] != "value" {
		t.Errorf("key1 = %q, want value", got["key1"])
	}
	if got["key2"] != "" {
		t.Errorf("key2 = %q, want empty string for nil pointer", got["key2"])
	}
}
