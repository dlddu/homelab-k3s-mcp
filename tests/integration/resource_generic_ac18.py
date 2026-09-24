"""권한 밖은 권한 밖이라고 말한다 — 승인 뒤의 403 이 (verb, resource) 쌍으로 번역된다.

검증 시나리오: test-resource-generic.md#시나리오 18
실행 대상: primary
병렬 레인: gate-objects

Go 단위 ``internal/k8s/resource_test.go::TestForbiddenBecomesAGrantStatement`` 가 번역
함수 하나를 가짜 403 으로 단언한다. 실물 apiserver 가 이 SA 에게 실제로 403 을 주는 것,
그 403 이 **승인이 이미 소비된 뒤에** 오는 것, 그리고 게이트 자신의 사전 읽기(AC11)가
같은 번역을 지나는 것은 그 층에서 관측되지 않는다 — 이 파일이 재는 것이 그 자리다.

One file, one deployment that is not the one the runner hands it: every SUT in
this harness is cluster-admin, so the 403 exists only on the narrow-RBAC
variant reached through this file's own port-forward. Which grants that
variant has, which it withholds and why the gate's pre-read shapes them is in
the header of tests/k8s/kind/rbac-narrow-fixture.yaml; this file depends on
three facts from it: clusterroles are readable but not patchable, configmaps
are patchable, networkpolicies are not even readable.

(c) is the positive control on the same deployment — the same dance and the
same code path succeed where the grant exists. Without it (a) could not be
told apart from a broken ServiceAccount or a broken gate.
"""

from __future__ import annotations

import asyncio
import json
import subprocess

from mcp import ClientSession
from mcp.shared.exceptions import McpError

from _auth_variant import API_KEY
from _gatekeeper import (
    decide,
    gatekeeper_url,
    get_request,
    list_requests,
    wait_for_pending,
)
from _helpers import base_url, open_session, port_forward, wait_for_healthz

VARIANT_NAMESPACE = "homelab-k3s-mcp-rbac-narrow"
VARIANT_DEPLOYMENT = "homelab-k3s-mcp-rbac-narrow"
VARIANT_SERVICE = VARIANT_DEPLOYMENT
# Clear of ci.yml's group forwards (8080·8088·8089·8090·8092·8093), of
# _gatekeeper's 8095·8096, and of the ports other lanes open while this file
# runs (_session_platform 18083, platform_auth_safety 18080-18082,
# github_commit_status_ac4 18084).
VARIANT_LOCAL_PORT = 18085

# The variant's own ClusterRole: the object whose patch is refused. If the
# refusal ever failed to happen, the patch would land a label on this fixture
# object and nothing else.
CLUSTER_ROLE = "homelab-k3s-mcp:rbac-narrow"
CONTROL_CONFIGMAP = "rg-ac18-control"
UNREADABLE_POLICY = "rg-ac18-unreadable"

LABEL_KEY = "homelab-k3s-mcp.test/rg-ac18"
FORBIDDEN_MARK = "rg-ac18-forbidden"
CONTROL_MARK = "rg-ac18-control-applied"
UNREADABLE_MARK = "rg-ac18-unreadable"

APISERVER_WORDING = ("is forbidden", "cannot patch resource", "cannot get resource")


def _kubectl(*args: str, check: bool = True) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["kubectl", *args], text=True, check=check, capture_output=True
    )


def _labels(kind: str, name: str, namespace: str | None = None) -> dict[str, str]:
    scope = ["-n", namespace] if namespace else []
    out = _kubectl(*scope, "get", kind, name, "-o", "json").stdout
    return json.loads(out)["metadata"].get("labels") or {}


def _available_replicas() -> int:
    out = _kubectl(
        "-n", VARIANT_NAMESPACE, "get", f"deploy/{VARIANT_DEPLOYMENT}",
        "-o", "jsonpath={.status.availableReplicas}",
    ).stdout
    return int(out.strip() or 0)


def _refusal_text(result) -> str:
    assert result.isError is True, f"call did not refuse: {result}"
    assert result.content, result
    block = result.content[0]
    assert block.type == "text", block
    return block.text


def _assert_translated(text: str, verb: str, resource: str) -> None:
    assert f"({verb}, {resource})" in text, text
    assert "this server has no" in text, text
    assert "retrying will not change the answer" in text, text
    for wording in APISERVER_WORDING:
        assert wording not in text, f"apiserver 의 403 문면이 그대로 흘렀다: {text}"


def _patch_args(
    api_version: str, kind: str, name: str, mark: str, namespace: str | None = None
) -> dict:
    args = {
        "apiVersion": api_version,
        "kind": kind,
        "name": name,
        "patchType": "merge",
        "patch": {"metadata": {"labels": {LABEL_KEY: mark}}},
    }
    if namespace is not None:
        args["namespace"] = namespace
    return args


async def _approved_call(session: ClientSession, gate: str, args: dict, mark: str):
    """resource_patch 를 띄우고 그 승인 요청을 사람 대신 승인한 뒤 결과와 요청 id 를 준다."""
    task = asyncio.create_task(session.call_tool("resource_patch", args))
    row = await wait_for_pending(gate, mark)
    await decide(gate, row["id"], "APPROVED")
    return await task, row["id"]


