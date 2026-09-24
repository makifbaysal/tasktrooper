package aws

import (
	"context"
	"fmt"
	"strconv"

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

// maxTotalResources caps one ListResources call so an account with an
// unbounded number of services can't turn a single sync into an unbounded
// number of AWS API calls; the caller sees a partial, not an error.
const maxTotalResources = 500

// ecsDescribeBatchSize is ECS's own limit: DescribeServices accepts at most
// 10 service ARNs per call.
const ecsDescribeBatchSize = 10

func (p *Provider) ListResources(ctx context.Context, cred domain.CloudCredential) ([]domain.CloudResource, error) {
	cfg, err := p.config(cred)
	if err != nil {
		return nil, err
	}
	region := cred.Fields[fieldRegion]

	var out []domain.CloudResource

	ecsResources, err := p.listECSServices(ctx, cfg, region)
	if err != nil {
		return nil, err
	}
	out = append(out, ecsResources...)

	if len(out) < maxTotalResources {
		lambdaResources, err := p.listLambdaFunctions(ctx, cfg, region)
		if err != nil {
			return nil, err
		}
		out = append(out, lambdaResources...)
	}

	if len(out) < maxTotalResources {
		appRunnerResources, err := p.listAppRunnerServices(ctx, cfg, region)
		if err != nil {
			return nil, err
		}
		out = append(out, appRunnerResources...)
	}

	if len(out) > maxTotalResources {
		out = out[:maxTotalResources]
	}
	for i := range out {
		out[i].AccountID = cred.AccountID
		out[i].Provider = p.Kind()
	}
	return out, nil
}

func (p *Provider) listECSServices(ctx context.Context, cfg aws.Config, region string) ([]domain.CloudResource, error) {
	client := p.ecsClient(cfg)
	var out []domain.CloudResource

	clusterPaginator := ecs.NewListClustersPaginator(client, &ecs.ListClustersInput{})
	for clusterPaginator.HasMorePages() && len(out) < maxTotalResources {
		page, err := clusterPaginator.NextPage(ctx)
		if err != nil {
			return nil, classify("ecs list clusters", err)
		}
		for _, clusterArn := range page.ClusterArns {
			if len(out) >= maxTotalResources {
				break
			}
			serviceArns, err := p.listECSServiceARNs(ctx, client, clusterArn)
			if err != nil {
				return nil, err
			}
			for i := 0; i < len(serviceArns) && len(out) < maxTotalResources; i += ecsDescribeBatchSize {
				batch := serviceArns[i:min(i+ecsDescribeBatchSize, len(serviceArns))]
				descOut, err := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
					Cluster:  aws.String(clusterArn),
					Services: batch,
				})
				if err != nil {
					return nil, classify("ecs describe services", err)
				}
				for _, svc := range descOut.Services {
					out = append(out, ecsServiceResource(svc, region))
				}
			}
		}
	}
	return out, nil
}

func (p *Provider) listECSServiceARNs(ctx context.Context, client *ecs.Client, clusterArn string) ([]string, error) {
	var arns []string
	paginator := ecs.NewListServicesPaginator(client, &ecs.ListServicesInput{Cluster: aws.String(clusterArn)})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, classify("ecs list services", err)
		}
		arns = append(arns, page.ServiceArns...)
	}
	return arns, nil
}

func ecsServiceResource(svc ecstypes.Service, region string) domain.CloudResource {
	clusterArn := aws.ToString(svc.ClusterArn)
	return domain.CloudResource{
		Ref: domain.CloudResourceRef{
			Kind:   domain.CloudResourceECSService,
			ID:     aws.ToString(svc.ServiceArn),
			Name:   aws.ToString(svc.ServiceName),
			Region: region,
			Extra: map[string]string{
				extraClusterARN:     clusterArn,
				extraClusterName:    shortARNName(clusterArn),
				extraTaskDefinition: aws.ToString(svc.TaskDefinition),
			},
		},
		Labels: map[string]string{
			"launch_type": string(svc.LaunchType),
			"desired":     strconv.Itoa(int(svc.DesiredCount)),
		},
	}
}

