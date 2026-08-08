"""End-to-end checks for the cluster-facing tools against the kind fixtures.

Covers namespace_list / workload_list / workload_logs / workload_restart /
workload_scale / pod_describe plus the deployed RBAC boundary.

Per-AC case names + docstrings declare the AC they verify (registry rule 3);
``docs/doc-tracker.md`` is the AC<->case mapping SSOT. ``run()`` is only a
dispatcher: it opens one session and calls the cases in an order the shared
fixture state requires (see the comments there).
"""

from __future__ import annotations

import asyncio
import datetime
import json
import re
import subprocess
import time

from mcp.shared.exceptions import McpError

from _helpers import (
    assert_destructive_annotation,
    base_url,
    open_session,
    wait_for_healthz,
)

NAMESPACE = "workload-test"
WORKLOAD = "workload-fixture"
STS_WORKLOAD = "workload-fixture-sts"
DS_WORKLOAD = "workload-fixture-ds"
CRASHLOOP_WORKLOAD = "crashloop-fixture"
# Must match the echo lines in tests/k8s/kind/test-deployment.yaml: the first
# container instance prints CRASHLOOP_MARKER and exits, every later one prints
# RECOVERED_MARKER and stays Running.
CRASHLOOP_MARKER = "crashloop-fixture: boom before exit"
RECOVERED_MARKER = "crashloop-fixture: recovered"
RESTART_ANNOTATION_PATH = (
    r"{.spec.template.metadata.annotations.kubectl\.kubernetes\.io/restartedAt}"
)

# Identity the deployed server runs as (k8s/serviceaccount.yaml + the
# ClusterRoleBinding in k8s/rbac.yaml), used by the RBAC-boundary case.
SERVER_NAMESPACE = "homelab-k3s-mcp"
SERVER_SERVICE_ACCOUNT = "homelab-k3s-mcp"
# Namespace of the dear-baby exec fixture, where pods/exec is exercised.
EXEC_NAMESPACE = "dear-baby-test"
SERVER_CLUSTER_ROLE = "homelab-k3s-mcp:workloads"

# The complete grant platform-auth-safety/AC3 describes, transcribed from the
# AC text: workloads get/list/watch/patch, pods get/list, pods/log get,
# pods/exec get+create, namespaces and events get/list — and nothing else.
# Equality against the live ClusterRole is what makes "delete/시크릿/워크로드
# create 규칙이 존재하지 않는다" an assertion rather than a reading.
EXPECTED_GRANT = {
    ("apps", "deployments"): {"get", "list", "watch", "patch"},
    ("apps", "statefulsets"): {"get", "list", "watch", "patch"},
    ("apps", "daemonsets"): {"get", "list", "watch", "patch"},
    ("", "namespaces"): {"get", "list"},
    ("", "pods"): {"get", "list"},
    ("", "pods/exec"): {"get", "create"},
    ("", "pods/log"): {"get"},
    ("", "events"): {"get", "list"},
}

# Verbs the AC names as never granted, probed where they would hurt most.
FORBIDDEN_PROBES = [
    ("delete", "deployments.apps", NAMESPACE),
    ("create", "deployments.apps", NAMESPACE),
    ("update", "deployments.apps", NAMESPACE),
    ("delete", "statefulsets.apps", NAMESPACE),
    ("delete", "daemonsets.apps", NAMESPACE),
    ("delete", "pods", NAMESPACE),
    ("create", "pods", NAMESPACE),
    ("get", "secrets", SERVER_NAMESPACE),
    ("list", "secrets", NAMESPACE),
    ("create", "namespaces", None),
    ("delete", "namespaces", None),
]

# A kubelet log line printed with timestamps=true is prefixed with an RFC3339
# instant and a single space.
LOG_TIMESTAMP_RE = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z ")


