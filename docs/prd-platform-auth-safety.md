# PRD: platform — 인증·안전 기반 (공통)

특정 도구에 속하지 않고 **모든 도구에 공통으로 적용**되는 인증·권한 경계·런타임 안전에 대한
횡단 요구사항. (도구별 PRD와 달리, 이 문서는 도구들이 안전하게 동작하기 위한 공통 토대를
정의한다.)

## 달성 가치
- **V3: 안전한 운영(Safe-by-default)** — 무인증 접근·과도한 권한·통합 미설정으로 인한 다운을
  기본적으로 차단하고, 가능한 피해 범위를 구조적으로 제한한다.

## 범위
- **포함**: OAuth Bearer 인증 게이트, 인증 메타데이터 디스커버리, 클러스터 RBAC 최소권한,
  하드닝된 런타임, 서버 수준 graceful degradation, 헬스/레디니스.
- **제외**: 사용자·토큰 발급(외부 IdP 책임), 네트워크 인그레스 정책(클러스터 책임),
  도구별 동작(각 도구 PRD 참조).

## Acceptance Criteria

### AC1: 인증 게이트
- **설명**: `MCP_AUTH_DISABLED`가 설정되지 않은 한, `/mcp`는 JWKS로 서명 검증되는 유효한
  Bearer JWT 없이는 요청을 거부한다.
- **달성 가치**: V3
- **검증 방법**: 토큰 없음/무효 요청은 401로 거부되고 `WWW-Authenticate`가 보호 리소스
  메타데이터를 가리키며, 유효 토큰 요청은 정상 처리된다.

### AC2: 인증 디스커버리
- **설명**: `/.well-known/oauth-protected-resource`가 발급자/리소스를 광고하고, OIDC
  discovery(`/.well-known/openid-configuration`)로 JWKS를 동적 로드한다.
- **달성 가치**: V3
- **검증 방법**: 보호 리소스 메타데이터가 발급자/리소스를 반환하고, 표준 MCP 클라이언트가 이를
  통해 인증을 자동 구성할 수 있다.

### AC3: 최소권한 권한 경계 (RBAC)
- **설명**: 배포된 RBAC는 워크로드에 `get/list/watch/patch`, 파드에 `get/list`, `pods/log`에
  `get`, `pods/exec`에 `get/create`, 네임스페이스·이벤트에 `get/list`만 부여한다. 워크로드
  `delete`/`create`, 시크릿 읽기 권한은 부여하지 않는다.
- **달성 가치**: V3
- **검증 방법**: `k8s/rbac.yaml`에 delete/secret/워크로드 create 규칙이 존재하지 않으며, 도구가
  수행 가능한 최대 동작이 위 동사 집합으로 제한된다.

### AC4: 하드닝된 런타임
- **설명**: 컨테이너는 nonroot 사용자, `readOnlyRootFilesystem`, 모든 capability 드롭,
  `seccompProfile: RuntimeDefault`로 구동된다(distroless nonroot 이미지, 정적 바이너리).
- **달성 가치**: V3
- **검증 방법**: 배포 매니페스트의 securityContext가 위 설정을 포함하고 컨테이너가 비특권으로
  정상 기동한다.

### AC5: 서버 수준 graceful degradation
- **설명**: 통합(GitHub/AWS/Grafana/k8s) 일부가 미설정이어도 서버는 정상 기동하고
  `tools/list`도 정상 응답한다. (도구별 unavailable 동작은 각 도구 PRD에 정의)
- **달성 가치**: V3
- **검증 방법**: 자격증명 env를 비운 채 기동해도 서버가 떠 있고 `tools/list`가 정상 응답한다.

### AC6: 헬스·레디니스
- **설명**: `/healthz`(liveness)·`/readyz`(readiness)와 startup 프로브를 제공해 오케스트레이터가
  서버 상태를 올바르게 판단하게 한다.
- **달성 가치**: V3
- **검증 방법**: 각 프로브 경로가 정상/비정상 상태를 올바르게 반영한다.

## 비기능 요구사항
- `MCP_AUTH_DISABLED`는 신뢰된 네트워크의 로컬 개발/테스트 용도로만 사용하며 운영 배포에서는
  인증 활성을 기본으로 한다.
- 비프로브 HTTP 요청은 메서드·경로·상태·소요시간이 로깅되어 최소한의 접근 가시성을 남긴다.
