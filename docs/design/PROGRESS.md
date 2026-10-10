# PROGRESS — nhật ký tiến độ bộ tài liệu thiết kế MotoLink

Nhánh làm việc: `docs/motolink-architecture`. Prompt gốc: `docs/prompt/MotoLink-planning-prompt.md` (mục 0). Trạng thái: **hoàn tất Phase A–D, Phase E chưa push được (không có remote)**, chờ chủ dự án duyệt (xem `16` §1 và `17` §1).

## Trạng thái phase

- [x] Phase A — Khảo sát repo (chỉ có `docs/prompt/`, không code/README/CLAUDE.md) + tra cứu nguồn → `research/`
- [x] Phase B — `02-adr`, `05-data-model`, `07-websocket-protocol`
- [x] Phase C — 08, 09, 10, 06, 11, 12, 13, 14, 03, 04, 01, 15, 16, 17, 00, `appendix-CLAUDE.md`
- [x] Phase D — review chéo (checklist 0.6 ghi ở cuối `00-README.md`); sửa mâu thuẫn
- [~] Phase E — commit xong trên nhánh `docs/motolink-architecture` (không merge, không PR). **Push chưa thực hiện được: repo không có remote `origin`** (`git remote -v` rỗng); chủ dự án cần thêm remote rồi `git push -u origin docs/motolink-architecture`

## Nhật ký

- 2026-10-10 Phase A: repo trống → chỉ tạo `docs/design/`, không sửa ngoài `docs/`. Môi trường chặn nhiều host (xem `research/unverified.md`). Tra được: Play targetSdk (API 36 từ 31/08/2026), FGS `location`, luồng quyền nền Android, phiên bản thư viện Go (proxy.golang.org), MapLibre Android 13.6.1 (Maven Central), tag Kafka image, Luật BVDLCN 91/2025/QH15.
- Phase B/C: viết theo thứ tự prompt 0.2; mỗi file một commit.
- Phase D: grep ADR→file, bảng↔endpoint, WS type↔sequence, TTL Redis, topic Kafka, TODO/TBD, "exactly-once". Phát hiện & sửa: kết nối WS theo user (không theo device); tính `clock_offset` ở server; offline-start cần mạng; thiếu `auth.refresh` trong sequence S2; routing outbox `sos_*`; endpoint không có bảng (`/telemetry/client`, `/config`) cần ghi chú; cụm "TODO trần" trong 15 bị grep nhầm. Mermaid: `mmdc` render 11/11 khối OK.

- 2026-10-10 Chủ dự án xác nhận: **D-02** đăng nhập cơ bản trước, Google + Facebook sau; **D-04** xin đủ quyền chạy nền (`ACCESS_BACKGROUND_LOCATION`); **D-05** giữ Kafka; **D-18** chấp nhận cửa sổ mất dữ liệu, cải thiện sau; **D-09** dùng backend miễn phí (chưa chọn nhà cung cấp → rủi ro mới R-21, tension với D-05). Đã cập nhật ADR-003/007/013/017, `05` (bỏ `google_sub`, dự phòng `auth_identities`), `11`, `12`, `13`, `15`, `16`, `17`, `00`.
- 2026-10-10 D-09 chốt **Render free**; D-04 xin quyền nền (xác nhận lại). Tra Render qua nguồn thứ cấp (render.com bị chặn) → U14. Thêm ADR-021 (deploy `direct_pg`, Kafka giữ ở dev/test), `13` §7, R-21 cụ thể, sửa roadmap M4-T05/M5-T09/M7-T04.

## Quyết định chốt sớm

Xem `02-adr.md` (bảng tổng hợp) và `00-README.md` §3. Quyết định cần chủ dự án: `16` §1 (D-01…D-19).

## Việc còn lại

Không còn việc thiết kế bắt buộc. Việc tiếp theo là của chủ dự án: duyệt/điều chỉnh các quyết định ở `16` §1, chuẩn bị máy thật cho spike M1, sau đó bắt đầu theo `17`.

## Tiến độ code (nhánh `feat/m0-foundation`)

Môi trường soạn code: không có Docker daemon; kiểm bằng PostgreSQL 16 + Redis 7 cài sẵn (`TEST_DATABASE_URL`, `TEST_REDIS_ADDR`). Go toolchain tự nâng lên 1.26 do dependency yêu cầu (Dockerfile dùng `golang:1.26`; `golangci-lint` cài sẵn build bằng Go 1.25 nên chưa chạy được local — CI dùng bản mới).

| Task | Trạng thái | Ghi chú |
|---|---|---|
| M0-T01 monorepo + CI | xong | `server/`, `deploy/`, `.github/workflows/ci.yml`, `Makefile` |
| M0-T02 compose dev | xong (chưa chạy thật) | `docker compose config` hợp lệ; **chưa `up`** vì sandbox không có Docker daemon — chạy thử trên máy bạn; tên biến `KAFKA_*` của image chưa xác minh (U9) |
| M0-T03 platform | xong | config, logx (redaction), metrics, UUIDv7 |
| M0-T04 migration | xong | `00001_baseline.sql` khớp `05`; test với PG thật |
| M0-T05 golden contract | **chưa** | |
| M0-T06 Android skeleton | **chưa** | cần Android SDK/máy thật |
| M2-T01 identity | xong | register/login/refresh xoay vòng + phát hiện dùng lại/logout/logout-all, max 10 phiên, argon2id có semaphore |
| M2-T02 middleware | xong | request id, recover, access log (không body), auth + thu hồi, rate limit, lỗi problem+json, metrics |
| M2-T03 idempotency | xong | replay, 422 khác body, 409 đồng thời, không cache 5xx, theo user |
| M2-T04.. trip, invitation, member, tracking-session | **chưa** | |

Sai lệch có chủ đích so với tài liệu:
- Truy vấn SQL viết tay bằng `pgx` (chưa dùng `sqlc`, chưa cài); chuyển sang `sqlc` khi số truy vấn tăng.
- Test tích hợp dùng `TEST_DATABASE_URL`/`TEST_REDIS_ADDR` (tự bỏ qua nếu không đặt) thay vì testcontainers; CI cung cấp Postgres service. Redis test trong CI chưa bật.
- `POST /auth/register` không dùng `Idempotency-Key` (xem `06`).
- Bug tìm được khi test: (1) `NoopRevoker` làm phiên đã logout vẫn hợp lệ — sửa để fallback PostgreSQL; (2) khóa log `route` bị redaction nhầm là tọa độ — đổi thành `endpoint` + test hồi quy.