def wait_for_crashloop_restart(timeout: float = 180.0) -> None:
    """Block until the crash-once fixture pod has restarted at least once.

    previous=true logs only exist once the kubelet has restarted the container
    (restartCount >= 1). The fixture crashes exactly once and then stays
    Running, so after the first restart lastState.terminated is pinned to the
    marker-printing instance and this returns for good. The fixture is applied
    minutes before this script runs in CI, so this normally returns
    immediately; the poll is a safety net for scheduling/backoff timing.
    """
    deadline = time.monotonic() + timeout
    last = "<no pod yet>"
    while time.monotonic() < deadline:
        proc = subprocess.run(
            [
                "kubectl",
                "-n",
                NAMESPACE,
                "get",
                "pod",
                "-l",
                f"app={CRASHLOOP_WORKLOAD}",
                "-o",
                "jsonpath={.items[0].status.containerStatuses[0].restartCount}",
            ],
            capture_output=True,
            text=True,
        )
        last = proc.stdout.strip() or proc.stderr.strip()
        if proc.returncode == 0 and proc.stdout.strip().isdigit():
            if int(proc.stdout.strip()) >= 1:
                return
        time.sleep(3)
    raise RuntimeError(
        f"crashloop fixture never reached restartCount >= 1 within {timeout:.0f}s"
        f" (last observation: {last!r})"
    )


def wait_for_crashloop_log_age(min_age: float, timeout: float = 60.0) -> None:
    """Block until the recovered crashloop instance has been running >= min_age.

    The instance prints RECOVERED_MARKER once, at startup. Two cases depend on
    that line being safely in the past: workload-logs/AC1 reads it back (so the
    write must have been flushed) and workload-logs/AC4 asserts a small
    since_seconds window *excludes* it. Both become deterministic once the
    container has been up longer than that window. In CI the fixture is applied
    minutes earlier, so this returns on the first probe.
    """
    deadline = time.monotonic() + timeout
    last = "<no startedAt yet>"
    while time.monotonic() < deadline:
        proc = subprocess.run(
            [
                "kubectl",
                "-n",
                NAMESPACE,
                "get",
                "pod",
                "-l",
                f"app={CRASHLOOP_WORKLOAD}",
                "-o",
                "jsonpath={.items[0].status.containerStatuses[0]"
                ".state.running.startedAt}",
            ],
            capture_output=True,
            text=True,
        )
        last = proc.stdout.strip() or proc.stderr.strip()
        if proc.returncode == 0 and proc.stdout.strip():
            started = datetime.datetime.fromisoformat(proc.stdout.strip())
            age = (
                datetime.datetime.now(datetime.timezone.utc) - started
            ).total_seconds()
            if age >= min_age:
                return
        time.sleep(1)
    raise RuntimeError(
        f"crashloop fixture never reached running age >= {min_age:.0f}s within"
        f" {timeout:.0f}s (last observation: {last!r})"
    )


def kubectl_jsonpath(jsonpath: str, resource: str = f"deploy/{WORKLOAD}") -> str:
    out = subprocess.check_output(
        [
            "kubectl",
            "-n",
            NAMESPACE,
            "get",
            resource,
            "-o",
            f"jsonpath={jsonpath}",
        ],
        text=True,
    )
    return out.strip()


def kubectl_wait_rollout() -> None:
    subprocess.run(
        [
            "kubectl",
            "-n",
            NAMESPACE,
            "rollout",
            "status",
            f"deploy/{WORKLOAD}",
            "--timeout=120s",
        ],
        check=True,
    )


def wait_for_status_replicas(expected: int, timeout: float = 120.0) -> None:
    """Block until deploy/WORKLOAD reports `expected` running replicas.

    Used instead of `kubectl rollout status` for the scale-to-zero leg: an
    empty .status.replicas is how the apiserver renders zero, and waiting for
    the field to drain is what proves the scale actually took effect.
    """
    deadline = time.monotonic() + timeout
    last = ""
    while time.monotonic() < deadline:
        last = kubectl_jsonpath("{.status.replicas}")
        observed = int(last) if last.isdigit() else 0
        if observed == expected:
            return
        time.sleep(2)
    raise RuntimeError(
        f"deploy/{WORKLOAD} never reported {expected} replicas within"
        f" {timeout:.0f}s (last observation: {last!r})"
    )


def live_cluster_role_grant() -> dict:
    """Read the ClusterRole that is actually bound in the cluster."""
    raw = subprocess.check_output(
        ["kubectl", "get", "clusterrole", SERVER_CLUSTER_ROLE, "-o", "json"],
        text=True,
    )
    grant: dict = {}
    for rule in json.loads(raw)["rules"]:
        for group in rule.get("apiGroups", []):
            for resource in rule.get("resources", []):
                grant.setdefault((group, resource), set()).update(rule.get("verbs", []))
    return grant


