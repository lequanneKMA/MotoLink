# PROGRESS — nhật ký tiến độ bộ tài liệu thiết kế MotoLink

Nhánh tài liệu: `docs/motolink-architecture`; code ở `feat/m0-foundation` → `feat/m2-trips` (remote `lequanneKMA/MotoLink`). Prompt gốc: `docs/prompt/MotoLink-planning-prompt.md` (mục 0). Trạng thái: **hoàn tất Phase A–D, Phase E đã push**, chờ chủ dự án duyệt (xem `16` §1 và `17` §1).

## Trạng thái phase

- [x] Phase A — Khảo sát repo (chỉ có `docs/prompt/`, không code/README/CLAUDE.md) + tra cứu nguồn → `research/`
- [x] Phase B — `02-adr`, `05-data-model`, `07-websocket-protocol`
- [x] Phase C — 08, 09, 10, 06, 11, 12, 13, 14, 03, 04, 01, 15, 16, 17, 00, `appendix-CLAUDE.md`
- [x] Phase D — review chéo (checklist 0.6 ghi ở cuối `00-README.md`); sửa mâu thuẫn
- [~] Phase E — commit xong trên nhánh `docs/motolink-architecture` (không merge, không PR). **Push chưa thực hiện được: repo không có remote `origin`** (`git remote -v` rỗng); chủ dự án cần thêm remote rồi `git push -u origin docs/motolink-architecture`

## Nhật ký

- 2026-10-10 Phase A: repo trống → chỉ tạo `docs/design/`, không sửa ngoài `docs/`. Môi trường chặn nhiều host (xem `research/unverified.md`). Tra được: Play targetSdk (API 36 từ 31/08/2026), FGS `location`, luồng quyền nền Android, phiên bản thư viện Go (proxy.golang.org), MapLibre Android 13.6.1 (Maven Central), tag Kafka image, Luật BVDLCN 91/2025/QH15.
- Phase B/C: viết theo thứ tự prompt 0.2; mỗi file một commit.
- Phase D: grep ADR→file, bảng↔endpoint, WS type↔sequence, TTL Redis, topic Kafka, TODO/TBD, "exactly-once". Phát hiện & sửa: kết nối WS theo user (không theo device); tính `clock_offset` ở server; offline-start cần mạng; thiếu `auth.refresh` trong sequence S2; routing outbox `sos_*`; endpoint không có bảng (`/telemetry/client`, `/config`) cần ghi chú; cụm "TODO trần" trong 15 bị grep nhầm. Mermaid: `mmdc` render 11/11 khối OK.

- 2026-10-10 Chủ dự án xác nhận: **D-02** đăng nhập cơ bản trước, Google + Facebook sau; **D-04** xin đủ quyền chạy nền (`ACCESS_BACKGROUND_LOCATION`); **D-05** giữ Kafka; **D-18** chấp nhận cửa sổ mất dữ liệu, cải thiện sau; **D-09** dùng backend miễn phí (chưa chọn nhà cung cấp → rủi ro mới R-21, tension với D-05). Đã cập nhật ADR-003/007/013/017, `05` (bỏ `google_sub`, dự phòng `auth_identities`), `11`, `12`, `13`, `15`, `16`, `17`, `00`.
- 2026-10-10 D-09 chốt **Render free**; D-04 xin quyền nền (xác nhận lại). Tra Render qua nguồn thứ cấp (render.com bị chặn) → U14. Thêm ADR-021 (deploy `direct_pg`, Kafka giữ ở dev/test), `13` §7, R-21 cụ thể, sửa roadmap M4-T05/M5-T09/M7-T04.

## Quyết định chốt sớm

Xem `02-adr.md` (bảng tổng hợp) và `00-README.md` §3. Quyết định cần chủ dự án: `16` §1 (D-01…D-19).

## Việc còn lại

Không còn việc thiết kế bắt buộc. Việc tiếp theo là của chủ dự án: duyệt/điều chỉnh các quyết định ở `16` §1, chuẩn bị máy thật cho spike M1, sau đó bắt đầu theo `17`.

## Tiến độ code (nhánh `feat/m0-foundation`)

Môi trường soạn code: không có Docker daemon; kiểm bằng PostgreSQL 16 + Redis 7 cài sẵn (`TEST_DATABASE_URL`, `TEST_REDIS_ADDR`). Go toolchain tự nâng lên 1.26 do dependency yêu cầu (Dockerfile dùng `golang:1.26`; `golangci-lint` cài sẵn build bằng Go 1.25 nên chưa chạy được local — CI dùng bản mới).

