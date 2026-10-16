package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// SigningCredentials is what Provider signs its calls with, for an adapter
// outside this package that signs its own request to an AWS API on a
// connected account (the CloudWatch data source); region is the account's.
func SigningCredentials(ctx context.Context, cred domain.CloudCredential) (aws.Credentials, string, error) {
	cfg, err := (&Provider{}).config(cred)
	if err != nil {
		return aws.Credentials{}, "", err
	}
	creds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return aws.Credentials{}, "", err
	}
	return creds, cfg.Region, nil
}
