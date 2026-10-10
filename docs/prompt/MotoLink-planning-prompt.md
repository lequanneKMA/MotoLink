# MotoLink — Prompt thiết kế kiến trúc (v2, dùng cho Claude Code)

> Phần 0–2 là chỉ dẫn vận hành cho agent. Phần 3 trở đi là yêu cầu sản phẩm và kỹ thuật.
> Các dòng đánh dấu `<-- SỬA NẾU KHÁC` là giả định của chủ dự án, hãy dùng đúng giá trị đó.

---

## 0. Giao thức thực thi (đọc trước, tuân thủ tuyệt đối)

**Nhiệm vụ:** phân tích và viết bộ tài liệu thiết kế kỹ thuật + kế hoạch triển khai cho MotoLink. **Chưa viết implementation code.**

### 0.1 Đầu ra là các file, không phải một câu trả lời dài

Tài liệu rất lớn nên **không in toàn bộ vào chat**. Ghi vào thư mục `docs/design/` theo bảng sau, mỗi file xong thì commit riêng.

| File | Nội dung (mục trong phần 11 của prompt này) |
|---|---|
| `00-README.md` | Mục lục, bảng trạng thái quyết định, cách đọc bộ tài liệu |
| `01-summary-scope-stories.md` | (1) Executive summary, phạm vi MVP; (2) User stories + acceptance criteria |
| `02-adr.md` | (3) ADR, mỗi ADR có Context / Decision / Alternatives / Consequences |
| `03-architecture-diagrams.md` | (4) Sơ đồ tổng thể Mermaid; (5) Sequence: location update, reconnect, SOS, event persistence |
| `04-modules.md` | (6) Module/package Go và Android |
| `05-data-model.md` | (7) ERD Mermaid + schema PostgreSQL (DDL phác thảo) |
| `06-rest-api.md` | (8) REST API contract |
| `07-websocket-protocol.md` | (9) WebSocket message contract |
| `08-redis.md` | (10) Redis key/channel, TTL, script ordering |
| `09-kafka.md` | (11) Topic, partition key, schema, consumer, retry, DLQ |
| `10-concurrency.md` | (12) Concurrency model, lifecycle goroutine/channel |
| `11-security-privacy-offline.md` | (13) Security, privacy, pháp lý, offline sync |
| `12-android.md` | Kiến trúc Android, background location, bản đồ |
| `13-deploy-observability.md` | (14) Docker Compose, deploy, metrics, capacity estimation |
| `14-testing-benchmark.md` | (15) Test strategy, benchmark plan |
| `15-roadmap.md` | (16–17) Milestone, task card, Definition of Done |
| `16-risks-open-decisions.md` | (18) Rủi ro, giả định, quyết định cần tôi xác nhận |
| `17-implementation-blueprint.md` | Implementation Blueprint + checklist việc code đầu tiên |
| `PROGRESS.md` | Nhật ký tiến độ để có thể tiếp tục nếu session bị ngắt |
| `research/sources.md` | Mọi nguồn đã tra cứu: URL, ngày truy cập, kết luận |
| `research/unverified.md` | Những thông tin chưa xác minh được |

### 0.2 Thứ tự làm việc (có phụ thuộc, không đảo)

1. **Phase A — Khảo sát.** Kiểm tra repo hiện tại (cấu trúc, README, `CLAUDE.md`, convention). Nếu repo trống hoặc không liên quan đến MotoLink, chỉ tạo `docs/design/`, **không sửa file ngoài `docs/`**, không tạo `go.mod`, không scaffold dự án. Sau đó tra cứu nguồn chính thức (mục 0.4).
2. **Phase B — Nền tảng contract.** Viết trước: `02-adr` (các quyết định cốt lõi), `05-data-model`, `07-websocket-protocol`. Mọi file sau phải tham chiếu theo các tên/field đã chốt ở đây.
3. **Phase C — Các file còn lại**, theo thứ tự: 08, 09, 10, 06, 11, 12, 13, 14, 03, 04, 01, 15, 16, 17, 00.
4. **Phase D — Tự review chéo** (mục 0.6) và sửa mâu thuẫn.
5. **Phase E — Chốt.** Cập nhật `PROGRESS.md`, commit, push nhánh `docs/motolink-architecture`. Không tự merge.

