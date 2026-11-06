# 레퍼런스와 측정 기록

## 1. Noita 분석 자료

위치: `C:\Users\tongm\Downloads\noita-analysis` (저장소 밖)

- 처음 읽을 문서: `clone-spec/ENVIRONMENT_GAME_HANDOFF.txt`. 현재 목적과 자료 범위가 정리돼 있습니다.
- 셀 갱신: `clone-spec/MATERIAL_TICK.txt` (작업 영역, 순회 방향, 중복 갱신 방지, 요청 큐)
- 병렬 실행·휴면: `clone-spec/PRIORITY_RESULTS.txt` 1~2절
- 파괴와 강체: `clone-spec/DESTRUCTION_BRIDGE.txt`
- 스트리밍·저장: `clone-spec/WORLD_STREAMING.txt`
- 프레임 순서·RNG: `clone-spec/FRAME_ORDER.txt`

사용 규칙(AGENTS.md와 같음): 규칙과 수치만 참고하고, 코드·의사코드·자산·이름을 옮기지 않습니다.

### 참고하는 방법

시뮬레이션 메커니즘이나 재질 값을 건드리기 전에 `noita-reference` skill(`.claude/skills/noita-reference`)을 씁니다.

- 메커니즘별로 읽을 절·문서·재질 필드와 연관 메커니즘은 skill의 [메커니즘 지도](../.claude/skills/noita-reference/references/mechanics.md) 한 곳에만 둡니다. 새 출처를 찾으면 거기에 추가합니다.
- 조회는 `go run ./tools/noitaref`로 합니다. 절(`section C49`), 문서(`doc MATERIAL_TICK`), 재질의 실제 값(`material water`), 키워드 검색(`search 액체 충격`)을 볼 수 있습니다.
- 재질의 실제 값은 엔진이 상속까지 처리해 읽어 들인 최종값입니다(`analysis-detail/runtime-completion/materials-expanded-analysis.json`). XML 선언값과 다를 수 있습니다.
- 결과는 메커니즘마다 `docs/briefs/`의 참고 요약으로 남깁니다.

### 현재 코드와 분석 자료의 대응 (2026-11-06, `feat/cell-motion`)

| 구분 | 내용 |
|---|---|
| 반영함 | 64×64 작업 영역, 아래 행부터 위로 훑기와 행마다 무작위 방향(MATERIAL_TICK 1~2절), 마지막 갱신 틱으로 중복 방지(4절), 셀 교환 즉시 적용(5절). 낙하 속도: 틱마다 가속, 첫 칸 뒤 남은 속도에서 칸마다 2씩 소모, 추가 낙하 중 절반 확률로 흩어짐 방향을 다시 뽑고 흐르던 방향이 없으면 1/8 왼쪽·1/8 오른쪽·나머지 곧장(C47, C49). 부딪히면 속도 0(C47). 가루가 더 가벼운 액체로 25% 확률로 가라앉기(C47 밀도 교환). 액체 가로 흐름: 미는 힘 누적, 1 이상일 때 한 칸, 칸마다 감쇠, 한쪽만 열리면 그쪽으로 누적(C49) |
| 일부러 다르게 함 | 실행 그룹: 4그룹 대신 아래부터 청크 행 순서(design.md 3절). 난수: worker별 LCG 대신 (시드, 틱, 청크) 키의 SplitMix64(결정성). 수치: float 대신 고정소수점(Q8). 미끄러짐: 원본의 점성 판정 대신 재질별 미끄러짐 확률(%). 가루의 대각 방향: 원본은 왼쪽 먼저, 여기서는 무작위(쏠림 방지). 액체 가로 이동은 빈 칸으로만(옆으로 밀도 교환 없음). 가루도 첫 낙하 틱에 가속(원본 가루는 첫 틱 가속 생략 C47:1180, 원본 액체는 첫 틱에도 가속 C49:1283이라 액체는 같음). 막히면 미는 힘을 0으로(관측 규칙과 다름. 물이 기둥으로 쌓이는 원인 후보로, 다음 액체 작업에서 C49 모델과 대조해 다시 정함). 속도·미는 힘에 상한(병렬 안전 범위) |
| 아직 반영 안 함 | 액체끼리 막혔을 때 위쪽 재배치(C50), 낙하 중인 가루의 정지 확률과 부착(C47), 휴면(MATERIAL_TICK 4절, PRIORITY_RESULTS 2절), 생성·변환 요청 큐(MATERIAL_TICK 6절) |

### 관측한 부하

