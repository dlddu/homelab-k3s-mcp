# PRD: opensearch_search

문서 저장소 docstore(`dlddu/docstore` — OpenSearch REST 부분집합을 말하는 EFS 파일 기반 저장소, 운영: `docstore` 네임스페이스)에서 문서를 전문(full-text) 검색하는 도구.

## 달성 가치
- **V4: 운영 지식의 축적·검색** — 축적된 운영 문서를 자연어 질의로 찾아 재사용한다.
- **V3: 안전한 운영(Safe-by-default)** — read-only 도구이며, 결과 상한과 미설정 시 graceful
  거부가 기본값이다.

## 도구 개요
- 입력: `query`(필수, 검색어), `index`(선택, 기본 전체 인덱스), `size`(선택, 기본 10, 최대 50)
- 동작: docstore 엔드포인트에 `simple_query_string` 검색 요청(OpenSearch `_search` 형식)을
  보낸다. 매칭 문서를 index·id·score·본문(`_source`)과 함께 반환한다.
- 서버 요구 설정: `OPENSEARCH_ENDPOINT` (docstore 기준 URL. 운영 값은 flux-cd-apps의
  ConfigMap `homelab-k3s-mcp-opensearch`가 준다.)
- 어노테이션: `readOnlyHint=true`, `idempotentHint=true`, `openWorldHint=true`

## Acceptance Criteria

### AC1: 질의 검색
- **설명**: `query`와 매칭되는 문서를 index·id·score·본문(`_source`)과 함께 반환한다.
  `index` 지정 시 해당 인덱스로 검색 범위를 한정하고, 미지정 시 컬렉션 전체 인덱스를
  대상으로 한다.
- **달성 가치**: V4
- **검증 방법**: 시드된 문서 중 질의어와 매칭되는 문서만 반환되고, 각 결과에 index·id·
  score·본문이 포함된다.

### AC2: 결과 상한
- **설명**: `size` 기본값은 10, 상한은 50이다. 상한을 초과하는 요청은 클램프하지 않고
  도구 에러로 거부한다.
- **달성 가치**: V3
- **검증 방법**: size 미지정 시 최대 10건이 반환되고, size=51 요청은 거부된다.

### AC3: docstore 접근(클러스터 내부 HTTP, 자격증명 없음)
- **설명**: 백엔드는 `OPENSEARCH_ENDPOINT`가 가리키는 docstore이며, 데이터 플레인 요청은
  클러스터 내부 HTTP로 서명 없이 보낸다. 서버는 이 경로를 위해 어떤 클라우드 자격증명도
  들거나 발급받지 않는다(이전 백엔드 OpenSearch Serverless의 AssumeRole·SigV4 경로는
  2026-10-30 제거). 운영에서 엔드포인트는 Traefik을 거치는 이름
  (`docstore-external.docstore.svc.cluster.local`)이라 scale-to-zero된 docstore를 Sablier가
  깨운다.
- **달성 가치**: V4
- **검증 방법**: 요청이 엔드포인트의 docstore에 도달하고, `Authorization`·
  `X-Amz-Security-Token` 헤더 없이 전송된다. Sablier 대기 응답(307 같은 URL)은 본문째 재시도된다.

### AC4: 미설정 시 graceful 거부
- **설명**: 필수 서버 설정이 없으면 unavailable 류 에러를 반환하며, 서버 기동·다른 도구에는
  영향을 주지 않는다.
- **달성 가치**: V3
- **검증 방법**: 관련 env가 비어 있을 때 unavailable 에러가 반환되고 서버는 계속 동작한다.