Dùng todo list nội bộ để theo dõi. Sau mỗi file: cập nhật `PROGRESS.md` (file nào xong, quyết định mới, việc còn lại).

### 0.3 Không dừng để hỏi

Điểm chưa rõ thì chọn phương án hợp lý nhất, ghi vào `16-risks-open-decisions.md` kèm nhãn `[GIẢ ĐỊNH]` và tiếp tục. Chỉ dừng nếu bị chặn hoàn toàn về kỹ thuật.

### 0.4 Tra cứu và nhãn độ chắc chắn

Thời điểm hiện tại là tháng 10/2026; dữ liệu huấn luyện có thể đã cũ. Với phiên bản thư viện, Android API/chính sách Google Play, Kafka client, chính sách tile bản đồ, quy định dữ liệu cá nhân: **phải tra cứu tài liệu chính thức** và dẫn URL.

Mọi khẳng định phụ thuộc phiên bản/chính sách phải gắn một nhãn:
- `[ĐÃ XÁC MINH: <url>, <ngày>]`
- `[GIẢ ĐỊNH]`
- `[CẦN XÁC NHẬN]` (cần tôi quyết)
- `[CHƯA XÁC MINH]` (không truy cập được nguồn, kèm lý do, ghi vào `research/unverified.md`)

Nếu môi trường chặn truy cập web, vẫn viết tiếp bằng nhãn `[CHƯA XÁC MINH]`. **Tuyệt đối không bịa số phiên bản, URL, hay trích dẫn chính sách.**

### 0.5 Được và không được viết gì

Được: Mermaid, DDL SQL phác thảo, JSON mẫu, bảng, pseudo-code ngắn (≤ 30 dòng/ví dụ) cho concurrency, Docker Compose phác thảo nằm trong tài liệu.
Không: source code implementation, scaffold project, file build/config thật ngoài `docs/`.

### 0.6 Tiêu chí tự kiểm trước khi kết thúc

Chạy checklist này và ghi kết quả vào cuối `00-README.md`:

- [ ] Đủ 18 mục của phần 11 đã có file tương ứng.
- [ ] Mọi `type` message trong `07` xuất hiện trong ít nhất một sequence diagram hoặc được giải thích vì sao không.
- [ ] Mọi bảng trong ERD có endpoint hoặc consumer sử dụng; mọi endpoint trỏ về bảng có thật.
- [ ] Mọi Kafka topic có producer, consumer, partition key, retention và DLQ (hoặc lý do không cần).
- [ ] Mọi Redis key có TTL hoặc lý do không có TTL.
- [ ] Mọi ADR được tham chiếu ở ít nhất một file khác; không có quyết định nào mâu thuẫn giữa các file.
- [ ] Cú pháp Mermaid đã được kiểm tra; dùng `mermaid-cli` nếu cài được, nếu không thì rà soát thủ công và ghi chú.
- [ ] Không còn `TODO`/`TBD` trần; mọi điểm mở đều nằm trong `16` với nhãn.
- [ ] Không có khẳng định "không mất dữ liệu"/"exactly-once" mà chưa có chứng minh.

---

## 1. Vai trò

Bạn là Senior/Staff Software Engineer, Backend Architect và Android Architect.

Hãy thiết kế **MotoLink**: ứng dụng Android cho nhóm bạn đi xe máy cùng nhau chia sẻ vị trí realtime, theo dõi hành trình và giữ liên lạc trong chuyến đi.

