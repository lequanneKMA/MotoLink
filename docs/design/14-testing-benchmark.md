# 14 — Chiến lược kiểm thử và kế hoạch benchmark

Tham chiếu: `07`, `08`, `09`, `10`, `11`, `13`. Các mục tiêu dưới đây là **mục tiêu kiểm chứng**, không phải giả định chắc chắn đạt (prompt mục 12). Kết quả đo được ghi vào `docs/design/bench/` (thư mục tạo khi có kết quả) và dùng để sửa ADR.

## 1. Các lớp test

| Lớp | Phạm vi | Công cụ | Chạy khi |
|---|---|---|---|
| Unit | Domain: máy trạng thái trip/session/membership; so sánh `(session_id, seq)`; validate điểm GPS; haversine/RDP; token bucket; ackAggregator (`up_to_seq` liên tục + rejected); mã mời; JWT/refresh rotation (logic) | `go test` | Mỗi commit |
| Concurrency | Hub shard, outbox coalescing, slow consumer, shutdown, ref-count subscribe | `go test -race`, `goleak`, `-count=N`, `-shuffle` | Mỗi commit (race) |
| Integration (hạ tầng thật) | PG (migration, transaction T1–T9, `ON CONFLICT`, partition, retention), Redis (Lua `loc_set`, presence, Pub/Sub), Kafka (producer/consumer, DLQ, restart) | `testcontainers-go` (PG/Redis/Kafka thật; **không** dùng miniredis cho Lua/ordering) | Mỗi PR / nightly |
| Authorization matrix | Mỗi REST endpoint × vai trò (nonmember, member, owner, removed, self khác) ⇒ mã kỳ vọng; mỗi WS message × vai trò | Table-driven test sinh từ bảng `06` §3 và `07` §7 | Mỗi PR |
| WebSocket protocol | Contract tests: golden JSON cho mọi `type` (`07`); hello timeout; version mismatch; unknown type; frame quá lớn; token hết hạn (4401); thay thế kết nối (4409) | Client test Go (`coder/websocket`) | Mỗi PR |
| Idempotency | Mỗi endpoint IK: replay cùng key ⇒ cùng kết quả, một lần ghi; key khác hash ⇒ 422; đồng thời ⇒ 409 | Integration | Mỗi PR |
| Android unit/repository | Repository + Room (in-memory), reconciliation, `AdaptiveSampler`, outbox_ops state machine, áp dụng snapshot có điều kiện | JUnit + Room test, Turbine (Flow) `[CHƯA XÁC MINH: U11]` | Mỗi PR |
| Android UI | Compose UI tests cho luồng chính (login, tạo/tham gia trip, SOS pending) | Compose test | Nightly |
| E2E nhiều client | N client mô phỏng (Go) + 1–2 app thật; kịch bản trip đầy đủ qua 2 instance | Công cụ nội bộ `cmd/simclient` (viết ở M3) | Nightly/trước release |
| Load | 50 → 500 kết nối, cố định tải | `simclient` (+ `k6` WebSocket nếu tiện `[CHƯA XÁC MINH]`) | M3/M4/M7 |
| Chaos/recovery | Kill Redis, kill Kafka, mạng chập chờn, restart process, đầy ổ | Script `docker compose kill/pause`, `tc netem` | M4/M5/M7 |
| Field Android | Pin/giờ, độ chính xác, mất nền do OEM | Máy thật | M1, M6, M7 |
| Bảo mật | `govulncheck`, quét secret, kiểm log không có tọa độ, test brute-force mã mời | CI | Mỗi PR |

## 2. Tiêu chí kiểm chứng ban đầu → test cụ thể

