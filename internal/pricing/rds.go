package pricing

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"CloudOracle/internal/iac/aws"
)

// EstimateRDS calculates the monthly cost of an RDS database instance,
// covering both compute (instance hours * 730) and database storage
// (GB-month * AllocatedStorage).
//
// Aurora engines (aurora, aurora-mysql, aurora-postgresql) are NOT
// supported here. Aurora's per-second compute and storage I/O billing
// uses a different Pricing API shape and lives behind aws_rds_cluster /
// aws_rds_cluster_instance — see EstimateRDSClusterInstance (Hito 13.5).
// EstimateRDS returns an error if attrs.Engine starts with "aurora".
//
// Supported engines are postgres, mysql, mariadb. Commercial engines
// (oracle*, sqlserver*) carry license-model branching that is out of
// scope for this milestone.
//
// Assumptions made by this function (each lowers Confidence):
//
//  1. licenseModel = "No license required". Valid for the three
//     supported OSS engines, where AWS does not charge a license fee.
//  2. Multi-AZ doubles compute and storage cost. AWS encodes this in
//     the deploymentOption filter rather than in our math: passing
//     "Multi-AZ" yields a per-hour rate that is already roughly 2x the
//     Single-AZ rate, and the same applies to GB-month storage.
//  3. IOPS-month billing for io1/io2 storage is NOT included. Only the
//     GB-month base rate is charged. A note is appended when storage
//     type is io1/io2 so the omission is visible to users.
//
// Returns an error for nil attrs, empty region/Engine/InstanceClass,
// AllocatedStorage <= 0, Aurora engines, unsupported engines, unknown
// storage types, API failures, missing products, or unit mismatches.
func EstimateRDS(ctx context.Context, src productGetter, attrs *aws.RDSAttributes, region string) (Estimate, error) {
	if region == "" {
		return Estimate{}, fmt.Errorf("EstimateRDS: empty region")
	}
	if attrs == nil {
		return Estimate{}, fmt.Errorf("EstimateRDS: nil attrs")
	}
	if attrs.Engine == "" {
		return Estimate{}, fmt.Errorf("EstimateRDS: empty Engine")
	}
	if strings.HasPrefix(attrs.Engine, "aurora") {
		return Estimate{}, fmt.Errorf("EstimateRDS: Aurora engine %q must be priced via EstimateRDSClusterInstance", attrs.Engine)
	}
	if attrs.InstanceClass == "" {
		return Estimate{}, fmt.Errorf("EstimateRDS: empty InstanceClass")
	}
	if attrs.AllocatedStorage <= 0 {
		return Estimate{}, fmt.Errorf("EstimateRDS: AllocatedStorage must be > 0, got %d", attrs.AllocatedStorage)
	}

	dbEngine, err := mapEngine(attrs.Engine)
	if err != nil {
		return Estimate{}, err
	}
	storageVolType, err := mapRDSStorageType(attrs.StorageType)
	if err != nil {
		return Estimate{}, err
	}
	deployment := mapDeploymentOption(attrs.MultiAZ)

	compute, err := lookupRDSComputePrice(ctx, src, attrs.InstanceClass, dbEngine, deployment, region)
	if err != nil {
		return Estimate{}, err
	}
	storage, err := lookupRDSStoragePrice(ctx, src, storageVolType, deployment, region, attrs.AllocatedStorage)
	if err != nil {
		return Estimate{}, err
	}

	notes := []string{"License: No license required (postgres/mysql/mariadb)"}
	if attrs.MultiAZ {
		notes = append(notes, "Multi-AZ deployment (2x base price)")
	}
	if attrs.StorageType == "io1" || attrs.StorageType == "io2" {
		notes = append(notes, "io1/io2 IOPS-month billing not included in estimate")
	}

	return Estimate{
		MonthlyUSD: compute + storage,
		Currency:   "USD",
		Breakdown: []LineItem{
			{Component: "Compute", MonthlyUSD: compute},
			{Component: "Storage", MonthlyUSD: storage},
		},
		Confidence: ConfidenceLow,
		Notes:      notes,
	}, nil
}

