# 강체 기반 참고 요약

작성: 2026-11-07 · 관련 작업: `feat/box2d`

## 범위

- 이번 변경이 다루는 메커니즘: 강체 시뮬레이션의 바탕. Box2D v3.2를 저장소에 넣고 `internal/physics`로 감쌉니다. 월드, 정적 상자, 동적 상자, Step, 위치·각도 읽기만 있습니다.
- 다루지 않는 것: 강체↔격자 연동(픽셀 지우기·찍기, 부력, 저항), 파괴, 반응. 4단계의 다음 항목입니다.
- 함께 읽은 연관 메커니즘(mechanics.md의 Related):
  - 액체 속 강체(C31, C41, C42, C26): 단위 변환과 Noita의 강체 적분 단서만 읽었습니다. 부력·저항 규칙은 연동 작업에서 다시 읽습니다.
  - 파괴(DESTRUCTION_BRIDGE, C05): 강체의 정밀도와 도형 단서만 읽었습니다.
- mechanics.md에는 Box2D 월드 설정 자체의 행이 없습니다. 그래서 `search`로 `Box2D`, `b2World`, `physics_scale`, `world_origin`, `solver`, `iteration`, `velocity_iterations`, `position_iterations`, `PhysicsBody2`, `world step`, `강체 수`, `단위 변환`, `dt`를 찾았습니다.
- 모델: 이 범위에 맞는 모델은 없습니다. `native_probe_buoyancy`, `native_probe_contact_drag`는 연동 작업에서 읽습니다.

## 관측 규칙

| 규칙 (자기 말로) | 근거 (절:줄) | 확인 수준 |
|---|---|---|
| 강체는 위치·속도·질량·관성을 double로 들고 있다 | C05:69, C42:923 | RUNTIME + EXECUTED |
| 충돌 형상은 볼록 polygon이고 정점은 3~8개다. 파편이 모두 삼각형은 아니다 | C05:70-72, DESTRUCTION_BRIDGE:15-20 | RUNTIME + STATIC |
| 화면(픽셀) 좌표와 강체 좌표 사이에 전역 원점과 축별 배율이 있다. 픽셀을 float32로 바꾸고 원점을 뺀 뒤 배율로 나눈다 | C42:932, DESTRUCTION_BRIDGE:30 | EXECUTED + STATIC |
| 선형 감쇠는 1/(1+dt·damping)이고, 선속력이 250을 넘으면 자르는 분기가 있다(단위는 강체 쪽) | C42:953-954 | STATIC |
| 정적 몸체에는 밀도 0을 넘긴다. 직접 붙인 몸체가 밀도 0이면 질량 1·관성 0으로 대신 채워졌다 | DESTRUCTION_BRIDGE:23-24, C05:79-81 | STATIC + RUNTIME |
| 강체 시스템(PhysicsBodySystem, PhysicsBody2System)의 갱신 우선순위는 101로, 캐릭터(88~92)와 AI·피해(100) 뒤, 투사체(110) 앞이다 | FRAME_ORDER:48-63 | EXECUTED(getter 33개) |
| Box2D 버전, 한 틱의 시간, 반복·sub-step 수, 배율의 실제 값, 강체가 몇 스레드로 도는지, 결정성은 관측되지 않았다 | DESTRUCTION_BRIDGE:43(설정을 고르지 않음), 위 검색 결과 없음 | OPEN → 2026-11-07 C131·C132·C134로 일부 답함(아래) |
| (2026-11-07 추가) Step은 dt = float32(1/60), 속도 반복 6, 위치 반복 최대 2, 필요할 때 TOI 보정. Box2D 2.x 계열 구조이고 정확한 버전은 OPEN | RIGID_BODY_STEP_PROFILE:15-45, 131-138 | STATIC + LIVE |
| (2026-11-07 추가) 한 월드의 Step은 별도 스레드 하나에서 도는 작업 하나이고, 다음 결과를 쓰기 전에 기다린다. 섬별 병렬 solver는 확인되지 않음 | RIGID_BODY_STEP_PROFILE:47-68 | STATIC + LIVE |
| (2026-11-07 추가) Step 시간 중앙값: 강체 128개 장면 0.24~0.28 ms, 실제 기지 장면 0.43 ms(9800X3D) | RIGID_BODY_STEP_PROFILE:103-107, RIGID_BODY_LOAD_DESTRUCTION:83-91 | LIVE |
| (2026-11-07 추가) 실제 지형의 동적 몸체 454~457개, 깨어 있는 것 360~410개. 300개는 관측 최대치가 아님 | RIGID_BODY_LOAD_DESTRUCTION:4-12, 60-81 | LIVE |
| (2026-11-07 추가) 배율 10 px/물리 단위(실측 10.0000004) | BODY_GRID_COUPLING:57 | LIVE |

## 현재 코드 대조

