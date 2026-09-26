package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	apprunnertypes "github.com/aws/aws-sdk-go-v2/service/apprunner/types"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

func TestVerify(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		srv := newFakeServer(t, onAction("GetCallerIdentity", http.StatusOK,
			callerIdentityXML("111122223333", "arn:aws:iam::111122223333:user/test")))
		p := testProvider(srv.URL)

		meta, err := p.Verify(context.Background(), testCred())

		require.NoError(t, err)
		assert.Equal(t, "111122223333", meta["account_id"])
		assert.Equal(t, "arn:aws:iam::111122223333:user/test", meta["arn"])
		assert.Equal(t, "us-east-1", meta["region"])
	})

	for _, code := range []string{"InvalidClientTokenId", "SignatureDoesNotMatch", "AccessDenied", "ExpiredToken"} {
		t.Run(code, func(t *testing.T) {
			srv := newFakeServer(t, onAction("GetCallerIdentity", http.StatusForbidden, stsErrorXML(code, "denied")))
			p := testProvider(srv.URL)

			_, err := p.Verify(context.Background(), testCred())

			require.Error(t, err)
			assert.ErrorIs(t, err, port.ErrCloudAuth)
		})
	}
}

func noClustersRoute(t *testing.T) route {
	return onTarget("AmazonEC2ContainerServiceV20141113.ListClusters", http.StatusOK,
		mustJSON(t, map[string]any{"clusterArns": []string{}}))
}

func noLambdaFunctionsRoute() route {
	return onPath(http.MethodGet, "/2015-03-31/functions", http.StatusOK, `{"Functions":[]}`)
}

func noAppRunnerServicesRoute(t *testing.T) route {
	return onTarget("AppRunner.ListServices", http.StatusOK,
		mustJSON(t, map[string]any{"ServiceSummaryList": []any{}}))
}

func TestListResources_ECS_ClustersAndBatching(t *testing.T) {
	const clusterA = "arn:aws:ecs:us-east-1:111122223333:cluster/cluster-a"
	const clusterB = "arn:aws:ecs:us-east-1:111122223333:cluster/cluster-b"

	servicesA := make([]string, 12)
	for i := range servicesA {
		servicesA[i] = fmt.Sprintf("arn:aws:ecs:us-east-1:111122223333:service/cluster-a/svc-%02d", i)
	}
	servicesB := []string{"arn:aws:ecs:us-east-1:111122223333:service/cluster-b/svc-0"}

	var mu sync.Mutex
	var batches [][]string

	listClusters := onTarget("AmazonEC2ContainerServiceV20141113.ListClusters", http.StatusOK,
		mustJSON(t, map[string]any{"clusterArns": []string{clusterA, clusterB}}))
	listServicesA := onTargetContains("AmazonEC2ContainerServiceV20141113.ListServices", clusterA, http.StatusOK,
		mustJSON(t, map[string]any{"serviceArns": servicesA}))
	listServicesB := onTargetContains("AmazonEC2ContainerServiceV20141113.ListServices", clusterB, http.StatusOK,
		mustJSON(t, map[string]any{"serviceArns": servicesB}))

	describeServices := onTargetDynamic("AmazonEC2ContainerServiceV20141113.DescribeServices",
		func(w http.ResponseWriter, _ *http.Request, body []byte) {
			var req struct {
				Cluster  string   `json:"cluster"`
				Services []string `json:"services"`
			}
			_ = json.Unmarshal(body, &req)

			mu.Lock()
			batches = append(batches, append([]string(nil), req.Services...))
			mu.Unlock()

			services := make([]map[string]any, 0, len(req.Services))
			for _, arn := range req.Services {
				services = append(services, map[string]any{
					"serviceArn":     arn,
					"serviceName":    shortARNName(arn),
					"clusterArn":     req.Cluster,
					"status":         "ACTIVE",
					"desiredCount":   1,
					"runningCount":   1,
					"launchType":     "FARGATE",
					"taskDefinition": "arn:aws:ecs:us-east-1:111122223333:task-definition/app:3",
					"deployments": []map[string]any{
						{"id": "ecs-svc/1", "status": "PRIMARY", "rolloutState": "COMPLETED"},
					},
				})
			}
			writeJSON(t, w, map[string]any{"services": services, "failures": []any{}})
		})

	srv := newFakeServer(t, listClusters, listServicesA, listServicesB, describeServices,
		noLambdaFunctionsRoute(), noAppRunnerServicesRoute(t))
	p := testProvider(srv.URL)

	resources, err := p.ListResources(context.Background(), testCred())

	require.NoError(t, err)
	assert.Len(t, resources, 13)

	mu.Lock()
	defer mu.Unlock()
	// Distinct batches, not requests: the SDK retries a request that a slow
	// runner answered late, and a retry is the same batch sent twice.
	distinct := map[string]bool{}
	for _, b := range batches {
		assert.LessOrEqual(t, len(b), ecsDescribeBatchSize)
		distinct[strings.Join(b, ",")] = true
	}
	require.Len(t, distinct, 3, "cluster-a needs two DescribeServices batches (10+2), cluster-b needs one")
}

