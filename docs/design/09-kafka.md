# 09 — Kafka: topic, partition key, schema, consumer, retry, DLQ

Tham chiếu: ADR-005 (pipeline), ADR-007 (cấu hình & lối thoát), ADR-008 (outbox). Phiên bản Kafka/Docker image: `apache/kafka`, tag không-rc mới nhất thấy được 4.3.1 `[ĐÃ XÁC MINH: Docker Hub, 2026-10-10]`; bản stable chính thức `[CHƯA XÁC MINH]` (U9) → pin tag cụ thể khi triển khai. Client: `franz-go` v1.22.1.

## 1. Vai trò, không trùng Redis

| Dữ liệu | Redis | Kafka | PostgreSQL |
|---|---|---|---|
| Vị trí mới nhất | ✔ (live) | | |
| Lịch sử điểm | | ✔ (đệm 3 ngày, replay) | ✔ (nguồn sự thật dài hạn) |
| Presence | ✔ | | |
| Trip event / SOS | Pub/Sub (realtime, không bền) | ✔ (phân phối bất đồng bộ: FCM, thông báo) | ✔ (nguồn sự thật, + outbox) |

## 2. Topic

Naming: `motolink.<miền>.<phiên bản>`; DLQ: `<topic>.dlq`. `auto.create.topics.enable=false`; topic tạo bằng script khởi tạo (init container) để cấu hình tường minh.

| Topic | Producer | Partition key | Partitions | Retention | Consumer group(s) | DLQ |
|---|---|---|---|---|---|---|
| `motolink.location.v1` | Server (role `ws`/`api`: nhánh bền ingest điểm GPS) | `session_id` | 3 | 3 ngày (`retention.ms=259200000`) + `retention.bytes=2 GiB`/partition `[GIẢ ĐỊNH]` | `cg.history-writer` | `motolink.location.v1.dlq` (14 ngày) |
| `motolink.trip-events.v1` | Outbox relay (role `worker`) | `trip_id` | 3 | 7 ngày | `cg.notifier` | `motolink.trip-events.v1.dlq` (14 ngày) |
| `motolink.sos.v1` | Outbox relay | `trip_id` | 3 | 14 ngày | `cg.notifier` | `motolink.sos.v1.dlq` (30 ngày) |

Cấu hình broker (single node, KRaft combined): `process.roles=broker,controller`, `num.partitions=3`, `default.replication.factor=1`, `min.insync.replicas=1`, `offsets.topic.replication.factor=1`, `transaction.state.log.replication.factor=1`, `transaction.state.log.min.isr=1`, `log.retention.check.interval.ms=300000`, heap `KAFKA_HEAP_OPTS="-Xms384m -Xmx512m"` `[GIẢ ĐỊNH]`.

**Vì sao `session_id` làm key (location):** cùng session vào cùng partition ⇒ locality và lô liền nhau. **Đúng đắn không phụ thuộc thứ tự** vì sink có khóa `(session_id, seq)` và `ON CONFLICT DO NOTHING`; do đó việc tăng số partition sau này (đổi ánh xạ key→partition) không gây sai dữ liệu, chỉ thay đổi locality tạm thời. **Vì sao `trip_id` (trip-events/sos):** consumer thông báo cần xử lý event của một trip theo thứ tự `event_seq` tương đối (member_joined trước trip_started…); dùng thứ tự partition làm tối ưu, nhưng consumer vẫn idempotent theo `event_id`.

3 partition là đủ: throughput đỉnh ~170 msg/s ≪ năng lực một partition; 3 cho phép tối đa 3 consumer song song khi chạy nhiều instance, `[GIẢ ĐỊNH]` đo lại ở M5.

## 3. Event envelope

Giá trị: JSON UTF-8 (đơn giản, dễ debug; kích thước chấp nhận được với nén zstd ở producer). Header Kafka:

