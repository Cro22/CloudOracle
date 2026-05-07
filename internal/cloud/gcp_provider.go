package cloud

import (
	"CloudOracle/internal/config"
	"CloudOracle/internal/shared"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	compute "cloud.google.com/go/compute/apiv1"
	functions "cloud.google.com/go/functions/apiv2"
	"golang.org/x/sync/errgroup"
	sqladmin "google.golang.org/api/sqladmin/v1beta4"
)

type GCPProvider struct {
	instances      gcpInstancesLister
	disks          gcpDisksLister
	sql            gcpSQLLister
	functions      gcpFunctionsLister
	projectID      string
	serviceTimeout time.Duration
}

func NewGCPProvider(ctx context.Context, cfg config.Config) (*GCPProvider, error) {
	projectID := cfg.Cloud.GCPProject
	if projectID == "" {
		return nil, fmt.Errorf("GOOGLE_CLOUD_PROJECT environment variable is required for GCP provider")
	}

	instancesClient, err := compute.NewInstancesRESTClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("creating Compute Engine instances client: %w", err)
	}

	disksClient, err := compute.NewDisksRESTClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("creating Compute Engine disks client: %w", err)
	}

	sqlService, err := sqladmin.NewService(ctx)
	if err != nil {
		return nil, fmt.Errorf("creating Cloud SQL client: %w", err)
	}

	functionsClient, err := functions.NewFunctionClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("creating Cloud Functions client: %w", err)
	}

	return &GCPProvider{
		instances:      &realGCPInstancesLister{client: instancesClient, projectID: projectID},
		disks:          &realGCPDisksLister{client: disksClient, projectID: projectID},
		sql:            &realGCPSQLLister{service: sqlService, projectID: projectID},
		functions:      &realGCPFunctionsLister{client: functionsClient, projectID: projectID},
		projectID:      projectID,
		serviceTimeout: cfg.ServiceTimeout,
	}, nil
}

func (p *GCPProvider) Name() string {
	return "gcp"
}

func (p *GCPProvider) FetchResources(ctx context.Context) ([]shared.Resource, error) {
	fetchers := []struct {
		name  string
		fetch func(context.Context) ([]shared.Resource, error)
	}{
		{"Compute Engine", p.fetchComputeInstances},
		{"Cloud SQL", p.fetchCloudSQLInstances},
		{"Persistent Disks", p.fetchPersistentDisks},
		{"Cloud Functions", p.fetchCloudFunctions},
	}

	results := make([][]shared.Resource, len(fetchers))
	g, gCtx := errgroup.WithContext(ctx)

	for i, f := range fetchers {
		i, f := i, f
		g.Go(func() error {
			fetchCtx, cancel := context.WithTimeout(gCtx, p.serviceTimeout)
			defer cancel()

			res, err := f.fetch(fetchCtx)
			if err != nil {
				slog.Warn("failed to fetch cloud resources",
					"provider", "gcp",
					"service", f.name,
					"error", err,
				)
				return nil
			}
			results[i] = res
			return nil
		})
	}

	_ = g.Wait()

	var all []shared.Resource
	for _, r := range results {
		all = append(all, r...)
	}
	return all, nil
}

func (p *GCPProvider) fetchComputeInstances(ctx context.Context) ([]shared.Resource, error) {
	instances, err := p.instances.listInstances(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing Compute Engine instances: %w", err)
	}

	var resources []shared.Resource
	for _, instance := range instances {
		zone := extractLastSegment(instance.GetZone())
		labels := instance.GetLabels()
		if len(labels) == 0 {
			labels = nil
		}

		resources = append(resources, shared.Resource{
			ID:           instance.GetName(),
			AccountID:    p.projectID,
			Service:      "compute",
			ResourceType: extractLastSegment(instance.GetMachineType()),
			Region:       extractRegionFromZone(zone),
			MonthlyCost:  0.0,
			UsageMetric:  0.0,
			Tags:         labels,
			CreatedAt:    parseGCPTimestamp(instance.GetCreationTimestamp()),
			UpdatedAt:    time.Now(),
		})
	}
	return resources, nil
}

