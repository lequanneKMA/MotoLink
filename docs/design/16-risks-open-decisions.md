# 16 — Rủi ro, giả định, quyết định cần xác nhận

Nhãn: `[GIẢ ĐỊNH]` (tôi đã chọn mặc định hợp lý và tiếp tục theo mục 0.3), `[CẦN XÁC NHẬN]` (chủ dự án quyết), `[CHƯA XÁC MINH]` (xem `research/unverified.md`). Không có điểm mở nào nằm ngoài file này (checklist 0.6).

## 1. Quyết định cần chủ dự án xác nhận trước khi code

**Chặn** = phải có trước M0 kết thúc/trước task nêu; **Mặc định** = tôi sẽ làm theo nếu không có phản hồi.

| ID | Quyết định | Khuyến nghị / mặc định | Chặn? | Ảnh hưởng |
|---|---|---|---|---|
| D-01 | `minSdk` | **26** `[GIẢ ĐỊNH]` (không có số liệu phân bố VN: U6). `targetSdk=36` đã xác minh (S1) | Trước M0-T06 | Phạm vi máy hỗ trợ, code tương thích |
| D-02 ✅ | Phương thức xác thực MVP | **Đã xác nhận 2026-10-10:** đăng nhập cơ bản (email + mật khẩu) trước; sau này tích hợp **Google và Facebook**; không OTP SMS (ADR-003) | Trước M2-T01 | Thiết kế auth, nhà cung cấp email |
| D-03 | Quên mật khẩu & xác minh email | **Hoãn** cả hai ở MVP (cần nhà cung cấp email); chấp nhận cho beta kín; bắt buộc trước phát hành rộng | Trước M6-T09 | Rủi ro R-12/R-13 |
| D-04 ✅ | Quyền vị trí nền | **Đã xác nhận 2026-10-10: xin đủ quyền để app chạy nền** — `ACCESS_BACKGROUND_LOCATION` + FGS `location` + hướng dẫn bỏ tối ưu pin (ADR-013, `12` §5.1). Hệ quả: phải làm form vị trí nền Play + video + disclosure (R-02); spike M1 vẫn kiểm xem có đủ chống OEM kill không | Spike M1 / M6-T08 | Review Google Play, khả năng tự phục hồi tracking |
| D-05 ✅ | Giữ Kafka hay dùng `direct_pg` mặc định | **Đã xác nhận 2026-10-10: giữ Kafka** (mục tiêu học). **Giải quyết tension với D-09 (ADR-021):** Kafka giữ ở code/local/CI; **deploy Render dùng `direct_pg`** vì Kafka không chạy được trong 512 MB | Trước M5 | RAM host, độ phức tạp vận hành |
| D-06 | Số liệu retention | Điểm thô **30 ngày**, partition **90 ngày**, trip lưu **365 ngày** sau archive, outbox/idempotency theo `05` §6 `[GIẢ ĐỊNH]` | Trước M6-T01 | Dung lượng đĩa (`13` §4.2), quyền riêng tư |
| D-07 | Mặc định riêng tư lịch sử | `history_visibility='members'`, mỗi người tắt được `share_history`; owner **không** có đặc quyền xem thêm | Trước M2-T04 | Schema/quyền |
| D-08 | Pháp lý (không phải tư vấn pháp lý) | Luật sư xác nhận: (a) hiệu lực NĐ 13/2023 so với NĐ 356/2025 (S7 mâu thuẫn); (b) vị trí chính xác có là dữ liệu nhạy cảm; (c) nội dung consent/thông báo; (d) chuyển dữ liệu ra nước ngoài (FCM, hạ tầng); (e) người dưới 18; (f) có phải lập hồ sơ đánh giá tác động; (g) quyền xuất dữ liệu `GET /me/export` | Trước M6-T06/T08 | Màn hình consent, privacy policy, Play form |
| D-09 ✅ | Hạ tầng thực tế | **Đã xác nhận 2026-10-10: Render free** (ADR-021). Dưới đây là phần nội dung cũ còn lại cần theo dõi. **Chủ dự án chọn: dùng backend miễn phí, không thuê VPS** `[GIẢ ĐỊNH]` cách hiểu: hosting free-tier/tự có máy. Điều kiện Render free tra được chỉ từ nguồn thứ cấp `[CHƯA XÁC MINH: U14]`: 512 MB/0.1 CPU, ngủ sau 15 phút, Postgres free 30 ngày, Key Value 25 MB, băng thông 5 GB/tháng. Còn cần: nhà cung cấp, RAM/CPU/đĩa thực tế, hạn mức băng thông, có cho process chạy liên tục + WebSocket + cổng TCP riêng không, tên miền + TLS, nơi lưu backup, nhà cung cấp email (nếu D-03 làm). **Tension:** thiết kế `13` giả định 1 VPS 2 vCPU/4 GB chạy Postgres + Redis + Kafka + Go; free-tier thường không đáp ứng (xem R-21) | Còn lại: chọn PG miễn phí không hết hạn + đọc trang Render chính thức **trước M4**; M0–M3 chạy local | `13` §4; ADR-017; ADR-007 |
| D-10 | Một kết nối WS mỗi user | **Có** (kết nối mới thay cũ, 4409); không hỗ trợ đa thiết bị đồng thời | Trước M3-T02 | Presence, UX đổi máy |
| D-11 | Giới hạn trip | Tối đa **15** thành viên; chỉ owner tạo mã mời; mã 8 ký tự, hạn mặc định 24 giờ | Trước M2-T05 | Schema `max_members`, UX |
| D-12 | Phiên bản công nghệ | Go (stable mới nhất khi bắt đầu), PostgreSQL major, Redis, Caddy, Prometheus, Grafana, tag Kafka: chốt và ghim digest ở M0 (U10); thư viện Android ghim ở M1 (U11) | M0-T01/T02, M1 | Reproducibility |
| D-13 | DI và thư viện Android | Chưa chọn (Hilt / Koin / thủ công); đề xuất tự dựng nhẹ lúc đầu, đánh giá lại ở M2 | Trước M2-T10 | Cấu trúc code |
| D-14 | Monorepo | **Có** một repo (server + android + docs + deploy) | Trước M0-T01 | Layout `04` §1 |
| D-15 | Nguồn tile | Quyết định ở spike M1-T06 giữa tự host (PMTiles/tile server) và thương mại — phải đạt tiêu chí chủ quyền VN + ToS | Gate M1 | ADR-014, chi phí |
| D-16 | Tuyên bố giới hạn SOS | App **không thay thế** số khẩn cấp; hiển thị cảnh báo khi onboarding và trên màn hình SOS ("gọi 113/114/115 khi nguy hiểm tính mạng" — số điện thoại và nội dung cần xác nhận) | Trước M6-T04 | Trách nhiệm/pháp lý, kỳ vọng người dùng |
| D-17 | Ngưỡng nghiệm thu thực địa | Pin/giờ, tỉ lệ mất nền chấp nhận được: chốt sau M1 với số đo thật | Gate M1 | DoD M7 |
| D-18 ✅ | Chấp nhận các cửa sổ mất dữ liệu ở §3 | **Đã xác nhận 2026-10-10: chấp nhận, sẽ cải thiện sau** (nâng cấp PITR/replica/RF khi có người dùng thật). Lưu ý: nếu D-09 chuyển sang free-tier có đĩa/backup yếu hơn thì cửa sổ L-5 có thể rộng hơn §3 mô tả — cần đánh giá lại khi chọn host | Trước M5 | Kỳ vọng độ tin cậy |
| D-19 | Tên app, package id, tên miền API | Đề xuất tạm `MotoLink`, `app.motolink` (placeholder) | Trước M0-T06 | Manifest, chứng chỉ |

