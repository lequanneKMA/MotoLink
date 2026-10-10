# 07 — WebSocket protocol (v1)

Tham chiếu: ADR-004 (thứ tự), ADR-005 (pipeline), ADR-009 (Hub), ADR-011 (resume). Chi tiết goroutine ở `10`; Redis ở `08`; sequence ở `03`.
Endpoint: `GET /v1/ws` (HTTP upgrade, TLS qua Caddy). Payload JSON UTF-8, một message mỗi frame text.

## 1. Envelope

```json
{
  "v": 1,
  "type": "location.update",
  "id": "01J...uuid",
  "trip_id": "018f...uuid",
  "ts": 1760072400123,
  "event_seq": 42,
  "payload": { }
}
```

| Field | Kiểu | Quy tắc |
|---|---|---|
| `v` | int | Phiên bản giao thức, bắt buộc. Hiện là `1` |
| `type` | string | Bắt buộc. Dạng `nhóm.hành_động` |
| `id` | string(uuid) | Bên gửi sinh khi muốn **tương quan/ACK** (bắt buộc cho `sos.create`; tùy chọn các loại khác). Server dùng làm `ref` trong `ack`/`error` |
| `trip_id` | uuid | Bắt buộc với message thuộc một trip; server **luôn kiểm tra quyền theo `trip_id` này mỗi message** (xem §7) |
| `ts` | int64 ms epoch | Thời điểm bên gửi tạo envelope. Server **không** dùng `ts` của client để sắp thứ tự |
| `event_seq` | int64 | Chỉ có ở message **bền** server→client (`trip.event`, `sos.event`). Tăng liền mạch theo trip |
| `payload` | object | Theo từng `type` |

Giới hạn: kích thước frame ≤ 16 KiB (`location.update` ≤ 20 điểm ≈ 4 KiB); vượt → đóng `1009`.

## 2. Danh mục message

Cột "Loại": **E** = ephemeral (mất/cũ thì bỏ, không ACK theo bản tin); **D** = durable (có `event_seq`, client ACK cursor); **C** = control.

### Client → Server

| `type` | Loại | Cần ACK/phản hồi | Ghi chú |
|---|---|---|---|
| `hello` | C | `welcome` hoặc `error`+đóng | Message **đầu tiên**, trong 5 s sau upgrade |
| `auth.refresh` | C | `ack` | Gửi token mới trước khi token cũ hết hạn |
| `trip.join` | C | `trip.joined` | Đăng ký nhận realtime của một trip (nếu `hello.resume` chưa bao gồm) |
| `trip.leave` | C | `ack` | Hủy đăng ký (không rời membership) |
| `location.update` | E (nhưng điểm vào nhánh bền) | `location.ack` (gộp) | Tối đa 20 điểm/frame |
| `sos.create` | D | `ack` (có `sos_id`) | `id` bắt buộc; gửi lại cùng `client_sos_id` là idempotent |
| `sos.receipt` | C | `ack` | Xác nhận đã nhận/đã xem SOS |
| `event.ack` | C | không | Cursor đã xử lý: `{event_seq}` theo `trip_id` |
| `ping` | C | `pong` | Tùy chọn (đo RTT/keepalive); heartbeat chính là WS ping của server |

### Server → Client

| `type` | Loại | Ghi chú |
|---|---|---|
| `welcome` | C | Trả lời `hello` hợp lệ |
| `trip.joined` | C | Snapshot ban đầu + head cursor |
| `location.event` | E | Batch vị trí mới nhất của các thành viên (đã coalesce) |
| `location.ack` | C | Cumulative ACK nhánh bền (ADR-005) |
| `presence.event` | E | online/offline của thành viên |
| `trip.event` | D | Sự kiện bền: lifecycle trip, thành viên, tracking |
| `sos.event` | D | SOS mới (cùng không gian `event_seq`) |
| `sos.status` | E | Tổng hợp trạng thái nhận/xem gửi cho **người gửi** SOS (nguồn sự thật vẫn là REST) |
| `ack` | C | Phản hồi thành công cho message có `id` |
| `error` | C | Lỗi (có thể kèm `ref`) |
| `pong` | C | |
| `resync.required` | C | Client phải lấy snapshot qua REST |
| `closing` | C | Báo trước khi server đóng (shutdown, slow consumer) |

## 3. Payload mẫu

### 3.1 Kết nối và xác thực

