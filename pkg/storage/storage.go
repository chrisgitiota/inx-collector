package storage

import (
	"context"
	"bytes"
	"github.com/iotaledger/hive.go/core/logger"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/lifecycle"
	"github.com/minio/minio-go/v7/pkg/tags"
)

type Storage struct {
	*logger.WrappedLogger
	client                      			*minio.Client
	DefaultBucketName           			string
	ObjectsInspectionListBucketName 		string
	KeysToSendToPeerCollectorBucketName 	string
	DefaultBucketExpirationDays 			int
	region                      			string
	objectExtension             			string
	PeerCollectorUrl						string
}

func NewStorage(params Parameters, log *logger.WrappedLogger) (Storage, error) {

	// Initialize minio client object.
	client, err := minio.New(params.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(params.AccessKeyID, params.SecretAccessKey, ""),
		Secure: params.Secure,
	})
	if err != nil {
		return Storage{}, err
	}

	storage := Storage{
		WrappedLogger:               			logger.NewWrappedLogger(log.LoggerNamed("Storage")),
		client:                      			client,
		DefaultBucketName:           			params.DefaultBucketName,
		ObjectsInspectionListBucketName: 		params.ObjectsInspectionListBucketName,
		KeysToSendToPeerCollectorBucketName: 	params.KeysToSendToPeerCollectorBucketName,
		DefaultBucketExpirationDays: 			params.DefaultBucketExpirationDays,
		region:                      			params.Region,
		objectExtension:             			params.ObjectExtension,
	}

	return storage, nil
}

func (s *Storage) CheckCreateBucket(bucketName string, ctx context.Context) (bool, error) {
	exists, err := s.BucketExists(bucketName, ctx)
	if err != nil {
		return false, err
	}
	if exists {
		return true, nil
	}
	err = s.CreateBucket(bucketName, ctx)
	if err != nil {
		return false, err
	}
	return false, nil
}

func (s *Storage) CreateBucket(bucketName string, ctx context.Context) error {
	s.WrappedLogger.LogInfof("Creating bucket '%s' ...", bucketName)
	err := s.client.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{Region: s.region})
	if err != nil {
		s.WrappedLogger.LogErrorf("Creating bucket '%s' ... failed, error: %w", bucketName, err)
		return err
	}

	s.WrappedLogger.LogDebugf("Creating bucket '%s' ... done", bucketName)
	return nil
}

func (s *Storage) SetBucketExpirationDays(bucketName string, days int, ctx context.Context) error {
	// days = 0 means that the bucket has no expiration
	if days == 0 {
		s.WrappedLogger.LogInfof("No lifecycle for bucket '%s'", bucketName)
		return nil
	}

	config := lifecycle.NewConfiguration()
	config.Rules = []lifecycle.Rule{
		{
			ID:     "expire-bucket",
			Status: "Enabled",
			Expiration: lifecycle.Expiration{
				Days: lifecycle.ExpirationDays(days),
			},
		},
	}
	err := s.client.SetBucketLifecycle(ctx, bucketName, config)
	if err != nil {
		s.WrappedLogger.LogErrorf("Failed setting lifecycle for bucket '%s', error: %w", bucketName, err)
	}
	return nil
}

func (s *Storage) GetBucketExpirationDays(bucketName string, ctx context.Context) (int, error) {

	config, err := s.client.GetBucketLifecycle(ctx, bucketName)
	if err != nil {
		s.WrappedLogger.LogInfof("Failed retrieving lifecycle for bucket '%s', error: %w", bucketName, err)
	}
	// days = 0 means that the bucket has no expiration
	days := 0
	for _, rule := range config.Rules {
		if rule.ID == "expire-bucket" && rule.Status == "Enabled" {
			days = int(rule.Expiration.Days)
			break
		}
	}

	return days, nil
}

func (s *Storage) BucketExists(bucketName string, ctx context.Context) (bool, error) {
	exists, err := s.client.BucketExists(ctx, bucketName)
	if err == nil && exists {
		return true, nil
	} else if err != nil {
		return false, err
	}
	return false, nil
}