| Task | Trạng thái | Ghi chú |
|---|---|---|
| M0-T01 monorepo + CI | xong | `server/`, `deploy/`, `.github/workflows/ci.yml`, `Makefile` |
| M0-T02 compose dev | xong (chưa chạy thật) | `docker compose config` hợp lệ; **chưa `up`** vì sandbox không có Docker daemon — chạy thử trên máy bạn; tên biến `KAFKA_*` của image chưa xác minh (U9) |
| M0-T03 platform | xong | config, logx (redaction), metrics, UUIDv7 |
| M0-T04 migration | xong | `00001_baseline.sql` khớp `05`; test với PG thật |
| M0-T05 golden contract | **chưa** | |
| M0-T06 Android skeleton | xong (**chưa build được `:app` trong sandbox**) | xem mục Android bên dưới |
| M2-T01 identity | xong | register/login/refresh xoay vòng + phát hiện dùng lại/logout/logout-all, max 10 phiên, argon2id có semaphore |
| M2-T02 middleware | xong | request id, recover, access log (không body), auth + thu hồi, rate limit, lỗi problem+json, metrics |
| M2-T03 idempotency | xong | replay, 422 khác body, 409 đồng thời, không cache 5xx, theo user |
| M2-T04.. trip, invitation, member, tracking-session | xong | trips/members/invitations/sessions/route/snapshot/events, tài khoản (consent, devices, xóa, export), test phân quyền |
| M3/M4 realtime | xong | `internal/realtime`: hub shard đơn chủ, ingest, ACK cộng dồn, Redis `loc_set` (Lua), presence ZSET + janitor, Pub/Sub có phát hiện gián đoạn, resume snapshot+replay, `resync.required`; test với PG + Redis thật, 2 instance |
| M5 durable | xong (Kafka chưa chạy thật) | `internal/durable`: `PGSink` (direct_pg), `KafkaSink`/`HistoryWriter` (bisect + DLQ), outbox relay `inproc|kafka`, `POST …/points`. **Kafka không có trong sandbox**: code chỉ biên dịch + test phần logic (bisect, codec); cần chạy thử với compose. Chưa có công cụ `motolink-admin dlq` |
| M6 SOS/thông báo/jobs | xong | `internal/sos` (T8, idempotent, receipt, resolve, `sos.status`), `internal/notify` (inbox, FCM HTTP v1 — no-op nếu thiếu `FCM_CREDENTIALS_FILE`), `internal/jobs` (timeout, stats, auto-end, partition, downsample, cleanup; khóa Redis) |
| M0-T05 golden contract | xong | `server/testdata/contract/*.json` do Go test sinh (`-update`); `ContractTest` phía Android giải mã bằng chính DTO của app. Đổi field ở một bên ⇒ test đỏ |

Sai lệch có chủ đích so với tài liệu:
- Truy vấn SQL viết tay bằng `pgx` (chưa dùng `sqlc`, chưa cài); chuyển sang `sqlc` khi số truy vấn tăng.
- Test tích hợp dùng `TEST_DATABASE_URL`/`TEST_REDIS_ADDR` (tự bỏ qua nếu không đặt) thay vì testcontainers; CI cung cấp Postgres service. Redis test trong CI chưa bật.
- `POST /auth/register` không dùng `Idempotency-Key` (xem `06`).
- Bug tìm được khi test: (1) `NoopRevoker` làm phiên đã logout vẫn hợp lệ — sửa để fallback PostgreSQL; (2) khóa log `route` bị redaction nhầm là tọa độ — đổi thành `endpoint` + test hồi quy.

## Tiến độ Android (nhánh `feat/m0-foundation`, thư mục `android/`)

| Phần | Trạng thái kiểm chứng |
|---|---|
| `:logic` (JVM thuần): lấy mẫu thích ứng, lọc chất lượng, Haversine/RDP, codec WebSocket v1, `PositionKey`/reconciler, `EventCursor`, `PendingTracker` (ACK), backoff, phân loại hiện diện, máy trạng thái tracking, hướng dẫn OEM, định dạng | **41 test JVM chạy xanh** (Gradle + Kotlin 2.3.21). Golden frame lấy từ `07`. Test bắt được 1 lỗi hợp đồng thật: `v`/`supported_v` bị bỏ khi bằng giá trị mặc định (đã sửa) |
| `:app` lớp mạng (Retrofit 3 / OkHttp 5.5 / kotlinx-serialization, authenticator refresh một luồng, Keystore) | **Biên dịch được** (kiểm riêng với `android-all` API 36 + stub `TokenStore`); bắt được 1 lỗi biên dịch thật (tham số constructor không phải property) — đã sửa. Chưa chạy |
| `:app` phần còn lại: Room, TrackingService (FGS location), LocationSource (Fused + LocationManager), luồng quyền, Compose UI, MapLibre, DataStore | **Chưa biên dịch**: cần Android SDK (`dl.google.com`) và thư viện androidx/play-services (Google Maven) — đều bị chặn trong sandbox. Viết cẩn thận nhưng chắc chắn sẽ có lỗi khi sync/build lần đầu |
| Phiên bản thư viện | Kotlin 2.3.21, KSP 2.3.12, coroutines 1.10.2, retrofit 3.0.0, okhttp 5.5.0, serialization 1.11.0, MapLibre 13.6.1: **xác minh tồn tại** trên Maven Central. AGP 8.13.0, Compose BOM, Room, WorkManager, DataStore, lifecycle, activity/navigation, play-services-location: **không xác minh được** — nâng lên bản ổn định mới nhất khi sync |
| Tên package MapLibre (`org.maplibre.android.*`, `org.maplibre.geojson.*`) | `[CHƯA XÁC MINH]` |

