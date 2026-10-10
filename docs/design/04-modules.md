# 04 — Module / package

Tham chiếu: ADR-001 (modular monolith), ADR-009, ADR-018. Đây là cấu trúc **dự kiến** để định hướng; tên package cụ thể có thể điều chỉnh khi code nhưng ranh giới và hướng phụ thuộc phải giữ. Chưa tạo scaffold (mục 0.5).

## 1. Repo layout (đề xuất)

```
motolink/                      (monorepo)
 ├─ server/                    Go module
 │   ├─ cmd/
 │   │   ├─ server/            main: đọc config, chọn ROLES, wiring, graceful shutdown
 │   │   ├─ simclient/         công cụ mô phỏng client cho test/load (M3)
 │   │   └─ admin/             DLQ replay, tạo partition thủ công, v.v. (M5)
 │   ├─ internal/
 │   │   ├─ platform/          hạ tầng dùng chung (config, log, metrics, pg, redis, kafka, clock, id)
 │   │   ├─ identity/          user, auth, token, device, consent
 │   │   ├─ trip/              trip, membership, invitation, trip_events, state machines
 │   │   ├─ tracking/          tracking sessions, validation điểm, ingest pipeline, history, stats
 │   │   ├─ realtime/          ws conn, hub, protocol codec, presence, pub/sub bridge
 │   │   ├─ sos/               sos, recipients, receipts
 │   │   ├─ notify/            notifications, FCM sender, notifier consumer
 │   │   ├─ eventbus/          outbox, relay, topics, envelope, producer/consumer helpers
 │   │   ├─ httpapi/           router, middleware, handlers (mỏng), DTO, error mapping
 │   │   └─ jobs/              scheduler: retention, partitions, auto-end, stats
 │   ├─ db/
 │   │   ├─ migrations/        SQL (goose)
 │   │   └─ queries/           SQL cho sqlc
 │   └─ go.mod
 ├─ android/                   Gradle project (module :app)
 ├─ docs/                      prompt/, design/
 └─ deploy/                    compose, Caddyfile, .env.example, scripts
```
Một `go.mod` cho server. Repo Android cùng monorepo để contract (`docs/design`) đi cùng.

## 2. Go: module và trách nhiệm

| Package | Trách nhiệm (một câu) | Dùng bảng/kênh | Phụ thuộc vào (cho phép) |
|---|---|---|---|
| `platform` | Cấu hình, logger (redaction tọa độ), metrics registry, pool PG/Redis, client Kafka, sinh UUIDv7, clock giả để test | — | stdlib + thư viện hạ tầng |
| `identity` | Đăng ký/đăng nhập, JWT, refresh rotation, thu hồi, thiết bị/FCM token, consent | users, refresh_tokens, devices, consent_records, `ml:revoked` | `platform` |
| `trip` | Trip + membership + invitation + máy trạng thái + ghi `trip_events` (T3–T6), hàm `Authorize(user, trip, action)` **duy nhất** cho REST và WS | trips, trip_members, trip_invitations, trip_events, outbox | `platform`, `identity` (ID người dùng) |
| `tracking` | Tạo/đổi trạng thái session; validate điểm GPS; `ingest` (Lua `loc_set`, enqueue Kafka, ack aggregator); `history-writer` sink; tính thống kê; truy vấn route | tracking_sessions, location_points, `ml:loc` | `platform`, `trip` (Authorize), `eventbus` (producer) |
| `realtime` | WS Conn (read/write loop), Hub shards, outbox coalescing, protocol codec (`07`), presence (`ml:pres`, `ml:conn`), Pub/Sub bridge, replay/snapshot | `ml:*` channels | `platform`, `trip`, `tracking` (qua interface `Ingestor`), `sos` (qua interface) |
| `sos` | Tạo/hủy/giải quyết SOS, recipients, receipts, trạng thái tổng hợp | sos_events, sos_recipients | `platform`, `trip` |
| `notify` | Notifier consumer, tạo `notifications`, gửi FCM, xử lý token lỗi | notifications, devices | `platform`, `eventbus` |
| `eventbus` | Outbox (ghi trong transaction của module khác qua helper), relay, định nghĩa topic/envelope, wrapper producer/consumer + DLQ + backoff | outbox | `platform` |
| `httpapi` | Router, middleware (auth, request_id, rate limit, idempotency, recover), handlers mỏng gọi use-case của module | idempotency_keys | tất cả module domain (chỉ gọi, không chứa nghiệp vụ) |
| `jobs` | Scheduler + task: retention/downsampling, tạo partition, auto-end trip, xóa token hết hạn, dọn outbox/idempotency | nhiều bảng | domain modules qua interface nhỏ |

