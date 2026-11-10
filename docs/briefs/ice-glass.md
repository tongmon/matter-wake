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
| 384×384 창 | `rigid.convertWindow` 256 | 다름(개발자 결정 2026-11-10, 나중에 384) | 몸체 이미지는 `rigid.MaxPixels`(65,536 = 256²)까지라 384²(147,456)를 담지 못합니다. 창을 한도에 맞추고 창 밖은 지형으로 남깁니다. 아래 "자료에 없음" |
| 덩어리 전체가 목적 재질의 몸체 하나 | `convert.go` `fill`, `convertAt` | 같음 | 셀의 색조를 픽셀로 옮깁니다. 손상은 0에서 다시 시작합니다(Noita도 셀을 새로 만들 때 재질 hp로 채움, C54:1600. 우리 규칙 "다른 재질이 되면 손상 0", design.md 5절) |
| 목적 재질의 고정 종류 > 0이면 static | 재질 필드 `anchor`가 있으면 static(`material.Def.Anchor`) | 같음 | 고정 종류를 "갈라질 때 남는 조각"(`top`, `bottom`, `largest`)으로 둡니다. 종류 1·4는 OPEN이라 두지 않습니다 |
| 떠 있기만 해서는 안 바뀜 | 계기는 셀 파괴뿐 | 같음 | |
| 칠한 몸체도 static | 몸체 놓기 도구는 늘 dynamic | 다름(개발 도구) | Noita의 개체 몸체처럼 dynamic입니다. static 몸체는 지형 변환만 만듭니다. 대시보드에 static 놓기를 더할지는 필요해지면 묻습니다 |
| 손상으로 static이 풀리지 않음 | `destroy.go` `split`: static 몸체는 남는 조각이 static 그대로 | 같음 | |
| 갈라지면 한 조각만 static, 나머지는 dynamic | `destroy.go` `keeps`, `split` | 같음 | 위·아래는 이미지의 가장 위·아래 픽셀 행으로 봅니다. static 몸체는 돌지 않으므로 이미지의 행이 월드의 행입니다. 같으면 먼저 찾은 조각입니다(자체 설계, Noita는 OPEN). 2픽셀 미만 조각은 후보가 아닙니다(1픽셀 몸체 없음, design.md 8절) |
| 부서짐 폭발 재질은 셀 하나를 잃으면 통째로 부서져 될 재질 셀 | `destroy.go` `shatter`, 재질 필드 `shatter` | 같음 + 개수가 다름 | 개수는 남은 픽셀 수입니다(개발자 결정 2026-11-10, "남은 픽셀 수만큼"). Noita는 원래 픽셀 수라 셀이 늘어납니다 |
| 유리만 부서지고 ice_glass·glass_brittle은 셀만 잃음 | `shatter`는 glass_body에만. 유리 지형이 바뀌는 glass_slab에는 없음 | 같음 | 처음에는 유리 지형이 부서지는 유리가 되게 했다가, 개발자 결정(2026-11-10, "Noita처럼 나눠 두기 (추천)")으로 Noita처럼 나눔 |
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
| glass | glass_static | convert_to_box2d_material | glass_brittle | 이름 연결 | body `glass_slab` |
| glass_slab | glass_brittle | density, durability, hp | 6, 0, 10000 | 그대로 | density 6, hardness 0, hp 10000(5틱) |
| glass_slab | glass_brittle | solid_friction, solid_restitution | 0.9, 0 | 그대로 | friction 0.9, restitution 0 |
| glass_slab | glass_brittle | solid_static_type | 2 (STATIC_BUT_BREAK) | 가장 큰 조각이 남음 | anchor `largest` |
| glass_slab | glass_brittle | solid_on_break_explode | false | 부서지지 않음 | shatter 없음 |
| glass_slab | glass_brittle | crackability, solid_on_sleep_convert, solid_break_to_type | 100, true, glass_static | PR B: 균열 대상, 잠듦 변환은 glass로 | PR B에서 |
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
| ice_body, glass_slab, glass_body | — | buoyancy | 재질 몸체는 C41 없음(C135) | 0 | buoyancy 0 |
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
  - 창은 384×384 대신 256×256(몸체 픽셀 한도). 개발자 결정(2026-11-10, "지금 256, 나중에 384 (추천)"): 큰 얼음 지형을 넣을 때 변환을 여러 틱에 나누는 일과 함께, 몸체 셀이 쓰지 않는 `Cell.Rest` 바이트로 픽셀 번호를 24비트로 늘려 384로 바꿈
  - 1픽셀 덩어리는 몸체로 만들지 않고 지형으로 둠. 우리에게 1픽셀 몸체가 없다는 규칙(design.md 8절)과 맞춤(자체 설계)
  - 유리 가루는 원래 픽셀 수 대신 남은 픽셀 수만큼(개발자 결정 2026-11-10). 가루를 놓을 칸이 차 있으면 재삽입 대기열로(자체 설계)
  - 셀 보존 검사: 변환과 부서짐은 재질 하나의 셀을 다른 재질로 바꾸므로, 바꾼 수를 재질별로 세어(`Game.Turned`, 무한 모드의 `Game.Made`와 같은 방식) 검사에서 뺌(자체 설계)
  - 유리 지형(glass_static)이 바뀌는 몸체: 처음에는 부서지는 유리가 되게 했다가, 개발자 결정(2026-11-10, "Noita처럼 나눠 두기 (추천)")으로 Noita처럼 부서지지 않는 glass_slab(glass_brittle 값)이 되게 함. 통째로 부서지는 glass_body(Noita glass)는 몸체 놓기 도구와 나중의 소품용(Noita 관측 그대로)
  - 고정 종류는 숫자 enum 대신 남는 조각의 이름(`top`, `bottom`, `largest`)