Tôi tự phát triển cả hai phần. Tôi muốn học sâu Go concurrency, networking, distributed systems, event-driven architecture, Android background services và vận hành hệ thống, **nhưng đây phải là sản phẩm có người dùng thực**, không phải demo khoe công nghệ.

Hãy kiểm tra tính khả thi, chủ động phát hiện vấn đề trong yêu cầu, giải thích trade-off và đưa ra quyết định cụ thể. Không đồng ý với mọi đề xuất một cách máy móc. **Nếu một yêu cầu trong prompt này sai, thiếu hoặc mâu thuẫn, hãy nêu rõ và đề xuất sửa** (ghi trong ADR hoặc mục 18).

---

## 2. Ràng buộc đã chốt và giả định nền

| Hạng mục | Giá trị | |
|---|---|---|
| Đội ngũ | 1 developer, vừa làm vừa học | |
| Thị trường | Việt Nam, giao diện tiếng Việt | `<-- SỬA NẾU KHÁC` |
| Hạ tầng MVP | 1 VPS Ubuntu, 2 vCPU / 4 GB RAM, tự host | `<-- SỬA NẾU KHÁC` |
| Quy mô MVP | Nhóm 2–15 người, ~50 trip hoạt động đồng thời, ~500 kết nối | `<-- SỬA NẾU KHÁC` |
| Tần suất GPS | Đang chạy xe: 2–5 giây (thích ứng); đứng yên: 15–30 giây | `<-- SỬA NẾU KHÁC` |
| Phân phối | Google Play (có thể sideload APK khi thử nghiệm) | `<-- SỬA NẾU KHÁC` |
| Thiết bị | Có cả máy Xiaomi/Oppo/Samsung/Vivo phổ biến ở VN; có thể có máy không có Google Play Services | |

Điểm neo để tính capacity: 500 thiết bị × 1 update/3 giây ≈ 167 update/giây vào; với trip 10 người, fan-out ra ≈ 9× số đó. Hãy kiểm chứng giả định này trong `13-deploy-observability.md` (mục capacity estimation: băng thông, RAM Redis, dung lượng Kafka/PostgreSQL mỗi ngày, mức vừa với VPS 4 GB, bao gồm cả Kafka).

---

## 3. Tầm nhìn sản phẩm

MotoLink dành cho nhóm bạn đi phượt, đi chơi hoặc di chuyển cùng nhau bằng xe máy.

### Người dùng có thể

1. Đăng ký, đăng nhập và quản lý hồ sơ. (Chọn phương thức xác thực và nêu lý do, ví dụ email + Google Sign-In so với OTP SMS có chi phí.)
2. Tạo chuyến đi, đặt tên và thiết lập quyền riêng tư.
3. Mời thành viên bằng mã tham gia hoặc lời mời.
4. Xem vị trí hiện tại của tất cả thành viên trên bản đồ.
5. Thấy thành viên nào online, offline hoặc mất cập nhật vị trí.
6. Bắt đầu, tạm dừng, kết thúc ghi nhận hành trình.
7. Xem lại tuyến đường, thời lượng, quãng đường.
8. Gửi SOS kèm vị trí cho các thành viên; **thành viên nhận được cả khi app của họ đang không chạy** (push notification).
9. Nhận thông báo về thành viên mới, chuyến đi và sự kiện quan trọng.
10. Tạm ngừng chia sẻ vị trí của mình mà không rời chuyến đi (nếu quyết định giữ trong MVP, nêu rõ).

Ứng dụng phải chịu được mạng di động không ổn định: client mất kết nối, reconnect và đồng bộ lại.

### Vòng đời trip

Định nghĩa rõ máy trạng thái của trip, của tracking session và của membership (ví dụ: draft/active/paused/ended/archived), điều kiện chuyển trạng thái và ai được phép chuyển.

### Chưa triển khai ban đầu