HTTP upgrade: `Authorization: Bearer <access_jwt>` (không đưa token vào URL). Nếu sai/hết hạn → `401` trước khi upgrade.

`hello` (C→S):
```json
{"v":1,"type":"hello","id":"c1","ts":1760072400000,"payload":{
  "device_id":"018f...", "app_version":"0.3.1", "os":"android-14",
  "client_time_ms":1760072400000,
  "supported_v":[1],
  "resume":[{"trip_id":"018f-trip","last_event_seq":41}]
}}
```

`welcome` (S→C):
```json
{"v":1,"type":"welcome","ts":1760072400050,"payload":{
  "conn_id":"018f...", "user_id":"018f...",
  "server_time_ms":1760072400050,
  "clock_offset_ms":50,
  "v_min":1,"v_max":1,
  "heartbeat":{"server_ping_interval_s":20,"pong_timeout_s":10},
  "limits":{"max_frame_bytes":16384,"max_points_per_frame":20}
}}
```
`clock_offset_ms = server_now − client_time_ms` (sai số ≈ độ trễ một chiều; chấp nhận). Server lưu theo kết nối để quy đổi `client_ts` (ADR-004). Client dùng để hiệu chỉnh đồng hồ hiển thị.

`auth.refresh`:
```json
{"v":1,"type":"auth.refresh","id":"c9","payload":{"access_token":"eyJ..."}}
```
Server kiểm tra `sub` khớp, `sid` chưa bị thu hồi, trả `ack`. Quá hạn token + 30 s ân hạn mà chưa refresh → đóng `4401`.

### 3.2 Join / snapshot

`trip.join` (C→S): `{"v":1,"type":"trip.join","id":"c2","trip_id":"T","payload":{"last_event_seq":41}}`

`trip.joined` (S→C):
```json
{"v":1,"type":"trip.joined","trip_id":"T","ts":1760072400100,"payload":{
  "trip":{"status":"active","name":"Đà Lạt 3N2Đ"},
  "head_event_seq":45,
  "replay":{"from_seq":42,"count":4,"complete":true},
  "members":[
    {"user_id":"U1","display_name":"Minh","color":"#E53935","role":"owner",
     "sharing_state":"sharing","presence":"online",
     "location":{"session_id":"S1","seq":812,"lat":11.9404,"lng":108.4583,
                 "accuracy_m":8.0,"speed_mps":9.2,"heading_deg":135,
                 "server_ts":1760072399000}},
    {"user_id":"U2","display_name":"Lan","color":"#1E88E5","role":"member",
     "sharing_state":"paused","presence":"online","location":null}
  ]
}}
```
Sau `trip.joined`, server gửi tiếp `replay.count` message `trip.event`/`sos.event` có `event_seq` từ `from_seq` đến `head_event_seq` (nếu `complete=false` — quá nhiều hoặc quá cũ — server không replay mà gửi `resync.required`).

`location: null` khi thành viên không chia sẻ (`sharing_state='paused'`), chưa có điểm, hoặc điểm đã hết TTL. **Server không bao giờ gửi vị trí của thành viên đang `paused`.**

### 3.3 Location

`location.update` (C→S):
```json
{"v":1,"type":"location.update","trip_id":"T","ts":1760072403000,"payload":{
  "session_id":"S1",
  "points":[
    {"seq":813,"client_ts":1760072402890,"lat":11.94052,"lng":108.45841,
     "accuracy_m":7.5,"speed_mps":9.4,"heading_deg":136,"altitude_m":1480.0}
  ]
}}
```
Quy tắc: `seq` tăng nghiêm ngặt trong frame; điểm gửi lại giữ nguyên `seq`. `points` rỗng bị từ chối.

`location.ack` (S→C), gộp ≤ 1 lần/giây/session hoặc sau mỗi lô Kafka thành công:
```json
{"v":1,"type":"location.ack","trip_id":"T","payload":{
  "session_id":"S1","up_to_seq":820,
  "rejected":[{"seq":815,"reason":"low_accuracy"}]
}}
```
Ngữ nghĩa: mọi `seq ≤ up_to_seq` thuộc một trong hai: **đã nằm trong Kafka** (acks=all) hoặc **bị từ chối vĩnh viễn** (liệt kê trong `rejected`, client xóa khỏi hàng đợi). Điểm bị từ chối với lý do tạm thời (`rate_limited`, `server_busy`) **không** nằm trong `up_to_seq` — `up_to_seq` chỉ tiến tới khi mọi seq nhỏ hơn đều đã giải quyết. Client xóa khỏi Room mọi điểm `≤ up_to_seq` của session đó.

