# 강체↔격자 연동 참고 요약

작성: 2026-11-07 · 관련 작업: `feat/body-grid-coupling`

갱신: 2026-11-07 · `fix/body-pixel-holes`: 몸체 픽셀을 픽셀 중심의 앞쪽 사상(Noita) 대신 칸 중심의 역사상으로 씁니다(개발자 결정, "역사상으로"). 앞쪽 사상이 기운 몸체에 남기는 구멍으로 모래가 스며들었기 때문입니다. 아래 대조표와 빈칸을 그에 맞게 고쳤습니다.

## 범위

- 이번 변경이 다루는 메커니즘:
  - 몸체 픽셀을 격자의 셀로 쓰고 지우기(C134)
  - 몸체 픽셀이 겹친 칸의 셀 처리와 밀려난 셀의 재삽입(C134 5절, C50, C114, C115)
  - 셀 지형에서 강체 충돌 형상 만들기(C133)
  - 가루의 쉬는 시간 세기(C133의 분류 2, C134 5.1c가 씀)
- 이번 PR이 다루지 않는 것: 부력(C41)과 접촉 저항(C42)은 다음 PR, 몸체의 저장과 블록 상주는 그다음 PR, 파괴·입자·조인트는 범위 밖입니다(아래 "나중에").
- 함께 읽은 연관 메커니즘:
  - 강체 Step과 부하(C131 RIGID_BODY_STEP_PROFILE, C132 RIGID_BODY_LOAD_DESTRUCTION)
  - 액체 속 강체(C41, C42, C26, C31), 파괴 도형·질량(C05)
  - 액체 충돌과 위쪽 재배치(C50, C114, C115, 모델 `model_reinsert_chain`, `native_probe_cell_reinsert`)
  - 셀 휴면의 카운터(MATERIAL_TICK 4~5절)
  - 기존 [강체 기반 참고 요약](rigid-body-base.md)
- mechanics.md에 "강체↔격자 연동"과 "지형 충돌" 행이 없어 이번에 더했습니다.
- 검색: `재삽입`, `지형 충돌`, `CSolidCell`, `몸체 픽셀`. 새로 찾은 출처는 PLAYER_INTEGRATION 2절(지형 충돌 분류와 셀 클래스 ID를 구분)뿐이고, 플레이어 충돌이라 이번 범위 밖입니다.

## 관측 규칙

