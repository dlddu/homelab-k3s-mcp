# PRD: opensearch_document_delete

OpenSearch REST 호환 백엔드(운영: docstore — `dlddu/docstore`, EFS 파일 기반 · OpenSearch REST 부분집합. OpenSearch 자체로도 교체 가능)의 인덱스에서 지정 id의 문서 하나를
삭제하는 도구.

## 달성 가치
- **V4: 운영 지식의 축적·검색** — 낡거나 잘못된 지식을 정리하는 삭제 경로.
- **V2: 단명·최소권한 자격증명** — 백엔드가 SigV4를 요구하면(AWS 관리형) 정적 키 없이 AssumeRole
  단명 자격증명으로 서명한다(선택 모드).
- **V3: 안전한 운영(Safe-by-default)** — 파괴적 작업임을 명시하고, 노출 범위를 단일 문서
  삭제로 한정한다.

## 도구 개요
- 입력: `index`(필수), `id`(필수)
- 동작: 백엔드에 해당 문서를 삭제하고 결과(deleted/not_found)를
  반환한다. 이 도구가 노출하는 삭제는 **단일 문서 삭제뿐**이며, 인덱스 삭제·delete-by-query는
  노출하지 않는다(운영 백엔드 docstore 는 그것들을 구현하지도 않는다).
- 서버 요구 설정: `OPENSEARCH_ENDPOINT` · 인증 설정 (opensearch_search와 공유)
- 어노테이션: `readOnlyHint=false`, `destructiveHint=true`, `idempotentHint=true`,
  `openWorldHint=true`
  (idempotent=true: 같은 삭제의 재호출은 not_found로 수렴하며 상태가 동일하다)

## Acceptance Criteria

### AC1: 단일 문서 삭제
- **설명**: 지정한 index/id의 문서만 삭제하고 deleted를 반환한다. 같은 인덱스의 다른
  문서에는 영향을 주지 않으며, 삭제된 문서는 이후 검색에 노출되지 않는다.
- **달성 가치**: V4
- **검증 방법**: 시드 문서 2건 중 1건 삭제 후, 삭제 대상만 검색에서 사라지고 나머지는
  유지된다.

### AC2: 부재 문서의 명확한 처리
- **설명**: 존재하지 않는 index/id에 대한 삭제는 not_found를 명확한 결과로 반환하며,
  서버·다른 도구에는 영향을 주지 않는다.
- **달성 가치**: V3
- **검증 방법**: 없는 id 삭제 호출이 not_found로 응답하고 서버는 계속 동작한다.

### AC3: 파괴적 작업 표기
- **설명**: `tools/list`에서 이 도구가 `destructiveHint=true`로 광고된다.
- **달성 가치**: V3
- **검증 방법**: `tools/list` 응답의 어노테이션이 destructiveHint=true이다.

### AC4: 백엔드 접근(OpenSearch REST 호환 · 인증 모드 설정)
- **설명**: 백엔드는 `OPENSEARCH_ENDPOINT`가 가리키는 OpenSearch REST 호환 서버이며(운영:
  docstore), 도구는 백엔드 종류를 모른다 — `_search`(simple_query_string)·`_doc` 와이어 형식만
  쓴다. 인증은 설정으로 고르며 코드 변경 없이 바꿀 수 있다: 없음(기본 — 클러스터 내부
  docstore 또는 security off OpenSearch) · basic · SigV4(STS AssumeRole 단명 자격증명, service
  `es`|`aoss`, 정적 AWS 키 미사용). 모드가 모호한 설정(basic과 SigV4 동시, basic 한쪽만,
  알 수 없는 서명 서비스)은 시작 시 unavailable로 거부한다. 운영 엔드포인트는 Traefik을 거치는
  이름(`docstore-external.docstore.svc.cluster.local`)이라 scale-to-zero된 docstore를 Sablier가
  깨우며, 대기 응답(307 같은 URL)은 본문째 재시도된다.
- **달성 가치**: V4 (SigV4 모드는 V2)
- **검증 방법**: 기본 모드에서 요청이 백엔드에 `Authorization`·`X-Amz-Security-Token` 없이
  도달한다. basic 모드는 Basic 헤더를, SigV4 모드는 지정 서비스 스코프의 AssumeRole 자격증명
  서명을 싣는다. 모호한 설정은 거부된다.

### AC5: 미설정 시 graceful 거부
- **설명**: 필수 서버 설정이 없으면 unavailable 류 에러를 반환하며, 서버 기동·다른 도구에는
  영향을 주지 않는다.
- **달성 가치**: V3
- **검증 방법**: 관련 env가 비어 있을 때 unavailable 에러가 반환되고 서버는 계속 동작한다.
