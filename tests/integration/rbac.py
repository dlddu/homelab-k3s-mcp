"""End-to-end check for the least-privilege RBAC boundary (platform AC3).

Authenticates *as* the server's ServiceAccount and asserts the full
permission matrix with ``kubectl auth can-i`` against the live cluster:

- every rule granted by ``k8s/rbac.yaml`` is allowed, and
- representative *ungranted* permissions are denied — no delete anywhere, no
  secret reads, no workload creation (test-platform-auth-safety.md S3).

Authentication uses a dedicated kubeconfig containing only a short-lived
ServiceAccount token. Two tempting shortcuts are wrong:

- ``--as`` impersonation answered deterministically wrong for
  ``create pods/exec`` in CI (run #93/#94) while a real exec succeeded, and
- ``--token`` on top of the admin kubeconfig is silently ignored — the
  kubeconfig's TLS client certificate wins authentication first, so every
  query evaluates as cluster-admin (run #95 answered yes to all 33 checks).

A ``kubectl auth whoami`` guard therefore asserts the evaluated identity is
exactly the ServiceAccount before any matrix check runs.

The ALLOWED matrix below is intentionally hard-coded rather than derived from
the manifest: if ``k8s/rbac.yaml`` changes, this file must change with it, so
a privilege widening shows up as a reviewable diff in both places.
"""

from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile

SA_NAMESPACE = "homelab-k3s-mcp"
SA_NAME = "homelab-k3s-mcp"
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


def sa_token() -> str:
    """Mint a short-lived token for the server's ServiceAccount."""
    proc = subprocess.run(
        ["kubectl", "-n", SA_NAMESPACE, "create", "token", SA_NAME, "--duration=10m"],
        capture_output=True,
        text=True,
    )
    if proc.returncode != 0 or not proc.stdout.strip():
        raise RuntimeError(f"create token failed: {proc.stderr.strip()!r}")
    return proc.stdout.strip()


def sa_kubeconfig(token: str) -> str:
    """Write a kubeconfig whose only credential is the ServiceAccount token.

    The cluster endpoint and CA come from the current (admin) kubeconfig; the
    admin's client certificate is deliberately left behind so authentication
    can only happen via the token.
    """
    proc = subprocess.run(
        ["kubectl", "config", "view", "--minify", "--raw", "-o", "json"],
        capture_output=True,
        text=True,
    )
    if proc.returncode != 0:
        raise RuntimeError(f"config view failed: {proc.stderr.strip()!r}")
    cluster = json.loads(proc.stdout)["clusters"][0]["cluster"]
    doc = {
        "apiVersion": "v1",
        "kind": "Config",
        "clusters": [{"name": "target", "cluster": cluster}],
        "users": [{"name": "sa", "user": {"token": token}}],
        "contexts": [{"name": "sa@target", "context": {"cluster": "target", "user": "sa"}}],
        "current-context": "sa@target",
    }
    fd, path = tempfile.mkstemp(prefix="rbac-sa-", suffix=".kubeconfig")
    with os.fdopen(fd, "w") as f:
        json.dump(doc, f)
    return path


def assert_identity(kubeconfig: str) -> None:
    """Fail fast unless queries will evaluate as the ServiceAccount itself."""
    proc = subprocess.run(
        ["kubectl", "--kubeconfig", kubeconfig, "auth", "whoami", "-o", "json"],
        capture_output=True,
        text=True,
    )
    if proc.returncode != 0:
        raise RuntimeError(f"auth whoami failed: {proc.stderr.strip()!r}")
    username = json.loads(proc.stdout)["status"]["userInfo"]["username"]
    expected = f"system:serviceaccount:{SA_NAMESPACE}:{SA_NAME}"
    if username != expected:
        raise RuntimeError(f"evaluating as {username!r}, expected {expected!r}")
    print(f"identity confirmed: {username}")


def can_i(kubeconfig: str, verb: str, resource: str, namespaced: bool) -> bool:
    cmd = ["kubectl", "--kubeconfig", kubeconfig, "auth", "can-i", verb, resource]
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


def diagnose(kubeconfig: str) -> None:
    """Print the full rules view for the ServiceAccount on unexpected answers."""
    listing = subprocess.run(
        ["kubectl", "--kubeconfig", kubeconfig, "auth", "can-i", "--list", "-n", NAMESPACE],
        capture_output=True,
        text=True,
    )
    print("--- can-i --list (as ServiceAccount token) ---", file=sys.stderr)
    print(listing.stdout, file=sys.stderr)


def main() -> None:
    failures: list[str] = []
    kubeconfig = sa_kubeconfig(sa_token())
    try:
        print(f"--- RBAC boundary as ServiceAccount {SA_NAMESPACE}/{SA_NAME} (token-only kubeconfig) ---")
        assert_identity(kubeconfig)

        allow_fail = 0
        for verb, resource, namespaced in ALLOWED:
            if not can_i(kubeconfig, verb, resource, namespaced):
                failures.append(f"expected ALLOW, got deny: {verb} {resource}")
                allow_fail += 1
        print(f"allowed matrix: {len(ALLOWED) - allow_fail}/{len(ALLOWED)} grants confirmed")

        deny_fail = 0
        for verb, resource, namespaced in DENIED:
            if can_i(kubeconfig, verb, resource, namespaced):
                failures.append(f"expected DENY, got allow: {verb} {resource}")
                deny_fail += 1
        print(f"denied matrix: {len(DENIED) - deny_fail}/{len(DENIED)} ungranted permissions confirmed absent")

        if failures:
            diagnose(kubeconfig)
            for f in failures:
                print("FAIL:", f, file=sys.stderr)
            raise SystemExit(1)
        print("rbac boundary ok")
    finally:
        os.unlink(kubeconfig)


if __name__ == "__main__":
    main()