Không xây turn-by-turn, tối ưu tuyến, dữ liệu giao thông, mạng xã hội, thanh toán, AI, microservices phức tạp, đa tenant. Có thể đề xuất cho phiên bản sau nhưng không để làm phình MVP.

### Định hướng ưu tiên

Dùng được ngoài đời thực; vị trí đủ mới; một client chậm không gây nghẽn hệ thống; lịch sử hành trình lưu có chủ đích và không nhân đôi khi retry; quyền riêng tư của vị trí đặt hàng đầu; chi phí thấp, tự host trên một máy.

---

## 4. Công nghệ và mục tiêu học tập

### Android

Kotlin, Jetpack Compose, Coroutines/Flow, Room, Retrofit hoặc Ktor Client, WebSocket client, WorkManager (cho đồng bộ nền phù hợp), Foreground Service (ghi GPS khi người dùng chủ động bắt đầu), FCM (cho SOS/thông báo khi app không chạy), một thư viện bản đồ.

Phải quyết định và có ADR cho:
- **Nguồn vị trí:** Fused Location Provider (Google Play Services) so với `LocationManager` thuần; chiến lược cho máy không có GMS.
- **Bản đồ:** đánh giá MapLibre Native và các lựa chọn khác cho Việt Nam: nguồn dữ liệu, chi phí, attribution, chính sách tile, giới hạn sử dụng, hỗ trợ offline. **Không giả định OSM tile công cộng dùng miễn phí không giới hạn.** Kiểm tra thêm cách nhà cung cấp hiển thị biên giới/chủ quyền lãnh thổ Việt Nam (Hoàng Sa, Trường Sa) vì đây là vấn đề thực tế với app phát hành tại Việt Nam.
- **minSdk/targetSdk** theo yêu cầu Google Play hiện hành, và phân bố phiên bản Android.

Phải phân biệt rõ xử lý vị trí khi app đang mở / chạy nền / bị Android thu hồi process. Giải thích: luồng xin quyền (foreground → background location, Android 10/11+), yêu cầu foreground service type `location` trên các bản Android mới, thông báo foreground, battery optimization, và **việc các hãng (Xiaomi, Oppo, Vivo, Samsung…) kill service nền**: nêu cái gì khắc phục được và cái gì không thể khắc phục chỉ bằng retry hay WorkManager. Nêu các khai báo bắt buộc trên Google Play cho quyền vị trí nền/foreground service.

### Backend

Golang, REST, WebSocket, PostgreSQL (PostGIS khi thực sự cần truy vấn địa lý), Redis (vị trí mới nhất, TTL, Pub/Sub), Kafka (event stream, lịch sử hành trình, xử lý bất đồng bộ), Docker Compose, Prometheus + Grafana hoặc tương đương, structured logging, tracing nếu hợp lý.

Phải so sánh và chọn (kèm lý do, kiểm tra tình trạng bảo trì hiện tại): thư viện WebSocket (`coder/websocket`, `gorilla/websocket`, …), Kafka client cho Go (`franz-go`, `segmentio/kafka-go`, `sarama`, bản cgo của Confluent, …), truy cập DB (`pgx` + `sqlc` hoặc ORM), migration tool, router HTTP, cách quản lý token (access JWT + refresh rotating). Kafka chạy ở chế độ nào cho 1 VPS (KRaft single node) và tốn bao nhiêu RAM.

Ưu tiên **modular monolith** với module rõ ràng. Không tách microservice chỉ vì có Kafka. Mỗi công nghệ mới phải nêu vấn đề thực tế nó giải quyết.

---

## 5. Go concurrency — phần trung tâm, phân tích sâu

### 5.1 WebSocket connection lifecycle
Cấu trúc quản lý kết nối; read loop và write loop; xác thực connection; heartbeat/ping-pong và phát hiện kết nối chết; register/unregister; client disconnect, reconnect; server shutdown; giới hạn số connection và tài nguyên mỗi client.

