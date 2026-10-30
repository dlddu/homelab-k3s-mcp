package opensearch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	sdkconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// AuthMode names how requests to the backend are authenticated. It is chosen
// by configuration alone, so moving between backends (docstore in the cluster,
// a self-hosted OpenSearch, an AWS-managed domain or collection) never needs a
// code change.
type AuthMode string

const (
	// AuthNone sends plain requests (docstore, or OpenSearch with security off).
	AuthNone AuthMode = "none"
	// AuthBasic sends HTTP basic auth (OpenSearch security plugin).
	AuthBasic AuthMode = "basic"
	// AuthSigV4 signs with short-lived credentials from STS AssumeRole
	// (AWS OpenSearch Service domain "es" or Serverless collection "aoss").
	AuthSigV4 AuthMode = "sigv4"
)

const (
	defaultRoleSessionName = "homelab-k3s-mcp"
	defaultSigningService  = "es"
)

type authenticator interface {
	mode() AuthMode
	apply(ctx context.Context, req *http.Request, body []byte) *Error
}

type noAuth struct{}

func (noAuth) mode() AuthMode                                      { return AuthNone }
func (noAuth) apply(context.Context, *http.Request, []byte) *Error { return nil }

type basicAuth struct{ user, pass string }

func (basicAuth) mode() AuthMode { return AuthBasic }
func (b basicAuth) apply(_ context.Context, req *http.Request, _ []byte) *Error {
	req.SetBasicAuth(b.user, b.pass)
	return nil
}

type sigv4Auth struct {
	region  string
	service string
	creds   aws.CredentialsProvider
	signer  *v4.Signer
}

func (sigv4Auth) mode() AuthMode { return AuthSigV4 }
func (s sigv4Auth) apply(ctx context.Context, req *http.Request, body []byte) *Error {
	sum := sha256.Sum256(body)
	payloadHash := hex.EncodeToString(sum[:])
	// OpenSearch Serverless requires the payload hash as an explicit header;
	// managed domains accept it too.
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	creds, err := s.creds.Retrieve(ctx)
	if err != nil {
		return requestError("assume role: %v", err)
	}
	if err := s.signer.SignHTTP(ctx, creds, req, payloadHash, s.service, s.region, time.Now()); err != nil {
		return requestError("sign request: %v", err)
	}
	return nil
}

// authFromEnv picks the authentication mode from the environment:
//
//   - OPENSEARCH_USERNAME + OPENSEARCH_PASSWORD → basic
//   - OPENSEARCH_ROLE_ARN → SigV4 with AssumeRole credentials. Base credentials
//     come from the default AWS chain; OPENSEARCH_REGION sets the region when
//     the chain has none, OPENSEARCH_SIGNING_SERVICE is "es" (default) or
//     "aoss", OPENSEARCH_STS_ENDPOINT and OPENSEARCH_ROLE_SESSION_NAME are
//     optional.
//   - neither → none
//
// Setting both, or only one of the basic pair, is a configuration error.
func authFromEnv(ctx context.Context) (authenticator, error) {
	user, pass := os.Getenv("OPENSEARCH_USERNAME"), os.Getenv("OPENSEARCH_PASSWORD")
	roleARN := os.Getenv("OPENSEARCH_ROLE_ARN")

	if (user == "") != (pass == "") {
		return nil, fmt.Errorf("OPENSEARCH_USERNAME and OPENSEARCH_PASSWORD must be set together")
	}
	if user != "" && roleARN != "" {
		return nil, fmt.Errorf("set either OPENSEARCH_USERNAME/OPENSEARCH_PASSWORD (basic) or OPENSEARCH_ROLE_ARN (SigV4), not both")
	}
	switch {
	case user != "":
		return basicAuth{user: user, pass: pass}, nil
	case roleARN != "":
		return sigv4FromEnv(ctx, roleARN)
	default:
		return noAuth{}, nil
	}
}

func sigv4FromEnv(ctx context.Context, roleARN string) (authenticator, error) {
	service := os.Getenv("OPENSEARCH_SIGNING_SERVICE")
	if service == "" {
		service = defaultSigningService
	}
	if service != "es" && service != "aoss" {
		return nil, fmt.Errorf(`OPENSEARCH_SIGNING_SERVICE must be "es" or "aoss", got %q`, service)
	}

	var loadOpts []func(*sdkconfig.LoadOptions) error
	if region := os.Getenv("OPENSEARCH_REGION"); region != "" {
		loadOpts = append(loadOpts, sdkconfig.WithRegion(region))
	}
	baseCfg, err := sdkconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	if baseCfg.Region == "" {
		return nil, fmt.Errorf("no AWS region resolved: set OPENSEARCH_REGION when OPENSEARCH_ROLE_ARN is set")
	}

	sessionName := os.Getenv("OPENSEARCH_ROLE_SESSION_NAME")
	if sessionName == "" {
		sessionName = defaultRoleSessionName
	}
	stsEndpoint := os.Getenv("OPENSEARCH_STS_ENDPOINT")
	stsClient := sts.NewFromConfig(baseCfg, func(o *sts.Options) {
		if stsEndpoint != "" {
			o.BaseEndpoint = aws.String(stsEndpoint)
		}
	})
	provider := stscreds.NewAssumeRoleProvider(stsClient, roleARN, func(o *stscreds.AssumeRoleOptions) {
		o.RoleSessionName = sessionName
	})
	return sigv4Auth{
		region:  baseCfg.Region,
		service: service,
		creds:   aws.NewCredentialsCache(provider),
		signer:  v4.NewSigner(),
	}, nil
}
