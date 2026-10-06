package aws

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const envFnArn = "arn:aws:lambda:us-east-1:111122223333:function:fn-1"

func lambdaRef() domain.CloudResourceRef {
	return domain.CloudResourceRef{Kind: domain.CloudResourceLambdaFunction, ID: envFnArn, Name: "fn-1"}
}

func TestLambdaEnvIsMergedIntoTheFunctionsVariables(t *testing.T) {
	var updated map[string]any
	srv := newFakeServer(t,
		onPath(http.MethodGet, "/2015-03-31/functions/"+envFnArn+"/configuration", http.StatusOK, mustJSON(t, map[string]any{
			"FunctionArn": envFnArn, "Environment": map[string]any{"Variables": map[string]string{"KEEP": "1"}},
		})),
		onPathDynamic(http.MethodPut, "/2015-03-31/functions/"+envFnArn+"/configuration", func(w http.ResponseWriter, _ *http.Request, body []byte) {
			require.NoError(t, json.Unmarshal(body, &updated))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		}),
	)
	p := testProvider(srv.URL)

	vars, err := p.ListEnvVars(context.Background(), testCred(), lambdaRef())
	require.NoError(t, err)
	require.Len(t, vars, 1)
	assert.Equal(t, "KEEP", vars[0].Key)

	require.NoError(t, p.UpsertEnvVars(context.Background(), testCred(), lambdaRef(), []domain.CloudEnvWrite{
		{Key: "SESSION_SECRET", Value: "s3cret", Sensitive: true},
	}))
	env := updated["Environment"].(map[string]any)["Variables"].(map[string]any)
	assert.Equal(t, "1", env["KEEP"])
	assert.Equal(t, "s3cret", env["SESSION_SECRET"])
}

func appRunnerRef() domain.CloudResourceRef {
	return domain.CloudResourceRef{Kind: domain.CloudResourceAppRunnerService, ID: "arn:aws:apprunner:us-east-1:111122223333:service/web/abc", Name: "web"}
}

