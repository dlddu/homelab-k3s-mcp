# 2026-09-15 — resource_watch documentation overlap

Task: `rct_20260915-0007` in `tbm_homelab-k3s-mcp-comment-redundancy`.
Ledger rows: line comments #24 and #25.
Source reviewed: `877d854087d7eb824f0a639fcad56a8519e5c5c6` (main).

## Decision and scope

Remove the four comment bodies identified by the detector. All four have a
concrete recovery path in repository documentation (policy path ②). A rationale
can explain something that the code alone cannot recover and still be redundant
when the same rationale already exists in a document. The ledger's integrity
gate does not establish semantic nonredundancy.

The implementation added 74 counted comment lines. This pass removes **19**:
18 explanatory lines and one empty `//` paragraph separator. It does not classify
the other 55 added lines, earlier comments, or the repository as a whole as
nonredundant. Their prior decisions remain in the ledger; unique or ambiguous
knowledge is retained unless a concrete recovery path has been established.

## Removal evidence

Line references below are pinned to the reviewed commit, so later edits do not
move the evidence. The design decisions remain in [doc-tracker](../../doc-tracker/index.md).

| Comment body | Removed lines | Existing recovery source and matching knowledge |
| --- | ---: | --- |
| `WatchMaxEvents`, `internal/k8s/resource.go:54–59` | 6 | [doc-tracker:297–301](https://github.com/dlddu/homelab-k3s-mcp/blob/877d854087d7eb824f0a639fcad56a8519e5c5c6/docs/doc-tracker.md#L297) records the implementation's choice of 100, the whole-object/context-size rationale, and explicit truncation reporting. |
| `WatchResources`, `internal/k8s/resource.go:501–506` | 6 | [doc-tracker:295–297](https://github.com/dlddu/homelab-k3s-mcp/blob/877d854087d7eb824f0a639fcad56a8519e5c5c6/docs/doc-tracker.md#L295) records the apiserver window, local timer, and uncooperative-proxy rationale. The count includes the empty paragraph separator. |
| Watch event `stripNoise` call, `internal/k8s/resource.go:586–589` | 4 | [doc-tracker:301–305](https://github.com/dlddu/homelab-k3s-mcp/blob/877d854087d7eb824f0a639fcad56a8519e5c5c6/docs/doc-tracker.md#L301) records why both fields remain noise across verbs and why a hundred whole objects makes this matter. |
| `resource_watch` registry entry, `internal/mcp/gate.go:90–92` | 3 | [doc-tracker:286–294](https://github.com/dlddu/homelab-k3s-mcp/blob/877d854087d7eb824f0a639fcad56a8519e5c5c6/docs/doc-tracker.md#L286) records the existing sensitive-read gate, whole-object exposure, and why registration needs no new gate rule. |

Row #25's earlier assertion that the value's rationale exists only in the comment
is superseded for this body: the source document already records both the choice
and its rationale. The retention decision for the named bodies in rows #24/#25
is superseded by this pass. Previous decisions remain as dated history.

## Preserved material

- The original first documentation lines for `WatchMaxEvents` and
  `WatchResources` remain verbatim, including their AC6 pointers.
- Machine-readable directives, executable Go content, declarations, constants,
  test code, test comments, and Python docstrings are unchanged.
- The watch Error-event partial-window rationale and test counterexample/control
  explanations are outside this four-block decision and remain in place.
- The repository policy and the source design decisions are unchanged. No new
  redundancy judgment is implied for untouched ledger ranges.

## Accounting and verification

At this source snapshot, `internal/k8s/resource.go` loses 16 counted lines and
`internal/mcp/gate.go` loses 3. The paired test files remain unchanged. Row #25
therefore changes 222 → 206 and row #24 changes 185 → 182. The line-comment total
changes 1722 → 1703; the 83 comment-bearing files, 34 registered ranges, and zero
remainder remain. Docstrings remain 1207 lines in 64 files / 15 registered ranges,
with zero remainder and fingerprint
`09cfb64b5ccfaac7625823a50deb7d009cf541a60490aab994cf43adf56d8efd`.

The new pass is linked from the ledger. Document inventory increases from 40 to
41 documents (policy documents 10 → 11), with all documents reachable and no
broken links. These are snapshot counts; subsequent changes must be remeasured.

### Execution rebase accounting

Main advanced to `5197ed32f98f56d305a98bba7038c255f32300af` before execution.
The approved rebase preserves the later approval-gate changes and every prior
ledger decision, with this four-body correction prepended to rows #24/#25.
The same 19 comment deletions now change row #24 from 342 to **339**
(`42ec749fc39c`), row #25 from 229 to **213** (`22ff4ebefe26`), and the total
from 2043 to **2024** across 85 comment-bearing files and 35 registered ranges.
The line-comment fingerprint is
`49db0ea22cc270a43df154c71101d9c472386aabf1519783f3c4a3a2f805c58e`.
Docstrings, both zero-remainder declarations, and the 40-to-41 document delta
are unchanged. The recovery paragraphs at the immutable source links above
also survive verbatim on this main revision.

Validation commands:

```bash
python3 scripts/check_comment_policy.py
python3 scripts/check_doc_inventory.py
python3 tests/integration/check_ac_mapping.py
python3 scripts/check_mock_policy.py
git diff --check
```

Also inspect the Go diff against the reviewed base: it must contain only the
19 full-line comment deletions above. Non-comment content and protected first
documentation lines must remain identical. The existing PR CI supplies Go
format/build/vet, unit, and integration checks before merge.
