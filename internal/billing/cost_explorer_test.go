package billing

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
)

type fakeCE struct {
	outputs []*costexplorer.GetCostAndUsageOutput
	err     error
	inputs  []*costexplorer.GetCostAndUsageInput
}

func (f *fakeCE) GetCostAndUsage(
	_ context.Context,
	in *costexplorer.GetCostAndUsageInput,
	_ ...func(*costexplorer.Options),
) (*costexplorer.GetCostAndUsageOutput, error) {
	f.inputs = append(f.inputs, in)
	if f.err != nil {
		return nil, f.err
	}
	out := f.outputs[len(f.inputs)-1]
	return out, nil
}

func group(service, amount string) cetypes.Group {
	return cetypes.Group{
		Keys: []string{service},
		Metrics: map[string]cetypes.MetricValue{
			costMetric: {Amount: aws.String(amount), Unit: aws.String("USD")},
		},
	}
}

func recordsByService(report Report) map[string]float64 {
	out := make(map[string]float64)
	for _, r := range report.Records {
		out[r.Service] = r.AmountUSD
	}
	return out
}

func TestCostExplorer_SumsAcrossBucketsAndServices(t *testing.T) {
	fake := &fakeCE{outputs: []*costexplorer.GetCostAndUsageOutput{{
		ResultsByTime: []cetypes.ResultByTime{
			{Groups: []cetypes.Group{
				group("Amazon Elastic Compute Cloud - Compute", "100.50"),
				group("Amazon RDS Service", "40.00"),
			}},
			{Groups: []cetypes.Group{
				group("Amazon Elastic Compute Cloud - Compute", "99.50"),
			}},
		},
	}}}
	src := NewCostExplorerSource(fake)

	report, err := src.Costs(context.Background(), apr1(), apr30End())
	if err != nil {
		t.Fatalf("Costs: %v", err)
	}
	if report.DataSource != AWSCostExplorerDataSource {
		t.Errorf("DataSource = %q, want %q", report.DataSource, AWSCostExplorerDataSource)
	}
	got := recordsByService(report)
	// ec2 across two buckets: 100.50 + 99.50 = 200; rds: 40.
	if got["amazon elastic compute cloud - compute"] != 200 {
		t.Errorf("ec2 total = %v, want 200", got["amazon elastic compute cloud - compute"])
	}
	if got["amazon rds service"] != 40 {
		t.Errorf("rds total = %v, want 40", got["amazon rds service"])
	}
	for _, r := range report.Records {
		if r.Provider != "aws" {
			t.Errorf("record provider = %q, want aws", r.Provider)
		}
	}
}

func TestCostExplorer_TimePeriodEndIsExclusive(t *testing.T) {
	fake := &fakeCE{outputs: []*costexplorer.GetCostAndUsageOutput{{}}}
	src := NewCostExplorerSource(fake)

	if _, err := src.Costs(context.Background(), apr1(), apr30End()); err != nil {
		t.Fatalf("Costs: %v", err)
	}
	in := fake.inputs[0]
	if got := aws.ToString(in.TimePeriod.Start); got != "2026-04-01" {
		t.Errorf("TimePeriod.Start = %q, want 2026-04-01", got)
	}
	// Inclusive end 2026-04-30 → CE exclusive end is the next day.
	if got := aws.ToString(in.TimePeriod.End); got != "2026-05-01" {
		t.Errorf("TimePeriod.End = %q, want 2026-05-01 (exclusive)", got)
	}
}

func TestCostExplorer_Paginates(t *testing.T) {
	fake := &fakeCE{outputs: []*costexplorer.GetCostAndUsageOutput{
		{
			ResultsByTime: []cetypes.ResultByTime{{Groups: []cetypes.Group{group("ec2", "10")}}},
			NextPageToken: aws.String("page2"),
		},
		{
			ResultsByTime: []cetypes.ResultByTime{{Groups: []cetypes.Group{group("ec2", "5")}}},
		},
	}}
	src := NewCostExplorerSource(fake)

	report, err := src.Costs(context.Background(), apr1(), apr30End())
	if err != nil {
		t.Fatalf("Costs: %v", err)
	}
	if len(fake.inputs) != 2 {
		t.Fatalf("GetCostAndUsage called %d times, want 2", len(fake.inputs))
	}
	if got := aws.ToString(fake.inputs[1].NextPageToken); got != "page2" {
		t.Errorf("second call NextPageToken = %q, want page2", got)
	}
	if recordsByService(report)["ec2"] != 15 {
		t.Errorf("ec2 total = %v, want 15 (10 + 5 across pages)", recordsByService(report)["ec2"])
	}
}

func TestCostExplorer_ErrorWrapsAsSourceError(t *testing.T) {
	fake := &fakeCE{err: errors.New("access denied")}
	src := NewCostExplorerSource(fake)

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

func TestCostExplorer_SkipsGroupsMissingMetric(t *testing.T) {
	fake := &fakeCE{outputs: []*costexplorer.GetCostAndUsageOutput{{
		ResultsByTime: []cetypes.ResultByTime{{Groups: []cetypes.Group{
			{Keys: []string{"ec2"}, Metrics: map[string]cetypes.MetricValue{}},
		}}},
	}}}
	src := NewCostExplorerSource(fake)

	report, err := src.Costs(context.Background(), apr1(), apr30End())
	if err != nil {
		t.Fatalf("Costs: %v", err)
	}
	if len(report.Records) != 0 {
		t.Errorf("records = %v, want none (metric absent)", report.Records)
	}
}

func TestSortRecordsDeterministic(t *testing.T) {
	// Guard: records map iteration order doesn't matter to callers because the
	// API handler sorts; here we just confirm both services survive.
	fake := &fakeCE{outputs: []*costexplorer.GetCostAndUsageOutput{{
		ResultsByTime: []cetypes.ResultByTime{{Groups: []cetypes.Group{
			group("b", "1"), group("a", "2"),
		}}},
	}}}
	report, _ := NewCostExplorerSource(fake).Costs(context.Background(), apr1(), apr30End())
	names := make([]string, 0, len(report.Records))
	for _, r := range report.Records {
		names = append(names, r.Service)
	}
	sort.Strings(names)
	if len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Errorf("services = %v, want [a b]", names)
	}
}

func apr1() time.Time { return time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC) }

func apr30End() time.Time {
	return time.Date(2026, 4, 30, 23, 59, 59, 999999999, time.UTC)
}
