# 몸체 저장과 블록 상주 참고 요약

작성: 2026-11-09 · 관련 작업: `feat/body-saves`

## 범위

- 이번 변경이 다루는 메커니즘:
  - 512×512 블록을 내릴 때 그 블록의 몸체를 격자에서 지우고, 블록과 함께 저장하고, Box2D에서 빼는 것
  - 블록을 올릴 때 몸체를 key 순으로 다시 만들어 쓰는 것
  - 블록의 칸에서 꺼내 재삽입을 기다리던 셀
  - 세션 저장(종료 때 메모리의 모든 블록)과 다시 연 세션이 매 틱 같게 이어지는 것
  - 지형 장면의 몸체 놓기 도구(`B`)
- 함께 읽은 연관 메커니즘(mechanics.md의 "Streaming, residency, saving" 행과 Related):
  - WORLD_STREAMING 저장 절(:42-52), STREAMING_POLICY 4·5절(:76-103), CELL_LIFECYCLE_STREAMING(C130) 6절과 맺음 지침(:149-199), C07(:104-114), C17(:229-251)
  - 몸체↔격자 연동(BODY_GRID_COUPLING 4·5절, :91-117, :137-139), 파괴의 저장 연결(DESTRUCTION_BRIDGE:33-38), C135(BODY_BUOYANCY_FRAGMENTS:8-27)
  - 재삽입 대기열(C50, C114, C115, [강체↔격자 연동 참고 요약](body-grid-coupling.md)), 지형 충돌(C133)
  - 상주 규칙의 모델 `model_streaming_policy`는 [스트리밍 참고 요약](streaming.md) 때 읽었습니다. 그 모델에는 몸체가 없어 이번에 다시 볼 것이 없습니다.
- 검색: `search 저장 body`, `search 00740e70`, `search 비상주 body`, `search 언로드 body`, `search 퇴거 강체`, `search body 소유`, `search 경계 body`, `search 재진입 body`, `search type3 저장`. 처음에는 소유·걸침·가장자리에 대한 문장이 하나도 없었습니다.
- 추가 분석 C137(2026-11-09, 개발자 승인 "C137 먼저, C136 다음", 분석 폴더의 별도 세션, `doc BODY_BLOCK_RESIDENCY`, 근거 `analysis-detail/resid-c137`, 주장 22개 재검증 통과): 소유, 걸친 몸체, 비상주 블록으로 가는 몸체, 왕복에서 남는 것을 LIVE로 답했습니다. 처음 구현은 결과 없이 🔶로 했고, 결과가 온 뒤 아래 표와 코드를 고쳤습니다(소유 규칙을 바꾸고, 가장자리와 왕복은 일부러 다르게 둠).

## 관측 규칙

