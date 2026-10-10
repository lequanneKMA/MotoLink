# 02 — Architecture Decision Records (ADR)

Mọi file khác tham chiếu ADR theo mã `ADR-0xx`. Muốn đổi contract: sửa ADR trước, ghi ngày và lý do ở mục *Lịch sử* cuối file.
Nhãn: `[ĐÃ XÁC MINH]`, `[GIẢ ĐỊNH]`, `[CẦN XÁC NHẬN]`, `[CHƯA XÁC MINH]` (xem `research/`).

## Bảng tổng hợp

| ADR | Quyết định | Trạng thái |
|---|---|---|
| 001 | Modular monolith, một binary Go, nhiều "role" bật/tắt bằng config | Chốt |
| 002 | Bộ thư viện Go: `net/http`, `coder/websocket`, `pgx`+`sqlc`, `goose`, `go-redis`, `franz-go`, `golang-jwt`, `slog` | Chốt (phiên bản pin khi code) |
| 003 | Xác thực: email + mật khẩu (argon2id); access JWT EdDSA ngắn hạn + refresh token xoay vòng có phát hiện tái sử dụng; Google Sign-In để sau | Chốt, `[CẦN XÁC NHẬN]` |
| 004 | Định danh & thứ tự: UUIDv7, `session_id` do server cấp, `seq` theo session, so sánh `(session_id, seq)` | Chốt |
| 005 | Pipeline vị trí: đường **live** (Redis + Pub/Sub) tách khỏi đường **bền** (Kafka → PostgreSQL); ACK = đã ghi vào Kafka | Chốt |
| 006 | Fan-out liên instance: Redis Pub/Sub, một channel mỗi trip, subscribe theo ref-count, lọc echo bằng `origin` | Chốt |
| 007 | Kafka KRaft single node, `franz-go`, at-least-once + ghi PG idempotent; không tuyên bố exactly-once | Chốt, có lối thoát |
| 008 | Transactional outbox **chỉ** cho trip events và SOS; không dùng cho location | Chốt |
| 009 | Hub: shard single-owner goroutine + outbox coalescing theo client | Chốt |
| 010 | PostgreSQL thuần (không PostGIS ở MVP); `location_points` partition theo tháng; retention + downsampling | Chốt |
| 011 | Giao thức WS: envelope `v/type/id/trip_id/ts/payload`, cursor `event_seq`, resume + snapshot | Chốt |
| 012 | Máy trạng thái: trip (`draft→active→ended→archived`, **không** có pause cấp trip), tracking session, membership | Chốt |
| 013 | Android location: Foreground Service `location` + Fused Location, fallback `LocationManager`; chấp nhận giới hạn OEM | Chốt |
| 014 | Bản đồ: MapLibre Native, nguồn tile do mình kiểm soát; không dùng tile OSM công cộng; spike M1 chọn cuối | Chốt hướng, `[CẦN XÁC NHẬN]` |
| 015 | SOS: PostgreSQL là nguồn sự thật; WS + FCM; client queue + `client_sos_id` chống trùng | Chốt |
| 016 | Quyền riêng tư mặc định & consent vị trí | Chốt, `[CẦN XÁC NHẬN]` pháp lý |
| 017 | Triển khai: Docker Compose, Caddy (TLS), Prometheus + Grafana; tracing hoãn | Chốt |
| 018 | Android: single-module, OkHttp/Retrofit, Room, WorkManager | Chốt |
| 019 | Offline-first: Room là nguồn UI; outbox cục bộ; reconcile bằng snapshot | Chốt |
| 020 | Cắt scope MVP (danh sách các thứ **không** làm) | Chốt |

### Các điểm trong prompt gốc được đánh giá lại (mục 1 của prompt yêu cầu nêu rõ)

1. **PostGIS trong Docker Compose** (prompt mục 9): không cần ở MVP → ADR-010. Mọi truy vấn MVP là "theo session, theo thứ tự `seq`", không có truy vấn không gian.
2. **Kafka cho quy mô MVP**: ở ~170 msg/s, một đường PG batch insert đơn giản (không Kafka) vẫn đáp ứng thừa. Kafka được giữ **chủ yếu vì mục tiêu học tập**, đồng thời mang lại lợi ích thật nhưng nhỏ (tách ghi PG khỏi đường live, replay). Điều này được nói thẳng ở ADR-007 kèm lối thoát `HISTORY_SINK=direct_pg`. Đây là đánh đổi có chủ đích, không phải "bắt buộc về kỹ thuật".
3. **Kafka trong 4 GB RAM** (prompt mục 2): khả thi nhưng chiếm phần lớn ngân sách; xem `13` (ngân sách RAM) và `[CHƯA XÁC MINH]` U9.
4. **"Mỗi trip subscribe Redis channel"** (prompt 5.4): đúng cho MVP, nhưng cần ref-count và có đường nâng cấp sang channel theo shard → ADR-006.
5. **Giả định `minSdk`**: prompt không cho; đề xuất 26 `[GIẢ ĐỊNH]` (ADR-013).
6. **Pause cấp trip** (ví dụ ở prompt mục 3: draft/active/paused/ended/archived): cắt `paused` cấp trip, giữ pause ở cấp tracking session và sharing → ADR-012, ADR-020.
7. **Thiết bị không có Google Play Services**: FCM không hoạt động → SOS chỉ đến được qua WS khi app đang sống. Đây là giới hạn không khắc phục được ở MVP, phải nói rõ với người dùng (ADR-013, ADR-015).

