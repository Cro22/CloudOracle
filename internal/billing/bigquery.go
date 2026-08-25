package billing

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/bigquery"
	"google.golang.org/api/iterator"
)

const (
	// GCPBigQueryDataSource marks a Report as real billed cost from the GCP
	// billing export, the BigQuery sibling of AWSCostExplorerDataSource. The
	// agent / dashboard can drop the "approximation" caveat when they see this.
	GCPBigQueryDataSource = "billing_gcp_bigquery"
	gcpBigQueryNote       = "Costs are net costs (list cost plus credits) from the " +
		"GCP billing export in BigQuery for the requested period (grouped by service)."
)

// billingRow is one already-parsed (service, cost) pair from the billing-export
// query. The interface returns these instead of a *bigquery.RowIterator so tests
// can inject canned rows without the BigQuery SDK — the same flattening trick the
// GCP inventory listers use in internal/cloud/gcp_clients.go.
type billingRow struct {
	Service string
	Cost    float64
}

type bigQueryAPI interface {
	query(ctx context.Context, sql string) ([]billingRow, error)
}

// BigQuerySource implements Source against the standard GCP billing export
// (a dataset table named like gcp_billing_export_v1_XXXXXX). `table` is the
// fully-qualified, backtick-quoted `project.dataset.table` reference.
type BigQuerySource struct {
	api   bigQueryAPI
	table string
}

func NewBigQuerySource(api bigQueryAPI, table string) *BigQuerySource {
	return &BigQuerySource{api: api, table: table}
}

// NewGCPBigQuerySource builds a source backed by a real BigQuery client.
// Credentials come from Application Default Credentials (GOOGLE_APPLICATION_
// CREDENTIALS or the metadata server), the same as the GCP inventory clients.
func NewGCPBigQuerySource(
	ctx context.Context, projectID, dataset, table string,
) (*BigQuerySource, error) {
	client, err := bigquery.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("creating BigQuery client: %w", err)
	}
	qualified := fmt.Sprintf("`%s.%s.%s`", projectID, dataset, table)
	return NewBigQuerySource(&realBigQuery{client: client}, qualified), nil
}

// Costs runs the grouped-by-service query for [start, end] and sums the net
// cost per service into one CostRecord each. usage_start_time is the export's
// partition column, so filtering on it keeps the scan (and cost) bounded.
func (s *BigQuerySource) Costs(
	ctx context.Context, start, end time.Time,
) (Report, error) {
	rows, err := s.api.query(ctx, s.sql(start, end))
	if err != nil {
		return Report{}, &SourceError{Code: "billing_query_failed", Err: err}
	}

	perService := map[string]float64{}
	for _, r := range rows {
		perService[normalizeService(r.Service)] += r.Cost
	}
	records := make([]CostRecord, 0, len(perService))
	for service, amount := range perService {
		records = append(records, CostRecord{
			Provider:  "gcp",
			Service:   service,
			AmountUSD: amount,
		})
	}
	return Report{
		Records:    records,
		DataSource: GCPBigQueryDataSource,
		Note:       gcpBigQueryNote,
	}, nil
}

// sql builds the billing-export query. Net cost is list `cost` plus the credits
// array (Google's documented "total cost"). The bounds are formatted from
// caller-supplied time.Time values (never user input), so string interpolation
// is safe from injection here. end is the handler's inclusive 23:59:59.999 close.
func (s *BigQuerySource) sql(start, end time.Time) string {
	return fmt.Sprintf(
		"SELECT service.description AS service, "+
			"SUM(cost + IFNULL((SELECT SUM(c.amount) FROM UNNEST(credits) c), 0)) AS cost "+
			"FROM %s "+
			"WHERE usage_start_time >= TIMESTAMP('%s') "+
			"AND usage_start_time <= TIMESTAMP('%s') "+
			"GROUP BY service",
		s.table,
		start.UTC().Format(time.DateTime),
		end.UTC().Format(time.DateTime),
	)
}

// realBigQuery wraps *bigquery.Client, running the query and flattening its
// RowIterator into []billingRow. Null service/cost cells (rounding rows, credits
// with no service) survive as zero-valued fields.
type realBigQuery struct {
	client *bigquery.Client
}

func (r *realBigQuery) query(ctx context.Context, sql string) ([]billingRow, error) {
	it, err := r.client.Query(sql).Read(ctx)
	if err != nil {
		return nil, err
	}
	var out []billingRow
	for {
		var row struct {
			Service bigquery.NullString
			Cost    bigquery.NullFloat64
		}
		switch err := it.Next(&row); err {
		case iterator.Done:
			return out, nil
		case nil:
			out = append(out, billingRow{Service: row.Service.StringVal, Cost: row.Cost.Float64})
		default:
			return nil, err
		}
	}
}
