package gcp

// SQLInstanceAttributes captures the cost-impacting fields of a
// google_sql_database_instance.
type SQLInstanceAttributes struct {
	// DatabaseVersion, e.g. "POSTGRES_15", "MYSQL_8_0", "SQLSERVER_2019_STANDARD".
	// Used only to detect SQL Server, whose licensing the estimator doesn't model.
	DatabaseVersion string

	// Tier is the machine tier from settings.tier, e.g. "db-custom-2-8192",
	// "db-f1-micro", "db-n1-standard-2". Required for pricing.
	Tier string

	// DiskSizeGB is settings.disk_size. Zero when the plan omits it (Cloud SQL
	// defaults to 10 GB); the estimator applies that default.
	DiskSizeGB int

	// DiskType is settings.disk_type ("PD_SSD" default, "PD_HDD").
	DiskType string

	// Regional is true when settings.availability_type == "REGIONAL" (HA), which
	// roughly doubles compute and storage cost.
	Regional bool
}

// ExtractSQLInstance reads cost-impacting attributes from a
// google_sql_database_instance attribute map. `database_version` is top-level;
// the tier, disk, and availability live in the single `settings` block.
//
// Required: settings.tier. A missing tier routes to a Skipped estimate.
func ExtractSQLInstance(attrs map[string]interface{}) (*SQLInstanceAttributes, error) {
	const typ = "google_sql_database_instance"
	if len(attrs) == 0 {
		return nil, errEmptyAttrs(typ)
	}

	dbVersion, _, err := getString(attrs, "database_version")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}

	out := &SQLInstanceAttributes{DatabaseVersion: dbVersion}

	settings, present, err := getNestedFirst(attrs, "settings")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}
	if !present {
		// No settings block → no tier → nothing to price. Return the shell; the
		// estimator skips on the empty tier.
		return out, nil
	}

	tier, _, err := getString(settings, "tier")
	if err != nil {
		return nil, wrapAttr(typ+".settings", err)
	}
	out.Tier = tier

	diskSize, _, err := getInt(settings, "disk_size")
	if err != nil {
		return nil, wrapAttr(typ+".settings", err)
	}
	out.DiskSizeGB = diskSize

	diskType, _, err := getString(settings, "disk_type")
	if err != nil {
		return nil, wrapAttr(typ+".settings", err)
	}
	out.DiskType = diskType

	availability, _, err := getString(settings, "availability_type")
	if err != nil {
		return nil, wrapAttr(typ+".settings", err)
	}
	out.Regional = availability == "REGIONAL"

	return out, nil
}
