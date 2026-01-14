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
	defaultClusterName    = "unitune-cluster"
	defaultNamespace      = "unitune-build"
	defaultServiceAccount = "unitune-builder"
	defaultImageTag       = "latest"
	buildJobTimeout       = 15 * time.Minute
)

// BuilderConfig holds configuration for the container build process
type BuilderConfig struct {
	AWSConfig  awsclient.Config
	S3Bucket   string
	S3Key      string
	ContextDir string
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

	// Infer image name from directory
	imageName := filepath.Base(cfg.ContextDir)
	if imageName == "" || imageName == "." {
		return fmt.Errorf("could not infer image name from context directory")
	}

	// Build timestamp for job naming
	timestamp := time.Now().Format("20060102150405")

	// Prepare job parameters
	params := k8s.BuildKitJobParams{
		JobName:            fmt.Sprintf("unitune-build-%s", timestamp),
		Namespace:          defaultNamespace,
		BuildID:            timestamp,
		ServiceAccountName: defaultServiceAccount,
		S3Bucket:           cfg.S3Bucket,
		S3Key:              cfg.S3Key,
		ECRRegistry:        fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com", accountID, cfg.AWSConfig.Region),
		ImageName:          imageName,
		ImageTag:           defaultImageTag,
		AWSRegion:          cfg.AWSConfig.Region,
	}

	// Dry run mode - just print the rendered YAML
	if cfg.DryRun {
		return printJobYAML(params)
	}

	// Create K8s client
	fmt.Println("🔌 Connecting to EKS cluster...")
	k8sClient, err := k8s.NewK8sClientForEKS(cfg.AWSConfig, defaultClusterName)
	if err != nil {
		return fmt.Errorf("failed to connect to EKS cluster: %w", err)
	}

	// Render job from template
	job, err := k8s.RenderBuildKitJob(params)
	if err != nil {
		return fmt.Errorf("failed to render build job: %w", err)
	}

	// Create the job
	fmt.Printf("🚀 Creating build job: %s\n", params.JobName)
	if err := k8sClient.CreateJob(ctx, job); err != nil {
		return fmt.Errorf("failed to create build job: %w", err)
	}

	// Stream logs
	fmt.Println("📋 Streaming build logs...")
	go func() {
		if err := k8sClient.StreamJobLogs(ctx, params.JobName, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to stream logs: %v\n", err)
		}
	}()

	// Wait for job completion
	fmt.Println("⏳ Waiting for build to complete...")
	if err := k8sClient.WaitForJobCompletion(ctx, params.JobName, buildJobTimeout); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	fmt.Printf("✅ Image pushed: %s/%s:%s\n", params.ECRRegistry, imageName, defaultImageTag)
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
