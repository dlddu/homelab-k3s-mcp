"""Deployed-identity e2e for platform-auth-safety/AC3 (최소권한 RBAC 경계).

검증 시나리오: test-platform-auth-safety.md#시나리오 3
실행 대상: primary

이 도메인의 유일한 파일로, MCP 서버를 상대하지 않는다 — 실제로 바인딩된 ClusterRole과
apiserver SubjectAccessReview만 읽으므로 세션도, 픽스처 선행 조건도 필요 없다.
``실행 대상: primary`` 는 러너가 이 파일을 어느 그룹에서 한 번 돌릴지를 신고하는
것이고, 전달되는 base URL은 쓰이지 않는다.
"""

from __future__ import annotations

import json
import subprocess

from _workload import NAMESPACE, SERVER_NAMESPACE


SERVER_SERVICE_ACCOUNT = "homelab-k3s-mcp"


SERVER_CLUSTER_ROLE = "homelab-k3s-mcp:workloads"


# Reach worth asking about: every verb the workload kinds could carry, plus the
# neighbours a widening would most plausibly pull in — the stream subresources,
# secrets, nodes and their proxy, configmaps. Coverage, not policy; which of
# these are allowed is read off the role at runtime.
PROBE_CATALOGUE: dict[tuple[str, str], set[str]] = {
    ("apps", "deployments"): {"get", "list", "watch", "patch", "create", "update",
                              "delete", "deletecollection"},
    ("apps", "statefulsets"): {"get", "list", "watch", "patch", "create", "update",
                               "delete", "deletecollection"},
    ("apps", "daemonsets"): {"get", "list", "watch", "patch", "create", "update",
                             "delete", "deletecollection"},
    ("", "pods"): {"get", "list", "watch", "patch", "create", "delete"},
    ("", "pods/exec"): {"get", "create"},
    ("", "pods/log"): {"get"},
    ("", "pods/attach"): {"create"},
    ("", "pods/portforward"): {"create"},
    ("", "namespaces"): {"get", "list", "create", "delete"},
    ("", "events"): {"get", "list"},
    ("", "configmaps"): {"get", "list"},
    ("", "secrets"): {"get", "list", "watch", "create", "update", "patch", "delete"},
    ("", "nodes"): {"get", "list"},
    ("", "nodes/proxy"): {"get", "create"},
}


# Probed without -n, or the answer describes a namespaced resource that does
# not exist rather than the cluster-scoped one that does.
CLUSTER_SCOPED = {"namespaces", "nodes", "nodes/proxy"}


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
    assert grant, f"ClusterRole {SERVER_CLUSTER_ROLE} carries no rules"
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


def expected_answer(grant: dict, group: str, resource: str, verb: str) -> str:
    """What the bound role implies the apiserver must say about one pair."""
    return "yes" if verb in grant.get((group, resource), set()) else "no"


def probe(grant: dict, group: str, resource: str, verb: str,
          namespace: str | None) -> str:
    """Ask about one pair and require the answer the bound role implies."""
    target, _, subresource = resource.partition("/")
    if group:
        target = f"{target}.{group}"
    expected = expected_answer(grant, group, resource, verb)
    answer = can_i(verb, target, namespace, subresource or None)
    print(f"    can-i {verb} {resource}: {answer} (role implies {expected})")
    assert answer == expected, (
        f"{verb} {resource}: apiserver says {answer},"
        f" ClusterRole {SERVER_CLUSTER_ROLE} implies {expected}"
    )
    return answer


def unprobed_grants(grant: dict) -> dict:
    """Granted pairs the catalogue would never ask about."""
    return {f"{group or 'core'}/{resource}": sorted(missing)
            for (group, resource), verbs in grant.items()
            if (missing := verbs - PROBE_CATALOGUE.get((group, resource), set()))}


def test_platform_auth_safety_ac3_rbac_boundary() -> None:
    """AC: platform-auth-safety/AC3 — the deployed identity is capped at what is declared for it.

    Every pair in the catalogue is put to the apiserver as a SubjectAccessReview
    under the ServiceAccount's fully impersonated identity; the bound role decides
    which answer each one must give.

    Reading the role to build the expectation is deliberate and is the whole
    difference from the previous edition, which asserted a copy of the AC's verb
    list. A copy makes the grant unchangeable rather than observed — widening
    k8s/rbac.yaml then fails a test that is only repeating a sentence, which is
    what happened when the read axis tried.

    The coverage assertion at the end is the part that is easy to leave out: a
    verb added to the role would otherwise pass by never being asked about.
    """
    grant = live_cluster_role_grant()

    granted = refused = 0
    for (group, resource), verbs in sorted(PROBE_CATALOGUE.items()):
        namespace = None if resource in CLUSTER_SCOPED else NAMESPACE
        for verb in sorted(verbs):
            if probe(grant, group, resource, verb, namespace) == "yes":
                granted += 1
            else:
                refused += 1

    # Asked a second time in the server's own namespace, where its credentials
    # live: a namespaced grant would answer differently here than above, and a
    # cluster-scoped one must not.
    if probe(grant, "", "secrets", "get", SERVER_NAMESPACE) == "yes":
        granted += 1
    else:
        refused += 1

    missing = unprobed_grants(grant)
    assert not missing, {"granted but never probed": missing}
    print("rbac boundary ok:", granted, "granted verbs,", refused, "refused verbs")


def run() -> None:
    print("--- platform-auth-safety/AC3 ---")
    test_platform_auth_safety_ac3_rbac_boundary()
    print("ok: platform-auth-safety/AC3")


if __name__ == "__main__":
    run()
