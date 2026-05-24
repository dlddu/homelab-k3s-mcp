package awsconfig

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type fakeS3 struct {
	out       *s3.GetObjectOutput
	err       error
	gotBucket string
	gotKey    string
}

func (f *fakeS3) GetObject(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if in.Bucket != nil {
		f.gotBucket = *in.Bucket
	}
	if in.Key != nil {
		f.gotKey = *in.Key
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.out, nil
}

func TestGetConfigMapsObjectAndMetadata(t *testing.T) {
	modified := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	fake := &fakeS3{out: &s3.GetObjectOutput{
		Body:         io.NopCloser(strings.NewReader("[default]\nregion = ap-northeast-2\n")),
		ContentType:  aws.String("text/plain"),
		ETag:         aws.String(`"abc123"`),
		LastModified: aws.Time(modified),
	}}
	c := &Client{s3: fake, bucket: "homelab-config", key: "aws/config"}

	obj, err := c.GetConfig(context.Background())
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if fake.gotBucket != "homelab-config" || fake.gotKey != "aws/config" {
		t.Fatalf("requested s3://%s/%s", fake.gotBucket, fake.gotKey)
	}
	if obj.Content != "[default]\nregion = ap-northeast-2\n" {
		t.Fatalf("content = %q", obj.Content)
	}
	if obj.Size != int64(len(obj.Content)) {
		t.Fatalf("size = %d, want %d", obj.Size, len(obj.Content))
	}
	if obj.ContentType != "text/plain" {
		t.Fatalf("contentType = %q", obj.ContentType)
	}
	if obj.ETag != "abc123" {
		t.Fatalf("etag = %q, want unquoted abc123", obj.ETag)
	}
	if obj.LastModified != "2026-05-10T12:00:00Z" {
		t.Fatalf("lastModified = %q", obj.LastModified)
	}
}

func TestGetConfigWrapsGetObjectError(t *testing.T) {
	fake := &fakeS3{err: errors.New("AccessDenied")}
	c := &Client{s3: fake, bucket: "homelab-config", key: "aws/config"}

	_, err := c.GetConfig(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	var awsErr *Error
	if !errors.As(err, &awsErr) || awsErr.kind != kindFetch {
		t.Fatalf("error = %v (%T)", err, err)
	}
	if !strings.Contains(err.Error(), "get s3://homelab-config/aws/config") ||
		!strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestUnavailableGetConfig(t *testing.T) {
	_, err := NewUnavailable("boom").GetConfig(context.Background())
	if err == nil || !strings.Contains(err.Error(), "aws config unavailable: boom") {
		t.Fatalf("error = %v", err)
	}
}

func TestFromEnvUnsetBucketReturnsNil(t *testing.T) {
	t.Setenv("AWS_CONFIG_S3_BUCKET", "")
	client, err := FromEnv(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if client != nil {
		t.Fatalf("client = %v, want nil", client)
	}
}

func TestFromEnvRequiresKeyAndRole(t *testing.T) {
	t.Setenv("AWS_CONFIG_S3_BUCKET", "homelab-config")

	t.Setenv("AWS_CONFIG_S3_KEY", "")
	t.Setenv("AWS_CONFIG_ROLE_ARN", "arn:aws:iam::123456789012:role/config-reader")
	if _, err := FromEnv(context.Background()); err == nil || !strings.Contains(err.Error(), "AWS_CONFIG_S3_KEY") {
		t.Fatalf("missing key err = %v", err)
	}

	t.Setenv("AWS_CONFIG_S3_KEY", "aws/config")
	t.Setenv("AWS_CONFIG_ROLE_ARN", "")
	if _, err := FromEnv(context.Background()); err == nil || !strings.Contains(err.Error(), "AWS_CONFIG_ROLE_ARN") {
		t.Fatalf("missing role err = %v", err)
	}
}
