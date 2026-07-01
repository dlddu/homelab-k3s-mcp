# PRD: opensearch_document_delete

OpenSearch Serverless 컬렉션(운영: `kubernetes-docs`)의 인덱스에서 지정 id의 문서 하나를
삭제하는 도구.

## 달성 가치
- **V4: 운영 지식의 축적·검색** — 낡거나 잘못된 지식을 정리하는 삭제 경로.
- **V2: 단명·최소권한 자격증명** — 정적 키 없이 AssumeRole 단명 자격증명으로 SigV4 서명
  접근한다.
- **V3: 안전한 운영(Safe-by-default)** — 파괴적 작업임을 명시하고, 노출 범위를 단일 문서
  삭제로 한정한다.

## 도구 개요
- 입력: `index`(필수), `id`(필수)
- 동작: SigV4(service `aoss`) 서명으로 해당 문서를 삭제하고 결과(deleted/not_found)를
  반환한다. 이 도구가 노출하는 삭제는 **단일 문서 삭제뿐**이며, 인덱스 삭제·delete-by-query는
  노출하지 않는다(부여된 데이터 액세스 정책은 `aoss:*`이지만 도구 표면에서 범위를 좁힌다).
- 서버 요구 설정: `OPENSEARCH_ENDPOINT`, `OPENSEARCH_ROLE_ARN` (opensearch_search와 공유)
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

### AC4: AssumeRole·SigV4 접근
- **설명**: 베이스 자격증명은 기본 체인에서 오고, 데이터 플레인 요청은 STS AssumeRole로
  얻은 단명 자격증명으로 SigV4 서명하여 수행한다. 정적 AWS 키를 사용하지 않는다.
- **달성 가치**: V2
- **검증 방법**: 접근 경로가 AssumeRole → SigV4 서명 요청이며 정적 키 환경변수에 의존하지
  않는다.

### AC5: 미설정 시 graceful 거부
- **설명**: 필수 서버 설정이 없으면 unavailable 류 에러를 반환하며, 서버 기동·다른 도구에는
  영향을 주지 않는다.
- **달성 가치**: V3
- **검증 방법**: 관련 env가 비어 있을 때 unavailable 에러가 반환되고 서버는 계속 동작한다.