| 규칙 (자기 말로) | 근거 (절:줄) | 확인 수준 |
|---|---|---|
| 배율은 물리 단위 하나에 10픽셀(실측 10.0000004)이고, 원점이 따로 있다 | BODY_GRID_COUPLING:57 | LIVE |
| 몸체의 불투명 픽셀 하나하나가 격자의 셀이다. 몸체마다 고정된 셀 목록을 지웠다 다시 넣고, 새로 만들거나 버리지 않는다 | BODY_GRID_COUPLING:8-9, 61-67 | STATIC + LIVE |
| 픽셀의 로컬 좌표는 이미지 왼쪽 위를 원점으로 한 픽셀 중심 ((i+0.5)·0.1, (j+0.5)·0.1)이다 | BODY_GRID_COUPLING:65 | LIVE |
| 칸 = 축마다 floor(원점 + 배율 × (몸체 변환 × 로컬 좌표)). 픽셀에서 격자로 가는 앞쪽 사상이다. 0 쪽 절삭이 아니다 | BODY_GRID_COUPLING:72-76 | STATIC + LIVE(343,478건 불일치 0) |
| 같은 칸에 사상된 두 픽셀은 목록에서 나중 것이 칸을 차지한다. 17×11 상자(178픽셀)는 0·90·180°에 178칸, 40~50°에 149~152칸이다 | BODY_GRID_COUPLING:78-89 | LIVE |
| 배율이 정확히 10이 아니라 0°에서도 한 줄이 겹쳐 사라질 수 있다 | BODY_GRID_COUPLING:87-89 | LIVE |
| 한 프레임의 순서: 이전 Step 결과 → 움직인 몸체를 모두 지움 → key 순으로 씀 → (조인트) → 지형 충돌 갱신 → 셀 갱신 → 재삽입 → (파괴) → 다음 Step | BODY_GRID_COUPLING:93-97, 109 | STATIC + LIVE |
| 변환(x, y, 각)이 조금이라도 바뀌었거나 강제 표시가 있을 때만 지우고 다시 쓴다. 0.25픽셀 이동에도 다시 쓴다. 잠들었거나 변환이 같으면 셀이 그대로 남는다 | BODY_GRID_COUPLING:102-124 | STATIC + LIVE |
| 지울 때는 칸이 아직 자기 셀일 때만 비운다(공기). 비운 칸은 셀 갱신의 일반 이동과 재삽입만 채운다 | BODY_GRID_COUPLING:106-107, 215-223 | STATIC + LIVE |
| key는 목록 순번에서 우선도×1000을 뺀 값이다. 관측한 우선도는 모두 0 | BODY_GRID_COUPLING:104 | STATIC + LIVE |
| 쓸 칸이 비상주이거나 월드 밖이면 쓰지 않는다 | BODY_GRID_COUPLING:138 | STATIC |
| 비었거나 자기 몸체 셀이면 쓴다 | BODY_GRID_COUPLING:139 | LIVE |
| 고정 지형(rock_static)이면 쓰지 않는다 | BODY_GRID_COUPLING:140-143, 183 | STATIC + LIVE(1,712회) |
| 쉬는 상태(state 3)의 모래 중 지형 분류가 2가 아니거나, 몸체 이미지의 짧은 변이 8 미만이면 쓰지 않는다 | BODY_GRID_COUPLING:140-143, 182 | STATIC + LIVE(1,439회) |
| 분류 2의 모래는 그 픽셀의 속도²(물리 단위/s, 선속도 + ω×r)로 나뉜다: 5 미만은 쓰지 않음, 21 이상은 밀어냄, 그 사이는 확률 | BODY_GRID_COUPLING:144-147, 180-181 | LIVE(5 미만, 21 이상) / STATIC만(사이, OPEN) |
| 다른 몸체의 셀이면 내 key가 작을 때 쓰지 않고, 아니면 덮어쓴다. 덮인 몸체는 움직이기 전까지 그 픽셀이 격자에 없다(47프레임 관측) | BODY_GRID_COUPLING:151-158 | STATIC + LIVE |
| 그 밖의 셀(흐르는 액체, 움직이는 모래, 기체, 불)은 칸에서 빼고 몸체 픽셀을 쓴다. 액체는 몸체가 아래로 25px/s보다 빠르고 카메라 크기의 입자 영역 안이면 입자, 아니면 재삽입 대기열. 불은 삭제 | BODY_GRID_COUPLING:159-169, 173-192 | STATIC + LIVE |
| 대기열과 입자까지 세면 액체 셀의 정체성 집합이 모든 캡처에서 보존됐다. 바닥에 가라앉은 상자 아래 물 400셀이 retry에 계속 남아 수면이 오르지 않았다 | BODY_GRID_COUPLING:194-206 | LIVE |
| 몸체 위에 쌓인 셀은 몸체에 무게를 주지 않고 깨우지도 않는다 | BODY_GRID_COUPLING:225-233 | LIVE |
| 몸체 픽셀 단계의 비용: 기본 부하 장면에서 중앙값 0.442 ms(p95 1.129, 최대 1.847), 픽셀 중앙값 5,372, 몸체 297개(9800X3D, 계측 상태) | BODY_GRID_COUPLING:237-252 | LIVE |
| 재삽입: 셀의 원래 좌표부터 위로 129행(시작 포함). 범위 밖과 비상주는 건너뜀. 빈칸이면 넣고, type 3이 아니고 더 가벼운(밀도 strict) 셀이면 그 셀을 밀어내고 넣음. type 3이면 바로 실패. 같거나 무거운 다른 셀은 통과. 6행 이상 올라간 뒤 장애물마다 20% 확률로 x를 ±1(반반) 옮김 | C50:1352-1363, `native_probe_cell_reinsert` | EXECUTED + LIVE |
| 재삽입 스케줄러: retry를 먼저, 무작위 시작 index부터 예산만큼 시도(새 요청이 있으면 예산 ≤300, 그리고 ≤retry 수). 성공은 마지막 원소로 덮어 제거. 성공이 있으면 예산 500, 없으면 20보다 클 때 절반. 새 요청(fresh)은 모두 시도하고 실패는 retry 끝에, 예산 500 | C50:1365-1379 | EXECUTED |
| 밀려난 셀: retry 시도에서는 retry 끝에 바로 붙어 같은 호출에서 다시 시도될 수 있고, fresh 시도에서는 다음 호출의 fresh가 된다 | C115:3412-3417 | EXECUTED |
| 계속 실패하는 501개의 예산은 500→250→125→62→31→15에서 멈춘다. 만료 없는 셀은 끝까지 큐에 남는다 | C114:3386-3390 | EXECUTED |
| 만료 기한이 있는 셀은 기한 뒤 삭제, retry 100,000개 초과분 삭제 | C50:1370, 1377-1379, C114:3393-3395 | EXECUTED(합성) |
| 지형 충돌 분류: 고정/부착 셀 1, 쉬는 모래(state 3)이고 카운터 ≥ 20이면 2, 나머지(액체, 움직이는 모래, 기체, 불, 몸체 셀) 0 | TERRAIN_COLLISION:27-46 | STATIC + NATIVE(320조건) |
| 모래 경로의 카운터는 state 3이면 늘고(51까지) 아니면 0이 된다. 셀이 옮겨지면 1 줄어든다 | MATERIAL_TICK 4절(:48), 5절(:65) | STATIC + EXECUTED |
| 분류 배열과 재생성 단위는 64×64 영역이다. 분류가 바뀐 영역의 기존 지형 몸체를 모두 지우고 새로 만든다. 옆 영역은 따로 | TERRAIN_COLLISION:61-77 | STATIC + LIVE |
| 형상은 내부를 채운 볼록 분할이 아니라 경계를 따라 얇은 polygon을 만든다. 한 칸 파임이 경계에 그대로 남지 않는 단순화가 있다 | TERRAIN_COLLISION:79-98 | STATIC + LIVE |
| 갱신 요청은 강체의 현재·속도 예측 AABB에서 온다. 요청이 있고 마지막 갱신 뒤 10틱이 지나야 분류를 다시 훑는다(20틱 넘으면 영역 전체). 분류가 바뀐 영역은 다음 프레임 Step 전에 재생성된다 | TERRAIN_COLLISION:100-124, BODY_GRID_COUPLING:118 | STATIC + NATIVE + LIVE |
| 근처에 강체가 없으면 지형 충돌을 만들지 않는다 | TERRAIN_COLLISION:152-160 | LIVE |
| 한 프레임에 24영역(fixture 96개) 재생성이 7.69 ms였다(계측 포함). 한 셀 삭제 영역 하나는 약 0.05~0.06 ms | TERRAIN_COLLISION:163-170 | LIVE |
| 강체 Step은 dt = float32(1/60), 속도 반복 6, 위치 반복 최대 2(Box2D 2.x 계열). 별도 스레드 하나에서 돌고 다음 결과를 쓰기 전에 기다린다 | RIGID_BODY_STEP_PROFILE:15-59 | STATIC + LIVE |
| Step 시간: 강체 128개 추가 장면 중앙값 0.24~0.28 ms, 기본 부하 장면 0.43 ms | RIGID_BODY_STEP_PROFILE:103-107, RIGID_BODY_LOAD_DESTRUCTION:83-91 | LIVE |
| 실제 지형의 동적 몸체 454~457개(깨어 있는 것 360~410), touching 접촉 약 270, 조인트 약 360 | RIGID_BODY_LOAD_DESTRUCTION:60-81 | LIVE |
| 몸체의 질량: 64×8 나무 3.072, 강철 4.096. 파편 밀도 약 0.6 | C31:533, C05:78 | RUNTIME |
| 공기 중 몸체의 vy는 틱마다 약 2px/s 늘고, 부력 시험의 중력은 12 | C31:537, C41:911-912 | RUNTIME + LIVE |