func (s *Storage) UploadObject(objectName string, bucketName string, object Object, ctx context.Context) error {

	objectReader, err := object.GetByteReader()
	if err != nil {
		return err
	}

	s.WrappedLogger.LogInfof("Uploading object '%s' to bucket '%s' ...", objectName, bucketName)
	_, err = s.client.PutObject(ctx, bucketName, objectName+s.objectExtension, objectReader, objectReader.Size(), minio.PutObjectOptions{ContentType: "application/json"})
	if err != nil {
		s.WrappedLogger.LogErrorf("Uploading object '%s' to bucket '%s' ... failed, error: %w", objectName, bucketName, err)
		return err
	}

	s.WrappedLogger.LogInfof("Uploading object '%s' to bucket '%s' ... done", objectName, bucketName)
	return nil
}

func (s *Storage) GetObject(bucketName string, objectName string, ctx context.Context) (*minio.Object, error) {
	s.WrappedLogger.LogInfof("Retrieving object '%s' from bucket '%s' ... ", objectName, bucketName)
	object, err := s.client.GetObject(ctx, bucketName, objectName+s.objectExtension, minio.GetObjectOptions{})
	if err != nil {
		s.WrappedLogger.LogErrorf("Retrieving object '%s' from bucket '%s' ... failed, error: %w", objectName, bucketName, err)
		return nil, err
	}

	s.WrappedLogger.LogDebugf("Retrieving object '%s' from bucket '%s' ... done", objectName, bucketName)
	return object, nil
}


func (s *Storage) GetObjectsInspectionList(ctx context.Context) <-chan minio.ObjectInfo {
	objectCh := s.client.ListObjects(ctx, s.ObjectsInspectionListBucketName, minio.ListObjectsOptions{
		Recursive: true,
	})
	return objectCh
}

func (s *Storage) GetKeysToBeSendToPeerCollector(ctx context.Context) <-chan minio.ObjectInfo {
	objectCh := s.client.ListObjects(ctx, s.KeysToSendToPeerCollectorBucketName, minio.ListObjectsOptions{
		Recursive: true,
	})
	return objectCh
}

func (s *Storage) DeleteObjectNameFromObjectsInspectionList(objectName string, ctx context.Context) error {
	return s.client.RemoveObject(ctx, s.ObjectsInspectionListBucketName, objectName, minio.RemoveObjectOptions{})
}

func (s *Storage) DeleteObjectNameFromKeysToSendToPeerCollectorList(objectName string, ctx context.Context) error {
	return s.client.RemoveObject(ctx, s.KeysToSendToPeerCollectorBucketName, objectName, minio.RemoveObjectOptions{})
}

func (s *Storage) GetObjectTagging(bucketName string, objectName string, ctx context.Context) (*tags.Tags, error) {
	s.WrappedLogger.LogInfof("Retrieving Tags for object '%s' from bucket '%s' ... ", objectName, bucketName)
	tags, err := s.client.GetObjectTagging(ctx, bucketName, objectName+s.objectExtension, minio.GetObjectTaggingOptions{})
	if err != nil {
		s.WrappedLogger.LogErrorf("Retrieving Tags for object '%s' from bucket '%s' ... failed, error: %w", objectName, bucketName, err)
		return nil, err
	}

	s.WrappedLogger.LogDebugf("Retrieving Tags for object '%s' from bucket '%s' ... done", objectName, bucketName)
	return tags, nil
}

func (s *Storage) DeleteObject(bucketName string, objectName string, ctx context.Context) error {
	return s.client.RemoveObject(ctx, bucketName, objectName+s.objectExtension, minio.RemoveObjectOptions{})
}

func (s *Storage) UploadObjectNameToBucket(bucketName string, objectName string, ctx context.Context) error {
	emptyJsonBytes := []byte("{}")
	emptyJsonReader := bytes.NewReader(emptyJsonBytes)

	s.WrappedLogger.LogInfof("Uploading objectName '%s' to bucket '%s' ...", objectName, bucketName)
	_, err := s.client.PutObject(ctx, bucketName, objectName, emptyJsonReader, emptyJsonReader.Size(), minio.PutObjectOptions{ContentType: "application/json"})
	if err != nil {
		s.WrappedLogger.LogErrorf("Uploading objectName '%s' to bucket '%s' ... failed, error: %w", objectName, bucketName, err)
		return err
	}

	s.WrappedLogger.LogInfof("Uploading objectName '%s' to bucket '%s' ... done", objectName, bucketName)
	return nil
}