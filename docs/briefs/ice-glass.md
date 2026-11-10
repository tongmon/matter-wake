# 얼음·유리 깨짐 참고 요약

작성: 2026-11-10 · 관련 작업: `feat/ice-glass`(PR A: 재질, 고정 몸체, 지형→몸체 변환, 고정 몸체의 분리, 유리 가루), 이어서 `feat/body-cracks`(PR B: 충돌 균열, 잠듦 변환)

## 범위

- 이번 변경이 다루는 메커니즘:
  - 얼음·유리 지형 셀이 파괴되면, 4방향으로 이어진 같은 재질 덩어리가 고정(static) 몸체 하나가 되는 것
  - 고정 몸체: 손상으로는 고정이 풀리지 않고, 갈라지면 한 조각만 고정으로 남는 규칙(얼음은 가장 위, 나무는 가장 아래, 그 밖은 가장 큰 조각)
  - 유리 몸체가 셀을 하나라도 잃으면 몸체 전체가 유리 가루 셀이 되는 것
  - 고정 몸체의 저장과 블록 상주
  - (PR B) 접근 속도에 따른 충돌 균열과, 카메라에서 멀어진 얼음·유리 몸체를 지형 셀로 되돌리는 잠듦 변환
- 함께 읽은 연관 메커니즘(mechanics.md의 "Destruction", "Body↔grid coupling", "Streaming" 행과 Related):
  - C136 `doc STATIC_SOLID_BREAKING` 전체, C138 `doc BODY_CRACK_SLEEP` 전체, `section C136`
  - DESTRUCTION_BRIDGE:28-40(고정 종류, 부서져 될 재질, 잠듦 변환이 서로 다른 경로라는 것), REPORT_MATERIALS:240-256(고정 종류의 enum과 유리 값)
  - BODY_BUOYANCY_FRAGMENTS:121-124, 237(고정 종류 > 0 재질을 칠하면 static이 된다는 정적 판독과 그 OPEN)
  - 이미 반영한 [강체 파괴 참고 요약](destruction.md)(C77·C78·C79의 조각 나누기), [몸체 저장 참고 요약](body-saves.md)(C137), [강체↔격자 연동 참고 요약](body-grid-coupling.md)
  - 모델은 이 행에 없습니다. C136·C138은 원본 실행 관측(LIVE)과 정적 판독(STATIC)이고, 실행 모델(tools/*.py)은 원시 기록을 다시 계산하는 스크립트뿐입니다(STATIC_SOLID_BREAKING:125-129, BODY_CRACK_SLEEP:105-109).

## 관측 규칙

| 규칙 (자기 말로) | 근거 (절:줄) | 확인 수준 |
|---|---|---|
| 몸체로 바뀔 재질(convert_to_box2d_material)이 있는 고정 지형 셀이 파괴되면, 다음 프레임에 그 셀의 아래·왼쪽·오른쪽·위 이웃마다 같은 재질인지 보고, 같으면 그 이웃에서 4방향으로 같은 재질만 채운다 | STATIC_SOLID_BREAKING:8-11, 49-58 | STATIC + LIVE |
| 채우기는 시드 중심 384×384 창 안으로 제한된다. 창보다 큰 덩어리가 실제로 잘리는지는 보지 않았다 | STATIC_SOLID_BREAKING:11, 55, 68 | STATIC (LIVE 아님) |
| 채운 덩어리 전체가 목적 재질의 몸체 하나가 된다. 손상 근처만이 아니다(64×24 얼음 1,528셀 → 1,527셀 몸체 하나). 채운 셀의 색을 이미지로 옮기고 셀을 격자에서 지운다 | STATIC_SOLID_BREAKING:10-11, 56-57, 60-67 | STATIC + LIVE |
| 생긴 몸체는 목적 재질의 고정 종류가 0보다 크면 static이고 제자리에 있다(관측한 다섯 변환 모두 static) | STATIC_SOLID_BREAKING:12, 58, 65 | STATIC + LIVE |
| 떠 있는 덩어리도 손상되기 전에는 지형으로 남는다(100프레임). 지지가 없다는 것만으로는 바뀌지 않는다 | STATIC_SOLID_BREAKING:13, 63-64 | LIVE |
| 파기(폭발 아닌 셀 제거)가 같은 변환을 일으키는지는 시험하지 않았다 | STATIC_SOLID_BREAKING:69, 141 | OPEN |
| 고정 종류 > 0 재질로 칠한 몸체도 static으로 시작한다. 개체(PhysicsBody2)로 만든 같은 재질은 dynamic이다 | STATIC_SOLID_BREAKING:14, 72-75 | STATIC + LIVE |
| 손상만으로는 static이 풀리지 않는다(얼음 1,527 → 1,356셀, 나무 384 → 339셀 모두 static 유지) | STATIC_SOLID_BREAKING:76-77 | LIVE |
| static 몸체가 재구성에서 둘 이상의 연결 성분으로 갈라지면 하나만 원래 static 몸체로 남고 나머지는 새 dynamic 몸체가 된다. 종류 3(나무)은 가장 낮게 내려온 성분, 5(얼음)는 가장 높이 올라간 성분, 그 밖(2)은 첫 성분(가장 큰 것)이 남는다. 두 실행이 같았다 | STATIC_SOLID_BREAKING:15-19, 78-87 | STATIC + LIVE |
| 종류 1(늘 static)과 4(다른 재질로 부서지는 dynamic)는 몸체 장면이 없다 | STATIC_SOLID_BREAKING:88-90 | OPEN |
| 다른 재질로 부서지는 것은 부서짐 폭발(solid_on_break_explode) 재질뿐이다. 셀을 하나라도 잃은 몸체는 다음 재구성에서 몸체 전체가 부서져 될 재질(glass_broken) 셀이 되고 몸체가 사라진다. 피해 폭발은 없다 | STATIC_SOLID_BREAKING:20-24, 93-98 | LIVE + STATIC |
| 그때 만드는 셀 수는 남은 픽셀이 아니라 원래 픽셀 수다(10셀을 잃은 128픽셀 → 128셀). 셀 수가 늘어난다 | STATIC_SOLID_BREAKING:21, 94-95, 137 | LIVE |
| 바닥 충돌만으로는 유리가 부서지지 않았다(300·600·918 px/s). 계기는 셀 손실이다 | STATIC_SOLID_BREAKING:23, 99-104 | LIVE |
| ice_glass, glass_brittle, steel은 셀을 잃어도 부서져 될 재질로 바뀌지 않는다 | STATIC_SOLID_BREAKING:22, 105-108 | LIVE |
| 잠듦 변환 재질의 몸체는 약 1,000프레임 잠들어도 바뀌지 않았다(카메라를 움직이지 않음) | STATIC_SOLID_BREAKING:109-112 | LIVE |
| 변환과 분리 자체에는 난수가 보이지 않았다 | STATIC_SOLID_BREAKING:138 | STATIC |
| (PR B) 균열은 접촉 하나마다, 균열 가능(crackability > 0)한 쪽만 판정한다. 접촉 직전 법선 방향 상대 속도 \|v\|가 50 px/s를 넘으면 전역 난수로 U∈[0,1]을 뽑아 10U ≤ \|v\|(물리 단위)이면 균열을 만든다. 그래서 50 px/s 이하 없음, 50~100 확률, 100 이상 늘 생김 | BODY_CRACK_SLEEP:8-13, 44-58 | STATIC + LIVE |
| (PR B) 균열은 접촉점에서 흔들리며 나아가는 이동체이고 지나간 줄의 균열 가능 셀을 지운다. 남는 입자는 셀로 돌아오지 않아 셀 수가 줄어든다. 셀이 지워지는 정확한 조건은 OPEN | BODY_CRACK_SLEEP:14, 48-51, 70-74, 123 | STATIC + LIVE, 일부 OPEN |
| (PR B) 910 px/s의 한 Step 충돌에서는 판정 자체가 없었다(616 px/s는 있음) | BODY_CRACK_SLEEP:16, 66-67, 124 | LIVE, 원인 OPEN |
| (PR B) 잠듦 변환은 카메라 중심이 마지막 점검 위치에서 512 px 넘게 움직였을 때만 돈다. 잠듦 변환 재질의 dynamic 몸체 중 카메라 중심에서 350 px 이상이면서 잠들었거나 750 px 이상인 몸체를 부서져 될 재질 셀로 바꾼다(셀 수 그대로) | BODY_CRACK_SLEEP:17-21, 76-96 | STATIC + LIVE |

## 현재 코드 대조

이번 변경 전에는 몸체가 모두 dynamic이었고(`internal/physics`에 형상 없는 static 몸체가 없음), 지형 셀이 몸체가 되는 경로도, 몸체가 다른 재질 셀이 되는 경로도 없었습니다. 아래는 이번 구현(PR A)과의 대조입니다.

| 관측 규칙 | 우리 코드 (파일:줄) | 같음 / 다름 / 없음 | 다름이면 |
|---|---|---|---|
| 몸체로 바뀔 재질의 지형 셀이 파괴되면 네 이웃에서 같은 재질을 채움 | `internal/game/game.go` `dig` → `internal/rigid/convert.go` `Destroyed`, `Convert`, `convertAt` | 같음 + 계기가 다름 | 계기는 채굴 도구(개발자 결정 2026-11-10, "채굴도 계기로"). Noita는 파기를 시험하지 않았고(OPEN) 채굴 도구가 없습니다. 이웃 순서는 Noita와 같게 아래·왼쪽·오른쪽·위입니다 |
| 다음 프레임에 처리 | `game.Step`: 파기 → 나누기(`Break`) → 변환(`Convert`) | 다름(자체 설계) | 같은 틱, 셀 갱신 전에 바꿉니다. 파괴를 판 틱에 나누는 것과 같은 까닭입니다(design.md 8절 "한 틱의 순서"). 바뀐 몸체는 그 자리에서 바로 쓰여, 그 틱에 몸체 놓기 도구가 빈칸으로 볼 틈이 없습니다 |
| 384×384 창 | `rigid.convertWindow` 256 🔶🧪 | 다름 | 몸체 이미지는 `rigid.MaxPixels`(65,536 = 256²)까지라 384²(147,456)를 담지 못합니다. 창을 한도에 맞추고 창 밖은 지형으로 남깁니다. 아래 "자료에 없음" |
| 덩어리 전체가 목적 재질의 몸체 하나 | `convert.go` `fill`, `convertAt` | 같음 | 셀의 색조를 픽셀로 옮깁니다. 손상은 0에서 다시 시작합니다(Noita도 셀을 새로 만들 때 재질 hp로 채움, C54:1600. 우리 규칙 "다른 재질이 되면 손상 0", design.md 5절) |
| 목적 재질의 고정 종류 > 0이면 static | 재질 필드 `anchor`가 있으면 static(`material.Def.Anchor`) | 같음 | 고정 종류를 "갈라질 때 남는 조각"(`top`, `bottom`, `largest`)으로 둡니다. 종류 1·4는 OPEN이라 두지 않습니다 |
| 떠 있기만 해서는 안 바뀜 | 계기는 셀 파괴뿐 | 같음 | |
| 칠한 몸체도 static | 몸체 놓기 도구는 늘 dynamic | 다름(개발 도구) | Noita의 개체 몸체처럼 dynamic입니다. static 몸체는 지형 변환만 만듭니다. 대시보드에 static 놓기를 더할지는 필요해지면 묻습니다 |
| 손상으로 static이 풀리지 않음 | `destroy.go` `split`: static 몸체는 남는 조각이 static 그대로 | 같음 | |
| 갈라지면 한 조각만 static, 나머지는 dynamic | `destroy.go` `keeps`, `split` | 같음 | 위·아래는 이미지의 가장 위·아래 픽셀 행으로 봅니다. static 몸체는 돌지 않으므로 이미지의 행이 월드의 행입니다. 같으면 먼저 찾은 조각입니다(자체 설계, Noita는 OPEN). 2픽셀 미만 조각은 후보가 아닙니다(1픽셀 몸체 없음, design.md 8절) |
| 부서짐 폭발 재질은 셀 하나를 잃으면 통째로 부서져 될 재질 셀 | `destroy.go` `shatter`, 재질 필드 `shatter` | 같음 + 개수가 다름 | 개수는 남은 픽셀 수입니다(개발자 결정 2026-11-10, "남은 픽셀 수만큼"). Noita는 원래 픽셀 수라 셀이 늘어납니다 |
| 유리만 부서지고 ice_glass·glass_brittle은 셀만 잃음 | `shatter` 필드는 유리 몸체에만 | 같음 | 단, 유리 지형이 바뀌는 몸체를 우리는 부서지는 유리로 둡니다(아래 "바꿔서 씀") |
| 변환·분리에 난수 없음 | `fill`, `anchorPiece`, `shatter` 모두 난수 없음 | 같음 | |
| 고정 몸체의 저장 | `rigid/save.go` `record`·`remake`, `save` 블록 형식 4의 `static` 표시 | 자체 설계 | Noita의 static 몸체 저장은 분석에 없습니다. 우리 몸체 저장 규칙(C137 기반)을 그대로 따릅니다 |
| (PR B) 충돌 균열 | 없음 | 없음 | PR B |
| (PR B) 잠듦 변환 | 없음 | 없음 | PR B |

## 실제 수치

Noita 값은 `go run ./tools/noitaref -fields solid_,density,hp,durability,crackability,convert_to,liquid_gravity,liquid_viscosity material ice_static ice_glass glass_static glass_brittle glass glass_broken sand wood`(`analysis-detail/runtime-completion/materials-expanded-analysis.json`)입니다.

| matter-wake 재질 | Noita 재질 | 필드 | Noita 값 | 변환식 | matter-wake 값 |
|---|---|---|---|---|---|
| ice | ice_static | cell_type, liquid_static | 1, true | 고정 액체 계열 = 고정 재질 | kind static |
| ice | ice_static | durability, hp | 0, 30000 | 그대로(채굴 규칙, design.md 5절) | hardness 0, hp 30000(도구 2000/틱으로 15틱) |
| ice | ice_static | density | 10 | 고정 재질은 상대 순서만 씀. Noita rock_static도 10 | density 255(stone과 같음) |
| ice | ice_static | convert_to_box2d_material | ice_glass | 이름 연결 | body `ice_body` |
| ice_body | ice_glass | density | 6 | 몸체 질량 밀도 그대로(나무와 같은 방식) | density 6 |
| ice_body | ice_glass | durability, hp | 0, 10000 | 그대로 | hardness 0, hp 10000(5틱) |
| ice_body | ice_glass | solid_friction, solid_restitution | 0.05, 0.2 | 그대로 | friction 0.05, restitution 0.2 |
| ice_body | ice_glass | solid_static_type | 5 (ICE_BREAK) | 가장 위 조각이 남음 | anchor `top` |
| ice_body | ice_glass | crackability, solid_on_sleep_convert, solid_break_to_type | 100, true, ice_static | PR B | PR B에서 |
| glass | glass_static | durability, hp, density | 0, 30000, 10 | ice와 같음 | hardness 0, hp 30000, density 255 |
| glass | glass_static | convert_to_box2d_material | glass_brittle | 이름 연결. 목적 몸체를 부서지는 유리로 바꿈(아래) | body `glass_body` |
| glass_body | glass | density, durability, hp | 6, 0, 200 | 그대로 | density 6, hardness 0, hp 200(1틱) |
| glass_body | glass | solid_friction, solid_restitution | 0.3, 0.2 | 그대로 | friction 0.3, restitution 0.2 |
| glass_body | glass | solid_static_type | 2 (STATIC_BUT_BREAK) | 가장 큰 조각이 남음 | anchor `largest` |
| glass_body | glass | solid_on_break_explode, solid_break_to_type | true, glass_broken | 셀을 잃으면 통째로 부서짐 | shatter `glass_powder` |
| glass_powder | glass_broken | liquid_sand | true | 가루 | kind powder |
| glass_powder | glass_broken | density | 6(모래도 6) | 상대 순서만 씀 | density 30(모래와 같음) |
| glass_powder | glass_broken | liquid_gravity | 1.1 | × 0.25(C47) | fallAccel 0.275 |
| glass_powder | glass_broken | liquid_viscosity | 50 | 미끄러짐 % 그대로(모래와 같음) | slideChance 50 |
| glass_powder | glass_broken | durability, hp | 0, 1000 | 그대로 | hardness 0, hp 1000 |
| glass_powder | — | 가라앉기 | 모래와 같은 밀도 교환 | 모래와 같음 | sinkChance 25 |
| ice_body, glass_body | — | buoyancy | 재질 몸체는 C41 없음(C135) | 0 | buoyancy 0 |
| wood | wood | solid_static_type | 3 (TREE_BREAK) | 가장 아래 조각이 남음 | anchor `bottom`(나무 static 몸체는 아직 만들 길이 없음) |

## 연관 메커니즘

| 메커니즘 | 이번에 반영 | 이유 |
|---|---|---|
| 조각 나누기(C77~C79, [파괴](destruction.md)) | 예 | static 몸체가 갈라질 때 같은 4방향 조각 찾기를 쓰고, 남는 조각만 고르는 규칙을 바꿉니다. dynamic이 되는 조각은 지금의 새 조각과 같습니다(겹침 정리 포함) |
| 몸체 저장(C137, [몸체 저장](body-saves.md)) | 예 | static 몸체도 닿는 블록과 함께 나가고 다시 만들어집니다. 블록 형식 4에 static 표시를 더합니다 |
| 지형 충돌(C133) | 예 | static 몸체의 칸은 몸체 셀이라 지형 분류에서 열림입니다. 다른 몸체와는 Box2D 형상끼리 부딪힙니다. static 몸체 근처에는 지형 충돌을 만들지 않습니다(static끼리는 부딪히지 않음) |
| 재삽입 대기열(C50, C114, C115) | 예 | 유리 가루가 놓일 칸이 이미 찼으면 그 셀을 대기열에 넣습니다 |
| 충돌 균열, 잠듦 변환(C138) | 나중에(PR B) | 개발자가 PR을 둘로 나눔 |
| 전기로 얼음 깨기(C126) | 아니오 | 전기는 범위 결정 전(로드맵) |

## 채택 / 변경 / 제외

- 그대로 씀:
  - 몸체로 바뀔 재질의 지형 셀이 파괴되면, 네 이웃(아래·왼쪽·오른쪽·위 순서)에서 같은 재질을 4방향으로 채워 덩어리 전체를 그 재질이 가리키는 고체의 몸체 하나로 만듦(Noita 관측)
  - 떠 있기만 해서는 바뀌지 않음(Noita 관측)
  - 고체에 고정 종류가 있으면 바뀐 몸체는 static이고, 손상으로 풀리지 않으며, 갈라지면 한 조각만 static으로 남음. 얼음은 가장 위, 나무는 가장 아래, 그 밖은 가장 큰 조각(Noita 관측)
  - 부서짐 재질의 몸체는 셀을 하나 잃으면 다음 나누기에서 통째로 그 가루 재질 셀이 되고 몸체가 사라짐. 피해 폭발 없음(Noita 관측)
  - 재질 값(Noita 관측 값, 위 표)
- 바꿔서 씀:
  - 계기에 채굴 도구를 더함(개발자 결정 2026-11-10). Noita에는 채굴 도구가 없음(자체 설계)
  - 같은 틱에 바꿈(Noita는 다음 프레임). 파괴와 같은 틱 순서(자체 설계)
  - 창은 384×384 대신 256×256(몸체 픽셀 한도, 자체 설계 🔶🧪)
  - 1픽셀 덩어리는 몸체로 만들지 않고 지형으로 둠. 우리에게 1픽셀 몸체가 없다는 규칙(design.md 8절)과 맞춤(자체 설계)
  - 유리 가루는 원래 픽셀 수 대신 남은 픽셀 수만큼(개발자 결정 2026-11-10). 가루를 놓을 칸이 차 있으면 재삽입 대기열로(자체 설계)
  - 셀 보존 검사: 변환과 부서짐은 재질 하나의 셀을 다른 재질로 바꾸므로, 바꾼 수를 재질별로 세어(`Game.Turned`, 무한 모드의 `Game.Made`와 같은 방식) 검사에서 뺌(자체 설계)
  - 유리 지형(glass_static)이 바뀌는 몸체: Noita는 부서지지 않는 glass_brittle이고, 부서지는 glass는 개체로만 나옵니다. 우리는 유리 지형이 부서지는 유리 몸체가 되게 했습니다(작업 지시의 "유리가 셀 하나를 잃으면 통째로 가루", 🔶 질문). 데이터의 `body` 한 줄로 바꿀 수 있습니다
  - 고정 종류는 숫자 enum 대신 남는 조각의 이름(`top`, `bottom`, `largest`)
- 쓰지 않음:
  - 고정 종류 1(늘 static)과 4(dynamic인데 다른 재질로 부서짐): 몸체 장면이 OPEN이고 쓸 재질이 없음
  - 칠한(놓은) 몸체가 static으로 시작하는 것: 몸체 놓기 도구는 개발용이고 Noita의 개체처럼 dynamic(🔶)

## 자료에 없음 / OPEN → 우리 설계

TPT에는 강체, 파괴, 몸체가 없습니다(tpt.md "Sleep, chunk scheduling, rigid bodies, destruction, streaming": none). 아래 빈칸은 모두 우리 측정으로만 설계합니다.

| 빈칸 | 자료 상태 | TPT 방식 (파일:함수) | 우리 설계 (우리 제약에 맞춘 방법) | 측정 방법 | 추가 분석 제안 |
|---|---|---|---|---|---|
| 파기가 변환을 일으키는지 | OPEN(:69, 141) | 없음 | 일으킴(개발자 결정 2026-11-10). Noita에는 채굴 도구가 없음 | `TestDiggingIceMakesAStaticBody` | 아니오 (개발자 결정으로 정함) |
| 384×384보다 큰 덩어리 | OPEN(:68) | 없음 | 🔶🧪 시드(파괴된 셀의 이웃) 중심 256×256 창 안만 몸체가 되고, 창 밖은 지형으로 남아 다음 파괴 때 바뀜. 창은 픽셀 한도(65,536)와 같음. 측정(개발 PC, 가운데를 판 정사각형): 한 변 64·128·192·256이 0.19·0.67·1.5·2.7 ms, 16칸에 하나 구멍이면 0.45·1.7·3.9·7.2 ms(기준 CPU ×2.5). 시간의 약 60%가 형상 | `BenchmarkConvert`, `TestConvertStopsAtTheWindow` | 아니오 (게임 느낌에 중요하지 않고, 한도 때문에 Noita 값을 쓸 수 없음) |
| 갈라질 때 위·아래가 같은 조각이 둘 이상 | 없음 | 없음 | 먼저 찾은 조각(이미지 행 순서). 위 규칙에서는 가장 위 픽셀이 든 조각이 늘 먼저라 동률이 없음 | `TestStaticSplitKeeps*` | 아니오 |
| 2픽셀 미만 덩어리 | 없음 | 없음 | 지형으로 둠(1픽셀 몸체 없음) | 테스트 | 아니오 |
| static 몸체가 바뀔 때 그 위에서 잠든 몸체 | pitfalls.md는 Box2D가 정적 몸체를 지워도 잠든 몸체를 깨우지 않는다고 적음 | 없음 | 재 보니 v3.2는 형상을 지울 때 닿아 있던 접촉의 두 몸체를 깨움(`b2DestroyContact`). 그래서 따로 깨우지 않음. 밑을 판 notch, 부서짐, 잘림 세 경우를 테스트로 고정(pitfalls.md에 정정을 더함) | `TestBodiesOnStaticBodiesFallWhenItBreaks` | 아니오 |
| 가루가 놓일 칸이 차 있음 | Noita는 그 자리에 셀을 만듦(차 있으면 OPEN) | 없음 | 그 셀은 재삽입 대기열로(셀 보존). 월드 밖·메모리 밖이면 부순 사람의 인벤토리로 | 테스트 | 아니오 |
| static 몸체의 저장 | 없음 | 없음 | 블록 형식 4에 static 표시. 나머지는 몸체 저장 규칙 그대로 | `TestLoadedStaticBodyIsTheSame` | 아니오 |
| (PR B) 균열 난수와 접촉 순서 | Noita는 전역 난수, 순서 OPEN | 없음 | (seed, tick, 몸체 key)에서 뽑고 접촉은 몸체·모양 번호로 정렬 | PR B | PR B에서 |
| (PR B) 빠른 충돌의 균열 | 910 px/s에 사건 없음(원인 OPEN) | 없음 | 빠를수록 늘 균열(개발자 결정 2026-11-10). Box2D v3.2의 접촉 hit 사건(접근 속도)을 씀 | PR B | PR B에서 |

## 결과 (PR A, 2026-11-10)

- 테스트: `internal/rigid` convert_test.go(변환, 창, 작은 덩어리, 세 가지 anchor, 유리 부서짐, static 몸체 위 몸체, 저장 왕복과 얼지 않음, 할당 0회), `internal/game` `TestIceAndGlassTurnAsTheToolDigs`, `internal/save` `TestBlockReadsVersion3`, `internal/physics` `TestAddStaticBody`, 리플레이 골든 `ice_glass.rpl`(셀 보존과 1·4 worker가 같음). 다른 골든은 새 재질의 0 칸만 늘었음.
- 측정: 깨짐 없는 틱의 몸체 몫 546.7 → 544.7 µs(main과 번갈아 5회 중앙값), 변환 비용은 위 표.
- 화면: [ice-glass.png](../images/ice-glass.png)(틱 14, 52, 82, 142, 154, 599. 주황은 static 몸체, 초록은 dynamic 몸체, 분홍은 지형 chain), [ice-glass-panel.png](../images/ice-glass-panel.png)(대시보드로 얼음 몸체를 아래 10 m/s로 던진 직후).

## 검증 계획

- 테스트(`internal/rigid`): 얼음 지형을 파면 덩어리가 static 몸체 하나가 됨(셀 수, 재질, 칸이 그대로), 떠 있는 덩어리는 파기 전까지 지형, 창보다 큰 덩어리는 창만, 1픽셀은 지형, static 몸체는 움직이지 않고 얼지 않음, 갈라지면 얼음은 위·나무는 아래·그 밖은 큰 조각이 남고 나머지는 떨어짐, 유리는 셀 하나를 잃으면 남은 픽셀 수만큼 가루, static 몸체 위의 상자는 받침이 잘리면 떨어짐, static 몸체의 저장 왕복이 비트까지 같음, 깨짐 없는 틱의 할당 0회.
- 리플레이 골든 `ice_glass.rpl`(샌드박스): 얼음 덩어리를 파서 static 몸체로 바꾸고 가로로 잘라 아래가 떨어지게 하고, 유리 판을 파서 가루로 만들고, 던진 유리 몸체를 파서 가루로 만듦. 매 틱 셀 보존(바꾼 수를 뺌), 1·4 worker 같음.
- 측정: 깨짐 없는 틱의 몸체 비용 `go test -run '^$' -bench Bodies -count 5 ./internal/rigid/` 전후, 변환 비용 `BenchmarkConvert`(창 128·192·256).
- 비교 화면: `go run ./cmd/matter-wake -replay internal/replay/testdata/ice_glass.rpl -shots ... -dash panel,bodies,chains`.
- 직접 플레이로 볼 점: 얼음을 팔 때 덩어리 전체가 몸체가 되는 순간이 보이지 않는지(같은 색), 가로로 판 얼음의 아래가 떨어지는 모습, 유리 판 하나를 팔 때 판 전체가 가루로 무너지는 모습, 큰 얼음 덩어리에서 창 경계가 보이는지.
