# 테스트 문서: 이벤트 기록 (event log)

## 검증 대상 AC

- AC1: 호출당 한 레코드, 집계 가능한 필드 (PRD: 이벤트 기록)
- AC2: 거부 사유와 게이트 판정 (PRD: 이벤트 기록)
- AC3: 기록에 싣지 않는 것 (PRD: 이벤트 기록)
- AC4: 실행되지 않은 호출도 남는다 (PRD: 이벤트 기록)
- AC5: stdout 너머 보존, 그리고 수집기 미설정 시 graceful (PRD: 이벤트 기록)

> **시나리오 4·5 는 전용 e2e 로 닫혔고(2026-09-21 · `rct_20260921-0003`), 나머지 일곱은 ⏳ 구현 대기다.**
> 레코드 방출 지점은 2026-09-21 에 디스패처에 착지했고(AC1·AC3 — `msg="tool call"` 한 줄, 필드
> `tool`·`principal`·`target.*`·`result`), 같은 날 거부 사유와 게이트 판정이 필드로 더해졌다(AC2·AC4 —
> `reason`, `gate.request_id`·`gate.decision`·`gate.auto_approved`; 인증 실패도
> `principal=unauthenticated reason=auth_failed` 로 남는다). **AC5 의 수집 경로는 같은 날 배포 구성으로
> 확정했다**(`rct_20260921-0006` — 서버는 stdout 한 줄만 내고, 클러스터의 Grafana Alloy 가 전 파드 stdout 을
> Grafana Cloud Loki 로 보낸다; 선언은 `k8s/deployment.yaml` 파드 템플릿 라벨의 주석, 셀렉터는
> `{namespace="homelab-k3s-mcp", app="homelab-k3s-mcp", container="server"}`). 남은 일곱 중 시나리오
> 2·3·6·7·8 은 남은 차단이 전용 파일 저작뿐이다. 나머지의 해제 조건은 각 시나리오에 적는다. 등재는
> `doc-tracker/`의 ⏳ 표에 있다.

## 테스트 시나리오

### 시나리오 1: 호출당 한 레코드, 다섯 필드

- **사전 조건**: 이벤트 기록 표면 구현 · 인증된 세션
- **실행 단계**: 부류가 다른 도구 셋을 한 번씩 호출한다(리소스 좌표를 쓰는 `resource_get`,
  좌표가 없는 `ping`, 외부 시스템을 쓰는 `grafana_token`)
- **기대 결과**: 호출마다 **정확히 하나**의 레코드가 나오고, `{시각, 도구, 주체, 대상, 결과}`를
  모두 갖는다. 좌표가 없는 도구는 대상 필드가 비어 있되 **필드 자체는 존재**한다.
  같은 구간의 레코드 수가 `mcp_tool_calls_total` 증가분과 **같다** — 두 층의 수가 어긋나면
  집계의 기준이 둘이 된다
- **검증 AC**: AC1
- **자동화**: (미작성) — 레코드 방출 지점은 착지했다(2026-09-21). 남은 선행: 기대 결과의 등식 절이
  요구하는 `mcp_tool_calls_total`(`prd-metrics` AC1)

### 시나리오 2: 게이트 판정 여섯의 구분

- **사전 조건**: 표면 구현 · 실물 gatekeeper 픽스처(`tests/k8s/kind/`)
- **실행 단계**: 승인 · 거절 · 타임아웃 · 미설정 네 경로를 태운다
- **기대 결과**: 네 레코드의 판정 필드가 서로 다른 값으로 갈리고, `request_id`가 gatekeeper 쪽
  요청과 일치한다. 「거절」과 「통신 실패」가 같은 값으로 뭉치지 않는다
- **검증 AC**: AC2
- **자동화**: (미작성) — 선행 없음(AC2 착지 2026-09-21: `gate.decision` 은 `approved`·`rejected`·
  `expired`·`timeout`·`unreachable`·`unconfigured` 여섯 값, `gate.request_id` 동반; 실물 gatekeeper
  픽스처는 `tests/k8s/kind/gatekeeper-fixture.yaml`). 저작만 남았다

### 시나리오 3: `AUTO_APPROVE` 표기

- **사전 조건**: 표면 구현 · `AUTO_APPROVE`를 켠 배포 변형
- **실행 단계**: 게이트 대상 도구를 호출한다
- **기대 결과**: 레코드의 자동 승인 표기 필드가 참이다. 끈 변형에서는 거짓이고, 두 경우의
  판정 필드는 모두 「승인」이라 **표기 필드 없이는 구별되지 않는다**
- **검증 AC**: AC2
- **자동화**: (미작성) — 선행 없음(AC2 착지 2026-09-21: `gate.auto_approved` 필드; `AUTO_APPROVE`
  변형은 `approval_gate_ac9.py` 가 이미 태운다). 저작만 남았다

### 시나리오 4: 자격증명 값 비노출

- **사전 조건**: 표면 구현 · GitHub·Grafana·AWS 통합 구성
- **실행 단계**: 자격증명을 발급하는 도구 셋을 호출한 뒤 레코드 전문을 수집한다
- **기대 결과**: 발급된 토큰 값·API 키·JWT 원문을 **값 그대로 검색해 0건**이다
- **검증 AC**: AC3
- **자동화**: 통합 `tests/integration/event_log_ac3_credentials.py`(primary) — 발급 도구 셋을 부른 뒤
  SUT 파드 로그의 `msg="tool call"` 레코드를 모아, 응답에서 뽑은 발급 값·서버가 쥔 API 키·http-trace 가
  기록한 STS 발급 키·JWT 모양을 검색해 0건임을 단언한다(레코드가 도구당 정확히 하나 늘었고 응답에
  값이 있음을 먼저 단언한다). Go 단위 `internal/mcp/eventlog_test.go::TestRecordsCarryNoCredentialsPayloadsOrBodies`(가짜 통합)

