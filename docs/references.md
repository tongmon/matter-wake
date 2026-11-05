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

### 메커니즘별 참고 위치

시뮬레이션 메커니즘을 구현하거나 바꾸기 전에 해당 행의 자료를 읽습니다. `C숫자`는 `clone-spec/COMPLETION_PROGRESS.txt`의 절 번호, 파일 이름은 `clone-spec/` 아래 문서입니다.

| 메커니즘 | 볼 곳 |
|---|---|
| 셀 갱신 순서, 작업 영역, 중복 갱신 방지, 요청 큐 | MATERIAL_TICK.txt, C33, C40, C112, C113 |
| 병렬 실행 그룹과 완료 대기 | PRIORITY_RESULTS.txt 1절, C121, C122 |
| 휴면과 깨우기 | PRIORITY_RESULTS.txt 2절, C25 |
| 가루(모래) 이동, 밀도 교환, 부착 | C47 |
| 점성 재질 | C48 |
| 액체 낙하와 가로 흐름 | C49 |
| 액체끼리 막힘과 위쪽 재배치 | C50, C114, C115 |
| 부착 해제 | C51 |
| 반응, 온도, 재질 변환 | C52, C53, C54, `REPORT_MATERIALS.txt` |
| 연소와 불 | C55, C57, C61, C62 |
| 기체 | C24, C56 |
| 튀는 액체 입자 | C81~C84 |
| 강체와 액체(부력, 저항) | C31, C41, C42, C26 |
| 파괴, 윤곽, 볼록 분할 | DESTRUCTION_BRIDGE.txt, C34, C35, C68~C72, C76~C80, C92~C108 |
| 폭발과 투사체의 셀 파괴 | C66, C67, C73~C75 |
| 비상주 영역, 스트리밍, 저장 | WORLD_STREAMING.txt, C43, C122 |
| 프레임 순서와 RNG | FRAME_ORDER.txt, C23, C118 |
| 전기(선택) | C123~C127 |
| 플레이어 충돌(참고) | C44, C45, C63 |

### 현재 코드와 분석 자료의 대응 (2026-11-06 골격)

| 구분 | 내용 |
|---|---|
| 반영함 | 64×64 작업 영역, 아래 행부터 위로 훑기와 행마다 무작위 방향(MATERIAL_TICK 1~2절), 마지막 갱신 틱으로 중복 방지(4절), 셀 교환 즉시 적용(5절) |
| 일부러 다르게 함 | 실행 그룹: 4그룹 대신 아래부터 청크 행 순서(design.md 3절). 난수: worker별 LCG 대신 (시드, 틱, 청크) 키의 SplitMix64(결정성). 수치: float 대신 정수 |
| 아직 반영 안 함 (임시 규칙) | 모래 이동(C47: 가속, 정지 확률, 점성에 따른 대각 확률, 확률적 가라앉기, 부착), 액체 이동(C49, C50), 휴면(PRIORITY_RESULTS 2절), 생성·변환 요청 큐(MATERIAL_TICK 6절) |

골격의 모래·물 규칙은 Go/C 성능 비교용으로 만든 단순 규칙을 그대로 가져온 것이고, 분석 자료를 보고 만든 것이 아닙니다.

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

## 4. uplunge에서 가져온 방식

같은 개발자의 Go + Ebitengine 프로젝트(`C:\Users\tongm\Documents\GitHub\uplunge`)에서 다음을 가져왔습니다.

- AGENTS.md 작업 규칙, design.md의 ✅/🔶/🧪 표시
- `tools/archtest` 방식의 아키텍처 규칙 테스트
- 고정 60Hz와 Update당 Step 한 번, 직접 구현한 SplitMix64 난수
- 골든 테스트와 `-update` 플래그, `-shots` 틱별 PNG 캡처(Update 안에서 그림)
- 할당 0회 테스트, 엄격한 JSON 로딩
