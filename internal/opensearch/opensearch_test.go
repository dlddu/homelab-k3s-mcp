package opensearch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

// fakeDoer captures the signed request and returns a canned response.
type fakeDoer struct {
	status int
	body   string
	err    error
	got    *http.Request
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	f.got = req
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{
		StatusCode: f.status,
		Body:       io.NopCloser(strings.NewReader(f.body)),
		Header:     http.Header{},
	}, nil
}

func testClient(doer doer) *Client {
	return &Client{
		endpoint: "https://example.aoss.amazonaws.com",
		region:   "ap-northeast-2",
		creds:    credentials.NewStaticCredentialsProvider("AKIDEXAMPLE", "secret", "session-token"),
		signer:   v4.NewSigner(),
		http:     doer,
	}
}

func int64Ptr(v int64) *int64   { return &v }
func strPtr(s string) *string   { return &s }
func floatEq(a, b float64) bool { return a == b }

const searchResponse = `{
  "hits": {
    "total": {"value": 2, "relation": "eq"},
    "hits": [
      {"_index": "runbooks", "_id": "r1", "_score": 1.5, "_source": {"title": "etcd backup"}},
      {"_index": "runbooks", "_id": "r2", "_score": null, "_source": {"title": "etcd restore"}}
    ]
  }
}`

func TestSearchSignsRequestWithAssumedRoleCreds(t *testing.T) {
	fake := &fakeDoer{status: http.StatusOK, body: searchResponse}
	c := testClient(fake)

	result, err := c.Search(context.Background(), "etcd backup", strPtr("runbooks"), nil)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	req := fake.got
	if req.Method != http.MethodPost || req.URL.String() != "https://example.aoss.amazonaws.com/runbooks/_search" {
		t.Fatalf("request = %s %s", req.Method, req.URL)
	}
	authz := req.Header.Get("Authorization")
	if !strings.HasPrefix(authz, "AWS4-HMAC-SHA256 ") {
		t.Fatalf("Authorization = %q", authz)
	}
	if !strings.Contains(authz, "/ap-northeast-2/aoss/aws4_request") {
		t.Fatalf("Authorization not scoped to aoss: %q", authz)
	}
	if req.Header.Get("X-Amz-Content-Sha256") == "" {
		t.Fatal("X-Amz-Content-Sha256 header missing")
	}
	if req.Header.Get("X-Amz-Security-Token") != "session-token" {
		t.Fatalf("X-Amz-Security-Token = %q", req.Header.Get("X-Amz-Security-Token"))
	}

	body, _ := io.ReadAll(req.Body)
	if !strings.Contains(string(body), `"size":10`) {
		t.Fatalf("default size missing from body: %s", body)
	}
	if !strings.Contains(string(body), `"query":"etcd backup"`) {
		t.Fatalf("query missing from body: %s", body)
	}

	if result.Total != 2 || len(result.Hits) != 2 {
		t.Fatalf("result = %+v", result)
	}
	h := result.Hits[0]
	if h.Index != "runbooks" || h.ID != "r1" || h.Score == nil || !floatEq(*h.Score, 1.5) {
		t.Fatalf("hit = %+v", h)
	}
	if string(h.Source) != `{"title": "etcd backup"}` {
		t.Fatalf("source = %s", h.Source)
	}
	if result.Hits[1].Score != nil {
		t.Fatalf("null score should map to nil, got %v", *result.Hits[1].Score)
	}
}

