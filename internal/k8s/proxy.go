package k8s

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"k8s.io/client-go/rest"
)

// ProxyDefaultReadSeconds and ProxyMaxReadSeconds bound the half of this call
// nothing else bounds. The three other stream tools cap a window because a
// command or a port never has to stop talking; proxy caps one because the
// endpoint it reaches may be a log follower that is *designed* not to
// (`/containerLogs?follow=true` is reachable, by AC15's choice not to keep a
// path allowlist). They are exported for the reason PortForwardDefaultReadSeconds
// is: the tool layer refuses an out-of-range window before the cluster is
// reached, and a second copy of the numbers is a second place for them to drift.
const (
	ProxyDefaultReadSeconds = 10
	ProxyMaxReadSeconds     = 30
)

// proxyKinds are the three kinds AC15 names. Nothing beside it filters paths,
// and that absence is the contract rather than an omission.
var proxyKinds = map[string]bool{"pods": true, "services": true, "nodes": true}

// ProxyResource reaches one object's HTTP endpoint through the apiserver's proxy
// subresource (prd-resource-generic AC15). The verb it exercises follows the HTTP
// method, and the path travels verbatim — including kubelet's high-power
// endpoints on nodes/proxy, which the gate marks rather than blocks
// (prd-approval-gate AC3).
//
// The round trip is made over the rest config's own transport rather than
// through rest.Request's Do or Stream, and the reason is that neither of those
// can answer this tool's two questions at once. Do buffers the whole body before
// anyone can look at it, which a follow-mode log endpoint never lets finish;
// Stream caps at the source but turns any non-2xx into an error, and a 404 from
// the thing being proxied to is this tool's *answer*, not its failure. Building
// the request URL with rest.Request and then carrying it over the transport
// keeps the capped read and the real status code together.
func (s *KubeService) ProxyResource(ctx context.Context, ref ProxyRef, method, path string, body []byte, contentType string, readSeconds int) (*ProxyOutcome, error) {
	if readSeconds <= 0 {
		readSeconds = ProxyDefaultReadSeconds
	}
	if readSeconds > ProxyMaxReadSeconds {
		return nil, apiErrorf("readSeconds must be at most %d (prd-resource-generic AC15)", ProxyMaxReadSeconds)
	}

	res, err := s.resolve(ctx, ref.APIVersion, ref.Kind)
	if err != nil {
		return nil, err
	}
	namespace, err := objectNamespace(res, ref.Kind, ref.Namespace)
	if err != nil {
		return nil, err
	}
	if res.gvr.Group != "" || !proxyKinds[res.gvr.Resource] {
		return nil, apiErrorf(
			"%s does not serve a proxy this layer can run: AC15 takes Pod, Service and Node (prd-resource-generic AC15)",
			ref.Kind,
		)
	}
	if err := s.streamSubresourceServed(ctx, res.gvr, ref.Kind, "proxy"); err != nil {
		return nil, err
	}

	verb := ProxyVerbForMethod(method)
	if verb == "" {
		return nil, apiErrorf("%s is not an HTTP method this tool proxies (prd-resource-generic AC15)", method)
	}

	req := s.clientset.CoreV1().RESTClient().Verb(method).
		Resource(res.gvr.Resource).
		Name(ref.Name).
		SubResource("proxy")
	if namespace != "" {
		req = req.Namespace(namespace)
	}
	segments, query, err := splitProxyPath(path)
	if err != nil {
		return nil, APIError(err.Error())
	}
	if len(segments) > 0 {
		req = req.Suffix(segments...)
	}
	for key, values := range query {
		for _, value := range values {
			req = req.Param(key, value)
		}
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(readSeconds)*time.Second)
	defer cancel()

	transport, err := rest.TransportFor(s.config)
	if err != nil {
		return nil, APIError(err.Error())
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, req.URL().String(), bytes.NewReader(body))
	if err != nil {
		return nil, APIError(err.Error())
	}
	if len(body) > 0 && contentType != "" {
		httpReq.Header.Set("Content-Type", contentType)
	}

	resp, err := (&http.Client{Transport: transport}).Do(httpReq)
	if err != nil {
		return nil, APIError(err.Error())
	}
	defer resp.Body.Close()

	// The cap has to stop the reader, not just the buffer: a follow-mode
	// endpoint keeps writing, and a copy that only discarded the overflow would
	// spend the whole window doing it. onFull drops the connection instead.
	//
	// A read cut short by either is an ending rather than a failure, so the
	// copy's error is not one: what arrived before the cut is the answer, and
	// Truncated on the outcome says it was cut.
	buf := &limitedWriter{limit: streamMaxOutputBytes, onFull: cancel}
	_, _ = io.Copy(buf, resp.Body)

	if refusal := apiserverRefusal(resp, buf.Bytes(), verb, res.gvr.Resource); refusal != nil {
		return nil, refusal
	}

	return newProxyOutcome(ref, method, path, resp.StatusCode, buf, readSeconds), nil
}

