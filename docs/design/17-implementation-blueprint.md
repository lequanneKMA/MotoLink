# 17 — Implementation Blueprint và checklist việc code đầu tiên

Dành cho bạn (chủ dự án) sau khi duyệt thiết kế. Thứ tự ở đây bám `15-roadmap.md`; mã task trỏ về task card tương ứng.

## 1. Việc phải làm trước khi viết dòng code đầu tiên

Xác nhận (trả lời ngắn gọn từng mục; mặc định ở `16` §1 sẽ được dùng nếu bạn không đổi):

- [ ] **D-01** minSdk 26?
- [ ] **D-02/D-03** Email + mật khẩu, hoãn quên mật khẩu & xác minh email cho beta kín?
- [ ] **D-04** Không xin quyền vị trí nền ở MVP (xem lại sau gate M1)?
- [ ] **D-05** Giữ Kafka (mục tiêu học) với lối thoát `direct_pg`?
- [ ] **D-06/D-07** Retention 30/90/365 ngày; mặc định riêng tư lịch sử?
- [ ] **D-09** Thông tin VPS (đĩa, **băng thông tháng**), tên miền, nơi lưu backup.
- [ ] **D-10/D-11** Một kết nối WS/user; trip tối đa 15; mã mời do owner tạo.
- [ ] **D-14/D-19** Monorepo; tên app/package id.
- [ ] **D-16** Nội dung cảnh báo "SOS không thay thế số khẩn cấp".
- [ ] **D-18** Đã đọc §3 của `16` (cửa sổ mất dữ liệu) và chấp nhận.
- [ ] Chuẩn bị **máy thật**: ≥ 1 Xiaomi/Oppo, ≥ 1 Samsung, và nếu có, 1 máy không GMS. Đây là điều kiện để làm M1.
- [ ] Mở **luật sư/nguồn chính thức** cho D-08 song song (không chặn M0–M5, chặn beta rộng).

## 2. Thứ tự thực hiện chính xác

```
Bước 0  Duyệt thiết kế (mục 1)                                   -- bạn
Bước 1  M0-T01  monorepo + CI                                    -- ngày 1
Bước 2  M0-T06  khung Android (song song)                        -- ngày 1-2
Bước 3  M1-T01  SPIKE nền 60 phút trên máy thật                  -- ưu tiên SỐ 1
        M1-T06  SPIKE bản đồ/tile/chủ quyền (song song)
Bước 4  M1-T03, T04, T02, T05, T07, T08                          -- hoàn tất M1
Bước 5  GATE M1: đọc báo cáo, quyết định D-04, D-15, D-17; cập nhật ADR-013/014
Bước 6  M0-T02..T05  hạ tầng dev + schema + golden (có thể làm xen kẽ trong lúc chờ test M1)
Bước 7  M2   backend core (T01..T09, T11) -> M2-T10 Android
Bước 8  M3   realtime (T01..T10)
Bước 9  M4   Redis nhiều instance
Bước 10 M5   Kafka + ACK + phục hồi  (cập nhật số đo RAM thật vào 13)
Bước 11 M6   sản phẩm hoàn chỉnh -> beta kín
Bước 12 M7   độ tin cậy, deploy, backup -> phát hành hạn chế
```
Nguyên tắc: không bắt đầu bước sau khi DoD của bước trước (xem `15` §5) chưa đạt; nếu một spike làm thay đổi ADR, sửa ADR trước khi code tiếp.

## 3. Checklist việc code đầu tiên (tuần đầu)

Mục tiêu: có bằng chứng sớm về rủi ro lớn nhất, và nền tảng để mọi thứ sau chạy trên.

**Ngày 1 — nền**
- [ ] Tạo repo (monorepo) với layout `04` §1; thư mục `docs/` đã có; thêm `CLAUDE.md` từ `appendix-CLAUDE.md` (M0-T01).
- [ ] `server/go.mod`, `cmd/server` rỗng, CI: `go vet`, lint, `go test -race`, `govulncheck`.
- [ ] Khung Android `:app` build được, `targetSdk 36`, manifest **không** có quyền nền (M0-T06).