func TestSearchWithoutIndexTargetsCollection(t *testing.T) {
	fake := &fakeDoer{status: http.StatusOK, body: `{"hits":{"total":{"value":0},"hits":[]}}`}
	c := testClient(fake)

	result, err := c.Search(context.Background(), "anything", nil, int64Ptr(50))
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if fake.got.URL.Path != "/_search" {
		t.Fatalf("path = %s", fake.got.URL.Path)
	}
	body, _ := io.ReadAll(fake.got.Body)
	if !strings.Contains(string(body), `"size":50`) {
		t.Fatalf("size missing from body: %s", body)
	}
	if result.Total != 0 || len(result.Hits) != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestSearchRejectsSizeOverMaxWithoutClamping(t *testing.T) {
	fake := &fakeDoer{status: http.StatusOK, body: searchResponse}
	c := testClient(fake)

	_, err := c.Search(context.Background(), "q", nil, int64Ptr(51))
	if err == nil || !strings.Contains(err.Error(), "size must be <= 50") {
		t.Fatalf("err = %v", err)
	}
	if fake.got != nil {
		t.Fatal("request must not be sent when size exceeds the cap")
	}

	if _, err := c.Search(context.Background(), "q", nil, int64Ptr(0)); err == nil ||
		!strings.Contains(err.Error(), "size must be >= 1") {
		t.Fatalf("err = %v", err)
	}
}

func TestSearchWrapsNonOKStatus(t *testing.T) {
	fake := &fakeDoer{status: http.StatusForbidden, body: `{"message":"no permission"}`}
	c := testClient(fake)

	_, err := c.Search(context.Background(), "q", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "status 403") || !strings.Contains(err.Error(), "no permission") {
		t.Fatalf("err = %v", err)
	}
	var osErr *Error
	if !errors.As(err, &osErr) || osErr.kind != kindRequest {
		t.Fatalf("err = %v (%T)", err, err)
	}
}

func TestPutDocumentWithIDUpserts(t *testing.T) {
	fake := &fakeDoer{status: http.StatusCreated, body: `{"_index":"notes","_id":"n1","result":"created"}`}
	c := testClient(fake)

	result, err := c.PutDocument(context.Background(), "notes", strPtr("n1"), map[string]any{"title": "hello"})
	if err != nil {
		t.Fatalf("PutDocument: %v", err)
	}
	if fake.got.Method != http.MethodPut || fake.got.URL.Path != "/notes/_doc/n1" {
		t.Fatalf("request = %s %s", fake.got.Method, fake.got.URL.Path)
	}
	body, _ := io.ReadAll(fake.got.Body)
	if string(body) != `{"title":"hello"}` {
		t.Fatalf("body = %s", body)
	}
	if result.Index != "notes" || result.ID != "n1" || result.Result != "created" {
		t.Fatalf("result = %+v", result)
	}
}

func TestPutDocumentWithoutIDAutoGenerates(t *testing.T) {
	fake := &fakeDoer{status: http.StatusCreated, body: `{"_index":"notes","_id":"auto-xyz","result":"created"}`}
	c := testClient(fake)

	result, err := c.PutDocument(context.Background(), "notes", nil, map[string]any{"title": "hello"})
	if err != nil {
		t.Fatalf("PutDocument: %v", err)
	}
	if fake.got.Method != http.MethodPost || fake.got.URL.Path != "/notes/_doc" {
		t.Fatalf("request = %s %s", fake.got.Method, fake.got.URL.Path)
	}
	if result.ID != "auto-xyz" {
		t.Fatalf("result = %+v", result)
	}
}

func TestPutDocumentReportsUpdated(t *testing.T) {
	fake := &fakeDoer{status: http.StatusOK, body: `{"_index":"notes","_id":"n1","result":"updated"}`}
	c := testClient(fake)

	result, err := c.PutDocument(context.Background(), "notes", strPtr("n1"), map[string]any{"title": "v2"})
	if err != nil {
		t.Fatalf("PutDocument: %v", err)
	}
	if result.Result != "updated" {
		t.Fatalf("result = %+v", result)
	}
}

func TestPutDocumentWrapsErrorStatus(t *testing.T) {
	fake := &fakeDoer{status: http.StatusBadRequest, body: `{"error":"mapper_parsing_exception"}`}
	c := testClient(fake)

	_, err := c.PutDocument(context.Background(), "notes", strPtr("n1"), map[string]any{"x": 1})
	if err == nil || !strings.Contains(err.Error(), "status 400") {
		t.Fatalf("err = %v", err)
	}
}

func TestDeleteDocumentDeleted(t *testing.T) {
	fake := &fakeDoer{status: http.StatusOK, body: `{"_index":"notes","_id":"n1","result":"deleted"}`}
	c := testClient(fake)

	result, err := c.DeleteDocument(context.Background(), "notes", "n1")
	if err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}
	if fake.got.Method != http.MethodDelete || fake.got.URL.Path != "/notes/_doc/n1" {
		t.Fatalf("request = %s %s", fake.got.Method, fake.got.URL.Path)
	}
	if result.Result != "deleted" || result.Index != "notes" || result.ID != "n1" {
		t.Fatalf("result = %+v", result)
	}
}

