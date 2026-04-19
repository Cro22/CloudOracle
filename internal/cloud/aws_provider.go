package cloud

import (
	"CloudOracle/internal/shared"
	"context"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

const awsRegion = "us-east-2"
const awsProfile = "cloudoracle"

type AWSProvider struct {
	ec2Client    *ec2.Client
	rdsClient    *rds.Client
	lambdaClient *lambda.Client
	stsClient    *sts.Client
	accountID    string
}

func NewAWSProvider(ctx context.Context) (*AWSProvider, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithSharedConfigProfile(awsProfile),
		config.WithRegion(awsRegion),
	)
	if err != nil {
		return nil, fmt.Errorf("loading AWS configuration (profile=%s, region=%s): %w",
			awsProfile, awsRegion, err)
	}

	stsClient := sts.NewFromConfig(cfg)
	ec2Client := ec2.NewFromConfig(cfg)
	rdsClient := rds.NewFromConfig(cfg)
	lambdaClient := lambda.NewFromConfig(cfg)

	identity, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, fmt.Errorf("validating AWS credentials via STS (profile=%s): %w",
			awsProfile, err)
	}

	return &AWSProvider{
		ec2Client:    ec2Client,
		rdsClient:    rdsClient,
		lambdaClient: lambdaClient,
		stsClient:    stsClient,
		accountID:    *identity.Account,
	}, nil
}

func (p *AWSProvider) Name() string {
	return "aws"
}

func (p *AWSProvider) FetchResources(ctx context.Context) ([]shared.Resource, error) {
	var allResources []shared.Resource

	fetchers := []struct {
		name  string
		fetch func(context.Context) ([]shared.Resource, error)
	}{
		{"EC2", p.fetchEC2Instances},
		{"RDS", p.fetchRDSInstances},
		{"EBS", p.fetchEBSVolumes},
		{"Lambda", p.fetchLambdaFunctions},
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

func (p *AWSProvider) fetchEC2Instances(ctx context.Context) ([]shared.Resource, error) {
	input := &ec2.DescribeInstancesInput{
		Filters: []ec2types.Filter{
			{
				Name:   strPtr("instance-state-name"),
				Values: []string{"running"},
			},
		},
	}

	paginator := ec2.NewDescribeInstancesPaginator(p.ec2Client, input)

	var resources []shared.Resource

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describing EC2 instances: %w", err)
		}

		for _, reservation := range page.Reservations {
			for _, instance := range reservation.Instances {
				resources = append(resources, mapEC2ToResource(instance, p.accountID, awsRegion))
			}
		}
	}

	return resources, nil
}

func mapEC2ToResource(instance ec2types.Instance, accountID, region string) shared.Resource {
	return shared.Resource{
		ID:           *instance.InstanceId,
		AccountID:    accountID,
		Service:      "ec2",
		ResourceType: string(instance.InstanceType),
		Region:       region,
		MonthlyCost:  0.0,
		UsageMetric:  0.0,
		Tags:         convertEC2Tags(instance.Tags),
		CreatedAt:    *instance.LaunchTime,
		UpdatedAt:    time.Now(),
	}
}

func (p *AWSProvider) fetchRDSInstances(ctx context.Context) ([]shared.Resource, error) {
	paginator := rds.NewDescribeDBInstancesPaginator(p.rdsClient, &rds.DescribeDBInstancesInput{})

	var resources []shared.Resource

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describing RDS instances: %w", err)
		}

		for _, db := range page.DBInstances {
			tags := p.fetchRDSTags(ctx, db.DBInstanceArn)

			createdAt := time.Now()
			if db.InstanceCreateTime != nil {
				createdAt = *db.InstanceCreateTime
			}

			resources = append(resources, shared.Resource{
				ID:           *db.DBInstanceIdentifier,
				AccountID:    p.accountID,
				Service:      "rds",
				ResourceType: *db.DBInstanceClass,
				Region:       awsRegion,
				MonthlyCost:  0.0,
				UsageMetric:  0.0,
				Tags:         tags,
				CreatedAt:    createdAt,
				UpdatedAt:    time.Now(),
			})
		}
	}

	return resources, nil
}