- 쓰지 않음:
  - 고정 종류 1(늘 static)과 4(dynamic인데 다른 재질로 부서짐): 몸체 장면이 OPEN이고 쓸 재질이 없음
  - 칠한(놓은) 몸체가 static으로 시작하는 것: 몸체 놓기 도구는 개발용이고 Noita의 개체처럼 dynamic(🔶)

## 자료에 없음 / OPEN → 우리 설계

TPT에는 강체, 파괴, 몸체가 없습니다(tpt.md "Sleep, chunk scheduling, rigid bodies, destruction, streaming": none). 아래 빈칸은 모두 우리 측정으로만 설계합니다.

| 빈칸 | 자료 상태 | TPT 방식 (파일:함수) | 우리 설계 (우리 제약에 맞춘 방법) | 측정 방법 | 추가 분석 제안 |
|---|---|---|---|---|---|
| 파기가 변환을 일으키는지 | OPEN(:69, 141) | 없음 | 일으킴(개발자 결정 2026-11-10). Noita에는 채굴 도구가 없음 | `TestDiggingIceMakesAStaticBody` | 아니오 (개발자 결정으로 정함) |
| 384×384보다 큰 덩어리 | OPEN(:68) | 없음 | 시드(파괴된 셀의 이웃) 중심 256×256 창 안만 몸체가 되고(개발자 결정 2026-11-10, 나중에 384), 창 밖은 지형으로 남아 다음 파괴 때 바뀜. 창은 픽셀 한도(65,536)와 같음. 측정(개발 PC, 가운데를 판 정사각형): 한 변 64·128·192·256이 0.19·0.67·1.5·2.7 ms, 16칸에 하나 구멍이면 0.45·1.7·3.9·7.2 ms(기준 CPU ×2.5). 시간의 약 60%가 형상 | `BenchmarkConvert`, `TestConvertStopsAtTheWindow` | 아니오 (게임 느낌에 중요하지 않고, 한도 때문에 Noita 값을 쓸 수 없음) |
| 갈라질 때 위·아래가 같은 조각이 둘 이상 | 없음 | 없음 | 먼저 찾은 조각(이미지 행 순서). 위 규칙에서는 가장 위 픽셀이 든 조각이 늘 먼저라 동률이 없음 | `TestStaticSplitKeeps*` | 아니오 |
| 2픽셀 미만 덩어리 | 없음 | 없음 | 지형으로 둠(1픽셀 몸체 없음) | 테스트 | 아니오 |
| static 몸체가 바뀔 때 그 위에서 잠든 몸체 | pitfalls.md는 Box2D가 정적 몸체를 지워도 잠든 몸체를 깨우지 않는다고 적음 | 없음 | 재 보니 v3.2는 형상을 지울 때 닿아 있던 접촉의 두 몸체를 깨움(`b2DestroyContact`). 그래서 따로 깨우지 않음. 밑을 판 notch, 부서짐, 잘림 세 경우를 테스트로 고정(pitfalls.md에 정정을 더함) | `TestBodiesOnStaticBodiesFallWhenItBreaks` | 아니오 |
| 가루가 놓일 칸이 차 있음 | Noita는 그 자리에 셀을 만듦(차 있으면 OPEN) | 없음 | 그 셀은 재삽입 대기열로(셀 보존). 월드 밖·메모리 밖이면 부순 사람의 인벤토리로 | 테스트 | 아니오 |
| static 몸체의 저장 | 없음 | 없음 | 블록 형식 4에 static 표시. 나머지는 몸체 저장 규칙 그대로 | `TestStaticBodyLeavesAndComesBack`, `TestStaticBodyFillsABlockThatComesBackLater` | 아니오 |
| (PR B) 균열 난수와 접촉 순서 | Noita는 전역 난수, 순서 OPEN | 없음 | (seed, tick, 몸체 key)에서 뽑고 접촉은 몸체·모양 번호로 정렬 | PR B | PR B에서 |
| (PR B) 빠른 충돌의 균열 | 910 px/s에 사건 없음(원인 OPEN) | 없음 | 빠를수록 늘 균열(개발자 결정 2026-11-10). Box2D v3.2의 접촉 hit 사건(접근 속도)을 씀 | PR B | PR B에서 |
| (PR B) 균열과 잠듦 변환의 대상 | Noita: crackability 100인 ice_glass, glass_brittle, glass. 잠듦 변환은 ice_glass → ice_static, glass_brittle → glass_static | 없음 | glass_slab도 균열 대상이고 잠듦 변환으로 glass가 됨(개발자 결정 2026-11-10, glass_slab을 나눌 때). 나머지는 Noita 값 그대로: 균열 대상 ice_body, glass_body, 잠듦 변환 ice_body → ice, glass_body는 잠듦 변환 없음. 모두 데이터 필드로 둠 | PR B | PR B에서 |

## 결과 (PR A, 2026-11-10)

- 테스트: `internal/rigid` convert_test.go(변환, 창, 작은 덩어리, 세 가지 anchor, 유리 지형은 부서지지 않음, 유리 부서짐, static 몸체 위 몸체, 저장 왕복과 얼지 않음, 블록이 하나씩 돌아올 때의 static 몸체, 할당 0회), `internal/game` `TestIceAndGlassTurnAsTheToolDigs`, `internal/save` `TestBlockReadsVersion3`, `internal/physics` `TestAddStaticBody`, 리플레이 골든 `ice_glass.rpl`(셀 보존과 1·4 worker가 같음). 다른 골든은 새 재질의 0 칸만 늘었음.
- 측정: 깨짐 없는 틱의 몸체 몫 546.7 → 544.7 µs(main과 번갈아 5회 중앙값), 변환 비용은 위 표.
- 화면: [ice-glass.png](../images/ice-glass.png)(틱 14, 52, 82, 142, 176, 455. 주황은 static 몸체, 초록은 dynamic 몸체, 분홍은 지형 chain), [ice-glass-panel.png](../images/ice-glass-panel.png)(대시보드로 얼음 몸체를 아래 10 m/s로 던진 직후).

