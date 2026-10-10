# MotoLink — Bộ tài liệu thiết kế kỹ thuật

Phiên bản: 2026-10-10. Nhánh: `docs/motolink-architecture` (chỉ có commit cục bộ, chưa push vì repo chưa cấu hình remote). Sinh theo mục 0 của `docs/prompt/MotoLink-planning-prompt.md`. **Chưa có implementation code** (đúng mục 0.5).

## 1. Mục lục

| File | Nội dung | Mục prompt §11 |
|---|---|---|
| [01-summary-scope-stories](01-summary-scope-stories.md) | Executive summary, phạm vi MVP, user stories + acceptance | 1, 2 |
| [02-adr](02-adr.md) | 20 ADR (Context/Decision/Alternatives/Consequences) | 3 |
| [03-architecture-diagrams](03-architecture-diagrams.md) | Sơ đồ tổng thể; sequence: location, reconnect, SOS, persistence | 4, 5 |
| [04-modules](04-modules.md) | Module/package Go và Android | 6 |
| [05-data-model](05-data-model.md) | ERD, DDL PostgreSQL, máy trạng thái, transaction, retention | 7 |
| [06-rest-api](06-rest-api.md) | REST contract + ma trận quyền | 8 |
| [07-websocket-protocol](07-websocket-protocol.md) | WS contract, resume, authz từng message | 9 |
| [08-redis](08-redis.md) | Key/channel/TTL/Lua, hành vi khi Redis lỗi | 10 |
| [09-kafka](09-kafka.md) | Topic, key, schema, consumer, retry, DLQ, lag | 11 |
| [10-concurrency](10-concurrency.md) | Goroutine/channel/lifecycle/shutdown | 12 |
| [11-security-privacy-offline](11-security-privacy-offline.md) | Bảo mật, riêng tư, pháp lý VN, offline sync | 13 |
| [12-android](12-android.md) | Kiến trúc Android, nền, OEM, bản đồ | (mục 4, 8 prompt) |
| [13-deploy-observability](13-deploy-observability.md) | Compose, deploy, backup, metrics, **capacity** | 14 |
| [14-testing-benchmark](14-testing-benchmark.md) | Test strategy, benchmark plan | 15 |
| [15-roadmap](15-roadmap.md) | Milestone M0–M7, task card, Definition of Done | 16, 17 |
| [16-risks-open-decisions](16-risks-open-decisions.md) | Rủi ro, giả định, **quyết định cần bạn xác nhận** | 18 |
| [17-implementation-blueprint](17-implementation-blueprint.md) | Thứ tự thực hiện + checklist việc code đầu tiên | cuối |
| [appendix-CLAUDE](appendix-CLAUDE.md) | `CLAUDE.md` đề xuất cho giai đoạn code | Phụ lục A |
| [PROGRESS](PROGRESS.md) | Nhật ký tiến độ | — |
| [research/sources](research/sources.md), [research/unverified](research/unverified.md) | Nguồn đã tra, thông tin chưa xác minh | 0.4 |

## 2. Cách đọc

1. Muốn nắm nhanh: `01` → `02` (bảng tổng hợp + các điểm prompt được đánh giá lại) → `03` → `16`.
2. Chuẩn bị code: `17` rồi `15`; contract nằm ở `05`, `06`, `07`, `08`, `09`, `10`.
3. Khi code, tài liệu là **nguồn sự thật**. Đổi contract ⇒ sửa tài liệu + ADR trước (`appendix-CLAUDE.md`).
4. Nhãn độ chắc chắn: `[ĐÃ XÁC MINH: …]`, `[GIẢ ĐỊNH]`, `[CẦN XÁC NHẬN]`, `[CHƯA XÁC MINH]`.

## 3. Bảng trạng thái quyết định

| ADR | Quyết định (tóm tắt) | Trạng thái |
|---|---|---|
| 001 | Modular monolith, một binary, `ROLES` | Chốt |
| 002 | Bộ thư viện Go (phiên bản tra 2026-10-10) | Chốt, pin khi code |
| 003 | Email + mật khẩu; JWT EdDSA 15' + refresh xoay vòng | Chốt — D-02/D-03 chờ xác nhận |
| 004 | UUIDv7, `session_id` server cấp, `(session_id, seq)` | Chốt |
| 005 | Live path tách khỏi durable path; ACK = đã vào Kafka | Chốt |
| 006 | Redis Pub/Sub per-trip, ref-count, lọc echo | Chốt |
| 007 | Kafka KRaft 1 node, at-least-once + sink idempotent; lối thoát `direct_pg` | Chốt — D-05 |
| 008 | Outbox cho trip event/SOS | Chốt |
| 009 | Hub shard single-owner + coalescing | Chốt |
| 010 | PG không PostGIS; partition tháng; retention | Chốt — D-06 |
| 011 | WS envelope, cursor `event_seq`, resume/snapshot | Chốt |
| 012 | State machines (không pause cấp trip) | Chốt |
| 013 | FGS location, Fused + fallback; **không** xin quyền nền | Chốt hướng — **gate M1**, D-04 |
| 014 | MapLibre + tile do mình kiểm soát; không tile OSM công cộng | Chốt hướng — **spike M1**, D-15 |
| 015 | SOS: PG sự thật, WS + FCM, dedupe | Chốt — D-16 |
| 016 | Riêng tư mặc định + consent | Chốt — D-08 (pháp lý) |
| 017 | Compose, Caddy, Prometheus/Grafana | Chốt — D-09 |
| 018 | Android single-module, OkHttp/Retrofit | Chốt — D-13 |
| 019 | Offline-first, `outbox_ops` | Chốt |
| 020 | Cắt scope MVP | Chốt |

