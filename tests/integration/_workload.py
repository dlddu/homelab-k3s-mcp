"""workload-test 픽스처를 상대하는 파일들의 공유 표면 (매칭 단위가 아니다).

``run_all.py::matching_unit_paths()`` 가 ``_`` 접두 파일을 걸러내므로 이 모듈은 AC를
주검증하지 않고 ``검증 AC:`` 선언도 갖지 않는다. ``tests/k8s/kind/test-deployment.yaml``
픽스처의 이름·마커 계약과 kubectl 관측 헬퍼, 그리고 아래 **선행 조건 헬퍼**를 담는다.

## 선행 조건을 파일이 스스로 성립시킨다

2026-09-03 분할 전까지 ``workload.py`` 한 파일이 16개 AC를 겸용하며 케이스를 **고정된
순서**로 불렀다(읽기 전용 조회 → restart → scale(레플리카를 1로 되돌리고 기다린다) →
그 레플리카를 필요로 하는 logs·pod_describe). 러너가 파일별 프로세스로 돌리므로 그
순서를 파일 경계 너머로 옮길 수는 없고, 러너의 ``실행 순서:`` 로 고정하는 길은 결합을
파일 단위로 옮길 뿐 없애지 않는다.

그래서 **픽스처 상태를 필요로 하는 파일이 자기 선행 조건을 스스로 성립시킨다**:
``ensure_workload_fixture_baseline()`` 이 ``deploy/workload-fixture`` 를 매니페스트가
선언한 기준선(Ready 파드 정확히 1개)으로 멱등하게 되돌린다. 그 결과 이 도메인의 파일은
어느 것도 ``실행 순서:`` 를 선언하지 않는다 — 순서 의존이 남아 있지 않다는 뜻이다.

크래시루프 픽스처 쪽 결합은 처음부터 없었다: ``wait_for_crashloop_restart()`` ·
``wait_for_crashloop_log_age()`` 가 이미 멱등 폴링이라, 그것을 부르는 파일은 다른 파일이
무엇을 했는지와 무관하게 자기 전제를 성립시킨다.
"""

from __future__ import annotations

import datetime
import subprocess
import time

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


# Namespace the server itself is deployed in (k8s/serviceaccount.yaml + the
# ClusterRoleBinding in k8s/rbac.yaml). Read by the case that asserts an
# unscoped listing reaches past the fixture namespace, and by the
# RBAC-boundary case's impersonated identity.
SERVER_NAMESPACE = "homelab-k3s-mcp"


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


#: Replica count ``tests/k8s/kind/test-deployment.yaml`` declares for the fixture.
#: ``ensure_workload_fixture_baseline`` restores exactly this.
FIXTURE_REPLICAS = 1


def wait_for_single_ready_pod(selector: str, timeout: float = 120.0) -> None:
    """Block until exactly one pod matches ``selector`` and it is Ready.

    ``kubectl rollout status`` returning is not enough for the cases that resolve a
    pod *by selector*: it completes as soon as the new ReplicaSet is available, so a
    superseded pod can still be Terminating and still carry the label.
    ``pod_describe`` picks one pod out of that set, so a snapshot case asserting
    ``phase == Running`` and ``ready is True`` could observe the dying one.
    Requiring the set to have settled to a single Ready pod is what makes those
    assertions deterministic without depending on which file ran before this one.
    """
    deadline = time.monotonic() + timeout
    last = "<no pods yet>"
    while time.monotonic() < deadline:
        proc = subprocess.run(
            [
                "kubectl",
                "-n",
                NAMESPACE,
                "get",
                "pod",
                "-l",
                selector,
                "-o",
                "jsonpath={range .items[*]}{.metadata.deletionTimestamp}|"
                "{range .status.conditions[?(@.type=='Ready')]}{.status}{end};{end}",
            ],
            capture_output=True,
            text=True,
        )
        last = proc.stdout.strip() or proc.stderr.strip()
        if proc.returncode == 0:
            pods = [entry for entry in proc.stdout.strip().split(";") if entry]
            # One pod, no deletionTimestamp, Ready=True.
            if pods == ["|True"]:
                return
        time.sleep(2)
    raise RuntimeError(
        f"pods matching {selector!r} in {NAMESPACE} never settled to a single Ready"
        f" pod within {timeout:.0f}s (last observation: {last!r})"
    )


def ensure_workload_fixture_baseline() -> None:
    """Bring ``deploy/workload-fixture`` back to its declared baseline, idempotently.

    This is the precondition every file that reads or mutates the fixture opens
    with, so no file depends on the cluster state another file left behind (see the
    module docstring). Already-at-baseline is the common case and costs two kubectl
    reads: the scale patch is only issued when ``.spec.replicas`` disagrees.

    kubectl rather than the ``workload_scale`` tool on purpose — a precondition
    established through the tool under test would make the setup depend on the very
    behaviour a case is there to falsify.

    Deliberately *not* hidden inside a session helper: it appears in the ``run()``
    of each file that needs it, so the set of files carrying this precondition is
    greppable.
    """
    if kubectl_jsonpath("{.spec.replicas}") != str(FIXTURE_REPLICAS):
        subprocess.run(
            [
                "kubectl",
                "-n",
                NAMESPACE,
                "scale",
                f"deploy/{WORKLOAD}",
                f"--replicas={FIXTURE_REPLICAS}",
            ],
            check=True,
        )
    kubectl_wait_rollout()
    wait_for_single_ready_pod(f"app={WORKLOAD}")