### 5.2 Hub và channel
Hub quản lý client theo trip: register/unregister, nhận location update, fan-out tới thành viên cùng trip, nhiều channel có capacity giới hạn, backpressure và slow consumer, không data race, giải phóng goroutine/channel đúng cách.

Phân tích single-owner goroutine so với mutex so với kết hợp (ưu/nhược) và chọn một. **Không tạo goroutine không giới hạn theo số message.** Phải có giới hạn hàng đợi, timeout và cơ chế khi consumer không theo kịp (drop-oldest cho location, đóng connection cho consumer quá chậm, …).

Với mỗi goroutine trong hệ thống nêu: ai tạo, ai sở hữu, ai đóng channel, điều kiện thoát, và cách `context` được truyền.

### 5.3 Xử lý GPS update
Kiểm tra: đúng user/trip/phiên kết nối; tọa độ hợp lệ; timestamp và thứ tự; tần suất tối đa; bản tin cũ/trùng/bất thường; rate limit theo client.

**Quy ước thứ tự (bắt buộc làm rõ):** timestamp thiết bị không đáng tin. Đề xuất bộ định danh `device_id`, `session_id` (mỗi lần bắt đầu tracking), `seq` tăng đơn điệu theo session, cùng `client_ts`, kèm `server_received_ts`. Nêu cách xử lý lệch đồng hồ và cách chọn "bản tin mới hơn" trong Redis (ví dụ Lua script so sánh `(session_id, seq)`) để vị trí cũ không ghi đè vị trí mới.

### 5.4 Fan-out và scale-out
Nhiều Go instance phục vụ thành viên của cùng một trip. Thiết kế Redis Pub/Sub để event instance A nhận đến được client ở instance B: tổ chức channel, subscription theo trip, subscribe/unsubscribe và ref-count, tránh broadcast lặp (nhận lại event của chính mình), cleanup khi hết client, giới hạn chi phí fan-out khi số nhóm/thành viên tăng. Đề xuất phương án MVP đơn giản kèm đường nâng cấp.

---

## 6. Redis và Kafka

Dùng cả hai nhưng **không trùng vai trò**. Vẽ sơ đồ luồng dữ liệu và định nghĩa dữ liệu nào lưu ở đâu.

**Redis:** vị trí hiện tại, presence + TTL, Pub/Sub liên instance, rate limit/dữ liệu tạm. Phân tích key schema, TTL, cập nhật có kiểm soát thứ tự, hành vi khi Redis mất kết nối. Redis Pub/Sub không phải kho lưu event bền vững.

**Kafka:** event của trip, location history cần lưu, consumer ghi PostgreSQL, consumer tính quãng đường/thống kê, consumer thông báo/cảnh báo. Thiết kế topic, event schema, partition key, retention, consumer group, retry, dead-letter topic, theo dõi lag.

Phải trả lời cụ thể từng câu (không liệt kê rồi bỏ lửng):
1. Location update vào Kafka trước hay sau khi broadcast?
2. Mức tin cậy cần đạt cho lịch sử hành trình?
3. Redis thành công mà Kafka thất bại (và ngược lại) thì xử lý ra sao?
4. Go process crash khi event còn trong channel thì có mất dữ liệu không?
5. Retry Kafka có tạo bản ghi trùng không; consumer idempotent bằng cách nào?
6. Consumer restart/replay thì lịch sử được bảo vệ ra sao?
7. Có cần transactional outbox không, khi nào?
8. Gửi từng GPS point vào Kafka hay lấy mẫu/batch để kiểm soát chi phí?

Chọn một kiến trúc MVP cụ thể, bảo vệ nó. Phân biệt at-most-once / at-least-once / exactly-once **ở từng khâu**. Không tuyên bố "không mất dữ liệu" nếu chưa chứng minh được.

