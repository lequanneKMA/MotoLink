# 08 — Redis: key, channel, TTL, script ordering

Tham chiếu: ADR-004, ADR-005, ADR-006. Redis phiên bản `[CẦN XÁC NHẬN]` (thiết kế không dùng tính năng mới: chỉ string/hash/zset, Lua, Pub/Sub). Redis **không** là nguồn sự thật cho bất kỳ dữ liệu nào: mất toàn bộ Redis chỉ làm mất trạng thái tạm (vị trí mới nhất, presence), tự phục hồi khi client gửi điểm kế tiếp.

## 1. Quy ước đặt tên

`ml:<nhóm>:<thành phần...>`, dấu `:` phân cấp, ID là UUID dạng chữ thường. Thành phần trong `{}` là **hash tag** (để nếu sau này chuyển Redis Cluster, các key của cùng trip nằm cùng slot và Lua nhiều key vẫn chạy). Đơn instance Redis ở MVP nên hash tag chỉ là dự phòng.

## 2. Bảng key (mọi key có TTL hoặc lý do không có)

| Key | Kiểu | Giá trị | TTL | Ghi bởi | Đọc bởi |
|---|---|---|---|---|---|
| `ml:loc:{<trip_id>}:<user_id>` | HASH | `sid` (session_id), `seq`, `lat`, `lng`, `acc`, `spd`, `hdg`, `cts` (recorded_at ms), `sts` (server_ts ms) | **900 s** (15 phút), làm mới mỗi lần ghi được chấp nhận | Lua `loc_set` | Snapshot (`MGET`-tương đương bằng pipeline `HGETALL`) |
| `ml:pres:{<trip_id>}` | ZSET | member = `user_id`, score = thời điểm hết hạn presence (ms, giờ Redis) | **3600 s**, làm mới mỗi lần `ZADD` (key rỗng thì tự biến mất) | Lua `pres_touch` | `pres_list`, `pres_reap` |
| `ml:conn:<user_id>` | STRING | `<instance_id>\|<conn_id>` — kết nối hiện hành duy nhất của user | **45 s**, làm mới mỗi pong/ping | Lua `conn_claim`, `conn_touch` | Kiểm tra thay thế kết nối |
| `ml:revoked:sid:<auth_sid>` | STRING | `1` — auth session đã bị thu hồi | **900 s** (bằng TTL access token) | Handler logout/đổi mật khẩu | Middleware REST + WS (từ chối nhanh không cần PG) |
| `ml:rl:<scope>:<id>:<bucket>` | STRING (counter) | số lần | bằng cửa sổ (ví dụ 60 s, 900 s) | `INCR`+`EXPIRE` (Lua) | Rate limit REST: login theo IP+email, register theo IP, thử mã mời theo user |
| `ml:lock:<job>` | STRING | `instance_id` | `PX` = 2× chu kỳ job (ví dụ 60 s), gia hạn khi chạy | Job scheduler | Job scheduler (chọn một instance chạy job retention/partition/auto-end) |

Không có key nào không TTL. Không dùng `KEYS`; không scan trên đường nóng.

Cấu hình Redis đề xuất (xem `13`): `maxmemory 128mb`, `maxmemory-policy volatile-ttl` (mọi key đều có TTL), tắt persistence (`save ""`, `appendonly no`) — dữ liệu tạm, chấp nhận mất khi restart; `timeout`/`tcp-keepalive` mặc định.

## 3. Channel Pub/Sub

| Channel | Publisher | Subscriber | Payload |
|---|---|---|---|
| `ml:ps:trip:{<trip_id>}` | Mọi instance: ingest vị trí (sau `loc_set`), outbox relay (sự kiện bền), handler REST (fast-path) | Instance nào có ≥ 1 client của trip (ref-count) | Envelope bên dưới |
| `ml:ps:user:<user_id>` | Handler logout/thu hồi, handler đổi membership, `conn_claim` (kết nối mới thay cũ) | Instance đang giữ kết nối của user | `{"kind":"revoke_session"\|"membership_changed"\|"superseded","trip_id"?,"conn_id"?,"origin":…}` |