| Header | Ý nghĩa |
|---|---|
| `event_id` | UUID duy nhất của event (location: `<session_id>:<seq>` dạng chuỗi) — khóa idempotency |
| `type` | `location.point`, `trip.event`, `sos.created`, … |
| `schema` | số nguyên, hiện `1` |
| `produced_at` | ms epoch |
| `traceparent` | tùy chọn (dành cho OTel sau này) |

Quy tắc tiến hóa schema: chỉ **thêm** field tùy chọn; consumer bỏ qua field lạ; đổi nghĩa/xóa field = topic `…v2` + chạy song song. Có thể chuyển sang Protobuf/Avro + registry nếu cần (`[GIẢ ĐỊNH]`: chưa cần).

### 3.1 `motolink.location.v1` — `location.point`
```json
{
  "trip_id": "018f-trip", "user_id": "018f-user",
  "session_id": "018f-sess", "session_month": "2026-10-01",
  "seq": 813,
  "recorded_at": 1760072402890, "received_at": 1760072403100,
  "lat": 11.94052, "lng": 108.45841,
  "accuracy_m": 7.5, "speed_mps": 9.4, "heading_deg": 136.0, "altitude_m": 1480.0,
  "suspect": false
}
```
`session_month` do server điền (từ thông tin session đã nạp khi xác thực) để consumer không phải tra cứu.
Kích thước ≈ 250–300 B JSON; nén lô zstd ≈ 80–110 B/điểm `[GIẢ ĐỊNH]`.

### 3.2 `motolink.trip-events.v1` — `trip.event`
```json
{ "event_id":"E46","trip_id":"T","event_seq":46,"kind":"member_joined",
  "actor_id":"U3","data":{"user_id":"U3","display_name":"Hà"},"created_at":1760072420000 }
```

### 3.3 `motolink.sos.v1` — `sos.created`, `sos.resolved`
```json
{ "event_id":"S-9","trip_id":"T","event_seq":47,"sos_id":"S-9","sender_id":"U1",
  "kind":"breakdown","message":"Hỏng xe","lat":11.94,"lng":108.45,"created_at":1760072430050 }
```
Tọa độ trong SOS là dữ liệu nhạy cảm: chỉ gửi cho consumer cần; log của consumer không in tọa độ.

## 4. Producer

- `franz-go`, producer **idempotent** (mặc định), `RequiredAcks = AllISR`, nén `zstd`, `ProducerLinger = 50 ms`, `ProducerBatchMaxBytes` ~ 256 KiB, `MaxBufferedRecords` ~ 10 000 và `MaxBufferedBytes` ~ 16 MiB `[GIẢ ĐỊNH]`.
- **Không chặn read loop**: enqueue không chặn (API không chặn của franz-go, tên API xác minh khi code). Hàng đợi đầy ⇒ điểm bị từ chối tạm (`server_busy`), **không ACK** ⇒ client giữ lại và gửi lại (ADR-005 mục 3).
- Callback hoàn tất (`err==nil`) ⇒ ghi `(session_id, seq)` vào `AckAggregator` (mutex nhỏ theo session); aggregator định kỳ ≤ 1 s phát `location.ack` với `up_to_seq` liên tục (xem `10`).
- Lỗi producer không retry được (lỗi cấu hình/ quyền, `MESSAGE_TOO_LARGE`) ⇒ metric `kafka_produce_errors_total{reason}`, điểm vào bảng "dead letter cục bộ" (log có `session_id,seq`, **không** có tọa độ) và vào `rejected: server_error` để client xóa — mất điểm này được chấp nhận và đo.

## 5. Consumer

Chung: franz-go consumer group; `DisableAutoCommit`; commit **sau** khi side-effect thành công; `BlockRebalanceOnPoll` để không mất quyền partition giữa chừng xử lý; `FetchMaxWait` ~ 250 ms; khi shutdown: ngừng poll → xử lý xong lô → commit → rời group.

### 5.1 `cg.history-writer` (topic `location.v1`)

