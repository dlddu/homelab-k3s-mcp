"""End-to-end check for the least-privilege RBAC boundary (platform AC3).

Impersonates the server's ServiceAccount with ``kubectl auth can-i`` and
asserts the full permission matrix against the live cluster:

- every rule granted by ``k8s/rbac.yaml`` is allowed, and
- representative *ungranted* permissions are denied — no delete anywhere, no
  secret reads, no workload creation (test-platform-auth-safety.md S3).

The ALLOWED matrix below is intentionally hard-coded rather than derived from
the manifest: if ``k8s/rbac.yaml`` changes, this file must change with it, so
a privilege widening shows up as a reviewable diff in both places.
"""

from __future__ import annotations

import subprocess
import sys

SERVICE_ACCOUNT = "system:serviceaccount:homelab-k3s-mcp:homelab-k3s-mcp"
# Any namespace works for namespaced checks (the grant is a ClusterRoleBinding);
# workload-test exists in CI and is where the fixtures live.
NAMESPACE = "workload-test"

# (verb, resource, namespaced) — keep in sync with k8s/rbac.yaml.
ALLOWED: list[tuple[str, str, bool]] = [
    # apps: deployments/statefulsets/daemonsets get,list,watch,patch
    *[
        (verb, resource, True)
        for resource in ("deployments.apps", "statefulsets.apps", "daemonsets.apps")
        for verb in ("get", "list", "watch", "patch")
    ],
    # core: namespaces get,list (cluster-scoped)
    ("get", "namespaces", False),
    ("list", "namespaces", False),
    # core: pods get,list
    ("get", "pods", True),
    ("list", "pods", True),
    # core: pods/exec get,create (dear_baby_reset_user exec stream)
    ("get", "pods/exec", True),
    ("create", "pods/exec", True),
    # core: pods/log get (workload_logs)
    ("get", "pods/log", True),
    # core: events get,list (pod_describe best-effort)
    ("get", "events", True),
    ("list", "events", True),
]

# Ungranted permissions that must stay denied: no delete, no secret reads,
# no workload creation.
DENIED: list[tuple[str, str, bool]] = [
    ("get", "secrets", True),
    ("list", "secrets", True),
    ("delete", "pods", True),
    ("delete", "deployments.apps", True),
    ("delete", "statefulsets.apps", True),
    ("delete", "daemonsets.apps", True),
    ("create", "deployments.apps", True),
    ("create", "statefulsets.apps", True),
    ("create", "daemonsets.apps", True),
    ("create", "namespaces", False),
    ("delete", "namespaces", False),
]


def can_i(verb: str, resource: str, namespaced: bool) -> bool:
    cmd = ["kubectl", "auth", "can-i", verb, resource, "--as", SERVICE_ACCOUNT]
    if namespaced:
        cmd += ["-n", NAMESPACE]
    proc = subprocess.run(cmd, capture_output=True, text=True)
    answer = proc.stdout.strip()
    # can-i exits 0 for "yes" and 1 for "no"; anything else (bad flag, RBAC
    # lookup failure) must fail loudly instead of reading as a denial.
    if proc.returncode == 0 and answer == "yes":
        return True
    if proc.returncode == 1 and answer == "no":
        return False
    raise RuntimeError(
        f"kubectl auth can-i {verb} {resource} failed: "
        f"rc={proc.returncode} stdout={answer!r} stderr={proc.stderr.strip()!r}"
    )


def main() -> None:
    failures: list[str] = []

    print(f"--- RBAC boundary as {SERVICE_ACCOUNT} ---")
    for verb, resource, namespaced in ALLOWED:
        if not can_i(verb, resource, namespaced):
            failures.append(f"expected ALLOW, got deny: {verb} {resource}")
    print(f"allowed matrix ok: {len(ALLOWED)} grants confirmed")

    for verb, resource, namespaced in DENIED:
        if can_i(verb, resource, namespaced):
            failures.append(f"expected DENY, got allow: {verb} {resource}")
    print(f"denied matrix ok: {len(DENIED)} ungranted permissions confirmed absent")

    if failures:
        for f in failures:
            print("FAIL:", f, file=sys.stderr)
        raise SystemExit(1)
    print("rbac boundary ok")


if __name__ == "__main__":
    main()
