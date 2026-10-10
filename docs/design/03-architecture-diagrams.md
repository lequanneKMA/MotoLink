# 03 — Sơ đồ kiến trúc và sequence

Tham chiếu: ADR-001, 005, 006, 008, 011, 015. Tên message theo `07`; key theo `08`; topic theo `09`. Cú pháp Mermaid được kiểm ở Phase D (ghi kết quả ở `00-README.md`).

## 1. Sơ đồ tổng thể

```mermaid
flowchart LR
    subgraph Android["Android (Kotlin, Compose)"]
      UI["UI + MapLibre"]
      SVC["TrackingService - FGS location"]
      ROOM[("Room")]
      RT["RealtimeClient - OkHttp WS"]
      REST["Retrofit REST"]
      FCMC["FCM receiver"]
    end

    subgraph Server["motolink-server - modular monolith"]
      direction TB
      API["api: REST + authz + idempotency"]
      WS["ws: Conn + Hub shards + ingest workers"]
      WRK["worker: consumers + outbox relay + jobs"]
    end

    PG[("PostgreSQL")]
    RD[("Redis")]
    KF[("Kafka KRaft")]
    FCMS(("Google FCM"))
    TILES[("Tile source - ADR-014")]

    UI --> ROOM
    SVC --> ROOM
    SVC --> RT
    UI --> RT
    UI --> REST
    UI --> TILES
    FCMS --> FCMC

    RT <-->|"wss /v1/ws"| WS
    REST -->|"https /v1/*"| API

    API --> PG
    API -->|"publish control events"| RD
    WS -->|"latest loc, presence, pub/sub"| RD
    WS -->|"durable branch: location.v1"| KF
    WS --> PG
    API -->|"outbox rows"| PG
    WRK -->|"outbox poll"| PG
    WRK -->|"trip-events.v1, sos.v1"| KF
    WRK -->|"publish to trip channel"| RD
    KF -->|"cg.history-writer"| WRK
    KF -->|"cg.notifier"| WRK
    WRK -->|"insert location_points, notifications"| PG
    WRK -->|"push"| FCMS
```

Phân vai dữ liệu: vị trí live → Redis; lịch sử → Kafka rồi PostgreSQL; trip event/SOS → PostgreSQL (nguồn sự thật) + outbox → Kafka + Redis Pub/Sub.

## 2. S1 — Location update (live + bền, hai instance)

```mermaid
sequenceDiagram
    autonumber
    participant A as Android A (người gửi)
    participant S1 as Server instance 1
    participant R as Redis
    participant K as Kafka
    participant S2 as Server instance 2
    participant B as Android B (viewer)
    participant W as history-writer
    participant P as PostgreSQL

    A->>S1: location.update (session_id, points seq 813..)
    S1->>S1: authz theo trip_id + validate + token bucket
    alt điểm hợp lệ và sharing đang bật
        S1->>R: EVAL loc_set (so sánh sid, seq)
        R-->>S1: 1 nếu mới hơn, 0 nếu cũ hoặc trùng
        opt kết quả 1
            S1->>S1: Hub shard fan-out cục bộ (coalesce)
            S1->>R: PUBLISH ml:ps:trip origin=S1
            R-->>S2: message (S1 bỏ qua echo của chính mình)
            S2->>S2: Hub shard fan-out cục bộ
            S2-->>B: location.event (batch, latest per user)
        end
    end
    par nhánh bền - không chờ nhánh live
        S1->>K: produce location.v1 key=session_id (acks=all, idempotent)
        K-->>S1: ack bản ghi
        S1-->>A: location.ack (up_to_seq, rejected)
        A->>A: xóa pending_point <= up_to_seq
    and consumer
        K->>W: poll lô (500 hoặc 200 ms)
        W->>P: INSERT ... ON CONFLICT DO NOTHING
        P-->>W: COMMIT
        W->>K: commit offset
    end
    Note over S1,B: Presence: Redis ZSET ml:pres, pong làm mới TTL
    S1-->>B: presence.event (online/offline) khi trạng thái đổi
    Note over A,S1: Kafka lỗi: không có location.ack, A giữ điểm và gửi lại, live vẫn chạy
```

## 3. S2 — Reconnect và resume

```mermaid
sequenceDiagram
    autonumber
    participant C as Android
    participant S as Server (instance bất kỳ)
    participant R as Redis
    participant P as PostgreSQL

    Note over C: Mất kết nối - Room giữ pending_point, pending_sos, last_event_seq
    C->>C: backoff 1 đến 30 s có jitter
    C->>S: HTTP upgrade /v1/ws (Authorization Bearer)
    alt access token hết hạn
        S-->>C: 401 hoặc đóng 4401
        C->>S: POST /v1/auth/refresh (REST)
        C->>S: HTTP upgrade lại với token mới
    end
    C->>S: hello (client_time_ms, resume trip_id + last_event_seq)
    S->>R: conn_claim - kết nối cũ nếu có bị 4409
    S-->>C: welcome (clock_offset_ms, limits, heartbeat)
    loop mỗi trip trong resume
        S->>S: authz - membership active
        S->>R: đọc ml:loc theo thành viên, ml:pres
        S->>P: SELECT trip_events WHERE seq > last_event_seq
        alt đủ dữ liệu bù
            S-->>C: trip.joined (snapshot, head_event_seq)
            S-->>C: trip.event / sos.event (replay)
        else quá nhiều hoặc quá cũ
            S-->>C: resync.required
            C->>S: GET /v1/trips/{id}/snapshot (REST)
            C->>S: trip.join (last_event_seq mới)
            S-->>C: trip.joined
        end
    end
    C->>C: áp dụng snapshot có điều kiện - chỉ nếu (session_id, seq) mới hơn
    C->>S: event.ack (event_seq đã lưu)
    C->>S: location.update gửi lại các điểm chưa ACK
    S-->>C: location.ack
    S-->>C: presence.event của thành viên khác
    opt kết nối đang sống và token sắp hết hạn
        C->>S: auth.refresh (access_token mới, lấy bằng REST refresh)
        S-->>C: ack
        Note over C,S: Không refresh trước exp + 30 s thì server đóng 4401
    end
```

