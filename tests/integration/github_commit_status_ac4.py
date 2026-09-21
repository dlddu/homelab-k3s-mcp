"""Deployed-server e2e for github-commit-status/AC4 (context 네임스페이스 밖은 거부).

검증 시나리오: test-github-commit-status.md#시나리오 4
실행 대상: primary
병렬 레인: github-app

One file, two deployments (the platform_auth_safety_ac8.py shape): (a) on the
primary the runner hands it, (b) on the prefix-less variant reached through
this file's own port-forward — why that variant exists and why no other one
can stand in is in the header of tests/k8s/kind/commit-status-variant.yaml.

The refusal *text* is what separates this AC from the two neighbours that
would also produce "tool error, nothing upstream": AC3's input validation
names the offending field, and AC5's unconfigured App says "github app
unavailable". Only the prefix guard names the allowed prefixes (a) or the env
var to set (b), and that is what each half asserts.
"""

from __future__ import annotations

import asyncio
import os
import subprocess
from typing import Any

import httpx
from mcp import ClientSession

from _auth_variant import API_KEY
from _helpers import base_url, open_session, port_forward, wait_for_healthz

MOCK_URL = os.environ.get("GITHUB_MOCK_URL", "http://127.0.0.1:8093").rstrip("/")

# Must match the kind overlay's patch (tests/k8s/kind/kustomization.yaml).
ALLOWED_PREFIX = "homelab-k3s-mcp/"
CONTEXT_PREFIXES_ENV = "GITHUB_COMMIT_STATUS_CONTEXT_PREFIXES"

VARIANT_NAMESPACE = "homelab-k3s-mcp-commit-status-variant"
VARIANT_DEPLOYMENT = "homelab-k3s-mcp-commit-status-variant"
VARIANT_SERVICE = VARIANT_DEPLOYMENT
# Clear of ci.yml's group forwards (8080·8088·8089·8090·8092·8093) and of the
# ports other lanes open while this one runs (_gatekeeper 8095·8096,
# _session_platform 18083, platform_auth_safety 18080-18082).
VARIANT_LOCAL_PORT = 18084

INSTALLATION_OWNER = "dlddu"
REPOSITORY = "test"
SHA = "0123456789abcdef0123456789abcdef01234567"
STATUS_PATH = f"/repos/{INSTALLATION_OWNER}/{REPOSITORY}/statuses/{SHA}"

BASE_ARGUMENTS = {
    "repository": REPOSITORY,
    "sha": SHA,
    "state": "success",
}
FOREIGN_CONTEXT = "ci/build"
OWN_CONTEXT = "homelab-k3s-mcp/e2e"


def reset_recorded() -> None:
    httpx.delete(f"{MOCK_URL}/_admin/requests", timeout=10.0).raise_for_status()


def recorded() -> list[dict[str, Any]]:
    response = httpx.get(f"{MOCK_URL}/_admin/requests", timeout=10.0)
    response.raise_for_status()
    return response.json()["requests"]


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
    """AC: github-commit-status/AC4 (a)

    The own-prefix call right after the refusal is the positive control that
    the guard is a prefix check and not a blanket refusal — it must actually
    be written, on this same deployment, in this same session.
    """
    reset_recorded()
    refused = await session.call_tool(
        "github_commit_status_create", {**BASE_ARGUMENTS, "context": FOREIGN_CONTEXT}
    )
    text = refusal_text(refused)
    assert f'context "{FOREIGN_CONTEXT}" is outside the allowed prefixes' in text, text
    assert ALLOWED_PREFIX in text, text
    assert recorded() == [], f"a refused context still reached the upstream: {recorded()}"

    accepted = await session.call_tool(
        "github_commit_status_create", {**BASE_ARGUMENTS, "context": OWN_CONTEXT}
    )
    assert accepted.isError is False, accepted
    writes = [r for r in recorded() if r["method"] == "POST" and r["path"] == STATUS_PATH]
    assert len(writes) == 1, f"the own-prefix context was not written: {recorded()}"


async def test_github_commit_status_ac4_unconfigured_prefix_refuses_everything() -> None:
    """AC: github-commit-status/AC4 (b)

    On the variant even the context the primary accepts is refused, and the
    refusal names the env var — which also proves the App itself *is*
    configured there (an unconfigured App would answer "github app
    unavailable" instead, and this assertion would fail on the wording).
    """
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
            reset_recorded()
            result = await session.call_tool(
                "github_commit_status_create", {**BASE_ARGUMENTS, "context": OWN_CONTEXT}
            )
            text = refusal_text(result)
            assert "no commit status context prefix is configured" in text, text
            assert CONTEXT_PREFIXES_ENV in text, text
            assert recorded() == [], (
                f"the prefix-less variant still reached the upstream: {recorded()}"
            )


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    print("--- github-commit-status/AC4 ---")
    try:
        async with open_session(url) as session:
            await test_github_commit_status_ac4_foreign_context_is_refused(session)
        await test_github_commit_status_ac4_unconfigured_prefix_refuses_everything()
    finally:
        reset_recorded()
    print("ok: github-commit-status/AC4")


if __name__ == "__main__":
    asyncio.run(run())
