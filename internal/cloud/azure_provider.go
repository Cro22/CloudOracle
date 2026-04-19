package cloud

import (
	"CloudOracle/internal/shared"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/appservice/armappservice/v4"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v6"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/sql/armsql/v2"
)

type AzureProvider struct {
	vmClient         *armcompute.VirtualMachinesClient
	disksClient      *armcompute.DisksClient
	sqlServersClient *armsql.ServersClient
	sqlDBClient      *armsql.DatabasesClient
	webAppsClient    *armappservice.WebAppsClient
	subscriptionID   string
}

func NewAzureProvider(ctx context.Context) (*AzureProvider, error) {
	subscriptionID := os.Getenv("AZURE_SUBSCRIPTION_ID")
	if subscriptionID == "" {
		return nil, fmt.Errorf("AZURE_SUBSCRIPTION_ID environment variable is required for Azure provider")
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("creating Azure credentials: %w", err)
	}

	vmClient, err := armcompute.NewVirtualMachinesClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating Azure VM client: %w", err)
	}

	disksClient, err := armcompute.NewDisksClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating Azure Disks client: %w", err)
	}

	sqlServersClient, err := armsql.NewServersClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating Azure SQL Servers client: %w", err)
	}

	sqlDBClient, err := armsql.NewDatabasesClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating Azure SQL Databases client: %w", err)
	}

	webAppsClient, err := armappservice.NewWebAppsClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating Azure Web Apps client: %w", err)
	}

	return &AzureProvider{
		vmClient:         vmClient,
		disksClient:      disksClient,
		sqlServersClient: sqlServersClient,
		sqlDBClient:      sqlDBClient,
		webAppsClient:    webAppsClient,
		subscriptionID:   subscriptionID,
	}, nil
}

func (p *AzureProvider) Name() string {
	return "azure"
}

func (p *AzureProvider) FetchResources(ctx context.Context) ([]shared.Resource, error) {
	var allResources []shared.Resource

	fetchers := []struct {
		name  string
		fetch func(context.Context) ([]shared.Resource, error)
	}{
		{"Virtual Machines", p.fetchVirtualMachines},
		{"Azure SQL", p.fetchSQLDatabases},
		{"Managed Disks", p.fetchManagedDisks},
		{"Functions", p.fetchFunctionApps},
	}

	for _, f := range fetchers {
		resources, err := f.fetch(ctx)
		if err != nil {
			log.Printf("WARNING: failed to fetch %s resources: %v", f.name, err)
			continue
		}
		allResources = append(allResources, resources...)
	}

	return allResources, nil
}

func (p *AzureProvider) fetchVirtualMachines(ctx context.Context) ([]shared.Resource, error) {
	var resources []shared.Resource

	pager := p.vmClient.NewListAllPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing Azure VMs: %w", err)
		}

		for _, vm := range page.Value {
			vmSize := ""
			if vm.Properties != nil && vm.Properties.HardwareProfile != nil && vm.Properties.HardwareProfile.VMSize != nil {
				vmSize = string(*vm.Properties.HardwareProfile.VMSize)
			}

			createdAt := time.Now()
			if vm.Properties != nil && vm.Properties.TimeCreated != nil {
				createdAt = *vm.Properties.TimeCreated
			}

			resources = append(resources, shared.Resource{
				ID:           derefStr(vm.Name),
				AccountID:    p.subscriptionID,
				Service:      "vm",
				ResourceType: vmSize,
				Region:       derefStr(vm.Location),
				MonthlyCost:  0.0,
				UsageMetric:  0.0,
				Tags:         convertAzureTags(vm.Tags),
				CreatedAt:    createdAt,
				UpdatedAt:    time.Now(),
			})
		}
	}

	return resources, nil
}

