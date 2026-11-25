package services

import (
	"bytes"
	"context"
	"errors"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	appConfig "github.com/vishwakarma09/freight-agent-aws-golang/internal/config"
)

func getPrivateBucketName() (string, error) {
	bucketName := appConfig.Settings.S3BucketName
	if bucketName == "" {
		return "", errors.New("S3_BUCKET_NAME is not configured")
	}
	return bucketName, nil
}

// GeneratePresignedUploadURL generates a tenant-scoped presigned PUT URL with ContentType enforcement.
func GeneratePresignedUploadURL(fileKey string, contentType string, expirationSeconds int64) (string, error) {
	bucketName, err := getPrivateBucketName()
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return "", err
	}

	client := s3.NewFromConfig(cfg)
	presignClient := s3.NewPresignClient(client)

	input := &s3.PutObjectInput{
		Bucket: &bucketName,
		Key:    &fileKey,
	}
	if contentType != "" {
		input.ContentType = &contentType
	}

	presignedReq, err := presignClient.PresignPutObject(ctx, input, s3.WithPresignExpires(time.Duration(expirationSeconds)*time.Second))
	if err != nil {
		return "", err
	}

	return presignedReq.URL, nil
}

// GeneratePresignedDownloadURL generates a short-lived presigned GET URL for private S3 documents.
func GeneratePresignedDownloadURL(fileKey string, expirationSeconds int64) (string, error) {
	bucketName, err := getPrivateBucketName()
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return "", err
	}

	client := s3.NewFromConfig(cfg)
	presignClient := s3.NewPresignClient(client)

	presignedReq, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: &bucketName,
		Key:    &fileKey,
	}, s3.WithPresignExpires(time.Duration(expirationSeconds)*time.Second))
	if err != nil {
		return "", err
	}

	return presignedReq.URL, nil
}

// UploadDocumentBytes uploads a server-generated document (BOL / Commercial Invoice) into the private S3 bucket.
func UploadDocumentBytes(fileKey string, contentType string, content []byte) error {
	bucketName, err := getPrivateBucketName()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return err
	}

	client := s3.NewFromConfig(cfg)
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &bucketName,
		Key:         &fileKey,
		Body:        bytes.NewReader(content),
		ContentType: &contentType,
	})
	return err
}