## 검증 계획

- 테스트(`internal/rigid`): 얼음 지형을 파면 덩어리가 static 몸체 하나가 됨(셀 수, 재질, 칸이 그대로), 떠 있는 덩어리는 파기 전까지 지형, 창보다 큰 덩어리는 창만, 1픽셀은 지형, static 몸체는 움직이지 않고 얼지 않음, 갈라지면 얼음은 위·나무는 아래·그 밖은 큰 조각이 남고 나머지는 떨어짐, 유리는 셀 하나를 잃으면 남은 픽셀 수만큼 가루, static 몸체 위의 상자는 받침이 잘리면 떨어짐, static 몸체의 저장 왕복이 비트까지 같음, 깨짐 없는 틱의 할당 0회.
- 리플레이 골든 `ice_glass.rpl`(샌드박스): 얼음 덩어리를 파서 static 몸체로 바꾸고 가로로 잘라 아래가 떨어지게 하고, 유리 판을 파서 static 몸체로 바꾸고 세로로 잘라 작은 쪽이 떨어지게 하고, 던진 glass_body를 파서 가루로 만듦. 매 틱 셀 보존(바꾼 수를 뺌), 1·4 worker 같음.
- 측정: 깨짐 없는 틱의 몸체 비용 `go test -run '^$' -bench Bodies -count 5 ./internal/rigid/` 전후, 변환 비용 `BenchmarkConvert`(창 128·192·256).
- 비교 화면: `go run ./cmd/matter-wake -replay internal/replay/testdata/ice_glass.rpl -shots ... -dash panel,bodies,chains`.
- 직접 플레이로 볼 점: 얼음·유리를 팔 때 덩어리 전체가 몸체가 되는 순간이 보이지 않는지(같은 색), 가로로 판 얼음의 아래가 떨어지는 모습, 세로로 자른 유리 판의 작은 쪽이 떨어지는 모습, `B`로 놓은 glass_body를 팔 때 통째로 가루로 무너지는 모습, 큰 얼음 덩어리에서 창 경계가 보이는지.

## PR B: 충돌 균열과 잠듦 변환 (2026-11-10, `feat/body-cracks`)

PR A가 머지된 뒤(21281c8) 같은 요약에 이어 씁니다. 다시 읽은 것: `doc BODY_CRACK_SLEEP` 전체, `section C138`, 균열 이동체가 전기 이동체라서 전기 이동 규칙 `section C123 C124 C125`(모델 `model_electricity_move`는 C124가 밝히듯 균열 분기를 빼고 있어 균열의 모양은 알려 주지 않음), 재질 값(`noitaref -fields crackability,solid_on_sleep_convert,solid_break_to_type`). Box2D v3.2의 접촉 hit 사건은 소스(`contact_solver.c`, `solver.c`, `contact.c`)로 확인했습니다.

### 관측 규칙 (PR B)

| 규칙 (자기 말로) | 근거 (절:줄) | 확인 수준 |
|---|---|---|
| 균열 판정은 접촉 사건마다, 그 사건의 두 몸체 중 균열 가능(crackability > 0) 재질인 쪽마다 한다 | BODY_CRACK_SLEEP:44-46 | STATIC |
| 판정 속도는 그 쪽의 접촉 직전 법선 방향 상대 속도다(300 px/s로 던진 몸체가 332 px/s로 닿을 때 사건 벡터 (0, 33.2), 직후 속도 -0.2배) | BODY_CRACK_SLEEP:52-54 | LIVE |
| 속도 50 px/s 이하는 판정하지 않는다. 넘으면 난수 U∈[0,1]을 하나 뽑아 U × 100 ≤ 속도(px/s)이면 균열을 하나 만든다. 그래서 50 px/s를 넘자마자 약 50% 확률이고, 100 px/s 이상은 늘 생긴다 | BODY_CRACK_SLEEP:8-11, 47-48, 55-58 | STATIC + LIVE(150판정/64생성, 어긋남 0) |
| crackability 값(100, 40)은 문턱이나 확률에 쓰이지 않는다. 0보다 큰지와 이동체가 지울 셀인지(1 이상)만 본다 | BODY_CRACK_SLEEP:13, 50 | STATIC |
| 균열은 접촉점에서 진행 방향으로 조금 물러난 곳에 생기고, 몸체 안쪽으로(그 쪽 속도의 반대) 나아간다. 무작위로 흔들리며 가고 0~2번 갈라진다(전기 이동체, 에너지 200) | BODY_CRACK_SLEEP:47-51 | STATIC |
| 전기 이동체는 비액체 셀에서 반경 1~4 안에서 진행 방향과의 내적에서 거리/2를 뺀 점수가 가장 높은 셀로 옮겨 가고, 걸음마다 에너지를 1 쓴다. 균열 분기는 이 모델에 없다 | C124:3620-3622, 3628, 3636 | NATIVE + LIVE(균열 분기 제외) |
| 이동체가 지나간 줄의 균열 가능 셀이 지워지고, 그 입자는 셀로 돌아오지 않아 셀 수가 준다. 셀이 지워지는 정확한 조건은 모른다(C139가 밝힘, 아래 "PR B 2") | BODY_CRACK_SLEEP:14, 70-74, 123 | LIVE + OPEN |
| 순서: Step 안의 접촉 사건 → 균열 생성 → 이동체가 셀을 지움 → 재구성 → 유리면 통째로 부서짐 | BODY_CRACK_SLEEP:74 | LIVE |
| 910 px/s로 한 Step에 튀어 오른 바닥 충돌에는 판정이 없었다(616 px/s는 있었음). 원인은 모른다 | BODY_CRACK_SLEEP:16, 66-67, 124 | LIVE + OPEN |
| 잠듦 변환은 카메라 중심이 마지막 점검 위치에서 512 px 넘게(거리² > 512²) 움직였을 때만 돌고, 그때 점검 위치를 지금 중심으로 바꾼다 | BODY_CRACK_SLEEP:17-18, 78-79 | STATIC + LIVE |
| 대상: static이 아니고 잠듦 변환 재질인 몸체 중 몸체 원점과 카메라 중심의 거리가 350 px 이상이면서 잠들었거나, 750 px 이상인 것. 그 재질의 break_to 셀로 바뀌고 몸체는 사라진다(셀 수 = 몸체 셀 수) | BODY_CRACK_SLEEP:80-86, 89-93 | STATIC + LIVE(예측 3 = 변환 3) |
| 잠든 시간이나 정지 기준은 조건이 아니다. 카메라 이동과 거리만 본다 | BODY_CRACK_SLEEP:95-96 | LIVE |

