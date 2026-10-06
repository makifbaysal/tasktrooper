package aws

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/apprunner"
	apprunnertypes "github.com/aws/aws-sdk-go-v2/service/apprunner/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	secretstypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Provider implements port.CloudEnvManager per resource kind:
//   - lambda_function: Environment.Variables (encrypted at rest with KMS,
//     Lambda's own way of keeping a function's secrets); a write applies to
//     the next invocation.
//   - app_runner_service: RuntimeEnvironmentVariables, with a secret as a
//     Secrets Manager ARN in RuntimeEnvironmentSecrets; UpdateService starts
//     a deployment.
//   - ecs_service: the serving container's environment, with a secret as a
//     Secrets Manager ARN in secrets[].valueFrom; a write registers a new
//     task definition revision and points the service at it. A deploy that
//     renders the task definition from a file in the repository drops
//     whatever that file does not list, hence OverwrittenOnDeploy.
//
// A secret stored in Secrets Manager is read by the service's own role (the
// App Runner instance role, the ECS task execution role); TaskTrooper never
// changes a role, so that role needs secretsmanager:GetSecretValue on
// tasktrooper/*.
var _ port.CloudEnvManager = (*Provider)(nil)

func (p *Provider) EnvCapabilities(ref domain.CloudResourceRef) (domain.EnvCapabilities, error) {
	production := []domain.DeployEnvironment{domain.EnvironmentProduction}
	switch ref.Kind {
	case domain.CloudResourceLambdaFunction, domain.CloudResourceAppRunnerService:
		return domain.EnvCapabilities{Targets: production, WritesRollOut: true}, nil
	case domain.CloudResourceECSService:
		return domain.EnvCapabilities{Targets: production, WritesRollOut: true, OverwrittenOnDeploy: true}, nil
	}
	return domain.EnvCapabilities{}, port.ErrUnsupported
}

