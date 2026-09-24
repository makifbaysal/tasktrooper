package aws

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/apprunner"
	apprunnertypes "github.com/aws/aws-sdk-go-v2/service/apprunner/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const defaultDeploymentLimit = 10

// maxVersionPages bounds ListVersionsByFunction: 10 pages of 50 versions is
// far more history than the deployments view needs, and Lambda functions
// that publish on every commit can otherwise have thousands of versions.
const maxVersionPages = 10

func (p *Provider) Deployments(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef, limit int) ([]domain.CloudDeployment, error) {
	cfg, err := p.config(cred)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = defaultDeploymentLimit
	}

	switch ref.Kind {
	case domain.CloudResourceECSService:
		return p.ecsDeployments(ctx, cfg, ref, limit)
	case domain.CloudResourceLambdaFunction:
		return p.lambdaDeployments(ctx, cfg, ref, limit)
	case domain.CloudResourceAppRunnerService:
		return p.appRunnerDeployments(ctx, cfg, ref, limit)
	default:
		return nil, fmt.Errorf("%w: aws deployments unsupported for %s", port.ErrUnsupported, ref.Kind)
	}
}

func truncateDeployments(d []domain.CloudDeployment, limit int) []domain.CloudDeployment {
	if limit > 0 && len(d) > limit {
		return d[:limit]
	}
	return d
}

func (p *Provider) ecsDeployments(ctx context.Context, cfg aws.Config, ref domain.CloudResourceRef, limit int) ([]domain.CloudDeployment, error) {
	clusterArn := ref.Extra[extraClusterARN]
	if clusterArn == "" {
		return nil, fmt.Errorf("aws: ecs service %q has no cluster recorded", ref.Name)
	}
	client := p.ecsClient(cfg)
	out, err := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String(clusterArn),
		Services: []string{ref.ID},
	})
	if err != nil {
		return nil, classify("ecs describe services", err)
	}
	if len(out.Services) == 0 {
		return nil, fmt.Errorf("aws ecs describe services: service not found: %w", port.ErrNotFound)
	}

	deployments := make([]domain.CloudDeployment, 0, len(out.Services[0].Deployments))
	for _, d := range out.Services[0].Deployments {
		deployments = append(deployments, ecsDeploymentDomain(d))
	}
	sort.Slice(deployments, func(i, j int) bool { return deployments[i].CreatedAt.After(deployments[j].CreatedAt) })
	return truncateDeployments(deployments, limit), nil
}

func ecsDeploymentDomain(d ecstypes.Deployment) domain.CloudDeployment {
	familyRev := shortARNName(aws.ToString(d.TaskDefinition))
	dep := domain.CloudDeployment{
		ID:            aws.ToString(d.Id),
		Status:        ecsRolloutStatus(d.RolloutState),
		CommitMessage: fmt.Sprintf("task definition %s", familyRev),
		Branch:        strings.ToLower(aws.ToString(d.Status)),
		CreatedAt:     aws.ToTime(d.CreatedAt),
	}
	if d.RolloutState == ecstypes.DeploymentRolloutStateCompleted {
		t := aws.ToTime(d.UpdatedAt)
		dep.ReadyAt = &t
	}
	return dep
}

func ecsRolloutStatus(s ecstypes.DeploymentRolloutState) domain.CloudDeploymentStatus {
	switch s {
	case ecstypes.DeploymentRolloutStateCompleted:
		return domain.CloudDeployReady
	case ecstypes.DeploymentRolloutStateInProgress:
		return domain.CloudDeployBuilding
	case ecstypes.DeploymentRolloutStateFailed:
		return domain.CloudDeployError
	default:
		return domain.CloudDeployUnknown
	}
}

func (p *Provider) lambdaDeployments(ctx context.Context, cfg aws.Config, ref domain.CloudResourceRef, limit int) ([]domain.CloudDeployment, error) {
	client := p.lambdaClient(cfg)
	var versions []lambdatypes.FunctionConfiguration

	paginator := lambda.NewListVersionsByFunctionPaginator(client, &lambda.ListVersionsByFunctionInput{
		FunctionName: aws.String(ref.ID),
	})
	for page := 0; paginator.HasMorePages() && page < maxVersionPages; page++ {
		out, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, classify("lambda list versions by function", err)
		}
		versions = append(versions, out.Versions...)
	}

	deployments := make([]domain.CloudDeployment, 0, len(versions))
	for _, v := range versions {
		deployments = append(deployments, lambdaVersionDeployment(v))
	}
	sort.Slice(deployments, func(i, j int) bool { return deployments[i].CreatedAt.After(deployments[j].CreatedAt) })
	return truncateDeployments(deployments, limit), nil
}

func lambdaVersionDeployment(v lambdatypes.FunctionConfiguration) domain.CloudDeployment {
	t := parseAWSTime(aws.ToString(v.LastModified))
	return domain.CloudDeployment{
		ID:            aws.ToString(v.Version),
		Status:        domain.CloudDeployReady,
		CommitMessage: aws.ToString(v.Description),
		CreatedAt:     t,
		ReadyAt:       &t,
	}
}

func (p *Provider) appRunnerDeployments(ctx context.Context, cfg aws.Config, ref domain.CloudResourceRef, limit int) ([]domain.CloudDeployment, error) {
	client := p.appRunnerClient(cfg)
	var ops []apprunnertypes.OperationSummary

	paginator := apprunner.NewListOperationsPaginator(client, &apprunner.ListOperationsInput{
		ServiceArn: aws.String(ref.ID),
	})
	for paginator.HasMorePages() && len(ops) < limit {
		out, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, classify("app runner list operations", err)
		}
		ops = append(ops, out.OperationSummaryList...)
	}

	deployments := make([]domain.CloudDeployment, 0, len(ops))
	for _, op := range ops {
		deployments = append(deployments, appRunnerOperationDeployment(op))
	}
	return truncateDeployments(deployments, limit), nil
}

func appRunnerOperationDeployment(op apprunnertypes.OperationSummary) domain.CloudDeployment {
	dep := domain.CloudDeployment{
		ID:            aws.ToString(op.Id),
		Status:        appRunnerOperationStatus(op.Status),
		CommitMessage: string(op.Type),
		CreatedAt:     aws.ToTime(op.StartedAt),
	}
	if op.EndedAt != nil {
		t := aws.ToTime(op.EndedAt)
		dep.ReadyAt = &t
	}
	return dep
}

func appRunnerOperationStatus(s apprunnertypes.OperationStatus) domain.CloudDeploymentStatus {
	switch s {
	case apprunnertypes.OperationStatusSucceeded, apprunnertypes.OperationStatusRollbackSucceeded:
		return domain.CloudDeployReady
	case apprunnertypes.OperationStatusPending, apprunnertypes.OperationStatusInProgress, apprunnertypes.OperationStatusRollbackInProgress:
		return domain.CloudDeployBuilding
	case apprunnertypes.OperationStatusFailed, apprunnertypes.OperationStatusRollbackFailed:
		return domain.CloudDeployError
	default:
		return domain.CloudDeployUnknown
	}
}