## 현재 코드 대조

이번 변경 전에는 연동이 없었습니다. 아래는 이번 PR의 구현과의 대조입니다.

| 관측 규칙 | 우리 코드 (파일:줄) | 같음 / 다름 / 없음 | 다름이면 |
|---|---|---|---|
| 배율 10px/단위 | `internal/rigid/rigid.go:41` `cellsPerMeter` | 같음(정확히 10) | 원점은 0. 셀 (0, 0)의 왼쪽 위가 (0 m, 0 m)이고 y는 아래로 큼(개발자 결정, design.md 8절) |
| 픽셀 하나 = 셀 하나, 같은 목록 재사용 | `internal/rigid/body.go:32` `body`, `internal/world/body.go:12` `BodyCell` | 다름(의도) | 몸체는 이미지(픽셀마다 색조)와 쓴 칸의 목록을 지님. 역사상이라 한 픽셀이 두 칸에 쓰이거나 한 칸에도 쓰이지 않을 수 있음. 셀은 값이라 객체 대신 (몸체 칸, 픽셀 번호) 연결을 셀에 둠(아래 "셀의 연결") |
| 픽셀 중심의 앞쪽 사상과 축별 floor | `internal/rigid/body.go:123` `write` | 다름(의도) | 칸마다 그 중심을 몸체 변환의 역으로 이미지에 옮겨 축마다 floor한 픽셀을 씀. 중심이 이미지 밖인 칸은 쓰지 않음. 구멍이 없고 칸 중심이 Box2D 모양 밖으로 나가지 않음. 처음 구현은 Noita와 같은 앞쪽 사상이었고, 기운 몸체의 구멍으로 모래가 스며들어 같은 날 바꿈(개발자 결정, design.md 8절) |
| 중복 칸은 나중 픽셀 | `internal/rigid/body.go:203` `claim` | 해당 없음 | 역사상은 칸마다 한 번 판정해 한 몸체 안의 중복이 없음. 자기 칸에 쓰는 것은 움직이지 않은 몸체를 다시 쓸 때뿐 |
| 0°에서 한 줄이 사라짐 | `internal/rigid/rigid.go:185` `add` | 다름 | 배율이 정확히 10이고 상자 중심이 칸 중심이나 모서리에 놓여 칸 중심이 픽셀 경계에서 반 픽셀 떨어지므로, 돌지 않은 상자는 픽셀마다 꼭 한 칸 |
| 프레임 순서 | `internal/game/game.go:490` `Step` | 같음(입자·조인트·파괴 없음) | 동기 실행. Box2D Step은 틱 끝, 결과는 다음 틱 처음에 씀 |
| 변환이 바뀌면 다시 씀, 잠든·그대로인 몸체는 셀 유지 | `internal/rigid/rigid.go:216` `Write` | 같음 | 각도 대신 위치와 회전(cos, sin)의 float 값을 비교 |
| 자기 셀일 때만 지움 | `internal/rigid/body.go:99` `erase` | 같음 | 쓴 칸의 목록을 돌며 몸체 칸 번호만 비교. 픽셀 번호는 여러 칸에 겹칠 수 있어 비교하지 않음 |
| key | `internal/rigid/rigid.go:185` `add` | 다름 | 우선도가 없어 만든 순서(번호)를 key로 씀. 목록 순번처럼 앞의 몸체가 먼저 쓰임 |
| 비상주·월드 밖은 안 씀 | `internal/rigid/body.go:203` `claim` | 같음 | |
| 고정 지형은 안 씀 | 같은 곳 | 같음 | |
| 쉬는 모래: 분류 2가 아니거나 짧은 변 < 8이면 안 씀 | 같은 곳 | 같음 | "쉬는 상태"는 떨어지는 중이 아니고 쉰 틱이 1 이상인 가루(`Cell.Rest`) |
| 분류 2 모래의 속도² 5/21 | `internal/rigid/body.go:237` `givesWay` | 같음(5 미만, 21 이상) / 다름(사이) | 속도는 그 칸 중심에서 몸체의 속도. 사이 구간은 OPEN이라 우리 설계: (v²−5)/16 확률, 난수는 (시드, 틱, 몸체 key, 픽셀) 해시 🧪 |
| 다른 몸체 셀은 key로 | 같은 곳 | 다름(보완) | 덮인 몸체는 덮은 몸체가 움직일 때 그 틱에 다시 씀(가려진 칸 수와 지워진 범위로 판단). Noita는 덮인 몸체가 움직일 때까지 구멍이 남음(47프레임 관측). 쉬는 상자 더미에서 구멍이 영영 남지 않게 함(자체 설계, 🔶) |
| 흐르는 액체·움직이는 모래 → 입자 또는 대기열 | `internal/world/body.go:51` `Displace` | 다름 | 입자가 아직 없어 모두 재삽입 대기열(개발자 결정 4). 카메라 의존(입자 영역)은 따르지 않음 |
| 기체 → 입자, 불 → 삭제 | 없음 | 없음 | 기체·불 재질이 아직 없음. 넣을 때 기체는 대기열, 불은 삭제(개발자 결정 4) |
| 몸체 위 셀의 무게 없음 | 해당 없음 | 같음 | 셀이 몸체에 힘을 주는 경로가 아직 없음(부력·저항은 다음 PR) |
| 재삽입 탐색 | `internal/world/reinsert.go:120` `reinsert` | 같음 | 고정 재질(Static)은 밀어내지 않고 통과만 함. 지금 돌은 밀도 255라 어차피 밀리지 않음 |
| 재삽입 스케줄러(예산, retry 먼저, 밀려난 셀) | `internal/world/reinsert.go:59` `reinsertQueued`, `:79` `retryCells` | 같음 | 난수는 전역 LCG 대신 (시드, 틱) 스트림 |
| 만료 삭제, 100,000 상한 | 없음 | 다름 | 셀 보존을 위해 두지 않음. 몸체가 밀어낸 셀에는 만료가 없음(BODY_GRID_COUPLING:194-206에서 끝까지 남음) |
| 지형 분류 0/1/2 | `internal/world/collision.go:25` `Classify` | 같음 | 부착(+22)·sticks_to_ceiling·never_box2d가 없어 분류 1은 Static, 2는 쉰 틱 ≥ 20인 가루 |
| 쉬는 카운터 | `internal/world/motion.go:98` `updatePowder`, `:208` `swap` | 다름 | 쉬는 상태로 갱신을 마칠 때 1씩 늘고 20(🧪)에서 멈춤. 움직이면 0(Noita는 옮겨질 때 1 줄임). 51이 아니라 20까지인 것은 셀 휴면이 아직 없어서 |
| 64×64 영역 재생성 | `internal/rigid/terrain.go:127` `refresh` | 같음 | |
| 얇은 polygon | `internal/rigid/terrain.go:286` `outline` | 다름 | Box2D v3 chain(한쪽 면 선분, 유령 점으로 이음)과 Douglas–Peucker 단순화(🧪 0.75칸) |
| 요청: 현재·예측 AABB, 10틱 gate | `internal/rigid/terrain.go:76` `want`, `:45` `Collide` | 다름 | gate 없이 매 틱 Step 전에 분류를 비교해 바뀐 청크만 재생성(결정적, 🔶) |
| 근처에 강체가 없으면 안 만듦 | 같은 곳 | 같음 | |
| Step 설정 | `internal/rigid/rigid.go:48` `subSteps` | 다름 | Box2D v3.2라 반복 수 대신 sub-step 4(🧪). dt 1/60 |
| 질량: 재질 밀도 × 0.1 kg/m² | `internal/rigid/rigid.go:51` `densityScale` | 같음 | |
| 중력 12 | `internal/rigid/rigid.go:43` `gravity` | 같음 | 🧪(개발자 결정 1) |