## 2. Rủi ro lớn nhất (xếp theo mức nghiêm trọng)

| ID | Rủi ro | Khả năng | Tác động | Giảm thiểu / kế hoạch | Mốc |
|---|---|---|---|---|---|
| R-01 | **OEM kill dịch vụ nền** (Xiaomi/Oppo/Vivo/Samsung): mất GPS khi màn hình tắt; không có API công khai khắc phục triệt để | Cao | Rất cao (làm mất giá trị cốt lõi) | Spike M1 trên máy thật; hướng dẫn cài đặt theo hãng; ghi Room để không mất điểm đã ghi; phát hiện + thông báo gián đoạn; đã xin đủ quyền nền (D-04) nhưng **không loại bỏ được OEM kill**; gate M1 đo xem có đủ không, nếu không thì đổi kỳ vọng sản phẩm (ADR-013, `12` §5.3) | M1 |
| R-02 | Google Play từ chối/đòi bổ sung do dùng vị trí/FGS; chính sách chưa đọc được nguồn gốc (U1) | Trung bình | Cao | **Đã quyết định xin quyền nền (D-04) ⇒ rủi ro tăng:** chuẩn bị form vị trí nền + video + disclosure; biện minh core functionality (ghi hành trình khi tắt màn hình); đọc lại chính sách gốc sớm (từ M1, không để đến M6-T08); sideload APK cho beta nếu bị từ chối; phương án lùi: bỏ quyền nền và quay lại ADR-013 bản cũ | M1–M6 |
| R-03 | Tile bản đồ: ToS/chi phí/chủ quyền Việt Nam (U2–U5); MapLibre Native có hỗ trợ nguồn tự host hay không | Trung bình | Cao (pháp lý + hiển thị sai chủ quyền) | Spike M1-T06 với tiêu chí nghiệm thu; style do mình kiểm soát; cấu hình URL từ server | M1 |
| R-04 | Tuân thủ dữ liệu cá nhân (Luật 91/2025, NĐ 356/2025/NĐ 13/2023 chưa rõ — S7, U7) | Trung bình | Cao | Thiết kế thận trọng (consent riêng, xóa, xuất, lưu tối thiểu); luật sư xác nhận D-08 trước beta rộng | M6 |
| R-05 | **Mất dữ liệu / sai lệch** do single node: RF=1 Kafka, 1 PG không replica, backup 24 giờ | Thấp–TB | Cao | Mô tả rõ cửa sổ (§3); metric lag; backup mã hóa; diễn tập restore; nâng cấp (PITR/replica) khi có người dùng thật | M7 |
| R-06 | SOS không đến được: máy không GMS, app bị force-stop, FCM lỗi, người nhận tắt thông báo | Trung bình | Rất cao (an toàn) | Phân biệt trạng thái đã nhận/ chưa; hiển thị trung thực "N/M đã nhận"; kênh WS+FCM; cảnh báo D-16; không hứa hẹn bảo đảm | M6 |
| R-07 | RAM 4 GB chật khi có Kafka + PG + 2 instance + giám sát | Trung bình | Trung bình | Ngân sách `13` §4.3; đo thật ở M5/M7; tắt Grafana; `HISTORY_SINK=direct_pg` | M5 |
| R-08 | Băng thông ra hàng tháng vượt hạn mức VPS (~147 GB/tháng kịch bản xấu: `13` §4.2) | Trung bình | Trung bình | Coalescing (đã thiết kế); giảm tần suất khi đứng yên; nén; kiểm hạn mức (D-09) | M4 |
| R-09 | Tăng trưởng đĩa PG (≈ 11 GB/30 ngày thô ở 500 thiết bị đỉnh) | Trung bình | Trung bình | Retention + downsample; partition; giám sát đĩa | M6 |
| R-10 | Scope creep với 1 developer; hệ thống phức tạp (Hub, Redis, Kafka, Android nền) | Cao | Trung bình | Cắt scope ADR-020; milestone cho bản dùng được; gate; `direct_pg`; không thêm công nghệ | Mọi |
| R-11 | Lỗi concurrency (race, rò rỉ goroutine, deadlock) trong Hub | Trung bình | Cao | Single-owner, test `-race`/`goleak`/`-count`, review checklist `10` §10, simclient | M3 |
| R-12 | Chiếm tài khoản / spam đăng ký khi không xác minh email | Trung bình | Trung bình | Rate limit; argon2id; sau này email verification (D-03) | M6 |
| R-13 | Không có quên mật khẩu → người dùng mất tài khoản | Cao | Trung bình | Chấp nhận cho beta; thêm trước phát hành rộng | M6 |
| R-14 | Người dùng lạm dụng định vị (theo dõi người khác không đồng ý) | Thấp–TB | Rất cao (đạo đức/pháp lý) | Chỉ thu khi chủ động bắt đầu + thông báo FGS luôn hiện; chỉ thành viên đã tham gia thấy; có thể rời và tạm ngừng chia sẻ bất kỳ lúc nào; mã mời hạn ngắn | M6 |
| R-15 | GPS giả (mock) hoặc dữ liệu sai | Thấp | Thấp | Validate tốc độ/nhảy, `suspect`; không giải quyết triệt để (chấp nhận) | — |
| R-16 | Pin tiêu hao lớn → bị gỡ cài đặt | Trung bình | Cao | Lấy mẫu thích ứng, đo ở M1/M7, cho người dùng chọn chế độ tiết kiệm | M1 |
| R-17 | Phụ thuộc API thư viện (franz-go non-blocking enqueue, MapLibre pmtiles, FCM SDK) chưa xác minh | Thấp | Thấp–TB | Xác minh khi pin phiên bản; tránh phụ thuộc tính năng chưa thấy | M0/M5 |
| R-18 | Lệch đồng hồ thiết bị, CGNAT làm rate limit IP sai | Thấp | Thấp | Offset ở `hello`; giới hạn IP rộng, ưu tiên giới hạn theo user | M3 |
| R-19 | Pub/Sub mất message âm thầm (không ai biết) | Trung bình | Thấp (vị trí tự lành; event có đường bù) | `resync.required{pubsub_gap}` sau phục hồi; phát hiện lỗ `event_seq` | M4 |
| R-20 | Giả định ACK `location.ack`: client dựa vào ACK để xóa dữ liệu — lỗi logic ACK sẽ gây mất điểm | Thấp | Cao | Test A7/A8, thuộc tính "up_to_seq liên tục", client xóa `≤ up_to_seq` theo session | M5 |
| R-21 | **Render free hạn chế nặng** (`[CHƯA XÁC MINH: U14]`): service ngủ sau 15 phút (SOS đầu tiên có thể trễ ≈ 1 phút), 0.1 CPU/512 MB, băng thông 5 GB/tháng, Postgres free chỉ 30 ngày, Redis không bền, chưa rõ WebSocket | Cao | Cao (an toàn SOS, mất dữ liệu PG sau 30 ngày, vượt băng thông) | ADR-021: một service `direct_pg`; PG miễn phí không hết hạn từ nhà cung cấp khác hoặc backup/restore định kỳ; kiểm WS ngay ở M3-T02; giới hạn kết nối; đọc trang Render chính thức trước M4; **nâng gói luôn bật trước khi phát hành rộng**; không dùng Render free cho benchmark | Trước M4 |

