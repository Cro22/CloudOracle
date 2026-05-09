package pricing

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	"github.com/aws/aws-sdk-go-v2/service/pricing/types"
)

// pricingRegion is the AWS region we pin the Pricing API client to. The
// API is only served from us-east-1 and ap-south-1; us-east-1 is the
// default sane choice. See package doc for the full quirk list.
const pricingRegion = "us-east-1"

// maxPages caps GetProducts pagination defensively. A real GetProducts
// call rarely returns more than a handful of pages even with broad
// filters; hitting the cap signals a misconfigured filter set (or a
// pathological mock) rather than a genuinely huge result set.
const maxPages = 100

// pricingAPI is the subset of *pricing.Client we depend on. Defining it
// as an interface lets tests inject a mock without spinning up an
// httptest server. *pricing.Client satisfies this interface naturally.
type pricingAPI interface {
	GetProducts(ctx context.Context, params *pricing.GetProductsInput, opts ...func(*pricing.Options)) (*pricing.GetProductsOutput, error)
}

// Client wraps the AWS Pricing API for product lookups. Construct it
// with NewClient in production code; tests use newClientWithAPI to
// inject a fake implementation of pricingAPI.
type Client struct {
	api pricingAPI
}

// NewClient creates a production Client using the default AWS credentials
// chain (env vars, shared config, EC2/ECS/EKS metadata, etc.).
//
// Region is forced to us-east-1 — the Pricing API is only available
// there and in ap-south-1, and the endpoint region is independent from
// the region of the priced resource (that's a filter value).
func NewClient(ctx context.Context) (*Client, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(pricingRegion))
	if err != nil {
		return nil, fmt.Errorf("pricing: loading AWS config: %w", err)
	}
	return &Client{api: pricing.NewFromConfig(cfg)}, nil
}

// newClientWithAPI is the test-only constructor. Unexported on purpose —
// production code goes through NewClient so the credentials chain and
// region pinning happen exactly once and the same way everywhere.
func newClientWithAPI(api pricingAPI) *Client {
	return &Client{api: api}
}

// GetProducts queries the Pricing API for products matching the given
// filters and returns the raw PriceList strings. Each string is an
// opaque JSON document; this package does NOT decode them — that's the
// job of per-service mappers in a later milestone.
//
// serviceCode examples: "AmazonEC2", "AmazonRDS", "AmazonEBS".
//
// filters is a map of Pricing API field name → value; every entry is
// translated to a TERM_MATCH filter and the filters are ANDed by the
// service. An empty filters map is valid and returns every product for
// the service. Filters are sorted by field name before being sent so
// that two equivalent calls produce byte-identical request payloads
// (helpful for the upcoming caching layer in milestone 13.2).
//
// Pagination is automatic: if the response carries a NextToken, this
// method follows it until the API stops returning one. As a safety net,
// pagination stops after 100 pages and a slog warning is emitted.
//
// Context cancellation is honored between pages — if ctx is cancelled
// mid-pagination, the partially-collected results are discarded and the
// cancellation error is returned wrapped.
func (c *Client) GetProducts(ctx context.Context, serviceCode string, filters map[string]string) ([]string, error) {
	if serviceCode == "" {
		return nil, fmt.Errorf("pricing: empty serviceCode")
	}

	pricingFilters := buildFilters(filters)

	slog.Info("pricing: GetProducts starting",
		"serviceCode", serviceCode,
		"filters", len(pricingFilters),
	)

	var (
		all       []string
		nextToken *string
	)

	for page := 0; page < maxPages; page++ {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("pricing: GetProducts(%s): %w", serviceCode, err)
		}

		sc := serviceCode
		in := &pricing.GetProductsInput{
			ServiceCode: &sc,
			Filters:     pricingFilters,
			NextToken:   nextToken,
		}
		out, err := c.api.GetProducts(ctx, in)
		if err != nil {
			return nil, fmt.Errorf("pricing: GetProducts(%s): %w", serviceCode, err)
		}
		all = append(all, out.PriceList...)

		if out.NextToken == nil || *out.NextToken == "" {
			slog.Info("pricing: GetProducts done",
				"serviceCode", serviceCode,
				"products", len(all),
			)
			return all, nil
		}
		nextToken = out.NextToken
	}

	slog.Warn("pricing: GetProducts hit pagination cap",
		"serviceCode", serviceCode,
		"maxPages", maxPages,
		"products", len(all),
	)
	return all, nil
}

// buildFilters converts the caller's map into a sorted []types.Filter
// where every entry has Type=TERM_MATCH. Sorting by field name keeps
// the request payload deterministic across calls with semantically
// identical filter sets — important for the upcoming cache key.
func buildFilters(filters map[string]string) []types.Filter {
	if len(filters) == 0 {
		return nil
	}
	fields := make([]string, 0, len(filters))
	for k := range filters {
		fields = append(fields, k)
	}
	sort.Strings(fields)

	out := make([]types.Filter, 0, len(fields))
	for _, f := range fields {
		field := f
		value := filters[f]
		out = append(out, types.Filter{
			Type:  types.FilterTypeTermMatch,
			Field: &field,
			Value: &value,
		})
	}
	return out
}
