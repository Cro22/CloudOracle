package billing

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
)

const (
	// AWSCostExplorerDataSource marks a Report as real billed cost, distinct
	// from the snapshot approximation. The agent / dashboard can drop the
	// "approximation" caveat when they see this.
	AWSCostExplorerDataSource = "billing_aws_cost_explorer"
	awsCostExplorerNote       = "Costs are real unblended costs from the AWS Cost " +
		"Explorer API for the requested period (grouped by service)."
	costMetric = "UnblendedCost"
)

// costExplorerAPI is the slice of *costexplorer.Client the source needs. As
// with the EC2/RDS clients in internal/cloud, narrowing it to an interface lets
// unit tests inject a fake without reaching AWS — the concrete client satisfies
// it implicitly.
type costExplorerAPI interface {
	GetCostAndUsage(
		ctx context.Context,
		in *costexplorer.GetCostAndUsageInput,
		optFns ...func(*costexplorer.Options),
	) (*costexplorer.GetCostAndUsageOutput, error)
}

// CostExplorerSource implements Source against the AWS Cost Explorer API.
type CostExplorerSource struct {
	client costExplorerAPI
}

func NewCostExplorerSource(client costExplorerAPI) *CostExplorerSource {
	return &CostExplorerSource{client: client}
}

// NewAWSCostExplorerSource loads AWS config (region + optional shared profile)
// and builds a source backed by a real Cost Explorer client. Cost Explorer is
// a global service; the region only affects the endpoint.
func NewAWSCostExplorerSource(
	ctx context.Context, region, profile string,
) (*CostExplorerSource, error) {
	opts := []func(*awsconfig.LoadOptions) error{}
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}
	if profile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(profile))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config for cost explorer: %w", err)
	}
	return NewCostExplorerSource(costexplorer.NewFromConfig(cfg)), nil
}

// Costs queries GetCostAndUsage grouped by SERVICE for [start, end] and sums
// the unblended cost across the returned time buckets, one CostRecord per
// service. CE's TimePeriod end is exclusive, so we advance `end` (which the
// handler set to 23:59:59.999 of the closing day) by a nanosecond to roll to
// the following date — keeping the API's inclusive contract.
func (s *CostExplorerSource) Costs(
	ctx context.Context, start, end time.Time,
) (Report, error) {
	in := &costexplorer.GetCostAndUsageInput{
		TimePeriod: &cetypes.DateInterval{
			Start: aws.String(start.Format(time.DateOnly)),
			End:   aws.String(end.Add(time.Nanosecond).Format(time.DateOnly)),
		},
		Granularity: cetypes.GranularityMonthly,
		Metrics:     []string{costMetric},
		GroupBy: []cetypes.GroupDefinition{{
			Type: cetypes.GroupDefinitionTypeDimension,
			Key:  aws.String("SERVICE"),
		}},
	}

	perService := map[string]float64{}
	for {
		out, err := s.client.GetCostAndUsage(ctx, in)
		if err != nil {
			return Report{}, &SourceError{Code: "billing_query_failed", Err: err}
		}
		for _, byTime := range out.ResultsByTime {
			for _, g := range byTime.Groups {
				service := "unknown"
				if len(g.Keys) > 0 {
					service = normalizeService(g.Keys[0])
				}
				metric, ok := g.Metrics[costMetric]
				if !ok {
					continue
				}
				perService[service] += parseAmount(metric.Amount)
			}
		}
		if out.NextPageToken == nil {
			break
		}
		in.NextPageToken = out.NextPageToken
	}

	// Amounts are returned unrounded; the HTTP handler rounds the final
	// aggregates once, the same way it does for the snapshot source.
	records := make([]CostRecord, 0, len(perService))
	for service, amount := range perService {
		records = append(records, CostRecord{
			Provider:  "aws",
			Service:   service,
			AmountUSD: amount,
		})
	}
	return Report{
		Records:    records,
		DataSource: AWSCostExplorerDataSource,
		Note:       awsCostExplorerNote,
	}, nil
}

func parseAmount(raw *string) float64 {
	if raw == nil {
		return 0
	}
	v, err := strconv.ParseFloat(*raw, 64)
	if err != nil {
		return 0
	}
	return v
}

// normalizeService lowercases and trims the Cost Explorer service name. CE uses
// long human names ("Amazon Elastic Compute Cloud - Compute") that don't match
// the snapshot taxonomy (ec2, rds, …); we keep the real billing name rather than
// guess a fuzzy mapping, only normalizing case/whitespace so it's stable.
func normalizeService(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
