# 테스트 문서: 메트릭 (metrics)

## 검증 대상 AC

- AC1: 도구별 호출 수 (PRD: 메트릭)
- AC2: 거부 사유별 카운터 (PRD: 메트릭)
- AC3: 처리 지연 (PRD: 메트릭)
- AC4: 라벨에 싣지 않는 것 (PRD: 메트릭)
- AC5: 노출 경로와 인증 경계 (PRD: 메트릭)

> **전 시나리오가 전용 e2e 로 닫혔다(2026-09-21 · `rct_20260921-0010`).** 표면은 같은 날 섰고(`internal/metrics` — 네 계열 ·
> `:9090/metrics` 별도 리스너 · 단위 테스트가 AC1~AC5 를 레코드 층에서 단언한다 — `rct_20260921-0008`), 다섯 파일은
> 배포된 서버를 **파드의 9090 포트로** 스크레이프한다(`tests/integration/_metrics.py` — 변형 배포의 Service 에는
> `metrics` 포트가 없어 파드 포트를 직접 연다). 3 의 「승인이 수 초 걸리는」 상태는 지연 주입 픽스처가 아니라
> **승인자의 지연**으로 만든다 — 실물 gatekeeper 앞에서 호출은 판정까지 블록하고, 판정은 파일이 사람 대신 내리므로
> 승인 전에 기다린 시간이 곧 게이트 대기다. 하네스 밖 관측: 미등록 이름의 `tool="unregistered"` 접힘은
> `internal/metrics/metrics_test.go`.

## 테스트 시나리오

### 시나리오 1: 결과별 카운터와 미호출 도구의 0

- **사전 조건**: 메트릭 표면 구현 · 스크레이프 가능한 배포
- **실행 단계**: 성공·거부·에러를 각각 한 번씩 일으키고 스크레이프한다
- **기대 결과**: 세 결과 라벨의 카운터가 각각 1이고 서로 섞이지 않는다. **한 번도 호출되지
  않은 등록 도구도 0으로 노출된다** — 계열 부재와 0이 구별된다
- **검증 AC**: AC1
- **자동화**: `tests/integration/metrics_ac1_results.py`(primary · 2026-09-21 `rct_20260921-0010`) — `ping`(성공) · `kind` 없는
  `resource_get`(거부 `invalid_input`) · 없는 ConfigMap 의 `resource_get`(에러) 를 한 번씩 만들고 스크레이프 전후 차로
  세 (도구, 결과) 계열이 각 1, 같은 도구의 다른 결과 계열이 0 임을 단언한다. 「미호출 도구의 0」은 파드 stdout 레코드에
  한 번도 나오지 않은 등록 도구 집합을 계산해 그 도구들의 세 결과 계열이 **값 0 으로 존재**함으로 잰다 — 그런 도구가
  남아 있도록 두 스모크 바로 뒤 장벽 자리(`실행 순서: 2`)에서 돈다

### 시나리오 2: 거부 사유가 갈린다

- **사전 조건**: 표면 구현 · 실물 gatekeeper 픽스처 · 통합 미설정 변형
- **실행 단계**: 인증 실패 · 입력 검증 · 게이트 거절 · 게이트 미설정 · 통합 미설정을 각각
  일으킨다
- **기대 결과**: 사유 라벨이 다섯으로 갈리고 각각 1이다. **「사람이 거절」과 「게이트 통신
  실패」가 같은 라벨로 합쳐지지 않는다**
- **검증 AC**: AC2
- **자동화**: `tests/integration/metrics_ac2_reasons.py`(auth-variant · 2026-09-21 `rct_20260921-0010`) — 넷은 이 변형이 한 배포로
  낸다(잘못된 bearer 의 원시 `tools/call` → `auth_failed` · `kind` 없는 좌표 → `invalid_input` · `grafana_token` →
  `unconfigured` · 백엔드 없는 `resource_patch` → `gate_unconfigured`), 다섯째 `gate_rejected` 는 gatekeeper-variant 에
  짧은 포트포워드로 닿아 `resource_patch` 를 띄우고 forward-auth 헤더로 거절한다. 각 배포의 `mcp_tool_refusals_total`
  차에서 다섯 사유가 각 1 이고, 거절 배포의 `gate_unreachable` 차가 0 이며, 배포마다 sum(refusals) 의 차 ==
  `mcp_tool_calls_total{result="refused"}` 의 차(AC2 불변식)

### 시나리오 3: 게이트 대기와 처리 지연의 분리

- **사전 조건**: 표면 구현 · 승인을 일부러 지연시킬 수 있는 gatekeeper 픽스처
- **실행 단계**: 즉답 도구(`ping`)와 승인이 수 초 걸리는 게이트 대상 도구를 호출한다
- **기대 결과**: 두 계열이 따로 기록되고, 승인 대기가 길어져도 **처리 지연 계열은 그만큼
  커지지 않는다**