### C138 3.4절: 실제로 잃은 셀

16×8(128픽셀) 몸체를 바위 바닥이나 steel 판에 던진 다섯 실행입니다(판정/생성, 끝의 셀 수). 생성 하나당 잃은 셀을 오른쪽에 셈했습니다(우리 계산).

| 실행(최대 \|v\|) | glass_brittle | ice_glass | rock_static_box2d |
|---|---|---|---|
| r100(172 px/s) | 3/1 → 125(생성당 3) | 16/4 → 94(8.5) | 3/1 → 128(0) |
| r300(332) | 7/3 → 116(4) | 10/4 → 125(0.75) | 4/1 → 128(0) |
| r600(616) | 14/5 → 108(4) | 17/6 → 108(3.3) | 4/2 → 128(0) |
| r900(첫 충돌 910, 판정은 184 px/s) | 2/1 → 128(0) | 9/6 → 118(1.7) | 3/1 → 118(10) |
| s300(steel 판, 330) | 4/1 → 121(7) | 5/2 → 111(8.5) | 5/3 → 126(0.7) |

glass_brittle과 ice_glass를 합하면 생성 33개에 셀 129개, 생성 하나당 약 3.9셀이고 0~17셀로 퍼집니다. 잃은 수는 속도에 비례하지 않습니다. glass는 모든 실행에서 첫 생성 뒤 통째로 부서졌습니다.

### 현재 코드 대조 (PR B)

| 관측 규칙 | 우리 코드 | 같음 / 다름 / 없음 | 다름이면 |
|---|---|---|---|
| 접촉 사건마다 균열 가능 쪽을 판정 | `internal/physics` `Hits`(Box2D 접촉 hit 사건), `internal/rigid` crack.go | 같음 | 사건은 균열 가능 재질의 형상에만 켜서(`enableHitEvents`), 다른 접촉은 사건을 만들지 않습니다 |
| 접촉 직전 법선 상대 속도 | Box2D hit 사건의 `approachSpeed`: Step의 해를 구하기 전에 접촉점에서 잰 법선 방향 상대 속도, 그 접촉의 점 중 가장 큰 것 | 같음(재는 시점) | Box2D는 사건마다 한 값을 줍니다. 두 몸체가 같은 속도를 받습니다(Noita도 사건 하나의 벡터) |
| 50 px/s 넘으면 U×100 ≤ 속도 | `crackMin` 50, `crackSure` 100칸/s 🧪 | 같음 | 칸 = px |
| 난수 | (seed, 틱, 몸체 key, 그 몸체의 이번 틱 판정 번호) 해시 🔶 | 다름 | Noita는 전역 LCG 하나. 결정성을 위해 몸체마다 따로 뽑음 |
| 910 px/s 한 Step 충돌에 판정 없음 | 따르지 않음: Box2D v3.2는 빠른 몸체를 연속 충돌로 멈춘 뒤 다음 Step의 접촉에서 큰 접근 속도로 사건을 냄 | 다름(개발자 결정 2026-11-10, "빠를수록 늘 균열") | `TestFastHitCracks` |
| 이동체가 지나간 셀을 지움 | 균열 이동체(아래 "PR B 2") | 같음(C139) | 처음에는 5픽셀 임시 선이었음. 충돌당 4개를 점에 나누는 것, 난수, 저장은 🔶 |
| 지운 셀은 돌아오지 않음 | 지운 픽셀을 재질별로 셈(`Game.Lost`) | 같음 + 셈 | 개발자 결정 2026-11-10, "Noita처럼 지우고 셈" |
| 재구성 → 유리 통째로 | 다음 틱의 `Break`(shatter) | 같음 | |
| 잠듦 변환: 카메라 512 px, 350 잠듦 / 750 | `game` 카메라 중심 점검, `rigid.System.Settle` 🧪 512, 350, 750 | 같음 | 지형 장면만(방은 카메라가 움직이지 않음) |
| 몸체 원점과의 거리 | 질량 중심과의 거리 🔶 | 다름 | 우리 조각은 부모의 원점을 지녀 원점이 몸체 밖일 수 있음 |
| break_to 셀, 셀 수 그대로 | 재질 필드 `sleepTo`, 픽셀마다 칸 하나(유리 부서짐과 같은 자리 규칙) | 같음 | |

### 실제 수치 (PR B)

