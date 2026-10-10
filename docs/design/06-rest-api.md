# 06 — REST API contract (v1)

Tham chiếu: ADR-003 (auth), ADR-004 (ID), ADR-008/011/012/015. Bảng và cột nằm ở `05`; message WS ở `07`.
Base URL: `https://<host>/v1`. JSON UTF-8. Thời gian: ISO-8601 UTC (`2026-10-10T05:00:00Z`) trong REST; WS dùng ms epoch (chủ ý, tiết kiệm băng thông).

## 1. Quy ước chung

### 1.1 Xác thực
`Authorization: Bearer <access_jwt>` trừ các endpoint đánh dấu **public**. Token hết hạn ⇒ `401 token_expired`; thu hồi (`ml:revoked:sid:*`) ⇒ `401 session_revoked`.

### 1.2 Phiên bản app
Header `X-App-Version: 0.3.1`, `X-Device-Id: <uuid>`. `GET /v1/config` (public) trả `min_app_version`, `ws_v_min/ws_v_max`, `tile_style_url`, `limits`; app cũ hơn `min_app_version` phải chặn và yêu cầu cập nhật.

### 1.3 Idempotency
Header `Idempotency-Key: <uuid>` **bắt buộc** cho các `POST` ghi quan trọng (đánh dấu **IK**). Lưu ở `idempotency_keys` theo `(user_id, key)` 24 giờ cùng `request_hash` (SHA-256 của method+path+body chuẩn hóa):
- Cùng key + cùng hash ⇒ trả **y nguyên** phản hồi đầu (kèm header `Idempotent-Replay: true`).
- Cùng key + khác hash ⇒ `422 idempotency_key_reuse`.
- Request đầu đang chạy (đồng thời) ⇒ `409 request_in_progress`.
Phản hồi `5xx` không được lưu (cho phép thử lại).

### 1.4 Lỗi (RFC 9457 dạng rút gọn)
```json
{"type":"https://motolink.app/errors/forbidden","code":"forbidden","title":"Không đủ quyền","status":403,"detail":"…","request_id":"01J…","errors":[{"field":"name","code":"too_long"}]}
```
Mã chung: `400 bad_request`, `401 unauthorized|token_expired|session_revoked`, `403 forbidden`, `404 not_found` (cũng dùng khi người gọi **không có quyền biết** tài nguyên tồn tại — tránh dò ID), `409 conflict|invalid_state|request_in_progress`, `410 gone` (mã mời hết hạn), `413`, `422 validation_failed|idempotency_key_reuse`, `429 rate_limited` (kèm `Retry-After`), `500`, `503 unavailable`.

### 1.5 Phân trang
`?limit=` (mặc định 50, tối đa 200) & `?cursor=` (opaque). Phản hồi `{"items":[…],"next_cursor":"…"|null}`.

### 1.6 Vai trò dùng trong bảng quyền
`any` (đã đăng nhập) · `member` (thành viên **active** của trip) · `owner` (chủ trip, cũng là member) · `self` (chủ tài nguyên). Mọi handler tài nguyên-trong-trip kiểm tra membership **bằng truy vấn DB/cache theo `trip_id` trong path**, không dựa vào claim.

## 2. Endpoint

Ký hiệu: **IK** = cần `Idempotency-Key`. Cột "Bảng" trỏ về `05`.

### 2.1 Auth
| Method & path | Quyền | Request | Response | Lỗi chính | IK | Bảng |
|---|---|---|---|---|---|---|
| `POST /auth/register` | public | `{email,password,display_name,device:{device_id,model,os_version,app_version,has_gms},consents:[{kind,version}]}` | `201 {user,access_token,expires_in,refresh_token}` | `409 email_taken`, `422 validation_failed` (mật khẩu yếu/ngắn, thiếu consent) | — (khóa unique email chặn tạo tài khoản thứ hai khi gửi lại; không dùng `Idempotency-Key` vì chưa có `user_id`) | users, devices, consent_records, refresh_tokens |
| `POST /auth/login` | public | `{email,password,device:{…}}` | `200 {user,access_token,expires_in,refresh_token}` | `401 invalid_credentials`, `429` | — | users, refresh_tokens, devices |
| `POST /auth/refresh` | public (có refresh token) | `{refresh_token}` | `200 {access_token,expires_in,refresh_token}` (token xoay) | `401 invalid_refresh`, `401 refresh_reused` (đã thu hồi cả family) | — | refresh_tokens |
| `POST /auth/logout` | any | `{}` (thu hồi family của `sid` hiện tại) | `204` | | — | refresh_tokens; Redis `revoked` |
| `POST /auth/logout-all` | any | `{}` | `204` | | — | refresh_tokens |
Chính sách mật khẩu: ≥ 8 ký tự, không nằm trong danh sách phổ biến nhỏ nhúng sẵn `[GIẢ ĐỊNH]`. Rate limit: login 5 lần/15 phút theo (IP+email), register 10/giờ/IP (Redis `ml:rl:*`).

