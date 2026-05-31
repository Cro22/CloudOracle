package cloud

import (
	"CloudOracle/internal/shared"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
)

// fakeEC2 satisfies ec2APIClient. Each test wires up the function fields it
// needs; absent fields panic so a missing expectation surfaces immediately.
type fakeEC2 struct {
	describeInstances func(ctx context.Context, in *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error)
	describeVolumes   func(ctx context.Context, in *ec2.DescribeVolumesInput) (*ec2.DescribeVolumesOutput, error)
}

func (f *fakeEC2) DescribeInstances(ctx context.Context, in *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	return f.describeInstances(ctx, in)
}
func (f *fakeEC2) DescribeVolumes(ctx context.Context, in *ec2.DescribeVolumesInput, _ ...func(*ec2.Options)) (*ec2.DescribeVolumesOutput, error) {
	return f.describeVolumes(ctx, in)
}

type fakeRDS struct {
	describeDBInstances func(ctx context.Context, in *rds.DescribeDBInstancesInput) (*rds.DescribeDBInstancesOutput, error)
	listTagsForResource func(ctx context.Context, in *rds.ListTagsForResourceInput) (*rds.ListTagsForResourceOutput, error)
}

func (f *fakeRDS) DescribeDBInstances(ctx context.Context, in *rds.DescribeDBInstancesInput, _ ...func(*rds.Options)) (*rds.DescribeDBInstancesOutput, error) {
	return f.describeDBInstances(ctx, in)
}
func (f *fakeRDS) ListTagsForResource(ctx context.Context, in *rds.ListTagsForResourceInput, _ ...func(*rds.Options)) (*rds.ListTagsForResourceOutput, error) {
	return f.listTagsForResource(ctx, in)
}

type fakeLambda struct {
	listFunctions func(ctx context.Context, in *lambda.ListFunctionsInput) (*lambda.ListFunctionsOutput, error)
	listTags      func(ctx context.Context, in *lambda.ListTagsInput) (*lambda.ListTagsOutput, error)
}

func (f *fakeLambda) ListFunctions(ctx context.Context, in *lambda.ListFunctionsInput, _ ...func(*lambda.Options)) (*lambda.ListFunctionsOutput, error) {
	return f.listFunctions(ctx, in)
}
func (f *fakeLambda) ListTags(ctx context.Context, in *lambda.ListTagsInput, _ ...func(*lambda.Options)) (*lambda.ListTagsOutput, error) {
	return f.listTags(ctx, in)
}

func newTestAWSProvider(ec2c ec2APIClient, rdsc rdsAPIClient, lc lambdaAPIClient) *AWSProvider {
	return &AWSProvider{
		ec2Client:      ec2c,
		rdsClient:      rdsc,
		lambdaClient:   lc,
		accountID:      "123456789012",
		region:         "us-east-2",
		serviceTimeout: 5 * time.Second,
	}
}

func strP(s string) *string { return &s }

// TestFetchEC2Instances_Pagination verifies that the SDK paginator consumes
// every page, not just the first. It catches exactly the bug introduced if
// someone refactors the fetcher and forgets to call HasMorePages in a loop.
func TestFetchEC2Instances_Pagination(t *testing.T) {
	page1Time := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	page2Time := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)

	calls := 0
	ec2c := &fakeEC2{
		describeInstances: func(_ context.Context, in *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
			calls++
			switch calls {
			case 1:
				return &ec2.DescribeInstancesOutput{
					NextToken: strP("page-2"),
					Reservations: []ec2types.Reservation{{
						Instances: []ec2types.Instance{{
							InstanceId:   strP("i-aaa"),
							InstanceType: ec2types.InstanceTypeT3Micro,
							LaunchTime:   &page1Time,
						}},
					}},
				}, nil
			case 2:
				if in.NextToken == nil || *in.NextToken != "page-2" {
					t.Errorf("expected NextToken=page-2 on second call, got %v", in.NextToken)
				}
				return &ec2.DescribeInstancesOutput{
					Reservations: []ec2types.Reservation{{
						Instances: []ec2types.Instance{{
							InstanceId:   strP("i-bbb"),
							InstanceType: ec2types.InstanceTypeM5Large,
							LaunchTime:   &page2Time,
						}},
					}},
				}, nil
			default:
				t.Fatalf("DescribeInstances called %d times, want 2", calls)
				return nil, nil
			}
		},
	}

	p := newTestAWSProvider(ec2c, nil, nil)
	got, err := p.fetchEC2Instances(context.Background())
	if err != nil {
		t.Fatalf("fetchEC2Instances: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(resources) = %d, want 2 (paginator should exhaust both pages)", len(got))
	}
	if got[0].ID != "i-aaa" || got[1].ID != "i-bbb" {
		t.Errorf("resources = [%s, %s], want [i-aaa, i-bbb]", got[0].ID, got[1].ID)
	}
}