## 3. Cửa sổ mất dữ liệu và giới hạn trung thực (không tuyên bố "không mất dữ liệu")

| # | Tình huống | Hệ quả | Phát hiện |
|---|---|---|---|
| L-1 | Người dùng gỡ app / xóa dữ liệu / thiết bị hỏng khi còn điểm **chưa ACK** | Mất các điểm đó | Không phát hiện được phía server (chỉ thấy khoảng trống) |
| L-2 | Buffer cục bộ tràn (24 giờ / 20 000 điểm) khi mất mạng rất lâu | Điểm cũ bị downsample | Metric phía client + cảnh báo UI |
| L-3 | Đĩa broker Kafka hỏng sau khi ACK nhưng trước khi `history-writer` ghi PG | Mất lô điểm đó (RF=1) | `kafka_consumer_lag`; mục tiêu lag < 10 s ⇒ cửa sổ ngắn |
| L-4 | PG ngừng hoạt động lâu hơn retention Kafka (3 ngày) | Mất dữ liệu quá hạn đệm | Cảnh báo lag |
| L-5 | Mất PG/đĩa VPS giữa hai lần backup | Mất tối đa 24 giờ dữ liệu (RPO) | Backup/diễn tập; nâng cấp PITR |
| L-6 | OEM kill dịch vụ nền | Không có điểm trong khoảng đó (không thể bù) | `product_tracking_interrupted_total`, gap ratio |
| L-7 | Live path (Redis/Hub/WS) | **Cố ý** at-most-once: viewer có thể bỏ lỡ điểm trung gian | Không cần; điểm mới nhất luôn thay thế |
| L-8 | FCM | Có thể trễ, trùng, hoặc không đến | `sos_recipients.received_at`, ack |
| L-9 | Lệch giữa PG và Redis khi Redis restart | Vị trí hiện tại trống đến điểm kế tiếp | Hiển thị "mất cập nhật" |

