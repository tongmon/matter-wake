# 알려진 함정

새 함정을 만나면 여기에 추가합니다. 코드 주석이나 커밋 메시지에만 남기면 다음 작업자가 찾지 못합니다.

## Go

- **정수 상수의 나눗셈은 float 식 안에서도 정수로 먼저 계산됩니다.** `const speed = 15`일 때 `float32(u*speed) - speed/2`의 `speed/2`는 7.5가 아니라 7입니다. 상수 식은 형이 없는 정수로 계산된 뒤에 float로 바뀌기 때문입니다. 소수가 필요한 상수는 `15.0`처럼 소수로 씁니다. 균열 조각의 속도가 −7~8칸/s로 치우친 것을 테스트가 찾았습니다(2026-11-11, `rigid.speckSpeed`, `TestCrackThrowsSpecks`).
- **`math/rand`는 게임 로직에서 쓰지 않습니다.** Go 버전마다 알고리즘과 시드 처리가 바뀔 수 있어 리플레이와 골든 테스트가 깨집니다. `internal/rng`를 씁니다.
- **핫루프에서 클로저나 메서드 값을 매 호출 만들면 할당이 생깁니다.** `w.stepChunk`처럼 메서드 값을 필드에 한 번 저장해 두고 씁니다(`world.World.stepChunkFn`).
- **고정소수점 곱셈을 산술 시프트(`>> 8`)로 나누면 음수 쪽으로 치우칩니다.** 시프트는 음의 무한대 쪽으로 내림이라, 감쇠를 반복하면 양수는 0이 되지만 음수는 -1/256에서 멈춰 왼쪽으로 흐르는 힘이 사라지지 않습니다. `fixed.Q8.Mul`은 0 쪽으로 자르는 정수 나눗셈을 씁니다(`TestMulIsSymmetric`).
- **`json.Decoder.More()`로는 뒤에 붙은 데이터를 다 잡지 못합니다.** 배열·객체의 다음 원소만 보기 때문에, 문서 뒤에 붙은 `]`나 `}`는 통과합니다. 한 번 더 `Decode`해서 `io.EOF`인지 확인합니다(`internal/material`, 2026-11-06 Codex 리뷰).
- **Go float 계산은 아키텍처마다 마지막 비트가 다를 수 있습니다.** 언어 명세가 `a + x*y`를 FMA 하나로 합치는 것을 허용하고, Go 컴파일러는 arm64(Apple Silicon Mac)에서는 기본으로 합치지만 amd64에서는 `GOAMD64=v3` 이상일 때만 합칩니다(`cmd/compile/internal/ssa/_gen/ARM64.rules`, `AMD64.rules`). 셀 시뮬레이션처럼 임계값 비교가 많은 코드에서는 한 비트 차이로 궤적이 갈라져 골든 테스트와 리플레이가 OS마다 달라집니다. 시뮬레이션 상태는 정수로 두고, float를 꼭 써야 하면 곱셈 결과를 `float32(x*y) + z`처럼 명시적으로 변환해 합치기를 막습니다.
- **함수 안에서 반올림하지 않은 곱은 함수를 넘어 호출한 쪽의 덧셈과 합쳐질 수 있습니다.** Go가 함수를 인라인하면, 반환값 안의 곱(`return Vec2{X: k * v.X}`)이 호출한 쪽의 `f.X + up.X`와 만나 arm64에서 FMA 하나(`FMADDS`)가 됩니다. 함수가 돌려주는 곱도 모두 `float32(k * v.X)`처럼 변환합니다. `GOARCH=arm64 go build -gcflags=-S`로 확인합니다(Codex 리뷰, 2026-11-08, `internal/rigid` 부력과 저항).
- **`out/` 아래에 `.go` 파일을 두지 않습니다.** `out/`은 git에서는 제외되지만 Go 모듈 안이라, `go test ./...`와 린트가 그 파일까지 컴파일합니다. 임시 실험 코드는 저장소 밖(세션 scratchpad)의 자기 이름 폴더에 둡니다(2026-11-06, skill 평가 실행이 남긴 재현 코드로 검사가 깨짐).
- **`go test`의 캐시는 테스트가 연 파일만 추적합니다.** 테스트가 저장소 밖의 무언가에 의존하면 `-count=1`로 돌립니다.
- **병렬 작업이 셀마다 쓰는 구조체를 슬라이스에 붙여 두면 worker끼리 캐시 줄을 나눠 씁니다(false sharing).** 청크 휴면의 작업 구조체(`world.job`)가 커지자 8 worker 벤치마크가 3.10 ms에서 3.44 ms로 느려졌고, 끝에 64바이트 패딩을 넣어 되돌렸습니다(2026-11-06). 1 worker에서는 보이지 않으므로 여러 worker로 잽니다.
- **`game.Input`은 128바이트보다 작게 둡니다.** gocritic은 80바이트 이상을 값으로 받는 인자(`hugeParam`)와 128바이트 이상을 값으로 도는 `range`(`rangeValCopy`)를 막습니다. 대시보드의 `Change`가 들어가 120바이트가 되면서 `game.Step`은 `*Input`을 받습니다. 필드를 더할 때는 큰 필드를 앞에 두고 크기를 확인하고(`unsafe.Sizeof`), 넘으면 `[]Input`을 인덱스로 돕니다(2026-11-10).
- **`world.Block`은 4MB 배열입니다.** 값으로 넘기거나 `*b`로 복사하면 4MB를 통째로 옮기고, 지역 변수로 두면 스택이 커집니다. 늘 `*world.Block`으로 주고받습니다.
- **Windows에서는 다른 핸들이 열어 둔 파일을 이름 바꾸기로 덮어쓸 수 없습니다.** Go의 `os.Open`은 삭제 공유 없이 열기 때문에, 읽는 중인 파일 위로 `os.Rename`하면 "Access is denied"가 납니다. Linux와 macOS에서는 성공하므로 Windows에서만 드러납니다. 같은 파일을 읽고 쓰는 고루틴은 서로 기다리게 합니다(`internal/app` prefetch.go, 2026-11-06 Codex 리뷰).
- **백그라운드에서 미리 읽어 둔 블록은 그 뒤에 저장된 블록보다 오래될 수 있습니다.** 읽기 시작 전에 블록의 저장 횟수를 기억해 두었다가, 읽는 동안 다시 저장됐으면 버립니다. 쓰기를 기다리는 사본이 있는 블록은 미리 읽지 않습니다(`internal/app` prefetch.go, 2026-11-06).
- **셀을 바꾸는 새 경로는 표시해야 합니다.** 청크는 변경 사각형 안만 훑으므로, 표시하지 않고 셀을 바꾸면 그 주변이 잠든 채로 남아 셀이 공중에 멈춥니다. 틱 안에서는 `World.mark`, 틱 밖에서는 `World.touch`를 거칩니다. 테스트에서 `w.cells`를 직접 고칠 때는 `Set`이나 `Fill`로 먼저 표시된 칸만 고칩니다.
- **블록 안의 `x, err :=`는 이름 붙은 반환값 `err`를 가립니다.** 그 블록에서 `defer`가 `err`를 고쳐도 함수가 돌려주는 값은 그대로라, 마무리 에러가 사라집니다. 반환값을 고치는 `defer`는 함수 맨 바깥 블록에서 걸고, 안쪽 에러는 다른 이름으로 받습니다(`cmd/matter-wake` profileTo, 2026-11-07 Codex 리뷰).
- **Windows에서 Go의 `time.Now`는 약 0.52 ms 단위로만 움직입니다.** 이 PC에서 잰 값입니다(2026-11-07). 그래서 한 틱이나 한 그리기를 `time.Now`로 재면 0, 0.5, 1.0 ms처럼 계단으로 나옵니다. `-timing`은 그래서 Windows에서 성능 카운터(`QueryPerformanceCounter`, 이 PC에서 0.1 µs 단위)로 잽니다(`internal/app` clock_windows.go, 2026-11-07). 앱 계층에서 짧은 구간을 새로 잴 때도 `time.Now` 대신 이 시계를 씁니다. `go test -bench`는 많은 반복을 평균하므로 계단이 지워져 `time.Now`로도 정확합니다.
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
- **Git Bash(MSYS)는 `/`로 시작하는 인자를 Windows 경로로 바꿉니다.** `//`로 시작하는 인자는 `/` 하나로 줄어듭니다. 2026-11-10 Go 주석 `// A static body`를 찾을 문자열로 스크립트에 넘겼더니 `/ A static body`가 되어, 바꾼 자리 앞에 `/` 하나가 남아 컴파일이 깨졌습니다. 그런 인자는 파일에 적어 넘기거나 `MSYS_NO_PATHCONV=1`을 붙입니다.
- **Windows의 Python 3.9는 줄이 길고 한글이 있는 UTF-8 스크립트를 "Non-UTF-8 code"로 거부할 수 있습니다.** 파일은 UTF-8로 읽히는데도 그렇습니다(2026-11-10). 첫 줄에 `# -*- coding: utf-8 -*-`를 두면 읽습니다.
- **Noita 원시 기록의 칸 좌표는 0 쪽으로 자른 값입니다.** Noita는 위치를 정수로 잘라 칸을 정하므로, 음수 좌표(지표 위, C140의 방은 y −416~−240)에서는 우리처럼 내린 값보다 한 칸 큽니다. 입자의 마지막 본 칸(+54/+58)처럼 자른 값을 우리 코드와 대조할 때는 음수 좌표에서 한 칸을 맞춥니다. 경로처럼 그 칸의 모서리를 다시 쓰는 규칙은 음수 좌표에서 모서리가 칸의 반대편에 있어, 같은 규칙이어도 자유롭게 돌린 궤적은 달라집니다(2026-11-11, design.md 3절 "튀는 입자"의 2).
- **CI의 `go test -race`는 일반 실행보다 약 16배 느리고, 패키지마다 10분 한도가 있습니다.**
  - 2026-11-07 Windows 러너에서 `internal/app`이 10분을 넘어 main CI가 실패했습니다. Linux와 macOS도 552~559초로 한도에 붙어 있었습니다.
  - 원인은 테스트가 매 틱 부르는 `World.Checksum`이었습니다. 바이트마다 FNV-1a를 돌려 셀 100만 개에 12.8 ms가 걸렸고, 로컬 `internal/app` 테스트 시간의 87%를 썼습니다. 셀을 64비트 단어 두 개로 묶어 섞는 자체 해시로 바꿔 2.1 ms가 됐습니다(`internal/world` checksum.go).
  - 큰 월드를 여러 틱 돌리는 테스트를 더하면, PR CI에서 race 단계의 패키지 시간을 확인합니다. 로컬에서는 MinGW가 없으면 race를 못 돌리므로, 일반 실행 시간에 16을 곱해 가늠합니다.
  - 2026-11-11 `internal/replay`가 race에서 344~460초였고, 느린 러너에서는 600초를 넘어 PR #39의 CI가 OS를 바꿔 가며 실패했습니다(문서만 바꾼 커밋도). 얼음·유리 리플레이 둘을 더하며 한도에 붙었고, 리플레이마다 1·4 worker로 두 번 돌았기 때문입니다. 이제 race 빌드(`raceDetector`)에서는 리플레이마다 4 worker로 한 번만 돌려 골든과 비교하고, 1·4 worker가 같은지는 일반 실행에서 봅니다. 같은 실행에서 `internal/game`도 race에서 391~426초였으니, 거기에 큰 테스트를 더할 때도 시간을 봅니다.