func TestListResources_Lambda_Pagination(t *testing.T) {
	var calls int32
	listFunctions := onPathDynamic(http.MethodGet, "/2015-03-31/functions",
		func(w http.ResponseWriter, r *http.Request, _ []byte) {
			n := atomic.AddInt32(&calls, 1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			if n == 1 {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"Functions": []map[string]any{
						{"FunctionArn": "arn:aws:lambda:us-east-1:111122223333:function:fn-1", "FunctionName": "fn-1", "Runtime": "nodejs20.x"},
					},
					"NextMarker": "page-2",
				})
				return
			}
			assert.Equal(t, "page-2", r.URL.Query().Get("Marker"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Functions": []map[string]any{
					{"FunctionArn": "arn:aws:lambda:us-east-1:111122223333:function:fn-2", "FunctionName": "fn-2", "Runtime": "python3.12"},
				},
			})
		})

	srv := newFakeServer(t, listFunctions, noClustersRoute(t), noAppRunnerServicesRoute(t))
	p := testProvider(srv.URL)

	resources, err := p.ListResources(context.Background(), testCred())

	require.NoError(t, err)
	require.Len(t, resources, 2)
	assert.Equal(t, "fn-1", resources[0].Ref.Name)
	assert.Equal(t, "fn-2", resources[1].Ref.Name)
	assert.EqualValues(t, 2, atomic.LoadInt32(&calls))
}

func TestListResources_AppRunner_Listing(t *testing.T) {
	listServices := onTarget("AppRunner.ListServices", http.StatusOK, mustJSON(t, map[string]any{
		"ServiceSummaryList": []map[string]any{
			{
				"ServiceArn":  "arn:aws:apprunner:us-east-1:111122223333:service/svc-a/abc123",
				"ServiceName": "svc-a",
				"ServiceId":   "abc123",
				"ServiceUrl":  "svc-a.awsapprunner.com",
				"Status":      "RUNNING",
			},
		},
	}))

	srv := newFakeServer(t, listServices, noClustersRoute(t), noLambdaFunctionsRoute())
	p := testProvider(srv.URL)

	resources, err := p.ListResources(context.Background(), testCred())

	require.NoError(t, err)
	require.Len(t, resources, 1)
	assert.Equal(t, domain.CloudResourceAppRunnerService, resources[0].Ref.Kind)
	assert.Equal(t, "svc-a", resources[0].Ref.Name)
	assert.Equal(t, "abc123", resources[0].Ref.Extra[extraServiceID])
	assert.Equal(t, "https://svc-a.awsapprunner.com", resources[0].URL)
}

