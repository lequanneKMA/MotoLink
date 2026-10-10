# CLAUDE.md (đề xuất cho giai đoạn code — chép vào gốc repo khi bắt đầu M0-T01)

## Mục tiêu
MotoLink: app Android cho nhóm đi xe máy chia sẻ vị trí realtime, ghi hành trình, SOS. Sản phẩm thật, 1 developer, 1 VPS (2 vCPU/4 GB). Ưu tiên: vị trí đủ mới, riêng tư, không nghẽn khi client chậm, lịch sử không nhân đôi.

## Stack đã chốt (xem docs/design/02-adr.md)
Go modular monolith (net/http, coder/websocket, pgx+sqlc, goose, go-redis, franz-go, golang-jwt, slog) · PostgreSQL (không PostGIS) · Redis · Kafka KRaft 1 node · Caddy · Prometheus+Grafana · Android Kotlin/Compose/Room/OkHttp+Retrofit/WorkManager/FCM/MapLibre.

## Cấu trúc repo
`server/` (cmd/, internal/{platform,identity,trip,tracking,realtime,sos,notify,eventbus,httpapi,jobs}, db/) · `android/` (:app) · `deploy/` · `contract-tests/` · `docs/design/`.

## Lệnh
- Dev stack: `docker compose -f deploy/docker-compose.yml up -d`
- Build/test Go: `cd server && go build ./... && go test ./... -race` · Lint: `golangci-lint run` · `govulncheck ./...`
- Migration: `go run ./cmd/server migrate up` · sqlc: `sqlc generate`
- Android: `./gradlew :app:assembleDebug :app:testDebugUnitTest`

## Quy tắc bắt buộc
1. **Đọc contract trong `docs/design/` trước khi code; không tự đổi contract.** Muốn đổi: cập nhật tài liệu + ADR trước, rồi mới code.
2. Nguồn sự thật: `05` schema · `06` REST · `07` WebSocket · `08` Redis · `09` Kafka · `10` concurrency · `02` ADR.
3. **Không log tọa độ** (lat/lng/points/route), kể cả mức debug; không để tọa độ vào metric label hay payload push.
4. Concurrency: không goroutine không giới hạn; mọi goroutine có owner, `context`, điều kiện thoát; chỉ người gửi duy nhất được `close(ch)`; mọi channel có capacity và chính sách đầy; `go test -race` bắt buộc.
5. Error handling: bọc lỗi với ngữ cảnh (`%w`), không nuốt lỗi; lỗi trả client theo `06` §1.4; không panic ngoài `main`.
6. Authorization: mọi REST handler và mọi WS message kiểm quyền qua hàm `Authorize` duy nhất; không tin `trip_id` từ client sau lần join; tài nguyên không có quyền trả `404`.
7. Ghi quan trọng phải idempotent (`Idempotency-Key`, `client_sos_id`, `(session_id, seq)`); event bền ghi `trip_events` + `outbox` cùng transaction.
8. Thứ tự vị trí chỉ theo `(session_id, seq)`; không dùng `client_ts` để xếp thứ tự.
9. Không tuyên bố exactly-once / "không mất dữ liệu"; mô tả đúng mức bảo đảm (xem ADR-005).
10. Đặt tên: Go chuẩn (gói chữ thường, không underscore); JSON `snake_case`; Kafka topic `motolink.<miền>.v<n>`; Redis key `ml:<nhóm>:...`.
11. Không thêm dependency/công nghệ mới mà không có ADR. Không PostGIS, không microservice, không OTel ở MVP.
12. Thay đổi hành vi phải có test; test hạ tầng dùng testcontainers (không miniredis cho Lua/ordering).
13. Secret không vào git; `.env.example` không có giá trị thật.
14. Cập nhật `docs/design/PROGRESS.md` (hoặc changelog dự án) sau mỗi task hoàn thành.