## cgo (4단계부터)

- **`C:\Program Files\LLVM\bin`의 clang은 MSVC 대상이라 cgo에 쓸 수 없습니다.** Windows의 cgo와 `-race`에는 MinGW 계열 컴파일러가 필요합니다. 개발 PC에는 llvm-mingw(clang 23.1.5, UCRT)가 `%LOCALAPPDATA%\llvm-mingw\bin`에 있고 사용자 PATH에 들어 있습니다(2026-11-07). Go는 기본으로 `gcc`를 부르는데, llvm-mingw의 `gcc.exe`가 그 clang입니다. `CC=clang`으로 두면 PATH에서 먼저 나오는 MSVC 쪽 clang이 잡힙니다. PATH를 바꾸기 전에 띄운 터미널과 에이전트 세션은 새 PATH를 모르므로, 다시 열거나 명령 앞에 경로를 붙입니다.
- **Windows CI 러너는 이미지에 든 MinGW gcc(2026-11 기준 15.2.0)가 PATH에 있어 cgo와 `-race`가 됩니다.** MSYS2도 `C:\msys64`에 있지만 PATH에는 없습니다. `go run ./tools/check`의 cgo 단계가 쓰인 컴파일러를 출력합니다.
- **C 구조체에 Go 포인터를 넣지 않습니다.** Box2D의 `userData` 등에는 정수 핸들을 넣습니다. cgo 포인터 규칙 위반은 런타임 검사가 잡습니다. `physics.Body`는 Box2D 몸체 id를 묶은 `uint64`입니다.
- **Go는 `#cgo CFLAGS`에 `-ffp-contract=off`를 허용하지 않습니다.** 보안 허용 목록(`cmd/go/internal/work/security.go`)에 없어 "invalid flag"로 빌드가 멈추고, 풀려면 빌드마다 `CGO_CFLAGS_ALLOW`를 넣어야 합니다. Box2D는 이 플래그로 FMA 합치기를 막아 플랫폼 간 결정성을 지킵니다. 그래서 C 파일마다 맨 처음 `internal/physics/box2d_fp.h`를 포함해 pragma로 같은 규칙을 겁니다. clang은 `#pragma STDC FP_CONTRACT OFF`, gcc는 `#pragma GCC optimize("fp-contract=off")`입니다(gcc는 STDC pragma를 무시합니다). clang은 기본(`on`)에서 한 식 안의 `a*b + c`를 합칩니다. `TestCompiledWithoutContraction`이 컴파일러마다 확인합니다(2026-11-07).
- **cgo는 패키지 폴더의 C 파일만 컴파일합니다.** 그래서 `third_party/box2d/src`의 원본을 고치지 않고, `internal/physics/box2d_<이름>.c`가 원본을 `#include`합니다. 원본 안의 `#include "core.h"`는 원본 파일의 폴더에서 먼저 찾으므로 그대로 됩니다. 여러 원본을 한 파일로 묶으면 안 됩니다. `contact_solver_w8.c`는 `B2_SIMD_WIDTH`를 8로 정한 뒤 `core.h`를 포함해 AVX2 경로를 켜는데, `core.h`는 `#pragma once`라 한 파일 안에서는 처음 한 번만 SIMD를 고릅니다. 묶으면 AVX2 경로가 조용히 빠집니다.
- **Go의 빌드 캐시는 C 파일이 패키지 폴더 밖에서 포함한 파일의 변경을 보지 못합니다.** 캐시 키에는 패키지 폴더의 파일만 들어갑니다. 그래서 `third_party/box2d`를 고쳐도 `go build`와 `go test`가 예전에 컴파일한 Box2D를 그대로 씁니다. 원본에 `#error`를 넣어도 테스트가 통과했습니다. `internal/physics/box2d_sum.go`의 `box2dSum`(원본의 SHA-256)을 바꿔야 다시 컴파일하고, `TestBox2DSumIsCurrent`가 원본이 바뀌면 실패하며 새 값을 알려 줍니다(Codex 리뷰, 2026-11-07).
- **`CC`에는 따옴표로 묶은 경로가 들어갈 수 있습니다.** go 명령은 `"C:\Program Files\...\gcc.exe" -m64`처럼 따옴표 안의 공백을 지킵니다. 공백으로 자르면 `"C:\Program`을 실행하려다 실패합니다. `tools/check`는 go 명령과 같은 규칙으로 자릅니다(`splitCC`, Codex 리뷰, 2026-11-07).
- **cgo는 `_test.go`에서 쓸 수 없습니다.** 테스트가 C를 불러야 하면 테스트 전용 함수를 일반 파일에 둡니다(`internal/physics` probe.go, `useSSE2` 등 비공개 메서드).
- **Go에서 static C 함수의 주소를 가져올 수 없습니다.** `C.f`를 값으로 쓰면 링크 때 "undefined symbol"이 납니다. 콜백 등록은 C 쪽 static 함수가 하게 합니다(`physicsSilenceBox2D`).
- **C 호출 인자에 `&x`를 바로 쓰면 gocritic이 cgo가 만든 코드를 잡습니다.** cgo가 `_cgoCheckPointer(..., 0 == 0)`을 만들어 `dupSubExpr`가 납니다. 포인터를 변수에 담아 넘깁니다. 같은 cgo 파일에서 `if err := f(); err != nil`도 `uncheckedInlineErr`로 잘못 잡혀, 두 문장으로 씁니다(2026-11-07).
- **cgo 호출 하나는 개발 PC에서 약 34 ns입니다.** 몸체 300개의 위치와 회전을 하나씩 읽으면 20 µs로, 같은 몸체의 Step(약 55 µs)의 3분의 1이 넘습니다. 몸체가 많아지면 한 번의 호출로 여러 몸체를 읽습니다(references.md 3절).
- **Box2D는 월드를 잠금 없는 전역 표(최대 128개)에 둡니다.** `b2CreateWorld`와 `b2DestroyWorld`를 여러 goroutine에서 동시에 부르면 C 안에서 경쟁이 납니다. race 검출기는 C 메모리를 보지 못합니다. 월드는 한 goroutine에서 차례로 만들고 닫습니다. 그래서 `internal/physics`의 테스트는 `t.Parallel`을 쓰지 않습니다.
- **Box2D는 잘못된 입력을 거부하면 stdout에 출력합니다.** 로직은 출력하지 않으므로 `internal/physics`가 로그 콜백을 아무것도 안 하는 함수로 바꾸고, 같은 실수를 Go 쪽에서 먼저 에러로 돌려줍니다.
- **Box2D 릴리스 빌드는 지운 몸체의 핸들을 검사하지 않습니다.** `DestroyBody` 뒤에 같은 `physics.Body`로 위치를 읽으면 Go panic이 아니라 C 안의 접근 위반으로 프로세스가 죽습니다(2026-11-07 테스트에서 확인). 핸들을 지운 쪽에서 버립니다. `b2Body_IsValid`로 매번 검사하면 읽기마다 cgo 호출이 하나 늘어 쓰지 않았습니다.
- **Box2D는 동적 몸체와 겹치게 만든 정적 형상에서 그 몸체를 밀어냅니다.** 같은 자리에 겹치는 형상을 되풀이해 만들면 몸체가 조금씩 밀려, 움직이는 이웃 몸체를 파고듭니다. 움직이는 몸체끼리의 접촉은 정적 형상보다 무르기 때문입니다. 셀에서 만든 지형은 기운 몸체와 늘 조금 겹치므로, `internal/rigid`는 몸체에 파고드는 새 정착 칸을 지형에서 뺍니다(`keepOut`, 2026-11-08).
- **Box2D는 정적 몸체를 지워도 그 위에서 잠든 몸체를 깨우지 않습니다.** 지형 충돌을 다시 만들면(지우고 새로 만들기) 그 위에서 잠든 상자는 받침이 사라져도 공중에 떠 있습니다. `internal/rigid`는 다시 만든 청크 근처의 몸체를 `physics.World.Wake`로 깨웁니다(`TestTerrainRebuildsOnlyOnChange`, 2026-11-07).
  - 2026-11-10 다시 확인한 것(얼음·유리 작업, 저장소 밖 임시 측정): v3.2의 `b2DestroyContact`는 닿아 있던(touching) 접촉이면 두 몸체를 깨웁니다. 잠든 몸체의 닿은 접촉은 잠든 동안에도 그 표시를 지닙니다. 정적 chain 몸체를 지우거나 정적 몸체의 형상을 바꾸면 그 위에서 잠든 상자가 깼습니다. 그래서 static 몸체(얼음·유리)가 바뀔 때는 따로 깨우지 않습니다(`TestBodiesOnStaticBodiesFallWhenItBreaks`). 위의 처음 관측이 어떤 경우였는지는 다시 재현하지 못했고, 닿지 않은 채 잠든 몸체(예측 여유 안의 접촉)는 깨지 않으므로 지형의 `Wake`는 그대로 둡니다.