| 규칙 (자기 말로) | 근거 (절:줄) | 확인 수준 |
|---|---|---|
| 블록을 내리는 함수는 그 블록 안의 몸체를 모으고, 조인트로 이어진 몸체까지 재귀로 모읍니다. 조인트는 두 종류만 확인했습니다 | WORLD_STREAMING:42-45 | STATIC |
| 몸체마다 픽셀 배열, 식별자, 화면 좌표로 바꾼 위치, 각도, 두 속도, 활성·수면 같은 비트 플래그, 연결 정보를 저장용 구조에 복사합니다. 파일 규격과 필드의 뜻은 확정되지 않았습니다 | WORLD_STREAMING:46-48, DESTRUCTION_BRIDGE:37 | STATIC |
| 고른 블록은 몸체·개체를 모은 뒤 블록 표에서 빠지고 저장 작업으로 갑니다. 디스크 저장 완료와 상주 해제는 따로 일어납니다 | STREAMING_POLICY:97-101 | STATIC |
| 저장 → 다시 실행에서 상자 몸체의 ID, fixture 정점 원시값, 질량, 관성이 남았습니다. 개체 ID는 바뀌었고, API로 바꾼 gravity_scale은 1로 돌아왔습니다 | C07:104-113 | RUNTIME |
| 블록이 내려갔다 돌아온 왕복에서도 몸체 ID, 질량, 관성, 셀 수가 남았습니다. 돌아온 뒤에도 물리가 이어져 좌표와 각도가 같은 순간의 값이라고 할 수는 없습니다 | C17:241-246 | RUNTIME |
| 블록 셀 인코더는 칸마다 재질 코드와 바뀐 색만 씁니다. 운동·휴면 상태는 쓰지 않고, 읽을 때 셀을 새로 만듭니다. 물리 고체(type 3) 칸은 따로 플래그를 다루는데, 그 내용은 이 범위에서 확인하지 않았습니다 | CELL_LIFECYCLE_STREAMING:149-171 | EXECUTED |
| 몸체 픽셀을 쓸 칸이 상주하지 않거나 월드 밖이면 쓰지 않습니다. 지울 때도 상주하고 경계 안이며 아직 그 몸체의 칸일 때만 지웁니다 | BODY_GRID_COUPLING:107, :138 | STATIC |
| 재질로 칠한 몸체와 새 파괴 조각에는 개체가 없습니다. 블록을 붙잡아 두는 keep-alive는 개체의 컴포넌트가 하는 일입니다 | BODY_BUOYANCY_FRAGMENTS:8-20, WORLD_STREAMING:34-40 | LIVE / STATIC |
| (분석가의 제안, Noita 사실 아님) 정확하게 이어 가려면 순간 상태도 저장해야 하고, 재구성 방식이면 로드 뒤의 차이를 따로 시험해야 합니다 | CELL_LIFECYCLE_STREAMING:198-199 | 제안 |
| 몸체는 블록에 속하지 않습니다. 블록을 내리는 순간 그 블록 사각형으로 Box2D AABB를 질의해, 겹치는 몸체를 통째로 그 블록 파일에 저장하고 세계에서 지웁니다. 정지한 16×8 상자는 블록과 1픽셀 떨어지면 수집됐고 2·3·4픽셀이면 아니었습니다(AABB의 여유). 중심, 원점, 셀이 어느 블록에 많은지와 상관없이 먼저 내려가는 블록이 가져가고, 한 몸체는 한 파일에만 들어갑니다 | BODY_BLOCK_RESIDENCY 1절, 3.1-3.3 | STATIC + LIVE |
| 걸친 몸체가 내려가면, 남는 이웃 블록에 있던 그 몸체의 칸도 같은 순간 공기가 되고 Box2D 몸체도 없어집니다. 남는 블록에 고아 셀이나 몸체가 없고, 어느 쪽이 먼저 내려가도 대칭입니다 | BODY_BLOCK_RESIDENCY 4절 | LIVE |
| 비상주 블록으로 들어가는 몸체는 멈추지 않고 계속 Step됩니다. 그 블록에는 픽셀을 쓰지 못해 놓인 칸이 0이 되고, 작은 몸체 청소(C135)가 40프레임 뒤 지웠습니다. 저장되지 않아 영영 사라졌습니다. 몸체는 블록을 요청하지도, 내리기를 늦추지도 않습니다 | BODY_BLOCK_RESIDENCY 5절 | LIVE |
| 왕복에서 b2Body id, 위치, float32로 반올림한 각도, damping, 빠진 픽셀이 든 이미지는 남고, 선속도·각속도는 0으로, gravity_scale은 1로, 잠든 몸체는 깨어 돌아옵니다. 저장 함수가 속도와 gravity_scale을 읽지 않습니다. 몸체를 먼저 격자에서 지운 뒤 셀을 저장해, 몸체 픽셀이 셀로 이중 저장되지 않습니다 | BODY_BLOCK_RESIDENCY 6.1-6.3 | STATIC + LIVE |
| 내려가는 블록의 재삽입 대기열 셀·액체 입자, 비상주 블록 아래로 떨어지는 몸체, 움직이는 몸체의 AABB 여유, 조인트로 이어진 몸체의 LIVE 수집은 미확정입니다 | BODY_BLOCK_RESIDENCY 6.4, 7절, 10절 | OPEN |

## 현재 코드 대조