### 시나리오 5: Secret 본문·스트림 페이로드 비노출

- **사전 조건**: 표면 구현 · 알려진 값을 담은 Secret · `resource_exec` 가능한 파드
- **실행 단계**: `resource_get(kind=Secret)` 승인 호출과 `resource_exec` 왕복을 태운다
- **기대 결과**: Secret 데이터 값과 exec 명령의 페이로드·응답 본문이 레코드에 없다.
  도구 응답에는 있고 레코드에는 없다는 **두 관측이 같은 실행에서** 나온다
- **검증 AC**: AC3
- **자동화**: 통합 `tests/integration/event_log_ac3_payloads.py`(gatekeeper-variant) — 자기 Secret 과 그것을
  마운트한 파드를 세우고 `AUTO_APPROVE` 로 승인된 `resource_get(kind=Secret)`·`resource_exec` 를 태운 뒤,
  같은 호출의 레코드에서 값·명령·본문을 검색해 0건임을 단언한다(응답에 base64 값과 stdout 이 있음을 먼저
  단언한다). Go 단위는 시나리오 4 와 같은 테스트

### 시나리오 6: 인증 실패도 레코드를 남긴다

- **사전 조건**: 표면 구현
- **실행 단계**: 잘못된 API 키와 만료된 JWT로 각각 호출한다
- **기대 결과**: 두 호출 모두 결과 필드가 거부이고 사유가 인증 실패인 레코드를 남긴다.
  **주체 필드에 제시된 자격증명 값이 실리지 않는다**(AC3과의 교차)
- **검증 AC**: AC4, AC3
- **자동화**: (미작성) — 선행 없음(AC4 착지 2026-09-21: 인증 층이 tools/call 본문의 401 마다
  `principal=unauthenticated result=refused reason=auth_failed` 한 줄을 남긴다; 만료 JWT 는 OIDC
  픽스처로 만든다). 저작만 남았다

### 시나리오 7: 검증·게이트·미설정 거부의 기록

- **사전 조건**: 표면 구현 · 통합 미설정 배포 변형
- **실행 단계**: 입력 검증 거부(잘못된 좌표), 게이트 거부, 미설정 거부를 각각 일으킨다
- **기대 결과**: 셋 다 레코드가 남고 사유가 서로 다르다. 클러스터·외부 시스템에 요청이
  **나가지 않았음**도 함께 관측된다
- **검증 AC**: AC4
- **자동화**: (미작성) — 선행 없음(AC2·AC4 착지 2026-09-21: 사유는 `invalid_input`·`gate_*`·
  `unconfigured` 로 갈리고 미설정 거부는 `result=refused` 다; 통합 미설정 변형은 `auth-variant`).
  저작만 남았다

### 시나리오 8: 수집기 미설정·도달 불가에서의 graceful

- **사전 조건**: 표면 구현 · 수집기 미설정 변형과 도달 불가 변형
- **실행 단계**: 두 변형에서 서버를 기동하고 도구를 호출한다
- **기대 결과**: 기동 성공, 도구 정상 응답, stdout 레코드 정상. **수집 실패가 도구 응답을
  실패로 만들지 않는다**
- **검증 AC**: AC5
- **자동화**: (미작성) — 선행 없음(AC5 착지 2026-09-21: 서버는 수집기와 연결을 갖지 않으므로 두 변형의
  서버 측 관측은 같다 — 기동·도구 응답·stdout 레코드. **kind 하네스가 곧 「수집기 미설정」 변형이다**(Alloy 가
  없다); 「도달 불가」 변형은 수집기(Alloy→Loki)의 장애이지 서버의 상태가 아니라, 서버 쪽에서 가를 구성 축이
  없는 것이 곧 이 설계의 fail-open 이다. `event_log_ac3_credentials.py` 가 같은 변형에서 이미 셋을 관측한다).
  저작만 남았다

### 시나리오 9: 파드 교체 뒤에도 남는다

- **사전 조건**: 표면 구현 · 수집 경로가 구성된 배포
- **실행 단계**: 호출을 남기고 파드를 롤링 재시작한 뒤, 재시작 이전 호출의 레코드를 수집
  경로에서 조회한다
- **기대 결과**: 재시작 전 레코드가 조회된다 — 「stdout 너머 보존」이 파드 수명과 무관함을
  이 시나리오가 단독으로 증명한다
- **검증 AC**: AC5
- **자동화**: (미작성) — 수집 경로는 배포됐으나(운영: Alloy → Grafana Cloud Loki) **kind 하네스에는 수집 경로가
  없다**(Loki·Alloy 픽스처 부재) — 해제 조건은 `doc-tracker/` ⏳ 표의 이 행. 운영 검증 절차: 교체 전 파드 이름을
  적어 두고 롤아웃 뒤 `grafana_token` 의 읽기 토큰으로
  `` {namespace="homelab-k3s-mcp", container="server", pod="<교체 전 파드>"} |= `msg="tool call"` `` 를 조회한다
  (2026-09-21 실측: 4시간 창에서 교체된 파드 5개의 레코드 196건 조회 — `| logfmt` 로 `result`·`reason` 이 라벨이 된다)

---

## 관련 문서

- PRD: [prd-event-log.md](prd-event-log.md)
- 가치: [values.md](values.md) V6
