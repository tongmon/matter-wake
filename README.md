# matter-wake

픽셀 재질 상호작용과 채굴·건축을 결합한 2D 게임입니다. Go와 Ebitengine으로 만듭니다.

- [설계 문서](docs/design.md)
- [로드맵](docs/roadmap.md)
- [레퍼런스와 측정 기록](docs/references.md)
- [알려진 함정](docs/pitfalls.md)
- 에이전트 작업 규칙: [AGENTS.md](AGENTS.md)

## 실행

```
go run ./cmd/matter-wake
```

- `-seed N`: 월드 시드 (기본: 새 시드를 만들고 시작할 때 로그에 출력)
- `-scale N`: 창 배율 (기본 2)
- `-width`, `-height`: 월드 크기(셀) (기본 480×270)
- `-workers N`: 한 틱에 쓸 고루틴 수 (기본: CPU 수)
- `-materials PATH`: 재질 파일 (기본 `data/materials.json`)
- `-shots 0,60,300`: 지정한 틱의 화면을 PNG로 저장하고 마지막 틱 뒤에 종료. 입력은 무시함
- `-shots-dir DIR`: PNG 저장 폴더 (기본 `out/shots`)
- `-record PATH`: 이번 세션의 입력을 종료할 때 리플레이 파일로 저장
- `-replay PATH`: 리플레이 파일을 재생하고 끝나면 종료. 월드 크기와 시드는 리플레이가 정하고, 입력 장치는 무시함. `-shots`와 함께 쓸 수 있음

조작 (임시): 왼쪽 클릭 놓기, 오른쪽 클릭 파기, 1~3 재질 선택(모래, 물, 돌).

## 검사

```
go run ./tools/check
```

포맷, `go mod tidy`, 린트, 테스트, (cgo가 있으면) 경합 검사를 한 번에 돌립니다. `-fix`는 포맷을 먼저 고치고, `-vuln`은 의존성 취약점 검사를 더합니다.

```
go run ./tools/review
```

커밋한 브랜치를 Codex로 리뷰하고 `out/codex-review-N.txt`에 저장합니다. Codex는 저장소, 임시 폴더, Go 빌드 캐시에만 쓸 수 있어 테스트를 직접 돌려 볼 수 있고, 끝나면 남긴 파일이 없는지 확인합니다.