| matter-wake 재질 | Noita 재질 | 필드 | Noita 값 | matter-wake 값 |
|---|---|---|---|---|
| ice_body | ice_glass | crackability, solid_on_sleep_convert, solid_break_to_type | 100, true, ice_static | `cracks: true`, `sleepTo: ice` |
| glass_slab | glass_brittle | crackability, solid_on_sleep_convert, solid_break_to_type | 100, true, glass_static | `cracks: true`, `sleepTo: glass` |
| glass_body | glass | crackability, solid_on_sleep_convert | 100, false | `cracks: true`, 잠듦 변환 없음 |
| wood | wood | crackability, solid_on_sleep_convert | 0, false | 없음 |
| (전역) | — | 판정 문턱, 확실 문턱 | 물리 단위 5, 10(×10 = px/s) | `crackMin` 50, `crackSure` 100칸/s 🧪 |
| (전역) | — | 점검 이동, 잠듦 거리, 무조건 거리 | 512, 350, 750 px | 512, 350, 750칸 🧪 |

### 자료에 없음 / OPEN → 우리 설계 (PR B)

TPT에는 강체와 충돌 균열, 몸체의 잠듦 변환이 없습니다(tpt.md: rigid bodies, destruction, sleep은 none). TPT의 전기(SPRK)는 셀을 지우지 않고, 균열과 무관해 가져온 것이 없습니다.

| 빈칸 | 자료 상태 | TPT 방식 | 우리 설계 | 측정 방법 | 추가 분석 제안 |
|---|---|---|---|---|---|
| 균열 난수와 판정 순서 | Noita는 전역 LCG, 순서는 Box2D 사건 순 | 없음 | ✅ 2026-11-10 사건을 (균열 나는 몸체의 key, 접촉점 x, y) 순으로 정렬하고, 난수는 (seed, 틱, key, 그 몸체의 이번 틱 판정 번호) 해시에서 뽑음. worker 수와 무관 | 리플레이 골든 1·4 worker | 아니오 |
| 균열 선의 모양과 길이 | 이동체(에너지 200, 분기 0~2)의 셀 제거 조건 OPEN, 생성당 평균 약 3.9셀(위 표) | 없음 | ✅ 2026-11-10 Noita 방식으로(개발자 결정, "#39에 함께 넣기"). 추가 분석 C139의 관측 동작을 독자 구현함(아래 "PR B 2"). 그 전의 임시 선: 접촉점에서 몸체 안쪽으로 8방향 5걸음(`crackSteps`) | `TestCrackLossesMatchNoita`(C139 5절과 비교) | 예: C139(2026-11-10 끝남, 분석 폴더의 별도 세션) |
| 접근 속도를 재는 때 | Noita: listener 사건의 벡터(접촉 직전) | 없음 | ✅ 2026-11-10 Box2D hit 사건: 각 Step의 해를 구하기 전, 접촉점의 법선 상대 속도. 접촉이 시작될 때가 아니라 접촉이 이어지는 동안 매 Step 잴 수 있으나, 문턱(50칸/s)보다 빠르게 다가오는 Step에만 사건이 남 | `TestCracksNeedSpeed` | 아니오 |
| 아주 빠른 충돌 | 910 px/s에 사건 없음, 원인 OPEN | 없음 | 개발자 결정 2026-11-10 "빠를수록 늘 균열". Box2D v3.2 hit 사건이 100 m/s 충돌에도 남는지 테스트로 고정 | `TestFastHitCracks` | 아니오 |
| 균열로 떨어진 2픽셀 미만 조각 | Noita는 1픽셀을 지움(C135) | 없음 | ✅ 2026-11-10 균열과 함께 지우고 셈(`Game.Lost`). 균열이 난 몸체를 그 틱의 Step 바로 뒤에 나누므로 파낸 조각과 경로가 갈림(Codex 리뷰 뒤) | `TestCrackedBodyLeavesWithItsBlockAsItIs` | 아니오 |
| 잠듦 변환의 거리를 재는 점 | 몸체 원점 | 없음 | ✅ 2026-11-10 질량 중심. 우리 조각의 원점은 부모 것이라 몸체 밖일 수 있음 | 테스트 | 아니오 |
| 첫 점검 위치 | 없음 | 없음 | ✅ 2026-11-10 세션을 시작할 때의 카메라 중심. 저장한 세션은 마지막 점검 위치를 세션 파일에 적어 이어 감 | `TestFarIceTurnsBackIntoTerrain`, `TestSavedSessionGoesOnTheSame` | 아니오 |
| 리플레이와 대시보드에서 잠듦 변환 | Noita는 카메라 이동뿐 | 없음 | ✅ 2026-11-10 리플레이는 입력의 카메라로 그대로 돎(지형 장면). 방 장면은 카메라가 움직이지 않아 돌지 않음. 대시보드에 "지금 점검" 버튼은 두지 않음(필요하면 다음에) | 지형 리플레이 골든 | 아니오 |
| 얼린 몸체 | Noita에는 얼림이 없음 | 없음 | ✅ 2026-11-10 얼린 몸체(메모리 가장자리)는 바꾸지 않음(개발자 결정, "응 5번도 그대로 두는 것으로 하자"). 그 블록이 내려가면 몸체로 저장되고, 돌아온 뒤 다시 점검됨. 이유: 셀 보존(일부가 메모리 밖인 몸체를 바꾸면 셀을 잃음)과 단순함, 떨어져 나온 덩어리는 몸체로 남는 편이 그럴듯함. 성능은 이유가 아님 | 테스트 | 아니오 |

### 결과 (PR B, 2026-11-10)

