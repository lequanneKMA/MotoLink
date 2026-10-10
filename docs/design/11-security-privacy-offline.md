# 11 — Security, quyền riêng tư, pháp lý, đồng bộ offline

Tham chiếu: ADR-003, ADR-015, ADR-016, ADR-019; `06` (quyền từng endpoint), `07` §7 (quyền từng message WS), `05` §6 (xóa dữ liệu).
**Không phải tư vấn pháp lý.** Mọi khẳng định pháp lý có nhãn; cần luật sư xác nhận trước phát hành.

## 1. Mô hình đe dọa (rút gọn)

| # | Mối đe dọa | Tài sản | Biện pháp chính |
|---|---|---|---|
| T1 | Người ngoài trip đọc vị trí / đoán `trip_id` | Vị trí | Kiểm membership **mỗi request/message**; `404` thay `403`; UUIDv7 không phải bí mật nhưng không đủ để truy cập |
| T2 | Đoán/brute-force mã mời | Quyền vào trip | Mã ~40 bit, hạn mặc định 24 h, giới hạn lượt, rate limit theo user+IP, `resolve` không lộ thông tin thừa, thu hồi được |
| T3 | Đánh cắp refresh/access token | Phiên | Access 15 phút; refresh xoay vòng + phát hiện tái sử dụng (thu hồi family); lưu bằng Keystore ở Android; TLS |
| T4 | Replay/ gửi trùng (SOS, tham gia, bắt đầu session) | Toàn vẹn | `Idempotency-Key`, `client_sos_id`, khóa `(session_id, seq)` |
| T5 | Giả vị trí / spam | Chất lượng dữ liệu | Validate (phạm vi, tốc độ, nhảy), token bucket, `suspect`. **Không chống được GPS giả có chủ đích** (mock location) — chấp nhận, ghi ở `16` |
| T6 | Thành viên bị xóa vẫn nghe được vị trí qua WS đã mở | Vị trí | Control channel `membership_changed` ⇒ Hub loại khỏi phòng ngay; authz mỗi message |
| T7 | Rò rỉ tọa độ qua log/metrics/crash report | Vị trí | Không log tọa độ (redaction ở `slog` Handler + test); metrics không có label chứa `user_id`/`trip_id` cardinality cao hoặc tọa độ; crash report (nếu dùng) lọc |
| T8 | Truy cập trái phép DB/Redis/Kafka | Tất cả | Chỉ mạng Docker nội bộ, không publish cổng ra Internet; mật khẩu mạnh qua `.env`/secret; firewall chỉ 80/443/SSH |
| T9 | DoS WS (nhiều kết nối, frame lớn, chậm) | Khả dụng | Giới hạn kết nối/IP & tổng (cấu hình), kích thước frame, hello timeout 5 s, token bucket, ngắt slow consumer, giới hạn ở Caddy |
| T10 | Mất/ lộ backup | Dữ liệu | Backup mã hóa (age/gpg) trước khi rời VPS; khóa tách biệt |
| T11 | Lộ secret ký JWT | Phiên | Khóa Ed25519 trong secret, hỗ trợ xoay khóa (`kid`), JWKS nội bộ nhiều khóa |
| T12 | Lợi dụng FCM token/ thông báo | Quyền riêng tư | Payload push tối thiểu, không chứa tọa độ/ tên đầy đủ; token gắn user, chuyển quyền sở hữu khi đăng nhập user khác |

## 2. Xác thực và token

