# 알려진 함정

새 함정을 만나면 여기에 추가합니다. 코드 주석이나 커밋 메시지에만 남기면 다음 작업자가 찾지 못합니다.

## Go

- **`math/rand`는 게임 로직에서 쓰지 않습니다.** Go 버전마다 알고리즘과 시드 처리가 바뀔 수 있어 리플레이와 골든 테스트가 깨집니다. `internal/rng`를 씁니다.
- **핫루프에서 클로저나 메서드 값을 매 호출 만들면 할당이 생깁니다.** `w.stepChunk`처럼 메서드 값을 필드에 한 번 저장해 두고 씁니다(`world.World.stepChunkFn`).
- **고정소수점 곱셈을 산술 시프트(`>> 8`)로 나누면 음수 쪽으로 치우칩니다.** 시프트는 음의 무한대 쪽으로 내림이라, 감쇠를 반복하면 양수는 0이 되지만 음수는 -1/256에서 멈춰 왼쪽으로 흐르는 힘이 사라지지 않습니다. `fixed.Q8.Mul`은 0 쪽으로 자르는 정수 나눗셈을 씁니다(`TestMulIsSymmetric`).
- **`json.Decoder.More()`로는 뒤에 붙은 데이터를 다 잡지 못합니다.** 배열·객체의 다음 원소만 보기 때문에, 문서 뒤에 붙은 `]`나 `}`는 통과합니다. 한 번 더 `Decode`해서 `io.EOF`인지 확인합니다(`internal/material`, 2026-11-06 Codex 리뷰).
- **Go float 계산은 아키텍처마다 마지막 비트가 다를 수 있습니다.** 언어 명세가 `a + x*y`를 FMA 하나로 합치는 것을 허용하고, Go 컴파일러는 arm64(Apple Silicon Mac)에서는 기본으로 합치지만 amd64에서는 `GOAMD64=v3` 이상일 때만 합칩니다(`cmd/compile/internal/ssa/_gen/ARM64.rules`, `AMD64.rules`). 셀 시뮬레이션처럼 임계값 비교가 많은 코드에서는 한 비트 차이로 궤적이 갈라져 골든 테스트와 리플레이가 OS마다 달라집니다. 시뮬레이션 상태는 정수로 두고, float를 꼭 써야 하면 곱셈 결과를 `float32(x*y) + z`처럼 명시적으로 변환해 합치기를 막습니다.
- **`out/` 아래에 `.go` 파일을 두지 않습니다.** `out/`은 git에서는 제외되지만 Go 모듈 안이라, `go test ./...`와 린트가 그 파일까지 컴파일합니다. 임시 실험 코드는 저장소 밖(세션 scratchpad)의 자기 이름 폴더에 둡니다(2026-11-06, skill 평가 실행이 남긴 재현 코드로 검사가 깨짐).
- **`go test`의 캐시는 테스트가 연 파일만 추적합니다.** 테스트가 저장소 밖의 무언가에 의존하면 `-count=1`로 돌립니다.
- **병렬 작업이 셀마다 쓰는 구조체를 슬라이스에 붙여 두면 worker끼리 캐시 줄을 나눠 씁니다(false sharing).** 청크 휴면의 작업 구조체(`world.job`)가 커지자 8 worker 벤치마크가 3.10 ms에서 3.44 ms로 느려졌고, 끝에 64바이트 패딩을 넣어 되돌렸습니다(2026-11-06). 1 worker에서는 보이지 않으므로 여러 worker로 잽니다.
- **`world.Block`은 4MB 배열입니다.** 값으로 넘기거나 `*b`로 복사하면 4MB를 통째로 옮기고, 지역 변수로 두면 스택이 커집니다. 늘 `*world.Block`으로 주고받습니다.
- **Windows에서는 다른 핸들이 열어 둔 파일을 이름 바꾸기로 덮어쓸 수 없습니다.** Go의 `os.Open`은 삭제 공유 없이 열기 때문에, 읽는 중인 파일 위로 `os.Rename`하면 "Access is denied"가 납니다. Linux와 macOS에서는 성공하므로 Windows에서만 드러납니다. 같은 파일을 읽고 쓰는 고루틴은 서로 기다리게 합니다(`internal/app` prefetch.go, 2026-11-06 Codex 리뷰).
- **백그라운드에서 미리 읽어 둔 블록은 그 뒤에 저장된 블록보다 오래될 수 있습니다.** 읽기 시작 전에 블록의 저장 횟수를 기억해 두었다가, 읽는 동안 다시 저장됐으면 버립니다. 쓰기를 기다리는 사본이 있는 블록은 미리 읽지 않습니다(`internal/app` prefetch.go, 2026-11-06).
- **셀을 바꾸는 새 경로는 표시해야 합니다.** 청크는 변경 사각형 안만 훑으므로, 표시하지 않고 셀을 바꾸면 그 주변이 잠든 채로 남아 셀이 공중에 멈춥니다. 틱 안에서는 `World.mark`, 틱 밖에서는 `World.touch`를 거칩니다. 테스트에서 `w.cells`를 직접 고칠 때는 `Set`이나 `Fill`로 먼저 표시된 칸만 고칩니다.
- **블록 안의 `x, err :=`는 이름 붙은 반환값 `err`를 가립니다.** 그 블록에서 `defer`가 `err`를 고쳐도 함수가 돌려주는 값은 그대로라, 마무리 에러가 사라집니다. 반환값을 고치는 `defer`는 함수 맨 바깥 블록에서 걸고, 안쪽 에러는 다른 이름으로 받습니다(`cmd/matter-wake` profileTo, 2026-11-07 Codex 리뷰).
- **Windows에서 Go의 `time.Now`는 약 0.52 ms 단위로만 움직입니다.** 이 PC에서 잰 값입니다(2026-11-07). 그래서 한 틱이나 한 그리기를 `time.Now`로 재면 0, 0.5, 1.0 ms처럼 계단으로 나옵니다. `-timing`의 틱별 값도 그렇습니다. 같은 결정적 장면을 여러 번 돌려 틱마다 평균하거나, `go test -bench`의 평균을 씁니다. 평균하면 계단이 지워집니다.
- **`pprof.StartCPUProfile`은 받은 writer의 에러를 버립니다.** 디스크가 차서 쓰기가 실패해도 `StopCPUProfile`은 아무 말이 없고, 깨진 프로파일이 남습니다. writer를 감싸 첫 에러를 기억해 두고 멈춘 뒤 확인합니다(`cmd/matter-wake` profileTo, 2026-11-07 두 번째 Codex 리뷰).
- **틱당 예산이 있으면, 더 훑는 것만으로 결과가 바뀝니다.** 예산이 없을 때는 블록을 내렸다 올려 통째로 깨워도 결과가 같았습니다(잠든 셀은 훑어도 그대로라서). 예산이 있으면 훑을 셀이 늘어난 만큼 다른 청크가 미뤄집니다. 그래서 저장하려고 블록을 내렸다 올릴 때와 저장한 세션을 다시 열 때 청크 상태(`World.ChunkStates`)를 되돌립니다(`internal/game` SaveBlocks, 2026-11-07 Codex 리뷰).
- **종료할 때 저장하는 파일은 시작할 때 저장할 수 있는지 확인합니다.** `-record`처럼 세션 내내 모은 것을 끝에 한 번 쓰면, 경로가 폴더이거나 쓸 수 없을 때 세션 전체를 잃습니다. 모으는 양에 상한이 있으면 상한에서 모으기를 멈춰, 끝의 저장이 통째로 거부되지 않게 합니다(`internal/app`, 2026-11-06 Codex 리뷰).

