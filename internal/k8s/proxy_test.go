package k8s

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestProxyVerbFollowsTheMethod is the mapping AC15 makes the whole tool hang
// on, asserted here rather than only at the tool layer because both layers read
// it: the gate names a pair from it before the cluster is touched, and a 403 is
// worded from it afterwards. A second copy is what this test exists to prevent.
func TestProxyVerbFollowsTheMethod(t *testing.T) {
	for method, want := range map[string]string{
		"GET": "get", "POST": "create", "PUT": "update",
		"PATCH": "patch", "DELETE": "delete",
		"get": "get", "post": "create",
	} {
		if got := ProxyVerbForMethod(method); got != want {
			t.Errorf("ProxyVerbForMethod(%q) = %q, want %q", method, got, want)
		}
	}
	for _, method := range []string{"HEAD", "OPTIONS", "TRACE", "", "GETS"} {
		if got := ProxyVerbForMethod(method); got != "" {
			t.Errorf("ProxyVerbForMethod(%q) = %q, want no verb — an unmapped method has no pair to approve", method, got)
		}
	}
}

// TestSplitProxyPathKeepsTheQuery covers the difference between reaching
// kubelet's /exec with a command and asking it for a file named "?command=id".
// rest.Request escapes a suffix segment, so the query has to be handed over as
// parameters rather than as part of the path — scenario 15 reaches
// /exec/⟨ns⟩/⟨pod⟩/⟨c⟩?command=id, which is exactly that case.
func TestSplitProxyPathKeepsTheQuery(t *testing.T) {
	cases := []struct {
		path     string
		segments []string
		query    url.Values
	}{
		{"/healthz", []string{"healthz"}, url.Values{}},
		{"/", nil, url.Values{}},
		{"/metrics?verbose=1", []string{"metrics"}, url.Values{"verbose": {"1"}}},
		{
			"/exec/ops/api/app?command=id&command=-u",
			[]string{"exec", "ops", "api", "app"},
			url.Values{"command": {"id", "-u"}},
		},
		// Repeated and trailing slashes are dropped rather than turned into
		// empty segments: an empty segment becomes "//" in the built URL, and
		// the apiserver does not route that to the same place.
		{"/pods//containers/", []string{"pods", "containers"}, url.Values{}},
	}
	for _, tc := range cases {
		segments, query, err := splitProxyPath(tc.path)
		if err != nil {
			t.Fatalf("splitProxyPath(%q) = %v", tc.path, err)
		}
		if strings.Join(segments, "/") != strings.Join(tc.segments, "/") {
			t.Errorf("splitProxyPath(%q) segments = %v, want %v", tc.path, segments, tc.segments)
		}
		if query.Encode() != tc.query.Encode() {
			t.Errorf("splitProxyPath(%q) query = %v, want %v", tc.path, query, tc.query)
		}
	}
}

// TestProxyOutcomeReportsItsEncoding is the PortForwardOutcome reasoning applied
// to a surface that meets it more often: /metrics is text and a container's
// gzip is not, and Go's JSON encoder would turn the second into U+FFFD without
// saying so. A caller must never have to guess whether a replacement character
// was in the bytes or put there by this server.
func TestProxyOutcomeReportsItsEncoding(t *testing.T) {
	ref := ProxyRef{APIVersion: "v1", Kind: "Pod", Name: "api"}

	text := &limitedWriter{limit: streamMaxOutputBytes}
	text.Write([]byte("go_goroutines 42"))
	outcome := newProxyOutcome(ref, "get", "/metrics", 200, text, 10)
	if outcome.BodyEncoding != "utf-8" || outcome.Body != "go_goroutines 42" {
		t.Errorf("text outcome = %+v, want the bytes as themselves under utf-8", outcome)
	}
	if outcome.Method != "GET" || outcome.Verb != "get" {
		t.Errorf("outcome method/verb = %q/%q, want GET/get — the method is normalised once, here", outcome.Method, outcome.Verb)
	}
	if outcome.Status != 200 || outcome.Truncated {
		t.Errorf("outcome = %+v, want status 200 and no truncation", outcome)
	}

	binary := &limitedWriter{limit: streamMaxOutputBytes}
	binary.Write([]byte{0xff, 0xfe, 0x00})
	outcome = newProxyOutcome(ref, "GET", "/blob", 200, binary, 10)
	if outcome.BodyEncoding != "base64" {
		t.Errorf("binary outcome encoding = %q, want base64", outcome.BodyEncoding)
	}
	if outcome.Body != "//4A" {
		t.Errorf("binary outcome body = %q, want the base64 of the bytes", outcome.Body)
	}
}

// TestProxyOutcomeMarksACutBody is the cap's half of the same contract: an
// answer that ended because it hit 256 KiB has to say so, because a follow-mode
// endpoint's first 256 KiB looks exactly like a complete short answer.
func TestProxyOutcomeMarksACutBody(t *testing.T) {
	body := &limitedWriter{limit: 8}
	body.Write([]byte("0123456789"))
	outcome := newProxyOutcome(ProxyRef{Name: "api"}, "GET", "/logs", 200, body, 10)
	if !outcome.Truncated {
		t.Error("truncated = false, want true — a cut answer that does not say so is a lie about how much was said")
	}
	if outcome.Body != "01234567" {
		t.Errorf("body = %q, want only what fit under the cap", outcome.Body)
	}
}

// TestApiserverRefusalIsToldFromTheTargets is AC18 on the one surface where the
// two are genuinely confusable. A 403 from the apiserver is this server's grant
// being absent and must be said plainly; a 403 from whatever is being proxied to
// is an answer the caller asked for and got. The body is what separates them —
// the apiserver's is a JSON Status, the target's is whatever it serves.
func TestApiserverRefusalIsToldFromTheTargets(t *testing.T) {
	forbidden := &http.Response{StatusCode: http.StatusForbidden}

	status := []byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","code":403}`)
	err := apiserverRefusal(forbidden, status, "get", "nodes")
	if err == nil {
		t.Fatal("apiserverRefusal = nil for an apiserver Status, want AC18's explicit refusal")
	}
	for _, want := range []string{"no (get, nodes/proxy) grant", "retrying will not change the answer"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal = %q, want it to mention %q", err.Error(), want)
		}
	}

	if err := apiserverRefusal(forbidden, []byte("<html>Forbidden</html>"), "get", "pods"); err != nil {
		t.Errorf("apiserverRefusal = %v for the target's own 403, want it passed through as an answer", err)
	}
	ok := &http.Response{StatusCode: http.StatusOK}
	if err := apiserverRefusal(ok, status, "get", "pods"); err != nil {
		t.Errorf("apiserverRefusal = %v for a 200, want nil", err)
	}
}

func TestProxyTargetNameCarriesThePort(t *testing.T) {
	port := "9090"
	if got := proxyTargetName(ProxyRef{Name: "api", Port: &port}); got != "api:9090" {
		t.Errorf("proxyTargetName = %q, want \"api:9090\"", got)
	}
	if got := proxyTargetName(ProxyRef{Name: "api"}); got != "api" {
		t.Errorf("proxyTargetName = %q, want \"api\"", got)
	}
}