**Envelope trip channel** (JSON, nhỏ gọn):
```json
{"o":"inst-7f3a","k":"loc","t":"<trip_id>","ts":1760072403100,
 "p":[{"u":"U1","sid":"S1","seq":813,"lat":11.94052,"lng":108.45841,"acc":7.5,"spd":9.4,"hdg":136,"sts":1760072403100}]}
```
`k ∈ {loc, evt, pres, sos}`. `o` = origin instance. Subscriber **bỏ** envelope có `o == instance_id` vì instance đó đã fan-out cục bộ trực tiếp (không đi vòng qua Redis). Với `evt`/`sos`, `p` chứa đúng message `trip.event`/`sos.event` đã dựng sẵn (một lần dựng, N instance chỉ chuyển tiếp).

**Subscribe ref-count**: mỗi Hub shard giữ `map[trip_id]refcount` (single-owner, không race). Client join: `0→1` ⇒ gửi lệnh `SUBSCRIBE` tới goroutine subscriber; client rời: `1→0` ⇒ `UNSUBSCRIBE`. Subscriber goroutine (1/instance) là chủ duy nhất của đối tượng `PubSub` của go-redis. Khi mất kết nối Redis, go-redis tự kết nối lại và subscribe lại; sau khi phục hồi, instance gửi `resync.required{reason:"pubsub_gap"}` tới client của các trip đang giữ (client gọi REST events catch-up; vị trí tự lành ở điểm kế tiếp).

**Giới hạn chi phí**: số channel ≤ số trip có client trên instance; mỗi message đến mọi instance có subscriber của trip. Với ≥ 2 instance và 50 trip: băng thông Pub/Sub ≈ tổng ingest (≈170 msg/s × ~220 B ≈ 37 KB/s) × số instance quan tâm — nhỏ. Nâng cấp khi lớn: channel theo shard, xem ADR-006.

## 4. Lua scripts và thứ tự

### 4.1 `loc_set` — chỉ ghi nếu mới hơn

Thứ tự: so sánh từ điển `(sid, seq)`; `sid` là UUIDv7 chữ thường (ADR-004) nên so sánh chuỗi = so sánh thời gian cấp session.

```lua
-- KEYS[1] = ml:loc:{trip}:user
-- ARGV: sid, seq, lat, lng, acc, spd, hdg, cts, sts, ttl_s
local cur_sid = redis.call('HGET', KEYS[1], 'sid')
if cur_sid then
  local cur_seq = tonumber(redis.call('HGET', KEYS[1], 'seq'))
  local new_seq = tonumber(ARGV[2])
  if cur_sid > ARGV[1] or (cur_sid == ARGV[1] and cur_seq >= new_seq) then
    return 0                         -- cũ hoặc trùng: bỏ qua
  end
end
redis.call('HSET', KEYS[1], 'sid', ARGV[1], 'seq', ARGV[2], 'lat', ARGV[3], 'lng', ARGV[4],
           'acc', ARGV[5], 'spd', ARGV[6], 'hdg', ARGV[7], 'cts', ARGV[8], 'sts', ARGV[9])
redis.call('EXPIRE', KEYS[1], tonumber(ARGV[10]))
return 1                             -- đã ghi: mới nhất
```
Kết quả `1` ⇒ phát live (Hub + Pub/Sub). Kết quả `0` ⇒ **không phát**, nhưng điểm vẫn đi nhánh bền (ADR-005). Đây là cơ chế khiến "vị trí cũ không ghi đè mới" cả khi hai instance cùng xử lý điểm của một user (ví dụ sau reconnect sang instance khác trong lúc gói cũ còn bay).

Ghi chú: `sts` là thời điểm **server** nhận (giờ instance, ms). Giữa các instance giờ có thể lệch vài ms — không dùng `sts` để quyết định thứ tự.

### 4.2 Presence