- Mật khẩu: argon2id (tham số theo chuẩn hiện hành, chốt khi code bằng benchmark trên VPS: mục tiêu ~100–250 ms mỗi băm), so sánh thời gian hằng, không báo "email không tồn tại" riêng (trả `invalid_credentials` chung); đăng ký có thể lộ email tồn tại (`409 email_taken`) — chấp nhận ở MVP, giảm bằng rate limit.
- Access JWT: EdDSA, header `kid`; claims `sub`, `sid`, `jti`, `iat`, `exp` (15 phút), `aud=motolink-api`. Không chứa quyền theo trip.
- Refresh: opaque 256-bit, hash SHA-256 trong DB; xoay mỗi lần; tái sử dụng ⇒ thu hồi `family_id` + `ml:revoked:sid:*` cho các `sid` liên quan + đóng WS. Giới hạn song song: tối đa 10 family/user (cũ nhất bị thu hồi).
- Thu hồi: `logout`, `logout-all`, đổi mật khẩu, xóa tài khoản ⇒ ghi `revoked_at` + `SET ml:revoked:sid:<sid>` + publish `ml:ps:user:<id> revoke_session`.
- WebSocket: JWT trong header lúc upgrade; kiểm lại ở `hello`; token hết hạn ⇒ đóng `4401` sau ân hạn 30 s; `auth.refresh` làm mới.
- Không dùng token trong query string (tránh lọt vào log Caddy/proxy).

## 3. Authorization — đối chiếu từng bề mặt

Nguyên tắc: **một hàm kiểm quyền duy nhất theo (user, trip, hành động)** dùng chung cho REST và WS, không sao chép logic.

| Bề mặt | Nơi định nghĩa | Ghi chú |
|---|---|---|
| Từng REST endpoint | `06` cột "Quyền" và §3 | Test bảng-điều-khiển: mỗi endpoint × vai trò (nonmember/member/owner/self/removed) ⇒ mã trả về kỳ vọng (xem `14`) |
| Từng WS message | `07` §7 | Mỗi message qua `authorize(user, trip_id, type)`; membership cache trong Hub vô hiệu hóa bằng `membership_changed` |
| Redis snapshot/ Pub/Sub | Chỉ truy cập từ code server; client không nói chuyện trực tiếp Redis | Channel `ml:ps:trip:*` chỉ nhận từ server |
| Kafka | Mạng nội bộ; không expose | Không có ACL đặc biệt ở MVP (single tenant, một ứng dụng) `[GIẢ ĐỊNH]` |
| Quyền xem lịch sử | `06` §2.6, `trips.history_visibility`, `trip_members.share_history` | Mặc định: thành viên xem tuyến của nhau **trong trip**; mỗi người có thể tắt `share_history` |
| Sau khi trip `ended` | Chỉ xem route theo quy tắc trên; vị trí live biến mất (DEL Redis) | |

## 4. Quyền riêng tư vị trí

1. **Chỉ thu khi người dùng bắt đầu** (ADR-013); dừng khi kết thúc/pause/ trip ended/ bị xóa khỏi trip/ đăng xuất; notification foreground luôn hiện khi đang thu.
2. **Chia sẻ có kiểm soát**: `sharing_state=paused` ⇒ server không broadcast, không lưu Redis `loc`, snapshot trả `location:null`. (Thiết bị có thể vẫn ghi lịch sử riêng nếu người dùng bật "ghi hành trình riêng"; điểm đó vào nhánh bền nhưng chỉ chủ session xem được nếu `share_history=false`.)
3. **Giảm độ chính xác**: người dùng chọn "xấp xỉ" (Android 12+) ⇒ cho phép nhưng cảnh báo tính năng giảm `[ĐÃ XÁC MINH: S3]` (background cũng chỉ xấp xỉ nếu foreground là xấp xỉ).
4. **Dữ liệu tối thiểu**: không thu danh bạ, không thu vị trí khi không có session; `altitude` tùy chọn.
5. **Không log tọa độ**: `slog` handler loại field tên `lat|lng|latitude|longitude|coords|points|route` ở mọi mức; test CI chạy luồng đầy đủ rồi `grep` log để chắc chắn không có tọa độ mẫu; chế độ debug tọa độ chỉ bằng biến môi trường tường minh `DEBUG_LOG_COORDS=true` và bị cấm ở production (cảnh báo khi khởi động).
6. **Lưu trữ & xóa**: xem `05` §6 (30/90 ngày, downsample, xóa theo yêu cầu).
7. **Truy cập quản trị**: nhà phát triển không có công cụ xem vị trí người dùng; truy vấn thủ công vào bảng điểm chỉ khi xử lý sự cố và có ghi chú; `[CẦN XÁC NHẬN]` chính sách nội bộ.