func (p *Provider) listLambdaFunctions(ctx context.Context, cfg aws.Config, region string) ([]domain.CloudResource, error) {
	client := p.lambdaClient(cfg)
	var out []domain.CloudResource

	paginator := lambda.NewListFunctionsPaginator(client, &lambda.ListFunctionsInput{})
	for paginator.HasMorePages() && len(out) < maxTotalResources {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, classify("lambda list functions", err)
		}
		for _, fn := range page.Functions {
			out = append(out, lambdaFunctionResource(fn, region))
			if len(out) >= maxTotalResources {
				break
			}
		}
	}
	return out, nil
}

func lambdaFunctionResource(fn lambdatypes.FunctionConfiguration, region string) domain.CloudResource {
	extra := map[string]string{extraRuntime: string(fn.Runtime)}
	if fn.LoggingConfig != nil && aws.ToString(fn.LoggingConfig.LogGroup) != "" {
		extra[extraLogGroup] = aws.ToString(fn.LoggingConfig.LogGroup)
	}
	return domain.CloudResource{
		Ref: domain.CloudResourceRef{
			Kind:   domain.CloudResourceLambdaFunction,
			ID:     aws.ToString(fn.FunctionArn),
			Name:   aws.ToString(fn.FunctionName),
			Region: region,
			Extra:  extra,
		},
	}
}

func (p *Provider) listAppRunnerServices(ctx context.Context, cfg aws.Config, region string) ([]domain.CloudResource, error) {
	client := p.appRunnerClient(cfg)
	var out []domain.CloudResource

	paginator := apprunner.NewListServicesPaginator(client, &apprunner.ListServicesInput{})
	for paginator.HasMorePages() && len(out) < maxTotalResources {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, classify("app runner list services", err)
		}
		for _, svc := range page.ServiceSummaryList {
			out = append(out, appRunnerServiceSummaryResource(svc, region))
			if len(out) >= maxTotalResources {
				break
			}
		}
	}
	return out, nil
}

func appRunnerServiceSummaryResource(svc apprunnertypes.ServiceSummary, region string) domain.CloudResource {
	return domain.CloudResource{
		Ref: domain.CloudResourceRef{
			Kind:   domain.CloudResourceAppRunnerService,
			ID:     aws.ToString(svc.ServiceArn),
			Name:   aws.ToString(svc.ServiceName),
			Region: region,
			Extra:  map[string]string{extraServiceID: aws.ToString(svc.ServiceId)},
		},
		URL: httpsURL(aws.ToString(svc.ServiceUrl)),
	}
}

func (p *Provider) Resource(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef) (domain.CloudResourceDetail, error) {
	cfg, err := p.config(cred)
	if err != nil {
		return domain.CloudResourceDetail{}, err
	}

	var detail domain.CloudResourceDetail
	switch ref.Kind {
	case domain.CloudResourceECSService:
		detail, err = p.ecsServiceDetail(ctx, cfg, ref)
	case domain.CloudResourceLambdaFunction:
		detail, err = p.lambdaFunctionDetail(ctx, cfg, ref)
	case domain.CloudResourceAppRunnerService:
		detail, err = p.appRunnerServiceDetail(ctx, cfg, ref)
	default:
		return domain.CloudResourceDetail{}, fmt.Errorf("%w: aws resource kind %q", port.ErrUnsupported, ref.Kind)
	}
	if err != nil {
		return domain.CloudResourceDetail{}, err
	}
	detail.AccountID = cred.AccountID
	detail.Provider = p.Kind()
	return detail, nil
}