| 관측 규칙 | 우리 코드 (파일:줄) | 같음 / 다름 / 없음 | 다름이면 |
|---|---|---|---|
| double 정밀도 | `internal/physics/physics.go`(`#cgo CFLAGS`, `BOX2D_DOUBLE_PRECISION` 없음) | 다름: 단정밀도 | 의도(개발자 결정 2026-11-07, design.md 2절). Box2D 문서는 단정밀도로 원점에서 약 20 km까지 다룬다고 합니다(`docs/large_worlds.md`). 우리 월드는 16384셀이라 셀 하나가 1.2 m보다 작기만 하면 그 안에 듭니다. 배율이 정해지면 다시 봅니다(아래 빈칸). double로 바꾸면 위치만 double이 되고 ABI가 바뀝니다 |
| 볼록 polygon 3~8정점 | Box2D `B2_MAX_POLYGON_VERTICES` 8, 어댑터는 상자만 | 없음(상자만) | 파괴 작업에서 polygon을 더합니다 |
| 픽셀↔강체 좌표 변환 | `internal/rigid` body.go `write`(2026-11-07) | 같음(배율 10, 원점 0) | 연동 작업에서 넣음([강체↔격자 연동 참고 요약](body-grid-coupling.md)). 어댑터는 그대로 미터만 받습니다 |
| 선형 감쇠, 속력 상한 | Box2D 기본값(감쇠 0, 최대 400 m/s) | 다름 | Noita는 Box2D 2.x 계열로 보이고, v3.2의 적분기는 다릅니다. 값은 연동 작업에서 정합니다 |
| 밀도 0 동적 몸체 | `internal/physics/physics.go` `AddDynamicBox`: 에러 | 다름 | 의도. Box2D v3.2는 질량 1로 채우지 않고 질량 0으로 둡니다. 데이터 실수를 조용히 넘기지 않으려고 거부합니다 |
| 강체 갱신 순서 | `internal/game` game.go `Step`(2026-11-07) | 다름 | 셀과의 순서는 연동 작업에서 C134 4.1대로 둠(design.md 8절). 캐릭터는 아직 없음 |

## 증상 재현

해당 없음. 증상을 고치는 작업이 아닙니다.

## 실제 수치

재질 값을 쓰지 않습니다. 테스트 장면의 밀도 1, 마찰 0.625는 측정용 고정값이고 게임 값이 아닙니다.

## 연관 메커니즘

| 메커니즘 | 이번에 반영 | 이유 |
|---|---|---|
| 액체 속 강체(C31, C41, C42) | 나중에 | 강체↔격자 연동 항목 |
| 파괴(DESTRUCTION_BRIDGE, C68~C80) | 나중에 | 파괴 항목. 볼록 polygon API가 필요 |
| 강체 갱신 순서(FRAME_ORDER) | 나중에 | 게임에 붙일 때 |

## 채택 / 변경 / 제외

- 그대로 씀: 없음. 이번 범위에서 Noita 값을 옮길 규칙이 없습니다.
- 바꿔서 씀: 없음.
- 쓰지 않음: double 정밀도(위 대조표, 자체 설계).

## 자료에 없음 / OPEN → 우리 설계

TPT에는 강체가 없습니다(references/tpt.md). 모두 우리 측정으로 정합니다.

| 빈칸 | 자료 상태 | TPT 방식 | 우리 설계 | 측정 방법 | 추가 분석 제안 |
|---|---|---|---|---|---|
| 한 틱의 강체 시간과 sub-step 수 | 2026-11-07 C131이 답함: Noita는 2.x 반복 6/2, Step 중앙값 0.24~0.43 ms | 없음 | 🧪 틱마다 Step 한 번, dt 1/60 s, sub-step 4(Box2D 권장값). 6/2는 v3의 sub-step으로 옮길 수 없음(C131 2절). 연동에서 그대로 씀(design.md 8절) | `BenchmarkPile`, `BenchmarkBodies`(references.md 3절) | 아니오 |
| 스레드 | 2026-11-07 C131이 답함: 별도 스레드 하나, 결과를 쓰기 전에 기다림 | 없음 | 🔶 단일 스레드(worker 1개), 게임 스레드에서 동기로. 450개 장면의 Step 중앙값이 개발 PC 약 0.12 ms라 병렬이 필요 없음 | 같음 | 아니오 |
| 결정성 | 없음 | 없음 | 🔶 Box2D 쪽 조건(FMA 합치기 끄기, 자체 삼각함수, 비정규수 유지)을 지키고 300개 상자 해시를 골든으로 고정. SIMD 경로마다, 3개 OS마다 같음 | `TestDeterminismGolden`, `TestCompiledWithoutContraction` | 아니오 |
| 픽셀당 미터 배율, 좌표 방향 | 2026-11-07 C134가 답함: 10 px/단위 | 없음 | ✅ 2026-11-07 셀 0.1 m, y 아래(개발자 결정, design.md 8절) | 연동 작업 | 아니오 |
| 최대 강체 수 | 2026-11-07 C132가 답함: 동적 454~457개 | 없음 | ✅ 2026-11-07 최대 부하 장면은 동적 몸체 450개 🧪(개발자 결정, design.md 2절). 300개 더미는 결정성 골든으로 남김 | `BenchmarkBodies` | 아니오 |

## 검증 계획

- 테스트: `go test ./internal/physics/`. 결정성 골든, SSE2와 AVX2 비교, FMA 탐침, 입력 검사, 할당 0회.
- 스칼라 경로: `go test -tags box2dscalar -run Determinism ./internal/physics/`(`tools/check`가 돌림).
- 측정값과 기준: `go test -run '^$' -bench . -count 5 ./internal/physics/`. 300개 상자 한 틱이 기준 CPU 추정 16.7 ms 예산의 몇 %인지.
- 직접 플레이로 볼 점: 없음. 게임에 아직 붙지 않았습니다.
