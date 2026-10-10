# Nguồn đã tra cứu

Ngày truy cập: **2026-10-10** (môi trường chạy agent). Nhãn theo mục 0.4 của prompt. Nguồn không truy cập được nằm ở [`unverified.md`](unverified.md).

## 1. Chính sách / nền tảng Android

| # | Nguồn | Kết luận | Nhãn |
|---|---|---|---|
| S1 | https://developer.android.com/google/play/requirements/target-sdk | Từ **31/08/2026**, app mới và bản cập nhật phải nhắm **Android 16 (API 36)** trở lên để được đăng lên Google Play (trừ Wear OS/Automotive: API 35; TV/XR: API 34). Có thể xin gia hạn đến **01/11/2026**. → MotoLink: `targetSdk = 36`. | `[ĐÃ XÁC MINH: S1, 2026-10-10]` |
| S2 | https://developer.android.com/develop/background-work/services/fgs/service-types | Từ Android 14 (API 34) mỗi foreground service phải khai báo type. Type `location` cần permission manifest `FOREGROUND_SERVICE_LOCATION`; runtime cần ít nhất `ACCESS_COARSE_LOCATION` hoặc `ACCESS_FINE_LOCATION` đã cấp; **không thể tạo FGS `location` khi app đang ở nền** trừ khi đã có `ACCESS_BACKGROUND_LOCATION`. | `[ĐÃ XÁC MINH: S2, 2026-10-10]` |
| S3 | https://developer.android.com/develop/sensors-and-location/location/permissions/background | Android 10: hộp thoại có "Allow all the time". Android 11+ (API 30+): hộp thoại hệ thống **không** có lựa chọn này, người dùng phải vào trang Settings; app hiển thị rationale kèm nhãn từ `getBackgroundPermissionOptionLabel()`; người từ chối vẫn phải dùng được app. Android 12+: nếu người dùng chỉ cấp vị trí xấp xỉ ở foreground thì background cũng chỉ xấp xỉ. | `[ĐÃ XÁC MINH: S3, 2026-10-10]` |

## 2. Go ecosystem — phiên bản mới nhất và ngày phát hành (Go module proxy)

Truy vấn `https://proxy.golang.org/<module>/@latest` ngày 2026-10-10. Đây là bằng chứng về **tình trạng bảo trì** (ngày phát hành gần nhất), không phải đánh giá chất lượng.

| Module | Latest | Ngày phát hành | Nhận xét |
|---|---|---|---|
| `github.com/coder/websocket` | v1.8.15 | 2026-06-15 | Còn phát hành |
| `github.com/gorilla/websocket` | v1.5.3 | 2024-06-14 | Không có bản mới > 2 năm; vẫn dùng được nhưng không chọn |
| `github.com/twmb/franz-go` | v1.22.1 | 2026-09-27 | Rất tích cực |
| `github.com/twmb/franz-go/pkg/kadm` | v1.19.0 | 2026-09-18 | Admin helper (lag) |
| `github.com/segmentio/kafka-go` | v0.4.51 | 2026-04-23 | Còn phát hành, ít tính năng hơn |
| `github.com/IBM/sarama` | v1.61.1 | 2026-09-27 | Tích cực; API phức tạp hơn |
| `github.com/confluentinc/confluent-kafka-go/v2` | v2.16.0 | 2026-10-07 | Bản cgo (librdkafka) — bị loại vì cgo |
| `github.com/jackc/pgx/v5` | v5.11.0 | 2026-09-07 | |
| `github.com/sqlc-dev/sqlc` | v1.31.1 | 2026-04-22 | |
| `github.com/pressly/goose/v3` | v3.28.0 | 2026-09-02 | |
| `github.com/golang-migrate/migrate/v4` | v4.20.1 | 2026-09-09 | Phương án thay thế |
| `github.com/go-chi/chi/v5` | v5.3.2 | 2026-08-20 | Phương án thay thế router |
| `github.com/redis/go-redis/v9` | v9.23.0 | 2026-10-05 | |
| `github.com/golang-jwt/jwt/v5` | v5.3.1 | 2026-01-28 | |
| `github.com/prometheus/client_golang` | v1.25.0 | 2026-10-08 | |
| `github.com/testcontainers/testcontainers-go` | v0.44.0 | 2026-08-07 | |
| `go.opentelemetry.io/otel` | v1.47.0 | 2026-10-02 | Hoãn tracing sang sau MVP |
| `github.com/alicebob/miniredis/v2` | v2.39.0 | 2026-09-02 | Chỉ cho unit test; **không** dùng test Lua/ordering thật (dùng Redis thật qua testcontainers) |
| `firebase.google.com/go/v4` | v4.22.0 | 2026-09-23 | FCM Admin SDK |