def can_i(verb: str, resource: str, namespace: str | None = None,
          subresource: str | None = None) -> str:
    """Ask the apiserver whether the deployed ServiceAccount may do `verb`.

    Impersonates the full identity a ServiceAccount token carries (the user
    plus the three groups the apiserver derives from it), so the answer is the
    same SubjectAccessReview the server's own requests are evaluated against.
    Subresources go through the explicit ``--subresource`` flag rather than the
    ``pods/exec`` positional shorthand, whose parse is ambiguous.
    """
    cmd = [
        "kubectl",
        "auth",
        "can-i",
        verb,
        resource,
        f"--as=system:serviceaccount:{SERVER_NAMESPACE}:{SERVER_SERVICE_ACCOUNT}",
        "--as-group=system:serviceaccounts",
        f"--as-group=system:serviceaccounts:{SERVER_NAMESPACE}",
        "--as-group=system:authenticated",
    ]
    if subresource is not None:
        cmd.append(f"--subresource={subresource}")
    if namespace is not None:
        cmd += ["-n", namespace]
    proc = subprocess.run(cmd, capture_output=True, text=True)
    answer = proc.stdout.strip()
    assert answer in {"yes", "no"}, (
        f"unexpected `kubectl auth can-i {verb} {resource}"
        f"{' --subresource=' + subresource if subresource else ''}` output:"
        f" stdout={proc.stdout!r} stderr={proc.stderr!r}"
    )
    return answer


async def test_namespace_list_ac1_enumerates_namespaces(session) -> None:
    """AC: namespace-list/AC1 — namespaces come back with name, phase, creation time.

    Asserts the three fields the AC names are present on every item and carry
    real values on a known namespace: the workload fixture's namespace is
    reported Active with a parseable creation timestamp, and a namespace the
    test did not create (kube-system) is listed too, so the tool is enumerating
    the cluster rather than echoing a filter.
    """
    result = await session.call_tool("namespace_list", {})
    assert result.isError is False, result
    items = result.structuredContent["items"]
    names = [item["name"] for item in items]
    assert NAMESPACE in names, names
    assert "kube-system" in names, names

    for item in items:
        assert item["phase"], item
        assert item["creation_timestamp"], item

    active = next(item for item in items if item["name"] == NAMESPACE)
    assert active["phase"] == "Active", active
    # Parses (and therefore is a real instant), not just a non-empty string.
    datetime.datetime.fromisoformat(active["creation_timestamp"])
    print("namespace_list ok:", len(items), "namespaces")


async def test_workload_list_ac1_kinds_with_replica_summary(session) -> None:
    """AC: workload-list/AC1 — every kind enum returns its workloads with a replica summary.

    Calls workload_list once per enum member against the fixture namespace,
    which holds one object of each kind (test-deployment.yaml), and asserts
    both halves of the criterion: the listed object is the one of that kind,
    and each item carries the kind's own replica-summary fields as integers.
    The counts themselves are not pinned to a value because workload-scale/AC1
    moves the Deployment's replicas around in the same run.
    """
    expected_fields = {
        "Deployment": (
            WORKLOAD,
            ["replicas", "ready_replicas", "updated_replicas", "available_replicas"],
        ),
        "StatefulSet": (
            STS_WORKLOAD,
            ["replicas", "ready_replicas", "updated_replicas", "current_replicas"],
        ),
        "DaemonSet": (
            DS_WORKLOAD,
            [
                "desired_number_scheduled",
                "current_number_scheduled",
                "number_ready",
                "number_available",
                "updated_number_scheduled",
            ],
        ),
    }

    for kind, (fixture, fields) in expected_fields.items():
        result = await session.call_tool(
            "workload_list", {"kind": kind, "namespace": NAMESPACE}
        )
        assert result.isError is False, (kind, result)
        payload = result.structuredContent
        assert payload["kind"] == kind, payload
        items = payload["items"]
        names = [item["name"] for item in items]
        assert fixture in names, (kind, names)

        item = next(i for i in items if i["name"] == fixture)
        for field in fields:
            assert field in item, (kind, field, item)
            assert isinstance(item[field], int), (kind, field, item)
        # A kind's summary must not carry another kind's shape.
        for other_kind, (_, other_fields) in expected_fields.items():
            if other_kind == kind:
                continue
            for field in set(other_fields) - set(fields):
                assert field not in item, (kind, other_kind, field, item)
        print(f"workload_list {kind} ok:", names)