func TestFetchEC2Instances_APIError(t *testing.T) {
	ec2c := &fakeEC2{
		describeInstances: func(context.Context, *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
			return nil, errors.New("AccessDenied")
		},
	}
	p := newTestAWSProvider(ec2c, nil, nil)

	_, err := p.fetchEC2Instances(context.Background())
	if err == nil {
		t.Fatal("expected error when DescribeInstances fails, got nil")
	}
}

func TestFetchEBSVolumes_MapsFields(t *testing.T) {
	createTime := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	tagKey, tagVal := "Owner", "platform"

	ec2c := &fakeEC2{
		describeVolumes: func(context.Context, *ec2.DescribeVolumesInput) (*ec2.DescribeVolumesOutput, error) {
			return &ec2.DescribeVolumesOutput{
				Volumes: []ec2types.Volume{{
					VolumeId:   strP("vol-abc"),
					VolumeType: ec2types.VolumeTypeGp3,
					CreateTime: &createTime,
					Tags:       []ec2types.Tag{{Key: &tagKey, Value: &tagVal}},
				}},
			}, nil
		},
	}
	p := newTestAWSProvider(ec2c, nil, nil)

	got, err := p.fetchEBSVolumes(context.Background())
	if err != nil {
		t.Fatalf("fetchEBSVolumes: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Service != "ebs" || got[0].ID != "vol-abc" || got[0].ResourceType != "gp3" {
		t.Errorf("got = %+v, want service=ebs id=vol-abc type=gp3", got[0])
	}
	if got[0].Tags["Owner"] != "platform" {
		t.Errorf("Tags[Owner] = %q, want platform", got[0].Tags["Owner"])
	}
}

func TestFetchRDSInstances_FetchesTagsPerInstance(t *testing.T) {
	createdAt := time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)
	tagKey, tagVal := "env", "prod"

	rdsc := &fakeRDS{
		describeDBInstances: func(context.Context, *rds.DescribeDBInstancesInput) (*rds.DescribeDBInstancesOutput, error) {
			return &rds.DescribeDBInstancesOutput{
				DBInstances: []rdstypes.DBInstance{{
					DBInstanceIdentifier: strP("db-1"),
					DBInstanceClass:      strP("db.t3.micro"),
					DBInstanceArn:        strP("arn:aws:rds:us-east-2:123:db:db-1"),
					InstanceCreateTime:   &createdAt,
				}},
			}, nil
		},
		listTagsForResource: func(_ context.Context, in *rds.ListTagsForResourceInput) (*rds.ListTagsForResourceOutput, error) {
			if in.ResourceName == nil || *in.ResourceName != "arn:aws:rds:us-east-2:123:db:db-1" {
				t.Errorf("ListTagsForResource called with arn=%v, want db-1 arn", in.ResourceName)
			}
			return &rds.ListTagsForResourceOutput{
				TagList: []rdstypes.Tag{{Key: &tagKey, Value: &tagVal}},
			}, nil
		},
	}
	p := newTestAWSProvider(nil, rdsc, nil)

	got, err := p.fetchRDSInstances(context.Background())
	if err != nil {
		t.Fatalf("fetchRDSInstances: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Tags["env"] != "prod" {
		t.Errorf("Tags[env] = %q, want prod", got[0].Tags["env"])
	}
}

// TestFetchLambdaFunctions_TagFailureDoesNotAbort verifies that an error on
// ListTags for an individual function does not abort the fetch — the function
// is included with tags=nil and the rest of the scan keeps going.
func TestFetchLambdaFunctions_TagFailureDoesNotAbort(t *testing.T) {
	lc := &fakeLambda{
		listFunctions: func(context.Context, *lambda.ListFunctionsInput) (*lambda.ListFunctionsOutput, error) {
			return &lambda.ListFunctionsOutput{
				Functions: []lambdatypes.FunctionConfiguration{{
					FunctionName: strP("fn-broken"),
					FunctionArn:  strP("arn:aws:lambda:us-east-2:123:function:fn-broken"),
					Runtime:      lambdatypes.RuntimePython312,
					LastModified: strP("2026-02-01T00:00:00.000+0000"),
				}},
			}, nil
		},
		listTags: func(context.Context, *lambda.ListTagsInput) (*lambda.ListTagsOutput, error) {
			return nil, errors.New("ThrottlingException")
		},
	}
	p := newTestAWSProvider(nil, nil, lc)

	got, err := p.fetchLambdaFunctions(context.Background())
	if err != nil {
		t.Fatalf("fetchLambdaFunctions: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (tag failure should not drop the function)", len(got))
	}
	if got[0].Tags != nil {
		t.Errorf("Tags = %v, want nil when ListTags fails", got[0].Tags)
	}
}

// TestFetchResources_GracefulDegradation verifies the provider's key contract:
// if ONE service fails, the others keep delivering resources. This is what
// keeps a regional RDS outage from breaking the whole scan.
func TestFetchResources_GracefulDegradation(t *testing.T) {
	now := time.Now()

	ec2c := &fakeEC2{
		describeInstances: func(context.Context, *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
			return &ec2.DescribeInstancesOutput{
				Reservations: []ec2types.Reservation{{
					Instances: []ec2types.Instance{{
						InstanceId:   strP("i-ok"),
						InstanceType: ec2types.InstanceTypeT3Micro,
						LaunchTime:   &now,
					}},
				}},
			}, nil
		},
		describeVolumes: func(context.Context, *ec2.DescribeVolumesInput) (*ec2.DescribeVolumesOutput, error) {
			return nil, errors.New("EBS region down")
		},
	}
	rdsc := &fakeRDS{
		describeDBInstances: func(context.Context, *rds.DescribeDBInstancesInput) (*rds.DescribeDBInstancesOutput, error) {
			return nil, errors.New("RDS unavailable")
		},
		listTagsForResource: func(context.Context, *rds.ListTagsForResourceInput) (*rds.ListTagsForResourceOutput, error) {
			return &rds.ListTagsForResourceOutput{}, nil
		},
	}
	lc := &fakeLambda{
		listFunctions: func(context.Context, *lambda.ListFunctionsInput) (*lambda.ListFunctionsOutput, error) {
			return &lambda.ListFunctionsOutput{
				Functions: []lambdatypes.FunctionConfiguration{{
					FunctionName: strP("fn-ok"),
					FunctionArn:  strP("arn:aws:lambda:us-east-2:123:function:fn-ok"),
					Runtime:      lambdatypes.RuntimeNodejs20x,
					LastModified: strP("2026-02-01T00:00:00.000+0000"),
				}},
			}, nil
		},
		listTags: func(context.Context, *lambda.ListTagsInput) (*lambda.ListTagsOutput, error) {
			return &lambda.ListTagsOutput{}, nil
		},
	}

	p := newTestAWSProvider(ec2c, rdsc, lc)
	got, err := p.FetchResources(context.Background())
	if err != nil {
		t.Fatalf("FetchResources: %v", err)
	}

	services := map[string]int{}
	for _, r := range got {
		services[r.Service]++
	}
	if services["ec2"] != 1 {
		t.Errorf("ec2 count = %d, want 1", services["ec2"])
	}
	if services["lambda"] != 1 {
		t.Errorf("lambda count = %d, want 1", services["lambda"])
	}
	if services["ebs"] != 0 || services["rds"] != 0 {
		t.Errorf("expected ebs and rds to be empty (failed), got %+v", services)
	}
}

// TestFetchResources_AllServicesFail confirms that when everything fails,
// FetchResources returns nil without panicking — the caller gets an empty
// list, not a crash.
func TestFetchResources_AllServicesFail(t *testing.T) {
	failEC2 := &fakeEC2{
		describeInstances: func(context.Context, *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
			return nil, errors.New("boom")
		},
		describeVolumes: func(context.Context, *ec2.DescribeVolumesInput) (*ec2.DescribeVolumesOutput, error) {
			return nil, errors.New("boom")
		},
	}
	failRDS := &fakeRDS{
		describeDBInstances: func(context.Context, *rds.DescribeDBInstancesInput) (*rds.DescribeDBInstancesOutput, error) {
			return nil, errors.New("boom")
		},
	}
	failLambda := &fakeLambda{
		listFunctions: func(context.Context, *lambda.ListFunctionsInput) (*lambda.ListFunctionsOutput, error) {
			return nil, errors.New("boom")
		},
	}

	p := newTestAWSProvider(failEC2, failRDS, failLambda)
	got, err := p.FetchResources(context.Background())
	if err != nil {
		t.Fatalf("FetchResources should not return error on per-service failures, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0 (all services failed)", len(got))
	}
	_ = shared.Resource{} // referenced for the import even if got is empty
}
