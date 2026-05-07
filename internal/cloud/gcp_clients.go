package cloud

import (
	"context"

	compute "cloud.google.com/go/compute/apiv1"
	computepb "cloud.google.com/go/compute/apiv1/computepb"
	functions "cloud.google.com/go/functions/apiv2"
	functionspb "cloud.google.com/go/functions/apiv2/functionspb"
	"google.golang.org/api/iterator"
	sqladmin "google.golang.org/api/sqladmin/v1beta4"
)

// The GCP SDK clients return concrete iterator/pager types that are awkward to
// fake directly, so each lister interface flattens pagination into a single
// "give me everything" call. Real implementations wrap the SDK; tests pass
// stubs that return canned slices without touching the network.

type gcpInstancesLister interface {
	listInstances(ctx context.Context) ([]*computepb.Instance, error)
}

type gcpDisksLister interface {
	listDisks(ctx context.Context) ([]*computepb.Disk, error)
}

type gcpSQLLister interface {
	listSQLInstances(ctx context.Context) ([]*sqladmin.DatabaseInstance, error)
}

type gcpFunctionsLister interface {
	listFunctions(ctx context.Context) ([]*functionspb.Function, error)
}

type realGCPInstancesLister struct {
	client    *compute.InstancesClient
	projectID string
}

func (r *realGCPInstancesLister) listInstances(ctx context.Context) ([]*computepb.Instance, error) {
	req := &computepb.AggregatedListInstancesRequest{
		Project: r.projectID,
		Filter:  strPtr("status=RUNNING"),
	}
	var out []*computepb.Instance
	it := r.client.AggregatedList(ctx, req)
	for {
		pair, err := it.Next()
		if err == iterator.Done {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		if pair.Value == nil {
			continue
		}
		out = append(out, pair.Value.GetInstances()...)
	}
}

type realGCPDisksLister struct {
	client    *compute.DisksClient
	projectID string
}

func (r *realGCPDisksLister) listDisks(ctx context.Context) ([]*computepb.Disk, error) {
	req := &computepb.AggregatedListDisksRequest{Project: r.projectID}
	var out []*computepb.Disk
	it := r.client.AggregatedList(ctx, req)
	for {
		pair, err := it.Next()
		if err == iterator.Done {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		if pair.Value == nil {
			continue
		}
		out = append(out, pair.Value.GetDisks()...)
	}
}

type realGCPSQLLister struct {
	service   *sqladmin.Service
	projectID string
}

func (r *realGCPSQLLister) listSQLInstances(ctx context.Context) ([]*sqladmin.DatabaseInstance, error) {
	var out []*sqladmin.DatabaseInstance
	pageToken := ""
	for {
		call := r.service.Instances.List(r.projectID).Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return nil, err
		}
		out = append(out, resp.Items...)
		if resp.NextPageToken == "" {
			return out, nil
		}
		pageToken = resp.NextPageToken
	}
}

type realGCPFunctionsLister struct {
	client    *functions.FunctionClient
	projectID string
}

func (r *realGCPFunctionsLister) listFunctions(ctx context.Context) ([]*functionspb.Function, error) {
	req := &functionspb.ListFunctionsRequest{
		Parent: "projects/" + r.projectID + "/locations/-",
	}
	var out []*functionspb.Function
	it := r.client.ListFunctions(ctx, req)
	for {
		fn, err := it.Next()
		if err == iterator.Done {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		out = append(out, fn)
	}
}