Phạm vi M1 đã có trong code: ghi hành trình solo, dịch vụ nền, lấy mẫu thích ứng, phát hiện gián đoạn, lịch sử, bản đồ vẽ tuyến (nền trống — chưa chọn nguồn tile, D-15), luồng quyền (disclosure → vị trí → thông báo → nền), hướng dẫn OEM, đăng nhập/đăng ký (cần backend). 

Chạy lần đầu trên máy bạn: mở thư mục `android/` bằng Android Studio (cần JDK 17+ và Android SDK 36), để Gradle sync; sửa phiên bản nếu thiếu; emulator dùng `http://10.0.2.2:8080/` (đã mặc định cho debug), máy thật đặt `api.baseUrl=http://<IP-LAN>:8080/` trong `android/local.properties`. Test logic không cần SDK: `cd android && ./gradlew :logic:test` (khi chỉ có JDK, tạm bỏ `include(":app")` trong `settings.gradle.kts`).

### Android — phần đã thêm sau khi backend xong (chưa build được `:app` trong sandbox)

| Phần | Kiểm chứng |
|---|---|
| `:logic`: `TripLiveModel` (snapshot + sự kiện bền theo cursor + vị trí không lùi + SOS), `JwtExpiry`/`subject` | **52 test JVM xanh** (chạy bằng `android/scripts/test-logic.sh`, không cần SDK) |
| `:app` lớp mạng mở rộng (trips, sessions, points, SOS, notifications, devices), `TokenRefresher` dùng chung cho HTTP + WebSocket, `Idempotency-Key` tự động | **Biên dịch được** trong dự án JVM tạm (stub `TokenStore`/`BuildConfig`) + **8 test hợp đồng** giải mã JSON thật của server |
| `RealtimeClient` (OkHttp WebSocket: hello+resume, snapshot/replay, gap→resync qua REST, upload GPS theo ACK cộng dồn, SOS hàng đợi, 4401/4409/1008, backoff) | Chưa biên dịch/chạy; logic quyết định nằm trong `:logic` đã test |
| `SyncWorker`, `SessionSync`, `PointUploader` (REST dự phòng), `SosRepository`, `PushRegistrar` + `MotoLinkMessagingService` (FCM, chỉ bật khi có `app/google-services.json`) | Chưa biên dịch/chạy |
| UI: danh sách chuyến đi, tạo/nhập mã, chi tiết chuyến (bản đồ thành viên, trạng thái LIVE/STALE/LOST, mời, ghi, SOS, thông báo) | Chưa biên dịch/chạy |

Việc bạn cần làm lần đầu: sync Gradle (sửa phiên bản androidx/AGP/firebase-bom nếu thiếu), tạo Firebase project và đặt `google-services.json` vào `android/app/` nếu muốn push, chạy server (`docker compose up`) rồi thử 2 thiết bị/emulator cùng một chuyến.

### Bản đồ & chỉ đường (stack tự host)

| Phần | Kiểm chứng |
|---|---|
| Server `internal/maps`: proxy OSRM (chuẩn hóa, tự thử lại không `exclude=motorway`), tìm địa điểm theo bảng `places` (không dấu), sinh style MapLibre sáng/tối; endpoint `/v1/route`, `/v1/geocode`, `/v1/map/style/{theme}` | Test Go xanh (OSRM giả, Postgres thật với `unaccent`) + test hợp đồng JSON |
| `:logic` `Nav.kt`: giải mã polyline6, bám tuyến, còn lại/điểm rẽ kế, phát hiện lệch tuyến, lời thoại tiếng Việt | 18 test JVM |
| `deploy/maps`: `prepare.sh` (OSRM, planetiler, Noto fonts, osm2pgsql → `search.sql`), Martin, PostGIS, compose profile `maps` | `docker compose config` hợp lệ. **Chưa chạy** (không có Docker daemon/Geofabrik trong sandbox): tên image, cờ lệnh, `exclude=motorway`, tên font Martin cần kiểm lần đầu. **PostGIS chưa được test** (sandbox không có extension), chỉ phần SQL import |
| Android: `NavScreens`, `NavMap`, `NavViewModel`, `Speech`, `MapConfigRepository` | Chưa biên dịch; DTO được test hợp đồng |
