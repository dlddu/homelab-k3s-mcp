package opensearch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

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
		endpoint: "http://docstore.example.svc.cluster.local",
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

func TestSearchSendsUnsignedJSONRequest(t *testing.T) {
	fake := &fakeDoer{status: http.StatusOK, body: searchResponse}
	c := testClient(fake)

	result, err := c.Search(context.Background(), "etcd backup", strPtr("runbooks"), nil)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	req := fake.got
	if req.Method != http.MethodPost || req.URL.String() != "http://docstore.example.svc.cluster.local/runbooks/_search" {
		t.Fatalf("request = %s %s", req.Method, req.URL)
	}
	if req.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type = %q", req.Header.Get("Content-Type"))
	}
	// docstore is reached over in-cluster HTTP: no credential travels with it.
	for _, h := range []string{"Authorization", "X-Amz-Date", "X-Amz-Security-Token", "X-Amz-Content-Sha256"} {
		if v := req.Header.Get(h); v != "" {
			t.Fatalf("%s header must not be sent, got %q", h, v)
		}
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

func sigv4Client(doer doer, service string) *Client {
	return &Client{
		endpoint: "https://example." + service + ".amazonaws.com",
		http:     doer,
		auth: sigv4Auth{
			region:  "ap-northeast-2",
			service: service,
			creds:   credentials.NewStaticCredentialsProvider("AKIDEXAMPLE", "secret", "session-token"),
			signer:  v4.NewSigner(),
		},
	}
}

func TestSigV4SignsWithAssumedRoleCreds(t *testing.T) {
	for _, service := range []string{"aoss", "es"} {
		fake := &fakeDoer{status: http.StatusOK, body: searchResponse}
		c := sigv4Client(fake, service)
		if _, err := c.Search(context.Background(), "etcd backup", strPtr("runbooks"), nil); err != nil {
			t.Fatalf("%s: Search: %v", service, err)
		}
		authz := fake.got.Header.Get("Authorization")
		if !strings.HasPrefix(authz, "AWS4-HMAC-SHA256 ") ||
			!strings.Contains(authz, "/ap-northeast-2/"+service+"/aws4_request") {
			t.Fatalf("%s: Authorization = %q", service, authz)
		}
		if fake.got.Header.Get("X-Amz-Content-Sha256") == "" {
			t.Fatalf("%s: X-Amz-Content-Sha256 header missing", service)
		}
		if fake.got.Header.Get("X-Amz-Security-Token") != "session-token" {
			t.Fatalf("%s: X-Amz-Security-Token = %q", service, fake.got.Header.Get("X-Amz-Security-Token"))
		}
		if c.AuthMode() != AuthSigV4 {
			t.Fatalf("%s: mode = %s", service, c.AuthMode())
		}
	}
}

func TestBasicAuthSendsCredentials(t *testing.T) {
	fake := &fakeDoer{status: http.StatusOK, body: `{"_index":"n","_id":"1","result":"created"}`}
	c := &Client{endpoint: "https://opensearch.example", http: fake, auth: basicAuth{user: "admin", pass: "pw"}}
	if _, err := c.PutDocument(context.Background(), "n", strPtr("1"), map[string]any{"a": 1}); err != nil {
		t.Fatalf("PutDocument: %v", err)
	}
	user, pass, ok := fake.got.BasicAuth()
	if !ok || user != "admin" || pass != "pw" {
		t.Fatalf("basic auth = %q %q %v", user, pass, ok)
	}
	if fake.got.Header.Get("X-Amz-Date") != "" {
		t.Fatal("basic mode must not sign")
	}
	if c.AuthMode() != AuthBasic {
		t.Fatalf("mode = %s", c.AuthMode())
	}
}

func clearAuthEnv(t *testing.T) {
	for _, k := range []string{"OPENSEARCH_USERNAME", "OPENSEARCH_PASSWORD", "OPENSEARCH_ROLE_ARN",
		"OPENSEARCH_REGION", "OPENSEARCH_SIGNING_SERVICE", "OPENSEARCH_STS_ENDPOINT", "OPENSEARCH_ROLE_SESSION_NAME"} {
		t.Setenv(k, "")
	}
}

func TestFromEnvSelectsAuthMode(t *testing.T) {
	clearAuthEnv(t)
	t.Setenv("OPENSEARCH_ENDPOINT", "https://opensearch.example")

	c, err := FromEnv(context.Background())
	if err != nil || c.AuthMode() != AuthNone {
		t.Fatalf("none: client=%v err=%v", c, err)
	}

	t.Setenv("OPENSEARCH_USERNAME", "admin")
	t.Setenv("OPENSEARCH_PASSWORD", "pw")
	if c, err = FromEnv(context.Background()); err != nil || c.AuthMode() != AuthBasic {
		t.Fatalf("basic: client=%v err=%v", c, err)
	}

	clearAuthEnv(t)
	t.Setenv("OPENSEARCH_ROLE_ARN", "arn:aws:iam::123456789012:role/opensearch")
	t.Setenv("OPENSEARCH_REGION", "ap-northeast-2")
	if c, err = FromEnv(context.Background()); err != nil || c.AuthMode() != AuthSigV4 {
		t.Fatalf("sigv4: client=%v err=%v", c, err)
	}
	if got := c.auth.(sigv4Auth); got.service != "es" || got.region != "ap-northeast-2" {
		t.Fatalf("sigv4 defaults = %+v", got)
	}
	t.Setenv("OPENSEARCH_SIGNING_SERVICE", "aoss")
	if c, err = FromEnv(context.Background()); err != nil || c.auth.(sigv4Auth).service != "aoss" {
		t.Fatalf("sigv4 aoss: client=%v err=%v", c, err)
	}
}

func TestFromEnvRejectsAmbiguousAuth(t *testing.T) {
	cases := map[string]map[string]string{
		"username without password": {"OPENSEARCH_USERNAME": "admin"},
		"password without username": {"OPENSEARCH_PASSWORD": "pw"},
		"basic and sigv4": {"OPENSEARCH_USERNAME": "admin", "OPENSEARCH_PASSWORD": "pw",
			"OPENSEARCH_ROLE_ARN": "arn:aws:iam::123456789012:role/opensearch"},
		"unknown signing service": {"OPENSEARCH_ROLE_ARN": "arn:aws:iam::123456789012:role/opensearch",
			"OPENSEARCH_REGION": "ap-northeast-2", "OPENSEARCH_SIGNING_SERVICE": "s3"},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			clearAuthEnv(t)
			t.Setenv("OPENSEARCH_ENDPOINT", "https://opensearch.example")
			for k, v := range env {
				t.Setenv(k, v)
			}
			if c, err := FromEnv(context.Background()); err == nil {
				t.Fatalf("expected a configuration error, got client %+v", c)
			}
		})
	}
}

