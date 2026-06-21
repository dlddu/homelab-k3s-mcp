# 테스트 문서: dear_baby_reset_user

## 검증 대상 AC
- AC1: 온보딩 리셋 실행 (PRD: dear_baby_reset_user)
- AC2: 명시적 대상 지정 (PRD: dear_baby_reset_user)
- AC3: 파괴적 작업 표기 (PRD: dear_baby_reset_user)

## 테스트 시나리오

### 시나리오 1: 리셋 성공/실패 exec
- **사전 조건**: `dear-baby-test`에 dear-baby 백엔드 픽스처 파드 실행 중
- **실행 단계**: (a) 존재하는 이메일로 호출, (b) 존재하지 않는 이메일로 호출
- **기대 결과**: (a) 대상 파드(`dear-baby-fixture-*`)에서 `/reset-user` exec, exitCode=0,
  stdout에 "reset user for ...", success=true. (b) exitCode=1, stderr "no user found",
  success=false(도구 에러).
- **검증 AC**: AC1
- **자동화**: Go 단위 `mcp_test.go::TestDearBabyResetDispatchesWithDefaults`,
  `TestDearBabyResetReportsNonZeroExit`. 통합 `dear_baby.py`(success / failure path).

### 시나리오 2: 대상 지정(이메일 필수, 셀렉터/컨테이너 기본·재정의)
- **사전 조건**: 동일
- **실행 단계**: email 누락 호출 / 기본값 호출 / selector 재정의(없는 셀렉터) 호출
- **기대 결과**: email 누락은 거부. 기본 selector=`app=dear-baby`·container=`backend` 사용.
  매칭 Running 파드 없으면 "no Running pod matched" 도구 에러.
- **검증 AC**: AC2
- **자동화**: Go 단위 `mcp_test.go::TestDearBabyResetRequiresNamespaceAndEmail`,
  `TestDearBabyResetHonoursOverrides`. 통합 `dear_baby.py`(no Running pod path).

### 시나리오 3: 파괴적 어노테이션 광고
- **사전 조건**: 서버 기동
- **실행 단계**: `tools/list` 조회
- **기대 결과**: `dear_baby_reset_user`가 `destructiveHint=true`로 광고됨
- **검증 AC**: AC3
- **자동화**: Go 단위 `mcp_test.go::TestToolsListAdvertisesDearBabyReset`.
