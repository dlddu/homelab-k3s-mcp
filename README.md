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
  Merges that touch neither the server's Go sources nor `k8s/` (only `docs/`, `scripts/`,
  `tests/`, `*_test.go`, Markdown or other workflows) skip this workflow, so production is not
  restarted; such a commit never reaches `deploy`, and its tip's `Source-Commit` trailer
  still names what production runs.
- **Tests run on pull requests only.** `.github/workflows/ci.yml` is triggered by
  `pull_request` alone; what lands on `main` has already passed `ci passed`, so a `main`
  push runs only the image publish + pin above. A `changes` job skips the unit tests when
  no Go source changed, and the integration tests when only `docs/`, `scripts/` or
  Markdown changed, and the PR image publish (below) under the same exclusions as the
  `main` path filter; `ci passed` accepts a skip only where `changes` decided it.
- **Pull requests get a preview environment.** The same image workflow, called from
  `ci.yml`, publishes the PR's head SHA on PR pushes that touch the image (the same
  exclusions as the `main` path filter above). A PR that does not touch the image has no
  image at its head SHA, so `deploy/preview` brings no environment up for it. Label a PR
  `deploy/preview` and flux-cd-apps
  (`apps/homelab-k3s-mcp-preview`) renders a full environment for it at
  `homelab-k3s-mcp-pr-<number>.<private domain>`, pinned to that head SHA. Removing
  the label, closing, or merging the PR tears the environment down. Previews carry a
  copy of production's credential Secrets, so verify against them with an API key from
  `MCP_API_KEYS` rather than the OAuth flow.

`ci.yml` builds the image too, but only to load it into kind for the integration
tests — it never pushes.

## Authentication

The `/mcp` endpoint is protected by default. Three credential paths gate it and
they compose — configure any combination, but at least one must be present:

| Path | Env | For |
|------|-----|-----|
| OAuth 2.0 Bearer (RS256 JWT, JWKS-verified) | `MCP_OAUTH_ISSUER`, `MCP_OAUTH_AUDIENCE`, `MCP_OAUTH_RESOURCE` | Interactive MCP clients that run the OAuth flow |
| Static API keys | `MCP_API_KEYS` | Non-interactive automation that cannot run OAuth |
| GitHub Actions OIDC (RS256 JWT, JWKS-verified) | `MCP_GITHUB_OIDC_AUDIENCE`, `MCP_GITHUB_OIDC_RULES` (and/or `MCP_GITHUB_OIDC_RULES_*`), optional `MCP_GITHUB_OIDC_ISSUER` | Workflows in allowed repositories, each limited to its own tools |

A request presents its credential as `Authorization: Bearer <credential>`. The
middleware matches the value against the configured API keys first (constant-time
comparison). On no match, a JWT whose `iss` is the GitHub Actions issuer goes to
GitHub OIDC verification and any other JWT to OAuth verification. Any success
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

### GitHub Actions OIDC (`MCP_GITHUB_OIDC_*`)

A workflow asks GitHub for a short-lived OIDC token and presents it as the
bearer. The server verifies the signature against the issuer's JWKS, checks
`iss`, `aud` and `exp`, and admits the token only when a rule in
`MCP_GITHUB_OIDC_RULES` matches it. There is no secret to store or rotate.

Rules may be split across `MCP_GITHUB_OIDC_RULES` and any number of
`MCP_GITHUB_OIDC_RULES_<SUFFIX>` variables, each a JSON array of rules; the server
reads the unsuffixed one first, then the others in name order, and treats the
result as one list. Splitting exists because one SSM parameter holds at most
4 KB. A rule repeated across variables stops the server at startup, like one
repeated within a variable; an empty variable is ignored.

A rule names a repository (`repository` claim, `owner/name`, case-insensitive)
and optionally a workflow file (the path in the `workflow_ref` claim, any ref).
A rule without `workflow` matches every workflow in that repository. When
several rules match one token, their tools are combined:

```json
[
  {"repository": "dlddu/reconciler", "workflow": ".github/workflows/reconcile.yml",
   "tools": ["github_commit_status_create"]},
  {"repository": "dlddu/reconciler", "tools": ["opensearch_search"]}
]
```

Here `reconcile.yml` may use both tools and any other workflow in
`dlddu/reconciler` only `opensearch_search`. The workflow display name (`name:`)
is not used: several files may share it.

A rule that grants `github_app_installation_token` may also cap the tokens it
lets a workflow mint with `githubToken` — the repositories (names without owner)
and the highest level of each permission (`read` < `write` < `admin`):

```json
[
  {"repository": "dlddu/reconciler", "workflow": ".github/workflows/reconciler-drift-detector.yml",
   "tools": ["github_app_installation_token", "resource_list", "resource_get"],
   "githubToken": {"repositories": ["plugin-marketplace", "economic-opinion-trend-monitor"],
                   "permissions": {"contents": "read", "metadata": "read"}}}
]
```

A capped caller that omits `repositories` or `permissions` gets the cap, not the
App's whole installation, and asking for a repository, a permission or a level
outside the cap is refused (`-32602`) without minting anything.

`githubToken` may also be an array of caps, when a workflow needs different
permissions on different repositories — say write on the repositories it
changes and read on the one it installs plugins from:

```json
"githubToken": [
  {"repositories": ["pocket-aide", "feature-doc"], "permissions": {"contents": "write", "pull_requests": "write", "metadata": "read"}},
  {"repositories": ["plugin-marketplace"], "permissions": {"contents": "read", "metadata": "read"}}
]
```

A request must then fit **one** cap whole: its repositories and permissions are
checked against each cap in turn, never mixed across them, so the example cannot
mint write on `plugin-marketplace` or one token for `pocket-aide` and
`plugin-marketplace` together. An omitted `repositories` or `permissions` is
filled from the cap the request fits; if it fits more than one, the request is
ambiguous and refused — name both. When several rules match a workflow, their
caps are collected the same way, each kept whole. A matching rule that grants
the tool without `githubToken` leaves that workflow unbounded.

A cap may name the repositories it leaves **out** instead, with
`excludeRepositories` (and no `repositories`): every installed repository except
those, including ones installed later. Its `permissions` may be omitted, which
leaves the level unbounded on those repositories:

```json
"githubToken": [
  {"excludeRepositories": ["reconciler", "plugin-marketplace", "infrastructure", "flux-cd", "flux-cd-apps"]},
  {"repositories": ["plugin-marketplace"], "permissions": {"contents": "read", "metadata": "read"}}
]
```

GitHub cannot be asked for "all but these", so a request fits such a cap only
when it names its repositories. An exclusion cap that could grant `workflows`
(no `permissions`, or `workflows` among them) must exclude the rule's own
repository, or the server stops at startup.

Whatever the cap, a GitHub Actions caller never gets a token that can change
its **own** repository's workflow files: a token with the `workflows`
permission — named, or implied by omitting `permissions` — that includes the
calling repository (named, or implied by omitting `repositories`) is refused.
A workflow that could push a workflow file to its own repository could write one
that reads every secret held there, including keys meant for other workflows.
Changing workflows in other repositories stays possible: name them in
`repositories` and leave the caller's out. A rule whose cap puts `workflows` on
its own repository stops the server at startup.

A caller on this path sees only its tools in `tools/list`, and calling any
other tool fails exactly like calling a tool that does not exist
(`-32602 unknown tool: <name>`). Gated tools still wait for approval. API-key
and OAuth callers are not scoped. The server refuses to start if a rule names a
tool it does not have, has no tools, has an unknown field, or repeats another
rule's repository and workflow, or if `MCP_OAUTH_ISSUER` is the same issuer.
`MCP_GITHUB_OIDC_ISSUER` defaults to `https://token.actions.githubusercontent.com`;
its discovery document and JWKS are fetched at startup, so an unreachable issuer
stops the server.

