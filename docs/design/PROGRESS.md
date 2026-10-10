# PROGRESS — nhật ký tiến độ bộ tài liệu thiết kế MotoLink

Nhánh làm việc: `docs/motolink-architecture`. Prompt gốc: `docs/prompt/MotoLink-planning-prompt.md` (mục 0). Trạng thái: **hoàn tất Phase A–E**, chờ chủ dự án duyệt (xem `16` §1 và `17` §1).

## Trạng thái phase

- [x] Phase A — Khảo sát repo (chỉ có `docs/prompt/`, không code/README/CLAUDE.md) + tra cứu nguồn → `research/`
- [x] Phase B — `02-adr`, `05-data-model`, `07-websocket-protocol`
- [x] Phase C — 08, 09, 10, 06, 11, 12, 13, 14, 03, 04, 01, 15, 16, 17, 00, `appendix-CLAUDE.md`
- [x] Phase D — review chéo (checklist 0.6 ghi ở cuối `00-README.md`); sửa mâu thuẫn
- [x] Phase E — push nhánh `docs/motolink-architecture` (không tự merge, không tạo PR)

## Nhật ký

- 2026-10-10 Phase A: repo trống → chỉ tạo `docs/design/`, không sửa ngoài `docs/`. Môi trường chặn nhiều host (xem `research/unverified.md`). Tra được: Play targetSdk (API 36 từ 31/08/2026), FGS `location`, luồng quyền nền Android, phiên bản thư viện Go (proxy.golang.org), MapLibre Android 13.6.1 (Maven Central), tag Kafka image, Luật BVDLCN 91/2025/QH15.
- Phase B/C: viết theo thứ tự prompt 0.2; mỗi file một commit.
- Phase D: grep ADR→file, bảng↔endpoint, WS type↔sequence, TTL Redis, topic Kafka, TODO/TBD, "exactly-once". Phát hiện & sửa: kết nối WS theo user (không theo device); tính `clock_offset` ở server; offline-start cần mạng; thiếu `auth.refresh` trong sequence S2; routing outbox `sos_*`; endpoint không có bảng (`/telemetry/client`, `/config`) cần ghi chú; cụm "TODO trần" trong 15 bị grep nhầm. Mermaid: `mmdc` render 11/11 khối OK.

## Quyết định chốt sớm

Xem `02-adr.md` (bảng tổng hợp) và `00-README.md` §3. Quyết định cần chủ dự án: `16` §1 (D-01…D-19).

## Việc còn lại

Không còn việc thiết kế bắt buộc. Việc tiếp theo là của chủ dự án: duyệt/điều chỉnh các quyết định ở `16` §1, chuẩn bị máy thật cho spike M1, sau đó bắt đầu theo `17`.
