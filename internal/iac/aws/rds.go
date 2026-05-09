package aws

// RDSAttributes captures the cost-impacting fields of an aws_db_instance
// resource. Note: aws_rds_cluster (Aurora cluster-level) is a separate
// resource type and is out of scope for this milestone.
type RDSAttributes struct {
	// Engine is the DB engine, e.g. "postgres", "mysql", "mariadb",
	// "aurora-postgresql", "aurora-mysql". The aurora-* variants are valid
	// because aws_db_instance is also used to declare Aurora *read replicas*
	// (the cluster itself uses aws_rds_cluster, but instances attached to
	// it are still aws_db_instance). Required.
	Engine string

	// EngineVersion pins the engine release, e.g. "15.4". Optional — when
	// absent, AWS picks the default version.
	EngineVersion string

	// InstanceClass is the DB instance size, e.g. "db.t3.medium". Required.
	InstanceClass string

	// AllocatedStorage is the size in GB of the underlying volume. Required.
	AllocatedStorage int

	// StorageType selects the EBS-backed storage class (gp2 / gp3 / io1 /
	// standard). Defaults to "gp2", matching the AWS provider default.
	StorageType string

	// Iops is the provisioned IOPS for io1/gp3 storage. Zero means
	// "use the storage class default". Cross-attribute consistency
	// (io1 must have iops, gp3 may have iops) is *not* validated here —
	// that's a pricing-engine concern.
	Iops int

	// MultiAZ enables a synchronous standby replica in another AZ.
	// Significant pricing impact (roughly doubles the per-hour cost).
	// Defaults to false.
	MultiAZ bool
}

// ExtractRDS reads cost-impacting attributes from an aws_db_instance
// attribute map.
//
// Required: engine, instance_class, allocated_storage. Defaults:
// storage_type="gp2", iops=0, multi_az=false.
//
// The engine field accepts Aurora variants ("aurora-postgresql",
// "aurora-mysql") because aws_db_instance is the resource type for Aurora
// read replicas; the cluster head uses aws_rds_cluster (not handled here).
func ExtractRDS(attrs map[string]interface{}) (*RDSAttributes, error) {
	const typ = "aws_db_instance"
	if len(attrs) == 0 {
		return nil, errEmptyAttrs(typ)
	}

	engine, present, err := getString(attrs, "engine")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}
	if !present {
		return nil, errMissingRequired(typ, "engine")
	}

	instanceClass, present, err := getString(attrs, "instance_class")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}
	if !present {
		return nil, errMissingRequired(typ, "instance_class")
	}

	allocStorage, present, err := getInt(attrs, "allocated_storage")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}
	if !present {
		return nil, errMissingRequired(typ, "allocated_storage")
	}

	engineVersion, _, err := getString(attrs, "engine_version")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}

	storageType, _, err := getString(attrs, "storage_type")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}
	if storageType == "" {
		storageType = "gp2"
	}

	iops, _, err := getInt(attrs, "iops")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}

	multiAZ, _, err := getBool(attrs, "multi_az")
	if err != nil {
		return nil, wrapAttr(typ, err)
	}

	return &RDSAttributes{
		Engine:           engine,
		EngineVersion:    engineVersion,
		InstanceClass:    instanceClass,
		AllocatedStorage: allocStorage,
		StorageType:      storageType,
		Iops:             iops,
		MultiAZ:          multiAZ,
	}, nil
}
