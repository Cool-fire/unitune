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
