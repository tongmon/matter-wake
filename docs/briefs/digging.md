# 채굴(셀 HP와 내구 등급) 참고 요약

작성: 2026-11-06 · 관련 작업: `feat/dig-inventory` (재질별 등급·내구와 채굴 도구, 파낸 재질을 인벤토리로 회수하고 다시 놓기)

## 범위
- 이번 변경이 다루는 메커니즘: 셀 HP와 내구 등급(mechanics.md의 "Cell HP, explosions, projectiles"). 우리 쪽에서는 붓으로 파기와, 파낸 셀을 인벤토리로 옮기기입니다.
- 함께 읽은 연관 메커니즘: 재질 변환 때의 HP 초기화(C54), 폭발로 격자에서 빠진 셀의 행방(C83, CLONE_IMPLEMENTATION_ENTRY:146·162). 파괴 조각(DESTRUCTION_BRIDGE)은 강체 단계(4단계)의 일이라 읽지 않았습니다.
- 읽은 실행 모델: `native_probe_explosion_hole`(`hp_result`), `native_probe_explosion_ray`(`predict`), `native_probe_solid_explosion`(고체 연소·폭발 크기라 채굴과는 관계가 적음)

## 관측 규칙
| 규칙 (자기 말로) | 근거 (절:줄) | 확인 수준 |
|---|---|---|
| 셀마다 자기 HP가 있다. 셀이 생기거나 다른 재질로 바뀔 때 그 재질의 `hp` 값으로 채운다. 같은 재질로 바꾸는 것은 아무것도 하지 않아 HP도 그대로다 | C54:1588, C54:1600 | EXECUTED + LIVE |
| 피해는 셀 HP에서 정수로 뺀다. HP가 0 이하가 되면 그 셀을 공기로 바꾼다. 양수로 남으면 셀은 그대로 있다 | C66:2079, C66:2083, C75:2334 | EXECUTED + LIVE |
| 피해를 주는 쪽에는 내구 한계가 있다. 재질의 `durability`가 한계보다 크면 피해를 주지 못하고 막힌다. 같으면 피해를 준다 | C66:2075-2076, C67:2111, C75:2332-2333, 모델 `hp_result`(durability ≤ limit일 때만 적용) | EXECUTED + LIVE |
| 투사체 관통은 한계가 0 이하이면 이 검사를 하지 않는다. 폭발은 한계 0도 그대로 비교한다 | C66:2075-2076, C67:2112, C75:2333 | EXECUTED |
| 폭발 광선이 에너지를 쓰는 셀은 고체(강체 재질)와 `liquid_sand` 셀(가루, 고정 지형)뿐이다. 일반 액체·기체·불은 에너지를 쓰지 않고 지나간다 | C67:2101-2105 | EXECUTED + LIVE |
| 폭발 광선은 셀마다 min(남은 에너지, 셀 HP)를 쓰고, 한 셀이 받는 피해는 그 광선이 한 셀에 쓴 최댓값이다(합이 아님) | C67:2108-2109, C75:2331 | EXECUTED + LIVE |
| 투사체가 주는 피해는 속도 제곱에 비례하는 에너지이고, 셀을 지날 때마다 남은 HP 비율만큼 느려진다 | C66:2077-2084 | EXECUTED + LIVE |
| 폭발로 격자에서 빠진 셀이 곧 사라진 것은 아니다. 액체 입자가 되었다가 HP가 0 이하인 채로 격자에 돌아온 사례가 많다 | C83:2546-2548, CLONE_IMPLEMENTATION_ENTRY:146·162 | LIVE |

플레이어가 붓처럼 쓰는 채굴 도구는 자료에 없습니다. Noita에서 지형을 파는 것은 투사체와 폭발입니다. 아래 "자료에 없음"에 적었습니다.

## 현재 코드 대조
수정 전 코드(`main`, 5f8a3b2) 기준입니다.