## 5. Pháp lý Việt Nam (không phải tư vấn pháp lý)

| Mục | Nội dung thiết kế | Nhãn |
|---|---|---|
| Luật áp dụng | **Luật Bảo vệ dữ liệu cá nhân số 91/2025/QH15**, có hiệu lực 01/01/2026 | `[ĐÃ XÁC MINH: S6, 2026-10-10]` (số hiệu, hiệu lực) |
| Nghị định hướng dẫn | Nghị định 356/2025/NĐ-CP (31/12/2025) hướng dẫn Luật; **nguồn thứ cấp mâu thuẫn** về việc NĐ 13/2023/NĐ-CP còn hiệu lực hay đã thay thế | `[CHƯA XÁC MINH: S7]` → `[CẦN XÁC NHẬN]` |
| Phân loại vị trí | Dữ liệu vị trí chính xác có thể thuộc loại dữ liệu nhạy cảm/ cần biện pháp bảo vệ đặc biệt theo luật; chưa đọc toàn văn | `[CHƯA XÁC MINH: U7]` → thiết kế theo hướng **thận trọng** (coi là nhạy cảm) |
| Cơ sở xử lý | **Sự đồng ý rõ ràng, tách riêng cho vị trí**, có thể rút lại; không gộp vào điều khoản chung | `[CẦN XÁC NHẬN]` |
| Thông báo xử lý | Màn hình nêu: dữ liệu gì (vị trí chính xác, tốc độ, thời gian), mục đích (chia sẻ với thành viên trip, lưu hành trình, SOS), ai nhận (thành viên trip), lưu bao lâu, bên xử lý (nhà cung cấp hạ tầng/FCM của Google — **chuyển dữ liệu ra nước ngoài** cần đánh giá) | `[CẦN XÁC NHẬN]` |
| Quyền của chủ thể dữ liệu | Truy cập/sao chép (`GET /me/export`), chỉnh sửa (`PATCH /me`), xóa (`DELETE /me`), rút lại đồng ý (`PUT /me/consents`), phản đối | Thiết kế có sẵn; mức bắt buộc chính xác `[CẦN XÁC NHẬN]` |
| Hồ sơ/ đánh giá tác động, thông báo vi phạm | Có thể có nghĩa vụ lập hồ sơ đánh giá tác động xử lý dữ liệu và báo cáo sự cố trong thời hạn quy định | `[CHƯA XÁC MINH: U7]` → luật sư |
| Người dưới 16/18 tuổi | App chặn đăng ký dưới độ tuổi (hỏi năm sinh/ xác nhận ≥ 18 ở MVP `[GIẢ ĐỊNH]`); quy định về trẻ em có yêu cầu riêng | `[CẦN XÁC NHẬN]` |

### 5.1 Màn hình consent (thiết kế)
1. Khi đăng ký: chấp nhận Điều khoản + Chính sách quyền riêng tư (hai checkbox riêng, không tick sẵn).
2. Trước lần đầu bật tracking: màn hình **"MotoLink dùng vị trí của bạn"** giải thích bằng tiếng Việt rõ ràng, nêu "cả khi màn hình tắt, trong lúc chuyến đi đang ghi", nút "Đồng ý và tiếp tục"/"Không đồng ý"; từ chối vẫn dùng được các tính năng không cần vị trí (xem trip, SOS không có tọa độ nếu từ chối quyền). Đây đồng thời là **prominent disclosure** của Google Play `[CHƯA XÁC MINH: U1]` (nội dung phải xuất hiện trong app ngay trước hộp thoại quyền hệ thống, tách khỏi privacy policy, có lựa chọn từ chối).
3. Ghi `consent_records(kind='location_processing', version)`; nâng version văn bản ⇒ hỏi lại.
4. Cài đặt có mục "Quyền riêng tư": xem consent, rút lại, xuất dữ liệu, xóa tài khoản.

