package pricing

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"CloudOracle/internal/iac/gcp"
)

const (
	// defaultSQLDiskSizeGB is Cloud SQL's default storage when settings.disk_size
	// is omitted.
	defaultSQLDiskSizeGB = 10
	defaultSQLDiskType   = "PD_SSD"
)

// errUnpricedGCPSQLTier marks a Cloud SQL instance we can't price (unknown/absent
// tier, unknown disk type). errSQLServerNotModeled marks SQL Server, whose
// per-vCPU rate bundles licensing we don't model. Both route to a Skipped change;
// the "unsupported" prefix buckets them with unsupported types in plan notes.
var (
	errUnpricedGCPSQLTier  = errors.New("unsupported Cloud SQL tier not priced")
	errSQLServerNotModeled = errors.New("unsupported SQL Server pricing (licensing) not modeled")
)

// EstimateGCPSQLInstance calculates the monthly cost of a Cloud SQL instance
// (compute + storage) from the embedded static rates. Custom tiers are priced
// per vCPU-hour + per GB-RAM-hour; shared-core tiers are flat; REGIONAL (HA)
// availability doubles both compute and storage. Priced at US rates (region
// variation not modeled), Medium confidence with the static-table caveat.
func EstimateGCPSQLInstance(attrs *gcp.SQLInstanceAttributes) (Estimate, error) {
	if attrs == nil {
		return Estimate{}, fmt.Errorf("EstimateGCPSQLInstance: nil attrs")
	}
	if strings.HasPrefix(attrs.DatabaseVersion, "SQLSERVER") {
		return Estimate{}, errSQLServerNotModeled
	}
	if attrs.Tier == "" {
		return Estimate{}, fmt.Errorf("%w: no tier in plan", errUnpricedGCPSQLTier)
	}

	hourly, ok := gcpSQLComputeHourly(attrs.Tier)
	if !ok {
		return Estimate{}, fmt.Errorf("%w: %q", errUnpricedGCPSQLTier, attrs.Tier)
	}
	compute := hourly * HoursPerMonth

	notes := []string{
		"Priced from a static GCP price table at US rates (may drift; other regions differ)",
	}

	diskSize := attrs.DiskSizeGB
	if diskSize <= 0 {
		diskSize = defaultSQLDiskSizeGB
		notes = append(notes, fmt.Sprintf("Disk size not in plan; defaulted to %d GB", defaultSQLDiskSizeGB))
	}
	storageRate, ok := gcpSQLStorageGBMonth(attrs.DiskType)
	if !ok {
		return Estimate{}, fmt.Errorf("%w: unknown disk type %q", errUnpricedGCPSQLTier, attrs.DiskType)
	}
	storage := storageRate * float64(diskSize)

	if attrs.Regional {
		compute *= 2
		storage *= 2
		notes = append(notes, "REGIONAL (HA) availability doubles compute and storage")
	}

	return Estimate{
		MonthlyUSD: compute + storage,
		Currency:   "USD",
		Breakdown: []LineItem{
			{Component: "Compute", MonthlyUSD: compute},
			{Component: "Storage", MonthlyUSD: storage},
		},
		Confidence: ConfidenceMedium,
		Notes:      notes,
	}, nil
}

// gcpSQLComputeHourly returns the compute hourly rate for a Cloud SQL tier.
// Shared-core tiers (db-f1-micro, db-g1-small) are a flat lookup; everything
// else is parsed into vCPU + RAM and priced per-unit.
func gcpSQLComputeHourly(tier string) (float64, bool) {
	cs := gcpPrices.CloudSQL
	if rate, ok := cs.SharedCoreHourlyUSD[tier]; ok {
		return rate, true
	}
	vcpu, memGB, ok := parseSQLTier(tier)
	if !ok {
		return 0, false
	}
	return float64(vcpu)*cs.VCPUHourlyUSD + memGB*cs.RAMGBHourlyUSD, true
}

// parseSQLTier extracts vCPU count and RAM (GB) from a non-shared Cloud SQL tier:
//
//   - db-custom-<vcpu>-<mem_mb>  → vcpu, mem_mb/1024
//   - db-n1-standard-<n>         → n vCPU, 3.75 GB each
//   - db-n1-highmem-<n>          → n vCPU, 6.5 GB each
//   - db-n1-highcpu-<n>          → n vCPU, 0.9 GB each
//
// Returns ok=false for any other shape so the caller can Skip it.
func parseSQLTier(tier string) (vcpu int, memGB float64, ok bool) {
	parts := strings.Split(tier, "-")
	if len(parts) != 4 || parts[0] != "db" {
		return 0, 0, false
	}
	switch parts[1] {
	case "custom":
		v, err1 := strconv.Atoi(parts[2])
		m, err2 := strconv.Atoi(parts[3])
		if err1 != nil || err2 != nil || v <= 0 || m <= 0 {
			return 0, 0, false
		}
		return v, float64(m) / 1024.0, true
	case "n1":
		n, err := strconv.Atoi(parts[3])
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		switch parts[2] {
		case "standard":
			return n, 3.75 * float64(n), true
		case "highmem":
			return n, 6.5 * float64(n), true
		case "highcpu":
			return n, 0.9 * float64(n), true
		}
	}
	return 0, 0, false
}

// gcpSQLStorageGBMonth returns the Cloud SQL storage $/GB-month for a disk type,
// defaulting an empty type to PD_SSD.
func gcpSQLStorageGBMonth(diskType string) (float64, bool) {
	if diskType == "" {
		diskType = defaultSQLDiskType
	}
	p, ok := gcpPrices.CloudSQL.StorageGBMonthUSD[diskType]
	return p, ok
}
