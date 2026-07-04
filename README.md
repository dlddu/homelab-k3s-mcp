# homelab-k3s-mcp

An MCP (Model Context Protocol) server for operating a homelab k3s cluster and
its connected cloud resources through an AI assistant. It exposes tools over a
single HTTP `POST /mcp` endpoint.

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