func TestDeleteDocumentMissingMapsToNotFound(t *testing.T) {
	fake := &fakeDoer{status: http.StatusNotFound, body: `{"_index":"notes","_id":"ghost","result":"not_found"}`}
	c := testClient(fake)

	result, err := c.DeleteDocument(context.Background(), "notes", "ghost")
	if err != nil {
		t.Fatalf("DeleteDocument: %v", err)
	}
	if result.Result != "not_found" || result.ID != "ghost" {
		t.Fatalf("result = %+v", result)
	}
}

func TestDeleteDocumentWrapsErrorStatus(t *testing.T) {
	fake := &fakeDoer{status: http.StatusForbidden, body: `denied`}
	c := testClient(fake)

	_, err := c.DeleteDocument(context.Background(), "notes", "n1")
	if err == nil || !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("err = %v", err)
	}
}

func TestUnavailableFailsEveryCall(t *testing.T) {
	u := NewUnavailable("boom")
	if _, err := u.Search(context.Background(), "q", nil, nil); err == nil ||
		!strings.Contains(err.Error(), "opensearch unavailable: boom") {
		t.Fatalf("Search err = %v", err)
	}
	if _, err := u.PutDocument(context.Background(), "i", nil, nil); err == nil ||
		!strings.Contains(err.Error(), "opensearch unavailable: boom") {
		t.Fatalf("PutDocument err = %v", err)
	}
	if _, err := u.DeleteDocument(context.Background(), "i", "d"); err == nil ||
		!strings.Contains(err.Error(), "opensearch unavailable: boom") {
		t.Fatalf("DeleteDocument err = %v", err)
	}
}

func TestFromEnvUnsetEndpointReturnsNil(t *testing.T) {
	t.Setenv("OPENSEARCH_ENDPOINT", "")
	client, err := FromEnv(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if client != nil {
		t.Fatalf("client = %v, want nil", client)
	}
}

func TestFromEnvRequiresRoleARN(t *testing.T) {
	t.Setenv("OPENSEARCH_ENDPOINT", "https://example.aoss.amazonaws.com")
	t.Setenv("OPENSEARCH_ROLE_ARN", "")
	if _, err := FromEnv(context.Background()); err == nil || !strings.Contains(err.Error(), "OPENSEARCH_ROLE_ARN") {
		t.Fatalf("missing role err = %v", err)
	}
}

func TestFromEnvRequiresResolvableRegion(t *testing.T) {
	t.Setenv("OPENSEARCH_ENDPOINT", "https://example.aoss.amazonaws.com")
	t.Setenv("OPENSEARCH_ROLE_ARN", "arn:aws:iam::123456789012:role/opensearch")
	t.Setenv("OPENSEARCH_REGION", "")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("AWS_CONFIG_FILE", "/dev/null")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/dev/null")
	if _, err := FromEnv(context.Background()); err == nil || !strings.Contains(err.Error(), "OPENSEARCH_REGION") {
		t.Fatalf("missing region err = %v", err)
	}
}

func TestFromEnvBuildsClient(t *testing.T) {
	t.Setenv("OPENSEARCH_ENDPOINT", "https://example.aoss.amazonaws.com/")
	t.Setenv("OPENSEARCH_ROLE_ARN", "arn:aws:iam::123456789012:role/opensearch")
	t.Setenv("OPENSEARCH_REGION", "ap-northeast-2")
	client, err := FromEnv(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if client == nil {
		t.Fatal("client = nil")
	}
	if client.endpoint != "https://example.aoss.amazonaws.com" {
		t.Fatalf("endpoint = %q (trailing slash should be trimmed)", client.endpoint)
	}
	if client.region != "ap-northeast-2" {
		t.Fatalf("region = %q", client.region)
	}
}
