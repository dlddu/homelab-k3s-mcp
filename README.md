# homelab-k3s-mcp

An MCP (Model Context Protocol) server for operating a homelab k3s cluster and
its connected cloud resources through an AI assistant. It exposes tools over a
single HTTP `POST /mcp` endpoint.

## Documentation

Product values, per-tool PRDs, and their test documents are published at
<https://dlddu.github.io/homelab-k3s-mcp/>, served from `docs/` on `main`.
Start at the hub — it is organized by product value, and each tool row links its
PRD and test document side by side.

## Deployment

Images are published to `ghcr.io/dlddu/homelab-k3s-mcp` under **one tag only: the
commit SHA**. There is no `latest`.

- **Production is pinned.** On every push to `main`, `.github/workflows/docker-build-push.yaml`
  builds the commit, pushes `ghcr.io/dlddu/homelab-k3s-mcp:<sha>`, then force-pushes
  `main@<sha>` plus one commit pinning `k8s/deployment.yaml` to that SHA to the
  **`deploy` branch** (the `pin` job, `Source-Commit: <sha>` trailer). `main` is protected
  by a ruleset (required status check `ci passed`), so nothing commits back to it. Flux
  tracks `deploy`, so the single answer to "which commit is production running?" is the
  `image:` line in `k8s/deployment.yaml` on `deploy`. Roll back with a revert PR to `main`.
- **Tests run on pull requests only.** `.github/workflows/ci.yml` is triggered by
  `pull_request` alone; what lands on `main` has already passed `ci passed`, so a `main`
  push runs only the image publish + pin above. A `changes` job skips the unit tests when
  no Go source changed, and the integration tests when only `docs/`, `scripts/` or
  Markdown changed; `ci passed` accepts a skip only where `changes` decided it.
- **Pull requests get a preview environment.** The same image workflow, called from
  `ci.yml`, publishes the PR's head SHA on every PR push. Label a PR `deploy/preview` and flux-cd-apps
  (`apps/homelab-k3s-mcp-preview`) renders a full environment for it at
  `homelab-k3s-mcp-pr-<number>.<private domain>`, pinned to that head SHA. Removing
  the label, closing, or merging the PR tears the environment down. Previews carry a
  copy of production's credential Secrets, so verify against them with an API key from
  `MCP_API_KEYS` rather than the OAuth flow.

`ci.yml` builds the image too, but only to load it into kind for the integration
tests — it never pushes.

## Authentication

The `/mcp` endpoint is protected by default. Two credential paths gate it and
they compose — configure either or both, but at least one must be present:

| Path | Env | For |
|------|-----|-----|
| OAuth 2.0 Bearer (RS256 JWT, JWKS-verified) | `MCP_OAUTH_ISSUER`, `MCP_OAUTH_AUDIENCE`, `MCP_OAUTH_RESOURCE` | Interactive MCP clients that run the OAuth flow |
| Static API keys | `MCP_API_KEYS` | Non-interactive automation that cannot run OAuth |

A request presents its credential as `Authorization: Bearer <credential>`. The
middleware matches the value against the configured API keys first (constant-time
comparison), and on no match falls back to JWT verification. Either success
authorizes the request.

If neither path is configured (and auth is not explicitly disabled), the server
refuses to start rather than serve `/mcp` unauthenticated.

### API keys (`MCP_API_KEYS`)

Set `MCP_API_KEYS` to a comma-separated list of secrets. Surrounding whitespace
is trimmed and empty entries are dropped. Multiple keys are supported so each
client can hold its own, and any one can be revoked independently by removing it
from the list.

```sh
export MCP_API_KEYS="k-automation-1,k-ci-runner-2"
```

An automation client then calls the endpoint directly:

```sh
curl -sS -X POST https://mcp.example/mcp \
  -H "Authorization: Bearer k-automation-1" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

Notes:

- **OAuth coexistence.** When `MCP_OAUTH_*` is also set, keys and JWTs both work.
  When `MCP_OAUTH_*` is unset, the server runs in API-key-only mode: the OAuth
  discovery document (`/.well-known/oauth-protected-resource`) is not served and
  the `WWW-Authenticate` challenge advertises no `resource_metadata` — API keys
  are distributed out-of-band, not through discovery.
- **Transport.** API keys are long-lived secrets; terminate TLS at the ingress
  so they are never sent in clear text.
- **Handling.** Keys are injected via a Kubernetes Secret
  (`homelab-k3s-mcp-api-keys`, key `MCP_API_KEYS`), created out-of-band and never
  committed. Key values are never written to logs or response bodies; startup
  logs only the count of configured keys.
- **Rotation.** Add the new key, roll clients over, then drop the old key.

### Disabling auth

`MCP_AUTH_DISABLED=1` serves `/mcp` without any authentication. Use it only for
local development or trusted-network testing, never in production.

See `docs/prd-platform-auth-safety.md` (AC7, AC8) for the product requirements
behind this behavior.