| 관측 규칙 | 우리 코드 (파일:줄) | 같음 / 다름 / 없음 | 다름이면 |
|---|---|---|---|
| 블록의 몸체를 모아 저장 | internal/rigid/save.go `Unload`, `leavesWith`, internal/game/stream.go `unload` | 같음 | C137 전에는 "블록 안"의 기준이 자료에 없어 질량 중심이 든 블록으로 정했습니다. C137 뒤 Noita처럼 바꿈: 지금 변환의 돌린 사각형 범위를 1칸(🧪 `unloadMargin`) 넓혀 블록에 닿으면 함께 나감. Box2D AABB 대신 이미지 사각형의 범위를 씀(조각은 형상보다 조금 큼). 조인트가 없어 재귀 수집도 없음 |
| 걸친 몸체는 통째로, 남는 블록의 칸도 비움 | internal/rigid/save.go `Unload`(`erase`) | 같음 | `TestBodiesLeaveWithEveryBlockTheyTouch`가 양쪽 방향을 확인 |
| 비상주 블록으로 들어가는 몸체 | internal/rigid/save.go `freeze` | 다름(의도) | Noita는 계속 Step되다 청소로 사라짐(재질을 잃고, 청소는 전역 난수). 셀 보존과 결정성 때문에 얼립니다. references.md에 적음 |
| 왕복에서 속도·수면을 버림 | internal/rigid/save.go `restoreMotion` | 다름(개발자 결정 대기) | 우리는 속도와 수면을 저장하고 되돌립니다. Noita처럼 하려면 `restoreMotion` 하나를 바꿉니다(아래 빈칸) |
| 저장하는 필드 | internal/save/bodies.go `Body` | 같음(뜻) | 픽셀마다 있음·색조·손상, key, 재질, 원점의 위치와 회전(cos, sin), 질량 중심의 속도와 각속도, 깨어 있는지, 처음 상자 그대로인지. 화면 좌표 대신 Box2D의 미터. 각도 대신 cos·sin이라 다시 만든 회전이 비트까지 같음 |
| 몸체 → 블록 표에서 빼기 → 저장 순서 | internal/game/stream.go `unload` | 같음 | 몸체를 지우고 기록 → 대기열의 셀 → 셀 → 저장소. 저장소가 쓰기를 백그라운드에서 하는 것도 같음(`internal/app` prefetch.go) |
| 질량·관성이 남음 | internal/rigid/save.go `Load` | 같음 | 같은 형상을 같은 순서로 다시 만들어 질량, 관성, 질량 중심이 비트까지 같음(`TestLoadedBodyIsTheSame`) |
| 돌아온 뒤 물리가 이어짐 | internal/rigid/save.go `Load` | 같음 | 내린 블록의 몸체는 시간이 멈춘 채 있다가 떠날 때의 변환·속도로 돌아옴 |
| 셀 인코더는 재질과 색만 | internal/save/save.go | 다름 | 이미 적혀 있음(design.md 7절 저장 형식: 셀 필드를 모두 저장). 몸체 칸은 셀로 저장하지 않고 몸체로 저장함. Noita의 type 3 플래그 처리는 내용을 모름 |
| 비상주 칸에 쓰지 않고, 그 칸은 지우지 않음 | internal/rigid/body.go `claim`, `erase` | 같음 | |
| 개체 없는 몸체는 블록을 붙잡지 않음 | internal/game/stream.go `check` | 같음 | 몸체는 블록을 붙잡지 않음. 내릴 블록은 지금 규칙(카메라 영역과 화면 청크가 닿는 범위만 보호) 그대로 |

## Box2D의 상태 (v3.2를 직접 확인)

