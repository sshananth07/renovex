package objectstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

// R2ObjectStore implements ObjectStore against Cloudflare R2's S3-compatible
// API — the sole durable object store in the tester deployment (M8.5C plan:
// "R2 is the sole durable object store in the tester deployment"). Every
// spatial adapter (artifact, generation staging, visual asset, design
// reference) shares ONE R2ObjectStore instance, matching LocalObjectStore's
// existing "one store, many logical key prefixes" convention — the
// difference is purely which physical backend a given deployment mode
// wires.
type R2ObjectStore struct {
	client *s3.Client
	bucket string
}

// NewR2ObjectStore constructs an R2ObjectStore against R2's S3-compatible
// endpoint. R2 requires path-style addressing and has no region concept of
// its own (Cloudflare accepts "auto"); credentials are static
// access-key/secret pairs, never an assumed-role chain — this deployment
// has no AWS account to assume a role in.
func NewR2ObjectStore(accountID, accessKeyID, secretAccessKey, bucket, endpoint string) *R2ObjectStore {
	client := s3.New(s3.Options{
		Region:       "auto",
		BaseEndpoint: aws.String(endpoint),
		Credentials:  credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, ""),
		UsePathStyle: true,
	})
	return &R2ObjectStore{client: client, bucket: bucket}
}

func (s *R2ObjectStore) Put(ctx context.Context, key string, content io.Reader) (int64, error) {
	// PutObject needs a body it can compute a content-length/checksum
	// from; a plain io.Reader isn't seekable, so buffer it once here. R2
	// objects in this deployment (reference images, staged GLBs, visual
	// asset versions) are all bounded by the same size guards enforced
	// before Put is ever called (REFERENCE_IMAGE_MAX_BYTES and the
	// existing artifact/asset-generation size limits) — never unbounded
	// streaming uploads.
	buf, err := io.ReadAll(content)
	if err != nil {
		return 0, fmt.Errorf("objectstore: reading content for %q: %w", key, err)
	}
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(buf),
	})
	if err != nil {
		return 0, fmt.Errorf("objectstore: putting object %q: %w", key, err)
	}
	return int64(len(buf)), nil
}

func (s *R2ObjectStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, ErrObjectNotFound
		}
		return nil, fmt.Errorf("objectstore: getting object %q: %w", key, err)
	}
	return out.Body, nil
}

func (s *R2ObjectStore) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("objectstore: checking object %q: %w", key, err)
	}
	return true, nil
}

func (s *R2ObjectStore) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("objectstore: deleting object %q: %w", key, err)
	}
	return nil
}

// isNotFound reports whether err represents R2/S3's "no such key/object"
// response — HeadObject and GetObject use different error shapes for the
// same underlying condition (404 vs NoSuchKey), so both are checked.
func isNotFound(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NotFound", "NoSuchKey":
			return true
		}
	}
	return false
}