## 증상 재현

해당 없음. 새 메커니즘입니다.

## 실제 수치

| matter-wake 재질 | Noita 재질 | 필드 | Noita 값 | 변환식 | matter-wake 값 |
|---|---|---|---|---|---|
| wood(새 재질, kind solid) | wood_prop | density | 6 | Box2D 밀도 = 6 × 0.1 kg/m²(C31:533, C05:78) | density 6 → 0.6 kg/m² |
| wood | wood_prop | solid_friction | 0.9 | 그대로(Q8로 0.8984) | friction 0.9 |
| wood | wood_prop | solid_restitution | 0.2 | 그대로(Q8로 0.1992) | restitution 0.2 |
| (몸체 중력) | — | gravity | 12(C41:912) | 그대로, y 아래 | 12 m/s² 🧪 |
| sand | sand | 쉬는 카운터 문턱 | 20(TERRAIN_COLLISION:38-39) | 그대로 | `world.SettleTicks` 20 🧪 |

`go run ./tools/noitaref -fields density,solid_,cell_type material wood_prop wood steel`로 확인했습니다. wood_prop의 durability 8, hp 5000은 파괴 작업에서 씁니다.

## 연관 메커니즘

| 메커니즘 | 이번에 반영 | 이유 |
|---|---|---|
| 재삽입(C50, C114, C115) | 예 | 몸체가 밀어낸 셀이 이 경로로 돌아옴(개발자 결정 4). 액체끼리의 충돌(C50 1절)은 여전히 쓰지 않음(references.md "C50을 구현하지 않는 이유") |
| 부력(C41), 접촉 저항(C42) | 나중에(다음 PR) | 셀이 몸체에 힘을 주는 경로. 이번 PR의 몸체는 물속에서 가라앉기만 함 |
| 튀는 액체 입자(C81~C85) | 아니오 | 별도 로드맵 항목. 그때 아래로 빠른 몸체가 밀어낸 액체를 입자로 바꿀지 다시 봄 |
| 파괴(C68~C80) | 아니오 | 범위 밖. 몸체 이미지와 셀의 픽셀 번호가 그 바탕 |
| 조인트 | 아니오 | 범위 밖 |
| 몸체 저장·스트리밍 | 나중에 | 그때까지 몸체는 방 장면(샌드박스, 붕괴)에만 있음 |