func (p *GCPProvider) fetchCloudSQLInstances(ctx context.Context) ([]shared.Resource, error) {
	dbs, err := p.sql.listSQLInstances(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing Cloud SQL instances: %w", err)
	}

	var resources []shared.Resource
	for _, db := range dbs {
		var tags map[string]string
		tier := ""
		if db.Settings != nil {
			tier = db.Settings.Tier
			if len(db.Settings.UserLabels) > 0 {
				tags = db.Settings.UserLabels
			}
		}

		resources = append(resources, shared.Resource{
			ID:           db.Name,
			AccountID:    p.projectID,
			Service:      "cloudsql",
			ResourceType: tier,
			Region:       db.Region,
			MonthlyCost:  0.0,
			UsageMetric:  0.0,
			Tags:         tags,
			CreatedAt:    parseGCPTimestamp(db.CreateTime),
			UpdatedAt:    time.Now(),
		})
	}
	return resources, nil
}

func (p *GCPProvider) fetchPersistentDisks(ctx context.Context) ([]shared.Resource, error) {
	disks, err := p.disks.listDisks(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing Persistent Disks: %w", err)
	}

	var resources []shared.Resource
	for _, disk := range disks {
		zone := extractLastSegment(disk.GetZone())
		labels := disk.GetLabels()
		if len(labels) == 0 {
			labels = nil
		}

		resources = append(resources, shared.Resource{
			ID:           disk.GetName(),
			AccountID:    p.projectID,
			Service:      "persistent-disk",
			ResourceType: extractLastSegment(disk.GetType()),
			Region:       extractRegionFromZone(zone),
			MonthlyCost:  0.0,
			UsageMetric:  0.0,
			Tags:         labels,
			CreatedAt:    parseGCPTimestamp(disk.GetCreationTimestamp()),
			UpdatedAt:    time.Now(),
		})
	}
	return resources, nil
}

func (p *GCPProvider) fetchCloudFunctions(ctx context.Context) ([]shared.Resource, error) {
	fns, err := p.functions.listFunctions(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing Cloud Functions: %w", err)
	}

	var resources []shared.Resource
	for _, fn := range fns {
		runtime := ""
		if fn.GetBuildConfig() != nil {
			runtime = fn.GetBuildConfig().GetRuntime()
		}

		var createdAt time.Time
		if fn.GetUpdateTime() != nil {
			createdAt = fn.GetUpdateTime().AsTime()
		} else {
			createdAt = time.Now()
		}

		labels := fn.GetLabels()
		if len(labels) == 0 {
			labels = nil
		}

		resources = append(resources, shared.Resource{
			ID:           extractLastSegment(fn.GetName()),
			AccountID:    p.projectID,
			Service:      "functions",
			ResourceType: runtime,
			Region:       extractFromResourceName(fn.GetName(), "locations"),
			MonthlyCost:  0.0,
			UsageMetric:  0.0,
			Tags:         labels,
			CreatedAt:    createdAt,
			UpdatedAt:    time.Now(),
		})
	}
	return resources, nil
}

func extractLastSegment(url string) string {
	parts := strings.Split(url, "/")
	if len(parts) == 0 {
		return url
	}
	return parts[len(parts)-1]
}

func extractRegionFromZone(zone string) string {
	idx := strings.LastIndex(zone, "-")
	if idx == -1 {
		return zone
	}
	return zone[:idx]
}

func extractFromResourceName(name string, segment string) string {
	parts := strings.Split(name, "/")
	for i, p := range parts {
		if p == segment && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func parseGCPTimestamp(s string) time.Time {
	if s == "" {
		return time.Now()
	}

	t, err := time.Parse(time.RFC3339, s)
	if err == nil {
		return t
	}

	t, err = time.Parse(time.RFC3339Nano, s)
	if err == nil {
		return t
	}

	slog.Warn("could not parse GCP timestamp",
		"provider", "gcp",
		"timestamp", s,
	)
	return time.Now()
}
