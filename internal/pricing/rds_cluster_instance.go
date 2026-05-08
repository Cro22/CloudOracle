package pricing

import (
	"context"
	"fmt"
	"log/slog"

	"CloudOracle/internal/iac/aws"
)

// EstimateRDSClusterInstance calculates the monthly cost of a single
// Aurora cluster instance (writer or reader replica). Unlike
// aws_db_instance, this is for aws_rds_cluster_instance, which is the
// unit of compute in Aurora's storage-decoupled architecture.
//
// Aurora bills storage and I/O at the cluster level (aws_rds_cluster),
// not per-instance, so this function returns ONLY compute. The cluster
// header's storage cost is out of scope here — pricing it would need
// EstimateRDSCluster, which is not implemented.
//
// Supported engines: aurora-postgresql, aurora-mysql, the legacy
// "aurora" (MySQL 5.6). Other engines return an error so callers don't
// silently mis-price an unrelated cluster type.
//
// Assumptions made by this function (each lowers Confidence to Low):
//
//  1. Single-AZ deploymentOption. Aurora's redundancy model is
//     "multiple instances across AZs in one cluster", so the per-instance
//     pricing row is always Single-AZ — multi-AZ is achieved by adding
//     more aws_rds_cluster_instance resources, not by toggling a flag.
//  2. licenseModel = "No license required". Aurora doesn't charge a
//     license fee on top of the compute rate.
//
// Returns an error for nil attrs, empty region/Engine/InstanceClass,
// unsupported engines, API failures, missing products, or unit
// mismatches.
func EstimateRDSClusterInstance(ctx context.Context, src productGetter, attrs *aws.RDSClusterInstanceAttributes, region string) (Estimate, error) {
	if region == "" {
		return Estimate{}, fmt.Errorf("EstimateRDSClusterInstance: empty region")
	}
	if attrs == nil {
		return Estimate{}, fmt.Errorf("EstimateRDSClusterInstance: nil attrs")
	}
	if attrs.Engine == "" {
		return Estimate{}, fmt.Errorf("EstimateRDSClusterInstance: empty Engine")
	}
	if attrs.InstanceClass == "" {
		return Estimate{}, fmt.Errorf("EstimateRDSClusterInstance: empty InstanceClass")
	}

	dbEngine, err := mapAuroraEngine(attrs.Engine)
	if err != nil {
		return Estimate{}, err
	}

	filters := map[string]string{
		"productFamily":    "Database Instance",
		"databaseEngine":   dbEngine,
		"instanceType":     attrs.InstanceClass,
		"regionCode":       region,
		"deploymentOption": "Single-AZ",
		"licenseModel":     "No license required",
	}
	products, err := src.GetProducts(ctx, "AmazonRDS", filters)
	if err != nil {
		return Estimate{}, fmt.Errorf("EstimateRDSClusterInstance: lookup: %w", err)
	}
	if len(products) == 0 {
		return Estimate{}, fmt.Errorf("EstimateRDSClusterInstance: no compute price found for %s/%s in %s", attrs.InstanceClass, dbEngine, region)
	}
	if len(products) > 1 {
		slog.Warn("pricing: Aurora cluster instance query returned multiple products; using first",
			"instanceClass", attrs.InstanceClass,
			"engine", dbEngine,
			"region", region,
			"count", len(products),
		)
	}
	hourly, unit, err := parseOnDemandPriceUSD(products[0])
	if err != nil {
		return Estimate{}, fmt.Errorf("EstimateRDSClusterInstance: parsing price: %w", err)
	}
	if unit != "Hrs" {
		return Estimate{}, fmt.Errorf("EstimateRDSClusterInstance: expected unit Hrs, got %q", unit)
	}
	cost := hourly * HoursPerMonth

	return Estimate{
		MonthlyUSD: cost,
		Currency:   "USD",
		Breakdown:  []LineItem{{Component: "Compute", MonthlyUSD: cost}},
		Confidence: ConfidenceLow,
		Notes: []string{
			"Cluster-level storage and I/O charges not included (priced at aws_rds_cluster)",
			"Aurora Multi-AZ is via reader replicas (multiple aws_rds_cluster_instance), not a per-instance flag",
		},
	}, nil
}

// mapAuroraEngine converts a Terraform Aurora engine value to the
// Pricing API's databaseEngine filter value. Aurora MySQL is exposed
// under both "aurora-mysql" (recent) and "aurora" (legacy MySQL 5.6) by
// the AWS provider; both map to the same Pricing API row. Non-Aurora
// engines return an error pointing the caller to EstimateRDS, which is
// the correct entry point for aws_db_instance.
func mapAuroraEngine(engine string) (string, error) {
	switch engine {
	case "aurora-postgresql":
		return "Aurora PostgreSQL", nil
	case "aurora-mysql", "aurora":
		return "Aurora MySQL", nil
	case "postgres", "mysql", "mariadb":
		return "", fmt.Errorf("EstimateRDSClusterInstance: engine %q is non-Aurora; use EstimateRDS", engine)
	default:
		return "", fmt.Errorf("EstimateRDSClusterInstance: unsupported engine %q", engine)
	}
}