## 채택 / 변경 / 제외

- 그대로 씀:
  - 지우기를 모두 마친 뒤 key 순으로 쓰기, 변환이 같으면 건드리지 않기(Noita 관측)
  - 겹친 칸의 판정 순서와 문턱(5, 21, 짧은 변 8)(Noita 관측)
  - 재삽입 탐색과 스케줄러(Noita 관측, 개발자 결정 4)
  - 지형 분류 1/2와 카운터 문턱 20, 64×64 영역 재생성, 몸체 근처만(Noita 관측, 🔶)
  - 질량 = 재질 밀도 × 0.1, 중력 12(Noita 관측)
- 바꿔서 씀:
  - 픽셀 중심의 앞쪽 사상 대신 칸 중심의 역사상(개발자 결정 2026-11-07, "역사상으로"). Noita의 앞쪽 사상은 기운 몸체에 구멍을 남김(관측)
  - 입자 대신 모든 밀려난 셀을 대기열로, 카메라 의존 없음(개발자 결정 4)
  - 덮인 몸체는 덮은 몸체가 움직이면 다시 씀(자체 설계, 쉬는 더미에 구멍이 남지 않게)
  - 분류 2 사이 구간의 확률(자체 설계, OPEN)
  - 지형 형상: 얇은 polygon 대신 chain, 단순화 허용값 🧪(자체 설계)
  - 재생성 시점: 10/20틱 gate 대신 매 틱 Step 전 비교(자체 설계, 결정성)
  - 카운터: 20에서 멈춤, 움직이면 0(자체 설계)