---

## ADR-001 — Modular monolith, một binary

**Context.** Một developer, một VPS 2 vCPU/4 GB. Microservices thêm chi phí vận hành (deploy, tracing, versioning nội bộ) không giải quyết vấn đề nào hiện có.

**Decision.** Một repo Go, một binary `motolink-server` gồm các package module (xem `04-modules.md`). Các "role" chạy trong cùng process, bật/tắt bằng config `ROLES=api,ws,worker`:
- `api`: REST.
- `ws`: WebSocket + Hub + Redis subscriber.
- `worker`: Kafka consumers, outbox relay, job định kỳ (retention, auto-end).
Mặc định local: bật cả ba. Khi cần scale-out thử nghiệm: chạy N instance cùng role `api,ws`, 1 instance thêm `worker` (consumer group tự chia partition nên chạy nhiều worker cũng được).

**Alternatives.** (a) Tách `realtime` thành service riêng: hợp lý khi tải WS cần scale độc lập; chưa cần. (b) Serverless: không hợp WS dài hạn.

**Consequences.** Ranh giới module phải được giữ bằng quy ước import (không import chéo `internal/<module>/...` ngoài API công khai của module) và kiểm tra bằng `go vet`/lint tùy biến (hoặc `depguard`) — quyết định công cụ lint khi code. Nếu một role cần tách sau này, ranh giới đã sẵn.

---

## ADR-002 — Bộ thư viện Go

Phiên bản mới nhất tra ngày 2026-10-10 qua Go module proxy `[ĐÃ XÁC MINH: https://proxy.golang.org, 2026-10-10]` (xem `research/sources.md`). Phiên bản pin chính xác chốt khi `go mod init`.

| Hạng mục | Chọn | Lý do chính | Bị loại |
|---|---|---|---|
| HTTP router | `net/http` stdlib (`ServeMux` với pattern method+path) | Đủ cho ~40 endpoint; ít dependency; middleware viết tay ~100 dòng | `chi` v5.3.2 (tốt, còn phát hành; chỉ chọn nếu `ServeMux` thành vướng) |
| WebSocket | `github.com/coder/websocket` v1.8.15 (2026-06-15) | API `context`-first, `Close` có handshake, đọc/ghi an toàn với context, `CloseRead`, hỗ trợ nén tùy chọn; còn phát hành | `gorilla/websocket` v1.5.3 (2024-06-14, không có bản mới > 2 năm; API không dùng context, phải tự quản deadline) |
| Kafka client | `github.com/twmb/franz-go` v1.22.1 (2026-09-27) + `kadm` v1.19.0 cho lag | Pure Go (không cgo), idempotent producer, batch/linger, consumer group, `BlockRebalanceOnPoll`/commit thủ công, hoạt động rất tích cực | `segmentio/kafka-go` (ít tính năng điều khiển commit/rebalance), `sarama` (API khó dùng đúng), `confluent-kafka-go` (cgo → khó build/cross-compile, ảnh Docker nặng) |
| DB driver | `pgx/v5` v5.11.0 + `pgxpool` | Chuẩn de-facto, hỗ trợ `COPY`, batch, `LISTEN` nếu cần | `database/sql` + `lib/pq` (cũ) |
| Truy vấn | `sqlc` v1.31.1 | SQL tường minh, type-safe, hợp học SQL; không ORM | GORM/ent (ẩn SQL, khó kiểm soát hiệu năng insert hàng loạt) |
| Migration | `goose/v3` v3.28.0, SQL thuần, chạy bằng lệnh riêng khi deploy | Đơn giản, hỗ trợ embed | `golang-migrate` v4.20.1 (tương đương; chọn một) |
| Redis | `go-redis/v9` v9.23.0 | Hỗ trợ Lua (`Script`), Pub/Sub động | — |
| JWT | `golang-jwt/jwt/v5` v5.3.1, thuật toán EdDSA | Duy trì tốt | — |
| Hash mật khẩu | `golang.org/x/crypto/argon2` (argon2id) | Khuyến nghị hiện hành | bcrypt (giới hạn 72 byte) |
| Log | `log/slog` stdlib, JSON | Không thêm dependency | zap/zerolog |
| Metrics | `prometheus/client_golang` v1.25.0 | | — |
| FCM | `firebase.google.com/go/v4` v4.22.0 (messaging) | Admin SDK chính thức | Tự gọi HTTP v1 |
| Test | `testcontainers-go` v0.44.0 cho PG/Redis/Kafka; `miniredis` chỉ cho unit test không phụ thuộc Lua/ordering | | — |
| Tracing | OpenTelemetry **hoãn** (v1.47.0 sẵn có) | Một monolith + log có `request_id`/`trip_id` đủ cho MVP | — |

Phiên bản Go: `[CẦN XÁC NHẬN]` — khuyến nghị dùng stable mới nhất khi bắt đầu; `ServeMux` pattern cần Go ≥ 1.22.

**Consequences.** Tất cả dependency "vào đây" phải qua ADR nếu thêm mới.

---

## ADR-003 — Xác thực và token

**Context.** Thị trường Việt Nam. OTP SMS có chi phí mỗi tin và thủ tục nhà mạng/brandname `[CHƯA XÁC MINH]`. Một số máy không có GMS nên Google Sign-In (Credential Manager) không dùng được ở mọi máy.