async def test_workload_list_ac2_namespace_scope(session) -> None:
    """AC: workload-list/AC2 — namespace narrows the listing, omitting it widens it.

    The scoped call must return *only* that namespace's workloads (asserted
    over every item, not just the fixture), and the unscoped call must reach
    workloads the scoped one cannot see — the server's own Deployment in
    another namespace.
    """
    scoped = await session.call_tool(
        "workload_list", {"kind": "Deployment", "namespace": NAMESPACE}
    )
    assert scoped.isError is False, scoped
    payload = scoped.structuredContent
    items = payload.pop("items")
    assert payload == {"kind": "Deployment", "namespace": NAMESPACE}, payload
    names = [item["name"] for item in items]
    assert WORKLOAD in names, names
    for item in items:
        assert item["namespace"] == NAMESPACE, item
    assert (SERVER_NAMESPACE, SERVER_NAMESPACE) not in {
        (i["namespace"], i["name"]) for i in items
    }, items
    print("scoped list ok:", names)

    unscoped = await session.call_tool("workload_list", {"kind": "Deployment"})
    assert unscoped.isError is False, unscoped
    payload = unscoped.structuredContent
    items = payload.pop("items")
    assert payload == {"kind": "Deployment", "namespace": None}, payload
    pairs = {(i["namespace"], i["name"]) for i in items}
    assert (NAMESPACE, WORKLOAD) in pairs, pairs
    assert (SERVER_NAMESPACE, SERVER_NAMESPACE) in pairs, pairs
    print("unscoped list ok:", len(items), "items")


async def test_workload_restart_ac1_rolling_restart(session) -> None:
    """AC: workload-restart/AC1 — a restart patches the workload instead of recreating it.

    Asserts the trigger annotation the rollout keys off is written, that a new
    rollout is actually started (metadata.generation advances and the rollout
    completes), and — the "재생성/삭제를 사용하지 않는다" half — that the
    Deployment object is the same one afterwards: a delete+create would mint a
    new metadata.uid and reset creationTimestamp.
    """
    uid_before = kubectl_jsonpath("{.metadata.uid}")
    created_before = kubectl_jsonpath("{.metadata.creationTimestamp}")
    generation_before = int(kubectl_jsonpath("{.metadata.generation}"))
    assert uid_before and created_before, (uid_before, created_before)

    result = await session.call_tool(
        "workload_restart",
        {"kind": "Deployment", "namespace": NAMESPACE, "name": WORKLOAD},
    )
    assert result.isError is False, result
    payload = result.structuredContent
    restarted_at = payload.pop("restartedAt")
    assert restarted_at, "restartedAt should be a non-empty timestamp"
    assert payload == {
        "kind": "Deployment",
        "namespace": NAMESPACE,
        "name": WORKLOAD,
    }, payload

    annotation = kubectl_jsonpath(RESTART_ANNOTATION_PATH)
    print("restartedAt annotation:", annotation)
    assert annotation, "restartedAt annotation missing on resource"
    assert annotation == restarted_at, (annotation, restarted_at)

    generation_after = int(kubectl_jsonpath("{.metadata.generation}"))
    assert generation_after > generation_before, (
        generation_before,
        generation_after,
    )
    assert kubectl_jsonpath("{.metadata.uid}") == uid_before, "workload was recreated"
    assert kubectl_jsonpath("{.metadata.creationTimestamp}") == created_before, (
        "workload was recreated"
    )
    kubectl_wait_rollout()
    print("workload_restart ok at", restarted_at)


async def test_workload_scale_ac1_replica_count(session) -> None:
    """AC: workload-scale/AC1 — spec.replicas is set to the requested value, zero included.

    Walks 3 -> 0 -> 1 so both the ordinary path and the AC's explicit
    "0으로의 스케일다운도 허용한다" clause are observed, checking spec.replicas
    on the cluster after each call rather than trusting the tool's echo. Ends
    back at 1 replica, and waits for it, because the log and pod_describe cases
    later in run() need a Running fixture pod.
    """
    for replicas in (3, 0, 1):
        result = await session.call_tool(
            "workload_scale",
            {
                "kind": "Deployment",
                "namespace": NAMESPACE,
                "name": WORKLOAD,
                "replicas": replicas,
            },
        )
        assert result.isError is False, (replicas, result)
        assert result.structuredContent == {
            "kind": "Deployment",
            "namespace": NAMESPACE,
            "name": WORKLOAD,
            "replicas": replicas,
        }, result.structuredContent

        observed = kubectl_jsonpath("{.spec.replicas}")
        assert observed == str(replicas), (
            f"expected {replicas} replicas, got {observed!r}"
        )
        if replicas == 0:
            wait_for_status_replicas(0)
        else:
            kubectl_wait_rollout()
        print(f"workload_scale to {replicas} ok")