- 논리 화면: 427×242 (`VIRTUAL_RESOLUTION_X/Y`, 데이터 파일)
- 틱당 갱신 대상 64×64 작업 영역 수: 평상시 24~34개, 로드 직후 최대 약 127개
  - 출처: `analysis-detail/runtime-priority/sleep-natural-workers.json`, `analysis-detail/runtime-completion/rng-serial-workers.json`의 `total_workers`
  - 시험 장면 기준입니다. 대규모 전투 장면에서는 더 높을 수 있습니다.

## 2. Go와 네이티브 셀 시뮬레이션 비교 (2026-11-05)

- 환경: Ryzen 7 9800X3D(16스레드), Go 1.27, clang 20 `-O2`(Rust 대신 같은 LLVM 백엔드로 측정)
- 조건: 1024×1024 격자, 움직이는 셀(모래+물) 32.9만 개, 300틱, 같은 규칙. 네 실행의 체크섬이 모두 같음

| 구성 | ms/틱 |
|---|---|
| Go 1스레드 | 3.90 |
| C 1스레드 | 2.57 |
| Go 16스레드 | 0.62 |
| C 16스레드 | 0.44 |

- Go→DLL 호출 1회: 약 29ns (`syscall.SyscallN`). Go 함수 호출은 0.82ns.

## 3. Box2D 비교 (2026-11-06)

- box2d-go(공식 Box2D v3.2의 ccgo 번역)와 공식 C Box2D v3.2(clang 빌드)를 같은 벤치마크 장면으로 비교
- box2d-go는 Windows에서 테스트와 결정성 해시가 통과했습니다(SIMD 끔/켬 모두).
- ms/step, 3회 중 최솟값:

| 장면 | worker | C AVX2 | Go SIMD | Go SIMD 끔 |
|---|---|---|---|---|
| joint_grid | 1 | 3.40 | 4.94 | 4.92 |
| junkyard | 1 | 1.91 | 4.49 | 9.56 |
| queries | 1 | 5.30 | 8.78 | 9.80 |

- C Box2D v3.2는 실행할 때 AVX2/SSE2를 고르고, 내부 작업 스케줄러가 있습니다.
- Go 쪽 SIMD는 `GOEXPERIMENT=simd`와 `GOAMD64=v3`(AVX2 전용 실행 파일)가 필요합니다.

## 4. The Powder Toy 소스

위치: `C:\Users\tongm\Downloads\the-powder-toy-analysis` (저장소 밖). 릴리스 v100.1.400(태그 `v100.1.400`, 커밋 `d768aeb`)의 소스 사본이고 git 저장소는 아닙니다. 라이선스는 GPL-3.0입니다.

- **역할:** Noita 분석 자료에 없거나 확인되지 않은 부분을 우리 설계로 메울 때 보는 보조 참고입니다. 해당 분야는 압력과 공기 흐름, 열전도, 상태 변화, 기체 확산, 전기입니다. Noita 관측 규칙이 있으면 그것이 우선입니다.
- **쓰는 법:** `noita-reference` skill의 9단계(빈칸)에서만 봅니다. 메커니즘별 파일과 함수는 skill의 [TPT 지도](../.claude/skills/noita-reference/references/tpt.md) 한 곳에만 둡니다.
- **라이선스:** 코드, 주석, 원소 표를 옮기거나 Go로 번역하지 않습니다. 옮기면 matter-wake가 GPL 파생물이 됩니다. 방식만 이해하고 자기 말로 설계합니다. 참고 요약에는 "TPT 방식"으로 출처를 표시합니다.
- **구조가 다른 점:** TPT는 float 입자 배열을 한 스레드에서 배열 순서대로 갱신합니다. 압력, 공기 속도, 주변 온도는 4×4 픽셀 격자에 둡니다. 이동은 위쪽으로도 쓰고 최대 30칸까지 훑습니다. 원소 이름으로 분기하는 예외도 많습니다. 우리 제약(정수, 청크 행 병렬, `MaxReach`, 위로 쓰기 없음, 이름 분기 없음)에 맞춰 다시 설계해야 합니다.
- **수치:** TPT 고유 단위(켈빈, ±256 압력, 자체 화면 기준 중력)라 그대로 비교할 수 없습니다. 하나를 🧪 초기값의 근거로 쓸 때는 이 문서에 파일과 함수를 적습니다.

## 5. uplunge에서 가져온 방식

같은 개발자의 Go + Ebitengine 프로젝트(`C:\Users\tongm\Documents\GitHub\uplunge`)에서 다음을 가져왔습니다.

- AGENTS.md 작업 규칙, design.md의 ✅/🔶/🧪 표시
- `tools/archtest` 방식의 아키텍처 규칙 테스트
- 고정 60Hz와 Update당 Step 한 번, 직접 구현한 SplitMix64 난수
- 골든 테스트와 `-update` 플래그, `-shots` 틱별 PNG 캡처(Update 안에서 그림)
- 할당 0회 테스트, 엄격한 JSON 로딩