func (p *Provider) ecsServiceDetail(ctx context.Context, cfg aws.Config, ref domain.CloudResourceRef) (domain.CloudResourceDetail, error) {
	clusterArn := ref.Extra[extraClusterARN]
	if clusterArn == "" {
		return domain.CloudResourceDetail{}, fmt.Errorf("aws: ecs service %q has no cluster recorded", ref.Name)
	}
	client := p.ecsClient(cfg)
	out, err := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String(clusterArn),
		Services: []string{ref.ID},
	})
	if err != nil {
		return domain.CloudResourceDetail{}, classify("ecs describe services", err)
	}
	if len(out.Services) == 0 {
		reason := "service not found"
		if len(out.Failures) > 0 {
			reason = aws.ToString(out.Failures[0].Reason)
		}
		return domain.CloudResourceDetail{}, fmt.Errorf("aws ecs describe services: %s: %w", reason, port.ErrNotFound)
	}

	svc := out.Services[0]
	resource := ecsServiceResource(svc, ref.Region)
	status, statusDetail := ecsServiceStatus(svc)
	clusterName := shortARNName(clusterArn)
	familyRev := shortARNName(aws.ToString(svc.TaskDefinition))

	facts := []domain.KeyValue{
		{Label: "Cluster", Value: clusterName},
		{Label: "Launch type", Value: string(svc.LaunchType)},
		{Label: "Running / desired", Value: fmt.Sprintf("%d / %d", svc.RunningCount, svc.DesiredCount)},
		{Label: "Task definition", Value: familyRev},
	}
	if tg := loadBalancerTargetGroup(svc.LoadBalancers); tg != "" {
		facts = append(facts, domain.KeyValue{Label: "Target group", Value: tg})
	}

	return domain.CloudResourceDetail{
		CloudResource: resource,
		Status:        status,
		StatusDetail:  statusDetail,
		Revision:      familyRev,
		ConsoleURL: fmt.Sprintf("https://%s.console.aws.amazon.com/ecs/v2/clusters/%s/services/%s",
			ref.Region, clusterName, aws.ToString(svc.ServiceName)),
		Facts: facts,
	}, nil
}

// ecsServiceStatus mirrors the health a human reads off the ECS console: a
// service is only healthy once its primary deployment has finished rolling
// out AND the task count it asked for matches the task count actually
// running — either one lagging means the service isn't in the state it
// claims to be.
func ecsServiceStatus(svc ecstypes.Service) (domain.CloudResourceStatus, string) {
	var primary *ecstypes.Deployment
	for i := range svc.Deployments {
		if aws.ToString(svc.Deployments[i].Status) == "PRIMARY" {
			primary = &svc.Deployments[i]
			break
		}
	}
	active := aws.ToString(svc.Status) == "ACTIVE"

	if primary != nil {
		switch primary.RolloutState {
		case ecstypes.DeploymentRolloutStateCompleted:
			if active && svc.RunningCount == svc.DesiredCount {
				return domain.CloudStatusHealthy, ""
			}
		case ecstypes.DeploymentRolloutStateInProgress:
			return domain.CloudStatusDeploying, aws.ToString(primary.RolloutStateReason)
		case ecstypes.DeploymentRolloutStateFailed:
			return domain.CloudStatusFailed, aws.ToString(primary.RolloutStateReason)
		}
	}
	if svc.RunningCount == 0 && svc.DesiredCount > 0 {
		return domain.CloudStatusFailed, "no tasks running"
	}
	return domain.CloudStatusDegraded, ""
}

func loadBalancerTargetGroup(lbs []ecstypes.LoadBalancer) string {
	for _, lb := range lbs {
		if tg := aws.ToString(lb.TargetGroupArn); tg != "" {
			return tg
		}
	}
	return ""
}

