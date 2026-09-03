"""헬스·레디니스: 오케스트레이터가 가리키는 두 프로브 경로가 상태를 보고한다 (e2e).

검증 AC: platform-auth-safety/AC6
실행 대상: primary
실행 순서: 0

`tests/integration/check_ac_mapping.py`가 이 선언을 읽어 `docs/doc-tracker.md`의 레지스트리와
대조하고, `tests/integration/run_all.py`가 `실행 대상`을 읽어 이 파일을 배차한다.
프로브 확인은 다른 케이스들이 기대는 배포가 살아 있는지를 가장 먼저 말해 주므로
`실행 순서: 0` 으로 primary 그룹 앞머리에 둔다.
"""

from __future__ import annotations

from _helpers import base_url, get_json, wait_for_healthz


def test_platform_auth_safety_ac6_health_readiness(url: str) -> None:
    """AC: platform-auth-safety/AC6 — liveness and readiness probes report state.

    Asserts the two probe paths the orchestrator is pointed at (see the
    livenessProbe/readinessProbe/startupProbe in k8s/deployment.yaml) answer 200
    with their own status vocabulary on a healthy server: ``/healthz`` reports
    ``status=ok`` (liveness) and ``/readyz`` reports ``status=ready``
    (readiness). ``get_json`` raises on any non-2xx, so a probe path that
    disappeared or started erroring fails here.

    The unhealthy side of the criterion ("비정상 상태를 올바르게 반영") is not
    asserted: the deployed server offers no e2e-reachable way to force itself
    unready, and faking it would require a fixture that breaks the very
    deployment the other cases in this run share.
    """
    healthz = get_json(url, "/healthz")
    assert healthz.get("status") == "ok", f"unexpected /healthz: {healthz!r}"

    readyz = get_json(url, "/readyz")
    assert readyz.get("status") == "ready", f"unexpected /readyz: {readyz!r}"


def run() -> None:
    url = base_url()
    wait_for_healthz(url)

    print("--- health/readiness probes (AC: platform-auth-safety/AC6) ---")
    test_platform_auth_safety_ac6_health_readiness(url)
    print("probes ok")


if __name__ == "__main__":
    run()