`location.event` (S→C):
```json
{"v":1,"type":"location.event","trip_id":"T","ts":1760072403200,"payload":{
  "points":[
    {"user_id":"U1","session_id":"S1","seq":813,"lat":11.94052,"lng":108.45841,
     "accuracy_m":7.5,"speed_mps":9.4,"heading_deg":136,"server_ts":1760072403100}
  ]
}}
```
Mỗi `user_id` xuất hiện tối đa một lần/batch (coalesce). Client áp dụng điểm **chỉ nếu** `(session_id, seq)` lớn hơn bản đang giữ cho user đó (so sánh từ điển) — nên snapshot cũ hoặc event muộn không thể ghi đè vị trí mới hơn.

### 3.4 Presence

```json
{"v":1,"type":"presence.event","trip_id":"T","payload":{"user_id":"U2","state":"offline","at":1760072410000}}
```
`state ∈ {online, offline}`. "Mất cập nhật vị trí" (stale) **không** là presence: client suy ra từ tuổi của `server_ts` điểm cuối (ngưỡng hiển thị mặc định 30 s khi `online`, 90 s thì cảnh báo "mất tín hiệu"; xem `12`). Server phát `offline` khi presence key hết TTL hoặc kết nối cuối đóng (ADR `08`).

### 3.5 Sự kiện bền

`trip.event` (S→C): `event_seq` tăng liền mạch theo trip.
```json
{"v":1,"type":"trip.event","trip_id":"T","event_seq":46,"ts":1760072420000,"payload":{
  "event_id":"E46","kind":"member_joined","actor_id":"U3",
  "data":{"user_id":"U3","display_name":"Hà"}
}}
```
`kind` hợp lệ (v1): `trip_created`, `trip_started`, `trip_ended`, `trip_archived`, `member_joined`, `member_left`, `member_removed`, `member_sharing_changed`, `ownership_transferred`, `tracking_started`, `tracking_paused`, `tracking_resumed`, `tracking_ended`, `sos_resolved` (và `sos_created`, được gửi dưới dạng message `sos.event`). Client gặp `kind` lạ phải bỏ qua nhưng **vẫn** tiến cursor.

`event.ack` (C→S): `{"v":1,"type":"event.ack","trip_id":"T","payload":{"event_seq":46}}` — client gửi sau khi lưu vào Room (gộp, ≤ 1/s). Server dùng để (a) không replay lại sau resume nhanh, (b) metric độ trễ. **Không** bắt buộc cho tính đúng đắn (cursor thật nằm ở client `last_event_seq`).

### 3.6 SOS

`sos.create` (C→S):
```json
{"v":1,"type":"sos.create","id":"7d4f-client-sos-id","trip_id":"T","ts":1760072430000,"payload":{
  "client_sos_id":"7d4f-client-sos-id","kind":"breakdown","message":"Hỏng xe",
  "lat":11.94,"lng":108.45,"accuracy_m":12,"client_created_at":1760072429000
}}
```
`ack` (S→C): `{"v":1,"type":"ack","payload":{"ref":"7d4f-client-sos-id","ok":true,"result":{"sos_id":"S-9","event_seq":47,"duplicate":false}}}`. Gửi lại: `duplicate:true`, cùng `sos_id`.

`sos.event` (S→C, tới mọi member active trừ người gửi):
```json
{"v":1,"type":"sos.event","trip_id":"T","event_seq":47,"ts":1760072430100,"payload":{
  "sos_id":"S-9","sender_id":"U1","sender_name":"Minh","kind":"breakdown","message":"Hỏng xe",
  "lat":11.94,"lng":108.45,"accuracy_m":12,"created_at":1760072430050
}}
```
`sos.receipt` (C→S): `{"v":1,"type":"sos.receipt","id":"c20","trip_id":"T","payload":{"sos_id":"S-9","status":"received"}}` với `status ∈ {received, seen}`; `ack` trả về.

`sos.status` (S→C, chỉ gửi người gửi): `{"payload":{"sos_id":"S-9","total":8,"received":5,"seen":2,"push_failed":1}}`.

### 3.7 Điều khiển