**SOS và sự kiện quan trọng:** luồng tiếp nhận có ACK, lưu bền vững (PostgreSQL là nguồn sự thật, không chỉ Pub/Sub), retry, phân phối tới thành viên qua cả WebSocket lẫn FCM, theo dõi trạng thái đã nhận/đã xem, xử lý khi client gửi SOS lúc mất mạng (hàng đợi cục bộ + retry + chống trùng).

---

## 7. Database, API và giao thức message

### 7.1 Data model
ERD và schema PostgreSQL sơ bộ cho: users, session/refresh tokens, device (kèm FCM token), trips, trip_members, trip_invitations/mã tham gia, trip_events, tracking_sessions, location_history, sos_events (+ trạng thái nhận), notification records.

Mỗi bảng nêu cột/kiểu quan trọng, PK, FK, unique, index, quy tắc xóa và lưu trữ, transaction quan trọng. Phân tích khi nào cần PostGIS thay vì lat/lng; cách chia partition hoặc giới hạn `location_history` (không lưu GPS vô hạn): downsampling, retention, partition theo thời gian.

### 7.2 REST API
Endpoint cụ thể cho: authentication, hồ sơ, tạo/quản lý trip, tham gia/rời trip, quản lý thành viên, snapshot trạng thái trip và vị trí, lịch sử hành trình, SOS, đăng ký thiết bị/FCM token, đồng bộ dữ liệu phù hợp. Mỗi endpoint: method, path, quyền, request, response, lỗi chính, và yêu cầu idempotency (header `Idempotency-Key`) cho thao tác ghi quan trọng.

### 7.3 WebSocket protocol
JSON thống nhất với `type`, `event_id` khi cần, `trip_id`, `timestamp`, `payload`, `version`. Message mẫu cho: authentication/connection, join/leave trip, location update, location snapshot + realtime event, presence, ping/pong, SOS, error và ACK.

Phân biệt client→server và server→client; message nào cần ACK, message nào bỏ qua được khi đã lỗi thời. Giải thích reconnect: xác định message đã xử lý (`last_event_id`/cursor), đồng bộ snapshot mà **snapshot cũ không ghi đè event mới hơn**. Có chiến lược xử lý version mismatch giữa app cũ và server mới.

### 7.4 Redis key và Kafka envelope
Ví dụ cụ thể cho Redis key, Pub/Sub envelope, Kafka event envelope, naming convention nhất quán.

---

## 8. Kiến trúc Android

Package/module vừa đủ, tránh chia lớp chỉ để theo pattern. Cần: màn hình bản đồ trip, tạo/tham gia trip, danh sách thành viên + trạng thái, bắt đầu/kết thúc tracking, lịch sử, SOS + trạng thái gửi, cài đặt quyền riêng tư, đăng nhập/session.

Phân tích ViewModel, Repository, local/remote data source, StateFlow.

**Offline-first:** Room là nguồn dữ liệu cục bộ cho UI; dữ liệu nào ghi ngay; dữ liệu nào vào hàng đợi chờ đồng bộ; reconcile khi server trả snapshot; reconnect và mất kết nối lâu; chống ghi trùng cho thao tác quan trọng.

**Bảo mật và quyền riêng tư:** người ngoài trip không được thấy vị trí; chỉ thành viên có quyền mới xem/gửi event cho trip. Thiết kế authorization cho **từng REST endpoint và từng WebSocket message** (không xác thực một lần rồi tin `trip_id` từ client). Xét: token expiration và thu hồi session, mã mời có hạn/giới hạn lượt dùng, TLS, lưu token an toàn trên Android, không log tọa độ chính xác mặc định, giới hạn quyền xem lịch sử, xóa dữ liệu và retention, ngừng thu GPS khi kết thúc tracking.

**Pháp lý (không phải tư vấn pháp lý):** nêu các yêu cầu về dữ liệu cá nhân và đồng ý của người dùng đối với dữ liệu vị trí, theo quy định Việt Nam hiện hành (kiểm tra Nghị định 13/2023/NĐ-CP và văn bản thay thế/bổ sung mới nhất) và chính sách Google Play; đề xuất màn hình consent, privacy policy, quyền xóa dữ liệu. Gắn nhãn `[CẦN XÁC NHẬN]` với luật sư/nguồn chính thức.

