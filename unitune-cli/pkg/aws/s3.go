package aws

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Service struct {
	s3Client *s3.Client
	uploader *manager.Uploader
}

func NewS3Service(cfg aws.Config) *S3Service {
	client := s3.NewFromConfig(cfg)
	return &S3Service{
		s3Client: client,
		uploader: manager.NewUploader(client),
	}
}

// UploadToS3 uploads a reader to an S3 bucket with the given key
func (s *S3Service) UploadToS3(bucketName string, key string, body io.Reader) error {
	ctx := context.Background()
	_, err := s.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
		Body:   body,
	})

	if err != nil {
		return fmt.Errorf("failed to upload to s3://%s/%s: %w", bucketName, key, err)
	}

	return nil
}

func GenerateBuildContextKey() string {
	timestamp := time.Now().Format("20060102150405")
	return fmt.Sprintf("contexts/%s.tar", timestamp)
}

