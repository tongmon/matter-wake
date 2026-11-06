# 스트리밍(블록 상주와 퇴거) 참고 요약

작성: 2026-11-06 · 관련 작업: `feat/noita-residency`

## 범위
- 이번 변경이 다루는 메커니즘: 카메라가 어떤 블록을 요청하는지, 메모리의 블록을 언제 몇 개 내리는지, 어느 블록을 내리는지.
- 함께 읽은 연관 메커니즘(mechanics.md의 Related): 휴면. 청크 휴면은 바꾸지 않습니다. 블록 표(C43)와, 접근 범위가 상주하지 않는 작업 영역을 빼는 규칙(C122, `model_worker_residency`)은 #14에서 반영했고 이번에 바꾸지 않습니다.

읽은 자료: STREAMING_POLICY 전체, C129, C17, C120, WORLD_STREAMING, C43, C122, `model_streaming_policy`(이해용으로만 읽음).

## 관측 규칙
| 규칙 (자기 말로) | 근거 (절:줄) | 확인 수준 |
|---|---|---|
| 카메라 중심에서 화면 절반을 빼 화면 사각형을 만들고 네 값을 정수로 자릅니다(0 방향). 넷 중 하나라도 이전과 다를 때만 요청 단계로 갑니다 | STREAMING_POLICY:13-16 | STATIC + NATIVE + LIVE |
| 요청 영역: x, y를 0 방향으로 64의 배수로 맞춘 뒤 64를 빼고, 폭과 높이에 128을 더합니다 | STREAMING_POLICY:18-22, :35 | NATIVE + LIVE |
| 영역이 지난 요청과 같으면 아무것도 요청하지 않습니다. 다르면 그 영역과, 그 영역을 지난 영역에서 움직인 양의 4배만큼(축마다 ±512까지) 옮긴 영역을 따로 요청합니다. 둘 사이를 채우지는 않습니다 | STREAMING_POLICY:23-36 | NATIVE + LIVE |
| 영역이 걸치는 블록은 양 끝을 포함합니다(x>>9부터 (x+w)>>9까지). 끝이 정확히 512면 다음 블록도 듭니다 | STREAMING_POLICY:39-43 | STATIC, 요청 사각형은 LIVE 대조 |
| 상주하지도 않고 요청 중이지도 않은 블록에만 로딩 작업을 만듭니다. 로딩은 별도 스레드에서 비동기로 일어납니다 | STREAMING_POLICY:44-46, C120:3541-3543 | STATIC, LIVE |
| 퇴거 검사는 실제 시간으로 간격(기본 1초)이 지났을 때 합니다. 목표 상주 수 T는 12입니다 | STREAMING_POLICY:55-60, C17:231-234 | NATIVE, LIVE |
| N ≤ T면 간격을 기본값으로 되돌리고 내리지 않습니다. N ≥ 2T면 다음 간격을 절반으로 줄입니다. T < N ≤ 3T면 한 번, N > 3T면 N − 3T번 고르며, 고를 때마다 후보를 다시 봅니다. T는 상한이 아닙니다 | STREAMING_POLICY:63-74 | NATIVE |
| 보호되는 블록은 만료 틱이 현재 틱보다 큰 keep-alive 요청뿐입니다 | STREAMING_POLICY:77-79 | NATIVE + LIVE |
| 개체의 keep-alive(StreamingKeepAliveComponent)는 개체 영역을 양쪽 256칸 넓혀 보호합니다. 카메라의 요청 공식과는 다른 경로입니다 | STREAMING_POLICY:80-81, WORLD_STREAMING:29-34 | STATIC |
| 내릴 블록: 화면 가운데가 있는 블록과의 블록 단위 거리 제곱이 가장 큰, 보호되지 않은 상주 블록. y, 그다음 x 오름차순으로 훑고 더 클 때만 바꾸므로 같은 거리면 먼저 나온 것. 거리 0(화면 가운데 블록)은 고르지 않고, 후보가 없으면 보호를 무시하지 않습니다 | STREAMING_POLICY:83-90 | NATIVE + LIVE |
| 고른 블록은 저장 표시와 함께 내리고 저장합니다. 디스크 저장 완료와 상주 해제는 별개입니다 | STREAMING_POLICY:91, :97-101 | NATIVE, STATIC |
| N < 2T이고 별도 기한(delay_until) 전이면 검사를 미룹니다. 이 기한을 누가 왜 거는지는 미확정입니다 | STREAMING_POLICY:64-65 | NATIVE |
| 실제 장면에서 고를 때의 상주 수는 13~26, 간격은 1초~0.0078125초였습니다 | STREAMING_POLICY:114 | LIVE |