- `b2World_GetSnapshot`이 담는 것(`third_party/box2d/src/snapshot.c` `b2SerializeWorld`): 월드 설정, id pool 7개와 그 빈 번호 목록, solver set, 몸체·형상·접촉(manifold와 누적 충격량, 곧 warm starting), chain, sensor, island, broadphase 트리와 pair 집합, constraint graph 색입니다. 몸체의 수면 시간(`sleepTime`)은 몸체 구조에 있어 함께 들어갑니다. user data와 콜백은 빠집니다.
- 임시 측정(저장소 밖, `go test -overlay`): 상자 300개 장면을 30·120·400스텝 돌린 뒤 스냅샷으로 새 월드를 만들고(`b2CreateWorldFromSnapshot`), 같은 장면의 다른 월드에도 덮어쓴 뒤(`b2World_RestoreFromSnapshot`) 셋을 400스텝 함께 돌렸습니다. 매 스텝 `b2World_GetStateHash`와 모든 몸체의 위치·회전 비트가 같았습니다. 크기는 204~431 KB(몸체당 약 1.4 KB), 만드는 데 0.5 ms 이하였습니다. 몸체 handle은 월드 번호만 바꾸면 그대로 쓸 수 있었습니다.
- 그래도 쓰지 않았습니다(자체 설계):
  1. 같은 세션이 되려면 `internal/rigid`의 상태도 비트까지 저장해야 합니다. 몸체가 쓴 칸 목록, 마지막으로 쓴 변환, 가려진 칸 수, 범위와 이동량, 청크마다의 지형 분류와 정착 칸을 뺀 기억(keepOut)입니다. 하나라도 빠지면 조용히 어긋납니다.
  2. 형식이 "Experimental, will change"이고, 구조체 크기와 포인터 크기를 섞은 레이아웃 해시와 버전 13으로 묶여 있습니다. Box2D를 올리면 저장이 읽히지 않습니다.
  3. 메모리의 몸체가 블록 파일이 아니라 월드 전체 스냅샷에 들어가, 블록 파일이 스스로 완결되지 않습니다. 강제 종료 뒤에는 스냅샷과 블록 파일이 섞일 수 있습니다.
- 대신 쓰는 것: 저장을 재구성 지점으로 둡니다. 저장할 때 메모리의 블록을 행 순서로 하나씩 내리듯 모든 몸체와 대기열의 셀을 블록과 함께 꺼내 기록하고, Box2D 월드를 새로 만든 뒤, 다시 연 세션이 하는 것과 같은 순서(블록은 행 순서, 몸체는 key 순서)로 다시 올립니다. 다시 연 세션은 같은 기록을 같은 순서로 올리므로, 저장한 뒤 계속 간 세션과 매 틱 같습니다. 무엇을 빠뜨려도 두 세션이 함께 그것을 잃으므로 어긋나지 않습니다.
  - 대가: 저장하는 순간 깨어 있는 몸체는 접촉의 누적 충격량(warm starting)과 수면 시간을 잃습니다. 잠든 몸체는 잠든 채 다시 만들어 움직이지 않습니다. 저장은 지금 종료 때만 하므로 플레이에는 드러나지 않고, 자동 저장을 넣을 때 다시 봅니다.

## 실제 수치

이 변경은 재질 값을 바꾸지 않습니다. 쓰는 수치는 이미 정한 상주 수치(512 블록, [스트리밍 참고 요약](streaming.md))와 몸체 수치(셀 0.1 m, [강체↔격자 연동 참고 요약](body-grid-coupling.md))뿐입니다. 저장에 관한 Noita의 수치(파일 규격, 필드 크기)는 자료에 없습니다(WORLD_STREAMING:48).

| matter-wake 재질 | Noita 재질 | 필드 | Noita 값 | 변환식 | matter-wake 값 |
|---|---|---|---|---|---|
| (없음) | | | | | |

## 연관 메커니즘

| 메커니즘 | 이번에 반영 | 이유 |
|---|---|---|
| 재삽입 대기열(C50, C114, C115) | 예 | 블록의 칸에서 꺼낸 셀이 대기열에 남으면, 그 블록이 없는 동안 실패만 되풀이하며 retry 예산을 먹습니다. 블록과 함께 저장합니다 |
| 지형 충돌(C133) | 예 | 내린 블록의 청크 충돌을 버리고, 상주하지 않는 블록의 청크는 만들지 않습니다. 처음 만드는 청크는 근처 몸체를 깨우지 않습니다 |
| 조인트와 재귀 수집 | 아니오 | 조인트가 아직 없습니다 |
| 개체 keep-alive | 나중에 | 개체가 생길 때 |
| 작은 몸체 청소(C135) | 아니오 | 파괴 때 정한 대로 하지 않습니다 |