---

## 9. Triển khai và quan sát

Docker Compose local cho: Go server, PostgreSQL/PostGIS, Redis, Kafka (+ phụ thuộc). Đề xuất `.env`, config theo môi trường, migration, health/readiness, graceful shutdown. Chạy trên 1 Ubuntu VPS, sau đó nhiều Go instance để kiểm thử scale-out. Có mục backup/restore PostgreSQL và reverse proxy + TLS.

Metrics tối thiểu: active WebSocket connections; GPS updates/giây; message processing latency; realtime fan-out latency; queue depth, dropped/stale updates; Redis latency và Pub/Sub failures; Kafka producer errors/retries và consumer lag; PostgreSQL write latency; CPU/RAM/goroutine count. Phân biệt metrics phục vụ debug với metrics phản ánh chất lượng sản phẩm (ví dụ "tuổi vị trí hiển thị cho người xem", tỷ lệ SOS giao thành công).

---

## 10. Roadmap theo milestone

Mỗi milestone ghi: mục tiêu, dependency, task backend/Android/infra, kết quả demo được, test bắt buộc, điều kiện nghiệm thu. Cuối mỗi giai đoạn phải có một phiên bản **dùng được**.

Thứ tự đề xuất (đánh giá và điều chỉnh):

- **M0 — Foundation:** domain model, API/WS contract (contract-first), repository layout, local dev, ADR.
- **M1 — Android prototype dùng một mình:** bản đồ, GPS, lưu local, foreground service. *Đưa spike background location lên đầu và kiểm thử trên máy thật (ít nhất một máy Xiaomi/Oppo và một Samsung, chạy ≥ 30–60 phút, màn hình tắt)* vì đây là rủi ro kỹ thuật lớn nhất của sản phẩm.
- **M2 — Backend core:** auth, trip lifecycle, membership, PostgreSQL, REST.
- **M3 — Go realtime:** WebSocket, Hub, heartbeat, backpressure, reconnect, snapshot.
- **M4 — Multi-instance Redis:** vị trí hiện tại, TTL, presence, Pub/Sub qua ≥ 2 instance.
- **M5 — Kafka pipeline:** event, consumer, ghi lịch sử, retry, idempotency, phục hồi khi consumer restart.
- **M6 — Hoàn thiện sản phẩm:** lịch sử/thống kê, SOS (+ FCM), notification, quyền riêng tư, consent.
- **M7 — Reliability & performance:** integration/race/load test, quan sát, backup, deploy VPS.

Mỗi task trong roadmap viết theo **task card** để giao cho coding agent và review độc lập:

```
ID:            M3-T04
Tiêu đề:
Loại:          backend | android | infra | docs
Phụ thuộc:     (ID các task phải xong trước)
Song song với: (ID các task có thể làm cùng lúc)
Input:         (file contract/ADR cần đọc)
Output:        (package/file dự kiến tạo, KHÔNG liệt kê tên function chưa tồn tại)
Acceptance:    (tiêu chí kiểm chứng được)
Lệnh test:
Kích thước:    S | M | L
Checklist review:
```

---

## 11. Định dạng đầu ra

Tiếng Việt, giữ nguyên tên kỹ thuật tiếng Anh khi cần. Bộ tài liệu phải bao phủ 18 mục sau (ánh xạ sang file ở mục 0.1):

