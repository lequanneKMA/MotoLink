# MotoLink

Nhóm đi phượt chia sẻ vị trí thời gian thực, ghi hành trình, SOS. Thiết kế đầy đủ ở [`docs/design/`](docs/design/00-README.md);
tiến độ và những gì **chưa kiểm chứng** ở [`docs/design/PROGRESS.md`](docs/design/PROGRESS.md).

```
server/   Go modular monolith (API + WebSocket + worker)      android/  Kotlin + Compose (+ :logic JVM thuần)
deploy/   docker compose (Postgres, Redis, Kafka, server)     docs/     thiết kế, ADR, giao thức
```

## Chạy backend cục bộ (Docker)

```bash
cp deploy/.env.example deploy/.env      # sửa mật khẩu nếu muốn
make up                                  # postgres + redis + kafka + server (migrate tự chạy)
curl localhost:8080/readyz
```

Không muốn Kafka (giống profile Render free, ADR-021): đặt `HISTORY_SINK=direct_pg`, `OUTBOX_SINK=inproc` trong `deploy/.env`
và chạy `docker compose -f deploy/docker-compose.yml up -d --build postgres redis server`.

## Test

```bash
make test-int                 # Go, cần Postgres (TEST_DATABASE_URL) và tùy chọn Redis (TEST_REDIS_ADDR)
android/scripts/test-logic.sh # 52 test JVM của :logic, không cần Android SDK
```

Hợp đồng REST giữa server và app: `server/testdata/contract/*.json` được sinh bởi
`go test ./internal/httpapi -run TestGoldenContract -update`; `android/app/src/test/.../ContractTest.kt` giải mã chúng bằng DTO của app.

## Chạy app Android

Mở thư mục `android/` bằng Android Studio (JDK 17+, SDK 36), để Gradle sync (một số phiên bản thư viện androidx/AGP/Firebase
**chưa xác minh**, nâng lên bản ổn định mới nhất nếu thiếu). Emulator tự trỏ `http://10.0.2.2:8080/`; máy thật đặt
`api.baseUrl=http://<IP-LAN>:8080/` trong `android/local.properties`. Push FCM chỉ bật khi có `android/app/google-services.json`.