| # | Mục tiêu (prompt mục 12) | Test | Đạt khi |
|---|---|---|---|
| A1 | 50 thiết bị mô phỏng gửi GPS định kỳ | `simclient -n 50 -interval 3s -trips 5 -duration 30m` | Không `error` bất thường; `ws_location_dropped_total` ≈ 0; p95 fan-out ≤ 250 ms `[GIẢ ĐỊNH]`; ACK `up_to_seq` tiến đều |
| A2 | 500 kết nối (benchmark mở rộng) | `simclient -n 500 -trips 50` trên máy riêng (không cùng VPS) | Ổn định ≥ 30 phút; CPU/RAM trong ngân sách `13` §4.3; không rò rỉ goroutine (số `go_goroutines` ổn định) |
| A3 | 2 Go instance fan-out qua Redis | Kết nối người gửi vào instance 1, viewer vào instance 2 | Viewer nhận mọi điểm mới nhất ≤ 300 ms p95; không nhận echo trùng; ngắt Redis ⇒ suy giảm theo `08` §5 rồi tự hồi phục + `resync.required` |
| A4 | Reconnect đồng bộ snapshot | Ngắt mạng client 30 s – 10 phút (mô phỏng), trong lúc đó sinh event bền và vị trí | Sau reconnect: không lùi vị trí; không thiếu event (`event_seq` liên tục) hoặc nhận `resync.required`; điểm chưa ACK được gửi lại và không trùng trong PG |
| A5 | Slow consumer không làm hàng đợi tăng vô hạn | Client đọc 1 B/s / dừng đọc | Bộ nhớ mỗi client bị chặn (≤ cap + `members−1` điểm); `ctrl` tràn ⇒ đóng `1013`; instance không tăng RAM; client khác không bị chậm |
| A6 | Disconnect/shutdown không còn goroutine vô chủ | `goleak` sau test; SIGTERM giữa tải | `go_goroutines` trở về baseline; mọi kết nối nhận `closing`; thoát ≤ 25 s |
| A7 | Kafka consumer restart xử lý lại an toàn, không ghi trùng | Gửi M điểm; kill `history-writer` giữa lô, khởi động lại; lặp 20 lần; thêm một lần reset offset | `SELECT count(*)` = M duy nhất `(session_id,seq)`; `history_rows_conflicted_total` > 0 chứng tỏ có replay; không lỗi |
| A8 | Crash khi dữ liệu còn trong channel | `kill -9` server giữa tải; client gửi lại | Mọi điểm client đã gửi nhưng chưa ACK cuối cùng xuất hiện trong PG sau khi client reconnect; điểm đã ACK không mất khi Kafka còn sống |
| A9 | Đo p95/p99, throughput, CPU, RAM, dropped, lag | Dashboard Grafana + báo cáo `simclient` | Có số liệu cho từng kịch bản, lưu lại |

## 3. Các test then chốt cho tính đúng đắn

### 3.1 Thứ tự và bản tin cũ (`08` §4.1)
- Gửi `(S1, 5)` rồi `(S1, 3)` ⇒ Redis giữ 5; chỉ `5` được phát.
- Gửi `(S1, 9)` rồi `(S2, 1)` với `S2 > S1` ⇒ Redis giữ `(S2,1)` (session mới thay cũ).
- Gửi `(S2, 1)` rồi `(S1, 100)` ⇒ giữ `(S2,1)`.
- 100 goroutine ghi `seq` ngẫu nhiên cùng key ⇒ kết quả = max; (chạy với Redis thật).
- Snapshot đến muộn với vị trí cũ hơn bản đã có ở client ⇒ không ghi đè (test ở Android repository và ở `simclient`).

### 3.2 Idempotent sink (`09` §5.1)
- Chèn cùng lô hai lần ⇒ số hàng không đổi.
- Lô có điểm hỏng ⇒ bisect, chỉ điểm hỏng vào DLQ, phần còn lại ghi đủ.
- PG down giữa chừng ⇒ retry, không commit offset, không mất.

### 3.3 ACK
- `up_to_seq` chỉ tiến khi mọi seq nhỏ hơn đã giải quyết; điểm `rejected` không chặn tiến.
- Kafka down ⇒ không có ACK; client gửi lại; Kafka up ⇒ ACK đuổi kịp; PG không trùng.

### 3.4 Authorization
- Thành viên bị xóa: mọi `location.event`/`trip.event` ngừng đến nhanh (≤ 1 s); gửi `location.update` với `trip_id` cũ ⇒ `error forbidden`.
- Người không thuộc trip gọi mọi endpoint của trip ⇒ `404`.
- Mã mời hết hạn/hết lượt/thu hồi ⇒ lỗi đúng; brute-force bị `429`.

### 3.5 Offline (Android)
- Tắt mạng 10 phút khi tracking ⇒ `pending_point` tăng; bật lại ⇒ gửi, xóa theo `up_to_seq`; không trùng.
- SOS offline ⇒ `pending_sos`; online ⇒ một SOS duy nhất (kể cả ứng dụng bị kill giữa lúc gửi).
- Kill app giữa chừng ⇒ `seq` không bị cấp trùng sau khi mở lại (transaction Room).

### 3.6 Không log tọa độ
Chạy toàn bộ e2e với tọa độ mẫu độc nhất (ví dụ `11.123456`), sau đó `grep` log của mọi container ⇒ không có kết quả.

## 4. Kế hoạch benchmark