In production both values come from the Secret `homelab-k3s-mcp-github-oidc`
(an ExternalSecret in flux-cd-apps, SSM `/flux-cd/homelab-k3s-mcp/github-oidc`),
which `k8s/deployment.yaml` reads with `optional: true`; without it the path is
off. More rules come from the Secret `homelab-k3s-mcp-github-oidc-claude` (SSM
`/flux-cd/homelab-k3s-mcp/github-oidc-claude`, key `MCP_GITHUB_OIDC_RULES_CLAUDE`),
also optional.

Anyone who can push to an allowed repository can edit an allowed workflow file,
or run it from another branch, and so use its tools. Grant each rule only what
that workflow needs.

In the workflow:

```yaml
permissions:
  id-token: write
steps:
  - name: Call homelab-k3s-mcp
    run: |
      TOKEN=$(curl -sS -H "Authorization: bearer $ACTIONS_ID_TOKEN_REQUEST_TOKEN" \
        "$ACTIONS_ID_TOKEN_REQUEST_URL&audience=homelab-k3s-mcp" | jq -r .value)
      curl -sS -X POST https://mcp.example/mcp \
        -H "Authorization: Bearer $TOKEN" \
        -H "Content-Type: application/json" \
        -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

The call is recorded with the principal
`github_oidc:<owner/name>/<workflow path>`.

### Disabling auth

`MCP_AUTH_DISABLED=1` serves `/mcp` without any authentication. Use it only for
local development or trusted-network testing, never in production.

See `docs/prd-platform-auth-safety.md` (AC7–AC10) for the product requirements
behind this behavior.

## Approval gate

State-changing calls (and reads of sensitive kinds such as Secrets) wait for a
human approval in [PocketAide](https://github.com/dlddu/pocket-aide) before they
reach the cluster. The server reaches it at `APPROVAL_GATE_URL` and authenticates
with its own Kubernetes ServiceAccount token, projected by `k8s/deployment.yaml`
for the audience `pocket-aide` — there is no API key. Two things must exist on
the PocketAide side:

- the ServiceAccount `homelab-k3s-mcp/homelab-k3s-mcp` registered as a caller in
  the PocketAide app (approval gate settings → service account caller). Requests
  then belong to the user who registered it; only that user can decide them, and
  that user's auto-response mode applies;
- PocketAide's own ServiceAccount bound to `system:auth-delegator`, so it can
  verify the token with a TokenReview.

Without either, every gated call is refused (fail-closed); reads that are not
gated keep working. See `docs/prd-approval-gate.md` for the contract.

Each approval request also shows the operator why it was made, and from which
session. Every tool call that needs approval must pass a `reason` argument: one
or two sentences, sent to PocketAide as `reason`. The owner decided on
2026-10-25 that the approval screen must always show why. A gated call without
a reason is refused with an invalid-params error before any approval is
requested. Tools whose every call needs approval list `reason` as required.
`resource_get` and `resource_watch` need it only when reading a sensitive kind
such as a Secret. The dispatcher removes `reason` from the arguments before the
tool runs, so it never reaches Kubernetes. The `X-Session-Platform-Session-Id`
header on `/mcp`, when present, is sent as `sessionId`. Both values are for
display only and are never used to authorize anything.

Deploy PocketAide first, from a build that accepts these two fields
(dlddu/pocket-aide#319). An older PocketAide answers 400 to fields it does not
know, and because `reason` is required, every gated call would then be refused.

`pocket_aide_notification_send` uses the same address and token. Its
`requester` argument is required: the caller writes who is asking for the
notification and what they are working on (1 to 200 characters; empty or longer
is refused before sending). PocketAide stores it with the notification next to
the registered caller; it is not verified, and the authenticated principal stays
in this server's event log. See `docs/prd-pocket-aide-notification-send.md`
(AC5).