- Gom lô: tối đa **500 record hoặc 200 ms**.
- Ghi PG: một câu lệnh `INSERT INTO location_points (...) SELECT … FROM unnest(...) ON CONFLICT (session_month, session_id, seq) DO NOTHING` (hoặc `COPY` vào bảng tạm + insert; chọn khi benchmark ở M5). Một transaction cho cả lô.
- Lọc trước khi ghi: bỏ điểm đứng yên liên tiếp (cùng ô 3 m, `speed<0.5 m/s`) trừ heartbeat mỗi 30 s; điểm `suspect` vẫn ghi (cờ `suspect=true`). Lọc **sau** khi nhận từ Kafka nên không ảnh hưởng ACK/đúng đắn.
- Commit offset sau `COMMIT` PG.
- **Idempotency:** khóa tự nhiên `(session_month, session_id, seq)`. Replay/retry/trùng do client gửi lại ⇒ `DO NOTHING`. Consumer **không** UPDATE/DELETE lịch sử.
- **Restart/replay:** offset chưa commit ⇒ xử lý lại ⇒ không trùng (khóa) ⇒ lịch sử không bị sửa. Nếu reset offset về đầu (thao tác vận hành), kết quả giống nhau (idempotent), chỉ tốn thời gian.
- **Lỗi hạ tầng (PG down/timeout):** retry vô hạn với backoff mũ 0.5 s → 30 s, không DLQ, không commit; lag tăng ⇒ cảnh báo. Producer phía server vẫn nhận (đệm Kafka 3 ngày); nếu PG lỗi > 3 ngày thì mất dữ liệu quá retention — ghi ở `16`.
- **Lỗi dữ liệu (record hỏng, vi phạm CHECK/NOT NULL):** nếu cả lô lỗi do dữ liệu ⇒ **bisect** (chia đôi) đến record gây lỗi, thử riêng 3 lần, rồi đẩy vào `motolink.location.v1.dlq` kèm header `dlq.reason`, `dlq.source` (topic/partition/offset), `dlq.attempts`, rồi tiếp tục.
- **Điểm thuộc session không tồn tại:** kiểm tra mềm (cache session id đã biết, miss thì tra PG); không tồn tại ⇒ DLQ (reason `unknown_session`).

### 5.2 `cg.notifier` (topics `trip-events.v1`, `sos.v1`)

- Với mỗi event: xác định người nhận (member active tại thời điểm xử lý trừ actor), `INSERT INTO notifications … ON CONFLICT (user_id,type,ref_event_id) DO NOTHING` — nếu insert mới ⇒ cần push (idempotency chống push trùng khi replay; xem lưu ý FCM bên dưới).
- Push (FCM, `firebase.google.com/go/v4`): `sos.*` ưu tiên **high**, payload dữ liệu tối thiểu (`sos_id`, `trip_id`, `kind`) — client lấy chi tiết qua REST (không đặt tọa độ vào payload push; U8 giới hạn kích thước chưa xác minh). `trip_started` high-ish/normal; `member_joined` normal hoặc bỏ push nếu thiết bị đang online.
- Cập nhật `sos_recipients.delivered_push_at/push_status`, `notifications.push_status/push_attempts`.
- Outbox định tuyến theo `kind`: `sos_created` và `sos_resolved` → `motolink.sos.v1`; mọi kind còn lại → `motolink.trip-events.v1`.
- Sự kiện `tracking_ended` có `data.end_reason='timeout'` (OEM kill, `12` §5.3) ⇒ notifier tạo thông báo loại `tracking_interrupted` cho **chủ session** (push `system`, chỉ máy có FCM token).
- **Retry:** lỗi tạm (5xx, quota) ⇒ tối đa 5 lần trong tiến trình, backoff 1 s → 30 s có jitter; hết lần ⇒ ghi `push_status='failed'` và gửi record vào DLQ. Lỗi vĩnh viễn (token `UNREGISTERED`/`INVALID`) ⇒ xóa `devices.fcm_token`, không retry.
- **FCM không đảm bảo exactly-once:** nếu tiến trình chết sau khi FCM nhận nhưng trước khi ghi DB, replay có thể gửi trùng ⇒ payload chứa `ref_event_id`; client **dedupe theo `ref_event_id`** (Room). Trùng được chấp nhận; mất không.
- Throughput nhỏ ⇒ xử lý tuần tự theo partition, song song giới hạn 8 goroutine cho lời gọi FCM trong một lô.