| 관측 규칙 | 우리 코드 (파일:줄) | 같음 / 다름 / 없음 | references.md 기록 |
|---|---|---|---|
| 셀마다 HP | 없음. 셀은 12바이트이고 남는 바이트가 없음(`world.Cell`, `TestCellSize`) | **없음** | 안 적힘 |
| HP가 0 이하이면 공기로 | `internal/game/game.go` `Step`: 오른쪽 클릭이 `world.Paint(..., material.Air, false)`로 붓 안을 즉시 비움 | **다름**(시간이 걸리지 않음) | 안 적힘. 샌드박스 임시 조작으로만 적혀 있음(design.md 5절) |
| 내구 한계 | 없음. 모든 재질을 팜 | **없음** | 안 적힘 |
| 액체는 피해 대상이 아님 | `Paint`가 물도 지움 | **다름** | 안 적힘 |
| 파낸 셀의 행방 | 사라짐. 놓기는 재질을 무한히 만듦(`Paint(..., onlyEmpty)`) | 해당 없음(자료에 채굴 회수가 없음) | 안 적힘 |

이번 변경 뒤의 대응은 아래 "채택 / 변경 / 제외"에 적었고, references.md 표에도 옮깁니다. 셀마다 HP는 개발자 결정(2026-11-06)으로 셀의 전용 손상 칸(`Cell.Damage`, 셀 16바이트)이 되었습니다.

## 실제 수치
변환은 둘 다 그대로(×1)입니다. 우리 등급(`hardness`)은 Noita의 `durability`, 우리 내구(`hp`)는 Noita의 `hp`와 단위가 같습니다. 재질 값과 도구 값은 모두 🧪입니다.

| matter-wake 재질 | Noita 재질 | 필드 | Noita 값 | 변환식 | matter-wake 값 |
|---|---|---|---|---|---|
| stone | rock_static | durability / hp | 10 / 100000 | ×1 | hardness 10 / hp 100000 |
| sand | sand | durability / hp | 4 / 800 | ×1 | hardness 4 / hp 800 |
| water | water | durability / hp | 0 / 100 | 쓰지 않음(액체는 파지 않음) | 없음 |
| (참고) | soil | durability / hp | 10 / 2000 | | 흙을 넣을 때 |
| (참고) | rock_hard | durability / hp | 10 / 200000 | | |
| (참고) | gold | durability / hp | 12 / 8000 | | 등급이 10을 넘는 예 |
| (참고) | wood_static | durability / hp | 10 / 2000 | | |

값의 출처는 `analysis-detail/runtime-completion/materials-expanded-analysis.json`입니다(`go run ./tools/noitaref -fields hp,durability material rock_static sand water soil rock_hard gold wood_static`). 이 값들은 Noita에서 `liquid_sand`가 참인 셀, 즉 가루와 고정 지형이고, 물은 `liquid_sand`가 거짓입니다.

도구 값(자체 설계, 🧪): 힘 10, 틱당 2000, 붓 반경 4.
- 돌 한 칸은 ⌈100000 ÷ 2000⌉ = 50틱(약 0.8초)입니다.
- 모래 한 칸은 1틱입니다.
- 힘 10은 Noita 지형 대부분(등급 10)을 팔 수 있고 금(12) 같은 것은 못 파는 값입니다.

## 연관 메커니즘
| 메커니즘 | 이번에 반영 | 이유 |
|---|---|---|
| 재질 변환 때 HP 초기화(C54) | 예(형태를 바꿔서) | 남은 HP 대신 잃은 비율을 셀의 손상 칸에 둡니다. 새 셀과 다른 재질로 바뀐 셀은 0에서 시작하고, 같은 재질로 칠하면 그대로입니다(C54:1588) |
| 폭발·투사체의 셀 피해(C66, C67, C75) | 나중에(4단계) | 폭발과 투사체가 생기면 같은 `hardness`·`hp`를 씁니다. 그때 셀 HP를 어디에 둘지 다시 정합니다 |
| 격자에서 빠진 셀의 입자화(C81~C84) | 아니오 | 채굴은 셀을 인벤토리로 옮깁니다. 튀는 입자는 1단계의 다른 항목입니다 |

