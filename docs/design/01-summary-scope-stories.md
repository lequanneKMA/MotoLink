# 01 — Executive summary, phạm vi MVP, user stories

## 1. Executive summary

**MotoLink** là ứng dụng Android (tiếng Việt) cho nhóm 2–15 người đi xe máy cùng nhau: chia sẻ vị trí realtime trên bản đồ, thấy ai online/mất tín hiệu, ghi và xem lại hành trình, gửi SOS tới cả nhóm kể cả khi ứng dụng của họ không chạy.

**Kiến trúc đã chọn (một dòng mỗi ý):**
- *Backend:* modular monolith Go một binary (ADR-001), PostgreSQL là nguồn sự thật, Redis cho vị trí mới nhất/presence/Pub/Sub liên instance, Kafka (KRaft, 1 node) cho nhánh lịch sử và phân phối bất đồng bộ.
- *Vị trí:* hai nhánh độc lập — **live** (Redis + Hub, chấp nhận mất bản cũ) và **bền** (Kafka → PostgreSQL, ACK về client, at-least-once, ghi idempotent) (ADR-005).
- *Thứ tự:* `(session_id do server cấp, seq do client gán)`, không tin đồng hồ thiết bị (ADR-004).
- *Realtime:* Hub shard single-owner, outbox coalescing theo client, drop-oldest cho vị trí, đóng kết nối khi consumer quá chậm (ADR-009).
- *Android:* Foreground Service `location` do người dùng chủ động bật, Room là nguồn UI, hàng đợi offline, MapLibre; không xin quyền vị trí nền ở MVP (ADR-013, 018, 019).
- *Riêng tư:* chỉ thành viên trip thấy vị trí; kiểm quyền ở từng REST endpoint và từng WS message; consent riêng cho vị trí; không log tọa độ (ADR-016).

**Trung thực về đánh đổi:** ở quy mô MVP, Kafka không bắt buộc về mặt kỹ thuật; nó được giữ chủ yếu vì mục tiêu học và có lối thoát `HISTORY_SINK=direct_pg` (ADR-007). Rủi ro kỹ thuật lớn nhất là **ghi vị trí nền bị OEM kill** — phải kiểm bằng máy thật ở M1 trước khi đầu tư backend (`15`, `16`).

**Kết quả cuối mỗi giai đoạn là một bản dùng được** (xem `15`). Capacity sơ bộ vừa 1 VPS 4 GB nhưng chặt; nút thắt thật là băng thông ra theo tháng và đĩa PostgreSQL (`13` §4).

## 2. Phạm vi MVP

### Trong phạm vi
| # | Năng lực | Ghi chú |
|---|---|---|
| 1 | Đăng ký/đăng nhập email + mật khẩu, hồ sơ, đăng xuất | ADR-003; không có quên mật khẩu ở MVP (rủi ro trong `16`) |
| 2 | Tạo trip, tên, quyền riêng tư (`history_visibility`) | Vòng đời `draft→active→ended→archived` |
| 3 | Mời bằng mã (hạn, giới hạn lượt, thu hồi) | Chỉ owner tạo mã |
| 4 | Bản đồ nhóm realtime | MapLibre, nguồn tile theo ADR-014 |
| 5 | Trạng thái thành viên: online / offline / mất cập nhật vị trí | Suy ra từ presence + tuổi vị trí |
| 6 | Bắt đầu / tạm dừng / kết thúc ghi hành trình | Tracking session |
| 7 | Xem lại tuyến, thời lượng, quãng đường | Sau retention: polyline đã đơn giản hóa |
| 8 | SOS kèm vị trí: WS + FCM, theo dõi đã nhận/đã xem, chống trùng, gửi lại khi mất mạng | ADR-015 |
| 9 | Thông báo thành viên mới/ trip bắt đầu/kết thúc | FCM + trong app |
| 10 | **Tạm ngừng chia sẻ vị trí mà không rời trip** | **Giữ trong MVP** (ADR-016) |
| 11 | Chịu mạng yếu: reconnect, resume, đồng bộ lại | `07`, `11` §7 |
| 12 | Consent vị trí, xóa tài khoản/ dữ liệu, xuất dữ liệu `[CẦN XÁC NHẬN]` | `11` |