## 현재 코드 대조
바꾸기 전 코드(#16~#19)는 화면을 사방 256칸 넓힌 범위를 올리고, 블록이 12개를 넘으면 그 틱에 넘는 만큼을 내렸습니다. 이 256은 위 표의 개체 keep-alive 경로를 카메라 규칙으로 잘못 읽은 것이었습니다(2026-11-06에 STREAMING_POLICY로 확인). 아래는 바꾼 뒤의 대조입니다.

| 관측 규칙 | 우리 코드 (파일:줄) | 같음 / 다름 / 없음 | 다름이면 |
|---|---|---|---|
| 요청 영역 공식 | internal/game/stream.go `requestArea` | 같음(`Stream.Grid` 64, 🧪) | |
| 바뀌었을 때 앞쪽 영역 요청 | stream.go `request`, `aheadOf` | 같음(`Lead` 4, `LeadMax` 512, 🧪) | 첫 요청에는 앞쪽이 없음: 자료에 첫 "지난 영역"이 없어 자체 설계. references.md에 적음 |
| 바뀌었을 때만 요청 | stream.go `request` | 다름 | 카메라 영역은 매 틱 상주를 확인. 검사가 그 영역을 내리지 않으므로 평소 결과는 같고, 블록이 다른 이유로 빠졌을 때만 다름. references.md에 적음 |
| 양 끝 포함 | stream.go `areaBlocks` | 같음 | |
| 비동기 로딩 | stream.go `loadBlock`, internal/app prefetch.go | 다름 | 블록은 틱 안에서 올림. 내용 준비만 백그라운드(#19). 결정성 때문. references.md에 적음 |
| 실제 시간 간격 | stream.go `check` | 다름 | 틱으로 셈(`Stream.Check` 60틱, 🧪). 결정성 때문. references.md에 적음 |
| N과 T 규칙, 간격 절반·복구 | stream.go `check` | 같음 | 간격 하한은 1틱(Noita는 0.0078125초까지 관측) |
| 보호 = keep-alive 요청 | stream.go `check`의 `keep` | 다름 | 지금 카메라 영역을 보호. 아직 개체가 없고, 화면 블록이 빠지면 안 되기 때문. references.md에 적음 |
| 거리, 동률, 거리 0 | stream.go `farthest` | 같음 | |
| delay_until 미루기 | 없음 | 없음 | 의미가 미확정이라 두지 않음 |
| 내릴 때 저장 | stream.go `check`의 `store.Save` | 같음 | |

## 증상 재현 (증상을 고치는 작업일 때)
- 증상을 고치는 작업은 아닙니다. 개발자 결정(2026-11-06, "Noita 방식으로")에 따른 규칙 교체입니다.
- 결정 전 임시 측정(저장소 밖, `go test -overlay`): 같은 규칙을 임시로 구현해 16384² 지형에서 60Hz로 900틱 걸었습니다.
  - 틱당 16칸 직진: 틱 중앙값 1.04 → 1.16 ms, 상주 블록 최대 12 → 26, 깨어 있는 청크 +23%.
  - 1536칸 왕복: 다시 올린 블록 6 → 18, 내린 블록 0 → 9.
  - 틱당 4칸: 차이 없음.

## 실제 수치
재질 값은 없습니다. 스트리밍 수치는 다음과 같습니다.

| 항목 | Noita 값 | 변환 | matter-wake 값 |
|---|---|---|---|
| 요청 격자와 테두리 | 64 | 그대로 | `Grid` 64 🧪 |
| 앞쪽 배수 | 4 | 그대로 | `Lead` 4 🧪 |
| 앞쪽 축별 상한 | 512 | 그대로 | `LeadMax` 512 🧪 |
| 목표 상주 수 | 12 | 그대로 | `Target` 12 🧪 |
| 검사 간격 | 1.0초(실제 시간) | 60 Hz 틱으로 | `Check` 60틱 🧪 |

## 연관 메커니즘
| 메커니즘 | 이번에 반영 | 이유 |
|---|---|---|
| 청크 휴면 | 아니오 | 바꾸지 않음 |
| 접근 범위가 상주하지 않는 작업 영역 제외(C122) | 이미 반영(#14) | 바꾸지 않음 |
| 개체 keep-alive(양쪽 256칸 보호) | 나중에 | 생명체를 넣을 때 |
| 플랫폼 틱 기반 자동 저장(STREAMING_POLICY:58) | 나중에 | 자동 저장은 아직 없음 |

## 채택 / 변경 / 제외
- 그대로 씀: 요청 영역 공식, 바뀌었을 때 앞쪽 영역 요청, 양 끝 포함 블록 범위, N과 T에 따른 검사 횟수, 간격 절반과 복구, 가장 먼 블록 고르기와 동률 규칙 (Noita 관측)
- 바꿔서 씀:
  - 실제 시간 대신 틱으로 간격을 셈. 세션 결과가 실행 속도에 매이지 않게 (자체 설계)
  - 비동기 로딩 대신 틱 안에서 올리고, 내용만 백그라운드에서 준비. 결과가 디스크 속도에 매이지 않게 (자체 설계)
  - keep-alive 대신 지금 카메라 영역을 보호. 개체가 없고 화면 블록은 늘 있어야 하므로 (자체 설계)
  - 카메라 영역은 바뀔 때만이 아니라 매 틱 상주를 확인 (자체 설계)
  - 간격 하한 1틱 (자체 설계)
- 쓰지 않음: delay_until 미루기(의미 미확정), 요청 뒤 refresh 함수(물리·재질 정리를 포함하나 의미 미확정), 자동 저장 분기(범위 밖)

## 자료에 없음 / OPEN → 우리 설계
TPT(The Powder Toy)에는 월드 스트리밍이 없습니다. 스킬의 tpt.md도 스트리밍을 TPT가 다루지 않는 빈칸으로 둡니다.

| 빈칸 | 자료 상태 | TPT 방식 (파일:함수) | 우리 설계 (우리 제약에 맞춘 방법) | 측정 방법 | 추가 분석 제안 |
|---|---|---|---|---|---|
| 첫 요청 때의 "지난 영역" | 없음 | 없음 | 첫 요청은 카메라 영역만, 앞쪽 없음 | `TestStreamRequestsAhead` | 아니오 (게임 느낌에 거의 영향 없음) |
| 카메라 영역 자체가 보호되는지 | 보호 목록은 keep-alive만 서술 | 없음 | 카메라 영역은 내리지 않음 | `TestEvictionPicksFarthestFirst` | 아니오 (우리 설계가 측정을 통과) |
| 요청부터 상주 완료까지의 지연, 로딩 우선순위·취소 | OPEN(STREAMING_POLICY:130) | 없음 | 틱 안에서 올림. 미리 읽기가 늦으면 그 틱이 기다림 | 늦은 블록 수 임시 계측(디스크 지연 20 ms까지 늦음 없음) | 아니오 |
| 개체 keep-alive와 카메라 요청을 합치는 규칙 | OPEN(WORLD_STREAMING:35, STREAMING_POLICY:129-132) | 없음 | 생명체를 넣을 때 설계 | 생명체를 넣을 때 | 나중에 판단 |
| 저장 뒤 재개할 때 요청·검사 상태 | 미검증 | 없음 | `session.txt`의 `stream` 줄에 지난 영역, 마지막 검사 틱, 간격을 저장 | `TestSavedSessionGoesOnTheSame` | 아니오 |
| delay_until의 의미 | 미확정(STREAMING_POLICY:64-65) | 없음 | 두지 않음 | 없음 | 아니오 (세 조건 미충족: 아직 문제로 드러나지 않음) |

## 검증 계획
- 테스트: `TestRequestArea`, `TestAreaBlocksIncludeTheFarEdge`, `TestStreamRequestsAhead`, `TestEvictionCheckSchedule`, `TestEvictionPicksFarthestFirst`, `TestStandingStillUnloadsDownToTarget`, `TestLeavingAndComingBackKeepsTheBlocks`, `TestSavedSessionGoesOnTheSame`(검사를 7틱마다 하는 경우 포함), 세션 형식 테스트
- 측정값과 기준: 60Hz 지형 걷기에서 16.7 ms를 넘는 틱 0개. 구현 뒤 측정(16384² 지형, 900틱, 16 worker, 디스크 저장과 미리 읽기 켬):
  - 틱당 4칸: 중앙값 1.00 ms, 최대 2.6 ms, 상주 블록 최대 14개.
  - 틱당 16칸: 중앙값 1.00 ms, 최대 4.8 ms, 상주 블록 최대 26개.
  - 16.7 ms를 넘은 틱은 둘 다 0개.
- 비교 화면: `go run ./cmd/matter-wake -scene terrain`
- 직접 플레이로 볼 점: Shift로 오래 이동했다 되돌아올 때 끊김이 없는지, 왕복하는 동안 화면 가장자리가 멈췄다 움직이는 곳이 없는지