- 쓰지 않음:
  - 입자 영역(카메라 의존), 만료 삭제와 100,000 상한(셀 보존), 우선도(아직 필요 없음)
  - Step의 12 ms 넘으면 몸체를 static으로 바꾸는 경로(RIGID_BODY_STEP_PROFILE:116-129): 측정 뒤 필요할 때

## 셀의 연결 (자체 설계)

셀은 16바이트 값이라 Noita처럼 셀 객체가 몸체를 가리킬 수 없습니다. 몸체 셀은 재질 종류가 `solid`이고, 셀 규칙이 움직이지 않으므로 움직임 칸(`Vy`, `Ax`)을 쓰지 않습니다. 그 4바이트에 몸체 칸 번호(`Vy`)와 이미지의 픽셀 번호(`Ax`)를 둡니다(`world.BodyCell`, `Cell.Body`). 셀 크기는 16바이트 그대로입니다.

- 역사상이라 같은 픽셀 번호가 여러 칸에 있을 수 있습니다. 몸체가 쓴 칸의 목록을 따로 지니고, 지울 때 그 목록을 씁니다.
- 대가: 월드에 몸체 65,536개, 몸체 하나에 픽셀 65,536개(256×256)까지입니다. Noita의 가장 큰 관측은 몸체 1,071개(RIGID_BODY_LOAD_DESTRUCTION:71), 이미지 33,280픽셀(:95)입니다.
- 남는 2바이트 중 1바이트는 쉬는 카운터(`Rest`, 개발자 결정 6)이고, 1바이트가 남습니다.

