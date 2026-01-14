package aws

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

func GetAwsConfig() (aws.Config, error) {
	ctx := context.TODO()

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return cfg, errors.New("Error loading the AWS Config, Please check if the AWS Profiles are present..")
	}

	if cfg.Region == "" {
		return cfg, errors.New("AWS region not configured. Please set AWS_REGION environment variable or configure region in AWS profile")
	}

	return cfg, nil
}

func GetAccountID(cfg aws.Config) (string, error) {
	stsClient := sts.NewFromConfig(cfg)

	result, err := stsClient.GetCallerIdentity(context.TODO(), &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("failed to get account ID: %w", err)
	}

	if result.Account == nil {
		return "", errors.New("account ID not found in caller identity")
	}

	return *result.Account, nil
}

// AssumeRole assumes an IAM role and returns a new AWS config with the assumed role credentials
func AssumeRole(cfg aws.Config, roleArn string, sessionName string) (aws.Config, error) {
	stsClient := sts.NewFromConfig(cfg)

	result, err := stsClient.AssumeRole(context.TODO(), &sts.AssumeRoleInput{
		RoleArn:         aws.String(roleArn),
		RoleSessionName: aws.String(sessionName),
	})
	if err != nil {
		return aws.Config{}, fmt.Errorf("failed to assume role %s: %w", roleArn, err)
	}

	if result.Credentials == nil {
		return aws.Config{}, errors.New("no credentials returned from AssumeRole")
	}

	// Create a new config with the assumed role credentials
	assumedCfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithCredentialsProvider(aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
			return aws.Credentials{
				AccessKeyID:     *result.Credentials.AccessKeyId,
				SecretAccessKey: *result.Credentials.SecretAccessKey,
				SessionToken:    *result.Credentials.SessionToken,
				Source:          "AssumeRole",
			}, nil
		})),
		config.WithRegion(cfg.Region),
	)
	if err != nil {
		return aws.Config{}, fmt.Errorf("failed to create config with assumed role credentials: %w", err)
	}

	return assumedCfg, nil
}