`error` (S→C):
```json
{"v":1,"type":"error","payload":{"ref":"c2","code":"forbidden","message":"Bạn không thuộc chuyến đi này","retryable":false}}
```
Mã lỗi: `bad_request`, `unauthorized`, `token_expired`, `forbidden`, `not_found`, `invalid_state` (trip chưa/ không còn `active`), `session_paused`, `session_unknown`, `rate_limited` (kèm `retry_after_ms`), `payload_too_large`, `upgrade_required`, `server_busy`, `internal`.

`resync.required`: `{"v":1,"type":"resync.required","trip_id":"T","payload":{"reason":"gap_too_large"}}` → client gọi `GET /v1/trips/{id}/snapshot` (REST) rồi `trip.join` lại với `last_event_seq` mới.

`closing`: `{"v":1,"type":"closing","payload":{"reason":"server_shutdown","retry_after_ms":3000}}` rồi đóng `1001`.

`ping`/`pong`: `{"type":"ping","id":"p1"}` ↔ `{"type":"pong","payload":{"ref":"p1","server_time_ms":…}}`.

## 4. Mã đóng kết nối

| Code | Ý nghĩa | Client làm gì |
|---|---|---|
| 1000 | Đóng bình thường (client logout) | — |
| 1001 | Server shutdown/restart | Reconnect với jitter |
| 1008 | Vi phạm chính sách (hello sai, `upgrade_required`) | Không retry nếu `upgrade_required`; hiển thị cập nhật app |
| 1009 | Frame quá lớn | Bug client; không retry ngay |
| 1013 | Slow consumer / quá tải | Reconnect backoff |
| 4401 | Token hết hạn / bị thu hồi | Refresh token qua REST; nếu refresh thất bại → đăng nhập lại |
| 4403 | Không còn quyền (bị xóa khỏi trip…) cho kết nối này | Cập nhật UI, không retry trip đó |
| 4409 | Bị thay thế bởi kết nối mới của cùng `user` (một kết nối/user, xem `08` §4.2) | Không reconnect tự động nếu do chính app mở hai kết nối |
| 4429 | Rate limit nghiêm trọng | Backoff dài |

Reconnect: backoff mũ 1 s → 30 s có jitter ±50 %, reset sau kết nối ổn định 60 s.

## 5. Heartbeat và phát hiện kết nối chết

- Server gửi WS **ping** mỗi 20 s; không có pong trong 10 s → đóng. Client (OkHttp) cũng đặt `pingInterval` 25 s để giữ NAT mobile và phát hiện chết từ phía client. Ping/pong của giao thức không đi qua envelope.
- `presence` online được làm mới theo từng pong (Redis TTL 45 s, xem `08`).
- App-level `ping`/`pong` là tùy chọn (đo RTT cho `clock_offset`/metric).

## 6. Reconnect và resume

Thuật toán (client):
1. Mở WS; gửi `hello` với `resume = [(trip_id, last_event_seq)]` cho mỗi trip đang theo dõi (`last_event_seq` đọc từ Room).
2. Nhận `welcome`; với mỗi trip nhận `trip.joined` (snapshot) rồi các event bù `event_seq > last_event_seq`.
3. **Áp dụng snapshot có điều kiện:** với mỗi `member.location` trong snapshot, chỉ ghi nếu `(session_id, seq)` mới hơn bản đang có trong Room/UI; ngược lại bỏ. Với event bền: bỏ nếu `event_seq ≤ last_event_seq` (idempotent); nếu `event_seq > last_event_seq + 1` (lỗ hổng) → coi như mất, gọi `GET …/events?after_seq=` hoặc snapshot REST.
4. Gửi lại các điểm GPS chưa ACK (Room `pending_points`) bằng `location.update` (hoặc REST `POST …/points` nếu tồn đọng > 200 điểm); gửi lại `pending_sos`.
5. Tiếp tục tracking nếu session còn `active` (kiểm tra bằng `GET /v1/tracking-sessions/{sid}` nếu nghi ngờ).

Thuật toán (server) khi `hello.resume` có trip: kiểm tra membership active → join phòng (Hub) → đọc snapshot Redis + presence → đọc `trip_events WHERE seq > last` (tối đa 500, giới hạn thời gian 7 ngày) → gửi.

**Tính chất cần đạt:** không bao giờ lùi vị trí hiển thị của một thành viên; không bỏ sót event bền (hoặc nhận `resync.required`); event bền được áp dụng đúng một lần về mặt hiệu quả (idempotent theo `event_seq`).