**Decision.**
- MVP: **email + mật khẩu** (argon2id). Không bắt buộc xác minh email ở MVP `[GIẢ ĐỊNH]` (cần SMTP/nhà cung cấp email — chưa chọn); giảm rủi ro bằng rate limit đăng ký/đăng nhập theo IP và email. Quên mật khẩu **chưa có ở MVP** (ghi vào `16`); thay bằng "đăng xuất mọi thiết bị" và xóa tài khoản.
- Google Sign-In: thêm sau (M6+) như phương thức thứ hai cho máy có GMS. Cột `google_sub` đã dự phòng ở schema.
- **Access token**: JWT ký EdDSA (Ed25519), TTL 15 phút, claims `sub` (user_id), `sid` (auth session id), `iat`, `exp`, `jti`. Không chứa quyền theo trip (quyền tra DB/cache mỗi lần — xem `11`).
- **Refresh token**: chuỗi ngẫu nhiên 256 bit, lưu **hash SHA-256** trong `refresh_tokens`, TTL 60 ngày trượt, **xoay vòng mỗi lần dùng**; mỗi chuỗi xoay thuộc một `family_id`. Dùng lại token đã xoay → thu hồi cả family (phát hiện bị đánh cắp).
- Thu hồi: logout (một family), logout-all (mọi family của user), đổi mật khẩu, xóa tài khoản. Access token đã phát vẫn sống tối đa 15 phút; với WS, server kiểm tra `sid` còn hợp lệ ở mỗi lần `auth.refresh` và nhận sự kiện thu hồi qua Redis channel `ml:user:<user_id>` để đóng kết nối ngay (xem `08`, `10`).

**Alternatives.** OTP SMS (chi phí, phụ thuộc nhà mạng); Google-only (loại máy không GMS); session cookie (không hợp mobile).

**Consequences.** Không có khôi phục mật khẩu ở MVP là một lỗ hổng sản phẩm thật → ghi `[CẦN XÁC NHẬN]` ở `16`: chấp nhận cho thử nghiệm kín, bắt buộc có trước khi phát hành rộng.

---

## ADR-004 — Định danh và thứ tự bản tin vị trí

**Context.** `client_ts` không đáng tin (đồng hồ lệch, người dùng đổi giờ). Cần: chống bản tin cũ ghi đè mới; chống trùng khi retry; suy ra thứ tự không phụ thuộc đồng hồ thiết bị.

**Decision.**
- ID thực thể: **UUIDv7** sinh ở app server (hoặc client cho các ID idempotency). UUIDv7 sắp xếp được theo thời gian tạo.
- `device_id`: UUID do client sinh lần cài đặt, gửi lúc đăng ký thiết bị; chỉ là nhãn, không dùng để xác thực.
- `session_id` (tracking session): **do server cấp** khi `POST /v1/trips/{id}/tracking-sessions` (UUIDv7). Vì do server phát, thứ tự giữa các session của cùng (user, trip) = thứ tự UUID.
- `seq`: số nguyên tăng đơn điệu **theo session**, bắt đầu từ 1, do client gán; mỗi điểm GPS (kể cả điểm được đưa vào hàng đợi offline) có một `seq` duy nhất và không đổi khi gửi lại.
- `client_ts`: thời điểm đo (epoch ms, theo đồng hồ thiết bị); `server_received_ts`: server gán khi nhận.
- **Quy tắc "mới hơn"**: `(session_id, seq)` so sánh từ điển — `session_id` trước, `seq` sau. Redis chỉ ghi nếu mới hơn bản đang có (Lua, xem `08`). `client_ts` **chỉ để hiển thị/tính tốc độ**, không dùng để xếp thứ tự giữa các bản tin.
- **Lệch đồng hồ**: client gửi `client_time_ms` ở `hello`; server tính `clock_offset_ms = server_now − client_time_ms` (sai số ≈ độ trễ một chiều), lưu theo kết nối và trả trong `welcome`. Server lưu `recorded_at = client_ts + clock_offset` (ghi `clamp` về `[received − 24h, received + 5s]`; ngoài khoảng → đánh dấu `suspect` và dùng `received_at`). Điểm bị coi là bất thường theo quy tắc ở `10`.
- **Khóa trùng**: khóa tự nhiên của một điểm lịch sử = `(session_id, seq)`.

**Alternatives.** Dùng `client_ts` để sắp xếp (hỏng khi đồng hồ sai); vector clock/Lamport (thừa: mỗi session có đúng một writer là thiết bị đó).

**Consequences.** Client phải lưu `seq` bền (Room) để tiếp tục sau khi process bị kill **trong cùng session**; nếu mất state thì bắt đầu session mới. Mỗi lần "bắt đầu tracking" luôn tạo session mới; resume sau pause là cùng session.

---

## ADR-005 — Pipeline vị trí: đường live tách khỏi đường bền

**Context.** Phải trả lời các câu 1–4 của prompt mục 6. Hai nhu cầu khác nhau: *độ mới* (live, chấp nhận mất) và *lịch sử* (cần bền, chấp nhận trễ vài giây).

**Decision.** Mỗi điểm GPS đến server đi **hai nhánh độc lập** sau khi qua kiểm tra hợp lệ (`10`):

```
WS location.update ─▶ validate/rate-limit ─┬─▶ [LIVE]  Redis Lua (set if newer) ─▶ local Hub fan-out + Pub/Sub ─▶ viewers   (at-most-once, drop-oldest)
                                           └─▶ [DURABLE] producer buffer ─▶ Kafka (acks=all, idempotent) ─▶ ACK về client ─▶ consumer ─▶ PG ON CONFLICT DO NOTHING   (at-least-once + idempotent sink)
```

