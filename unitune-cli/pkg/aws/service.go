package aws

import (
	"context"
	"errors"


	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
)

func GetAwsConfig() (aws.Config, error) {
	ctx := context.TODO()

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return cfg, errors.New("Error loading the AWS Config, Please check if the AWS Profiles are present..")
	}

	return cfg, nil
}