## 자료에 없음 / OPEN → 우리 설계

TPT에는 강체가 없습니다(references/tpt.md:29, 50). 아래는 모두 우리 측정과 설계입니다.

| 빈칸 | 자료 상태 | TPT 방식 | 우리 설계 (우리 제약에 맞춘 방법) | 측정 방법 | 추가 분석 제안 |
|---|---|---|---|---|---|
| 몸체를 그리는 방법 | Noita가 몸체를 격자 셀로만 그리는지 이미지로 따로 그리는지 자료에 없음(검색 "몸체 렌더", "강체 그리기" 결과 없음). 앞쪽 사상의 구멍이 화면에 보이는지 모름 | 없음 | 몸체는 격자의 셀로만 그림. 역사상이라 구멍이 보이지 않음 | 리플레이 화면 | 아니오 |
| 역사상의 칸 고르기 | Noita에 없음(앞쪽 사상만 관측, BODY_GRID_COUPLING:72-89) | 없음 | 칸 중심이 이미지 안이면 그 픽셀(축마다 floor). 행마다 사각형이 지나는 구간을 1/64칸 넓혀 훑고 칸마다 판정. 몸체 범위는 돌린 사각형을 덮는 칸 범위라, 칸이 하나도 없는 작은 몸체도 근처 지형 충돌을 만듦 | `TestTurnedBodyHasNoHoles`, `TestRowSpansMissNoCell`, `TestBodyWithoutCellsStillCollides`, `TestSandStaysOutOfATurnedBody` | 아니오 |
| 분류 2 모래의 5 ≤ v² < 21 확률 | STATIC만, 난수원·분포 OPEN(BODY_GRID_COUPLING:146, 294) | 없음 | (v²−5)/16 확률. 난수는 순서와 무관한 (시드, 틱, key, 픽셀) 해시 🧪 | `TestSettledSandGivesWayByPixelSpeed` | 아니오(설계를 아직 시험하지 않음) |
| 덮인 몸체가 다시 나타나는 때 | Noita는 움직일 때까지 구멍(관측), 의도인지 모름 | 없음 | 다른 몸체에 가려진 픽셀이 있는 몸체는, 움직여 지워진 몸체의 범위와 겹치면 같은 틱에 다시 씀 🔶 | `TestCoveredBodyComesBack`, `TestPixelCoveredTwiceComesBack` | 아니오 |
| 지형 형상의 단순화 허용값 | 한 칸 파임이 사라지는 사례 하나뿐(TERRAIN_COLLISION:91-98) | 없음 | Douglas–Peucker 0.75칸 🧪(선분까지의 거리). 45° 계단은 바깥 모서리를 잇는 직선, 한 칸 돌기는 모서리 하나가 잘린 채 남음. 열린 chain은 양 끝의 격자 변을 남겨 유령 점이 이웃 청크의 변과 맞음 | `TestContourSimplifiesStairs`, `TestOutlineCoversEveryEdge` | 아니오 |
| 청크 경계에서 형상 잇기 | 옆 영역은 따로 재생성된다는 것만(TERRAIN_COLLISION:71-72) | 없음 | 경계에서 끝나는 열린 chain, 유령 점은 옆 청크의 다음 꼭짓점. 고체끼리 맞닿은 청크 경계에는 선분이 없음 | `TestTerrainChainsMeetAtChunkBorders`, 상자가 경계를 미끄러져 넘는 테스트 | 아니오 |
| 지형 마찰 | OPEN(TERRAIN_COLLISION:59, 242) | 없음 | 모든 지형 선분에 마찰 0.6 🧪(Box2D 기본값) | 직접 플레이 | 아니오 |
| 지형을 다시 만들 때 잠든 몸체 | Noita에서 지형 몸체를 바꿀 때 근처 몸체를 깨우는지는 자료에 없음 | 없음 | Box2D v3.2는 정적 몸체를 지워도 그 위의 잠든 몸체를 깨우지 않음. 다시 만든 청크에 닿을 수 있는 몸체를 깨움 🔶 | `TestTerrainRebuildsOnlyOnChange` | 아니오 |
| 재생성 시점 | worker가 돌 때 10/20틱 gate, 지연 최대 62프레임 관측(TERRAIN_COLLISION:126-150) | 없음 | 매 틱 몸체 쓰기 뒤 Step 전에, 몸체 근처 청크의 버전이 바뀌었으면 분류를 다시 계산해 바뀐 청크만 재생성(결정적, 🔶) | `BenchmarkBodies`의 terrain 단계 | 아니오 |
| 몸체 근처의 범위 | 현재·예측 AABB만 STATIC | 없음 | 칸 단위 AABB와 한 틱 뒤 예측을 합치고 4칸(🧪) 넓힌 범위에 걸친 청크 | 같음 | 아니오 |
| 월드 가장자리 | 없음 | 없음 | 월드 밖에 정적 상자 벽. 분류에서 월드 밖과 비상주는 고정(1) | `TestBodyStaysInTheWorld` | 아니오 |
| 몸체를 놓는 방법 | Noita는 개체 생성(범위 밖) | 없음 | 개발용 도구: 커서에 재질·크기가 정해진 상자를 떨어뜨림(입력 `Drop`, 리플레이 `B`). 자리에 돌·가루·몸체가 있으면 놓지 않음 🔶 | 리플레이 골든 | 아니오 |
| sub-step 수 | Noita는 2.x 반복 6/2라 옮길 수 없음(RIGID_BODY_STEP_PROFILE:44-45) | 없음 | Box2D 권장 4 🧪 | `BenchmarkBodies` | 아니오 |