async def test_workload_scale_ac2_daemonset_rejected(session) -> None:
    """AC: workload-scale/AC2 — DaemonSet is refused because the kind has no replicas.

    Targets the DaemonSet that really exists in the fixture namespace, so the
    refusal is observably about the *kind* rather than about a missing object,
    and checks the tool advertises the same restriction in its input schema
    (kind enum excludes DaemonSet).
    """
    tools = await session.list_tools()
    scale = next(tool for tool in tools.tools if tool.name == "workload_scale")
    kind_enum = scale.inputSchema["properties"]["kind"]["enum"]
    assert "DaemonSet" not in kind_enum, kind_enum
    assert {"Deployment", "StatefulSet"} <= set(kind_enum), kind_enum

    result = await session.call_tool(
        "workload_scale",
        {
            "kind": "DaemonSet",
            "namespace": NAMESPACE,
            "name": DS_WORKLOAD,
            "replicas": 1,
        },
    )
    assert result.isError, result
    text = result.content[0].text
    assert "DaemonSet does not have replicas" in text, text
    print("workload_scale daemonset rejection ok")


async def test_workload_logs_ac1_logs_by_workload(session) -> None:
    """AC: workload-logs/AC1 — the workload's selector resolves to a pod and its logs come back.

    Runs against the crash-once fixture rather than the pause-image fixture:
    pause emits nothing, so reading it back proves selector resolution but not
    that log *content* is returned. The recovered instance prints a known
    marker at startup, so a non-empty body containing that marker is the AC's
    "최근 로그가 반환된다". A workload no selector can resolve comes back as a
    tool error instead of a crash.
    """
    wait_for_crashloop_restart()
    wait_for_crashloop_log_age(5.0)

    result = await session.call_tool(
        "workload_logs",
        {"kind": "Deployment", "namespace": NAMESPACE, "name": CRASHLOOP_WORKLOAD},
    )
    assert result.isError is False, result
    payload = result.structuredContent
    assert payload["pod"].startswith(f"{CRASHLOOP_WORKLOAD}-"), payload
    assert payload["previous"] is False, payload
    assert RECOVERED_MARKER in payload["logs"], payload["logs"]
    assert RECOVERED_MARKER in result.content[0].text, result.content[0].text
    print("workload_logs by-workload ok, pod:", payload["pod"])

    missing = await session.call_tool(
        "workload_logs",
        {"kind": "Deployment", "namespace": NAMESPACE, "name": "does-not-exist"},
    )
    assert missing.isError, missing
    print("workload_logs missing-workload rejection ok")


async def test_workload_logs_ac2_tail_lines(session) -> None:
    """AC: workload-logs/AC2 — tailLines defaults to 200 and over-max requests are rejected.

    The omitted-argument call must report the documented default (200) rather
    than a server-side clamp, and 999999 must come back as a rejection. Input
    validation errors are JSON-RPC errors, which the SDK raises as McpError
    rather than returning as a tool result.
    """
    result = await session.call_tool(
        "workload_logs",
        {"kind": "Deployment", "namespace": NAMESPACE, "name": WORKLOAD},
    )
    assert result.isError is False, result
    payload = result.structuredContent
    pod_name = payload.pop("pod")
    assert pod_name.startswith(f"{WORKLOAD}-"), pod_name
    assert payload == {
        "kind": "Deployment",
        "namespace": NAMESPACE,
        "name": WORKLOAD,
        "container": None,
        "tailLines": 200,
        "previous": False,
        "timestamps": False,
        "sinceSeconds": None,
        "logs": "",
    }, payload
    assert result.content[0].text == "(no log output)", result.content[0].text
    print("workload_logs defaults ok, pod:", pod_name)

    try:
        await session.call_tool(
            "workload_logs",
            {
                "kind": "Deployment",
                "namespace": NAMESPACE,
                "name": WORKLOAD,
                "tail_lines": 999_999,
            },
        )
    except McpError as exc:
        assert "tail_lines" in str(exc), exc
        print("workload_logs tail_lines rejection ok")
    else:
        raise AssertionError("expected McpError for tail_lines over max")


