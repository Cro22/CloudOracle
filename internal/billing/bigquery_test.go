package billing

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeBQ struct {
	rows []billingRow
	err  error
	sqls []string
}

func (f *fakeBQ) query(_ context.Context, sql string) ([]billingRow, error) {
	f.sqls = append(f.sqls, sql)
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

func TestBigQuery_SumsAndTagsProvider(t *testing.T) {
	fake := &fakeBQ{rows: []billingRow{
		{Service: "Compute Engine", Cost: 100.50},
		{Service: "Cloud SQL", Cost: 40},
		{Service: "Compute Engine", Cost: 99.50}, // same service, second row
	}}
	src := NewBigQuerySource(fake, "`p.d.t`")

	report, err := src.Costs(context.Background(), apr1(), apr30End())
	if err != nil {
		t.Fatalf("Costs: %v", err)
	}
	if report.DataSource != GCPBigQueryDataSource {
		t.Errorf("DataSource = %q, want %q", report.DataSource, GCPBigQueryDataSource)
	}
	got := recordsByService(report)
	if got["compute engine"] != 200 { // 100.50 + 99.50, normalized lower-case
		t.Errorf("compute engine total = %v, want 200", got["compute engine"])
	}
	if got["cloud sql"] != 40 {
		t.Errorf("cloud sql total = %v, want 40", got["cloud sql"])
	}
	for _, r := range report.Records {
		if r.Provider != "gcp" {
			t.Errorf("record provider = %q, want gcp", r.Provider)
		}
	}
}

func TestBigQuery_SQLBoundsAndTable(t *testing.T) {
	fake := &fakeBQ{}
	src := NewBigQuerySource(fake, "`proj.ds.gcp_billing_export_v1_ABC`")

	if _, err := src.Costs(context.Background(), apr1(), apr30End()); err != nil {
		t.Fatalf("Costs: %v", err)
	}
	sql := fake.sqls[0]
	for _, want := range []string{
		"`proj.ds.gcp_billing_export_v1_ABC`",
		"TIMESTAMP('2026-04-01 00:00:00')",
		"TIMESTAMP('2026-04-30 23:59:59')",
		"GROUP BY service",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("SQL missing %q\ngot: %s", want, sql)
		}
	}
}

func TestBigQuery_ErrorWrapsAsSourceError(t *testing.T) {
	fake := &fakeBQ{err: errors.New("permission denied")}
	src := NewBigQuerySource(fake, "`p.d.t`")

	_, err := src.Costs(context.Background(), apr1(), apr30End())
	var srcErr *SourceError
	if !errors.As(err, &srcErr) {
		t.Fatalf("error = %v, want *SourceError", err)
	}
	if srcErr.Code != "billing_query_failed" {
		t.Errorf("code = %q, want billing_query_failed", srcErr.Code)
	}
	if !errors.Is(err, srcErr.Err) {
		t.Error("SourceError should unwrap to the underlying error")
	}
}