## 검증 계획

- 테스트:
  - world: 쉬는 카운터(`TestRestCountsTicksAtRest`), solid 셀은 움직이지 않고 막음, 재삽입 탐색과 스케줄러(`TestReinsert*`), 분류(`TestClassify`), 기존 `TestSleepMatchesScanningEverything`이 카운터와 함께 통과
  - rigid: 역사상(0~360°를 0.5° 간격으로 홀짝 크기에서 구멍 없음과 float64 오라클, 행 구간과 상자 전체 판정의 비교, 기운 상자 위 모래가 표면에 머묾), 지우기·쓰기, 겹친 칸 판정, 지형 윤곽(섬은 고리, 경계는 열린 chain), 상자가 돌·모래 위에 멈춤, 물 셀 보존(격자 + 대기열), 할당 0회
  - physics: chain 지형 위의 상자, 한쪽 면, 속도·변환 읽기
  - replay: 형식 6(`B` 버튼, `body` 줄), 몸체가 있는 골든에서 매 틱 (몸체 아닌 재질) 격자 + 대기열 + 인벤토리 = 시작, 몸체 픽셀 = 도구로 놓은 만큼
- 측정값과 기준: `go test -run '^$' -bench 'Bodies|WriteTurned' -count 5 ./internal/rigid/`. 몸체 450개(개발자 결정 3)에서 쓰기·지형·Step을 따로 중앙값, p95, 최대로 보고, 기준 CPU 추정이 3 ms(🧪, 개발자 결정 2) 안인지 봄
- 비교 화면: `go run ./cmd/matter-wake -replay internal/replay/testdata/bodies.rpl -shots 0,60,120,300 -shots-dir out/shots`
- 직접 플레이로 볼 점: 물에 떨어진 상자가 물을 밀어내는 모양과 수면 높이(가라앉은 상자 아래 물은 대기열에 남아 수면이 그만큼 낮음), 가라앉기(부력은 다음 PR), 모래 더미에 떨어진 상자가 멈추는 깊이, 상자끼리 쌓인 모양, 기운 상자의 가장자리 모양