func secretsRoutes(t *testing.T, created map[string]string, existing bool) []route {
	return []route{
		onTargetDynamic("secretsmanager.CreateSecret", func(w http.ResponseWriter, _ *http.Request, body []byte) {
			var in map[string]any
			require.NoError(t, json.Unmarshal(body, &in))
			if existing {
				w.Header().Set("Content-Type", "application/x-amz-json-1.1")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"__type":"ResourceExistsException","message":"exists"}`))
				return
			}
			created[in["Name"].(string)] = in["SecretString"].(string)
			writeJSON(t, w, map[string]any{"ARN": "arn:aws:secretsmanager:us-east-1:1:secret:" + in["Name"].(string)})
		}),
		onTargetDynamic("secretsmanager.PutSecretValue", func(w http.ResponseWriter, _ *http.Request, body []byte) {
			var in map[string]any
			require.NoError(t, json.Unmarshal(body, &in))
			created[in["SecretId"].(string)] = in["SecretString"].(string)
			writeJSON(t, w, map[string]any{"ARN": "arn:aws:secretsmanager:us-east-1:1:secret:" + in["SecretId"].(string) + "-put"})
		}),
	}
}

func TestAppRunnerSecretsBecomeSecretsManagerReferences(t *testing.T) {
	created := map[string]string{}
	var updated map[string]any
	routes := append(secretsRoutes(t, created, false),
		onTarget("AppRunner.DescribeService", http.StatusOK, mustJSON(t, map[string]any{
			"Service": map[string]any{
				"ServiceArn": appRunnerRef().ID,
				"SourceConfiguration": map[string]any{
					"AutoDeploymentsEnabled": true,
					"ImageRepository": map[string]any{
						"ImageIdentifier":     "123.dkr.ecr.us-east-1.amazonaws.com/web:latest",
						"ImageRepositoryType": "ECR",
						"ImageConfiguration":  map[string]any{"Port": "8080", "RuntimeEnvironmentVariables": map[string]string{"KEEP": "1", "GITHUB_TOKEN": "was-plain"}},
					},
				},
			},
		})),
		onTargetDynamic("AppRunner.UpdateService", func(w http.ResponseWriter, _ *http.Request, body []byte) {
			require.NoError(t, json.Unmarshal(body, &updated))
			writeJSON(t, w, map[string]any{"OperationId": "op-1", "Service": map[string]any{"ServiceArn": appRunnerRef().ID}})
		}),
	)
	p := testProvider(newFakeServer(t, routes...).URL)

	require.NoError(t, p.UpsertEnvVars(context.Background(), testCred(), appRunnerRef(), []domain.CloudEnvWrite{
		{Key: "GITHUB_TOKEN", Value: "ghp_abc", Sensitive: true},
		{Key: "GITHUB_REPO", Value: "o/r"},
	}))

	assert.Equal(t, "ghp_abc", created["tasktrooper/web/GITHUB_TOKEN"])
	raw, _ := json.Marshal(updated)
	assert.NotContains(t, string(raw), "ghp_abc", "the service only ever holds the reference")
	ic := updated["SourceConfiguration"].(map[string]any)["ImageRepository"].(map[string]any)["ImageConfiguration"].(map[string]any)
	plain := ic["RuntimeEnvironmentVariables"].(map[string]any)
	assert.Equal(t, "1", plain["KEEP"])
	assert.Equal(t, "o/r", plain["GITHUB_REPO"])
	assert.NotContains(t, plain, "GITHUB_TOKEN", "a variable turned secret leaves the plain map")
	secrets := ic["RuntimeEnvironmentSecrets"].(map[string]any)
	assert.Equal(t, "arn:aws:secretsmanager:us-east-1:1:secret:tasktrooper/web/GITHUB_TOKEN", secrets["GITHUB_TOKEN"])
	assert.Equal(t, true, updated["SourceConfiguration"].(map[string]any)["AutoDeploymentsEnabled"])
}

func TestAppRunnerConfiguredFromTheRepositoryIsRefused(t *testing.T) {
	srv := newFakeServer(t, onTarget("AppRunner.DescribeService", http.StatusOK, mustJSON(t, map[string]any{
		"Service": map[string]any{"SourceConfiguration": map[string]any{
			"CodeRepository": map[string]any{"RepositoryUrl": "https://github.com/o/r", "CodeConfiguration": map[string]any{"ConfigurationSource": "REPOSITORY"}},
		}},
	})))

	err := testProvider(srv.URL).UpsertEnvVars(context.Background(), testCred(), appRunnerRef(), []domain.CloudEnvWrite{{Key: "A", Value: "b"}})

	assert.ErrorIs(t, err, errAppRunnerRepoConfig)
}

func ecsRef() domain.CloudResourceRef {
	return domain.CloudResourceRef{
		Kind: domain.CloudResourceECSService, ID: "arn:aws:ecs:us-east-1:1:service/prod/api", Name: "api",
		Extra: map[string]string{extraClusterARN: "arn:aws:ecs:us-east-1:1:cluster/prod"},
	}
}

func TestECSWriteRegistersARevisionAndPointsTheServiceAtIt(t *testing.T) {
	created := map[string]string{}
	var registered, updated map[string]any
	routes := append(secretsRoutes(t, created, true),
		onTarget("AmazonEC2ContainerServiceV20141113.DescribeServices", http.StatusOK, mustJSON(t, map[string]any{
			"services": []map[string]any{{"serviceArn": ecsRef().ID, "clusterArn": ecsRef().Extra[extraClusterARN], "taskDefinition": "arn:aws:ecs:us-east-1:1:task-definition/api:7"}},
		})),
		onTarget("AmazonEC2ContainerServiceV20141113.DescribeTaskDefinition", http.StatusOK, mustJSON(t, map[string]any{
			"taskDefinition": map[string]any{
				"family": "api", "executionRoleArn": "arn:aws:iam::1:role/exec", "networkMode": "awsvpc",
				"requiresCompatibilities": []string{"FARGATE"}, "cpu": "256", "memory": "512",
				"containerDefinitions": []map[string]any{
					{"name": "log-router", "image": "fluent", "essential": false},
					{"name": "api", "image": "api:7", "essential": true, "portMappings": []map[string]any{{"containerPort": 8080}},
						"environment": []map[string]any{{"name": "KEEP", "value": "1"}, {"name": "SESSION_SECRET", "value": "old-plain"}}},
				},
			},
			"tags": []map[string]any{{"key": "team", "value": "web"}},
		})),
		onTargetDynamic("AmazonEC2ContainerServiceV20141113.RegisterTaskDefinition", func(w http.ResponseWriter, _ *http.Request, body []byte) {
			require.NoError(t, json.Unmarshal(body, &registered))
			writeJSON(t, w, map[string]any{"taskDefinition": map[string]any{"taskDefinitionArn": "arn:aws:ecs:us-east-1:1:task-definition/api:8"}})
		}),
		onTargetDynamic("AmazonEC2ContainerServiceV20141113.UpdateService", func(w http.ResponseWriter, _ *http.Request, body []byte) {
			require.NoError(t, json.Unmarshal(body, &updated))
			writeJSON(t, w, map[string]any{"service": map[string]any{}})
		}),
	)
	p := testProvider(newFakeServer(t, routes...).URL)

	vars, err := p.ListEnvVars(context.Background(), testCred(), ecsRef())
	require.NoError(t, err)
	assert.Len(t, vars, 2, "the serving container's variables, not the sidecar's")

	require.NoError(t, p.UpsertEnvVars(context.Background(), testCred(), ecsRef(), []domain.CloudEnvWrite{
		{Key: "SESSION_SECRET", Value: "s3cret", Sensitive: true},
	}))

	assert.Equal(t, "s3cret", created["tasktrooper/api/SESSION_SECRET"], "an existing secret gets a new value")
	raw, _ := json.Marshal(registered)
	assert.NotContains(t, string(raw), "s3cret")
	assert.NotContains(t, string(raw), "old-plain", "the plain copy is removed once it is a secret")
	assert.Contains(t, string(raw), `"valueFrom":"arn:aws:secretsmanager:us-east-1:1:secret:tasktrooper/api/SESSION_SECRET-put"`)
	assert.Contains(t, string(raw), `"KEEP"`)
	assert.Contains(t, string(raw), `"executionRoleArn":"arn:aws:iam::1:role/exec"`)
	assert.Contains(t, string(raw), `"team"`, "tags are carried to the new revision")
	assert.Equal(t, "arn:aws:ecs:us-east-1:1:task-definition/api:8", updated["taskDefinition"])
}

func TestEnvCapabilitiesPerKind(t *testing.T) {
	p := NewProvider()
	ecsCaps, err := p.EnvCapabilities(ecsRef())
	require.NoError(t, err)
	assert.True(t, ecsCaps.OverwrittenOnDeploy)
	lambdaCaps, err := p.EnvCapabilities(lambdaRef())
	require.NoError(t, err)
	assert.False(t, lambdaCaps.OverwrittenOnDeploy)
	assert.True(t, lambdaCaps.WritesRollOut)

	_, err = p.EnvCapabilities(domain.CloudResourceRef{Kind: domain.CloudResourceGKEWorkload})
	assert.ErrorIs(t, err, port.ErrUnsupported)
}