func (p *AzureProvider) fetchSQLDatabases(ctx context.Context) ([]shared.Resource, error) {
	var resources []shared.Resource

	serverPager := p.sqlServersClient.NewListPager(nil)
	for serverPager.More() {
		serverPage, err := serverPager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing Azure SQL servers: %w", err)
		}

		for _, server := range serverPage.Value {
			resourceGroup := extractResourceGroup(derefStr(server.ID))

			dbPager := p.sqlDBClient.NewListByServerPager(resourceGroup, derefStr(server.Name), nil)
			for dbPager.More() {
				dbPage, err := dbPager.NextPage(ctx)
				if err != nil {
					log.Printf("WARNING: failed to list databases for server %s: %v", derefStr(server.Name), err)
					break
				}

				for _, db := range dbPage.Value {
					sku := ""
					if db.SKU != nil && db.SKU.Name != nil {
						sku = *db.SKU.Name
					}

					createdAt := time.Now()
					if db.Properties != nil && db.Properties.CreationDate != nil {
						createdAt = *db.Properties.CreationDate
					}

					resources = append(resources, shared.Resource{
						ID:           derefStr(db.Name),
						AccountID:    p.subscriptionID,
						Service:      "sql",
						ResourceType: sku,
						Region:       derefStr(db.Location),
						MonthlyCost:  0.0,
						UsageMetric:  0.0,
						Tags:         convertAzureTags(db.Tags),
						CreatedAt:    createdAt,
						UpdatedAt:    time.Now(),
					})
				}
			}
		}
	}

	return resources, nil
}

func (p *AzureProvider) fetchManagedDisks(ctx context.Context) ([]shared.Resource, error) {
	var resources []shared.Resource

	pager := p.disksClient.NewListPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing Azure Managed Disks: %w", err)
		}

		for _, disk := range page.Value {
			skuName := ""
			if disk.SKU != nil && disk.SKU.Name != nil {
				skuName = string(*disk.SKU.Name)
			}

			createdAt := time.Now()
			if disk.Properties != nil && disk.Properties.TimeCreated != nil {
				createdAt = *disk.Properties.TimeCreated
			}

			resources = append(resources, shared.Resource{
				ID:           derefStr(disk.Name),
				AccountID:    p.subscriptionID,
				Service:      "managed-disk",
				ResourceType: skuName,
				Region:       derefStr(disk.Location),
				MonthlyCost:  0.0,
				UsageMetric:  0.0,
				Tags:         convertAzureTags(disk.Tags),
				CreatedAt:    createdAt,
				UpdatedAt:    time.Now(),
			})
		}
	}

	return resources, nil
}

func (p *AzureProvider) fetchFunctionApps(ctx context.Context) ([]shared.Resource, error) {
	var resources []shared.Resource

	pager := p.webAppsClient.NewListPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing Azure Web Apps: %w", err)
		}

		for _, app := range page.Value {
			if app.Kind == nil || !strings.Contains(strings.ToLower(*app.Kind), "functionapp") {
				continue
			}

			createdAt := time.Now()
			if app.Properties != nil && app.Properties.LastModifiedTimeUTC != nil {
				createdAt = *app.Properties.LastModifiedTimeUTC
			}

			resources = append(resources, shared.Resource{
				ID:           derefStr(app.Name),
				AccountID:    p.subscriptionID,
				Service:      "functions",
				ResourceType: derefStr(app.Kind),
				Region:       derefStr(app.Location),
				MonthlyCost:  0.0,
				UsageMetric:  0.0,
				Tags:         convertAzureTags(app.Tags),
				CreatedAt:    createdAt,
				UpdatedAt:    time.Now(),
			})
		}
	}

	return resources, nil
}

func convertAzureTags(tags map[string]*string) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	result := make(map[string]string, len(tags))
	for k, v := range tags {
		if v != nil {
			result[k] = *v
		} else {
			result[k] = ""
		}
	}
	return result
}

func extractResourceGroup(id string) string {
	parts := strings.Split(id, "/")
	for i, p := range parts {
		if strings.EqualFold(p, "resourceGroups") && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
