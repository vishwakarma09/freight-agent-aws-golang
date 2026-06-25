package services

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	appConfig "github.com/vishwakarma09/freight-agent-aws-golang/internal/config"
)

// GeneratePresignedUploadURL generates a secure presigned URL to upload a file directly to S3
func GeneratePresignedUploadURL(fileKey string, expirationSeconds int64) (string, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return "", err
	}

	client := s3.NewFromConfig(cfg)
	presignClient := s3.NewPresignClient(client)

	bucketName := appConfig.Settings.S3BucketName
	if bucketName == "" {
		bucketName = "dispatch-private-go"
	}

	presignedReq, err := presignClient.PresignPutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: &bucketName,
		Key:    &fileKey,
	}, s3.WithPresignExpires(time.Duration(expirationSeconds)*time.Second))
	if err != nil {
		return "", err
	}

	return presignedReq.URL, nil
}