## 4. S3 — SOS (kể cả khi người gửi mất mạng, người nhận không chạy app)

```mermaid
sequenceDiagram
    autonumber
    participant A as Android A (người gửi)
    participant S as Server
    participant P as PostgreSQL
    participant O as Outbox relay
    participant K as Kafka
    participant N as notifier
    participant R as Redis
    participant B as Android B (online WS)
    participant F as FCM
    participant C as Android C (app không chạy)

    A->>A: bấm SOS - ghi pending_sos (client_sos_id) vào Room
    loop đến khi nhận ack
        A->>S: sos.create (id = client_sos_id) hoặc REST POST /trips/{id}/sos
        alt mất mạng
            Note over A: WorkManager và reconnect thử lại, UI hiển thị đang chờ gửi
        end
    end
    S->>S: authz - member active, trip active
    S->>P: BEGIN - sos_events (UNIQUE user_id, client_sos_id) + sos_recipients + trip_events + outbox - COMMIT
    S-->>A: ack (sos_id, event_seq, duplicate=false)
    Note over S,A: Gửi lại cùng client_sos_id cho ack duplicate=true, không tạo SOS mới
    S->>R: PUBLISH fast path trip channel
    R-->>B: (qua instance giữ kết nối B) sos.event
    B-->>S: sos.receipt received rồi seen
    O->>P: poll outbox FOR UPDATE SKIP LOCKED
    O->>K: produce sos.v1 key=trip_id
    O->>R: PUBLISH lại (at-least-once, client dedupe theo event_seq)
    K->>N: consume sos.created
    N->>P: INSERT notifications ON CONFLICT DO NOTHING
    N->>F: push high priority (payload tối thiểu)
    F-->>C: push
    C->>S: GET /v1/sos/{id} rồi POST /sos/{id}/receipt received
    S->>P: UPDATE sos_recipients received_at
    S-->>A: sos.status (total, received, seen, push_failed)
    alt FCM lỗi tạm
        N->>N: retry tối đa 5 lần backoff
    else hết lần thử
        N->>K: produce sos.v1.dlq
        N->>P: push_status = failed
    end
```

## 5. S4 — Persistence của event (trip event qua outbox, và nhánh lịch sử vị trí)

```mermaid
sequenceDiagram
    autonumber
    participant H as REST handler (ví dụ POST /trips/{id}/start)
    participant P as PostgreSQL
    participant O as Outbox relay
    participant K as Kafka
    participant R as Redis
    participant V as Viewer (WS)
    participant N as notifier
    participant L as location (S1) -> history-writer

    H->>P: BEGIN
    H->>P: UPDATE trips SET status=active, event_seq=event_seq+1 RETURNING event_seq
    H->>P: INSERT trip_events (seq) + INSERT outbox
    H->>P: COMMIT
    H-->>H: trả 200 cho client (idempotent theo Idempotency-Key)
    H->>R: PUBLISH fast path (best effort)
    loop poll mỗi 200 ms
        O->>P: SELECT outbox WHERE published_at IS NULL FOR UPDATE SKIP LOCKED
        O->>K: produce trip-events.v1 key=trip_id
        K-->>O: ack
        O->>R: PUBLISH ml:ps:trip (trip.event, event_seq)
        O->>P: UPDATE outbox SET published_at
    end
    R-->>V: trip.event (event_seq) - client bỏ nếu event_seq nhỏ hơn hoặc bằng last_event_seq
    K->>N: consume - tạo notifications, push nếu cần
    Note over V: Nếu thấy lỗ event_seq hoặc mất Pub/Sub thì GET /trips/{id}/events?after_seq hoặc resync.required
    Note over L: Lịch sử vị trí: không dùng outbox - client giữ điểm đến khi ACK (xem S1 và ADR-005, ADR-008)
```

## 6. Ghi chú đọc sơ đồ

- S1 hiển thị rõ **hai nhánh độc lập**: live không chờ Kafka, ACK chỉ phụ thuộc Kafka.
- S4 nêu nguyên tắc: *event bền* đi theo outbox (at-least-once, consumer idempotent theo `event_id`/`event_seq`); *vị trí* không đi outbox.
- Mọi mũi tên "PUBLISH" là Pub/Sub không bền; sơ đồ S2 là đường bù khi mất.