### 2.2 Hồ sơ, đồng ý, thiết bị
| Method & path | Quyền | Request | Response | Lỗi | IK | Bảng |
|---|---|---|---|---|---|---|
| `GET /me` | any | — | `{user,consents,settings}` | | | users, consent_records |
| `PATCH /me` | self | `{display_name?,avatar_color?,locale?}` | `200 {user}` | `422` | — | users |
| `DELETE /me` | self | `{password}` (xác nhận) | `202 {job_id}` — bắt đầu xóa (05 §6) | `409 owner_must_transfer` | ✔ | users + dữ liệu phụ thuộc |
| `GET /me/export` | self | — | `202` rồi tải JSON (dữ liệu của chính user: hồ sơ, trip, session, điểm) | | — | nhiều bảng — `[CẦN XÁC NHẬN]` có cần ở MVP (quyền truy cập/sao chép dữ liệu) |
| `GET /me/consents` | self | — | `[{kind,version,granted_at,withdrawn_at}]` | | | consent_records |
| `PUT /me/consents/{kind}` | self | `{version,granted:true|false}` | `200` (`granted:false` với `location_processing` ⇒ server kết thúc session đang mở của user) | `422 unknown_version` | — | consent_records, tracking_sessions |
| `PUT /devices/{device_id}` | self | `{platform:"android",model,os_version,app_version,has_gms,fcm_token?}` | `200 {device}` | `409` nếu `fcm_token` thuộc user khác (chuyển quyền sở hữu: xóa token cũ) | — | devices |
| `DELETE /devices/{device_id}` | self | — | `204` (gọi khi đăng xuất) | | — | devices |

### 2.3 Trip
| Method & path | Quyền | Request | Response | Lỗi | IK | Bảng |
|---|---|---|---|---|---|---|
| `POST /trips` | any | `{name,history_visibility?}` | `201 {trip}` (status `draft`, caller là owner) | `422` | ✔ | trips, trip_members, trip_events |
| `GET /trips?status=&cursor=` | any | — | trips mà caller là member active (mặc định ẩn `archived`) | | | trips, trip_members |
| `GET /trips/{id}` | member | — | `{trip,my_membership,member_count}` | `404` | | trips, trip_members |
| `PATCH /trips/{id}` | owner | `{name?,history_visibility?}` | `200 {trip}` | `409 invalid_state` nếu `archived` | — | trips |
| `POST /trips/{id}/start` | owner | `{}` | `200 {trip}` (`draft→active`) | `409 invalid_state` | ✔ | trips, trip_events, outbox |
| `POST /trips/{id}/end` | owner | `{}` | `200 {trip}` (`draft|active→ended`; kết thúc mọi session) | `409` | ✔ | trips, tracking_sessions, trip_events, outbox |
| `POST /trips/{id}/archive` | owner | `{}` | `200 {trip}` (`ended→archived`) | `409` | ✔ | trips |
| `GET /trips/{id}/snapshot` | member | — | `{trip,members:[{user_id,display_name,color,role,sharing_state,presence,location?}],head_event_seq,my_open_session?}` | `404` | | trips, trip_members; Redis `loc`/`pres` |
| `GET /trips/{id}/events?after_seq=&limit=` | member | — | `{items:[{event_seq,id,kind,actor_id,data,created_at}],head_event_seq,complete}` (tối đa 500, trong 7 ngày) | `410 events_expired` ⇒ dùng snapshot | | trip_events |