func envVarsOf(names ...map[string]string) []domain.CloudEnvVar {
	seen := map[string]bool{}
	var out []domain.CloudEnvVar
	for _, m := range names {
		for k := range m {
			if k == "" || seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, domain.CloudEnvVar{Key: k, Targets: []domain.DeployEnvironment{domain.EnvironmentProduction}})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func (p *Provider) ListEnvVars(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef) ([]domain.CloudEnvVar, error) {
	if _, err := p.EnvCapabilities(ref); err != nil {
		return nil, err
	}
	cfg, err := p.config(cred)
	if err != nil {
		return nil, err
	}
	switch ref.Kind {
	case domain.CloudResourceLambdaFunction:
		fn, err := p.lambdaClient(cfg).GetFunctionConfiguration(ctx, &lambda.GetFunctionConfigurationInput{FunctionName: aws.String(ref.ID)})
		if err != nil {
			return nil, classify("lambda get function configuration", err)
		}
		var vars map[string]string
		if fn.Environment != nil {
			vars = fn.Environment.Variables
		}
		return envVarsOf(vars), nil
	case domain.CloudResourceAppRunnerService:
		svc, err := p.describeAppRunner(ctx, cfg, ref)
		if err != nil {
			return nil, err
		}
		plain, secrets, err := appRunnerEnv(svc.SourceConfiguration)
		if err != nil {
			return nil, err
		}
		return envVarsOf(plain, secrets), nil
	default:
		t, err := p.describeECS(ctx, cfg, ref)
		if err != nil {
			return nil, err
		}
		c := t.def.ContainerDefinitions[t.container]
		plain := map[string]string{}
		for _, kv := range c.Environment {
			plain[aws.ToString(kv.Name)] = ""
		}
		secrets := map[string]string{}
		for _, s := range c.Secrets {
			secrets[aws.ToString(s.Name)] = ""
		}
		return envVarsOf(plain, secrets), nil
	}
}

func (p *Provider) UpsertEnvVars(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef, vars []domain.CloudEnvWrite) error {
	if _, err := p.EnvCapabilities(ref); err != nil {
		return err
	}
	if len(vars) == 0 {
		return nil
	}
	cfg, err := p.config(cred)
	if err != nil {
		return err
	}
	switch ref.Kind {
	case domain.CloudResourceLambdaFunction:
		return p.upsertLambda(ctx, cfg, ref, vars)
	case domain.CloudResourceAppRunnerService:
		return p.upsertAppRunner(ctx, cfg, ref, vars)
	default:
		return p.upsertECS(ctx, cfg, ref, vars)
	}
}

// Redeploy is not needed on these kinds: every write above already rolls out.
func (p *Provider) Redeploy(context.Context, domain.CloudCredential, domain.CloudResourceRef) (domain.CloudDeployment, error) {
	return domain.CloudDeployment{}, port.ErrUnsupported
}

func (p *Provider) upsertLambda(ctx context.Context, cfg aws.Config, ref domain.CloudResourceRef, vars []domain.CloudEnvWrite) error {
	client := p.lambdaClient(cfg)
	fn, err := client.GetFunctionConfiguration(ctx, &lambda.GetFunctionConfigurationInput{FunctionName: aws.String(ref.ID)})
	if err != nil {
		return classify("lambda get function configuration", err)
	}
	// UpdateFunctionConfiguration replaces the whole map, so every variable
	// already there is sent back.
	merged := map[string]string{}
	if fn.Environment != nil {
		for k, v := range fn.Environment.Variables {
			merged[k] = v
		}
	}
	for _, v := range vars {
		merged[v.Key] = v.Value
	}
	if _, err := client.UpdateFunctionConfiguration(ctx, &lambda.UpdateFunctionConfigurationInput{
		FunctionName: aws.String(ref.ID),
		Environment:  &lambdatypes.Environment{Variables: merged},
	}); err != nil {
		return classifyEnvWrite("lambda update function configuration", "lambda:UpdateFunctionConfiguration", err)
	}
	return nil
}

func (p *Provider) describeAppRunner(ctx context.Context, cfg aws.Config, ref domain.CloudResourceRef) (*apprunnertypes.Service, error) {
	out, err := p.appRunnerClient(cfg).DescribeService(ctx, &apprunner.DescribeServiceInput{ServiceArn: aws.String(ref.ID)})
	if err != nil {
		return nil, classify("app runner describe service", err)
	}
	if out.Service == nil || out.Service.SourceConfiguration == nil {
		return nil, fmt.Errorf("aws: app runner service %q has no source configuration", ref.Name)
	}
	return out.Service, nil
}

var errAppRunnerRepoConfig = errors.New("aws: this App Runner service reads its configuration from apprunner.yaml in the repository — set its variables there")

// appRunnerEnv points at the maps App Runner reads a service's variables
// from; they are mutated in place by the upsert.
func appRunnerEnv(src *apprunnertypes.SourceConfiguration) (plain, secrets map[string]string, err error) {
	if img := src.ImageRepository; img != nil {
		if img.ImageConfiguration == nil {
			img.ImageConfiguration = &apprunnertypes.ImageConfiguration{}
		}
		ic := img.ImageConfiguration
		if ic.RuntimeEnvironmentVariables == nil {
			ic.RuntimeEnvironmentVariables = map[string]string{}
		}
		if ic.RuntimeEnvironmentSecrets == nil {
			ic.RuntimeEnvironmentSecrets = map[string]string{}
		}
		return ic.RuntimeEnvironmentVariables, ic.RuntimeEnvironmentSecrets, nil
	}
	if code := src.CodeRepository; code != nil && code.CodeConfiguration != nil {
		if code.CodeConfiguration.ConfigurationSource != apprunnertypes.ConfigurationSourceApi || code.CodeConfiguration.CodeConfigurationValues == nil {
			return nil, nil, errAppRunnerRepoConfig
		}
		v := code.CodeConfiguration.CodeConfigurationValues
		if v.RuntimeEnvironmentVariables == nil {
			v.RuntimeEnvironmentVariables = map[string]string{}
		}
		if v.RuntimeEnvironmentSecrets == nil {
			v.RuntimeEnvironmentSecrets = map[string]string{}
		}
		return v.RuntimeEnvironmentVariables, v.RuntimeEnvironmentSecrets, nil
	}
	return nil, nil, errors.New("aws: unrecognised App Runner source configuration")
}

func (p *Provider) upsertAppRunner(ctx context.Context, cfg aws.Config, ref domain.CloudResourceRef, vars []domain.CloudEnvWrite) error {
	svc, err := p.describeAppRunner(ctx, cfg, ref)
	if err != nil {
		return err
	}
	plain, secrets, err := appRunnerEnv(svc.SourceConfiguration)
	if err != nil {
		return err
	}
	for _, v := range vars {
		if v.Sensitive {
			arn, err := p.storeSecret(ctx, cfg, ref, v.Key, v.Value)
			if err != nil {
				return err
			}
			delete(plain, v.Key)
			secrets[v.Key] = arn
			continue
		}
		delete(secrets, v.Key)
		plain[v.Key] = v.Value
	}
	if _, err := p.appRunnerClient(cfg).UpdateService(ctx, &apprunner.UpdateServiceInput{
		ServiceArn:          aws.String(ref.ID),
		SourceConfiguration: svc.SourceConfiguration,
	}); err != nil {
		return classifyEnvWrite("app runner update service", "apprunner:UpdateService", err)
	}
	return nil
}

// ecsTarget is an ECS service with its current task definition, that
// definition's tags (returned beside it, not on it) and the index of its
// serving container.
type ecsTarget struct {
	service   ecstypes.Service
	def       *ecstypes.TaskDefinition
	tags      []ecstypes.Tag
	container int
}

func (p *Provider) describeECS(ctx context.Context, cfg aws.Config, ref domain.CloudResourceRef) (ecsTarget, error) {
	clusterArn := ref.Extra[extraClusterARN]
	if clusterArn == "" {
		return ecsTarget{}, fmt.Errorf("aws: ecs service %q has no cluster recorded", ref.Name)
	}
	client := p.ecsClient(cfg)
	out, err := client.DescribeServices(ctx, &ecs.DescribeServicesInput{Cluster: aws.String(clusterArn), Services: []string{ref.ID}})
	if err != nil {
		return ecsTarget{}, classify("ecs describe services", err)
	}
	if len(out.Services) == 0 {
		return ecsTarget{}, fmt.Errorf("aws ecs describe services: %s: %w", ref.Name, port.ErrNotFound)
	}
	svc := out.Services[0]
	td, err := client.DescribeTaskDefinition(ctx, &ecs.DescribeTaskDefinitionInput{
		TaskDefinition: svc.TaskDefinition,
		Include:        []ecstypes.TaskDefinitionField{ecstypes.TaskDefinitionFieldTags},
	})
	if err != nil {
		return ecsTarget{}, classify("ecs describe task definition", err)
	}
	if td.TaskDefinition == nil || len(td.TaskDefinition.ContainerDefinitions) == 0 {
		return ecsTarget{}, fmt.Errorf("aws: ecs service %q's task definition has no container", ref.Name)
	}
	return ecsTarget{service: svc, def: td.TaskDefinition, tags: td.Tags, container: servingContainer(td.TaskDefinition.ContainerDefinitions)}, nil
}

// servingContainer is the essential container with port mappings, else the
// first essential one, else the first.
func servingContainer(defs []ecstypes.ContainerDefinition) int {
	firstEssential := -1
	for i, c := range defs {
		essential := c.Essential == nil || *c.Essential
		if !essential {
			continue
		}
		if len(c.PortMappings) > 0 {
			return i
		}
		if firstEssential < 0 {
			firstEssential = i
		}
	}
	if firstEssential >= 0 {
		return firstEssential
	}
	return 0
}

func (p *Provider) upsertECS(ctx context.Context, cfg aws.Config, ref domain.CloudResourceRef, vars []domain.CloudEnvWrite) error {
	t, err := p.describeECS(ctx, cfg, ref)
	if err != nil {
		return err
	}
	td := t.def
	c := &td.ContainerDefinitions[t.container]
	for _, v := range vars {
		c.Environment = removeKeyValue(c.Environment, v.Key)
		c.Secrets = removeSecret(c.Secrets, v.Key)
		if v.Sensitive {
			arn, err := p.storeSecret(ctx, cfg, ref, v.Key, v.Value)
			if err != nil {
				return err
			}
			c.Secrets = append(c.Secrets, ecstypes.Secret{Name: aws.String(v.Key), ValueFrom: aws.String(arn)})
			continue
		}
		c.Environment = append(c.Environment, ecstypes.KeyValuePair{Name: aws.String(v.Key), Value: aws.String(v.Value)})
	}
	client := p.ecsClient(cfg)
	registered, err := client.RegisterTaskDefinition(ctx, &ecs.RegisterTaskDefinitionInput{
		Family:                  td.Family,
		ContainerDefinitions:    td.ContainerDefinitions,
		Cpu:                     td.Cpu,
		Memory:                  td.Memory,
		ExecutionRoleArn:        td.ExecutionRoleArn,
		TaskRoleArn:             td.TaskRoleArn,
		NetworkMode:             td.NetworkMode,
		Volumes:                 td.Volumes,
		PlacementConstraints:    td.PlacementConstraints,
		RequiresCompatibilities: td.RequiresCompatibilities,
		PidMode:                 td.PidMode,
		IpcMode:                 td.IpcMode,
		ProxyConfiguration:      td.ProxyConfiguration,
		InferenceAccelerators:   td.InferenceAccelerators,
		EphemeralStorage:        td.EphemeralStorage,
		RuntimePlatform:         td.RuntimePlatform,
		Tags:                    t.tags,
	})
	if err != nil {
		return classifyEnvWrite("ecs register task definition", "ecs:RegisterTaskDefinition (and iam:PassRole on the task's roles)", err)
	}
	if _, err := client.UpdateService(ctx, &ecs.UpdateServiceInput{
		Cluster:        t.service.ClusterArn,
		Service:        aws.String(ref.ID),
		TaskDefinition: registered.TaskDefinition.TaskDefinitionArn,
	}); err != nil {
		return classifyEnvWrite("ecs update service", "ecs:UpdateService", err)
	}
	return nil
}

func removeKeyValue(in []ecstypes.KeyValuePair, key string) []ecstypes.KeyValuePair {
	out := in[:0]
	for _, kv := range in {
		if aws.ToString(kv.Name) != key {
			out = append(out, kv)
		}
	}
	return out
}

func removeSecret(in []ecstypes.Secret, key string) []ecstypes.Secret {
	out := in[:0]
	for _, s := range in {
		if aws.ToString(s.Name) != key {
			out = append(out, s)
		}
	}
	return out
}

var secretNameUnsafe = regexp.MustCompile(`[^A-Za-z0-9/_+=.@-]`)

// secretName is the Secrets Manager name for one variable of one resource:
// stable, so a later write puts a new value instead of a new secret.
func secretName(ref domain.CloudResourceRef, key string) string {
	name := ref.Name
	if name == "" {
		name = shortARNName(ref.ID)
	}
	return secretNameUnsafe.ReplaceAllString("tasktrooper/"+name+"/"+key, "-")
}

func (p *Provider) secretsClient(cfg aws.Config) *secretsmanager.Client {
	return secretsmanager.NewFromConfig(cfg, func(o *secretsmanager.Options) { o.BaseEndpoint = p.baseEndpoint() })
}

func (p *Provider) storeSecret(ctx context.Context, cfg aws.Config, ref domain.CloudResourceRef, key, value string) (string, error) {
	client := p.secretsClient(cfg)
	name := secretName(ref, key)
	created, err := client.CreateSecret(ctx, &secretsmanager.CreateSecretInput{
		Name:         aws.String(name),
		SecretString: aws.String(value),
		Tags:         []secretstypes.Tag{{Key: aws.String("managed-by"), Value: aws.String("tasktrooper")}},
	})
	if err == nil {
		return aws.ToString(created.ARN), nil
	}
	if errorCode(err) != "ResourceExistsException" {
		return "", classifyEnvWrite("secrets manager create secret", "secretsmanager:CreateSecret", err)
	}
	put, err := client.PutSecretValue(ctx, &secretsmanager.PutSecretValueInput{
		SecretId:     aws.String(name),
		SecretString: aws.String(value),
	})
	if err != nil {
		return "", classifyEnvWrite("secrets manager put secret value", "secretsmanager:PutSecretValue", err)
	}
	return aws.ToString(put.ARN), nil
}

// classifyEnvWrite names the permission an AccessDenied is missing; the
// operation's own error never carries the value that was written.
func classifyEnvWrite(op, permission string, err error) error {
	switch errorCode(err) {
	case "AccessDenied", "AccessDeniedException":
		return fmt.Errorf("aws %s: %w: the credential needs %s", op, port.ErrCloudWriteDenied, permission)
	}
	return classify(op, err)
}