// ProxyVerbForMethod is AC15's method → verb map, and the single place it is
// written. The tool layer needs it before the cluster is touched (the gate has
// to name a pair to ask about) and this layer needs it to word a 403, so a copy
// on either side would be a copy that can drift from the declaration an operator
// approved.
func ProxyVerbForMethod(method string) string {
	switch strings.ToUpper(method) {
	case http.MethodGet:
		return "get"
	case http.MethodPost:
		return "create"
	case http.MethodPut:
		return "update"
	case http.MethodPatch:
		return "patch"
	case http.MethodDelete:
		return "delete"
	}
	return ""
}

// splitProxyPath separates the path from its query so each half can be handed
// to rest.Request the way that half is spelled. Passing the whole string as one
// suffix segment would escape the "?" and hand the target a path with a literal
// question mark in it — scenario 15 reaches `/exec/⟨ns⟩/⟨pod⟩/⟨c⟩?command=id`,
// so that difference is the difference between running the command and asking
// kubelet for a file that does not exist.
func splitProxyPath(path string) ([]string, url.Values, error) {
	parsed, err := url.Parse(path)
	if err != nil {
		return nil, nil, fmt.Errorf("path %q cannot be parsed: %v", path, err)
	}
	var segments []string
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	return segments, parsed.Query(), nil
}

// apiserverRefusal tells the apiserver saying no from the proxied target saying
// no. Both arrive as a 403 over the same connection, and they are different
// facts: the first is this server's grant (AC18 asks for that to be said plainly
// rather than passed through), the second is an answer the caller asked for and
// got. The apiserver's refusal is a JSON `Status`; a target that happens to
// answer 403 is whatever it serves, so the body is what distinguishes them.
func apiserverRefusal(resp *http.Response, body []byte, verb, resource string) error {
	if resp.StatusCode != http.StatusForbidden {
		return nil
	}
	var status struct {
		Kind       string `json:"kind"`
		APIVersion string `json:"apiVersion"`
	}
	if err := json.Unmarshal(body, &status); err != nil || status.Kind != "Status" {
		return nil
	}
	return apiErrorf(
		"refused: this server has no (%s, %s/proxy) grant; retrying will not change the answer",
		verb, resource,
	)
}

// newProxyOutcome decides how the answer travels, for the reason
// newPortForwardOutcome does: a proxy reaches whatever the object serves, and
// plenty of that is not UTF-8 (`/metrics` is, a container's gzip is not). Go's
// JSON encoder replaces invalid bytes with U+FFFD without saying so, so valid
// UTF-8 travels as itself, anything else travels base64-encoded, and the
// response says which.
func newProxyOutcome(ref ProxyRef, method, path string, status int, body *limitedWriter, readSeconds int) *ProxyOutcome {
	raw := body.Bytes()
	outcome := &ProxyOutcome{
		Method:      strings.ToUpper(method),
		Path:        path,
		Verb:        ProxyVerbForMethod(method),
		Name:        ref.Name,
		Status:      status,
		Truncated:   body.full,
		ReadSeconds: readSeconds,
	}
	if utf8.Valid(raw) {
		outcome.Body = string(raw)
		outcome.BodyEncoding = "utf-8"
		return outcome
	}
	outcome.Body = base64.StdEncoding.EncodeToString(raw)
	outcome.BodyEncoding = "base64"
	return outcome
}
