package aws

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

//go:embed permissions/permissions.json
var permissionsJSON []byte

type Permission struct {
	Sid      string   `json:"Sid"`
	Effect   string   `json:"Effect"`
	Action   []string `json:"Action"`
	Resource string   `json:"Resource"`
}

type PermissionsConfig struct {
	Version   string       `json:"Version"`
	Statement []Permission `json:"Statement"`
}

func GetPolicySourceArn(cfg aws.Config) (string, error) {
	stsClient := sts.NewFromConfig(cfg)

	result, err := stsClient.GetCallerIdentity(context.TODO(), &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", err
	}

	callerArn := *result.Arn

	if strings.HasSuffix(callerArn, ":root") {
		return callerArn, nil
	}

	if strings.Contains(callerArn, ":user/") {
		return callerArn, nil
	}

	if strings.Contains(callerArn, ":assumed-role/") {
		parts := strings.Split(callerArn, "/")
		roleName := parts[1]
		arnParts := strings.Split(callerArn, ":")
		accountId := arnParts[4]
		return fmt.Sprintf("arn:aws:iam::%s:role/%s", accountId, roleName), nil
	}

	return "", errors.New("Unable to determine the caller ARN")
}

func HasSimulatePrincipalPolicyPermission(cfg aws.Config, sourceArn string) (bool, error) {
	iamClient := iam.NewFromConfig(cfg)

	_, err := iamClient.SimulatePrincipalPolicy(context.TODO(), &iam.SimulatePrincipalPolicyInput{
		PolicySourceArn: &sourceArn,
		ActionNames:     []string{"iam:SimulatePrincipalPolicy"},
		ResourceArns:    []string{"*"},
	})

	return err == nil, err
}

func CheckRequiredPermissions(cfg aws.Config) error {
	var permConfig PermissionsConfig
	if err := json.Unmarshal(permissionsJSON, &permConfig); err != nil {
		return fmt.Errorf("failed to parse permissions.json: %v", err)
	}

	sourceArn, err := GetPolicySourceArn(cfg)
	if err != nil {
		return fmt.Errorf("failed to get policy source ARN: %v", err)
	}

	iamClient := iam.NewFromConfig(cfg)

	for _, perm := range permConfig.Statement {
		_, err := iamClient.SimulatePrincipalPolicy(context.TODO(), &iam.SimulatePrincipalPolicyInput{
			PolicySourceArn: &sourceArn,
			ActionNames:     perm.Action,
			ResourceArns:    []string{perm.Resource},
		})
		if err != nil {
			return fmt.Errorf("missing permissions %v on %s: %v", perm.Action, perm.Resource, err)
		}
	}

	return nil
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
