"""카디널리티와 민감 라벨 — 좌표가 다른 호출도, 클러스터가 새 종류를 들여도 계열은 늘지 않는다.

검증 시나리오: test-metrics.md#시나리오 4
실행 대상: primary

이름은 ``resource-generic-fixture.yaml`` 의 CRD 와 그룹부터 다르게 둔다.
"""

from __future__ import annotations

import asyncio
import json
import subprocess

from _helpers import base_url, open_session, wait_for_healthz
from _metrics import Snapshot, assert_label_universe, metrics_url, scrape

SERVER_NAMESPACE = "homelab-k3s-mcp"
SERVER_DEPLOYMENT = "homelab-k3s-mcp"
METRICS_LOCAL_PORT = 19090

NAMESPACES = ("workload-test", "default")
COORDINATES = 30

CRD_GROUP = "metrics.homelab-k3s-mcp.test"
CRD_NAME = f"metricssamples.{CRD_GROUP}"
CR_KIND = "MetricsSample"
CR_NAMESPACE = "workload-test"
CR_NAME = "mt-s4-sample"

CRD = {
    "apiVersion": "apiextensions.k8s.io/v1",
    "kind": "CustomResourceDefinition",
    "metadata": {"name": CRD_NAME, "labels": {"app.kubernetes.io/part-of": "homelab-k3s-mcp-tests"}},
    "spec": {
        "group": CRD_GROUP,
        "scope": "Namespaced",
        "names": {"kind": CR_KIND, "listKind": f"{CR_KIND}List", "plural": "metricssamples", "singular": "metricssample"},
        "versions": [
            {
                "name": "v1",
                "served": True,
                "storage": True,
                "schema": {
                    "openAPIV3Schema": {
                        "type": "object",
                        "properties": {"spec": {"type": "object", "properties": {"note": {"type": "string"}}}},
                    }
                },
            }
        ],
    },
}


def _apply(manifest: dict) -> None:
    subprocess.run(["kubectl", "apply", "-f", "-"], input=json.dumps(manifest), text=True, check=True, capture_output=True)


def _install_crd() -> None:
    _apply(CRD)
    subprocess.run(
        ["kubectl", "wait", "--for=condition=established", "--timeout=60s", f"crd/{CRD_NAME}"],
        check=True,
        capture_output=True,
    )
    _apply(
        {
            "apiVersion": f"{CRD_GROUP}/v1",
            "kind": CR_KIND,
            "metadata": {"name": CR_NAME, "namespace": CR_NAMESPACE},
            "spec": {"note": "cardinality"},
        }
    )


def _remove_crd() -> None:
    subprocess.run(
        ["kubectl", "delete", "crd", CRD_NAME, "--ignore-not-found", "--wait=false"],
        check=False,
        capture_output=True,
    )


async def test_coordinates_do_not_add_series(session, metrics: str) -> Snapshot:
    before = scrape(metrics)
    for i in range(COORDINATES):
        await session.call_tool(
            "resource_get",
            {"apiVersion": "v1", "kind": "ConfigMap", "namespace": NAMESPACES[i % 2], "name": f"mt-s4-coordinate-{i}"},
        )
    after = scrape(metrics)
    assert after.series_count() == before.series_count(), (
        f"좌표만 다른 호출 {COORDINATES}회에 계열이 {before.series_count()} → {after.series_count()}"
    )
    assert_label_universe(after)
    return after


async def test_a_new_kind_does_not_add_series(session, metrics: str, before: Snapshot) -> None:
    _install_crd()
    result = await session.call_tool(
        "resource_get",
        {"apiVersion": f"{CRD_GROUP}/v1", "kind": CR_KIND, "namespace": CR_NAMESPACE, "name": CR_NAME},
    )
    assert result.isError is False, f"커스텀 리소스 조회가 실패했다 — 기동 뒤 들어온 종류를 서버가 모른다: {result}"
    assert CR_NAME in (result.content[0].text or ""), result.content

    after = scrape(metrics)
    assert after.series_count() == before.series_count(), (
        f"CRD 하나에 계열이 {before.series_count()} → {after.series_count()} — 종류가 라벨에 실렸다"
    )
    assert_label_universe(after)
    for needle in (CR_KIND, "metricssample", CR_NAME, CR_NAMESPACE):
        assert needle not in after.text, f"노출에 좌표 {needle!r} 가 있다"


async def run() -> None:
    url = base_url()
    wait_for_healthz(url)
    try:
        with metrics_url(SERVER_NAMESPACE, SERVER_DEPLOYMENT, METRICS_LOCAL_PORT) as metrics:
            async with open_session(url) as session:
                print(f"--- metrics/시나리오 4 (좌표만 다른 호출 {COORDINATES}회 → 계열 불변 · 라벨 전집) ---")
                snapshot = await test_coordinates_do_not_add_series(session, metrics)
                print("--- metrics/시나리오 4 (CRD 설치 + 커스텀 리소스 조회 → 계열 불변) ---")
                await test_a_new_kind_does_not_add_series(session, metrics, snapshot)
    finally:
        _remove_crd()
    print("ok: test-metrics.md#시나리오 4")


if __name__ == "__main__":
    asyncio.run(run())