Trả lời cụ thể:
1. **Kafka trước hay sau broadcast?** Không có thứ tự phụ thuộc: hai nhánh chạy song song. Broadcast **không** chờ Kafka (Kafka chậm/sập không làm vị trí live trễ). Kafka **không** chờ broadcast.
2. **Mức tin cậy lịch sử:** *at-least-once từ thiết bị đến PostgreSQL, với điều kiện thiết bị còn giữ điểm chưa ACK* (xem 4). Không đảm bảo nếu người dùng gỡ app/xóa dữ liệu trước khi ACK, hoặc buffer cục bộ tràn (mặc định giữ tối đa 24 giờ/20 000 điểm, tràn → downsample điểm cũ nhất, ghi metric). Đây là mục tiêu thiết kế, **không phải đảm bảo "không mất dữ liệu"**.
3. **Redis OK, Kafka lỗi:** viewer vẫn thấy vị trí live; server **không gửi ACK** cho các `seq` chưa vào Kafka; client giữ điểm trong Room và gửi lại (điểm trùng bị loại ở Redis bằng so sánh `seq` và ở PG bằng khóa `(session_id, seq)`). Producer buffer có giới hạn; đầy → không nhận thêm điểm vào nhánh bền cho client đó (không ACK) và tăng metric `kafka_enqueue_rejected`. **Kafka OK, Redis lỗi:** điểm vẫn vào lịch sử; nhánh live suy giảm (xem `08` mục "Redis mất kết nối": fan-out chỉ trong cùng instance qua Hub, snapshot tạm lấy từ bộ nhớ Hub).
4. **Process crash khi event còn trong channel/producer buffer:** *mất khỏi bộ nhớ server, nhưng chưa ACK* nên client gửi lại sau reconnect → không mất lịch sử (trong điều kiện ở mục 2). Điểm đã ACK nằm trong Kafka (replicated=1 trên single node: **không** chịu được hỏng đĩa broker — xem rủi ro ở `16`).
5. **Retry Kafka có tạo trùng không?** Idempotent producer chặn trùng do retry **trong một producer session**; nhưng gửi lại từ client sau crash vẫn có thể tạo bản ghi Kafka trùng. Do đó **tính đúng đắn dựa vào sink**: PG khóa `(session_id, seq)` + `ON CONFLICT DO NOTHING`.
6. **Consumer restart/replay:** commit offset **sau** khi batch ghi PG thành công (at-least-once). Replay chỉ chèn lại các dòng đã có → `DO NOTHING`. Consumer không bao giờ UPDATE/DELETE lịch sử.
7. **Outbox?** Không cho location (mất một điểm được chấp nhận và tự phục hồi từ client). Có cho trip events + SOS → ADR-008.
8. **Từng điểm hay lấy mẫu/batch?** **Từng điểm** vào Kafka nhưng **gom lô ở producer** (linger ~50–100 ms, nén zstd) và client có thể gửi `points[]` (tối đa 20 điểm/frame). Giảm tải bằng lấy mẫu **ở client** theo thích ứng (ADR-013) và **ở consumer** khi ghi (bỏ điểm đứng yên liên tiếp trong cùng ô 3 m trừ heartbeat mỗi 30 s) — bỏ điểm **sau khi** đã vào Kafka nên không ảnh hưởng ACK.

**Phân loại mức bảo đảm theo từng khâu**

| Khâu | Bảo đảm | Cơ chế |
|---|---|---|
| Thiết bị → server (WS) | at-least-once (client gửi lại đến khi ACK) | Room buffer + `seq` |
| Server → Redis "latest" | last-write-wins có điều kiện | Lua so `(session_id, seq)` |
| Server → viewer (live) | **at-most-once**, có thể bỏ bản cũ | Hub coalescing, Pub/Sub |
| Server → Kafka | at-least-once (+ chống trùng retry nội bộ producer) | idempotent producer, `acks=all` |
| Kafka → PG | at-least-once; hiệu quả "effectively-once" **chỉ cho lô ghi PG** nhờ khóa tự nhiên | commit sau ghi; `ON CONFLICT DO NOTHING` |
| End-to-end lịch sử | at-least-once có điều kiện (mục 2) | — |

Không có khâu nào được tuyên bố exactly-once.

**Alternatives.** (a) Chỉ Kafka rồi consumer ghi Redis: thêm trễ + phụ thuộc Kafka cho live — loại. (b) Ghi PG đồng bộ trong request: ghép nối độ trễ DB vào đường live — loại. (c) Bỏ Kafka, batch insert PG trực tiếp — xem ADR-007.

**Consequences.** Cần ACK có ngữ nghĩa rõ (`location.ack` = "đã nằm trong Kafka"), client cần Room buffer; consumer phải idempotent; cần metric "độ trễ end-to-end của lịch sử".

---

## ADR-006 — Fan-out liên instance bằng Redis Pub/Sub

**Context.** Thành viên của cùng một trip có thể kết nối tới các instance khác nhau.