## 7. Authorization theo từng message

Mỗi message sau `hello` đi qua cùng pipeline: (1) đã xác thực & token chưa quá hạn; (2) nếu có `trip_id` → tra membership (cache trong Hub shard, vô hiệu hóa khi nhận control event; cache miss → PG) phải `active` và (cho `location.update`) trip `active` và `sharing_state='sharing'`; (3) `session_id` phải thuộc `(user, trip)` và đang `active`. Không bao giờ suy quyền từ lần join trước.

| Message | Điều kiện |
|---|---|
| `hello` | JWT hợp lệ; `device_id` thuộc user (hoặc tạo) |
| `trip.join` / resume | member active (trip ở bất kỳ trạng thái trừ `archived`) |
| `location.update` | member active ∧ trip `active` ∧ session `active` thuộc user ∧ `sharing_state` bất kỳ (nếu `paused`: **vẫn nhận vào nhánh bền** nhưng **không** broadcast live/Redis) |
| `sos.create` | member active ∧ trip `active` |
| `sos.receipt` | là recipient của SOS đó |
| `event.ack`, `trip.leave`, `ping` | đã `hello` |

Khi membership đổi (rời/bị xóa): control channel `ml:ps:user:<id>` báo → Hub shard loại client khỏi phòng; nếu bị xóa → gửi `error{forbidden}` rồi `trip.event`; kết nối vẫn sống cho các trip khác.

## 8. Validation điểm GPS (ngữ nghĩa)

| Kiểm tra | Hành vi |
|---|---|
| `lat∈[-90,90]`, `lng∈[-180,180]`, không NaN/Inf | Sai → `rejected: invalid_coordinates` |
| `(0,0)` chính xác | `rejected: null_island` |
| `accuracy_m` thiếu hoặc > 200 | `rejected: low_accuracy` (ngưỡng cấu hình) |
| `speed_mps` > 70 hoặc < 0 | Đánh `suspect`, **vẫn lưu** lịch sử, **không** broadcast |
| Nhảy: khoảng cách/Δt từ điểm trước cùng session > 90 m/s | `suspect` như trên |
| `seq` ≤ `seq` đã nhận của session | Trùng/ cũ: không broadcast; **vẫn coi đã xử lý** (đưa vào `up_to_seq`) |
| `client_ts + offset` ngoài `[now−24h, now+5s]` | Dùng `received_at` làm `recorded_at`, đánh `suspect` |
| Tần suất | Token bucket mỗi session: 2 điểm/s trung bình, burst 40; vượt → `rate_limited` (tạm, không vào `up_to_seq`) |
| Mỗi kết nối | 40 message/s burst 80; vượt liên tục → `4429` |

## 9. Version mismatch

- `hello.supported_v` là danh sách; server chọn `max(supported_v ∩ [v_min,v_max])`. Rỗng → `error{upgrade_required}` + đóng `1008`.
- Chỉ **thêm** field/`type`/`kind`; client bỏ qua cái lạ. Xóa/đổi nghĩa = tăng `v`.
- REST có header `X-Min-App-Version` trong phản hồi `/v1/config` để app tự yêu cầu cập nhật (`06`).
- Server giữ hỗ trợ `v-1` tối thiểu 90 ngày sau khi phát hành `v` `[GIẢ ĐỊNH]`.

## 10. Phủ message ↔ sequence (checklist 0.6)

Mỗi `type` xuất hiện trong `03-architecture-diagrams.md` ở sơ đồ ghi trong cột. Những loại không có sơ đồ riêng có giải thích.

| `type` | Sơ đồ (`03`) |
|---|---|
| `hello`, `welcome`, `trip.join`, `trip.joined`, `event.ack` | S2 Reconnect |
| `auth.refresh` | S2 (nhánh token hết hạn) |
| `location.update`, `location.ack`, `location.event` | S1 Location, S4 Persistence |
| `presence.event` | S1 (presence nhánh), S2 |
| `trip.event`, `resync.required` | S2, S4 |
| `sos.create`, `sos.event`, `sos.receipt`, `sos.status`, `ack` | S3 SOS |
| `error`, `closing`, `ping`, `pong`, `trip.leave` | Không có sơ đồ riêng: là message điều khiển một bước, không có luồng nhiều thành phần; hành vi mô tả ở §3–§5 |