async def test_workload_logs_ac3_previous_after_crash(session) -> None:
    """AC: workload-logs/AC3 — previous=true returns the terminated instance's own log.

    test-workload-logs.md S3 / AC3: after a crash, previous=true must return
    the terminated instance's actual log content. The fixture prints a known
    marker, exits non-zero exactly once, then stays Running — pinning
    lastState.terminated to the marker instance. The marker differs from the
    one the *current* instance prints, so this cannot pass by reading live logs.
    """
    wait_for_crashloop_restart()
    result = await session.call_tool(
        "workload_logs",
        {
            "kind": "Deployment",
            "namespace": NAMESPACE,
            "name": CRASHLOOP_WORKLOAD,
            "previous": True,
        },
    )
    assert result.isError is False, result
    payload = result.structuredContent
    pod_name = payload["pod"]
    assert pod_name.startswith(f"{CRASHLOOP_WORKLOAD}-"), pod_name
    assert payload["previous"] is True, payload
    assert CRASHLOOP_MARKER in payload["logs"], payload["logs"]
    assert CRASHLOOP_MARKER in result.content[0].text, result.content[0].text
    print("workload_logs previous content ok, pod:", pod_name)


async def test_workload_logs_ac4_container_and_filters(session) -> None:
    """AC: workload-logs/AC4 — container selection and the timestamps/since_seconds filters.

    Asserts container selection is really applied (a name no container has is
    refused, the fixture's own container is accepted) and that the two filters
    change the *output*, not just the echoed request: timestamps=true prefixes
    every line with an RFC3339 instant, and a since_seconds window narrower
    than the recovered instance's age drops the startup marker the unfiltered
    call returns.

    Not asserted: the AC's "파드에 컨테이너가 둘 이상이면 container가 필요하다"
    clause. Every fixture pod in this deployment is single-container, so the
    multi-container refusal has no target here; it needs a running
    multi-container fixture (tracked in the doc-tracker e2e backlog).
    """
    accepted = await session.call_tool(
        "workload_logs",
        {
            "kind": "Deployment",
            "namespace": NAMESPACE,
            "name": WORKLOAD,
            "container": "pause",
            "tail_lines": 10,
            "timestamps": True,
            "since_seconds": 60,
        },
    )
    assert accepted.isError is False, accepted
    payload = accepted.structuredContent
    assert payload["container"] == "pause", payload
    assert payload["tailLines"] == 10, payload
    assert payload["timestamps"] is True, payload
    assert payload["sinceSeconds"] == 60, payload

    refused = await session.call_tool(
        "workload_logs",
        {
            "kind": "Deployment",
            "namespace": NAMESPACE,
            "name": WORKLOAD,
            "container": "no-such-container",
        },
    )
    assert refused.isError, refused
    print("workload_logs container selection ok")

    # Filters are observed on a workload that actually emits output.
    wait_for_crashloop_restart()
    wait_for_crashloop_log_age(5.0)

    stamped = await session.call_tool(
        "workload_logs",
        {
            "kind": "Deployment",
            "namespace": NAMESPACE,
            "name": CRASHLOOP_WORKLOAD,
            "timestamps": True,
        },
    )
    assert stamped.isError is False, stamped
    lines = [line for line in stamped.structuredContent["logs"].splitlines() if line]
    assert lines, stamped.structuredContent
    for line in lines:
        assert LOG_TIMESTAMP_RE.match(line), line
    assert any(RECOVERED_MARKER in line for line in lines), lines

    recent = await session.call_tool(
        "workload_logs",
        {
            "kind": "Deployment",
            "namespace": NAMESPACE,
            "name": CRASHLOOP_WORKLOAD,
            "since_seconds": 1,
        },
    )
    assert recent.isError is False, recent
    assert RECOVERED_MARKER not in recent.structuredContent["logs"], (
        recent.structuredContent
    )
    print("workload_logs timestamps/since_seconds ok")