**Decision.**
- Một channel mỗi trip: `ml:ps:trip:{<trip_id>}`.
- Mỗi instance giữ **một** kết nối Redis Pub/Sub, `SUBSCRIBE` channel của trip khi client đầu tiên của trip đó (trên instance này) join; `UNSUBSCRIBE` khi client cuối rời, có ref-count nằm trong shard của Hub (chủ sở hữu duy nhất → không race).
- Mỗi instance có `instance_id` (UUID lúc khởi động). Envelope chứa `origin`; subscriber bỏ qua envelope có `origin == instance_id` (đã fan-out cục bộ trực tiếp, không đi vòng Redis).
- Luôn publish, không kiểm tra số subscriber (chi phí thấp với Redis); channel không có subscriber thì bản tin bị bỏ.
- Kênh điều khiển theo user: `ml:ps:user:<user_id>` (thu hồi session, bị xóa khỏi trip → đóng kết nối/leave phòng). Instance subscribe khi user có kết nối đầu tiên.
- **Pub/Sub không bền**: mất message khi subscriber ngắt. Hệ quả được chấp nhận cho location (bản kế tiếp thay thế). Với **sự kiện bền** (trip events, SOS) client phục hồi bằng `event_seq` + REST/WS resume (ADR-011), không dựa vào Pub/Sub.
- Giới hạn chi phí: số channel = số trip có ≥ 1 client trên instance (≤ 50 ở MVP). Đường nâng cấp (khi > ~10 000 trip hoạt động): sharded channel `ml:ps:shard:<hash(trip)%N>` + Hub lọc theo trip (đánh đổi: nhận thừa).

**Alternatives.** Redis Streams cho live (bền nhưng thừa, tốn RAM); NATS/Kafka cho live (thêm độ trễ + phụ thuộc); pattern-subscribe `ml:ps:trip:*` (mọi instance nhận mọi trip — lãng phí băng thông).

---

## ADR-007 — Kafka: cấu hình và lối thoát

**Context.** Prompt yêu cầu Kafka cho event stream/lịch sử. Tải MVP (~170 msg/s đỉnh) thấp. Kafka trên 4 GB RAM là khả thi nhưng tốn.

**Decision.**
- Kafka single node, **KRaft** (không ZooKeeper), image `apache/kafka` (tag không-rc mới nhất thấy được: 4.3.1 `[ĐÃ XÁC MINH: Docker Hub, 2026-10-10]`; bản stable chính thức `[CHƯA XÁC MINH]` → pin khi code). Chế độ combined (`broker,controller`) cho 1 node. Heap `-Xms384m -Xmx512m` `[GIẢ ĐỊNH]`, phải đo ở M5/M7 (`13`).
- Topic/partition/retention/DLQ: xem `09-kafka.md`. Replication factor = 1 (single node) → **mất đĩa broker = mất dữ liệu chưa vào PG**. Chấp nhận vì PG là nơi lưu lâu dài; cửa sổ rủi ro = độ trễ consumer (mục tiêu < 10 s) cộng thời gian ngoài dịch vụ.
- Client `franz-go`; producer idempotent, `acks=all`, nén zstd, linger 50 ms; consumer commit thủ công sau khi ghi PG.
- Lối thoát: `HISTORY_SINK=kafka|direct_pg`. Với `direct_pg`, nhánh bền ghi batch trực tiếp vào PG từ một worker trong process (cùng khóa idempotent). Giữ lối thoát này **rẻ** (một interface `HistorySink`) để nếu Kafka thành gánh nặng vận hành trên VPS thì không mất sản phẩm.
- Không dùng Kafka transactions / exactly-once semantics. Lý do: sink là PG (không thuộc Kafka transaction); idempotent sink đã đủ.

**Alternatives.** Redpanda (nhẹ hơn nhưng khác license/độ quen — `[CHƯA XÁC MINH]` yêu cầu RAM); NATS JetStream; chỉ PG (`LISTEN/NOTIFY` + bảng queue). Mục tiêu học Kafka khiến chọn Kafka; nêu thẳng đây là lựa chọn học tập.

**Consequences.** ~0.5–1 GB RAM cho Kafka; vận hành thêm một thành phần; bù lại có replay, consumer độc lập (thống kê, thông báo), và bài học thật về lag/rebalance/DLQ.

---

## ADR-008 — Transactional outbox: chỉ cho trip events và SOS

**Context.** Với trip event/SOS, hai việc "ghi PG" và "phát Kafka/Pub/Sub/FCM" phải nhất quán: không được có SOS ghi DB mà không bao giờ được phân phối (hoặc ngược lại).

**Decision.**
- Cùng một transaction PG: ghi bảng nghiệp vụ (`sos_events`, `trip_members`, …) + `trip_events` (nhật ký có `event_seq` theo trip) + `outbox` (một dòng cho mỗi message cần phát).
- **Outbox relay** (role `worker`): poll `SELECT … FROM outbox WHERE published_at IS NULL ORDER BY id FOR UPDATE SKIP LOCKED LIMIT n`, produce lên Kafka (`motolink.trip-events.v1` / `motolink.sos.v1`), rồi `UPDATE published_at`. At-least-once; consumer idempotent theo `event_id`.
- Phân phối realtime của event bền: relay (hoặc ngay sau commit trong handler, best-effort) publish lên Redis channel của trip để viewer đang online nhận; viewer offline bắt kịp qua `event_seq`.
- **Location không dùng outbox** (ADR-005).

**Alternatives.** Dual write không outbox (có cửa sổ lỗi mất SOS); CDC (Debezium — quá nặng).

**Consequences.** Thêm bảng `outbox` + job dọn dẹp (xóa dòng đã publish > 7 ngày). Độ trễ phát từ outbox ~ chu kỳ poll (mặc định 200 ms; dùng `LISTEN/NOTIFY` để đánh thức tùy chọn).