func TestECSServiceStatus(t *testing.T) {
	tests := []struct {
		name string
		svc  ecstypes.Service
		want domain.CloudResourceStatus
	}{
		{
			name: "healthy",
			svc: ecstypes.Service{
				Status: aws.String("ACTIVE"), RunningCount: 2, DesiredCount: 2,
				Deployments: []ecstypes.Deployment{
					{Status: aws.String("PRIMARY"), RolloutState: ecstypes.DeploymentRolloutStateCompleted},
				},
			},
			want: domain.CloudStatusHealthy,
		},
		{
			name: "deploying",
			svc: ecstypes.Service{
				Status: aws.String("ACTIVE"), RunningCount: 1, DesiredCount: 2,
				Deployments: []ecstypes.Deployment{
					{Status: aws.String("PRIMARY"), RolloutState: ecstypes.DeploymentRolloutStateInProgress},
				},
			},
			want: domain.CloudStatusDeploying,
		},
		{
			name: "failed rollout",
			svc: ecstypes.Service{
				Status: aws.String("ACTIVE"), RunningCount: 0, DesiredCount: 2,
				Deployments: []ecstypes.Deployment{
					{Status: aws.String("PRIMARY"), RolloutState: ecstypes.DeploymentRolloutStateFailed},
				},
			},
			want: domain.CloudStatusFailed,
		},
		{
			name: "no running tasks with none desired is not failed",
			svc: ecstypes.Service{
				Status: aws.String("ACTIVE"), RunningCount: 0, DesiredCount: 0,
			},
			want: domain.CloudStatusDegraded,
		},
		{
			name: "no running tasks while desired is failed",
			svc: ecstypes.Service{
				Status: aws.String("ACTIVE"), RunningCount: 0, DesiredCount: 3,
			},
			want: domain.CloudStatusFailed,
		},
		{
			name: "completed rollout but counts mismatch is degraded",
			svc: ecstypes.Service{
				Status: aws.String("ACTIVE"), RunningCount: 1, DesiredCount: 2,
				Deployments: []ecstypes.Deployment{
					{Status: aws.String("PRIMARY"), RolloutState: ecstypes.DeploymentRolloutStateCompleted},
				},
			},
			want: domain.CloudStatusDegraded,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := ecsServiceStatus(tt.svc)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLambdaFunctionStatus(t *testing.T) {
	tests := []struct {
		name string
		cfg  lambdatypes.FunctionConfiguration
		want domain.CloudResourceStatus
	}{
		{
			name: "healthy",
			cfg:  lambdatypes.FunctionConfiguration{State: lambdatypes.StateActive, LastUpdateStatus: lambdatypes.LastUpdateStatusSuccessful},
			want: domain.CloudStatusHealthy,
		},
		{
			name: "state failed",
			cfg:  lambdatypes.FunctionConfiguration{State: lambdatypes.StateFailed},
			want: domain.CloudStatusFailed,
		},
		{
			name: "state pending",
			cfg:  lambdatypes.FunctionConfiguration{State: lambdatypes.StatePending},
			want: domain.CloudStatusDeploying,
		},
		{
			name: "last update in progress",
			cfg:  lambdatypes.FunctionConfiguration{State: lambdatypes.StateActive, LastUpdateStatus: lambdatypes.LastUpdateStatusInProgress},
			want: domain.CloudStatusDeploying,
		},
		{
			name: "last update failed",
			cfg:  lambdatypes.FunctionConfiguration{State: lambdatypes.StateActive, LastUpdateStatus: lambdatypes.LastUpdateStatusFailed},
			want: domain.CloudStatusFailed,
		},
		{
			name: "inactive is degraded",
			cfg:  lambdatypes.FunctionConfiguration{State: lambdatypes.StateInactive},
			want: domain.CloudStatusDegraded,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := lambdaFunctionStatus(tt.cfg)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAppRunnerStatus(t *testing.T) {
	tests := []struct {
		name string
		in   apprunnertypes.ServiceStatus
		want domain.CloudResourceStatus
	}{
		{"running", apprunnertypes.ServiceStatusRunning, domain.CloudStatusHealthy},
		{"operation in progress", apprunnertypes.ServiceStatusOperationInProgress, domain.CloudStatusDeploying},
		{"create failed", apprunnertypes.ServiceStatusCreateFailed, domain.CloudStatusFailed},
		{"delete failed", apprunnertypes.ServiceStatusDeleteFailed, domain.CloudStatusFailed},
		{"paused", apprunnertypes.ServiceStatusPaused, domain.CloudStatusDegraded},
		{"deleted is unmapped", apprunnertypes.ServiceStatusDeleted, domain.CloudStatusUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := appRunnerStatus(tt.in)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveLogTarget_Lambda(t *testing.T) {
	p := testProvider("")

	t.Run("explicit log group from listing", func(t *testing.T) {
		ref := domain.CloudResourceRef{
			Kind: domain.CloudResourceLambdaFunction, Name: "fn-1",
			Extra: map[string]string{extraLogGroup: "/custom/group"},
		}
		target, err := p.resolveLogTarget(context.Background(), aws.Config{}, ref)
		require.NoError(t, err)
		assert.Equal(t, "/custom/group", target.group)
	})

	t.Run("default log group derived from function name", func(t *testing.T) {
		ref := domain.CloudResourceRef{Kind: domain.CloudResourceLambdaFunction, Name: "fn-1"}
		target, err := p.resolveLogTarget(context.Background(), aws.Config{}, ref)
		require.NoError(t, err)
		assert.Equal(t, "/aws/lambda/fn-1", target.group)
	})
}

func TestResolveLogTarget_AppRunner(t *testing.T) {
	p := testProvider("")
	ref := domain.CloudResourceRef{
		Kind: domain.CloudResourceAppRunnerService, Name: "svc-a",
		Extra: map[string]string{extraServiceID: "abc123"},
	}

	target, err := p.resolveLogTarget(context.Background(), aws.Config{}, ref)

	require.NoError(t, err)
	assert.Equal(t, "/aws/apprunner/svc-a/abc123/application", target.group)
}

func TestResolveLogTarget_ECS(t *testing.T) {
	const taskDefArn = "arn:aws:ecs:us-east-1:111122223333:task-definition/app:7"
	describeTaskDef := onTarget("AmazonEC2ContainerServiceV20141113.DescribeTaskDefinition", http.StatusOK, mustJSON(t, map[string]any{
		"taskDefinition": map[string]any{
			"taskDefinitionArn": taskDefArn,
			"family":            "app",
			"revision":          7,
			"containerDefinitions": []map[string]any{
				{
					"name": "sidecar",
				},
				{
					"name": "app",
					"logConfiguration": map[string]any{
						"logDriver": "awslogs",
						"options": map[string]any{
							"awslogs-group":         "/ecs/app",
							"awslogs-stream-prefix": "app",
							"awslogs-region":        "us-east-1",
						},
					},
				},
			},
		},
	}))
	srv := newFakeServer(t, describeTaskDef)
	p := testProvider(srv.URL)
	cfg, err := p.config(testCred())
	require.NoError(t, err)
	ref := domain.CloudResourceRef{
		Kind: domain.CloudResourceECSService, Name: "svc-a",
		Extra: map[string]string{extraTaskDefinition: taskDefArn},
	}

	target, err := p.resolveLogTarget(context.Background(), cfg, ref)

	require.NoError(t, err)
	assert.Equal(t, "/ecs/app", target.group)
	assert.Equal(t, "app", target.streamPrefix)
}

func TestBuildFilterPattern(t *testing.T) {
	tests := []struct {
		name             string
		text             string
		minSeverity      domain.LogSeverity
		wantPattern      string
		wantClientFilter bool
	}{
		{name: "no filters", wantPattern: ""},
		{name: "text only", text: `boom "quote"`, wantPattern: `"boom \"quote\""`},
		{name: "error severity only", minSeverity: domain.LogError, wantPattern: errorSeverityGroup},
		{name: "warning severity only", minSeverity: domain.LogWarning, wantPattern: errorSeverityGroup + " " + warnSeverityGroup},
		{name: "text and severity combine client side", text: "timeout", minSeverity: domain.LogError, wantPattern: `"timeout"`, wantClientFilter: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern, clientFilter := buildFilterPattern(tt.text, tt.minSeverity)
			assert.Equal(t, tt.wantPattern, pattern)
			assert.Equal(t, tt.wantClientFilter, clientFilter)
		})
	}
}

func TestInferSeverity(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    domain.LogSeverity
	}{
		{"json level field", `{"level":"error","msg":"boom"}`, domain.LogError},
		{"json severity field", `{"severity":"WARNING","msg":"careful"}`, domain.LogWarning},
		{"leading token error", "ERROR something broke", domain.LogError},
		{"leading token bracketed warn", "[WARN] disk almost full", domain.LogWarning},
		{"leading token panic", "panic: runtime error: index out of range", domain.LogCritical},
		{"leading token info", "INFO listening on :8080", domain.LogInfo},
		{"no recognizable token defaults to info", "just a plain message", domain.LogInfo},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, inferSeverity(tt.message))
		})
	}
}

func TestLogs_NewestFirstOrdering(t *testing.T) {
	filterLogEvents := onTarget("Logs_20140328.FilterLogEvents", http.StatusOK, mustJSON(t, map[string]any{
		"events": []map[string]any{
			{"timestamp": 1000, "message": "first", "logStreamName": "stream-a"},
			{"timestamp": 3000, "message": "third", "logStreamName": "stream-a"},
			{"timestamp": 2000, "message": "second", "logStreamName": "stream-a"},
		},
	}))
	srv := newFakeServer(t, filterLogEvents)
	p := testProvider(srv.URL)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceLambdaFunction, Name: "fn-1"}

	page, err := p.Logs(context.Background(), testCred(), ref, domain.RuntimeLogQuery{})

	require.NoError(t, err)
	require.Len(t, page.Entries, 3)
	assert.Equal(t, "third", page.Entries[0].Message)
	assert.Equal(t, "second", page.Entries[1].Message)
	assert.Equal(t, "first", page.Entries[2].Message)
}

func TestLogs_ResourceNotFound_EmptyPage(t *testing.T) {
	filterLogEvents := onTarget("Logs_20140328.FilterLogEvents", http.StatusBadRequest,
		`{"__type":"ResourceNotFoundException","message":"no such log group"}`)
	srv := newFakeServer(t, filterLogEvents)
	p := testProvider(srv.URL)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceLambdaFunction, Name: "fn-missing"}

	page, err := p.Logs(context.Background(), testCred(), ref, domain.RuntimeLogQuery{})

	require.NoError(t, err)
	assert.Empty(t, page.Entries)
}

func TestResource_ECS_Detail(t *testing.T) {
	const clusterArn = "arn:aws:ecs:us-east-1:111122223333:cluster/cluster-a"
	const serviceArn = "arn:aws:ecs:us-east-1:111122223333:service/cluster-a/svc-a"

	describeServices := onTarget("AmazonEC2ContainerServiceV20141113.DescribeServices", http.StatusOK, mustJSON(t, map[string]any{
		"services": []map[string]any{
			{
				"serviceArn":     serviceArn,
				"serviceName":    "svc-a",
				"clusterArn":     clusterArn,
				"status":         "ACTIVE",
				"desiredCount":   2,
				"runningCount":   2,
				"launchType":     "FARGATE",
				"taskDefinition": "arn:aws:ecs:us-east-1:111122223333:task-definition/app:5",
				"deployments": []map[string]any{
					{"id": "ecs-svc/1", "status": "PRIMARY", "rolloutState": "COMPLETED"},
				},
				"loadBalancers": []map[string]any{
					{"targetGroupArn": "arn:aws:elasticloadbalancing:us-east-1:111122223333:targetgroup/app/abc123"},
				},
			},
		},
		"failures": []any{},
	}))
	srv := newFakeServer(t, describeServices)
	p := testProvider(srv.URL)
	cred := testCred()
	ref := domain.CloudResourceRef{
		Kind: domain.CloudResourceECSService, ID: serviceArn, Name: "svc-a", Region: "us-east-1",
		Extra: map[string]string{extraClusterARN: clusterArn},
	}

	detail, err := p.Resource(context.Background(), cred, ref)

	require.NoError(t, err)
	assert.Equal(t, cred.AccountID, detail.AccountID)
	assert.Equal(t, domain.CloudAWS, detail.Provider)
	assert.Equal(t, domain.CloudStatusHealthy, detail.Status)
	assert.Equal(t, "app:5", detail.Revision)
	assert.Equal(t, "https://us-east-1.console.aws.amazon.com/ecs/v2/clusters/cluster-a/services/svc-a", detail.ConsoleURL)
	assert.Contains(t, detail.Facts, domain.KeyValue{Label: "Target group", Value: "arn:aws:elasticloadbalancing:us-east-1:111122223333:targetgroup/app/abc123"})
}

func TestResource_ECS_NotFound(t *testing.T) {
	describeServices := onTarget("AmazonEC2ContainerServiceV20141113.DescribeServices", http.StatusOK, mustJSON(t, map[string]any{
		"services": []any{},
		"failures": []map[string]any{
			{"arn": "arn:aws:ecs:us-east-1:111122223333:service/cluster-a/svc-gone", "reason": "MISSING"},
		},
	}))
	srv := newFakeServer(t, describeServices)
	p := testProvider(srv.URL)
	ref := domain.CloudResourceRef{
		Kind: domain.CloudResourceECSService, ID: "svc-gone", Name: "svc-gone",
		Extra: map[string]string{extraClusterARN: "arn:aws:ecs:us-east-1:111122223333:cluster/cluster-a"},
	}

	_, err := p.Resource(context.Background(), testCred(), ref)

	require.Error(t, err)
	assert.ErrorIs(t, err, port.ErrNotFound)
}

func TestResource_Lambda_Detail(t *testing.T) {
	const fnArn = "arn:aws:lambda:us-east-1:111122223333:function:fn-1"
	getFunction := onPath(http.MethodGet, "/2015-03-31/functions/"+fnArn, http.StatusOK, mustJSON(t, map[string]any{
		"Configuration": map[string]any{
			"FunctionArn":  fnArn,
			"FunctionName": "fn-1",
			"Runtime":      "nodejs20.x",
			"MemorySize":   256,
			"Timeout":      30,
			"LastModified": "2026-01-10T00:00:00.000+0000",
			"Version":      "3",
			"State":        "Active",
		},
	}))
	getURLConfig := onPath(http.MethodGet, "/2021-10-31/functions/"+fnArn+"/url", http.StatusOK, mustJSON(t, map[string]any{
		"FunctionUrl": "https://abc123.lambda-url.us-east-1.on.aws/",
	}))
	srv := newFakeServer(t, getFunction, getURLConfig)
	p := testProvider(srv.URL)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceLambdaFunction, ID: fnArn, Name: "fn-1", Region: "us-east-1"}

	detail, err := p.Resource(context.Background(), testCred(), ref)

	require.NoError(t, err)
	assert.Equal(t, domain.CloudStatusHealthy, detail.Status)
	assert.Equal(t, "3", detail.Revision)
	assert.Equal(t, "https://abc123.lambda-url.us-east-1.on.aws/", detail.URL)
	assert.Contains(t, detail.Facts, domain.KeyValue{Label: "Runtime", Value: "nodejs20.x"})
	assert.Contains(t, detail.Facts, domain.KeyValue{Label: "Memory", Value: "256 MB"})
}

func TestResource_Lambda_NoFunctionURL(t *testing.T) {
	const fnArn = "arn:aws:lambda:us-east-1:111122223333:function:fn-1"
	getFunction := onPath(http.MethodGet, "/2015-03-31/functions/"+fnArn, http.StatusOK, mustJSON(t, map[string]any{
		"Configuration": map[string]any{
			"FunctionArn": fnArn, "FunctionName": "fn-1", "Runtime": "python3.12", "State": "Active",
		},
	}))
	getURLConfig := onPath(http.MethodGet, "/2021-10-31/functions/"+fnArn+"/url", http.StatusNotFound,
		`{"__type":"ResourceNotFoundException","Message":"no url config"}`)
	srv := newFakeServer(t, getFunction, getURLConfig)
	p := testProvider(srv.URL)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceLambdaFunction, ID: fnArn, Name: "fn-1"}

	detail, err := p.Resource(context.Background(), testCred(), ref)

	require.NoError(t, err)
	assert.Empty(t, detail.URL)
}