// lookupRDSComputePrice runs the RDS Pricing API query for a database
// instance and returns the monthly USD cost (hourly price * 730). The
// licenseModel filter is hard-coded to "No license required" because
// EstimateRDS only accepts engines for which that is the correct value.
func lookupRDSComputePrice(ctx context.Context, src productGetter, instanceClass, dbEngine, deployment, region string) (float64, error) {
	filters := map[string]string{
		"productFamily":    "Database Instance",
		"instanceType":     instanceClass,
		"databaseEngine":   dbEngine,
		"deploymentOption": deployment,
		"regionCode":       region,
		"licenseModel":     "No license required",
	}
	products, err := src.GetProducts(ctx, "AmazonRDS", filters)
	if err != nil {
		return 0, fmt.Errorf("EstimateRDS: compute lookup: %w", err)
	}
	if len(products) == 0 {
		return 0, fmt.Errorf("EstimateRDS: no compute price found for %s/%s/%s in %s", instanceClass, dbEngine, deployment, region)
	}
	if len(products) > 1 {
		slog.Warn("pricing: RDS compute query returned multiple products; using first",
			"instanceClass", instanceClass,
			"engine", dbEngine,
			"deployment", deployment,
			"region", region,
			"count", len(products),
		)
	}
	hourly, unit, err := parseOnDemandPriceUSD(products[0])
	if err != nil {
		return 0, fmt.Errorf("EstimateRDS: parsing compute price: %w", err)
	}
	if unit != "Hrs" {
		return 0, fmt.Errorf("EstimateRDS: expected compute unit Hrs, got %q", unit)
	}
	return hourly * HoursPerMonth, nil
}

// lookupRDSStoragePrice runs the RDS Pricing API query for the database
// storage volume and returns the monthly USD cost (GB-month * size).
// Note that RDS uses a different filter vocabulary than EC2/EBS: the
// filter name is volumeType (not volumeApiName) and the values are the
// long-form names produced by mapRDSStorageType.
func lookupRDSStoragePrice(ctx context.Context, src productGetter, storageVolType, deployment, region string, sizeGB int) (float64, error) {
	filters := map[string]string{
		"productFamily":    "Database Storage",
		"volumeType":       storageVolType,
		"deploymentOption": deployment,
		"regionCode":       region,
	}
	products, err := src.GetProducts(ctx, "AmazonRDS", filters)
	if err != nil {
		return 0, fmt.Errorf("EstimateRDS: storage lookup: %w", err)
	}
	if len(products) == 0 {
		return 0, fmt.Errorf("EstimateRDS: no storage price found for %s/%s in %s", storageVolType, deployment, region)
	}
	if len(products) > 1 {
		slog.Warn("pricing: RDS storage query returned multiple products; using first",
			"volumeType", storageVolType,
			"deployment", deployment,
			"region", region,
			"count", len(products),
		)
	}
	gbMo, unit, err := parseOnDemandPriceUSD(products[0])
	if err != nil {
		return 0, fmt.Errorf("EstimateRDS: parsing storage price: %w", err)
	}
	if unit != "GB-Mo" {
		return 0, fmt.Errorf("EstimateRDS: expected storage unit GB-Mo, got %q", unit)
	}
	return gbMo * float64(sizeGB), nil
}

// mapEngine converts a Terraform RDS engine value to the Pricing API's
// databaseEngine filter value. Only postgres, mysql, and mariadb are
// supported; any other engine (including Aurora variants) returns an
// error so the caller produces a precise message at the boundary.
func mapEngine(engine string) (string, error) {
	switch engine {
	case "postgres":
		return "PostgreSQL", nil
	case "mysql":
		return "MySQL", nil
	case "mariadb":
		return "MariaDB", nil
	default:
		return "", fmt.Errorf("EstimateRDS: engine %q not supported in this version", engine)
	}
}

// mapRDSStorageType converts a Terraform RDS storage_type to the Pricing
// API's volumeType filter value. Note that RDS uses a different field
// (volumeType) and value vocabulary than EC2/EBS — they happen to model
// the same physical storage but the Pricing API surfaces them separately.
//
// Empty input defaults to "gp2", matching the AWS provider default for
// aws_db_instance.storage_type. Unknown input returns an error.
func mapRDSStorageType(tfType string) (string, error) {
	switch tfType {
	case "", "gp2":
		return "General Purpose", nil
	case "gp3":
		return "General Purpose-GP3", nil
	case "io1":
		return "Provisioned IOPS", nil
	case "io2":
		return "Provisioned IOPS-IO2", nil
	case "standard":
		return "Magnetic", nil
	default:
		return "", fmt.Errorf("EstimateRDS: unknown storage type %q", tfType)
	}
}

// mapDeploymentOption converts a Multi-AZ bool to the Pricing API's
// deploymentOption filter value. Single-AZ is the cost baseline;
// Multi-AZ roughly doubles both compute and storage line items in
// AWS's catalogue (the doubling is encoded in the catalogue rates, not
// applied by us).
func mapDeploymentOption(multiAZ bool) string {
	if multiAZ {
		return "Multi-AZ"
	}
	return "Single-AZ"
}