func (p *Provider) lambdaFunctionDetail(ctx context.Context, cfg aws.Config, ref domain.CloudResourceRef) (domain.CloudResourceDetail, error) {
	client := p.lambdaClient(cfg)
	out, err := client.GetFunction(ctx, &lambda.GetFunctionInput{FunctionName: aws.String(ref.ID)})
	if err != nil {
		return domain.CloudResourceDetail{}, classify("lambda get function", err)
	}
	fnCfg := out.Configuration
	resource := lambdaFunctionResource(*fnCfg, ref.Region)

	urlOut, err := client.GetFunctionUrlConfig(ctx, &lambda.GetFunctionUrlConfigInput{FunctionName: aws.String(ref.ID)})
	switch {
	case err == nil:
		resource.URL = aws.ToString(urlOut.FunctionUrl)
	case errorCode(err) == "ResourceNotFoundException":
		// No function URL configured — not every function has one.
	default:
		return domain.CloudResourceDetail{}, classify("lambda get function url config", err)
	}

	status, statusDetail := lambdaFunctionStatus(*fnCfg)
	facts := []domain.KeyValue{
		{Label: "Runtime", Value: string(fnCfg.Runtime)},
		{Label: "Memory", Value: fmt.Sprintf("%d MB", aws.ToInt32(fnCfg.MemorySize))},
		{Label: "Timeout", Value: fmt.Sprintf("%d s", aws.ToInt32(fnCfg.Timeout))},
		{Label: "Last modified", Value: aws.ToString(fnCfg.LastModified)},
	}

	return domain.CloudResourceDetail{
		CloudResource: resource,
		Status:        status,
		StatusDetail:  statusDetail,
		Revision:      aws.ToString(fnCfg.Version),
		Facts:         facts,
	}, nil
}

func lambdaFunctionStatus(cfg lambdatypes.FunctionConfiguration) (domain.CloudResourceStatus, string) {
	switch cfg.State {
	case lambdatypes.StateFailed:
		return domain.CloudStatusFailed, aws.ToString(cfg.StateReason)
	case lambdatypes.StatePending:
		return domain.CloudStatusDeploying, aws.ToString(cfg.StateReason)
	}
	switch cfg.LastUpdateStatus {
	case lambdatypes.LastUpdateStatusFailed:
		return domain.CloudStatusFailed, aws.ToString(cfg.LastUpdateStatusReason)
	case lambdatypes.LastUpdateStatusInProgress:
		return domain.CloudStatusDeploying, aws.ToString(cfg.LastUpdateStatusReason)
	}
	if cfg.State == lambdatypes.StateActive {
		return domain.CloudStatusHealthy, ""
	}
	return domain.CloudStatusDegraded, ""
}

func (p *Provider) appRunnerServiceDetail(ctx context.Context, cfg aws.Config, ref domain.CloudResourceRef) (domain.CloudResourceDetail, error) {
	client := p.appRunnerClient(cfg)
	out, err := client.DescribeService(ctx, &apprunner.DescribeServiceInput{ServiceArn: aws.String(ref.ID)})
	if err != nil {
		return domain.CloudResourceDetail{}, classify("app runner describe service", err)
	}
	svc := *out.Service
	resource := domain.CloudResource{
		Ref: domain.CloudResourceRef{
			Kind:   domain.CloudResourceAppRunnerService,
			ID:     aws.ToString(svc.ServiceArn),
			Name:   aws.ToString(svc.ServiceName),
			Region: ref.Region,
			Extra:  map[string]string{extraServiceID: aws.ToString(svc.ServiceId)},
		},
		URL: httpsURL(aws.ToString(svc.ServiceUrl)),
	}
	status, statusDetail := appRunnerStatus(svc.Status)
	return domain.CloudResourceDetail{
		CloudResource: resource,
		Status:        status,
		StatusDetail:  statusDetail,
	}, nil
}

func appRunnerStatus(status apprunnertypes.ServiceStatus) (domain.CloudResourceStatus, string) {
	switch status {
	case apprunnertypes.ServiceStatusRunning:
		return domain.CloudStatusHealthy, ""
	case apprunnertypes.ServiceStatusOperationInProgress:
		return domain.CloudStatusDeploying, ""
	case apprunnertypes.ServiceStatusCreateFailed, apprunnertypes.ServiceStatusDeleteFailed:
		return domain.CloudStatusFailed, ""
	case apprunnertypes.ServiceStatusPaused:
		return domain.CloudStatusDegraded, ""
	default:
		return domain.CloudStatusUnknown, string(status)
	}
}