func TestResource_AppRunner_Detail(t *testing.T) {
	const serviceArn = "arn:aws:apprunner:us-east-1:111122223333:service/svc-a/abc123"
	describeService := onTarget("AppRunner.DescribeService", http.StatusOK, mustJSON(t, map[string]any{
		"Service": map[string]any{
			"ServiceArn":  serviceArn,
			"ServiceName": "svc-a",
			"ServiceId":   "abc123",
			"ServiceUrl":  "svc-a.awsapprunner.com",
			"Status":      "RUNNING",
		},
	}))
	srv := newFakeServer(t, describeService)
	p := testProvider(srv.URL)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceAppRunnerService, ID: serviceArn, Name: "svc-a"}

	detail, err := p.Resource(context.Background(), testCred(), ref)

	require.NoError(t, err)
	assert.Equal(t, domain.CloudStatusHealthy, detail.Status)
	assert.Equal(t, "https://svc-a.awsapprunner.com", detail.URL)
}

func TestDeployments_ECS(t *testing.T) {
	describeServices := onTarget("AmazonEC2ContainerServiceV20141113.DescribeServices", http.StatusOK, mustJSON(t, map[string]any{
		"services": []map[string]any{
			{
				"serviceArn": "arn:aws:ecs:us-east-1:111122223333:service/cluster-a/svc-a",
				"deployments": []map[string]any{
					{
						"id": "ecs-svc/2", "status": "PRIMARY", "rolloutState": "COMPLETED",
						"taskDefinition": "arn:aws:ecs:us-east-1:111122223333:task-definition/app:6",
						"createdAt":      1700000200, "updatedAt": 1700000250,
					},
					{
						"id": "ecs-svc/1", "status": "ACTIVE", "rolloutState": "COMPLETED",
						"taskDefinition": "arn:aws:ecs:us-east-1:111122223333:task-definition/app:5",
						"createdAt":      1700000100, "updatedAt": 1700000150,
					},
				},
			},
		},
		"failures": []any{},
	}))
	srv := newFakeServer(t, describeServices)
	p := testProvider(srv.URL)
	ref := domain.CloudResourceRef{
		Kind: domain.CloudResourceECSService, ID: "arn:aws:ecs:us-east-1:111122223333:service/cluster-a/svc-a",
		Extra: map[string]string{extraClusterARN: "arn:aws:ecs:us-east-1:111122223333:cluster/cluster-a"},
	}

	deployments, err := p.Deployments(context.Background(), testCred(), ref, "", 0)

	require.NoError(t, err)
	require.Len(t, deployments, 2)
	assert.Equal(t, "ecs-svc/2", deployments[0].ID, "newest deployment first")
	assert.Equal(t, "task definition app:6", deployments[0].CommitMessage)
	assert.Equal(t, domain.CloudDeployReady, deployments[0].Status)
}