- 테스트: `internal/physics` hit_test.go(접근 속도, 91·200 m/s 충돌도 사건, 사건을 켠 형상과 문턱, 할당 0회), `internal/rigid` crack_test.go(속도 문턱, 확률이 속도/100, 균열 선이 안쪽 45° 안에서 5픽셀 이하, 던진 유리가 금 가고 가루가 됨, 1500칸/s 충돌, static 얼음의 균열, 잠듦 변환의 거리와 잠), `internal/game` `TestFarIceTurnsBackIntoTerrain`(카메라가 512칸 넘게 움직여야 점검), `internal/app` `TestSavedSessionGoesOnTheSame`(마지막 점검 자리가 저장으로 이어짐), `internal/save` 세션 형식 5. 리플레이 골든 `cracks.rpl`, `terrain_settle.rpl`(1·4 worker 같음, `cracks.rpl`은 매 틱 셀 보존에서 지운 픽셀을 셈).
- (5픽셀 임시 선 때의 측정, "PR B 2"에서 바뀜) 균열 하나가 지우는 픽셀: 꽉 찬 얼음 안으로 39번 냈을 때 평균 4.9(Noita 3.9).
- 측정: 깨짐 없는 틱의 몸체 몫 558.0 → 553.1 µs, Step 124.7 → 126.7 µs(main과 번갈아 5회 중앙값, 나무 몸체 450개).
- 화면: [cracks.png](../images/cracks.png), [cracks-static.png](../images/cracks-static.png), [cracks-panel.png](../images/cracks-panel.png), [settle.png](../images/settle.png).
- 남은 빈칸: 위 "자료에 없음"의 🔶(design.md 8절). 균열 이동체는 C139로 바꿨습니다(아래 "PR B 2").

## PR B 2: 균열 이동체 (C139, 2026-11-10, 같은 `feat/body-cracks`)

개발자가 균열 선을 Noita의 방식으로 바꾸기로 해("선의 모양과 매커니즘은 noita의 매커니즘을 따라하는 게 좋지 않나? 분석이 더 필요하다면 진행하고 말이야", "#39에 함께 넣기") 추가 분석 C139를 했고, 그 명세로 위의 5픽셀 임시 선을 바꿨습니다. 다시 읽은 것: `doc CRACK_MOVER` 전체(LIVE 4실행, 이동체 129개, 주장 21/21; 분기, 에너지 소진, 정적 지형 표시는 정적 판독뿐), 표시 색의 `doc CELL_DAMAGE_VISUALS` 3절, 재질 값(`noitaref -fields crackability,cell_type,solid_break_to_type`). 명세에 있는 실행 파일 주소와 앵커는 저장소에 옮기지 않았습니다.

### 관측 규칙 (C139)

| 규칙 (자기 말로) | 근거 (CRACK_MOVER 절:줄) | 확인 수준 |
|---|---|---|
| 균열은 접촉점에서 출발하고, 기준 방향은 접촉 직전 상대 속도의 반대(몸체 안쪽) 단위벡터이며 수명 내내 바뀌지 않는다. 바닥에 떨어진 몸체는 위로 | 3.1:59-63 | STATIC + LIVE(129개 모두 \|기준\| 5, 수직 낙하 116개는 (0, −5)) |
| 에너지 200, 속도 20, 분기 수 0~2, 분기 에너지 0~20(정수 균등) | 3.1:63 | STATIC |
| 걸음이 검사하는 칸은 이동 전 위치를 정수로 자른 칸이다. 첫 걸음은 생성 위치의 칸 | 3.1:64-65, 3.3:77 | STATIC + LIVE |
| 첫 프레임은 늘 움직이고, 그 뒤로는 프레임마다 0~100에서 뽑아 20 이하일 때만(21/101) 움직인다 | 3.2:67-68 | STATIC + LIVE(1,538프레임 중 0.213) |
| 움직이는 프레임마다 기준 방향을 돌린다: 32/129 확률로 ±(0.7~1.75) rad(부호 반반), 아니면 −0.25~0.25 rad. 돌린 방향은 저장하지 않아 꺾임이 쌓이지 않고 기준 방향 둘레로 지그재그한다 | 3.2:69-72 | STATIC + LIVE |
| 그 방향으로 4~10걸음, 걸음마다 에너지를 먼저 1 쓰고, 15/101 확률로 x나 y 한 축(반반)만 −1, 0, +1 흔들리고, 아니면 돌린 단위벡터만큼 간다 | 3.2:73, 3.3:75-76 | STATIC + LIVE(2,733걸음 재생 대조) |
| 걸음의 칸이 crackability ≥ 1 재질이면 표시한다(몸체 칸과 정적 지형을 가리지 않음). 대각 걸음이면 (새 x, 이전 y) 칸도 표시해 선이 4방향으로 이어진다 | 4.1:101-108 | STATIC + LIVE |
| 표시는 셀의 계수를 1 줄이고 색을 흰색 쪽으로 섞는다(채널 × 0.4 + 255 × 0.6) | 4.1:104, CELL_DAMAGE_VISUALS:57-65 | STATIC + NATIVE(제한) |
| 프레임의 마지막 걸음만 끝을 정한다: 표시 대상이 아니면 끝나고, 그 프레임에 표시한 것이 있으면 마지막 표시 칸의 몸체에서 표시된 칸을 모두(다른 이동체가 표시한 것까지) 지운다. 중간 걸음이 공기나 구멍을 지나도 계속 간다 | 3.4:80-85, 4.2:111-118 | STATIC + LIVE(지운 수 = 처리한 표시 합집합) |
| 마지막 프레임에 표시가 없던 이동체는 지우지 않아, 이전 표시가 밝은 금으로 남는다 | 4.3:119-124 | LIVE(v150 바위: 표시 62 중 52 남음) |
| 마지막 걸음이 표시였는데 에너지가 0 이하이면 지우지 않고 끝난다. 마지막 걸음이 표시가 아니면 에너지와 상관없이 위대로 지운다(마지막 걸음을 먼저 봄) | 3.4:80-82 | STATIC(LIVE 0건, 최소 남은 에너지 82) |
| 분기: 분기 수 > 0이고 에너지 = 분기 에너지이면 둘로 복제, 자식은 분기 수 −1, 에너지·분기 에너지 절반, 방향은 부모 기준을 U×1.2−1.5, U×1.2+0.3 rad 돌림. 부모는 그 프레임을 마치고 지워진다. 자식은 분기 에너지에서 태어나고 걸음마다 에너지를 먼저 써서 다시 분기하지 않는다 | 3.4:86-92 | STATIC(LIVE 0건) |
| 정적 지형 칸은 표시만 되고 이 경로로 지워지지 않는다 | 4.3:124-125 | STATIC |
| 지운 칸은 장식 입자가 되고, 몸체는 다음 프레임에 재구성된다 | 4.2:114-115, 6:156-158 | LIVE |
| 이동체는 세계 좌표로 걷는다. 움직이는 몸체는 긁힌다 | 6:161-164 | LIVE |
| 충돌 한 번에 몸체마다 이동체 4개(몸체 크기와 상관없이). 충돌 속도는 이동체를 만들지와 몇 개인지만 정하고 이동체 자체는 바꾸지 않는다 | 1:29-31, 5:127-149 | LIVE(16행 모두 4개) |

