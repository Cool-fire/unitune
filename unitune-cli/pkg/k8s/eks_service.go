package k8s

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/aws-iam-authenticator/pkg/token"
)

// ClusterInfo holds EKS cluster information
type ClusterInfo struct {
	Endpoint string
	CAData   []byte
}

// DescribeCluster retrieves information about an EKS cluster
func DescribeCluster(ctx context.Context, cfg aws.Config, clusterName string) (*ClusterInfo, error) {
	eksClient := eks.NewFromConfig(cfg)

	describeOutput, err := eksClient.DescribeCluster(ctx, &eks.DescribeClusterInput{
		Name: aws.String(clusterName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe EKS cluster %s: %w", clusterName, err)
	}

	cluster := describeOutput.Cluster
	if cluster == nil {
		return nil, fmt.Errorf("cluster %s not found", clusterName)
	}

	if cluster.Endpoint == nil {
		return nil, fmt.Errorf("cluster %s has no endpoint", clusterName)
	}

	if cluster.CertificateAuthority == nil || cluster.CertificateAuthority.Data == nil {
		return nil, fmt.Errorf("cluster %s has no certificate authority data", clusterName)
	}

	// Decode the CA certificate
	caData, err := base64.StdEncoding.DecodeString(*cluster.CertificateAuthority.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode cluster CA: %w", err)
	}

	return &ClusterInfo{
		Endpoint: *cluster.Endpoint,
		CAData:   caData,
	}, nil
}

// GenerateEKSToken generates an authentication token for EKS cluster access
// If roleArn is provided, the token will be generated using the assumed role
func GenerateEKSToken(clusterName string, roleArn string) (string, error) {
	gen, err := token.NewGenerator(true, false)
	if err != nil {
		return "", fmt.Errorf("failed to create token generator: %w", err)
	}

	tokenOpts := &token.GetTokenOptions{
		ClusterID: clusterName,
	}

	// If a role ARN is provided, assume that role for authentication
	if roleArn != "" {
		tokenOpts.AssumeRoleARN = roleArn
		tokenOpts.SessionName = "unitune-cli"
	}

	tok, err := gen.GetWithOptions(tokenOpts)
	if err != nil {
		return "", fmt.Errorf("failed to get EKS token: %w", err)
	}

	return tok.Token, nil
}

// CreateEKSRestConfig creates a Kubernetes REST config for connecting to an EKS cluster
func CreateEKSRestConfig(clusterInfo *ClusterInfo, bearerToken string) *rest.Config {
	return &rest.Config{
		Host:        clusterInfo.Endpoint,
		BearerToken: bearerToken,
		TLSClientConfig: rest.TLSClientConfig{
			CAData: clusterInfo.CAData,
		},
	}
}

// NewK8sClientForEKS creates a K8s client that connects to an EKS cluster
// If roleArn is provided, the client will assume that role for authentication
func NewK8sClientForEKS(cfg aws.Config, clusterName string, roleArn string, namespace string) (*K8sClient, error) {
	ctx := context.TODO()

	// Get cluster information (endpoint and CA certificate)
	clusterInfo, err := DescribeCluster(ctx, cfg, clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster info: %w", err)
	}

	// Generate EKS authentication token
	bearerToken, err := GenerateEKSToken(clusterName, roleArn)
	if err != nil {
		return nil, fmt.Errorf("failed to generate EKS token: %w", err)
	}

	// Create REST config for Kubernetes client
	restConfig := CreateEKSRestConfig(clusterInfo, bearerToken)

	return NewK8sClient(restConfig, namespace)
}

// NewBuildJobForEKS creates a BuildJob that connects to an EKS cluster
// If roleArn is provided, the client will assume that role for authentication
func NewBuildJobForEKS(cfg aws.Config, clusterName string, roleArn string, namespace string, buildJobConfig BuildJobConfig, jobSpec *batchv1.Job) (*BuildJob, error) {
	k8sClient, err := NewK8sClientForEKS(cfg, clusterName, roleArn, namespace)
	if err != nil {
		return nil, err
	}
	return NewBuildJob(buildJobConfig, k8sClient, jobSpec), nil
}