def test_platform_auth_safety_ac3_rbac_boundary() -> None:
    """AC: platform-auth-safety/AC3 — the deployed identity is capped at the granted verbs.

    Two layers, both against the running cluster rather than the manifest file.

    First the ClusterRole that is actually bound is read back and required to
    equal EXPECTED_GRANT exactly. Equality (not containment) is what asserts the
    AC's "워크로드 delete/create, 시크릿 읽기 권한은 부여하지 않는다": an extra
    resource or verb anywhere in the role fails here.

    Then the apiserver is asked, through SubjectAccessReview with the
    ServiceAccount's full impersonated identity, whether that grant is what the
    server can really do — every granted verb must answer yes and the AC's
    exclusion list must answer no. This catches permission that a *different*
    object confers (another ClusterRoleBinding, a group grant), which reading
    k8s/rbac.yaml alone would miss.
    """
    live = live_cluster_role_grant()
    assert live == EXPECTED_GRANT, {
        "only_in_cluster": {k: sorted(v) for k, v in live.items()
                            if EXPECTED_GRANT.get(k) != v},
        "only_expected": {k: sorted(v) for k, v in EXPECTED_GRANT.items()
                          if live.get(k) != v},
    }

    granted = 0
    for (group, resource), verbs in sorted(EXPECTED_GRANT.items()):
        target, _, subresource = resource.partition("/")
        if group:
            target = f"{target}.{group}"
        namespace = None if resource == "namespaces" else NAMESPACE
        for verb in sorted(verbs):
            answer = can_i(verb, target, namespace, subresource or None)
            print(f"    can-i {verb} {resource}: {answer}")
            assert answer == "yes", f"expected to be allowed: {verb} {resource}"
            granted += 1

    for verb, resource, namespace in FORBIDDEN_PROBES:
        answer = can_i(verb, resource, namespace)
        print(f"    can-i {verb} {resource}: {answer}")
        assert answer == "no", f"expected to be denied: {verb} {resource}"
    print(
        "rbac boundary ok:",
        granted,
        "granted verbs,",
        len(FORBIDDEN_PROBES),
        "refused verbs",
    )


async def test_workload_restart_ac2_destructive_hint(session) -> None:
    """AC: workload-restart/AC2 — workload_restart advertises destructiveHint=true.

    Verifies the destructive-operation marking via tools/list metadata only; no
    restart is triggered.
    """
    await assert_destructive_annotation(session, "workload_restart")


async def test_workload_scale_ac3_destructive_hint(session) -> None:
    """AC: workload-scale/AC3 — workload_scale advertises destructiveHint=true.

    Verifies the destructive-operation marking via tools/list metadata only; no
    scale is performed.
    """
    await assert_destructive_annotation(session, "workload_scale")


async def test_pod_describe_ac1_snapshot(session) -> None:
    """AC: pod-describe/AC1 — pod_describe returns a structured pod snapshot.

    Describes the running workload-fixture pod (resolved by label selector so the
    case is independent of the generated pod name) and asserts the snapshot
    carries pod metadata plus per-container state (state / ready / restart count),
    conditions, and the kubectl-describe-style rendered text.
    """
    result = await session.call_tool(
        "pod_describe",
        {"namespace": NAMESPACE, "selector": f"app={WORKLOAD}"},
    )
    assert result.isError is False, result
    snapshot = result.structuredContent
    assert snapshot["namespace"] == NAMESPACE, snapshot
    assert snapshot["name"].startswith(f"{WORKLOAD}-"), snapshot
    assert snapshot["phase"] == "Running", snapshot
    pause = next(
        (c for c in snapshot["containers"] if c["name"] == "pause"), None
    )
    assert pause is not None, snapshot["containers"]
    assert "pause" in pause["image"], pause
    assert pause["ready"] is True, pause
    assert pause["restart_count"] == 0, pause
    assert pause["state"] == "running", pause
    assert isinstance(snapshot["conditions"], list) and snapshot["conditions"], (
        snapshot
    )
    text = result.content[0].text
    assert "Name:" in text and NAMESPACE in text, text
    print("pod_describe snapshot ok, pod:", snapshot["name"])


