package billing

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/costmanagement/armcostmanagement"
)

const (
	// AzureCostManagementDataSource marks a Report as real billed cost from the
	// Azure Cost Management Query API — the Azure sibling of
	// AWSCostExplorerDataSource / GCPBigQueryDataSource.
	AzureCostManagementDataSource = "billing_azure_cost_management"
	azureCostManagementNote       = "Costs are actual costs from the Azure Cost " +
		"Management Query API for the requested period (grouped by service)."
)

// costManagementAPI is the slice of *armcostmanagement.QueryClient the source
// needs, narrowed to an interface so tests can inject a fake without reaching
// Azure — the concrete client satisfies it implicitly (same trick as the AWS
// costExplorerAPI and GCP bigQueryAPI sources).
type costManagementAPI interface {
	Usage(
		ctx context.Context,
		scope string,
		params armcostmanagement.QueryDefinition,
		opts *armcostmanagement.QueryClientUsageOptions,
	) (armcostmanagement.QueryClientUsageResponse, error)
}

// CostManagementSource implements Source against the Azure Cost Management
// Query API. scope is the subscription-level scope the query runs against.
type CostManagementSource struct {
	api   costManagementAPI
	scope string
}

func NewCostManagementSource(api costManagementAPI, subscriptionID string) *CostManagementSource {
	return &CostManagementSource{
		api:   api,
		scope: fmt.Sprintf("/subscriptions/%s", subscriptionID),
	}
}

// NewAzureCostManagementSource builds a source backed by a real Query client.
// Credentials come from DefaultAzureCredential (env / managed identity / CLI),
// the same chain the Azure inventory clients use in internal/cloud.
func NewAzureCostManagementSource(
	_ context.Context, subscriptionID string,
) (*CostManagementSource, error) {
	if subscriptionID == "" {
		return nil, fmt.Errorf("AZURE_SUBSCRIPTION_ID is required for the Azure Cost Management source")
	}
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("creating Azure credentials for cost management: %w", err)
	}
	client, err := armcostmanagement.NewQueryClient(cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating Azure Cost Management query client: %w", err)
	}
	return NewCostManagementSource(client, subscriptionID), nil
}

// Costs runs an ActualCost query grouped by ServiceName over [start, end] and
// sums the returned cost per service into one CostRecord each. With no
// granularity set, Azure returns a single aggregated row per service rather
// than daily buckets. The response columns are self-describing, so we locate
// the Cost and ServiceName columns by name instead of trusting positional order.
func (s *CostManagementSource) Costs(
	ctx context.Context, start, end time.Time,
) (Report, error) {
	def := armcostmanagement.QueryDefinition{
		Type:       to.Ptr(armcostmanagement.ExportTypeActualCost),
		Timeframe:  to.Ptr(armcostmanagement.TimeframeTypeCustom),
		TimePeriod: &armcostmanagement.QueryTimePeriod{From: &start, To: &end},
		Dataset: &armcostmanagement.QueryDataset{
			// Granularity omitted (nil): Azure aggregates over the whole period,
			// returning one row per service rather than daily/monthly buckets.
			Aggregation: map[string]*armcostmanagement.QueryAggregation{
				"totalCost": {
					Name:     to.Ptr("Cost"),
					Function: to.Ptr(armcostmanagement.FunctionTypeSum),
				},
			},
			Grouping: []*armcostmanagement.QueryGrouping{{
				Type: to.Ptr(armcostmanagement.QueryColumnTypeDimension),
				Name: to.Ptr("ServiceName"),
			}},
		},
	}

	// ponytail: single page. The Query API paginates large results via
	// Properties.NextLink (a re-POST, not a pager); a service-grouped monthly
	// query returns at most a few dozen rows, so one page covers it. Follow
	// NextLink here if a subscription ever exceeds a page.
	resp, err := s.api.Usage(ctx, s.scope, def, nil)
	if err != nil {
		return Report{}, &SourceError{Code: "billing_query_failed", Err: err}
	}
	props := resp.Properties
	if props == nil {
		return Report{Records: nil, DataSource: AzureCostManagementDataSource, Note: azureCostManagementNote}, nil
	}

	costIdx, serviceIdx := -1, -1
	for i, col := range props.Columns {
		switch strings.ToLower(toStr(col.Name)) {
		case "cost", "pretaxcost":
			costIdx = i
		case "servicename":
			serviceIdx = i
		}
	}
	if costIdx < 0 {
		return Report{}, &SourceError{
			Code: "billing_query_failed",
			Err:  fmt.Errorf("cost column absent from Azure Cost Management response"),
		}
	}

	perService := map[string]float64{}
	for _, row := range props.Rows {
		if costIdx >= len(row) {
			continue
		}
		service := "unknown"
		if serviceIdx >= 0 && serviceIdx < len(row) {
			if name, ok := row[serviceIdx].(string); ok && name != "" {
				service = normalizeService(name)
			}
		}
		perService[service] += toFloat(row[costIdx])
	}

	records := make([]CostRecord, 0, len(perService))
	for service, amount := range perService {
		records = append(records, CostRecord{
			Provider:  "azure",
			Service:   service,
			AmountUSD: amount,
		})
	}
	return Report{
		Records:    records,
		DataSource: AzureCostManagementDataSource,
		Note:       azureCostManagementNote,
	}, nil
}

func toStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// toFloat coerces a Cost Management row cell to float64. The API returns JSON
// numbers, which the SDK decodes as float64, but int is handled defensively.
func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}
