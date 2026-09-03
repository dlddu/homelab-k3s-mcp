"""Deployed-identity e2e for platform-auth-safety/AC3 (최소권한 RBAC 경계).

검증 AC: platform-auth-safety/AC3
실행 대상: primary

이 도메인의 유일한 파일로, MCP 서버를 상대하지 않는다 — 실제로 바인딩된 ClusterRole과
apiserver SubjectAccessReview만 읽으므로 세션도, 픽스처 선행 조건도 필요 없다.
``실행 대상: primary`` 는 러너가 이 파일을 어느 그룹에서 한 번 돌릴지를 신고하는
것이고, 전달되는 base URL은 쓰이지 않는다.

이 파일은 하나의 AC만 주검증한다(모델 `tbm_homelab-k3s-mcp-ac-e2e` 규칙 2).
AC↔파일 매핑 SSOT은 ``docs/doc-tracker.md``이고,
``tests/integration/check_ac_mapping.py``가 그 매핑과 이 선언의 일치를 CI에서 강제한다.
"""

from __future__ import annotations

import json
import subprocess

from _workload import NAMESPACE, SERVER_NAMESPACE


SERVER_SERVICE_ACCOUNT = "homelab-k3s-mcp"


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


def run() -> None:
    print("--- platform-auth-safety/AC3 ---")
    test_platform_auth_safety_ac3_rbac_boundary()
    print("ok: platform-auth-safety/AC3")


if __name__ == "__main__":
    run()
