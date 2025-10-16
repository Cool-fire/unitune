package aws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

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

func CheckRequiredPermissions(cfg aws.Config) error {
	data, err := os.ReadFile("permissions/permissions.json")
	if err != nil {
		return fmt.Errorf("failed to read permissions.json: %v", err)
	}

	var permConfig PermissionsConfig
	if err := json.Unmarshal(data, &permConfig); err != nil {
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
