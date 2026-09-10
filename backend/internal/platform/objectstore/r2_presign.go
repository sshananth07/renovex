package objectstore

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// PresignedUpload is what a caller needs to perform a direct browser/client
// PUT — R2 mode returns a short-lived presigned URL; local mode's
// equivalent (the existing HMAC proxy routes) constructs this same shape
// so callers never need to branch on deployment mode (M8.5C plan: "Existing
// browser/RoomPlan artifact uploads must use direct presigned PUT in R2
// mode... Preserve the current request-upload/finalize sequence and
// response shape (url, method, required headers, expiry)").
type PresignedUpload struct {
	URL             string
	Method          string
	RequiredHeaders map[string]string
	ExpiresAt       time.Time
}

// R2Presigner mints presigned GET/PUT URLs against R2's S3-compatible API,
// and additionally supports Head for finalize-time upload verification. A
// separate type from R2ObjectStore (not additional methods on it) because
// presigning needs its own *s3.PresignClient, and not every R2ObjectStore
// caller needs presigning capability.
type R2Presigner struct {
	client        *s3.Client
	presignClient *s3.PresignClient
	bucket        string
}

// NewR2Presigner constructs an R2Presigner sharing the same account/
// credentials/bucket/endpoint configuration NewR2ObjectStore uses — callers
// typically construct both against the same R2 bucket.
func NewR2Presigner(accountID, accessKeyID, secretAccessKey, bucket, endpoint string) *R2Presigner {
	client := s3.New(s3.Options{
		Region:       "auto",
		BaseEndpoint: aws.String(endpoint),
		Credentials:  credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, ""),
		UsePathStyle: true,
	})
	return &R2Presigner{client: client, presignClient: s3.NewPresignClient(client), bucket: bucket}
}

// PresignGet mints a short-lived presigned GET URL for key, valid for ttl.
func (p *R2Presigner) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, time.Time, error) {
	req, err := p.presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(p.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("objectstore: presigning GET for %q: %w", key, err)
	}
	return req.URL, time.Now().Add(ttl), nil
}

// PresignPut mints a short-lived presigned PUT for key, bound to
// contentType and checksumSHA256 (base64-encoded, matching S3's
// x-amz-checksum-sha256 convention) — the returned RequiredHeaders MUST be
// sent exactly as-is by the uploading client, or the signature will not
// validate (M8.5C plan: "Bind content type and checksum header in the PUT
// signature").
func (p *R2Presigner) PresignPut(ctx context.Context, key, contentType, checksumSHA256Base64 string, ttl time.Duration) (PresignedUpload, error) {
	req, err := p.presignClient.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:            aws.String(p.bucket),
		Key:               aws.String(key),
		ContentType:       aws.String(contentType),
		ChecksumSHA256:    aws.String(checksumSHA256Base64),
		ChecksumAlgorithm: "SHA256",
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return PresignedUpload{}, fmt.Errorf("objectstore: presigning PUT for %q: %w", key, err)
	}
	return PresignedUpload{
		URL:    req.URL,
		Method: req.Method,
		RequiredHeaders: map[string]string{
			"Content-Type":             contentType,
			"x-amz-checksum-sha256":    checksumSHA256Base64,
			"x-amz-checksum-algorithm": "SHA256",
		},
		ExpiresAt: time.Now().Add(ttl),
	}, nil
}

// HeadResult is what Finalize needs to verify an uploaded object without
// downloading it — object metadata/HEAD, per the plan's "checks R2 object
// metadata/HEAD, declared checksum, and size" requirement.
type HeadResult struct {
	SizeBytes      int64
	ChecksumSHA256 string // base64, empty if R2 did not return one
	ContentType    string
}

// Head returns key's stored metadata without transferring content, or
// ErrObjectNotFound if key does not exist — the finalize-time verification
// primitive PresignPut's caller uses before trusting a client-reported
// upload actually landed.
func (p *R2Presigner) Head(ctx context.Context, key string) (HeadResult, error) {
	out, err := p.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(p.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFound(err) {
			return HeadResult{}, ErrObjectNotFound
		}
		return HeadResult{}, fmt.Errorf("objectstore: heading object %q: %w", key, err)
	}
	result := HeadResult{}
	if out.ContentLength != nil {
		result.SizeBytes = *out.ContentLength
	}
	if out.ChecksumSHA256 != nil {
		result.ChecksumSHA256 = *out.ChecksumSHA256
	}
	if out.ContentType != nil {
		result.ContentType = *out.ContentType
	}
	return result, nil
}