- **cgo 없이(`CGO_ENABLED=0`) 빌드하면 `internal/physics`는 `needsCgoAndACCompilerSeeThirdPartyREADME`가 없다는 에러로 멈춥니다.** 원인을 알리려고 일부러 둔 이름입니다(nocgo.go). `go run ./tools/check`도 cgo 단계에서 실패합니다.
- **Box2D가 형상을 바꾸고 질량을 다시 셀 때 하는 속도 보정(v + ω × Δc)은 깨어 있는 몸체에만 합니다.** `b2UpdateBodyMassData`는 잠든 몸체의 속도 상태가 없어 건너뜁니다. 그래서 `physics.World.SetShapes`는 형상을 바꾸기 전에 몸체를 깨웁니다(2026-11-08, 파괴 작업).
- **`b2BodyDef.linearVelocity`는 원점의 속도이고, `b2Body_SetLinearVelocity`는 질량 중심의 속도입니다.** 형상 없이 만든 몸체에 속도를 주고 형상을 더하면 질량 중심이 원점에서 옮겨 가며 ω × Δc가 더해집니다. 파괴 조각처럼 질량 중심의 속도를 정해 주려면 형상을 다 만든 뒤 `SetVelocity`를 부릅니다(2026-11-08).
- **C에 주소를 넘기는 지역 변수는 호출마다 힙으로 옮겨집니다.** `def := C.b2DefaultBodyDef(); C.b2CreateBody(w.id, &def)`는 escape analysis가 C로 가는 포인터를 따라가지 못해 `def`를 힙에 만듭니다. 지형에서 몸체 근처 청크를 틱마다 다시 만들자 틱에 할당이 1회 생겼습니다. 자주 부르는 함수는 정의를 월드마다 한 번 만들어 둡니다(`physics.World.def`, `TestRebuildingChainsDoesNotAllocate`, 2026-11-09).
- **C에는 Go 포인터를 담지 않은 할당의 주소만 넘길 수 있습니다.** 정의를 `World` 구조체의 필드로 두고 `&w.def`를 넘기자 "argument of cgo function has Go pointer to unpinned Go pointer"로 panic했습니다. cgo는 그 주소가 든 할당 전체를 검사하고, `World`에는 슬라이스가 있습니다. 정의만 담은 할당(`new(C.b2BodyDef)`)을 따로 둡니다(2026-11-09).
- **Box2D는 몸체를 끄거나(`b2Body_Disable`) 재우면 속도를 버립니다.** 다시 켜거나 깨우면 정지 상태로 시작합니다. 메모리 가장자리에서 얼린 몸체가 이어 가려면 속도를 우리 쪽에 두었다가 돌려줘야 합니다(`internal/rigid` freeze, 2026-11-09). 동적 몸체를 지우면(`b2DestroyBody`) 닿아 있던 잠든 몸체는 Box2D가 깨웁니다. 정적 몸체는 위 항목처럼 깨우지 않습니다.
- **Box2D v3.2의 스냅샷(`b2World_GetSnapshot`)은 빌드에 묶인 형식입니다.** 구조체를 memcpy한 이미지라 구조체 크기와 포인터 크기를 섞은 레이아웃 해시와 버전(13)으로 검사하고, 헤더 주석도 형식이 바뀔 것이라고 적습니다. 비트까지 이어지지만 저장 파일에 넣으면 Box2D를 올릴 때 읽히지 않습니다(design.md 8절, 2026-11-09).
- **Box2D v3는 접촉을 Step마다 한 번, 넓힌 AABB가 겹치는 짝에서만 찾습니다.** 연속 충돌은 빠른 이동에만 있고 회전에는 없어, 빠르게 도는 긴 몸체의 끝은 한 Step 안에 정적 형상 속으로 들어갔다가 다음 Step에 세게 밀려납니다. 바닥 위 8칸에서 6 rad/s로 도는 200칸 판자는 바닥이 충돌로 있어도 끝이 한 칸 들어갔고, 17.6 m/s로 튀어 올랐습니다(2026-11-09, 몸체 저장 작업의 임시 측정).
