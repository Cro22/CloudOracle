package cloud

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/appservice/armappservice/v4"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v6"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/sql/armsql/v2"
)

// Azure SDK pagers are concrete generic types that are awkward to fake from
// outside the SDK package. Each lister interface here flattens pagination
// into a single "list everything" call so tests can return canned slices.

type azureVMLister interface {
	listVMs(ctx context.Context) ([]*armcompute.VirtualMachine, error)
}

type azureDisksLister interface {
	listDisks(ctx context.Context) ([]*armcompute.Disk, error)
}

type azureSQLLister interface {
	listSQLDatabases(ctx context.Context) ([]*armsql.Database, error)
}

type azureWebAppsLister interface {
	listWebApps(ctx context.Context) ([]*armappservice.Site, error)
}

type realAzureVMLister struct {
	client *armcompute.VirtualMachinesClient
}

func (r *realAzureVMLister) listVMs(ctx context.Context) ([]*armcompute.VirtualMachine, error) {
	var out []*armcompute.VirtualMachine
	pager := r.client.NewListAllPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, page.Value...)
	}
	return out, nil
}

type realAzureDisksLister struct {
	client *armcompute.DisksClient
}

func (r *realAzureDisksLister) listDisks(ctx context.Context) ([]*armcompute.Disk, error) {
	var out []*armcompute.Disk
	pager := r.client.NewListPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, page.Value...)
	}
	return out, nil
}

// realAzureSQLLister joins the two-step server -> databases listing into one
// call. A failure listing databases for a single server is logged and skipped
// so other servers still surface their DBs — same contract the inline code had.
type realAzureSQLLister struct {
	servers   *armsql.ServersClient
	databases *armsql.DatabasesClient
}

func (r *realAzureSQLLister) listSQLDatabases(ctx context.Context) ([]*armsql.Database, error) {
	var out []*armsql.Database

	serverPager := r.servers.NewListPager(nil)
	for serverPager.More() {
		serverPage, err := serverPager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing SQL servers: %w", err)
		}
		for _, server := range serverPage.Value {
			rg := extractResourceGroup(derefStr(server.ID))
			dbPager := r.databases.NewListByServerPager(rg, derefStr(server.Name), nil)
			for dbPager.More() {
				dbPage, err := dbPager.NextPage(ctx)
				if err != nil {
					// Match the prior behavior: skip this server and keep going.
					break
				}
				out = append(out, dbPage.Value...)
			}
		}
	}
	return out, nil
}

type realAzureWebAppsLister struct {
	client *armappservice.WebAppsClient
}

func (r *realAzureWebAppsLister) listWebApps(ctx context.Context) ([]*armappservice.Site, error) {
	var out []*armappservice.Site
	pager := r.client.NewListPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, page.Value...)
	}
	return out, nil
}