## 4. Giả định chính đã dùng (tổng hợp, `[GIẢ ĐỊNH]`)

`minSdk=26`; 4 giờ tracking đỉnh/ngày; kích thước điểm JSON ≈ 270 B, nén ≈ 100 B/điểm; 150 B/điểm PG; RAM Kafka heap 512 MiB; Go ≈ 200 MiB/instance; retention 30/90/365; token TTL 15 phút/60 ngày; presence TTL 45 s; ngưỡng stale 30/90 s; session timeout 10 phút; auto-end trip 24 giờ; không xác minh email ở MVP; domain sẵn có; Kafka không ACL (single tenant); `GOMEMLIMIT`; mã mời 8 ký tự; ngưỡng cảnh báo `13` §5.3; v-1 hỗ trợ 90 ngày; các ngưỡng validate GPS (`07` §8); giới hạn kết nối 1 500/instance, 20/IP.

## 5. Các điểm của prompt gốc được đánh giá/ sửa

1. PostGIS trong Compose: không cần (ADR-010).
2. Kafka tại quy mô này là lựa chọn học tập, có lối thoát (ADR-007).
3. "Zero data loss": không chứng minh được → thay bằng các bảo đảm có điều kiện (ADR-005, §3).
4. Trip `paused` ở ví dụ vòng đời: cắt, thay bằng pause ở cấp session/sharing (ADR-012).
5. Đồng bộ "chưa có `session_id`" khi offline: yêu cầu có mạng để bắt đầu (ADR-020).
6. Nguy cơ chủ quyền bản đồ và tile OSM: không dùng tile công cộng (ADR-014).
7. FGS `location` không thể khởi chạy từ nền khi thiếu quyền nền (S2) — điều prompt chưa nêu rõ. Chủ dự án đã chọn xin quyền nền (D-04) để có khả năng tự phục hồi tracking; đánh đổi là review Google Play (R-02).