1. Executive summary và phạm vi MVP.
2. User stories và acceptance criteria.
3. Các quyết định kiến trúc quan trọng (ADR).
4. Sơ đồ kiến trúc tổng thể (Mermaid).
5. Sequence diagram: location update, reconnect, SOS, event persistence.
6. Module/package của Go và Android.
7. Data model và ERD.
8. REST API contract.
9. WebSocket message contract.
10. Redis key/channel design.
11. Kafka topic, partition, event schema, consumer strategy.
12. Concurrency model và lifecycle goroutine/channel.
13. Security, privacy, offline synchronization.
14. Docker Compose, local development, deployment.
15. Testing strategy và benchmark plan.
16. Roadmap theo milestone, chia task nhỏ theo thứ tự phụ thuộc (đánh dấu task song song được và task phải chờ contract).
17. Definition of Done cho từng milestone.
18. Rủi ro lớn nhất và quyết định cần tôi xác nhận trước khi code.

Mỗi lựa chọn quan trọng đi kèm lý do và trade-off. Với thông tin phụ thuộc phiên bản/chính sách: dẫn link nguồn chính thức (mục 0.4).

---

## 12. Test plan và tiêu chí nghiệm thu

Các lớp test: unit (domain logic); integration (PostgreSQL, Redis, Kafka, dùng testcontainers hoặc tương đương); WebSocket protocol; concurrency + `go test -race`; Android UI/repository; end-to-end nhiều client mô phỏng; load test; chaos/recovery (Redis, Kafka, mạng, restart process).

Mục tiêu kiểm chứng ban đầu (là **mục tiêu test, không phải giả định chắc chắn đạt**):
- 50 thiết bị mô phỏng gửi GPS định kỳ; 500 kết nối trong benchmark mở rộng.
- Hai Go instance fan-out vị trí qua Redis.
- Client reconnect đồng bộ lại snapshot.
- Slow consumer không làm hàng đợi tăng vô hạn.
- Disconnect và shutdown không để lại goroutine vô chủ.
- Kafka consumer restart và xử lý lại event an toàn; không ghi trùng lịch sử khi event bị retry.
- Đo riêng p95/p99 latency, throughput, CPU, RAM, dropped events, consumer lag.

Nêu cách đo, công cụ và **điều kiện môi trường benchmark** (cấu hình máy, mạng, dữ liệu mẫu). Có test thực địa cho Android: pin tiêu hao/giờ, độ chính xác, mất nền do OEM.

---

## 13. Ràng buộc cuối

- Không viết toàn bộ source code.
- Không sa đà giải thích kiến thức cơ bản chung chung.
- Không thêm công nghệ chỉ để kiến trúc trông phức tạp.
- Không chia microservice quá sớm.
- Không coi Redis Pub/Sub là message queue bền vững.
- Không hứa exactly-once hoặc zero data loss nếu chưa chứng minh được.
- Không bỏ qua background location, reconnect, backpressure, security, data lifecycle.
- Không chỉ đưa ra phương án: **hãy chọn và bảo vệ phương án MVP tốt nhất.**
- Kiểm soát scope cho một developer độc lập; cắt bớt thay vì thêm.

Kết thúc bằng **`17-implementation-blueprint.md`**: thứ tự thực hiện chính xác và checklist việc đầu tiên tôi nên code sau khi duyệt thiết kế (kèm danh sách quyết định ở `16` mà tôi phải xác nhận trước).

Tài liệu phải đủ sâu để làm nguồn tham chiếu xuyên suốt quá trình phát triển MotoLink.

---

## Phụ lục A — `CLAUDE.md` đề xuất cho giai đoạn code (agent tạo ở cuối, trong `docs/design/appendix-CLAUDE.md`, ≤ 40 dòng)

Tóm tắt: mục tiêu sản phẩm; stack đã chốt; cấu trúc repo; lệnh build/test/lint; quy ước đặt tên, error handling, logging (không log tọa độ), concurrency (không goroutine không giới hạn, mọi goroutine có owner và context); quy tắc "đọc contract trong `docs/design/` trước khi code, không tự đổi contract; muốn đổi thì cập nhật ADR"; danh sách file/contract là nguồn sự thật.
