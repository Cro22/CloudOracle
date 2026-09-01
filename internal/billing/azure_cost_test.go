package billing

import (
	"context"
	"errors"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/costmanagement/armcostmanagement"
)

type fakeCostMgmt struct {
	resp     armcostmanagement.QueryClientUsageResponse
	err      error
	gotScope string
	gotDef   armcostmanagement.QueryDefinition
}

func (f *fakeCostMgmt) Usage(
	_ context.Context,
	scope string,
	def armcostmanagement.QueryDefinition,
	_ *armcostmanagement.QueryClientUsageOptions,
) (armcostmanagement.QueryClientUsageResponse, error) {
	f.gotScope, f.gotDef = scope, def
	if f.err != nil {
		return armcostmanagement.QueryClientUsageResponse{}, f.err
	}
	return f.resp, nil
}

// usageResp builds a QueryResult with the given columns and rows. Column order
// is caller-controlled so tests can prove lookup-by-name, not by position.
func usageResp(cols []string, rows [][]any) armcostmanagement.QueryClientUsageResponse {
	columns := make([]*armcostmanagement.QueryColumn, len(cols))
	for i, name := range cols {
		columns[i] = &armcostmanagement.QueryColumn{Name: to.Ptr(name)}
	}
	return armcostmanagement.QueryClientUsageResponse{
		QueryResult: armcostmanagement.QueryResult{
			Properties: &armcostmanagement.QueryProperties{Columns: columns, Rows: rows},
		},
	}
}

func TestAzureCost_SumsPerServiceAndTagsProvider(t *testing.T) {
	// Cost column deliberately first, ServiceName second — the source must map
	// by column name, not position.
	fake := &fakeCostMgmt{resp: usageResp(
		[]string{"Cost", "ServiceName", "Currency"},
		[][]any{
			{100.5, "Virtual Machines", "USD"},
			{40.0, "Storage", "USD"},
			{99.5, "Virtual Machines", "USD"}, // same service, second row
		},
	)}
	src := NewCostManagementSource(fake, "sub-123")

	report, err := src.Costs(context.Background(), apr1(), apr30End())
	if err != nil {
		t.Fatalf("Costs: %v", err)
	}
	if report.DataSource != AzureCostManagementDataSource {
		t.Errorf("DataSource = %q, want %q", report.DataSource, AzureCostManagementDataSource)
	}
	if fake.gotScope != "/subscriptions/sub-123" {
		t.Errorf("scope = %q, want /subscriptions/sub-123", fake.gotScope)
	}
	got := recordsByService(report)
	if got["virtual machines"] != 200 {
		t.Errorf("virtual machines = %v, want 200 (100.5 + 99.5)", got["virtual machines"])
	}
	if got["storage"] != 40 {
		t.Errorf("storage = %v, want 40", got["storage"])
	}
	for _, r := range report.Records {
		if r.Provider != "azure" {
			t.Errorf("record provider = %q, want azure", r.Provider)
		}
	}
}

func TestAzureCost_ErrorWrapsAsSourceError(t *testing.T) {
	fake := &fakeCostMgmt{err: errors.New("forbidden")}
	src := NewCostManagementSource(fake, "sub-123")

	_, err := src.Costs(context.Background(), apr1(), apr30End())
	var srcErr *SourceError
	if !errors.As(err, &srcErr) {
		t.Fatalf("error = %v, want *SourceError", err)
	}
	if srcErr.Code != "billing_query_failed" {
		t.Errorf("code = %q, want billing_query_failed", srcErr.Code)
	}
}

func TestAzureCost_MissingCostColumnErrors(t *testing.T) {
	fake := &fakeCostMgmt{resp: usageResp(
		[]string{"ServiceName", "Currency"},
		[][]any{{"Storage", "USD"}},
	)}
	src := NewCostManagementSource(fake, "sub-123")

	_, err := src.Costs(context.Background(), apr1(), apr30End())
	var srcErr *SourceError
	if !errors.As(err, &srcErr) {
		t.Fatalf("error = %v, want *SourceError for missing cost column", err)
	}
}

func TestAzureCost_NilPropertiesYieldsEmptyReport(t *testing.T) {
	fake := &fakeCostMgmt{} // zero-valued response: Properties is nil
	src := NewCostManagementSource(fake, "sub-123")

	report, err := src.Costs(context.Background(), apr1(), apr30End())
	if err != nil {
		t.Fatalf("Costs: %v", err)
	}
	if len(report.Records) != 0 {
		t.Errorf("records = %v, want none", report.Records)
	}
	if report.DataSource != AzureCostManagementDataSource {
		t.Errorf("DataSource = %q, want %q", report.DataSource, AzureCostManagementDataSource)
	}
}