---

## ADR-009 — Hub: shard single-owner + outbox coalescing

**Context.** Cần tránh data race, giới hạn bộ nhớ, và xử lý slow consumer.

**Decision.**
- **Hub chia N shard** (mặc định `N = GOMAXPROCS`), shard = `hash(trip_id) % N`. Mỗi shard là **một goroutine sở hữu duy nhất** map `trip_id → clients` và ref-count subscription. Mọi thay đổi/đọc map đi qua channel lệnh có capacity giới hạn của shard → không mutex trên đường nóng, không race.
- Mỗi client có `read loop` và `write loop` (2 goroutine) và một **outbox coalescing**:
  - hàng đợi *điều khiển/bền* (`error`, `trip.event`, `sos.event`, ack…) FIFO, capacity 64; tràn → đóng kết nối với mã `1013`/`slow_consumer` (client sẽ reconnect + resume qua `event_seq`, không mất dữ liệu bền);
  - bảng *location mới nhất theo user* (`map[user_id]LocationEvent`), ghi đè bản cũ → **drop-oldest tự nhiên**, bộ nhớ bị chặn bởi số thành viên;
  - write loop gom và gửi theo nhịp tối thiểu 250 ms (batch) hoặc ngay khi có điều khiển.
- Không tạo goroutine theo message. Số goroutine = 2/connection + hằng số (shards, subscriber, producer, consumers, relay).
- Chi tiết vòng đời, owner, điều kiện thoát: `10-concurrency.md`.

**Alternatives.** Mutex toàn cục (dễ, nhưng contention và khó suy luận khi vừa fan-out vừa đổi membership); mỗi trip một goroutine (số goroutine theo số trip, vẫn ổn nhưng khó giới hạn và khó đo); một goroutine hub duy nhất (nút cổ chai).

**Consequences.** Cần test `-race` và test goroutine-leak (`goleak`) ở mức tích hợp.

---

## ADR-010 — PostgreSQL: không PostGIS ở MVP; partition lịch sử

**Decision.**
- PostgreSQL thuần. Lưu `lat`, `lng` kiểu `double precision`, khoảng cách tính bằng haversine trong Go. PostGIS chỉ cần khi có truy vấn không gian thật (geofence, "ai trong bán kính X", map matching); hiện không có → **không thêm extension**. Ngưỡng nâng cấp: xuất hiện tính năng geofence/POI hoặc cần simplify trong SQL.
- `location_points` **partition theo tháng** của `session_month` (tháng bắt đầu session; một session luôn nằm trong một partition → khóa unique `(session_month, session_id, seq)` hợp lệ trên partitioned table). Partition được tạo trước bởi job (tháng hiện tại + 2 tháng tới).
- Vòng đời dữ liệu: điểm full-resolution giữ **30 ngày** `[GIẢ ĐỊNH]` sau khi session kết thúc; sau đó job downsample thành tuyến đơn giản hóa (Ramer–Douglas–Peucker, ngưỡng ~10 m) lưu ở `tracking_sessions.route_polyline` (encoded polyline) rồi xóa điểm thô; partition cũ hơn **90 ngày** bị `DROP`. Xem `05`, `11`.

**Alternatives.** TimescaleDB (thêm extension, license); không partition (xóa theo lô nặng, bloat).

---

## ADR-011 — Giao thức WebSocket, ACK và resume

**Decision (tóm tắt; chi tiết `07`).**
- Envelope JSON: `{"v":1,"type":"…","id":"…","trip_id":"…","ts":…,"payload":{…}}`. `id` do bên gửi sinh khi cần ACK/tương quan; event bền thêm `event_seq` (số nguyên tăng theo trip).
- Hai lớp dữ liệu: **ephemeral** (location, presence: không ACK theo từng bản tin; mất/ cũ thì bỏ) và **durable** (trip event, SOS: có `event_seq`, client ACK cursor).
- Reconnect: client gửi `hello` với `resume: [{trip_id, last_event_seq}]`; server trả `welcome` rồi với mỗi trip `trip.joined` chứa **snapshot** (vị trí mới nhất từ Redis + presence + `head_event_seq`) và replay các event bền có `event_seq > last_event_seq` (nếu quá nhiều/quá cũ → `resync.required`, client gọi REST snapshot). Snapshot không ghi đè bản mới hơn vì client áp dụng theo `(session_id, seq)` — chỉ nhận nếu mới hơn bản đang có.
- Version mismatch: `v` là integer; server hỗ trợ `v ∈ [min, max]` công bố trong `welcome`; client quá cũ nhận `error{code:"upgrade_required"}` rồi đóng `1008`; type lạ phải bị client bỏ qua (forward compatible); trường mới chỉ được **thêm**.
- Xác thực: header `Authorization: Bearer` ở HTTP upgrade (không đặt token trong URL để khỏi lọt vào access log) + `hello` trong 5 s; `auth.refresh` trước khi token hết hạn.

---

## ADR-012 — Máy trạng thái

Chi tiết bảng chuyển trạng thái ở `05-data-model.md` §Máy trạng thái.

