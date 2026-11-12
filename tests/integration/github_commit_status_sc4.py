"""Deployed-server e2e for github-commit-status/AC4 (context 네임스페이스 밖은 요청하지 못한다).

검증 시나리오: test-github-commit-status.md#시나리오 4
실행 대상: primary
병렬 레인: github-app

One file, two deployments (the platform_auth_safety_sc8.py shape) — why that
variant exists and why no other one can stand in is in the header of
tests/k8s/kind/commit-status-variant.yaml. The refusals are measured by what
reaches PocketAide: a request carrying this run's marker must not appear in the
primary user's commit status requests.
"""

from __future__ import annotations

import asyncio
import subprocess
import uuid

from mcp import ClientSession

from _approval_gate import approval_url, commit_status_requests, reject_commit_status_request
from _auth_variant import API_KEY
from _helpers import base_url, open_session, port_forward, wait_for_healthz

TOOL = "github_commit_status_request"

# Must match the kind overlay's patch (tests/k8s/kind/kustomization.yaml).
ALLOWED_PREFIX = "homelab-k3s-mcp/"
CONTEXT_PREFIXES_ENV = "GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES"

VARIANT_NAMESPACE = "homelab-k3s-mcp-commit-status-variant"
VARIANT_DEPLOYMENT = "homelab-k3s-mcp-commit-status-variant"
VARIANT_SERVICE = VARIANT_DEPLOYMENT
# Clear of ci.yml's group forwards (8080·8088·8089·8090·8092·8093) and of the
# ports other lanes open while this one runs (_approval_gate 8096,
# _session_platform 18083, platform_auth_safety 18080-18082).
VARIANT_LOCAL_PORT = 18084

FOREIGN_CONTEXT = "ci/build"
OWN_CONTEXT = "homelab-k3s-mcp/e2e"


def arguments(marker: str, context: str) -> dict:
    return {
        "repository": "dlddu/test",
        "sha": "0123456789abcdef0123456789abcdef01234567",
        "state": "success",
        "context": context,
        "requester": f"e2e 하네스 {marker}",
    }


def marked(url: str, marker: str) -> list[dict]:
    return [r for r in commit_status_requests(url) if marker in r["requester"]]


def refusal_text(result) -> str:
    assert result.isError is True, f"call did not refuse: {result}"
    assert result.content, result
    block = result.content[0]
    assert block.type == "text", block
    return block.text


def available_replicas(namespace: str, deployment: str) -> int:
    proc = subprocess.run(
        [
            "kubectl", "-n", namespace, "get", f"deploy/{deployment}",
            "-o", "jsonpath={.status.availableReplicas}",
        ],
        capture_output=True,
        text=True,
        timeout=60,
    )
    assert proc.returncode == 0, (
        f"kubectl get deploy/{deployment} failed ({proc.returncode}): {proc.stderr.strip()}"
    )
    return int(proc.stdout.strip() or 0)


async def test_github_commit_status_ac4_foreign_context_is_refused(
    session: ClientSession,
) -> None:
    """The own-prefix call right after the refusal is the positive control that
    the guard is a prefix check and not a blanket refusal — it must actually
    reach PocketAide, from this same deployment, in this same session.
    """
    marker = uuid.uuid4().hex[:12]
    refused = await session.call_tool(TOOL, arguments(marker, FOREIGN_CONTEXT))
    text = refusal_text(refused)
    assert f'context "{FOREIGN_CONTEXT}" is outside the allowed prefixes' in text, text
    assert ALLOWED_PREFIX in text, text

    with approval_url() as url:
        assert marked(url, marker) == [], f"a refused context still reached PocketAide: {marked(url, marker)}"
        accepted = await session.call_tool(TOOL, arguments(marker, OWN_CONTEXT))
        assert accepted.isError is False, accepted
        rows = marked(url, marker)
        assert len(rows) == 1 and rows[0]["context"] == OWN_CONTEXT, f"the own-prefix context was not filed: {rows}"
        reject_commit_status_request(url, rows[0]["id"])


async def test_github_commit_status_ac4_unconfigured_prefix_refuses_everything() -> None:
    replicas = available_replicas(VARIANT_NAMESPACE, VARIANT_DEPLOYMENT)
    assert replicas >= 1, (
        f"deploy/{VARIANT_DEPLOYMENT} in {VARIANT_NAMESPACE} has {replicas} available replicas"
    )

    with port_forward(
        VARIANT_NAMESPACE, VARIANT_SERVICE, 80, VARIANT_LOCAL_PORT, ready_path="/healthz"
    ) as url:
        wait_for_healthz(url)
        async with open_session(
            url, headers={"Authorization": f"Bearer {API_KEY}"}
        ) as session:
            result = await session.call_tool(TOOL, arguments(uuid.uuid4().hex[:12], OWN_CONTEXT))
            text = refusal_text(result)
            assert "no commit status context prefix is configured" in text, text
            assert CONTEXT_PREFIXES_ENV in text, text


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    print("--- github-commit-status/AC4 ---")
    async with open_session(url) as session:
        await test_github_commit_status_ac4_foreign_context_is_refused(session)
    await test_github_commit_status_ac4_unconfigured_prefix_refuses_everything()
    print("ok: github-commit-status/AC4")


if __name__ == "__main__":
    asyncio.run(run())
