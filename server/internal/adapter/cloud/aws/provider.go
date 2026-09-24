// Package aws implements port.CloudProvider against Amazon Web Services:
// ECS services, Lambda functions and App Runner services, read through the
// AWS SDK for Go v2. Credentials are never read from the environment or
// ~/.aws — every call builds its own aws.Config from the account's stored
// access key, secret key, optional session token and region.
package aws

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/apprunner"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	smithy "github.com/aws/smithy-go"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const (
	fieldAccessKeyID     = "access_key_id"
	fieldSecretAccessKey = "secret_access_key"
	fieldSessionToken    = "session_token"
	fieldRegion          = "region"
)

const (
	extraClusterARN     = "cluster_arn"
	extraClusterName    = "cluster_name"
	extraTaskDefinition = "task_definition"
	extraRuntime        = "runtime"
	extraLogGroup       = "log_group"
	extraServiceID      = "service_id"
)

const httpTimeout = 20 * time.Second

// Provider is stateless between calls: every method builds its own
// aws.Config and service clients from the credential it is given, so
// concurrent calls for different accounts never share a signer or a cached
// token.
type Provider struct {
	// endpointOverride points every service client at a fixed base URL; it
	// exists for tests, which run a fake AWS behind an httptest server.
	endpointOverride string
	now              func() time.Time
}

func NewProvider() *Provider {
	return &Provider{now: time.Now}
}

var _ port.CloudProvider = (*Provider)(nil)

func (p *Provider) Kind() domain.CloudProviderKind { return domain.CloudAWS }

func (p *Provider) config(cred domain.CloudCredential) (aws.Config, error) {
	accessKey := strings.TrimSpace(cred.Fields[fieldAccessKeyID])
	secretKey := cred.Fields[fieldSecretAccessKey]
	region := strings.TrimSpace(cred.Fields[fieldRegion])
	if accessKey == "" || secretKey == "" {
		return aws.Config{}, errors.New("aws: credential missing access_key_id or secret_access_key")
	}
	if region == "" {
		return aws.Config{}, errors.New("aws: credential missing region")
	}
	sessionToken := cred.Fields[fieldSessionToken]
	return aws.Config{
		Region: region,
		Credentials: aws.NewCredentialsCache(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, sessionToken),
		),
		HTTPClient:       &http.Client{Timeout: httpTimeout},
		RetryMaxAttempts: 2,
	}, nil
}

func (p *Provider) baseEndpoint() *string {
	if p.endpointOverride == "" {
		return nil
	}
	return aws.String(p.endpointOverride)
}

func (p *Provider) stsClient(cfg aws.Config) *sts.Client {
	return sts.NewFromConfig(cfg, func(o *sts.Options) { o.BaseEndpoint = p.baseEndpoint() })
}

func (p *Provider) ecsClient(cfg aws.Config) *ecs.Client {
	return ecs.NewFromConfig(cfg, func(o *ecs.Options) { o.BaseEndpoint = p.baseEndpoint() })
}

func (p *Provider) lambdaClient(cfg aws.Config) *lambda.Client {
	return lambda.NewFromConfig(cfg, func(o *lambda.Options) { o.BaseEndpoint = p.baseEndpoint() })
}

func (p *Provider) appRunnerClient(cfg aws.Config) *apprunner.Client {
	return apprunner.NewFromConfig(cfg, func(o *apprunner.Options) { o.BaseEndpoint = p.baseEndpoint() })
}

func (p *Provider) logsClient(cfg aws.Config) *cloudwatchlogs.Client {
	return cloudwatchlogs.NewFromConfig(cfg, func(o *cloudwatchlogs.Options) { o.BaseEndpoint = p.baseEndpoint() })
}

func (p *Provider) Verify(ctx context.Context, cred domain.CloudCredential) (map[string]string, error) {
	cfg, err := p.config(cred)
	if err != nil {
		return nil, err
	}
	out, err := p.stsClient(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, classify("sts get caller identity", err)
	}
	return map[string]string{
		"account_id": aws.ToString(out.Account),
		"arn":        aws.ToString(out.Arn),
		"region":     cred.Fields[fieldRegion],
	}, nil
}

func (p *Provider) Errors(ctx context.Context, cred domain.CloudCredential, ref domain.CloudResourceRef, since time.Time) ([]domain.RuntimeErrorGroup, error) {
	return nil, port.ErrUnsupported
}

// authErrorCodes are the codes STS and every signed AWS API return when the
// request itself is rejected (bad key, bad signature, expired or explicitly
// denied) — as opposed to a modeled, service-specific exception.
var authErrorCodes = map[string]bool{
	"InvalidClientTokenId":  true,
	"SignatureDoesNotMatch": true,
	"AccessDenied":          true,
	"ExpiredToken":          true,
}

func errorCode(err error) string {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode()
	}
	return ""
}

// classify wraps err with the sentinel the application layer branches on
// (ErrCloudAuth for a rejected credential, ErrNotFound for a resource that no
// longer exists) while keeping the underlying error reachable through
// errors.Unwrap for logging.
func classify(op string, err error) error {
	if err == nil {
		return nil
	}
	switch code := errorCode(err); {
	case authErrorCodes[code]:
		return fmt.Errorf("aws %s: %v: %w", op, err, port.ErrCloudAuth)
	case code == "ResourceNotFoundException":
		return fmt.Errorf("aws %s: %v: %w", op, err, port.ErrNotFound)
	default:
		return fmt.Errorf("aws %s: %w", op, err)
	}
}

func shortARNName(arn string) string {
	if i := strings.LastIndex(arn, "/"); i >= 0 {
		return arn[i+1:]
	}
	return arn
}

func httpsURL(host string) string {
	if host == "" {
		return ""
	}
	return "https://" + strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://")
}

func parseAWSTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{"2006-01-02T15:04:05.000-0700", time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