func TestFromEnvSigV4RequiresResolvableRegion(t *testing.T) {
	clearAuthEnv(t)
	t.Setenv("OPENSEARCH_ENDPOINT", "https://example.aoss.amazonaws.com")
	t.Setenv("OPENSEARCH_ROLE_ARN", "arn:aws:iam::123456789012:role/opensearch")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("AWS_CONFIG_FILE", "/dev/null")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/dev/null")
	if _, err := FromEnv(context.Background()); err == nil || !strings.Contains(err.Error(), "OPENSEARCH_REGION") {
		t.Fatalf("missing region err = %v", err)
	}
}

func TestFromEnvRejectsNonHTTPEndpoint(t *testing.T) {
	for _, endpoint := range []string{"docstore:9200", "ftp://docstore", "http://"} {
		t.Setenv("OPENSEARCH_ENDPOINT", endpoint)
		if _, err := FromEnv(context.Background()); err == nil || !strings.Contains(err.Error(), "OPENSEARCH_ENDPOINT") {
			t.Fatalf("endpoint %q: err = %v", endpoint, err)
		}
	}
}

func TestFromEnvBuildsClient(t *testing.T) {
	clearAuthEnv(t)
	t.Setenv("OPENSEARCH_ENDPOINT", "http://docstore-external.docstore.svc.cluster.local/")
	client, err := FromEnv(context.Background())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if client == nil {
		t.Fatal("client = nil")
	}
	if client.endpoint != "http://docstore-external.docstore.svc.cluster.local" {
		t.Fatalf("endpoint = %q (trailing slash should be trimmed)", client.endpoint)
	}
}

// When docstore is asleep, Sablier's blocking strategy holds the request and,
// past its timeout, answers 307 to the same URL. The client must replay the
// request, body included.
func TestSearchFollowsSablierRedirectWithBody(t *testing.T) {
	var calls int
	var lastBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		b, _ := io.ReadAll(r.Body)
		lastBody = string(b)
		if calls == 1 {
			http.Redirect(w, r, r.URL.String(), http.StatusTemporaryRedirect)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, searchResponse)
	}))
	defer srv.Close()

	c := &Client{endpoint: srv.URL, http: srv.Client()}
	result, err := c.Search(context.Background(), "etcd", nil, nil)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if calls != 2 || !strings.Contains(lastBody, `"query":"etcd"`) {
		t.Fatalf("calls = %d, replayed body = %q", calls, lastBody)
	}
	if result.Total != 2 {
		t.Fatalf("result = %+v", result)
	}
}