- **검증 AC**: AC3
- **자동화**: `tests/integration/metrics_ac3_gate_wait.py`(primary · 2026-09-21 `rct_20260921-0010`) — `ping` 한 번(처리 지연 계열에
  1 관측 · 대기 계열 무이동), 이어 workload-test 의 ConfigMap 에 `resource_patch` 를 띄워 승인 요청이 보인 뒤 **3초를
  기다렸다가** 승인한다. `mcp_gate_wait_seconds{decision="approved"}` 가 1 관측 · 합 ≥ 3.0 이고
  `mcp_tool_duration_seconds{tool="resource_patch"}` 가 1 관측 · 합 < 1.5 (대기의 절반 미만) — 대기가 처리 지연에
  실리지 않았다. 레코드의 `gate.decision=approved` 로 그 호출이 게이트를 지났음을 함께 확인한다

### 시나리오 4: 카디널리티와 민감 라벨

- **사전 조건**: 표면 구현
- **실행 단계**: ⑴ 좌표(kind·이름·네임스페이스)만 다른 호출을 수십 회 반복하고 스크레이프한다.
  ⑵ 이어서 **CRD를 하나 설치하고 그 커스텀 리소스를 조회**한 뒤 다시 스크레이프한다
- **기대 결과**: 두 경우 모두 시계열 수가 **늘지 않는다**. ⑵가 이 시나리오의 무게중심이다 —
  `kind`가 라벨이면 클러스터가 CRD를 들일 때마다 계열이 늘고, 그 증가는 서버가 통제하지
  못한다. 노출된 라벨 이름 집합이 정확히 `tool`·`result`·`reason` 셋이고, 리소스 좌표·주체
  식별자·경로·명령이 라벨 값 어디에도 없다
- **검증 AC**: AC4
- **자동화**: `tests/integration/metrics_ac4_cardinality.py`(primary · 2026-09-21 `rct_20260921-0010`) — ⑴ 네임스페이스·이름만 다른
  `resource_get` 30회 뒤 계열 수 불변 ⑵ 파일이 CRD(`metricssamples.metrics.homelab-k3s-mcp.test`)를 세우고 established
  를 기다려 커스텀 리소스를 하나 만든 뒤 `resource_get` 으로 조회(성공)하고 다시 스크레이프 — 계열 수 불변, 노출에
  종류·이름·네임스페이스 문자열 0. 라벨은 `_metrics.assert_label_universe` 로 잰다: 패밀리 넷 · 라벨 이름
  `tool`·`result`·`reason` + 히스토그램 구조 라벨 `decision`·`le` · 값은 전부 서버 상수(등록 도구 ∪ `unregistered` ·
  Results · Reasons · Verdicts · 버킷 경계)의 닫힌 전집 안. CRD 는 `finally` 에서 거둔다

### 시나리오 5: 노출 경로가 인증 경계를 우회하지 않는다

- **사전 조건**: 표면 구현 · 인증이 켜진 기본 배포와 노출을 끈 변형
- **실행 단계**: 메트릭 경로를 인증 없이 호출하고, 그 응답으로 도구 실행·리소스 조회가
  가능한지 살핀다. 이어서 노출을 끈 변형에서 기동·도구 호출을 확인한다
- **기대 결과**: 메트릭 응답은 AC4의 라벨 집합만 담고 도구를 실행하지 않으며 클러스터
  리소스를 반환하지 않는다. `/mcp`는 여전히 인증 없이는 401이다. 노출을 끈 변형에서도
  서버와 도구는 정상이다
- **검증 AC**: AC5
- **자동화**: `tests/integration/metrics_ac5_boundary.py`(auth-variant · 2026-09-21 `rct_20260921-0010`) — 인증이 켜진 이 변형에서
  무인증 `GET /metrics` 가 200 이되 본문의 모든 샘플이 네 패밀리의 것이고 라벨 전집이 닫혀 있으며, 같은 리스너의
  `/mcp` 는 404(`server.MetricsApp` 의 mux 에 `GET /metrics` 한 경로), 본 배포의 `/mcp` 는 무인증 401 · 키로 `pong`.
  「노출을 끈 변형」은 파일이 `tests/k8s/kind/metrics-off-variant.yaml`(auth-fixture 와 같은 구성 + `METRICS_DISABLED=1`)
  을 apply 해 세운다 — 로그의 `METRICS_DISABLED is set` 줄로 끈 것이 실재함을 먼저 확인하고, 가용 레플리카 ≥ 1 ·
  재시작 0 · `ping` → `pong` · 파드 9090 으로의 GET 이 응답 없이 실패함을 잰 뒤 네임스페이스를 거둔다

---

## 관련 문서

- PRD: [prd-metrics.md](prd-metrics.md)
- 가치: [values.md](values.md) V6