Toolchain trong môi trường soạn tài liệu: Go 1.24.7. Phiên bản Go dùng cho dự án: `[CẦN XÁC NHẬN]` (khuyến nghị bản stable mới nhất tại thời điểm bắt đầu code).

Nhãn chung cho bảng trên: `[ĐÃ XÁC MINH: https://proxy.golang.org, 2026-10-10]`.

## 3. Android / bản đồ / Kafka image

| # | Nguồn | Kết luận | Nhãn |
|---|---|---|---|
| S4 | https://repo1.maven.org/maven2/org/maplibre/gl/android-sdk/maven-metadata.xml | `org.maplibre.gl:android-sdk` bản ổn định (không pre) mới nhất: **13.6.1**; metadata cập nhật 2026-10-08. Có bản pre-release `13.7.0-pre-globe.0`. | `[ĐÃ XÁC MINH: S4, 2026-10-10]` |
| S5 | https://hub.docker.com/v2/repositories/apache/kafka/tags | Image `apache/kafka` có tag không-rc đến **4.3.1**, 4.3.0, 4.2.2; có rc của 4.3.2 và 4.4.0. Tag `latest` tồn tại. Chưa đọc release notes nên không rõ 4.3.1 là bản stable mới nhất chính thức. | `[ĐÃ XÁC MINH: S5, 2026-10-10]` về sự tồn tại tag; còn lại `[CHƯA XÁC MINH]` |

## 4. Pháp lý (không phải tư vấn pháp lý)

| # | Nguồn | Kết luận | Nhãn |
|---|---|---|---|
| S6 | https://congbao.chinhphu.vn/van-ban/luat-so-91-2025-qh15-45578.htm ; https://bocongan.gov.vn/chinh-sach-phap-luat/bai-viet/luat-bao-ve-du-lieu-ca-nhan-chinh-thuc-co-hieu-luc-thi-hanh-tu-ngay-01-01-2026-1767186124 | **Luật Bảo vệ dữ liệu cá nhân số 91/2025/QH15**, ban hành 26/06/2025, hiệu lực **01/01/2026** (theo kết quả tìm kiếm trỏ tới Công báo và Bộ Công an; chưa đọc toàn văn). | `[ĐÃ XÁC MINH: S6, 2026-10-10]` (số hiệu, ngày hiệu lực); nội dung điều khoản `[CHƯA XÁC MINH]` |
| S7 | Kết quả tìm kiếm (luatvietnam.vn, lexdata.vn) | **Nghị định 356/2025/NĐ-CP** (31/12/2025) hướng dẫn Luật. Các nguồn **mâu thuẫn** về việc Nghị định 13/2023/NĐ-CP còn hiệu lực hay đã hết hiệu lực từ 01/01/2026. | `[CHƯA XÁC MINH]` → `[CẦN XÁC NHẬN]` với luật sư |

## 5. Ghi chú phương pháp

- WebFetch/curl bị chặn cho một số host (xem `unverified.md`). Mọi thông tin phụ thuộc các host đó được gắn `[CHƯA XÁC MINH]`.
- Số liệu capacity ở `13-deploy-observability.md` là **tính toán từ giả định**, không phải đo đạc; được gắn `[GIẢ ĐỊNH]` và có kế hoạch đo ở `14`.