func (p *AWSProvider) fetchRDSTags(ctx context.Context, arn *string) map[string]string {
	if arn == nil {
		return nil
	}

	output, err := p.rdsClient.ListTagsForResource(ctx, &rds.ListTagsForResourceInput{
		ResourceName: arn,
	})
	if err != nil {
		log.Printf("WARNING: failed to fetch RDS tags for %s: %v", *arn, err)
		return nil
	}

	return convertRDSTags(output.TagList)
}

func (p *AWSProvider) fetchEBSVolumes(ctx context.Context) ([]shared.Resource, error) {
	paginator := ec2.NewDescribeVolumesPaginator(p.ec2Client, &ec2.DescribeVolumesInput{})

	var resources []shared.Resource

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describing EBS volumes: %w", err)
		}

		for _, vol := range page.Volumes {
			resources = append(resources, shared.Resource{
				ID:           *vol.VolumeId,
				AccountID:    p.accountID,
				Service:      "ebs",
				ResourceType: string(vol.VolumeType),
				Region:       awsRegion,
				MonthlyCost:  0.0,
				UsageMetric:  0.0,
				Tags:         convertEC2Tags(vol.Tags),
				CreatedAt:    *vol.CreateTime,
				UpdatedAt:    time.Now(),
			})
		}
	}

	return resources, nil
}

func (p *AWSProvider) fetchLambdaFunctions(ctx context.Context) ([]shared.Resource, error) {
	paginator := lambda.NewListFunctionsPaginator(p.lambdaClient, &lambda.ListFunctionsInput{})

	var resources []shared.Resource

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing Lambda functions: %w", err)
		}

		for _, fn := range page.Functions {
			createdAt := parseLambdaTimestamp(fn.LastModified)
			tags := p.fetchLambdaTags(ctx, fn.FunctionArn)

			resources = append(resources, shared.Resource{
				ID:           *fn.FunctionName,
				AccountID:    p.accountID,
				Service:      "lambda",
				ResourceType: string(fn.Runtime),
				Region:       awsRegion,
				MonthlyCost:  0.0,
				UsageMetric:  0.0,
				Tags:         tags,
				CreatedAt:    createdAt,
				UpdatedAt:    time.Now(),
			})
		}
	}

	return resources, nil
}

func (p *AWSProvider) fetchLambdaTags(ctx context.Context, arn *string) map[string]string {
	if arn == nil {
		return nil
	}

	output, err := p.lambdaClient.ListTags(ctx, &lambda.ListTagsInput{
		Resource: arn,
	})
	if err != nil {
		log.Printf("WARNING: failed to fetch Lambda tags for %s: %v", *arn, err)
		return nil
	}

	if len(output.Tags) == 0 {
		return nil
	}
	return output.Tags
}

func parseLambdaTimestamp(s *string) time.Time {
	if s == nil {
		return time.Now()
	}

	t, err := time.Parse("2006-01-02T15:04:05.000+0000", *s)
	if err == nil {
		return t
	}

	t, err = time.Parse(time.RFC3339, *s)
	if err == nil {
		return t
	}

	log.Printf("WARNING: could not parse Lambda timestamp %q, using current time", *s)
	return time.Now()
}

func convertEC2Tags(tags []ec2types.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}

	result := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := ""
		if tag.Key != nil {
			key = *tag.Key
		}
		value := ""
		if tag.Value != nil {
			value = *tag.Value
		}
		result[key] = value
	}
	return result
}

func convertRDSTags(tags []rdstypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}

	result := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := ""
		if tag.Key != nil {
			key = *tag.Key
		}
		value := ""
		if tag.Value != nil {
			value = *tag.Value
		}
		result[key] = value
	}
	return result
}

func strPtr(s string) *string {
	return &s
}