### Ngoài phạm vi (ADR-020)
Pause cấp trip; quên mật khẩu; Google Sign-In (M6+); bản đồ offline; chat/voice; geofence; chia sẻ công khai; đa thiết bị đồng thời; ghi hoàn toàn offline trước khi có `session_id`; reinstate thành viên bị xóa; iOS/web; turn-by-turn, tối ưu tuyến, giao thông, mạng xã hội, thanh toán, AI, đa tenant, microservices; PostGIS; tracing OTel.

### Đối tượng và môi trường
Người dùng Việt Nam, Android thực tế đa dạng hãng; có máy không có GMS (chấp nhận hạn chế: không FCM). Một VPS Ubuntu 2 vCPU/4 GB. Phát hành Google Play (và sideload APK để thử).

## 3. User stories và acceptance criteria

Quy ước: **US-x.y**; tiêu chí viết dạng Cho/Khi/Thì ngắn gọn. Liên kết sang contract nằm trong ngoặc.

### Epic A — Tài khoản
**US-A1 Đăng ký.** Là người dùng mới, tôi đăng ký bằng email và mật khẩu để có tài khoản.
- Khi nhập email hợp lệ, mật khẩu ≥ 8 ký tự và tick hai ô đồng ý (điều khoản, quyền riêng tư), thì tài khoản được tạo, tôi được đăng nhập (`POST /auth/register`).
- Khi email đã tồn tại, thì thấy lỗi rõ ràng; khi gửi lại cùng `Idempotency-Key`, không tạo tài khoản thứ hai.
- Consent được ghi với phiên bản văn bản (`consent_records`).

**US-A2 Đăng nhập/giữ phiên.** Tôi vẫn đăng nhập sau khi đóng app nhiều ngày.
- Access token hết hạn → app tự refresh âm thầm; refresh token xoay vòng.
- Khi refresh token bị dùng lại (bị đánh cắp), mọi phiên của chuỗi đó bị thu hồi và tôi được yêu cầu đăng nhập lại (`refresh_reused`).
- "Đăng xuất mọi thiết bị" làm các kết nối WS đang mở đóng trong ≤ 5 s.

**US-A3 Hồ sơ.** Tôi đổi tên hiển thị và màu đại diện (`PATCH /me`); thay đổi hiện cho các thành viên trong lần đồng bộ kế tiếp.

**US-A4 Xóa tài khoản.** Tôi xóa tài khoản và dữ liệu của mình.
- Nếu tôi là owner của trip còn thành viên khác → được yêu cầu chuyển quyền hoặc kết thúc trip trước.
- Sau khi xác nhận mật khẩu: phiên bị thu hồi, vị trí/lịch sử của tôi bị xóa theo `05` §6; tôi nhận xác nhận khi hoàn tất.

### Epic B — Trip và thành viên
**US-B1 Tạo trip.** Tôi tạo chuyến đi có tên (≤ 80 ký tự); tôi tự là owner; trạng thái `draft`.
**US-B2 Mời.** Là owner, tôi tạo mã tham gia (hạn mặc định 24 giờ, tối đa lượt dùng) và chia sẻ; tôi xem danh sách mã và thu hồi được.
- Mã chỉ hiện đầy đủ một lần; mã đã thu hồi/hết hạn/hết lượt không dùng được và thông báo lỗi không tiết lộ tồn tại trip.
**US-B3 Tham gia.** Tôi nhập mã, xem tên trip + số thành viên, đồng ý tham gia.
- Chỉ khi trip chưa `ended`; trip đủ `max_members` thì từ chối; đã là thành viên thì trả lại trạng thái hiện tại.
- Nhập sai quá ngưỡng bị `429`.
**US-B4 Quản lý thành viên.** Owner xóa một thành viên; người đó không còn thấy vị trí nhóm và kết nối WS của họ cho trip đó bị ngắt trong ≤ 5 s; họ không thể dùng lại mã để vào.
**US-B5 Rời trip.** Thành viên rời trip; session của họ kết thúc; nếu là owner phải chuyển quyền trước.
**US-B6 Bắt đầu/kết thúc trip.** Owner bắt đầu (`draft→active`) và kết thúc (`→ended`); khi kết thúc mọi tracking session đóng, vị trí live biến mất, mọi thành viên nhận thông báo.

