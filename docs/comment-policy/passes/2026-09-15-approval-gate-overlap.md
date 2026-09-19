# 2026-09-15 — approval-gate documentation overlap

Task: `rct_20260915-0008` in `tbm_homelab-k3s-mcp-comment-redundancy`.
Ledger rows: line comments #24 and #34.
Source reviewed: `be2aff078ba1424c440b2b15209f83a5808017ee` (main).

## Decision and scope

Remove the four explanatory bodies identified by the detector. Each has a
concrete recovery source in repository documentation (policy path ②), including
the reason for the decision. Being difficult to recover from executable code
alone does not justify keeping a rationale already present in the design log.

This pass removes **29 counted lines**: 25 explanatory lines and four empty
`//` paragraph separators. It revises only the decisions for these four bodies.
It does not reclassify the rest of approval-gate PR #104's additions, untouched
ledger ranges, or the repository as a whole. The earlier resource_watch pass
and all prior ledger decision text remain as history.

## Removal evidence

All line references below are pinned to the reviewed source. The design
decisions remain in [doc-tracker](../../doc-tracker/index.md).

| Comment body | Removed lines | Existing recovery source and matching knowledge |
| --- | ---: | --- |
| `TargetReader`, `internal/k8s/precondition.go:79–86` | 8 | [doc-tracker:405–415](https://github.com/dlddu/homelab-k3s-mcp/blob/be2aff078ba1424c440b2b15209f83a5808017ee/docs/doc-tracker.md#L427) records why the gate uses a separate interface, the conflict between AC11's pre-approval reads and AC1/AC16's zero tool-call count, and the rejected alternative of changing the existing service/AC contract. |
| `toolDeclaration.target` read exception, `internal/mcp/gate.go:55–62` | 8 | [doc-tracker:452–462](https://github.com/dlddu/homelab-k3s-mcp/blob/be2aff078ba1424c440b2b15209f83a5808017ee/docs/doc-tracker.md#L474) records why a sensitive collection watch has no single-object precondition, why AC3 needs only coordinates, and why rejecting it would invent an undocumented classifier. |
| `GatePairs` list paragraph, `internal/mcp/gate.go:414–419` | 6 | [doc-tracker:466–469](https://github.com/dlddu/homelab-k3s-mcp/blob/be2aff078ba1424c440b2b15209f83a5808017ee/docs/doc-tracker.md#L488) records the unused list declaration, its future deletecollection caller, and why declaration before use follows AC11 while undeclared use violates AC1. |
| `authorize` exclusions, `internal/mcp/gate.go:444–450` | 7 | [doc-tracker:458–461](https://github.com/dlddu/homelab-k3s-mcp/blob/be2aff078ba1424c440b2b15209f83a5808017ee/docs/doc-tracker.md#L481) records both exclusions: reads use coordinates without a single collection object; constant-pair tools declare no per-call coordinate, and the named writer addresses pods by selector. |

The first row supersedes row #34's retention of the type-separation rationale.
The other three supersede only the corresponding retention decisions in row
#24. The correction is prepended to each ledger row; historical text is intact.

## Preserved material

- The original `TargetReader` first documentation line and all remaining
  `GatePairs` documentation stay verbatim. No exported first documentation line,
  package documentation, or machine-readable directive is removed.
- The earlier parts of `target`'s comment (separate timing and refusal of an
  undescribed write) and `authorize`'s fail-closed paragraph remain. This pass
  makes no new redundancy judgment about them.
- Metadata-negotiation caveats, test counterexamples, and all other unique or
  ambiguous knowledge are outside this bounded decision and stay in place.
- Every non-comment Go byte, all tests, Python docstrings, fixtures, PRDs,
  policy rules and workflows are unchanged. The only design-log edit updates
  its machine-checked document inventory for this new pass.

## Accounting and verification

| Surface | Reviewed base | Prepared result |
| --- | ---: | ---: |
| Row #24 (`gate.go` and its test) | 335 | 314 (`97744fe4ec47`) |
| Row #34 (`precondition.go` and its test) | 111 | 103 (`4ea5b103f043`) |
| All counted line comments | 2005 | 1976 |
| Comment-bearing files / registered ranges | 87 / 38 | 87 / 38 |
| Docstring lines / files / ranges | 1210 / 65 / 15 | 1210 / 65 / 15 |
| Reachable documents / policy documents | 41 / 11 | 42 / 12 |

Both inventory remainders stay zero. Main advanced between the reviewed
base (`5b49837`) and this snapshot (`be2aff0`) with PR #109 and a deployment
pin; the branch was rebased onto `be2aff0`, the ledger conflict was resolved
by stacking this correction's re-judgment on main's merged remeasure, and
the two rows plus the total were remeasured with the repository checker. After the execution rebase onto `be2aff0` the remeasured line-comment
fingerprint is
`fe08468c877d6ce95426ca034d58d45f3846a01ef8e42b73b1f36b8340fba3dc`.
The unchanged docstring fingerprint is
`9863c1a326d05c258b5d45d96eb34d9ef14cfb8114164e29026259409f011cd8`.
The new pass is reachable through the ledger; no direct hub entry is necessary.
These counts describe this source snapshot and must be remeasured after any
rebase that changes a counted surface.

Validation commands:

```bash
python3 scripts/check_comment_policy.py
python3 scripts/check_doc_inventory.py
python3 tests/integration/check_ac_mapping.py
python3 scripts/check_mock_policy.py
git diff --check
```

Also compare the Go diff with the reviewed base: it must consist of exactly
the 29 full-line comment deletions listed above. Non-comment bytes and protected
documentation must remain identical. Verify that only the two named ledger
rows and the total changed, with each previous decision preserved verbatim.
The existing PR CI supplies Go format/build/vet, unit and integration checks.

## Handoff and rollback

The executor may merge this PR only after the separate review workflow makes
the task `approved`. If main changes an affected row or comment, refresh the
evidence and measurements without replacing a sibling worker's changes. A
semantic conflict needs renewed planning/review. No manual deployment is part
of this correction. Merging anything to `main` triggers
`.github/workflows/docker-build-push.yaml`: it publishes a new server image
and directly commits the image SHA pin into `k8s/deployment.yaml` on `main`,
and the deployment's rollout restarts the MCP service pods. Rollback is a
separate revert PR for this change, with ledger and document inventory
remeasured against then-current main; after merging the revert, confirm the
workflow published a replacement image, that `k8s/deployment.yaml` carries the
revert's image SHA pin, and that the rollout came up serving traffic
(`kubectl rollout status` and a gate smoke check) before declaring recovery.