async def test_pod_describe_ac2_target_resolution(session) -> None:
    """AC: pod-describe/AC2 — name / selector / workload targeting resolves one pod.

    Verifies each single targeting mode resolves to a workload-fixture pod and
    that supplying two modes at once is rejected (mutually exclusive). Target
    argument errors come back as JSON-RPC errors, surfaced by the SDK as
    McpError rather than a tool result object.
    """
    by_selector = await session.call_tool(
        "pod_describe",
        {"namespace": NAMESPACE, "selector": f"app={WORKLOAD}"},
    )
    assert by_selector.isError is False, by_selector
    pod_name = by_selector.structuredContent["name"]
    assert pod_name.startswith(f"{WORKLOAD}-"), pod_name

    by_name = await session.call_tool(
        "pod_describe",
        {"namespace": NAMESPACE, "name": pod_name},
    )
    assert by_name.isError is False, by_name
    assert by_name.structuredContent["name"] == pod_name, by_name.structuredContent

    by_workload = await session.call_tool(
        "pod_describe",
        {
            "namespace": NAMESPACE,
            "workload_kind": "Deployment",
            "workload_name": WORKLOAD,
        },
    )
    assert by_workload.isError is False, by_workload
    assert by_workload.structuredContent["name"].startswith(f"{WORKLOAD}-"), (
        by_workload.structuredContent
    )

    try:
        await session.call_tool(
            "pod_describe",
            {
                "namespace": NAMESPACE,
                "name": pod_name,
                "selector": f"app={WORKLOAD}",
            },
        )
    except McpError as exc:
        assert "mutually exclusive" in str(exc), exc
        print("pod_describe mutual-exclusion rejection ok")
    else:
        raise AssertionError("expected McpError for name+selector both provided")


async def test_pod_describe_ac3_events_best_effort(session) -> None:
    """AC: pod-describe/AC3 — the snapshot includes an events section best-effort.

    The server lists events best-effort and always returns an ``events`` array
    (empty when unavailable) without failing the describe call.
    """
    result = await session.call_tool(
        "pod_describe",
        {"namespace": NAMESPACE, "selector": f"app={WORKLOAD}"},
    )
    assert result.isError is False, result
    events = result.structuredContent["events"]
    assert isinstance(events, list), result.structuredContent
    print("pod_describe events best-effort ok, events:", len(events))


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    async with open_session(url) as session:
        # Order matters: the read-only listing cases run first, then the two
        # mutating cases (restart, then scale — scale leaves the fixture back at
        # one Running replica and waits for it), and only then the cases that
        # need that replica (logs, pod_describe). The RBAC and destructive-hint
        # cases are order-independent.
        print("--- namespace_list (AC: namespace-list/AC1) ---")
        await test_namespace_list_ac1_enumerates_namespaces(session)

        print("--- workload_list kinds (AC: workload-list/AC1) ---")
        await test_workload_list_ac1_kinds_with_replica_summary(session)

        print("--- workload_list namespace scope (AC: workload-list/AC2) ---")
        await test_workload_list_ac2_namespace_scope(session)

        print("--- workload_restart (AC: workload-restart/AC1) ---")
        await test_workload_restart_ac1_rolling_restart(session)

        print("--- workload_scale replicas (AC: workload-scale/AC1) ---")
        await test_workload_scale_ac1_replica_count(session)

        print("--- workload_scale daemonset (AC: workload-scale/AC2) ---")
        await test_workload_scale_ac2_daemonset_rejected(session)

        print("--- workload_logs by workload (AC: workload-logs/AC1) ---")
        await test_workload_logs_ac1_logs_by_workload(session)

        print("--- workload_logs tail lines (AC: workload-logs/AC2) ---")
        await test_workload_logs_ac2_tail_lines(session)

        print("--- workload_logs previous (AC: workload-logs/AC3) ---")
        await test_workload_logs_ac3_previous_after_crash(session)

        print("--- workload_logs container/filters (AC: workload-logs/AC4) ---")
        await test_workload_logs_ac4_container_and_filters(session)

        print("--- rbac boundary (AC: platform-auth-safety/AC3) ---")
        test_platform_auth_safety_ac3_rbac_boundary()

        print("--- workload_restart destructiveHint (AC: workload-restart/AC2) ---")
        await test_workload_restart_ac2_destructive_hint(session)
        print("workload_restart destructiveHint ok")

        print("--- workload_scale destructiveHint (AC: workload-scale/AC3) ---")
        await test_workload_scale_ac3_destructive_hint(session)
        print("workload_scale destructiveHint ok")

        print("--- pod_describe snapshot (AC: pod-describe/AC1) ---")
        await test_pod_describe_ac1_snapshot(session)

        print("--- pod_describe target resolution (AC: pod-describe/AC2) ---")
        await test_pod_describe_ac2_target_resolution(session)
        print("pod_describe target resolution ok")

        print("--- pod_describe events best-effort (AC: pod-describe/AC3) ---")
        await test_pod_describe_ac3_events_best_effort(session)


if __name__ == "__main__":
    asyncio.run(run())