## 채택 / 변경 / 제외

- 그대로 씀:
  - 블록을 내릴 때 그 블록에 닿는 몸체를 모두 통째로 블록과 함께 저장하고, 블록 표에서 빼기 전에 모읍니다. 걸친 몸체는 먼저 내려가는 블록이 가져가고, 남는 블록의 칸도 비웁니다. 몸체는 한 블록에만 저장됩니다 (Noita 관측, C137)
  - 몸체를 격자에서 먼저 지운 뒤 셀을 저장해 몸체 픽셀을 셀로 이중 저장하지 않습니다 (Noita 관측, C137)
  - 픽셀 배열, 식별자(key), 위치, 각도를 저장합니다 (Noita 관측)
  - 비상주 칸에는 몸체 픽셀을 쓰지 않고 지우지도 않습니다 (Noita 관측, 이미 있음)
  - 몸체는 블록을 요청하거나 붙잡지 않습니다 (Noita 관측, C137 5절)
- 바꿔서 씀:
  - 닿음의 기준은 Box2D AABB 대신 지금 변환에서 이미지의 돌린 사각형이 차지하는 범위를 🧪 1칸 넓힌 것 (Noita 관측의 1픽셀 수집·2픽셀 미수집을 맞춤. 조각은 형상보다 조금 넓게 봄)
  - 위치는 화면 좌표 대신 Box2D 원점의 미터, 각도는 float32 각도 대신 cos·sin으로. 다시 만든 몸체가 비트까지 같게 (자체 설계)
  - 비상주 블록에 닿을 수 있는 몸체는 얼림. Noita는 계속 Step되다 픽셀을 쓰지 못하고 청소로 지워져 재질을 잃음 (자체 설계, 셀 보존과 결정성)
  - 왕복에서 속도와 수면을 지킴. Noita는 버림 (자체 설계, 개발자 결정 대기, `restoreMotion` 하나로 바꿀 수 있음)
  - 세션 저장은 Box2D 스냅샷 대신 재구성 지점 (자체 설계, 위 Box2D 절)
  - 대기열의 셀을 블록과 함께 저장 (자체 설계, Noita의 처리는 OPEN)
- 쓰지 않음:
  - 조인트 재귀 수집 (조인트 없음)
  - 작은 몸체 청소 (C135 때 정한 대로)
  - Noita의 셀 인코더처럼 재질과 색만 저장하는 것 (이미 셀 필드를 모두 저장하기로 정함, design.md 7절)

## 자료에 없음 / OPEN → 우리 설계

TPT(The Powder Toy)에는 강체도 월드 스트리밍도 없습니다(스킬의 tpt.md "Sleep, chunk scheduling, rigid bodies, destruction, streaming: none"). 아래는 모두 Noita 관측(C137)과 우리 측정으로 정했습니다.