## Ebitengine

- **`WritePixels`는 premultiplied RGBA를 받습니다.** 반투명 재질의 색은 알파를 곱해서 넣습니다(`internal/palette`). `ReadPixels`도 premultiplied이고, Go의 `image.RGBA`도 같은 형식입니다.
- **`Draw` 한 번 사이에 `Update`가 여러 번 돌 수 있습니다.** 특정 틱의 화면이 필요하면 `Update` 안에서 그립니다(`internal/app/shots.go`).
- **`SetScreenFilterEnabled`는 deprecated입니다.** 기본 최종 화면 그리기가 이미 정수 배율에서는 nearest, 그 외에는 pixelated 필터를 쓰므로 픽셀 아트에 맞습니다.
- **`vector` 패키지는 한 경로가 같은 픽셀을 덮은 횟수를 4비트로 셉니다.** 수십 번 겹치는 경로는 사라집니다(uplunge에서 발견).
- **`ebiten.SetWindowSize`는 0 이하 크기에 panic합니다.** 명령줄 값은 Ebitengine에 넘기기 전에 검사해 에러로 돌려줍니다(`app.Run`, 2026-11-06 Codex 리뷰).
- **`ebiten.CursorPosition()`은 논리 화면 좌표를 돌려줍니다.** 창 배율을 직접 나눌 필요가 없습니다.
- **Windows에서 게임의 CPU 프로파일은 대부분 `IDXGISwapChain.Present`입니다.** 렌더 스레드가 화면 표시를 기다리는 시간이고, `runtime.cgocall`로 보입니다. 샌드박스 600틱에서 89%였습니다(2026-11-07). 게임 코드는 `go tool pprof -focus matter-wake/internal`로 봅니다.

## 도구

- **도구는 `tools.mod`로만 실행합니다.** `go tool -modfile=tools.mod golangci-lint ...`. `tools.mod`에 `go mod tidy`를 돌리면 게임 의존성까지 끌려 들어가므로 하지 않습니다.
- **golangci-lint v2 설정 키가 바뀌었습니다.** gofumpt는 `extra-rules` 대신 `extra.group-params`/`extra.clothe-returns`, exhaustive는 `ignore-enum-types` 대신 `ignore-types`를 씁니다.
- **govulncheck는 설치된 Go의 표준 라이브러리도 검사합니다.** 최신 패치보다 낮은 Go(예: 1.27.0)로 `-vuln`을 돌리면 이미 고쳐진 표준 라이브러리 취약점이 나옵니다. 로컬 Go를 최신 패치로 올립니다. CI는 항상 최신 `1.27.x`를 씁니다.
- **`material.ID`는 enum이 아닙니다.** `Air` 상수 하나 때문에 exhaustive가 enum으로 오인하므로 설정에서 제외했습니다.

## cgo (4단계부터)

- **설치된 clang은 MSVC 대상이라 cgo에 쓸 수 없습니다.** Windows의 cgo와 `-race`에는 llvm-mingw나 MSYS2 같은 MinGW 계열 컴파일러가 필요합니다.
- **C 구조체에 Go 포인터를 넣지 않습니다.** Box2D의 `userData` 등에는 정수 핸들을 넣습니다. cgo 포인터 규칙 위반은 런타임 검사가 잡습니다.
- **Box2D는 `-ffp-contract=off`로 컴파일해야 플랫폼 간 결정성이 유지됩니다.** Box2D의 CMake 설정이 그렇게 합니다.