### 5.3 Tính thống kê hành trình — không phải Kafka consumer

`distance_m`, `duration_s`, `point_count`, `route_polyline` được tính bởi **job định kỳ trong DB** (mỗi 30 s): chọn session `status='ended'` có `stats_version=0` hoặc có `point_count` khác số điểm hiện có và `ended_at < now() − 60 s` (chờ điểm đến muộn), đọc điểm theo `seq`, bỏ điểm `suspect`, tính haversine, simplify RDP, `UPDATE … stats_version = stats_version+1`. Idempotent (tính lại từ đầu mỗi lần). Lý do **không** dùng consumer Kafka: cần *tập điểm đầy đủ* và độ trễ chờ điểm muộn; consumer tăng dần (incremental) sẽ sai khi điểm đến không theo thứ tự. Giữ quyết định này trừ khi lịch sử tăng đến mức job quét thành vấn đề.

## 6. Dead-letter topic

- Record DLQ = bản gốc (key, value, headers) + headers: `dlq.reason`, `dlq.source.topic`, `dlq.source.partition`, `dlq.source.offset`, `dlq.attempts`, `dlq.at`.
- Không có consumer tự động đọc DLQ. Công cụ vận hành `motolink-admin dlq {inspect,replay,drop}` (sẽ viết ở M5; replay = produce lại về topic gốc sau khi sửa nguyên nhân). Ngoại lệ: không replay record `location` nếu session đã bị xóa.
- Cảnh báo: `kafka_dlq_records_total` tăng > 0 ⇒ thông báo.

## 7. Giám sát lag

`kadm` (franz-go) lấy `committed offset` và `end offset` mỗi 15 s ⇒ gauge `kafka_consumer_lag{group,topic,partition}` và `kafka_consumer_lag_seconds` (xấp xỉ bằng tuổi record chưa commit). Ngưỡng cảnh báo `[GIẢ ĐỊNH]`: `history-writer` lag > 5 000 record hoặc > 30 s trong 2 phút; `notifier` (sos) > 5 s. Metric producer: `kafka_produce_duration_seconds`, `kafka_produce_errors_total`, `kafka_buffered_records`.

## 8. Câu trả lời các câu hỏi bắt buộc (mục 6 prompt)

Xem ADR-005 mục 1–8 (đã trả lời đầy đủ; file này triển khai chi tiết 4, 5, 6, 8: crash ↔ chưa ACK; retry ↔ khóa tự nhiên; replay ↔ DO NOTHING; batch ↔ linger + `points[]`). Hai điều cần nhấn mạnh:

- **Không có exactly-once.** Producer idempotent chỉ chặn trùng *trong một vòng đời producer*; tính đúng đắn của lịch sử đến từ sink idempotent.
- **Cửa sổ mất dữ liệu có thật:** (a) điểm đã ACK nhưng chưa vào PG khi đĩa broker hỏng (RF=1); (b) client mất dữ liệu trước ACK; (c) PG hỏng lâu hơn retention Kafka. Mỗi cái có metric/cảnh báo và nằm trong `16`.

## 9. Lối thoát không Kafka

`HistorySink` có hai triển khai: `kafkaSink` (mặc định) và `directPGSink` (đệm trong bộ nhớ → batch insert cùng câu lệnh). Giữ cùng ngữ nghĩa ACK (`up_to_seq` sau khi `COMMIT`). Bật bằng `HISTORY_SINK=direct_pg`. Dùng khi: VPS thiếu RAM, Kafka gây sự cố vận hành.