## 채택 / 변경 / 제외
- 그대로 씀:
  - 재질마다 등급과 내구 두 값을 두고, 도구의 힘이 등급보다 작으면 파지 못합니다. 같으면 팝니다(Noita 관측, C75 `hp_result`).
  - 정수 내구를 깎아 0 이하가 되면 셀을 뺍니다(Noita 관측).
  - Noita 재질의 실제 값을 그대로 씁니다(Noita 관측).
- 바꿔서 씀 (이유):
  - 남은 HP를 정수로 두지 않고, 잃은 비율을 2바이트 칸에 내구의 1/65536 단위로 올려 셉니다. 내구가 얼마인 재질이든 칸 하나로 되게 하려는 것입니다(개발자 결정 2026-11-06). 대가로 내구 ÷ 피해가 정수보다 아주 조금 크면 마지막 한 번이 줄어듭니다.
  - 액체뿐 아니라 빈 칸도 대상이 아닙니다. 파는 대상은 가루와 고정 재질입니다(개발자 결정 2026-11-06. Noita 폭발 광선이 에너지를 쓰는 셀과 같습니다).
  - 피해는 붓 안의 셀마다 틱당 같은 양입니다. Noita 폭발이 셀마다 같은 피해(광선 최대 소모량)를 주는 것과 비슷합니다. 그러나 폭발처럼 거리나 차폐로 줄지는 않습니다(자체 설계).
- 쓰지 않음 (이유):
  - 투사체의 속도 제곱 에너지와 감속(C66): 투사체가 없습니다.
  - 폭발 광선 예산과 차폐(C67, C73~C75): 폭발이 없습니다. 4단계에서 봅니다.
  - 투사체의 "한계 0 이하이면 검사 안 함" 예외(C66): 도구의 힘 0은 등급 0인 재질만 파는 뜻으로 둡니다. 예외를 두지 않습니다.

## 자료에 없음 / OPEN → 우리 설계
검색한 말: `dig`, `digger`, `drill`, `CellEater`, `굴착`, `채굴`, `durability`, `내구`, `셀 HP`, `ground_penetration`, `crackability`. 채굴 도구에 관한 내용은 "채굴 도구의 작용은 새 게임의 설계 사항"이라는 안내(ENVIRONMENT_GAME_HANDOFF:38)뿐이었습니다.

TPT에는 파기 도구도, 입자별 HP도, 인벤토리도 없습니다. TPT의 `Hardness`는 산(acid)에 녹는 정도입니다(`Element.h`, tpt.md "Element fields").

