package deploy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Cool-fire/unitune/pkg/aws"
	"github.com/Cool-fire/unitune/pkg/k8s"
	awsclient "github.com/aws/aws-sdk-go-v2/aws"
)

const (
	defaultClusterName       = "unitune-cluster"
	defaultNamespace         = "unitune-build"
	defaultServiceAccount    = "unitune-builder"
	defaultImageTag          = "latest"
	defaultInitContainerName = "aws-setup"
	defaultMainContainerName = "buildkit"
	buildJobTimeout          = 15 * time.Minute
)

// BuilderConfig holds configuration for the container build process
type BuilderConfig struct {
	AWSConfig  awsclient.Config
	S3Bucket   string
	S3Key      string
	ContextDir string
	ImageName  string
	DryRun     bool
}

// BuildContainer orchestrates the container build process using BuildKit on EKS
func BuildContainer(cfg BuilderConfig) error {
	ctx := context.Background()

	// Get AWS account ID
	accountID, err := aws.GetAccountID(cfg.AWSConfig)
	if err != nil {
		return fmt.Errorf("failed to get AWS account ID: %w", err)
	}

	// Infer image tag from directory name
	imageTag := filepath.Base(cfg.ContextDir)
	if imageTag == "" || imageTag == "." {
		return fmt.Errorf("could not infer image tag from context directory")
	}

	// Build timestamp for job naming
	timestamp := time.Now().Format("20060102150405")

	// Prepare job parameters
	// Use the configured ECR repository (default: unitune) with directory name as tag
	params := k8s.BuildKitJobParams{
		JobName:            fmt.Sprintf("unitune-build-%s", timestamp),
		Namespace:          defaultNamespace,
		BuildID:            timestamp,
		ServiceAccountName: defaultServiceAccount,
		S3Bucket:           cfg.S3Bucket,
		S3Key:              cfg.S3Key,
		ECRRegistry:        fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com", accountID, cfg.AWSConfig.Region),
		ImageName:          cfg.ImageName,
		ImageTag:           imageTag,
		AWSRegion:          cfg.AWSConfig.Region,
	}

	// Dry run mode - just print the rendered YAML
	if cfg.DryRun {
		return printJobYAML(params)
	}

	// Setup the BuildJob on EKS
	buildJob, err := setupBuildJob(cfg.AWSConfig, accountID, params)
	if err != nil {
		return err
	}

	// Create the job
	fmt.Printf("🚀 Creating build job: %s\n", buildJob.Name())
	if err := buildJob.Create(ctx); err != nil {
		return fmt.Errorf("failed to create build job: %w", err)
	}

	// Stream logs
	fmt.Println("📋 Streaming build logs...")
	go func() {
		if err := buildJob.StreamLogs(ctx, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to stream logs: %v\n", err)
		}
	}()

	// Wait for job completion
	fmt.Println("⏳ Waiting for build to complete...")
	if err := buildJob.WaitForCompletion(ctx); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	fmt.Printf("✅ Image pushed: %s/%s:%s\n", params.ECRRegistry, params.ImageName, params.ImageTag)
	return nil
}

// printJobYAML renders and prints the job YAML for dry-run mode
func printJobYAML(params k8s.BuildKitJobParams) error {
	yamlContent, err := k8s.RenderBuildKitJobYAML(params)
	if err != nil {
		return fmt.Errorf("failed to render job YAML: %w", err)
	}

	fmt.Println("---")
	fmt.Println("# Dry run - BuildKit Job YAML:")
	fmt.Println(yamlContent)
	return nil
}

// setupBuildJob prepares and connects to the EKS cluster to create a BuildJob
func setupBuildJob(awsCfg awsclient.Config, accountID string, params k8s.BuildKitJobParams) (*k8s.BuildJob, error) {
	// Construct the cluster admin role ARN for EKS authentication
	clusterAdminRoleArn := fmt.Sprintf("arn:aws:iam::%s:role/%s-admin", accountID, defaultClusterName)

	// Render job from template
	job, err := k8s.RenderBuildKitJob(params)
	if err != nil {
		return nil, fmt.Errorf("failed to render build job: %w", err)
	}

	// Create BuildJob with configuration
	buildJobConfig := k8s.BuildJobConfig{
		JobName:           params.JobName,
		InitContainerName: defaultInitContainerName,
		MainContainerName: defaultMainContainerName,
		Timeout:           buildJobTimeout,
		JobSpec:           job,
	}

	// Create BuildJob (assumes the cluster admin role for authentication)
	fmt.Println("🔌 Connecting to EKS cluster...")
	return NewBuildJobForEKS(awsCfg, defaultClusterName, clusterAdminRoleArn, defaultNamespace, buildJobConfig)
}

// NewBuildJobForEKS creates a BuildJob that connects to an EKS cluster
// If roleArn is provided, the client will assume that role for authentication
func NewBuildJobForEKS(cfg awsclient.Config, clusterName string, roleArn string, namespace string, buildJobConfig k8s.BuildJobConfig) (*k8s.BuildJob, error) {
	eksService := aws.NewEksService(cfg)
	if eksService == nil {
		return nil, fmt.Errorf("failed to create EKS service")
	}

	k8sClient, err := eksService.NewK8sClientForEKS(clusterName, roleArn, namespace)
	if err != nil {
		return nil, err
	}

	return k8s.NewBuildJob(buildJobConfig, k8sClient), nil
}
