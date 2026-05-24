// Package awsconfig fetches a fixed AWS config object from S3 using an assumed
// IAM role. The base credentials come from the default AWS credential chain
// (the EC2/EKS instance profile in production); they are used only to assume
// the configured role via STS, and the resulting role credentials sign the S3
// read.
package awsconfig

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	sdkconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

const (
	defaultRoleSessionName = "homelab-k3s-mcp"
	getObjectTimeout       = 15 * time.Second
)

// errKind separates a "not configured" failure from a runtime fetch error.
type errKind int

const (
	kindUnavailable errKind = iota
	kindFetch
)

// Error is the error type returned by Service.
type Error struct {
	kind errKind
	msg  string
}

func (e *Error) Error() string {
	switch e.kind {
	case kindUnavailable:
		return "aws config unavailable: " + e.msg
	default:
		return "aws config error: " + e.msg
	}
}

func unavailable(msg string) *Error { return &Error{kind: kindUnavailable, msg: msg} }
func fetchError(msg string) *Error  { return &Error{kind: kindFetch, msg: msg} }

// Object is the fetched S3 config object and its metadata.
type Object struct {
	Bucket       string `json:"bucket"`
	Key          string `json:"key"`
	Content      string `json:"content"`
	ContentType  string `json:"contentType,omitempty"`
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"lastModified,omitempty"`
	Size         int64  `json:"size"`
}

// Service fetches the configured AWS config object from S3.
type Service interface {
	GetConfig(ctx context.Context) (*Object, error)
}

// Unavailable is a Service that fails every call with the same reason.
type Unavailable struct {
	reason string
}

// NewUnavailable builds an Unavailable service with the given reason.
func NewUnavailable(reason string) *Unavailable {
	if reason == "" {
		reason = "aws config integration is not configured"
	}
	return &Unavailable{reason: reason}
}

// GetConfig always fails with the configured reason.
func (u *Unavailable) GetConfig(context.Context) (*Object, error) {
	return nil, unavailable(u.reason)
}

// objectGetter is the subset of the S3 API the client depends on.
type objectGetter interface {
	GetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

// Client is the live S3-backed implementation of Service.
type Client struct {
	s3     objectGetter
	bucket string
	key    string
}

// FromEnv builds a Client from the AWS_CONFIG_* environment variables. It
// returns (nil, nil) when AWS_CONFIG_S3_BUCKET is unset, signalling that the
// integration is simply not configured (as opposed to misconfigured).
//
// When AWS_CONFIG_S3_ENDPOINT is set, both the STS and S3 calls are routed to
// that endpoint with path-style S3 addressing. This targets S3-compatible
// servers such as MinIO (which co-locates the STS and S3 APIs on one port) and
// is intended for smoke testing; production leaves it unset to use real AWS.
func FromEnv(ctx context.Context) (*Client, error) {
	bucket := os.Getenv("AWS_CONFIG_S3_BUCKET")
	if bucket == "" {
		return nil, nil
	}
	key := os.Getenv("AWS_CONFIG_S3_KEY")
	if key == "" {
		return nil, fmt.Errorf("AWS_CONFIG_S3_KEY is required when AWS_CONFIG_S3_BUCKET is set")
	}
	roleARN := os.Getenv("AWS_CONFIG_ROLE_ARN")
	if roleARN == "" {
		return nil, fmt.Errorf("AWS_CONFIG_ROLE_ARN is required when AWS_CONFIG_S3_BUCKET is set")
	}

	var loadOpts []func(*sdkconfig.LoadOptions) error
	if region := os.Getenv("AWS_CONFIG_S3_REGION"); region != "" {
		loadOpts = append(loadOpts, sdkconfig.WithRegion(region))
	}

	// Base credentials: the default chain (instance profile in production).
	baseCfg, err := sdkconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	sessionName := os.Getenv("AWS_CONFIG_ROLE_SESSION_NAME")
	if sessionName == "" {
		sessionName = defaultRoleSessionName
	}
	externalID := os.Getenv("AWS_CONFIG_ROLE_EXTERNAL_ID")
	endpoint := os.Getenv("AWS_CONFIG_S3_ENDPOINT")

	stsClient := sts.NewFromConfig(baseCfg, func(o *sts.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	})
	provider := stscreds.NewAssumeRoleProvider(stsClient, roleARN, func(o *stscreds.AssumeRoleOptions) {
		o.RoleSessionName = sessionName
		if externalID != "" {
			o.ExternalID = aws.String(externalID)
		}
	})

	s3Client := s3.NewFromConfig(baseCfg, func(o *s3.Options) {
		o.Credentials = aws.NewCredentialsCache(provider)
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true
		}
	})

	return &Client{s3: s3Client, bucket: bucket, key: key}, nil
}

// GetConfig fetches the configured object from S3 with the assumed-role
// credentials and returns its contents and metadata.
func (c *Client) GetConfig(ctx context.Context) (*Object, error) {
	ctx, cancel := context.WithTimeout(ctx, getObjectTimeout)
	defer cancel()

	out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(c.key),
	})
	if err != nil {
		return nil, fetchError(fmt.Sprintf("get s3://%s/%s: %v", c.bucket, c.key, err))
	}
	defer out.Body.Close()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fetchError(fmt.Sprintf("read s3://%s/%s: %v", c.bucket, c.key, err))
	}

	obj := &Object{
		Bucket:  c.bucket,
		Key:     c.key,
		Content: string(data),
		Size:    int64(len(data)),
	}
	if out.ContentType != nil {
		obj.ContentType = *out.ContentType
	}
	if out.ETag != nil {
		obj.ETag = strings.Trim(*out.ETag, `"`)
	}
	if out.LastModified != nil {
		obj.LastModified = out.LastModified.UTC().Format(time.RFC3339)
	}
	return obj, nil
}