### 2.4 Mời và tham gia
| Method & path | Quyền | Request | Response | Lỗi | IK | Bảng |
|---|---|---|---|---|---|---|
| `POST /trips/{id}/invitations` | owner (hoặc member nếu `invite_policy=members` — **MVP: chỉ owner**) | `{expires_in_s?(≤7d, mặc định 24h),max_uses?}` | `201 {id,code,expires_at,max_uses}` — `code` chỉ trả **một lần** (lưu hash) | `409 invalid_state` (trip ended/archived) | ✔ | trip_invitations |
| `GET /trips/{id}/invitations` | owner | — | danh sách (không có code), `used_count`, `revoked_at` | | | trip_invitations |
| `DELETE /trips/{id}/invitations/{inv_id}` | owner | — | `204` (set `revoked_at`) | | | trip_invitations |
| `POST /invitations/resolve` | any | `{code}` | `{trip_name,member_count,owner_name,valid:true}` (không lộ danh sách; chỉ khi hợp lệ) | `404` chung cho sai/hết hạn/thu hồi (tránh dò), `429` | — | trip_invitations, trips |
| `POST /trip-join` | any | `{code}` | `200 {trip,my_membership}` (T4 ở `05`) | `404 invalid_code`, `410 expired`, `409 trip_full|already_member|removed|invalid_state` | ✔ | trip_invitations, trip_members, trips, trip_events, outbox |
Rate limit mã mời: 10 lần sai/giờ/user + 30/giờ/IP. Mã 8 ký tự Crockford base32 (~40 bit) + hạn ngắn + giới hạn thử ⇒ xác suất đoán trúng một mã hợp lệ chấp nhận được `[GIẢ ĐỊNH — đánh giá lại ở M2]`.

### 2.5 Thành viên
| Method & path | Quyền | Request | Response | Lỗi | IK | Bảng |
|---|---|---|---|---|---|---|
| `GET /trips/{id}/members` | member | — | `[{user_id,display_name,color,role,status,sharing_state,joined_at}]` (member `active` + người đã rời nếu cần hiển thị lịch sử: `?include_left=true`) | | | trip_members, users |
| `PATCH /trips/{id}/members/me` | self (member) | `{sharing_state?,share_history?,color?}` | `200 {membership}`; đổi `sharing_state` ⇒ `trip.event member_sharing_changed` + xóa `ml:loc` nếu `paused` | `409` | — | trip_members, trip_events, outbox |
| `DELETE /trips/{id}/members/me` | self | — | `204` (`active→left`; kết thúc session mở) | `409 owner_must_transfer` | ✔ | trip_members, tracking_sessions, trip_events |
| `DELETE /trips/{id}/members/{user_id}` | owner | — | `204` (`active→removed`; đóng kết nối trip đó; kết thúc session) | `403` (không xóa chính mình/owner), `404` | ✔ | trip_members, tracking_sessions, trip_events |
| `POST /trips/{id}/transfer-ownership` | owner | `{user_id}` | `200` | `409` (đích không active) | ✔ | trip_members, trip_events |

### 2.6 Tracking session và lịch sử
| Method & path | Quyền | Request | Response | Lỗi | IK | Bảng |
|---|---|---|---|---|---|---|
| `POST /trips/{id}/tracking-sessions` | member | `{device_id}` | `201 {session:{id,status:"active",started_at,session_month}}` — `id` là `session_id` dùng cho `seq` | `409 trip_not_active`, `409 session_already_open` (trả kèm session đang mở để resume), `403 consent_required` | ✔ | tracking_sessions, trip_events |
| `GET /trips/{id}/tracking-sessions?user_id=&cursor=` | member (xem của người khác chỉ nếu `history_visibility='members'` ∧ `share_history` của họ) | — | danh sách session + thống kê | | | tracking_sessions, trip_members |
| `GET /tracking-sessions/{sid}` | self hoặc quyền xem lịch sử | — | `{session}` | `404` | | tracking_sessions |
| `POST /tracking-sessions/{sid}/pause` | self | `{}` | `200` | `409` | ✔ | tracking_sessions, trip_events |
| `POST /tracking-sessions/{sid}/resume` | self | `{}` | `200` | `409` | ✔ | tracking_sessions, trip_events |
| `POST /tracking-sessions/{sid}/end` | self | `{last_seq?}` | `200 {session}` (kích hoạt tính thống kê sau ân hạn) | `409` | ✔ | tracking_sessions, trip_events |
| `GET /tracking-sessions/{sid}/route?simplify_m=&format=polyline\|geojson` | self hoặc quyền xem lịch sử | — | tuyến đã đơn giản hóa (`simplify_m` mặc định 10) + thống kê; sau retention dùng `route_polyline` | `404`, `410 points_expired` (chỉ còn polyline) | | tracking_sessions, location_points |
| `POST /tracking-sessions/{sid}/points` | self | `{points:[{seq,client_ts,lat,lng,accuracy_m,speed_mps?,heading_deg?,altitude_m?}]}` (≤ 500 điểm) — **đường dự phòng/ xả tồn đọng** | `200 {up_to_seq,rejected:[{seq,reason}]}` — **cùng ngữ nghĩa ACK** với `location.ack` (đã vào Kafka) | `409 session_not_open`, `413`, `503 server_busy` (chưa ACK) | — (idempotent tự nhiên theo `(sid,seq)`) | Kafka → location_points |
Điểm qua REST **không broadcast live** (có thể cũ); chỉ vào nhánh bền. Điểm qua REST vẫn phải qua cùng validation (`07` §8).