func TestDeployments_Lambda(t *testing.T) {
	const fnArn = "arn:aws:lambda:us-east-1:111122223333:function:fn-1"
	listVersions := onPath(http.MethodGet, "/2015-03-31/functions/"+fnArn+"/versions", http.StatusOK, mustJSON(t, map[string]any{
		"Versions": []map[string]any{
			{"Version": "1", "LastModified": "2026-01-01T00:00:00.000+0000", "Description": "first"},
			{"Version": "2", "LastModified": "2026-01-10T00:00:00.000+0000", "Description": "second"},
		},
	}))
	srv := newFakeServer(t, listVersions)
	p := testProvider(srv.URL)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceLambdaFunction, ID: fnArn, Name: "fn-1"}

	deployments, err := p.Deployments(context.Background(), testCred(), ref, "", 0)

	require.NoError(t, err)
	require.Len(t, deployments, 2)
	assert.Equal(t, "2", deployments[0].ID, "newest version first")
	assert.Equal(t, "second", deployments[0].CommitMessage)
}

func TestDeployments_AppRunner(t *testing.T) {
	const serviceArn = "arn:aws:apprunner:us-east-1:111122223333:service/svc-a/abc123"
	listOperations := onTarget("AppRunner.ListOperations", http.StatusOK, mustJSON(t, map[string]any{
		"OperationSummaryList": []map[string]any{
			{"Id": "op-2", "Type": "UPDATE_SERVICE", "Status": "SUCCEEDED", "StartedAt": 1700000200, "EndedAt": 1700000260},
			{"Id": "op-1", "Type": "START_DEPLOYMENT", "Status": "SUCCEEDED", "StartedAt": 1700000100, "EndedAt": 1700000160},
		},
	}))
	srv := newFakeServer(t, listOperations)
	p := testProvider(srv.URL)
	ref := domain.CloudResourceRef{Kind: domain.CloudResourceAppRunnerService, ID: serviceArn, Name: "svc-a"}

	deployments, err := p.Deployments(context.Background(), testCred(), ref, "", 0)

	require.NoError(t, err)
	require.Len(t, deployments, 2)
	assert.Equal(t, "op-2", deployments[0].ID)
	assert.Equal(t, domain.CloudDeployReady, deployments[0].Status)
}