### 현재 코드 대조 (C139)

| 관측 규칙 | 우리 코드 (`internal/rigid` crack.go) | 같음 / 다름 / 없음 | 다름이면 |
|---|---|---|---|
| 접촉점에서 출발, 고정된 기준 방향 | `crack`: hit 점마다, 그 몸체 쪽 법선의 반대 | 같음 | Box2D 사건의 법선 = 접촉 직전 법선 방향 상대 속도의 방향(C138 3.2) |
| 에너지 200, 속도 20, 분기 0~2, 분기 에너지 0~20 | `crackEnergy`, `crackSpeed`, `crackForks`, `crackSplit` 🧪 | 같음 | |
| 이동 전 칸을 정수로 자름 | `math.Floor` | 같음(우리 좌표는 음수가 없음) | Noita는 0쪽 자름 |
| 첫 프레임은 늘, 뒤로 21/101 | `walk`: `started`가 아니거나 `IntN(101) <= 20` | 같음 | Noita는 "이전 칸 (0,0)"을 첫 프레임 표시로 씀. 우리는 따로 둠 |
| 꺾임 32/129 큰 것 ±0.7~1.75, 아니면 ±0.25, 쌓이지 않음 | `crackTurn`, `unit`(1/32767 단위) | 같음 | 돌림은 Box2D의 cos·sin(`physics.MakeRot`, CPU마다 같음) |
| 4~10걸음, 에너지 먼저, 흔들림 15/101 | `walk` | 같음 | |
| 표시와 대각 모서리 표시 | `markCrack`, `walk` | 같음(몸체 칸만) | 아래 "지형 칸 표시" |
| 흰색 쪽 0.6 섞기 | 표시된 픽셀은 칸에 손상 가득으로 보임(`crackShade`, 팔레트가 흰색 쪽 60%) | 같음(첫 표시) | 여러 번 지나도 더 밝아지지 않음 |
| 마지막 걸음만 끝을 정함, 끝날 때 그 몸체의 표시를 모두 지움 | `walk`, `crumble` | 같음 | |
| 표시 없는 마지막 프레임은 지우지 않음, 마지막 걸음이 표시였는데 에너지가 다 되면 지우지 않음 | `walk` | 같음 | 마지막 걸음을 먼저 봄(`TestCrackEndsOnItsLastStep`, Codex 리뷰) |
| 분기 | `fork`: 곁가지는 다음 틱부터 | 같음(정적 판독대로) | 곁가지의 난수는 부모 스트림에서 뽑은 씨앗 |
| 지운 칸은 입자 | 재질별로 셈(`Game.Lost`) | 다름(✅ "Noita처럼 지우고 셈") | |
| 다음 프레임에 재구성 | 그 틱 끝에 `Break`(Codex 리뷰로 정한 대로) | 다름 | 같은 프레임 두 번째 제거의 입자 중복이 없음 |
| 세계 좌표로 걸음 | 칸의 연결(`world.Cell.Body`)로 그 칸이 보이는 픽셀을 표시 | 같음 | |
| 충돌당 4개 | `crackChecks` 4: 그 틱에 몸체가 부딪힌 점들에 돌아가며 판정 4번 | 다름(🔶) | 아래 |
| 전역 LCG 하나 | 균열마다 자기 SplitMix64 스트림 | 다름(🔶) | 아래 |

### 실제 수치 (C139 5절과 우리 측정)

우리 측정은 `TestCrackLossesMatchNoita`: 시드 6개 × 150·300·600칸/s로 400×300 방의 돌 바닥에 던지고, 첫 충돌이 만든 균열이 모두 끝날 때까지 잃은 셀입니다(그 뒤 충돌은 판정하지 않음).

| 몸체 | Noita 첫 충돌 지워짐 (crackability 100) | matter-wake |
|---|---|---|
| 64×24 (ice_glass / glass_brittle ↔ ice_body / glass_slab) | 67, 69, 93, 48, 92, 95, 39, 42: 중앙값 68, 39~95 | 36번: 중앙값 73.5, 1~137 |
| 64×4 glass_brittle ↔ glass_slab | 18, 10, 11: 중앙값 11 | 18번: 중앙값 16, 6~21 |

이동체 하나의 걸음(`TestCrackWalksAboutItsAxis`, 250×250 얼음 안에서 200개): 뒤 프레임에 움직인 비율 0.202(Noita 명목 0.208, LIVE 0.213), 움직인 틱의 걸음 6.97(명목 7), 걸음당 기준 방향으로 0.700칸(꺾임의 평균 cos 0.824 × 흔들리지 않는 86/101 = 0.702), 가로 0.002칸, 131개(약 2/3)가 에너지 끝에서 분기.

재질: Noita의 crackability가 1 이상인 것은 모두 cell_type 3(box2d 고체)입니다. ice_glass 100, glass_brittle 100, glass 100, rock_static_box2d 40, glass_liquidcave 1, meteorite_crackable 100. 지형 재질 ice_static, glass_static, rock_static, wood_static, steel_static은 0입니다.

