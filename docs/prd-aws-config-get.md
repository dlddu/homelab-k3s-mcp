# PRD: aws_config_get

서버에 고정된 S3 객체(AWS config)를 AssumeRole 경로로 조회하는 도구.

## 달성 가치
- **V2: 단명·최소권한 자격증명** — 정적 키 없이 AssumeRole로 얻은 단명 자격증명으로 고정 객체
  하나만 읽는다.
- **V3: 안전한 운영(Safe-by-default)** — 접근 대상이 서버 고정 bucket/key로 한정된다.

## 도구 개요
- 입력: 없음 (bucket/key가 서버에 고정)
- 동작: 기본 AWS 자격증명 체인(운영 환경의 인스턴스 프로파일)으로 `AWS_CONFIG_ROLE_ARN`을
  STS AssumeRole 후, 그 자격증명으로 객체를 GetObject
- 서버 요구 설정: `AWS_CONFIG_S3_BUCKET`, `AWS_CONFIG_S3_KEY`, `AWS_CONFIG_ROLE_ARN`
- 어노테이션: `readOnlyHint=true`, `idempotentHint=true`, `openWorldHint=true`

## Acceptance Criteria

### AC1: 고정 객체 조회
- **설명**: 서버 고정 bucket/key의 객체를 읽어 내용과 메타데이터(size, content type, ETag,
  last-modified)를 반환한다.
- **달성 가치**: V2
- **검증 방법**: 설정된 객체의 내용과 메타데이터가 반환된다(통합 테스트가 MinIO 기반 STS/S3
  픽스처로 검증).

### AC2: 정적 키 미사용
- **설명**: 베이스 자격증명은 기본 체인(인스턴스 프로파일)에서 오고, 실제 객체 접근은 STS
  AssumeRole로 얻은 단명 자격증명으로 수행한다. 정적 AWS 키를 사용하지 않는다.
- **달성 가치**: V2
- **검증 방법**: 접근 경로가 AssumeRole → GetObject이며 정적 키 환경변수에 의존하지 않는다.

### AC3: 미설정 시 graceful 거부
- **설명**: 필수 서버 설정이 없으면 unavailable 류 에러를 반환하며, 서버 기동·다른 도구에는
  영향을 주지 않는다.
- **달성 가치**: V3
- **검증 방법**: 관련 env가 비어 있을 때 unavailable 에러가 반환되고 서버는 계속 동작한다.