async def test_resource_generic_ac18_missing_verb_is_named_after_approval(
    session: ClientSession, gate: str
) -> None:
    """AC: resource-generic/AC18 (a)

    Reading the ClusterRole first is not decoration: it shows the kind is
    inside this server's grant, so the pair the refusal names is a verb that
    is missing, not a kind. The gatekeeper record reading APPROVED afterwards
    is what places the 403 after the approval rather than in front of it —
    the pre-read path of (b) would have left no record at all.
    """
    readable = await session.call_tool(
        "resource_get",
        {"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "ClusterRole", "name": CLUSTER_ROLE},
    )
    assert readable.isError is False, readable

    result, request_id = await _approved_call(
        session,
        gate,
        _patch_args("rbac.authorization.k8s.io/v1", "ClusterRole", CLUSTER_ROLE, FORBIDDEN_MARK),
        FORBIDDEN_MARK,
    )
    _assert_translated(_refusal_text(result), "patch", "clusterroles")
    assert get_request(gate, request_id)["status"] == "APPROVED"
    assert LABEL_KEY not in _labels("clusterrole", CLUSTER_ROLE), (
        "거부됐다는 패치가 ClusterRole 에 적용돼 있다"
    )


async def test_resource_generic_ac18_granted_pair_goes_through(
    session: ClientSession, gate: str
) -> None:
    """AC: resource-generic/AC18 (c) — positive control

    Same deployment, same ServiceAccount, same approval dance, same
    resource_patch code path; the one difference from (a) is that this pair
    is granted. Must be observed on the variant, not the primary — a success
    on the primary would say nothing about this SA.
    """
    _kubectl(
        "-n", VARIANT_NAMESPACE, "create", "configmap", CONTROL_CONFIGMAP,
        "--from-literal=purpose=rg-ac18-control",
    )
    result, request_id = await _approved_call(
        session,
        gate,
        _patch_args("v1", "ConfigMap", CONTROL_CONFIGMAP, CONTROL_MARK, VARIANT_NAMESPACE),
        CONTROL_MARK,
    )
    assert result.isError is False, result
    assert get_request(gate, request_id)["status"] == "APPROVED"
    assert _labels("configmap", CONTROL_CONFIGMAP, VARIANT_NAMESPACE).get(LABEL_KEY) == CONTROL_MARK


async def test_resource_generic_ac18_ungranted_kind_is_refused_before_approval(
    session: ClientSession, gate: str
) -> None:
    """AC: resource-generic/AC18 (b)

    A kind with no grant of any verb never reaches the gatekeeper: the gate's
    own pre-read (AC11) is what the apiserver refuses, the refusal comes back
    as a JSON-RPC error rather than a tool result, and it is still the
    translated pair — for the verb that was actually refused, get. The
    request-id set difference is the observation that nothing was asked of a
    human; a marker search would pass vacuously here because there is no
    record to search. The difference is narrowed to requests naming the
    policy because other lanes' gate files add requests meanwhile.
    """
    before = {row["id"] for row in list_requests(gate)}
    try:
        result = await session.call_tool(
            "resource_patch",
            _patch_args("networking.k8s.io/v1", "NetworkPolicy", UNREADABLE_POLICY, UNREADABLE_MARK, VARIANT_NAMESPACE),
        )
    except McpError as exc:
        _assert_translated(str(exc), "get", "networkpolicies")
    else:
        raise AssertionError(f"권한 밖 종류의 patch 가 승인 요청까지 갔다: {result}")
    created = sorted(
        row["id"]
        for row in list_requests(gate)
        if row["id"] not in before and UNREADABLE_POLICY in row.get("context", "")
    )
    assert not created, f"사전 읽기가 거부됐는데 승인 요청이 생겼다: {created}"


async def run() -> None:
    wait_for_healthz(base_url())
    replicas = _available_replicas()
    assert replicas >= 1, (
        f"deploy/{VARIANT_DEPLOYMENT} in {VARIANT_NAMESPACE} has {replicas} available replicas"
    )

    print("--- resource-generic/시나리오 18 (narrow-RBAC variant) ---")
    with gatekeeper_url() as gate, port_forward(
        VARIANT_NAMESPACE, VARIANT_SERVICE, 80, VARIANT_LOCAL_PORT, ready_path="/healthz"
    ) as url:
        wait_for_healthz(url)
        try:
            async with open_session(
                url, headers={"Authorization": f"Bearer {API_KEY}"}
            ) as session:
                await test_resource_generic_ac18_missing_verb_is_named_after_approval(session, gate)
                await test_resource_generic_ac18_granted_pair_goes_through(session, gate)
                await test_resource_generic_ac18_ungranted_kind_is_refused_before_approval(session, gate)
        finally:
            _kubectl(
                "-n", VARIANT_NAMESPACE, "delete", "configmap", CONTROL_CONFIGMAP,
                "--ignore-not-found", check=False,
            )
            _kubectl("label", "clusterrole", CLUSTER_ROLE, f"{LABEL_KEY}-", check=False)
    print("ok: resource-generic/시나리오 18")


if __name__ == "__main__":
    asyncio.run(run())