| 빈칸 | 자료 상태 | TPT 방식 (파일:함수) | 우리 설계 (우리 제약에 맞춘 방법) | 측정 방법 | 추가 분석 제안 |
|---|---|---|---|---|---|
| 플레이어의 채굴 도구 | 없음(Noita는 투사체·폭발로 팜) | 없음 | 도구는 힘, 틱당 파는 양, 붓 반경을 가집니다(개발자 결정 2026-11-06). 파기 버튼을 누른 틱마다 붓 안의 셀 하나하나에 틱당 양만큼 손상을 더합니다. 손상이 내구 이상이 되면 그 셀을 공기로 바꿉니다 | `TestDigTakesHPOverRate`: 돌 한 칸이 정확히 ⌈hp ÷ 틱당 양⌉틱에 빠짐 | 아니오(세 조건 중 미충족: 아직 자체 설계를 시도하지 않음) |
| 12바이트 셀에서 손상을 둘 곳 | 없음(Noita는 64바이트 셀 안에 HP를 둠) | 없음 | 셀을 16바이트로 키우고 손상 전용 2바이트 칸을 둡니다(개발자 결정 2026-11-06). 처음에는 붓 아래 셀만 게임 계층이 기억하는 임시안으로 구현했다가 교체했습니다. 손상을 읽고 쓰는 곳은 `internal/world/damage.go`의 `World.Wear` 하나입니다. 측정: 1024×1024 1 worker +2% | `TestWearTakesHPOverAmount`, `TestWearCountsTinyDamage`, `TestDigDamageLastsWhenDiggingStops`, `BenchmarkStep` | 아니오(세 조건 중 미충족: 자체 설계가 측정을 통과함) |
| 손상을 받던 가루가 움직일 때 | 해당 없음(Noita는 HP가 셀과 함께 움직임) | 없음 | 셀이 자리를 바꿀 때 칸 전체가 함께 옮겨지므로 손상도 따라갑니다(Noita와 같음) | `TestDamageMovesWithTheCell`, `TestDigDamageDoesNotPassToAnotherCell` | 아니오 |
| 파낸 셀의 행방 | 없음(Noita는 셀을 지우거나 입자로 날림) | 없음 | 재질별 셀 수로 인벤토리에 담습니다. 한도는 1단계에서 두지 않습니다(개발자 결정 2026-11-06). 놓기는 붓 안의 빈 칸을 아래 행부터(행 안에서는 왼쪽부터) 채우고, 칸마다 인벤토리에서 하나씩 뺍니다. 다 떨어지면 남은 칸은 빈 채로 둡니다. 아래부터 채우므로 모자란 양으로 구멍을 메우면 바닥부터 찹니다 | `TestPlaceTakesFromInventory`, 리플레이 골든의 매 틱 보존 검사 | 아니오 |
| 시작 인벤토리 | 없음 | 없음 | 비어 있습니다. 판 만큼만 놓을 수 있고, 물은 퍼 담기 도구가 생길 때까지 놓을 수 없습니다. 개발용 `-creative`가 무한 놓기를 켜고 리플레이 헤더에 기록됩니다(개발자 결정 2026-11-06) | `TestCreativePlacesWithoutInventory`, 리플레이 골든 | 아니오 |

## 검증 계획
- 테스트(`internal/game`):
  - `TestDigRefusesHarderMaterial`: 등급이 도구 힘보다 높은 재질은 오래 파도 남습니다.
  - `TestDigTakesHPOverRate`: 내구만큼 시간이 걸립니다.
  - `TestDigSkipsLiquids`: 액체는 파지 않습니다.
  - `TestDigAndPlaceConserveCells`: 무작위로 파고 놓아도 재질마다 셀 수 + 인벤토리 수가 같습니다.
  - `TestPlaceTakesFromInventory`: 놓은 만큼 인벤토리에서 빠지고, 비어 있으면 놓지 못합니다.
  - `TestDigDamageLastsWhenDiggingStops`, `TestDigDamageDoesNotPassToAnotherCell`, 그리고 할당 0회 테스트. `internal/world`의 `TestWearTakesHPOverAmount`, `TestWearCountsTinyDamage`, `TestDamageMovesWithTheCell`, `TestNewCellsAreUndamaged`.
- 리플레이 골든(`internal/replay/testdata`): 기존 `sandbox_tools.rpl`을 새 규칙에 맞게 다시 쓰고, 파고 놓기를 되풀이하는 `dig_place_cycle.rpl`을 더합니다. 매 틱 재질마다 셀 수 + 인벤토리 수가 시작과 같아야 합니다.
- 비교 화면: `go run ./cmd/matter-wake -replay internal/replay/testdata/dig_place_cycle.rpl -shots 0,300,600 -shots-dir out/shots`
- 직접 플레이로 볼 점:
  - 돌 한 칸 50틱(약 0.8초)이 너무 느리거나 빠르지 않은가.
  - 끊어 파도 이어지는 손상이 자연스러운가(조금 깎아 둔 돌이 남는 것).
  - 모래가 1틱에 파여 무너지는 더미를 파는 느낌.