| 빈칸 | 자료 상태 | TPT 방식 (파일:함수) | 우리 설계 (우리 제약에 맞춘 방법) | 측정 방법 | 추가 분석 제안 |
|---|---|---|---|---|---|
| 몸체가 어느 블록 것인가(소유) | C137이 답함: 블록에 닿는 몸체를 모두(AABB, 1픽셀 수집·2픽셀 미수집, BODY_BLOCK_RESIDENCY 3.2). 움직이는 몸체의 AABB 여유는 OPEN | 없음 | 🔶 Noita처럼 소유가 없습니다. 내려가는 블록이 지금 닿는 몸체를 모두 가져갑니다(`rigid.leavesWith`, 🧪 `unloadMargin` 1칸). 처음에는 질량 중심이 든 블록으로 정했다가 C137 뒤 바꿨습니다 | `TestBodiesLeaveWithEveryBlockTheyTouch`, `TestLoadedBodiesTakeTheirKeysPlaces` | 아니오 (답을 받음) |
| 남는 블록에 걸친 몸체 | C137이 답함: 통째로 먼저 내려가는 블록과, 남는 블록의 칸은 즉시 공기(4절) | 없음 | 🔶 Noita와 같음. 그 위에 놓였던 몸체는 Box2D가 깨웁니다(동적 몸체를 지우면 닿아 있던 몸체를 깨움, 임시 측정으로 확인) | `TestBodiesLeaveWithEveryBlockTheyTouch`, `TestBodiesOnALeavingBodyFall` | 아니오 |
| 메모리에 없는 블록으로 움직이는 몸체(가장자리) | C137이 답함: 계속 Step, 픽셀을 못 써 놓인 칸 0, 청소로 영영 사라짐(5절). 아래로 떨어지는 경우는 OPEN | 없음 | 🔶 일부러 다르게: 근처 범위(다음 Step에 닿을 칸)가 메모리에 없는 블록에 닿는 몸체와, 그런 몸체의 근처 범위에 닿는 몸체는 얼립니다(Box2D disable). 범위는 속도와 회전으로 이미지의 어느 점이든 한 틱에 갈 수 있는 거리만큼 넓힙니다(`rigid.reach`, Codex 리뷰). 속도는 우리 쪽에 두었다가 블록이 돌아오면 풀며 돌려줍니다. 셀 갱신이 비상주 블록에 닿는 청크를 돌리지 않는 것과 같은 규칙이고, 얼린 몸체도 상주 블록에 닿아 있어 그 블록이 내려가면 함께 저장됩니다. Noita처럼 하면 재질이 사라져 셀 보존이 깨지고, 청소는 전역 난수라 결정적이지 않습니다(`rigid.reachesOut` 하나) | `TestBodyFreezesAtTheEdgeOfMemory`, 골든 `terrain_bodies.rpl`의 여섯째 상자 | 아니오 |
| 그 블록에서 꺼낸 재삽입 대기열의 셀 | OPEN(C137 6.4: 그 장면에 대기열 셀이 없었음) | 없음 | 🔶 꺼낸 칸이 그 블록이면 블록과 함께 저장하고, 올릴 때 새 셀(fresh)로 다시 넣습니다(`world.TakeQueued` 하나) | `TestQueuedCellsLeaveWithTheirBlock` | 아니오 (셀 보존만 지키면 되는 우리 규칙) |
| 다시 올린 몸체의 속도와 잠 | C137이 답함: Noita는 속도·각속도를 0으로, 잠든 몸체를 깨워 되돌림(6.1-6.2) | 없음 | 🔶 일부러 다르게(개발자 결정 대기): 속도를 그대로, 잠든 몸체는 잠든 채 만듭니다. 근처 범위가 그 블록 밖에 닿으면 그 칸은 그동안 바뀌었을 수 있어 깨워서 만듭니다. 떨어지던 몸체가 블록이 오가며 공중에서 멈추지 않고, 잠든 더미가 블록이 올 때마다 한꺼번에 깨지 않게 하려는 것입니다. Noita처럼 하려면 `rigid.restoreMotion` 하나를 바꿉니다 | `TestSleepingBodyComesBackAsleep`, `TestLoadedBodyIsTheSame` | 아니오 |
| 저장한 세션이 계속 간 세션과 같게 | Noita는 왕복에서 속도를 버려 같지 않음(C137 9절) | 없음 | 저장 = 재구성 지점(위 Box2D 절) | `TestSavedSessionGoesOnTheSame`(몸체 포함) | 아니오 |
| key | Noita는 매 프레임 우선도로 key를 다시 매김(BODY_GRID_COUPLING:103) | 없음 | 만든 순서. 세션 파일에 다음 key를 저장해 다시 연 세션도 같은 key를 줍니다 | `TestSavedSessionGoesOnTheSame` | 아니오 |

추가 분석 C137은 이 작업과 함께 개발자가 승인해(2026-11-09) 분석 폴더의 별도 세션이 했고, 결과(BODY_BLOCK_RESIDENCY)를 위 표에 반영했습니다. 소유와 걸침은 Noita를 따르게 바꿨고, 가장자리와 왕복의 속도·수면은 일부러 다르게 두었습니다. 남은 OPEN(대기열 셀, 아래로 떨어지는 몸체, 움직이는 몸체의 AABB 여유, 조인트)은 지금 추가 분석을 제안하지 않습니다. 대기열과 가장자리는 셀 보존을 지키는 우리 규칙이 이미 측정을 통과하고, 조인트는 아직 없습니다.