### Epic C — Chia sẻ vị trí và bản đồ
**US-C1 Xem nhóm trên bản đồ.** Là thành viên trip `active`, tôi thấy vị trí hiện tại của các thành viên đang chia sẻ.
- Vị trí mới hiển thị với độ trễ mục tiêu p95 ≤ 3 s tính từ lúc đo (chia nhỏ: ≤ 1 s xử lý server + chu kỳ GPS ≤ 3 s) `[mục tiêu kiểm chứng]`.
- Không bao giờ thấy vị trí lùi về điểm cũ hơn (so `(session_id, seq)`).
- Người không thuộc trip không truy cập được dữ liệu (kể cả biết `trip_id`).
**US-C2 Trạng thái thành viên.** Tôi thấy mỗi thành viên là *online*, *offline* hoặc *mất cập nhật vị trí*.
- *Mất cập nhật*: online nhưng điểm cuối cũ hơn 30 s (hiển thị mờ) và > 90 s (cảnh báo).
- *Offline*: sau khi mất kết nối ≤ 55 s (TTL presence 45 s + chu kỳ reap 10 s).
**US-C3 Tạm ngừng chia sẻ.** Tôi bấm "Tạm ngừng chia sẻ"; người khác không còn thấy vị trí tôi (hiển thị "đang ẩn"), tôi vẫn ở trong trip và vẫn nhận SOS; tiếp tục chia sẻ bằng một chạm.
- Khi tạm ngừng, server không lưu vị trí tôi vào Redis, không phát cho người khác (`07` §7).
**US-C4 Camera bản đồ.** Chế độ "theo tôi" và "xem cả nhóm"; kéo bản đồ tắt tự theo.
**US-C5 Hiển thị lãnh thổ.** Bản đồ hiển thị Hoàng Sa, Trường Sa thuộc Việt Nam (tiêu chí nghiệm thu bản đồ, ADR-014).

### Epic D — Hành trình
**US-D1 Bắt đầu ghi.** Tôi bấm *Bắt đầu*, đồng ý consent lần đầu, cấp quyền vị trí; thấy thông báo "Đang ghi hành trình" kèm nút Tạm dừng/Kết thúc.
- Chưa có mạng ⇒ báo cần kết nối để bắt đầu (ADR-020).
- Đã có session mở của tôi ⇒ app đề nghị tiếp tục thay vì tạo mới (`409 session_already_open`).
**US-D2 Tạm dừng/tiếp tục/kết thúc.** Tạm dừng: không thu/ không gửi điểm; tiếp tục: cùng `session_id`; kết thúc: GPS dừng ngay, thông báo FGS biến mất, thống kê được tính sau ≤ 2 phút.
**US-D3 Xem lại.** Tôi xem tuyến đã đi (đường vẽ trên bản đồ), thời lượng, quãng đường, số điểm.
- Thành viên khác chỉ xem được tuyến của tôi nếu `history_visibility='members'` và tôi chưa tắt `share_history`.
- Sau thời gian lưu thô (30 ngày) vẫn xem được tuyến đơn giản hóa; sau 90 ngày điểm thô bị xóa.
**US-D4 Bền khi mất mạng.** Khi mất mạng 10 phút giữa chuyến, các điểm trong thời gian đó vẫn xuất hiện trong lịch sử sau khi có mạng, không trùng.
**US-D5 Gián đoạn do hệ điều hành.** Nếu dịch vụ nền bị hệ thống tắt, khi mở lại tôi thấy "Chuyến ghi bị gián đoạn lúc HH:MM" và được hướng dẫn cài đặt theo hãng máy; (máy có GMS) có thể nhận thông báo đẩy "Việc ghi bị gián đoạn".