- **Trip:** `draft → active → ended → archived`. Chỉ `owner` start/end. `ended` tự động nếu `active` quá 24 giờ không còn tracking session `active` nào `[GIẢ ĐỊNH]`. `archived` do owner hoặc job sau 30 ngày kể từ `ended`.
- **Tracking session:** `active ⇄ paused → ended`. Chỉ chủ session (hoặc server khi trip ended/ member bị xóa/ mất heartbeat quá hạn) chuyển trạng thái.
- **Membership:** `active → left` (tự rời) | `active → removed` (owner). `removed` không thể tham gia lại trip đó bằng mã (không có "reinstate" ở MVP; owner tạo trip mới nếu cần). Chủ trip không thể rời khi còn thành viên khác trừ khi `transfer-ownership`.
- **Sharing (thuộc membership, trực giao):** `sharing ⇄ paused` — đáp ứng user story "tạm ngừng chia sẻ mà không rời chuyến" (giữ trong MVP; xem ADR-016).

---

## ADR-013 — Android: nguồn vị trí, nền, và giới hạn OEM

**Decision.**
- `LocationSource` là interface với hai triển khai: **Fused Location Provider** (khi có Google Play Services) và **`LocationManager` GPS/Network** (khi không có GMS). Chọn lúc chạy bằng kiểm tra khả dụng GMS.
- Chỉ thu GPS khi người dùng chủ động bấm "Bắt đầu" → **Foreground Service** với `foregroundServiceType="location"`, kèm notification liên tục có nút "Tạm dừng"/"Kết thúc". Khai báo `FOREGROUND_SERVICE` + `FOREGROUND_SERVICE_LOCATION` `[ĐÃ XÁC MINH: S2]`.
- Quyền: xin foreground location (`ACCESS_FINE_LOCATION`, chấp nhận xấp xỉ nhưng cảnh báo tính năng giảm) → bắt đầu service trong khi app ở foreground (**FGS location không thể khởi tạo từ nền nếu thiếu `ACCESS_BACKGROUND_LOCATION`** `[ĐÃ XÁC MINH: S2]`) → **chỉ khi cần** (ví dụ tự khởi động lại tracking sau khi bị kill, hoặc nhận lệnh từ nền) mới xin `ACCESS_BACKGROUND_LOCATION` bằng luồng Android 11+ qua Settings `[ĐÃ XÁC MINH: S3]`. **Quyết định: MVP KHÔNG xin `ACCESS_BACKGROUND_LOCATION`**, vì dịch vụ luôn được khởi chạy khi người dùng đang mở app và chạy tiếp như FGS; điều này giảm đáng kể gánh nặng review chính sách của Google Play `[CHƯA XÁC MINH: U1]`. Đánh đổi: nếu process bị OEM kill, tracking **không tự phục hồi từ nền** — người dùng phải mở lại app. Quyết định này cần được kiểm chứng ở spike M1; nếu thực tế mất nền quá thường xuyên thì xem xét xin quyền nền (`16`).
- `targetSdk = 36` `[ĐÃ XÁC MINH: S1]`; `minSdk = 26` `[GIẢ ĐỊNH]`, `[CẦN XÁC NHẬN]`.
- **Khắc phục được:** giữ FGS đúng type; hướng dẫn tắt tối ưu pin/ cho phép tự khởi động; ghi điểm vào Room liên tục để mất nền không mất điểm đã ghi; tự phát hiện "khoảng trống" và hiển thị cho người dùng. **Không khắc phục được chỉ bằng retry/WorkManager:** OEM kill FGS/process (Xiaomi/Oppo/Vivo...) mà không báo; WorkManager không thể lấy GPS liên tục; máy không GMS không có FCM. Mô tả trung thực trong `12`.

---

## ADR-014 — Bản đồ

**Context.** Không được giả định tile OSM công cộng miễn phí không giới hạn (chính sách chưa xác minh được: U2). Cần xử lý hiển thị chủ quyền Việt Nam (Hoàng Sa, Trường Sa): đây là yêu cầu thực tế khi phát hành tại VN.

**Decision.**
- SDK: **MapLibre Native Android** `org.maplibre.gl:android-sdk` bản ổn định **13.6.1** `[ĐÃ XÁC MINH: S4, 2026-10-10]`. Nguồn mở, không khóa nhà cung cấp, style/tile URL cấu hình được.
- Nguồn tile: ưu tiên **tự kiểm soát** (vector tiles dựng từ dữ liệu OSM vùng Việt Nam, phục vụ từ VPS/CDN hoặc file PMTiles) vì (i) không phụ thuộc chính sách bên thứ ba, (ii) **tự thêm lớp nhãn/đường biên chủ quyền** theo quy định Việt Nam trong style, (iii) chi phí dự đoán được. Tính khả thi `pmtiles://` trong MapLibre Native và chi phí băng thông `[CHƯA XÁC MINH: U5]` → **spike M1** quyết định giữa: (A) PMTiles/tile server tự host; (B) nhà cung cấp thương mại (phải đọc ToS: U3, kiểm tra biên giới: U4).
- Attribution: hiển thị `© OpenStreetMap contributors` (ODbL) và attribution của nhà cung cấp tile/style `[GIẢ ĐỊNH]` — kiểm tra điều khoản khi chốt.
- Offline: không có trong MVP (cắt, ADR-020); cache tile của SDK ở mức mặc định. Đường nâng cấp: offline region/PMTiles tải về.
- Tiêu chí nghiệm thu bản đồ ở M1: (1) Hoàng Sa/Trường Sa hiển thị đúng thuộc Việt Nam ở mức zoom nhỏ và lớn; (2) giấy phép/ToS cho phép dùng thương mại và cho mức tải dự kiến; (3) render mượt trên máy tầm thấp.

---

## ADR-015 — SOS

