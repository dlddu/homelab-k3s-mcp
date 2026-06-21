# 테스트 문서: ping

## 검증 대상 AC
- AC1: 항상 pong 응답 (PRD: ping)

## 테스트 시나리오

### 시나리오 1: pong 반환
- **사전 조건**: 서버 기동, 세션 연결
- **실행 단계**: 인자 없이 `ping` 호출
- **기대 결과**: 에러 없이 `pong` 반환
- **검증 AC**: AC1
- **자동화**: Go 단위 `internal/server/mcp_test.go::TestPingToolReturnsPong`. 도구 노출은
  통합 `tests/integration/smoke.py`(tools/list)로 확인.