### Epic E — SOS
**US-E1 Gửi SOS.** Tôi bấm giữ nút SOS 2 giây (chống bấm nhầm), chọn loại (tùy chọn), SOS gửi kèm vị trí hiện tại.
- Thấy trạng thái: *Đang chờ gửi → Đã gửi → N/M người đã nhận → N đã xem*.
- Gửi lại cùng SOS (mất mạng, retry) không tạo bản trùng (`client_sos_id`).
- Mất mạng: SOS xếp hàng và tự gửi khi có mạng; UI cho thấy đang chờ.
**US-E2 Nhận SOS.** Là thành viên, tôi nhận SOS qua WS nếu app đang mở, qua thông báo đẩy ưu tiên cao nếu app không chạy (máy có GMS); chạm vào mở màn hình SOS với vị trí người gửi.
- Server ghi `delivered_*`; ứng dụng gửi *received* khi nhận, *seen* khi mở.
**US-E3 Giải quyết SOS.** Người gửi hoặc owner đánh dấu *đã giải quyết/hủy*; mọi thành viên thấy trạng thái.
**US-E4 Trung thực về giới hạn.** Người gửi biết có bao nhiêu người **thực sự đã nhận**, và nếu có người không nhận được push (không token/ lỗi), hiển thị điều đó.

### Epic F — Thông báo
**US-F1** Tôi nhận thông báo khi: có thành viên mới, trip bắt đầu/kết thúc, SOS. Mỗi sự kiện chỉ tạo một thông báo cho tôi dù bị xử lý lại (`UNIQUE(user_id,type,ref_event_id)`; client dedupe).
**US-F2** Tôi xem danh sách thông báo và đánh dấu đã đọc.

### Epic G — Mạng không ổn định
**US-G1 Reconnect.** Khi mất sóng rồi có lại, app tự kết nối lại (backoff có jitter), thấy snapshot mới và các sự kiện đã bỏ lỡ; không có vị trí lùi.
**US-G2 Client chậm.** Điện thoại đọc chậm không làm máy chủ tăng bộ nhớ vô hạn hay làm chậm người khác; nếu quá chậm kết nối bị đóng và tự phục hồi.
**US-G3 Máy chủ khởi động lại.** Khi server restart, client nhận `closing`, kết nối lại trong ≤ 30 s, không mất điểm đã ACK.
**US-G4 App cũ.** App quá cũ so với server nhận thông báo "Cần cập nhật" thay vì lỗi khó hiểu (`upgrade_required`).

### Epic H — Quyền riêng tư và tuân thủ
**US-H1 Consent.** Trước khi thu vị trí lần đầu, tôi thấy giải thích rõ ràng (dữ liệu gì, ai thấy, lưu bao lâu, thu cả khi tắt màn hình), có nút từ chối mà vẫn dùng được phần còn lại.
**US-H2 Rút lại.** Rút consent vị trí ⇒ ghi dừng ngay; tôi có thể yêu cầu xóa dữ liệu.
**US-H3 Xuất dữ liệu** `[CẦN XÁC NHẬN]` — tôi tải bản sao dữ liệu của chính mình.
**US-H4 Không lộ qua log.** Tọa độ không xuất hiện trong log/ metric/ báo lỗi (kiểm bằng test tự động).

### Epic I — Vận hành (cho chủ dự án)
**US-I1** Tôi thấy dashboard: kết nối WS, điểm/giây, độ trễ fan-out, lag Kafka, tuổi vị trí hiển thị, tỉ lệ SOS giao được (`13` §5).
**US-I2** Tôi khôi phục được PostgreSQL từ backup (diễn tập hằng tháng).
**US-I3** Tôi triển khai bản mới mà người dùng chỉ thấy một lần reconnect.

## 4. Ánh xạ 10 năng lực của prompt → story
| Prompt §3 | Story |
|---|---|
| 1 Đăng ký/đăng nhập/hồ sơ | A1–A4 |
| 2 Tạo trip, quyền riêng tư | B1, B6, C3, D3 (`history_visibility`) |
| 3 Mời thành viên | B2, B3 |
| 4 Xem vị trí nhóm | C1, C4 |
| 5 Online/offline/mất cập nhật | C2 |
| 6 Bắt đầu/tạm dừng/kết thúc ghi | D1, D2 |
| 7 Xem lại tuyến | D3 |
| 8 SOS (kể cả app không chạy) | E1–E4 |
| 9 Thông báo | F1, F2, B4–B6 |
| 10 Tạm ngừng chia sẻ | C3 (**giữ trong MVP**) |