## 4. Những điều bạn cần biết ngay

- **Rủi ro lớn nhất** là OEM kill dịch vụ nền → làm spike máy thật (M1) trước backend.
- **Kafka không bắt buộc về kỹ thuật** ở quy mô này; giữ vì mục tiêu học, có lối thoát.
- **Không có tuyên bố "không mất dữ liệu"**; xem các cửa sổ mất dữ liệu ở `16` §3.
- **Nút thắt capacity thật**: băng thông ra hàng tháng (~147 GB/tháng ở kịch bản xấu) và đĩa PG — không phải CPU/RAM (`13` §4).
- Nhiều nguồn chính thức **không truy cập được** từ môi trường soạn tài liệu (xem `research/unverified.md`); các điểm đó mang nhãn `[CHƯA XÁC MINH]`.

## 5. Kết quả tự kiểm (checklist mục 0.6)

Ngày kiểm: 2026-10-10. Phương pháp ghi cạnh từng mục.

- [x] **Đủ 18 mục của phần 11 có file tương ứng.** Bảng §1: mục 1–2→`01`; 3→`02`; 4–5→`03`; 6→`04`; 7→`05`; 8→`06`; 9→`07`; 10→`08`; 11→`09`; 12→`10`; 13→`11`; 14→`13`; 15→`14`; 16–17→`15`; 18→`16`. Thêm `12`, `17`, phụ lục, research.
- [x] **Mọi `type` message trong `07` xuất hiện trong ít nhất một sequence hoặc được giải thích.** Đối chiếu bằng `grep` từng type với `03` (`hello`, `welcome`, `auth.refresh`, `trip.join`, `trip.joined`, `location.*`, `presence.event`, `trip.event`, `sos.*`, `event.ack`, `resync.required`, `ack` đều có). `trip.leave`, `error`, `closing`, `ping`, `pong` không có sơ đồ riêng và được giải thích ở `07` §10.
- [x] **Mọi bảng ERD có endpoint/consumer; mọi endpoint trỏ về bảng có thật.** 15/15 bảng DDL có dòng trong `05` §9 (kiểm bằng script so sánh danh sách `CREATE TABLE` với bảng §9). Cột "Bảng" trong `06` §2 trỏ tới các bảng này; ngoại lệ không dùng DB (`/config`, `/telemetry/client`, health, metrics, `/ws`) được nêu rõ trong `06` §2.9.
- [x] **Mọi Kafka topic có producer, consumer, partition key, retention, DLQ.** Bảng `09` §2 (3 topic, mỗi topic đủ 5 thuộc tính; DLQ riêng cho cả ba).
- [x] **Mọi Redis key có TTL hoặc lý do.** `08` §2: 6 key đều có TTL (900 s, 3600 s, 45 s, 900 s, theo cửa sổ, 2× chu kỳ); Pub/Sub channel không phải key và được mô tả ở §3.
- [x] **Mọi ADR được tham chiếu ở ít nhất một file khác; không mâu thuẫn.** Đếm bằng `grep` ADR-001…020 ngoài `02` (≥ 1 file mỗi ADR). Đã rà và sửa các mâu thuẫn tìm thấy: (a) kết nối WS theo `(user, device)` ↔ một kết nối/user — đã thống nhất theo user; (b) `clock_offset` tính ở client ↔ server — đã thống nhất server tính từ `hello.client_time_ms`; (c) bắt đầu tracking khi offline — đã thống nhất "cần mạng" (`11`, ADR-020); (d) routing outbox `sos_*` — nêu rõ ở `09`. *Giới hạn:* đây là rà soát bằng đọc/grep, không có công cụ kiểm tự động mâu thuẫn ngữ nghĩa.
- [x] **Cú pháp Mermaid đã kiểm.** Cài được `@mermaid-js/mermaid-cli`; chạy `mmdc` (Chromium có sẵn) render thành công **11/11** khối Mermaid (`03`×5, `05`, `10`×2, `12`, `13`, `15`). Công cụ nằm ngoài repo.
- [x] **Không còn `TODO`/`TBD` trần; mọi điểm mở nằm trong `16` với nhãn.** `grep -E 'TODO|TBD|FIXME'` không còn kết quả ngoài ngữ cảnh quy tắc. Các `[CẦN XÁC NHẬN]` được gom thành D-01…D-19 ở `16` §1. *Giới hạn:* một số `[CẦN XÁC NHẬN]` nằm rải rác trong file kỹ thuật có đối ứng D-xx tương ứng, không phải mọi câu đều liệt kê lại.
- [x] **Không có khẳng định "không mất dữ liệu"/"exactly-once" chưa chứng minh.** `grep` các cụm này: mọi lần xuất hiện là phủ định hoặc nêu giới hạn (`02` ADR-005/007, `09`, `16` §3). Lưu ý trung thực: các thuộc tính at-least-once/idempotent ở trên là **thiết kế cần được kiểm chứng bằng test A7/A8 (`14`)**, chưa được chứng minh bằng thực nghiệm.

### Giới hạn của bộ tài liệu (cần đọc)
- Số liệu capacity (`13` §4) là tính từ giả định, **chưa đo**.
- Các nguồn bị chặn (Google Play policy, OSM tile policy, tài liệu Kafka/Firebase/MapLibre, văn bản luật gốc…) chưa xác minh trực tiếp — xem `research/unverified.md`.
- Phiên bản thư viện Go lấy từ Go module proxy; thư viện Android và image hạ tầng chưa ghim.
- Không có kiểm tra pháp lý; mọi mục pháp lý cần luật sư xác nhận (D-08).
