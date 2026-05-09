package aws

// RDSClusterInstanceAttributes captures the cost-impacting fields of an
// aws_rds_cluster_instance resource — i.e. a single Aurora compute node
// attached to a cluster. The cluster header itself (aws_rds_cluster) is
// out of scope for this milestone because its cost is mostly storage,
// while compute-per-instance is what shows up in PRs.
type RDSClusterInstanceAttributes struct {
	// ClusterIdentifier links this instance to its parent cluster.
	// Required — without it, downstream cost analysis can't associate the
	// instance with cluster-level attributes (engine version, storage).
	ClusterIdentifier string

	// InstanceClass is the Aurora compute class, e.g. "db.r5.large". Required.
	// Aurora pricing uses the same db.* family naming as RDS but a different
	// price sheet, so the value passes through untouched.
	InstanceClass string

	// Engine is the Aurora engine variant: "aurora-postgresql",
	// "aurora-mysql", or the legacy "aurora" (MySQL 5.6). Accepted values
	// are intentionally not validated here — the pricing engine in a later
	// milestone owns the catalog of recognized engines.
	Engine string

	// EngineVersion pins the Aurora release. Optional; AWS picks the
	// cluster default when absent.
	EngineVersion string
}

// ExtractRDSClusterInstance reads cost-impacting attributes from an
// aws_rds_cluster_instance attribute map.
//
// Required: cluster_identifier, instance_class, engine. EngineVersion is
// optional. We don't validate engine values against any known list — that
// catalog lives with the pricing engine.
func ExtractRDSClusterInstance(attrs map[string]interface{}) (*RDSClusterInstanceAttributes, error) {
	const typ = "aws_rds_cluster_instance"
	if len(attrs) == 0 {
		return nil, errEmptyAttrs(typ)
	}
	wrap := func(err error) error { return wrapAttr(typ, err) }

	clusterID, present, err := getString(attrs, "cluster_identifier")
	if err != nil {
		return nil, wrap(err)
	}
	if !present {
		return nil, errMissingRequired(typ, "cluster_identifier")
	}

	instanceClass, present, err := getString(attrs, "instance_class")
	if err != nil {
		return nil, wrap(err)
	}
	if !present {
		return nil, errMissingRequired(typ, "instance_class")
	}

	engine, present, err := getString(attrs, "engine")
	if err != nil {
		return nil, wrap(err)
	}
	if !present {
		return nil, errMissingRequired(typ, "engine")
	}

	engineVersion, _, err := getString(attrs, "engine_version")
	if err != nil {
		return nil, wrap(err)
	}

	return &RDSClusterInstanceAttributes{
		ClusterIdentifier: clusterID,
		InstanceClass:     instanceClass,
		Engine:            engine,
		EngineVersion:     engineVersion,
	}, nil
}
