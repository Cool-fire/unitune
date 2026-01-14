package deploy

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Cool-fire/unitune/pkg/aws"
	awsclient "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type DeployOptions struct {
	DryRun    bool
	ImageName string
}

func (o *DeployOptions) BindFlags(fs *pflag.FlagSet) {
	fs.BoolVar(&o.DryRun, "dry-run", false, "Print the BuildKit job YAML without submitting to the cluster")
	fs.StringVar(&o.ImageName, "image-name", "unitune", "ECR repository name for the image")
}

func (o *DeployOptions) Run(cmd *cobra.Command, args []string) error {
	cfg, err := aws.GetAwsConfig()
	if err != nil {
		return fmt.Errorf("failed to get AWS config: %w", err)
	}

	contextDir, dockerfile, err := getBuildContext()
	if err != nil {
		return fmt.Errorf("failed to get build context: %w", err)
	}

	bucketName, err := getS3BucketName(cfg)
	if err != nil {
		return fmt.Errorf("failed to get S3 bucket name: %w", err)
	}

	s3Key := aws.GenerateBuildContextKey()
	s3Service := aws.NewS3Service(cfg)

	if err := uploadBuildContext(s3Service, bucketName, s3Key, contextDir, dockerfile); err != nil {
		return fmt.Errorf("failed to upload build context: %w", err)
	}

	// Build container using BuildKit on EKS
	builderCfg := BuilderConfig{
		AWSConfig:  cfg,
		S3Bucket:   bucketName,
		S3Key:      s3Key,
		ContextDir: contextDir,
		ImageName:  o.ImageName,
		DryRun:     o.DryRun,
	}

	if err := BuildContainer(builderCfg); err != nil {
		return fmt.Errorf("failed to build container: %w", err)
	}

	return nil
}

func getBuildContext() (string, string, error) {
	contextDir, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("failed to get current directory: %w", err)
	}

	dockerfile := Dockerfile
	if err := validateDockerfile(contextDir, dockerfile); err != nil {
		return "", "", err
	}

	return contextDir, dockerfile, nil
}

func validateDockerfile(contextDir, dockerfile string) error {
	dockerfilePath := filepath.Join(contextDir, dockerfile)
	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		return fmt.Errorf("Dockerfile not found in current directory: %s", contextDir)
	}
	return nil
}

func getS3BucketName(cfg awsclient.Config) (string, error) {
	accountID, err := aws.GetAccountID(cfg)
	if err != nil {
		return "", fmt.Errorf("failed to get AWS account ID: %w", err)
	}

	region := cfg.Region
	if region == "" {
		return "", fmt.Errorf("AWS region not configured")
	}

	bucketName := fmt.Sprintf("unitune-buildctx-%s-%s", accountID, region)
	fmt.Printf("📦 Using S3 bucket: %s\n", bucketName)
	return bucketName, nil
}

func uploadBuildContext(s3Service *aws.S3Service, bucketName, s3Key, contextDir, dockerfile string) error {
	fmt.Println("📦 Creating build context archive...")

	tarReader, err := CreateBuildContext(contextDir, &dockerfile)
	if err != nil {
		return fmt.Errorf("failed to create build context: %w", err)
	}
	defer tarReader.Close()

	fmt.Printf("📤 Uploading to s3://%s/%s\n", bucketName, s3Key)
	if err := s3Service.UploadToS3(bucketName, s3Key, tarReader); err != nil {
		return fmt.Errorf("failed to upload build context to S3: %w", err)
	}

	fmt.Printf("✅ Build context uploaded successfully to s3://%s/%s\n", bucketName, s3Key)
	return nil
}

func AddCommand() *cobra.Command {
	o := &DeployOptions{}

	c := &cobra.Command{
		Use:   "deploy",
		Short: "Build and deploy a container image",
		Long:  "Upload build context to S3 and build a container image using BuildKit on EKS",
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.Run(cmd, args)
		},
	}

	o.BindFlags(c.Flags())
	return c
}