- `conn_claim(user, instance, conn)`: `SET ml:conn:<user> "<inst>|<conn>" EX 45`, trả giá trị cũ. Nếu giá trị cũ khác rỗng và khác `conn` ⇒ publish `ml:ps:user:<user>` `{kind:"superseded", conn_id: <cũ>}` để instance giữ kết nối cũ đóng nó với `4409`.
- `pres_touch(trip, user)` (Lua để dùng giờ Redis): `local t=redis.call('TIME'); ZADD ml:pres:{trip} (t_ms+45000) user; EXPIRE ml:pres:{trip} 3600`. Gọi mỗi pong cho **từng trip** mà kết nối đã join (≤ vài trip) và khi `trip.join`. Nếu `ZADD` làm user từ "không online" thành online (user chưa có member hoặc score < now) ⇒ trả `1` ⇒ phát `pres online`.
- `conn_leave(user, conn)`: nếu `GET ml:conn:<user> == "<inst>|<conn>"` thì `DEL` và với mỗi trip: `ZREM` + trả danh sách đã gỡ ⇒ phát `pres offline`. Nếu giá trị khác (đã bị thay thế) thì **không** làm gì (tránh gỡ presence của kết nối mới).
- `pres_reap(trip)` (janitor, mỗi 10 s, chỉ cho trip có client trên instance): `ZRANGEBYSCORE ml:pres:{trip} -inf now` rồi `ZREM` các member đó (một Lua, atomic); mỗi member **thực sự bị `ZREM` bởi lần gọi này** ⇒ instance này phát `pres offline`. Chống phát trùng nhiều instance vì `ZREM` chỉ trả 1 một lần. Giải quyết trường hợp instance sập (không chạy `conn_leave`): instance khác có client cùng trip sẽ reap sau ≤ 45 s + 10 s.
- `pres_list(trip)`: `ZRANGEBYSCORE ml:pres:{trip} now +inf`.

Một user một kết nối WS tại một thời điểm `[GIẢ ĐỊNH]` (kết nối mới thay kết nối cũ). Đa thiết bị đồng thời cho cùng tài khoản nằm ngoài MVP (ADR-020).

### 4.3 Rate limit

`rl_incr(key, ttl)`: `local n=redis.call('INCR',K); if n==1 then redis.call('EXPIRE',K,ttl) end; return n`. Cửa sổ cố định, chấp nhận độ chính xác thấp. Rate limit **điểm GPS** làm trong bộ nhớ (token bucket mỗi session trong goroutine ingest) — không đi qua Redis để tránh một round-trip mỗi điểm.

## 5. Hành vi khi Redis không khả dụng

| Chức năng | Hành vi suy giảm |
|---|---|
| Ghi vị trí | `loc_set` timeout (100 ms) hoặc lỗi → circuit breaker mở sau 3 lỗi/5 s trong 10 s. Khi mở: **bỏ qua kiểm tra Lua**, dùng bộ nhớ Hub làm "latest" cục bộ (mỗi shard giữ `lastLoc[trip][user]`) để vẫn fan-out **cùng instance**; **không** publish liên instance. Nhánh Kafka không bị ảnh hưởng. |
| Viewer ở instance khác | Ngừng thấy vị trí mới của người ở instance này cho đến khi Redis phục hồi; hiển thị "mất cập nhật" theo tuổi vị trí. |
| Snapshot khi join | Dựng từ bộ nhớ Hub (chỉ phần của instance này) + presence suy từ kết nối cục bộ. Ghi cờ `degraded:true` trong `trip.joined.payload` (field thêm, tương thích). |
| Presence | Giữ trạng thái cục bộ; khi phục hồi, mỗi instance chạy lại `conn_claim` + `pres_touch` cho mọi kết nối cục bộ ("re-assert"). |
| Rate limit REST | Fail-open có giới hạn: cho qua nhưng áp trần bộ nhớ cục bộ theo IP; ghi metric. Riêng **login/đăng ký** fail-closed sau ngưỡng toàn cục để tránh brute-force khi Redis chết `[GIẢ ĐỊNH]`. |
| Thu hồi session (`ml:revoked`) | Tra PG (`refresh_tokens.revoked_at` theo `sid`) khi Redis chết — chậm hơn nhưng đúng. |
| Job lock | Không chạy job định kỳ (retention/partition) khi không lấy được lock; cảnh báo nếu > 1 giờ. |

Metric: `redis_up`, `redis_cmd_duration_seconds{cmd}`, `redis_circuit_state`, `pubsub_reconnects_total`, `pubsub_publish_errors_total` (xem `13`).

## 6. Không dùng Redis cho

Lịch sử hành trình, SOS, trip events (PG/Kafka). Pub/Sub **không** là queue bền: mất message khi subscriber ngắt là hành vi dự kiến, và mọi luồng bền có đường bù (event_seq + REST).

## 7. Kiểm thử bắt buộc (tham chiếu `14`)

Test `loc_set` bằng **Redis thật** (testcontainers), không dùng miniredis: thứ tự `(sid, seq)`, hai goroutine ghi đồng thời, sid mới hơn thay sid cũ dù `seq` nhỏ hơn, TTL được đặt, điểm trùng trả 0.
