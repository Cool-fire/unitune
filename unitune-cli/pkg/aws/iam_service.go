package aws

import (
	"context"
	"errors"
	"fmt"
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