**Ngày 2–3 — spike nền (M1-T01)**
- [ ] `TrackingService` tối thiểu: `foregroundServiceType="location"` + `FOREGROUND_SERVICE_LOCATION` + thông báo.
- [ ] Ghi điểm (thời gian, lat/lng, accuracy) vào Room mỗi 3 s, bắt đầu từ nút bấm khi app ở foreground.
- [ ] Nút xuất log (file) để đọc sau test; không upload.
- [ ] Chạy 60 phút màn hình tắt, máy trong túi: **Xiaomi/Oppo** và **Samsung**; thử hai cấu hình (mặc định / bỏ tối ưu pin + autostart).
- [ ] Ghi vào `docs/design/bench/m1-oem-report.md`: thời điểm service chết, khoảng trống, pin.

**Ngày 3–4 — spike bản đồ (M1-T06)**
- [ ] MapLibre `13.6.1` hiển thị một style thử (tạm dùng style thử nghiệm KHÔNG dành cho bản phát hành).
- [ ] Thử nguồn tile (A tự host, B thương mại); ghi ToS/chi phí.
- [ ] **Kiểm tra bằng mắt** hiển thị Hoàng Sa/Trường Sa ở nhiều mức zoom; chụp ảnh đưa vào báo cáo.
- [ ] Xác minh `pmtiles://` có/không được hỗ trợ (U5) → cập nhật `research/`.

**Ngày 5 — chốt gate**
- [ ] Viết kết luận M1; quyết định D-04, D-15, D-17; cập nhật ADR-013/014 và `16`.
- [ ] Nếu mất nền nghiêm trọng: dừng, họp lại phương án (xin quyền nền + khai báo Play; đổi cách sử dụng; giảm kỳ vọng).

**Sau gate — việc code backend đầu tiên**
- [ ] M0-T02 Compose dev stack (PG/Redis/Kafka) và đo RAM sơ bộ.
- [ ] M0-T03 `platform` (config, `slog` redaction tọa độ + test, UUIDv7).
- [ ] M0-T04 migration baseline từ `05` + test schema.
- [ ] M0-T05 golden JSON cho 06/07.
- [ ] M2-T01..T03 (identity, middleware, idempotency) rồi M2-T04 (trip + event_seq + outbox helper).

## 4. Những thứ KHÔNG làm sớm
- Không viết Hub/WS trước khi có auth + trip + authz (M2).
- Không thêm Redis/Kafka vào đường chạy trước M4/M5; M3 dùng bộ nhớ.
- Không tối ưu hóa (batch Lua, binary protocol) trước khi có số đo B1.
- Không thêm tính năng ngoài MVP (ADR-020) trước M7.
- Không thay đổi contract mà không sửa tài liệu + ADR trước.

## 5. Quy trình làm việc đề xuất cho từng task
1. Đọc Input của task card (ADR/contract); nếu cần đổi contract → sửa tài liệu trước.
2. Viết test (hoặc golden) trước khi cài đặt phần logic then chốt (thứ tự `(session_id, seq)`, ACK, idempotency).
3. Cài đặt nhỏ nhất đạt Acceptance; chạy `go test -race`/Gradle test.
4. Tự duyệt theo checklist review của task; ghi số đo vào `PROGRESS.md`/`bench/`.
5. Commit nhỏ; mỗi milestone kết thúc bằng bản chạy được trên máy thật.

## 6. Tài liệu nguồn sự thật khi code
`05` (schema) · `06` (REST) · `07` (WS) · `08` (Redis) · `09` (Kafka) · `10` (concurrency) · `02` (ADR). Mâu thuẫn giữa code và tài liệu ⇒ sửa tài liệu/ADR trước rồi mới sửa code.