### 자료에 없음 / OPEN → 우리 설계 (C139)

TPT에는 강체도 균열 이동체도 없습니다(tpt.md: rigid bodies, destruction은 none). TPT의 전기(SPRK)는 셀을 지우지 않아 가져온 것이 없습니다.

| 빈칸 | 자료 상태 | TPT 방식 | 우리 설계 | 측정 방법 | 추가 분석 제안 |
|---|---|---|---|---|---|
| 충돌당 이동체 4개의 출처 | OPEN(C139 9절: 접촉 수 구조) | 없음 | 🔶🧪 몸체가 부딪힌 틱에 판정 4번(`crackChecks`)을 그 틱의 점들에 정렬해 돌아가며. Box2D v3.2는 접촉마다 사건 하나라, `physics.World.Hits`가 사건의 접촉에서 문턱보다 빠르게 다가온 점을 모두 줌(평평하게 떨어진 상자는 두 아래 모서리). 우리 바닥 충돌은 청크마다 따로라 64칸 몸체는 접촉이 둘일 수 있는데, 판정 수를 점 수가 아닌 몸체마다 4로 두어 그 영향을 없앰 | `TestHitMakesFourCracks`, `physics.TestHitsListEveryPointOfTheContact` | 아니오 |
| 이동체의 난수 | 전역 LCG(C139 8절: 다른 시스템의 사용 순서가 결과를 바꿈) | 없음 | 🔶 n번째 판정이 만든 균열은 (seed, 틱, key, n+1)에서 시작하는 자기 스트림(✅ 판정 순서와 난수의 열쇠 그대로). 곁가지는 부모 스트림에서 뽑은 씨앗. 균열은 만든 순서로 틱마다 한 번 걷고 곁가지는 다음 틱부터 | 리플레이 골든 1·4 worker | 아니오 |
| 여러 틱에 사는 이동체와 저장·블록·몸체 | Noita는 저장 시 이동체 개체를 함께 저장(개체), 우리 조사 범위 밖 | 없음 | 🔶 걷는 균열은 세션 파일에(형식 6 `crack` 줄), 표시는 몸체 픽셀과 함께 블록에(형식 5). 저장을 사이에 둬도 같음(`TestSavedCracksGoOn`). 저장이 블록을 내렸다 올리며 Box2D를 새로 만들 때(`Reset`)도 균열은 그대로 걷는다. 블록이 나가면 그 위의 균열은 메모리에 없는 칸을 공기처럼 보고 끝남. 균열은 몸체에 묶이지 않아 몸체가 파이거나 부서지거나 지형으로 돌아가도 남은 칸을 보고 걸음. 리플레이는 처음부터 다시 돌아 같음 | `TestSavedCracksGoOn`, `save` 형식 시험 | 아니오 |
| 표시 상태를 둘 곳 | Noita는 셀 계수(+4)를 줄임. 채굴·폭발과 공유하는지 OPEN(C139 9절) | 없음 | 🔶 몸체 픽셀의 표시 비트(`pixel.bits`의 `pixelMarked`, 픽셀은 4바이트 그대로). 손상과 따로라 채굴 손상이 표시가 되거나 그 반대가 되지 않음. 칸은 손상 가득으로 그려 흰색 쪽 60% | `TestCrackWalksAboutItsAxis`(표시 칸의 손상), `TestCrackTakesOutTheMarksOfItsBody` | 아니오 |
| 지형 칸 표시 | 정적 판독: 표시만 되고 지워지지 않음(LIVE 없음) | 없음 | 🔶 하지 않음. 우리 `cracks`는 고체에만 둘 수 있고(4절), 지형 얼음·유리는 Noita ice_static·glass_static처럼 0. 균열 나는 지형 재질이 생기면 그때 정함 | 재질 검증 | 아니오 |
| 같은 프레임 두 번째 제거의 입자 중복 | LIVE 한 사례(C139 6절) | 없음 | 따르지 않음: 입자가 없고 몸체는 그 틱 끝에 나눔 | 해당 없음 | 아니오 |
| 분기와 에너지 소진의 LIVE | OPEN(C139 9절) | 없음 | 정적 판독대로 구현(`fork`, `walk`). 큰 몸체 안에서 2/3가 에너지 끝에 분기함 | `TestCrackWalksAboutItsAxis` | 아니오(필요하면 큰 균열 영역의 LIVE를 제안) |

### 결과 (PR B 2, 2026-11-10)

- 테스트: `internal/physics` `TestHitsListEveryPointOfTheContact`, `internal/rigid` `TestCrackWalksAboutItsAxis`, `TestCrackTakesOutTheMarksOfItsBody`, `TestHitMakesFourCracks`, `TestFastHitCracks`(이제 균열 4개가 생기는지), `TestCrackLossesMatchNoita`, `TestWalkingCracksDoesNotAllocate`, `internal/save` `TestBlockReadsVersion4`, 세션 형식 6, `internal/app` `TestSavedCracksGoOn`(저장 때 균열 3개, 저장 뒤 111픽셀을 같게 지움).
- 리플레이 골든: 균열 나는 몸체가 있는 `cracks`, `ice_glass`, `terrain_settle`만 바뀜(1·4 worker 같음, 매 틱 셀 보존 통과).
- 화면: [cracks.png](../images/cracks.png), [cracks-zoom.png](../images/cracks-zoom.png)(균열이 자라고 끝나며 지우는 모습), [cracks-static.png](../images/cracks-static.png), [cracks-panel.png](../images/cracks-panel.png), [settle.png](../images/settle.png)을 이 구현으로 다시 찍음.
- 남은 빈칸: 위 표의 🔶(design.md 8절에서 물음), Noita에서도 OPEN인 4개의 출처와 분기의 LIVE.