### 5.2 Google Play
- `targetSdk=36` `[ĐÃ XÁC MINH: S1]` (hạn 31/08/2026; đã quá hạn nên bắt buộc với bản đăng mới).
- Khai báo FGS type `location` + permission `FOREGROUND_SERVICE_LOCATION` `[ĐÃ XÁC MINH: S2]`; Play Console yêu cầu khai báo mục đích foreground service `[CHƯA XÁC MINH: U1]`.
- **Xin `ACCESS_BACKGROUND_LOCATION`** (D-04, ADR-013) ⇒ phải nộp form khai báo "Location permissions" ở Play Console, video demo (runtime prompt, prominent disclosure, tính năng chạy từ nền) và chứng minh core functionality `[CHƯA XÁC MINH: U1]`; disclosure phải nói rõ vị trí được thu "cả khi ứng dụng đóng hoặc không dùng" khi có chuyến đang ghi. Luồng xin quyền: `12` §5.1 `[ĐÃ XÁC MINH: S3]`. Rủi ro bị từ chối: R-02.
- Data safety form: khai báo vị trí chính xác, mục đích "chức năng ứng dụng", chia sẻ với thành viên khác trong nhóm, có thể xóa theo yêu cầu, mã hóa khi truyền `[CẦN XÁC NHẬN]` theo form hiện hành.
- Privacy policy URL công khai, trong app và trên store listing.

## 6. Bảo mật hạ tầng và ứng dụng

- TLS 1.2+ (Caddy), HSTS, chỉ cổng 80/443/22 mở. WS `wss://`. Android: `networkSecurityConfig` cấm cleartext ở bản release; không pin chứng chỉ ở MVP (rủi ro vận hành khi xoay cert) `[GIẢ ĐỊNH]`.
- Android lưu token: Android Keystore (khóa AES-GCM) bọc refresh token trong DataStore/Room; không lưu mật khẩu. Thư viện cụ thể (Tink/Security-crypto…) `[CHƯA XÁC MINH: U11]` — chốt khi code.
- `android:allowBackup="false"` (hoặc quy tắc loại trừ DB/token); `FLAG_SECURE` không cần.
- Bản đồ API key (nếu dùng nhà cung cấp thương mại) giới hạn theo package + SHA-1 chữ ký; không commit vào repo.
- Secrets (JWT key, mật khẩu DB/Redis, FCM service account) nằm trong `.env`/Docker secrets ngoài git; `.env.example` không chứa giá trị thật.
- Phụ thuộc: `govulncheck` và `go mod verify` trong CI; `dependabot`/renovate cấp độ vừa phải `[GIẢ ĐỊNH]`.
- Giới hạn: tối đa kết nối WS/instance (mặc định 1 500), mỗi IP (mặc định 20) — `[GIẢ ĐỊNH]`; người dùng di động chia sẻ NAT (CGNAT) nên giới hạn IP phải rộng.
- Cookie/CSRF: không áp dụng (Bearer token, không cookie).
- Admin/quan sát: Grafana/Prometheus chỉ lắng nghe trên loopback/VPN hoặc sau auth cơ bản của Caddy.

## 7. Đồng bộ offline

### 7.1 Nguyên tắc
Room là nguồn dữ liệu cho UI. Server là nguồn sự thật. Mỗi loại dữ liệu có chiến lược riêng:

| Dữ liệu | Ghi cục bộ ngay? | Đồng bộ lên | Xung đột / reconcile |
|---|---|---|---|
| Điểm GPS của mình | ✔ (`pending_points`, PK `(session_id, seq)`) | WS `location.update` / REST `points`; xóa khi `up_to_seq` | Idempotent theo `(session_id, seq)`; không có xung đột |
| Session mình (start/pause/resume/end) | ✔ trạng thái cục bộ + `outbox_ops` | REST với `Idempotency-Key` cố định theo thao tác | **Quyết định MVP:** `start` **cần mạng** để lấy `session_id` (server cấp, ADR-004); không có mạng ⇒ UI báo "Cần kết nối để bắt đầu chuyến". Sau khi đã có `session_id`, mất mạng không ảnh hưởng (điểm vào Room). `pause/resume/end` thực hiện cục bộ ngay và đồng bộ qua `outbox_ops`. (Chế độ ghi hoàn toàn offline rồi gắn vào session sau bị cắt — ADR-020.) |
| SOS | ✔ (`pending_sos`, `client_sos_id`) | WS `sos.create` hoặc REST; WorkManager + reconnect | Server dedupe; client hiển thị trạng thái |
| `sharing_state`, `share_history`, màu | ✔ lạc quan | REST `PATCH members/me` qua `outbox_ops` | Server thắng khi khác: snapshot sau cùng ghi đè |
| Dữ liệu trip, thành viên, event | ✗ (chỉ đọc) | — | Cập nhật bằng snapshot/ events |
| Vị trí người khác | cache Room (điểm cuối) cho hiển thị khi mất mạng | — | Chỉ nhận nếu `(session_id, seq)` mới hơn bản đang có |

### 7.2 Hàng đợi `outbox_ops` (Android)
Cột: `op_id (uuid, = Idempotency-Key)`, `kind`, `payload_json`, `created_at`, `attempts`, `next_attempt_at`, `state(pending|inflight|done|dead)`. Một `SyncWorker` (WorkManager, ràng buộc `NetworkType.CONNECTED`) xử lý tuần tự theo `created_at` cho mỗi `(kind, target)`; lỗi `5xx`/mạng ⇒ backoff mũ; `4xx` không thể thử lại (409 invalid_state, 403, 404) ⇒ `dead` + thông báo người dùng. Quan trọng: thao tác `end session` đến sau `pause` không được vượt thứ tự ⇒ tuần tự theo session.

### 7.3 Reconnect, mất kết nối lâu
- Mất WS: đếm backoff; UI hiển thị "Đang kết nối lại…"; GPS tiếp tục ghi Room (nếu đang tracking).
- Sau khi kết nối lại: `hello.resume` (xem `07` §6). Mất lâu (> 7 ngày, hoặc server báo `resync.required`/`410 events_expired`) ⇒ `GET /sync/bootstrap` + `GET /trips/{id}/snapshot` và **thay thế** dữ liệu trip trong Room theo snapshot (giữ nguyên `pending_*`).
- Backlog điểm lớn (> 200): dùng REST `POST …/points` theo lô 500, tuần tự, kiểm `up_to_seq`.
- Tràn buffer cục bộ (24 giờ/ 20 000 điểm): downsample điểm cũ nhất (giữ mỗi điểm thứ n, ưu tiên điểm đổi hướng) và tăng metric phía client; hiển thị cảnh báo khi > 80 % dung lượng.

### 7.4 Chống ghi trùng
Điểm: khóa `(session_id, seq)` ở Redis Lua (live), PG `ON CONFLICT` (bền). SOS: `client_sos_id`. REST ghi: `Idempotency-Key` (24 h). Event bền ở client: bỏ nếu `event_seq ≤ last_event_seq`. Push FCM: dedupe theo `ref_event_id`.

### 7.5 Snapshot cũ không ghi đè event mới
Quy tắc áp dụng ở client (và đã mô tả ở `07` §6): vị trí chỉ cập nhật nếu `(session_id, seq)` lớn hơn; trạng thái thành viên/trip chỉ cập nhật bằng event có `event_seq` lớn hơn `last_event_seq` hoặc bằng snapshot có `head_event_seq ≥ last_event_seq` (nếu snapshot có `head_event_seq` nhỏ hơn — snapshot cũ đến muộn — thì bỏ). Test thuộc `14`.
