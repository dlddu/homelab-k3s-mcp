# 테스트 문서: dear_baby_reset_user

## 검증 대상 AC
- AC1: 온보딩 리셋 실행 (PRD: dear_baby_reset_user)
- AC2: 명시적 대상 지정 (PRD: dear_baby_reset_user)
- AC3: 파괴적 작업 표기 (PRD: dear_baby_reset_user)

## 테스트 시나리오

### 시나리오 1: 리셋 성공/실패 exec
- **사전 조건**: `dear-baby-test`에 dear-baby 백엔드 픽스처 파드 실행 중
- **실행 단계**: (a) 존재하는 이메일로 호출, (b) 존재하지 않는 이메일로 호출
- **기대 결과**: (a) 대상 파드(`dear-baby-*`)에서 `/reset-user` exec, exitCode=0,
  stdout에 "reset user <email>", success=true. (b) exitCode=1, stderr "no user found",
  success=false(도구 에러).
- **검증 AC**: AC1
- **자동화**: Go 단위 `mcp_test.go::TestDearBabyResetDispatchesWithDefaults`,
  `TestDearBabyResetReportsNonZeroExit`. 통합
  `dear_baby_reset_user_ac1.py::test_dear_baby_reset_user_ac1_reset_execution`(success / failure path).
  **단, 온보딩 필드의 값 자체는 이 e2e가 단정하지 않는다** — 픽스처는 2026-09-26(#237)부터
  busybox 스텁이 아니라 sha 핀 실 백엔드 + 기동 시 마이그레이션·시드가 도는 SQLite
  (`tests/k8s/kind/dear-baby.yaml`)이므로 필드 초기화는 실제로 일어나며, 성공
  (`user@example.com`)·미발견(`missing@example.com`) 두 경로가 그 DB를 실제로 읽는다는 것까지는
  단정한다. 필드 값과 "레코드는 보존된다"를 읽으려면 DB를 열어야 하는데 그 이미지에는 바이너리가
  `/dear-baby-backend`·`/reset-user` 둘뿐이고 셸도 sqlite 클라이언트도 없어 `pods/exec`로는
  읽히지 않는다 — `resource_proxy`로 앱 HTTP API(로그인 → 온보딩 조회)를 경유하는 별도 시나리오가
  선결이다.

### 시나리오 2: 대상 지정(이메일 필수, 셀렉터/컨테이너 기본·재정의)
- **사전 조건**: 동일
- **실행 단계**: email 누락 호출 / 기본값 호출 / selector 재정의(없는 셀렉터) 호출
- **기대 결과**: email 누락은 거부. 기본 selector=`app=dear-baby`·container=`backend` 사용.
  매칭 Running 파드 없으면 "no Running pod matched" 도구 에러.
- **검증 AC**: AC2
- **자동화**: Go 단위 `mcp_test.go::TestDearBabyResetRequiresNamespaceAndEmail`,
  `TestDearBabyResetHonoursOverrides`. 통합
  `dear_baby_reset_user_ac2.py::test_dear_baby_reset_user_ac2_explicit_target`(email 누락 → `McpError:
  email is required`, 기본 selector·container 에코, selector 재정의 → no Running pod,
  container 재정의 → 실패 = 재정의가 실제로 반영됨의 판별자).

### 시나리오 3: 파괴적 어노테이션 광고
- **사전 조건**: 서버 기동
- **실행 단계**: `tools/list` 조회
- **기대 결과**: `dear_baby_reset_user`가 `destructiveHint=true`로 광고됨
- **검증 AC**: AC3
- **자동화**: Go 단위 `mcp_test.go::TestToolsListAdvertisesDearBabyReset`. 통합
  `dear_baby_reset_user_ac3.py::test_dear_baby_reset_user_ac3_destructive_hint`.