### 4.1 Môi trường (ghi vào mọi báo cáo)
| Mục | Giá trị cần ghi |
|---|---|
| Máy chủ thử | VPS Ubuntu 2 vCPU / 4 GB (cấu hình thật của sản phẩm), nhà cung cấp, vùng, phiên bản kernel, Docker, image tag/digest |
| Máy tải | **Máy riêng** (không cùng VPS), ≥ 4 vCPU, cùng khu vực mạng; ghi RTT tới VPS |
| Mạng | Đo RTT/băng thông; thêm kịch bản `tc netem` (độ trễ 100–300 ms, mất gói 1–5 %) để mô phỏng 4G |
| Dữ liệu mẫu | Quỹ đạo giả lập đi trên tuyến thật (GPX công khai hoặc tự sinh); tốc độ 5–20 m/s; dừng xen kẽ; 10 người/trip; session 1–3 giờ |
| Cấu hình | Toàn bộ `.env`, tham số PG/Redis/Kafka, `GOMEMLIMIT`, `GOMAXPROCS`; Go version |
| Độ lặp | Mỗi kịch bản ≥ 3 lần, báo cáo trung vị + khoảng; có bước khởi động 2 phút không tính |
| Thời lượng | 30 phút ổn định (soak thêm 4 giờ ở M7) |

### 4.2 Kịch bản
| ID | Kịch bản | Biến | Đầu ra |
|---|---|---|---|
| B1 | Baseline ingest | 50 / 200 / 500 thiết bị, 3 s | throughput, p50/p95/p99 `ingest_process_seconds`, `fanout_latency_seconds`, CPU/RAM |
| B2 | Fan-out theo kích thước trip | trip 2 / 5 / 10 / 15 người, tổng 500 thiết bị | latency vs kích thước trip; băng thông ra |
| B3 | 2 instance | tách người gửi/ viewer | thêm độ trễ do Pub/Sub; CPU Redis |
| B4 | Slow consumers | 5 % / 20 % client chậm | tác động lên client khác; tỉ lệ đóng 1013; RAM |
| B5 | Reconnect storm | Ngắt tất cả 500 client rồi kết nối lại (jitter vs không) | thời gian hồi phục; tải PG/Redis; số `resync.required` |
| B6 | Kafka/PG | `history-writer` với batch 100/500/1000 và `INSERT unnest` vs `COPY` | hàng/s, p99 `COMMIT`, WAL/s, kích thước/hàng thực tế (kiểm `13` §4.2) |
| B7 | Chaos | Kill Redis 60 s; kill Kafka 5 phút; restart server giữa tải; PG chậm (`pg_sleep`/giới hạn IO) | Hành vi suy giảm đúng `08` §5, `09`; không mất điểm được ACK; thời gian hồi phục |
| B8 | Soak | 4 giờ, 200 thiết bị | rò rỉ RAM/goroutine/FD; kích thước PG/Kafka theo ngày so với dự báo |
| B9 | Capacity thật | Tăng dần đến khi vỡ ngân sách | Số kết nối tối đa trên 2 vCPU/4 GB; nút thắt đầu tiên (CPU, RAM, băng thông, PG) |

### 4.3 Công cụ đo
`simclient` (tự viết, Go) ghi latency đầu-cuối bằng cách nhúng timestamp (đồng hồ cùng máy tải) vào `client_ts`; Prometheus + Grafana; `pprof` (CPU, heap, goroutine) mở ở cổng nội bộ; `go test -bench` cho vi benchmark (Lua `loc_set`, encode `location.event`, coalescing); `pg_stat_statements` (bật) cho truy vấn; `kafka-consumer-groups.sh`/`kadm` cho lag; `docker stats`. Báo cáo ghi sai lệch đo (đồng hồ, GC).

## 5. Test thực địa Android
| Chỉ số | Cách đo | Thiết bị |
|---|---|---|
| Pin tiêu hao/giờ | Pin 100 % → chạy 60 phút tắt màn hình, 3 s/lần (xe/đi bộ giả lập hoặc di chuyển thật); đọc `batterystats`/`dumpsys battery` | ≥ 3 máy: Xiaomi/Oppo, Samsung, máy tầm thấp; so sánh hồ sơ lấy mẫu (3 s vs 5 s vs thích ứng) |
| Độ chính xác | So sánh với tuyến chuẩn (đi quanh đường đã biết, GPS ngoài nếu có); thống kê `accuracy_m`, lệch tuyến | Như trên, trong nhà/ngoài trời/đô thị dày |
| Mất nền do OEM | Chạy ≥ 60 phút màn hình tắt, máy trong túi, có/không bật bỏ tối ưu pin; ghi thời điểm service chết, tỉ lệ điểm mất | **Ít nhất một Xiaomi/Oppo và một Samsung** (M1) |
| Sống sót sau vuốt Recents | Vuốt app khi đang tracking | Như trên |
| Máy không GMS | `LocationManager` + không FCM | Một máy hoặc giả lập không GMS |
| Mạng yếu | Chế độ máy bay 5 phút, vùng sóng yếu | |
Ngưỡng nghiệm thu: chốt sau M1 (`16`).