## 검증 계획

- 테스트(모두 구현보다 먼저 써서 실패를 확인함):
  - `internal/rigid`: `TestLoadedBodyIsTheSame`(기록 → 다시 만들기 → 같은 기록, 질량·관성·질량 중심·덮는 픽셀 표가 비트까지 같음, 다시 쓴 칸이 같음), `TestBodiesLeaveWithEveryBlockTheyTouch`(1칸 떨어진 몸체는 가져가고 2칸은 남김, 양쪽 방향, 내려간 블록에 몸체 칸이 하나도 없음), `TestBodiesOnALeavingBodyFall`, `TestSleepingBodyComesBackAsleep`, `TestBodyFreezesAtTheEdgeOfMemory`, `TestLoadedBodiesTakeTheirKeysPlaces`, `TestLoadRefusesBadRecords`, `TestReset`, `TestFrozenTickDoesNotAllocate`, 그리고 Codex 리뷰가 찾은 것: `TestLoadTakesAPieceWhoseOriginIsOutside`, `TestFastLoadedBodyFreezesBeforeItMoves`, `TestDugFrozenBodyStaysFrozen`, `TestSpinningBodyFreezesBeforeItTurnsIn`, `TestThawedBodyStopsOnTheFloorBelow`, `TestRestoredSleeperFallsWhenItsSupportGoes`, `TestSleepingStackLeavesAsleep`, `TestThawedSpinningBodyHasItsTerrainFirst`, `TestUnloadBlocksKeepsSleepAcrossBlocks`
  - `internal/world`: `TestQueuedCellsLeaveWithTheirBlock`
  - `internal/physics`: `TestSetBoxMakesTheSameBox`, `TestDisableAndSleep`, `TestRebuildingChainsDoesNotAllocate`
  - `internal/save`: `TestBlockKeepsQueuedCellsAndBodies`(모든 필드 비트, 재질은 이름으로), `TestBlockReadsVersion2`, `TestBlockRefusesBadBodies`, 세션 형식 4
  - `internal/game`: `TestLeavingAndComingBackKeepsTheBlocks`(몸체 다섯 개가 떠났다 같은 기록으로 돌아오고 같은 칸에 씀), `TestBodyToolWorksInTheTerrain`, `TestTerrainStepWithBodiesDoesNotAllocate`
  - `internal/app`: `TestSavedSessionGoesOnTheSame`(몸체를 땅 바로 위에 떨어뜨려 저장 때 서로 기대게 하고, 매 틱 체크섬과 몸체 기록 비교). 저장할 때 Box2D 월드를 새로 만들지 않게 바꾸면 실패하는 것을 확인함
  - `internal/replay`: 골든 `terrain_bodies.rpl`. 다른 골든은 그대로
- 측정 결과(design.md 8절, references.md 3절): 몸체 450개 내리기 0.34 ms, 다시 만들기 0.45 ms, 그 뒤 쓰기 0.20 ms. 블록 저장·읽기 5.9·6.2 ms(셀만 4.3·4.4 ms). `BenchmarkBodies` 몸체 몫 551 → 559 µs(+1.5%, 방은 얼리기를 건너뜀). `terrain_bodies.rpl` 틱 중앙값 0.8 → 0.9 ms, 16.7 ms를 넘은 틱 없음
- 저장이 몸체를 바꾸는지(임시 측정): 몸체가 공중에 있을 때 저장하면 저장하지 않은 세션과 끝까지 같고, 바닥과 서로에 닿아 깨어 있을 때 저장하면 다음 틱부터 다릅니다. 접촉의 누적 충격량을 새로 시작하기 때문입니다
- 비교 화면: `go run ./cmd/matter-wake -replay internal/replay/testdata/terrain_bodies.rpl -shots ... -shots-dir out/shots`
- 직접 플레이로 볼 점: 지형에서 `B`로 떨어뜨린 상자가 멀리 갔다 돌아왔을 때 그대로인지, 종료 뒤 다시 열었을 때 그대로인지, 화면 밖으로 떨어진 상자가 돌아왔을 때 어색하지 않은지