### 2.7 SOS
| Method & path | Quyền | Request | Response | Lỗi | IK | Bảng |
|---|---|---|---|---|---|---|
| `POST /trips/{id}/sos` | member | `{client_sos_id,kind,message?,lat?,lng?,accuracy_m?,client_created_at?}` | `201 {sos_id,event_seq,duplicate:false}` / `200 {…,duplicate:true}` | `409 trip_not_active` | ✔ (`Idempotency-Key` = `client_sos_id`) | sos_events, sos_recipients, trip_events, outbox |
| `GET /trips/{id}/sos?status=` | member | — | danh sách SOS của trip (+ trạng thái nhận **tổng hợp**; chi tiết từng người chỉ cho người gửi) | | | sos_events, sos_recipients |
| `GET /sos/{sos_id}` | member của trip đó | — | `{sos, recipients?}` (`recipients` chỉ cho người gửi) | `404` | | sos_events, sos_recipients |
| `POST /sos/{sos_id}/receipt` | recipient | `{status:"received"\|"seen"}` | `204` | | — (idempotent tự nhiên) | sos_recipients |
| `POST /sos/{sos_id}/resolve` | người gửi hoặc owner | `{status:"resolved"\|"cancelled"}` | `200` | `409` | ✔ | sos_events, trip_events, outbox |

### 2.8 Thông báo và đồng bộ
| Method & path | Quyền | Request | Response | Lỗi | IK | Bảng |
|---|---|---|---|---|---|---|
| `GET /notifications?cursor=` | self | — | danh sách | | | notifications |
| `POST /notifications/read` | self | `{ids:[…]}` hoặc `{all:true}` | `204` | | — | notifications |
| `GET /sync/bootstrap` | self | — | `{trips:[{trip,my_membership,head_event_seq,my_open_session?}],server_time}` — gọi khi đăng nhập/ mở app sau thời gian dài; app dùng để dựng Room | | | trips, trip_members, tracking_sessions |

### 2.9 Hệ thống
Các endpoint dưới đây **không lưu bảng PostgreSQL** (có lý do ghi bên cạnh); chúng không vi phạm quy tắc "endpoint trỏ về bảng có thật".

| Path | Ghi chú |
|---|---|
| `GET /config` (public) | Xem §1.2. Đọc từ cấu hình server, không bảng |
| `POST /telemetry/client` | Quyền `any`. Body: histogram bucket ẩn danh `{location_age_buckets,ack_latency_buckets,app_version}` — **không** có tọa độ/ID chuyến. Chỉ cộng vào metric Prometheus (`product_location_age_seconds`); không lưu DB. Giới hạn 1 request/phút/thiết bị |
| `GET /ws` | Nâng cấp WebSocket, xem `07` |
| `GET /healthz` (liveness), `GET /readyz` (kiểm PG, Redis; Kafka chỉ cảnh báo) | Cổng nội bộ, không qua Caddy công khai |
| `GET /metrics` | Cổng nội bộ riêng, chỉ Prometheus |

## 3. Ma trận kiểm quyền (tổng hợp)

| Hành động | public | any | member | owner | self |
|---|---|---|---|---|---|
| register/login/refresh/config | ✔ | | | | |
| tạo trip, tham gia bằng mã, `resolve` | | ✔ | | | |
| đọc trip/members/snapshot/events/sos | | | ✔ | ✔ | |
| start/end/archive/invite/remove/transfer | | | | ✔ | |
| rời trip, đổi sharing của mình | | | | | ✔ (member) |
| tracking của mình (create/pause/resume/end/points) | | | | | ✔ (member) |
| xem route người khác | | | ✔ nếu `history_visibility='members'` ∧ họ `share_history` | ✔ (như member; owner **không** có đặc quyền xem thêm) | |
Quy tắc "Không lộ sự tồn tại": truy cập tài nguyên của trip mà caller không là member ⇒ `404`.

## 4. Các điểm cần nhấn mạnh

- Mọi `POST` đánh dấu IK được kiểm thử bằng test replay (cùng key hai lần ⇒ cùng kết quả, một lần ghi).
- Thay đổi trạng thái trip/membership/session luôn ghi `trip_events` + `outbox` trong cùng transaction (T5 ở `05`).
- Không endpoint nào trả vị trí chính xác của người khác khi trip không `active`, trừ route lịch sử theo quyền ở §2.6.
- Không log body của `/points`, `/sos`, `/tracking-sessions/*/route` (chứa tọa độ).