**Decision.**
- Nguồn sự thật: `sos_events` + `sos_recipients` trong PostgreSQL, ghi trong cùng transaction với `trip_events` + `outbox` (ADR-008).
- Client tạo `client_sos_id` (UUID) lúc bấm; gửi qua WS (`sos.create`) hoặc REST (`POST /v1/trips/{id}/sos`, `Idempotency-Key = client_sos_id`). Server dedupe bằng `UNIQUE(user_id, client_sos_id)`; trả cùng `sos_id` cho lần gửi lại.
- Mất mạng: SOS vào bảng `pending_sos` cục bộ (Room), WorkManager + reconnect gửi lại đến khi nhận ACK; UI hiển thị trạng thái (`đang chờ gửi → đã gửi → N người đã nhận → N đã xem`).
- Phân phối: với mỗi thành viên active → (a) WS nếu online; (b) FCM high priority (payload tối thiểu: `sos_id`, `trip_id`, loại) tới mọi thiết bị có FCM token; client lấy chi tiết qua REST. Trạng thái nhận: `delivered_ws_at`, `delivered_push_at` (do server), `received_at` (client xác nhận), `seen_at` (người dùng mở).
- Retry FCM: tối đa 5 lần backoff mũ trong consumer, sau đó DLQ + trạng thái `push_failed`. FCM token không hợp lệ → xóa token.
- Giới hạn trung thực: thiết bị không GMS không nhận push; app bị force-stop không nhận push. Hiển thị cho người gửi "số người nhận được" để họ biết thực trạng.

---

## ADR-016 — Quyền riêng tư và consent

**Decision.**
- Mặc định riêng tư: vị trí chỉ lộ cho **thành viên active của trip đang `active`**; người ngoài (kể cả biết trip_id) không truy cập được; kiểm quyền ở **từng** REST handler và **từng** WS message (không tin `trip_id` từ client sau khi đã auth) → `11`.
- Ngừng thu GPS khi: người dùng bấm kết thúc/tạm dừng; trip `ended`; bị xóa khỏi trip; đăng xuất.
- Không log tọa độ ở mức mặc định (`slog` redaction; test tự động kiểm tra).
- Consent: màn hình riêng giải thích thu vị trí chính xác, mục đích, ai thấy, thời gian lưu; ghi `consent_records` (loại, phiên bản văn bản, thời điểm) trước khi bật tracking lần đầu; rút lại consent = dừng tracking + xóa theo yêu cầu.
- Tạm ngừng chia sẻ (`sharing=paused`) **nằm trong MVP**: thiết bị vẫn có thể ghi lịch sử riêng nhưng không gửi live cho người khác.
- Xóa dữ liệu: `DELETE /v1/me` xóa/ẩn danh tài khoản và dữ liệu vị trí của user (xem `05`, `11`).
- Pháp lý: Luật BVDLCN 91/2025/QH15 (hiệu lực 01/01/2026) `[ĐÃ XÁC MINH: S6]`; chi tiết điều khoản và tình trạng NĐ 13/2023 so với NĐ 356/2025 `[CHƯA XÁC MINH: S7, U7]` → `[CẦN XÁC NHẬN]` với luật sư.

---

## ADR-017 — Triển khai và quan sát

**Decision.** Docker Compose trên 1 VPS: `caddy` (TLS tự động, reverse proxy, WS), `server` (1..N), `postgres`, `redis`, `kafka`, `prometheus`, `grafana` (tùy chọn khi thiếu RAM). Migration chạy bằng service một lần. Backup PG bằng `pg_dump` định kỳ + `pg_basebackup`/WAL archiving là tùy chọn sau; ngoài VPS (object storage). Metrics theo mục 9 prompt; tracing OTel hoãn. Chi tiết `13`.

---

## ADR-018 — Kiến trúc Android

**Decision.** Một module `:app`, package theo tính năng (`auth`, `trip`, `map`, `tracking`, `sos`, `settings`, `core/{db,net,ws,sync,location}`), MVVM (ViewModel + StateFlow + Repository), Compose. HTTP: **Retrofit + OkHttp**; WebSocket: **OkHttp WebSocket** (ping tích hợp, cùng stack TLS/interceptor). Room, WorkManager, FCM. Không DI framework nặng ở đầu (Hilt/Koin: chọn khi code, `[CẦN XÁC NHẬN]`). Không đa module ở MVP. Chi tiết `12`.

---

## ADR-019 — Offline-first

**Decision.** Room là nguồn dữ liệu cho UI. Ghi ngay: điểm GPS (`pending_points`), SOS (`pending_sos`), thay đổi `sharing`. Hàng đợi đồng bộ: `outbox_ops` cho thao tác REST ghi quan trọng với `Idempotency-Key` cố định. Server snapshot được **reconcile** (áp dụng nếu mới hơn theo `(session_id, seq)` hoặc `event_seq`). Chi tiết `11`, `12`.

---

## ADR-020 — Cắt scope MVP

Không làm: trip-level pause; quên mật khẩu; Google Sign-In (M6+); offline map; chat/voice; geofence; so sánh/ chia sẻ công khai; đa thiết bị đồng thời cho cùng một session; sửa/thu hồi mã mời hàng loạt; web client; iOS; reinstate thành viên bị xóa; tracing OTel; PostGIS.
Có thể làm sớm nếu rẻ: `transfer-ownership` (cần để owner rời trip).

---

## Lịch sử

| Ngày | Thay đổi |
|---|---|
| 2026-10-10 | Bản đầu, ADR-001…020 |