### Quy tắc phụ thuộc
1. `platform` không import module domain. Module domain không import `httpapi`/`realtime`.
2. Domain → domain chỉ theo mũi tên ở cột cuối; không vòng. Giao tiếp ngược chiều bằng **interface nhỏ định nghĩa ở phía người dùng** (ví dụ `realtime` định nghĩa `Ingestor` do `tracking` thỏa mãn).
3. Mỗi module có API công khai tối thiểu (file `api.go` + type); mọi thứ khác ở sub-package `internal`/không export.
4. Handler (`httpapi`, `realtime`) **không** chứa SQL/nghiệp vụ; gọi use-case.
5. SQL sinh bởi `sqlc` nằm trong package `store` của từng module (không chia sẻ truy vấn giữa module; cần dữ liệu module khác ⇒ gọi API module đó).
6. Một transaction chỉ nằm trong một module; transaction cần nhiều module (T4, T6, T8) do module `trip`/`sos` sở hữu và gọi helper `eventbus.Outbox(tx, …)`.
7. Kiểm tra bằng công cụ (ví dụ `depguard`/`go-arch-lint` hoặc test import) — chọn khi code `[CẦN XÁC NHẬN]`.

### Bản đồ role → thành phần
| Role (`ROLES`) | Khởi động |
|---|---|
| `api` | `httpapi` (REST), middleware |
| `ws` | `realtime` (Hub, ingest workers, Redis subscriber, ack aggregator, Kafka producer) |
| `worker` | `eventbus` relay, consumers (`tracking` history-writer, `notify`), `jobs` |

## 3. Android: module/package

Single module `:app` (ADR-018); package theo `12-android.md` §2. Bảng trách nhiệm:

| Package | Trách nhiệm |
|---|---|
| `app` | `Application`, điều hướng, tema, khởi tạo (DI nhẹ) |
| `core.db` | Room: entity/DAO/migration |
| `core.net` | Retrofit + OkHttp, refresh token, Idempotency-Key |
| `core.ws` | `RealtimeClient`, mã hóa/giải mã protocol, reconnect/resume, áp dụng event vào repository |
| `core.location` | `LocationSource` (Fused/Platform), `AdaptiveSampler`, bộ lọc chất lượng điểm |
| `core.sync` | `OutboxOps`, `SyncWorker`, `PendingPointsUploader` |
| `core.push` | FCM service, kênh thông báo |
| `feature.*` | Màn hình + ViewModel + Repository của từng tính năng (auth, trip, map, tracking, history, sos, settings) |

Quy tắc: ViewModel chỉ expose `StateFlow<UiState>` và nhận `UiEvent`; Repository là nơi duy nhất ghi Room từ dữ liệu mạng; `TrackingService` không chứa logic UI; không singleton giữ `Context` Activity.

## 4. Quản lý contract
- `07` (WS), `06` (REST), `05` (schema), `09` (Kafka), `08` (Redis) là **nguồn sự thật**; mã hóa/giải mã được kiểm bằng golden JSON dùng chung (thư mục `contract-tests/` ở root, cả Go và Android test đều đọc).
- Thay đổi contract ⇒ sửa tài liệu + ADR trước, rồi code.
