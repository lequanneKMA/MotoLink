-- +goose Up
-- Baseline schema, mirrors docs/design/05-data-model.md. Change the doc first.

-- ===== Identity =====
CREATE TABLE users (
  id              uuid PRIMARY KEY,
  email           text NOT NULL,
  email_verified  boolean NOT NULL DEFAULT false,
  password_hash   text,                       -- argon2id; NULL if only social login later
  display_name    text NOT NULL CHECK (char_length(display_name) BETWEEN 1 AND 50),
  avatar_color    text NOT NULL DEFAULT '#2E7D32',
  locale          text NOT NULL DEFAULT 'vi',
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  deleted_at      timestamptz
);
CREATE UNIQUE INDEX users_email_uk ON users (lower(email)) WHERE deleted_at IS NULL;

CREATE TABLE devices (
  id             uuid PRIMARY KEY,
  user_id        uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  platform       text NOT NULL DEFAULT 'android' CHECK (platform IN ('android')),
  model          text,
  os_version     text,
  app_version    text,
  has_gms        boolean NOT NULL DEFAULT true,
  fcm_token      text,
  fcm_updated_at timestamptz,
  last_seen_at   timestamptz,
  created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX devices_user_idx ON devices (user_id);
CREATE UNIQUE INDEX devices_fcm_uk ON devices (fcm_token) WHERE fcm_token IS NOT NULL;

CREATE TABLE refresh_tokens (
  id            uuid PRIMARY KEY,
  user_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  device_id     uuid,
  family_id     uuid NOT NULL,
  token_hash    bytea NOT NULL,
  issued_at     timestamptz NOT NULL DEFAULT now(),
  expires_at    timestamptz NOT NULL,
  rotated_at    timestamptz,
  revoked_at    timestamptz,
  user_agent    text
);
CREATE UNIQUE INDEX refresh_tokens_hash_uk ON refresh_tokens (token_hash);
CREATE INDEX refresh_tokens_user_idx   ON refresh_tokens (user_id);
CREATE INDEX refresh_tokens_family_idx ON refresh_tokens (family_id);
CREATE INDEX refresh_tokens_expiry_idx ON refresh_tokens (expires_at);

-- ===== Trip =====
CREATE TABLE trips (
  id             uuid PRIMARY KEY,
  owner_id       uuid NOT NULL REFERENCES users(id),
  name           text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
  status         text NOT NULL DEFAULT 'draft'
                 CHECK (status IN ('draft','active','ended','archived')),
  history_visibility text NOT NULL DEFAULT 'members'
                 CHECK (history_visibility IN ('members','self_only')),
  max_members    int  NOT NULL DEFAULT 15 CHECK (max_members BETWEEN 2 AND 15),
  event_seq      bigint NOT NULL DEFAULT 0,
  started_at     timestamptz,
  ended_at       timestamptz,
  archived_at    timestamptz,
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX trips_owner_idx  ON trips (owner_id);
CREATE INDEX trips_status_idx ON trips (status) WHERE status IN ('draft','active');

CREATE TABLE trip_members (
  trip_id        uuid NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
  user_id        uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role           text NOT NULL CHECK (role IN ('owner','member')),
  status         text NOT NULL CHECK (status IN ('active','left','removed')),
  sharing_state  text NOT NULL DEFAULT 'sharing' CHECK (sharing_state IN ('sharing','paused')),
  share_history  boolean NOT NULL DEFAULT true,
  color          text,
  joined_at      timestamptz NOT NULL DEFAULT now(),
  left_at        timestamptz,
  PRIMARY KEY (trip_id, user_id)
);
CREATE INDEX trip_members_user_idx ON trip_members (user_id, status);
CREATE UNIQUE INDEX trip_one_owner_uk ON trip_members (trip_id) WHERE role='owner' AND status='active';

CREATE TABLE trip_invitations (
  id           uuid PRIMARY KEY,
  trip_id      uuid NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
  created_by   uuid NOT NULL REFERENCES users(id),
  code_hash    bytea NOT NULL,
  expires_at   timestamptz NOT NULL,
  max_uses     int NOT NULL DEFAULT 15 CHECK (max_uses BETWEEN 1 AND 50),
  used_count   int NOT NULL DEFAULT 0,
  revoked_at   timestamptz,
  created_at   timestamptz NOT NULL DEFAULT now(),
  CHECK (used_count <= max_uses)
);
CREATE UNIQUE INDEX trip_invitations_code_uk ON trip_invitations (code_hash);
CREATE INDEX trip_invitations_trip_idx ON trip_invitations (trip_id);

CREATE TABLE trip_events (
  id           uuid PRIMARY KEY,
  trip_id      uuid NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
  seq          bigint NOT NULL,
  type         text NOT NULL,
  actor_id     uuid REFERENCES users(id) ON DELETE SET NULL,
  payload      jsonb NOT NULL DEFAULT '{}',
  created_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (trip_id, seq)
);

-- ===== Tracking =====
CREATE TABLE tracking_sessions (
  id              uuid PRIMARY KEY,
  trip_id         uuid NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
  user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  device_id       uuid REFERENCES devices(id) ON DELETE SET NULL,
  status          text NOT NULL DEFAULT 'active' CHECK (status IN ('active','paused','ended')),
  end_reason      text CHECK (end_reason IN ('user','trip_ended','removed','timeout','account_deleted')),
  session_month   date NOT NULL,
  started_at      timestamptz NOT NULL DEFAULT now(),
  paused_total_s  int NOT NULL DEFAULT 0,
  ended_at        timestamptz,
  last_seq        int NOT NULL DEFAULT 0,
  last_seen_at    timestamptz,
  point_count     int,
  distance_m      double precision,
  duration_s      int,
  stats_version   int NOT NULL DEFAULT 0,
  route_polyline  text
);
CREATE INDEX tracking_sessions_trip_idx ON tracking_sessions (trip_id, started_at);
CREATE INDEX tracking_sessions_user_idx ON tracking_sessions (user_id, started_at DESC);
CREATE UNIQUE INDEX tracking_sessions_one_open_uk ON tracking_sessions (trip_id, user_id)
  WHERE status IN ('active','paused');

CREATE TABLE location_points (
  session_month  date        NOT NULL,
  session_id     uuid        NOT NULL,
  seq            int         NOT NULL,
  trip_id        uuid        NOT NULL,
  user_id        uuid        NOT NULL,
  recorded_at    timestamptz NOT NULL,
  received_at    timestamptz NOT NULL,
  lat            double precision NOT NULL CHECK (lat BETWEEN -90 AND 90),
  lng            double precision NOT NULL CHECK (lng BETWEEN -180 AND 180),
  accuracy_m     real,
  speed_mps      real,
  heading_deg    real,
  altitude_m     real,
  suspect        boolean NOT NULL DEFAULT false,
  PRIMARY KEY (session_month, session_id, seq)
) PARTITION BY RANGE (session_month);
CREATE INDEX location_points_user_time_idx ON location_points (user_id, recorded_at);

-- Pre-create monthly partitions: previous month .. +3 months. The retention job
-- (M6-T01) keeps creating future ones.
-- +goose StatementBegin
DO $$
DECLARE
  m date;
BEGIN
  FOR i IN -1..3 LOOP
    m := (date_trunc('month', now()) + make_interval(months => i))::date;
    EXECUTE format(
      'CREATE TABLE IF NOT EXISTS %I PARTITION OF location_points FOR VALUES FROM (%L) TO (%L)',
      'location_points_' || to_char(m, 'YYYY_MM'), m, (m + interval '1 month')::date);
  END LOOP;
END $$;
-- +goose StatementEnd

-- ===== SOS =====
CREATE TABLE sos_events (
  id                uuid PRIMARY KEY,
  trip_id           uuid NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
  user_id           uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  client_sos_id     uuid NOT NULL,
  kind              text NOT NULL DEFAULT 'general'
                    CHECK (kind IN ('general','accident','breakdown','lost','medical')),
  message           text CHECK (char_length(message) <= 280),
  lat               double precision,
  lng               double precision,
  accuracy_m        real,
  client_created_at timestamptz,
  status            text NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved','cancelled')),
  resolved_by       uuid REFERENCES users(id) ON DELETE SET NULL,
  resolved_at       timestamptz,
  created_at        timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, client_sos_id)
);
CREATE INDEX sos_events_trip_idx ON sos_events (trip_id, created_at DESC);

CREATE TABLE sos_recipients (
  sos_id            uuid NOT NULL REFERENCES sos_events(id) ON DELETE CASCADE,
  user_id           uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  delivered_ws_at   timestamptz,
  delivered_push_at timestamptz,
  push_status       text CHECK (push_status IN ('pending','sent','failed','no_token')),
  received_at       timestamptz,
  seen_at           timestamptz,
  PRIMARY KEY (sos_id, user_id)
);
CREATE INDEX sos_recipients_user_idx ON sos_recipients (user_id, seen_at);

-- ===== Notifications / utilities =====
CREATE TABLE notifications (
  id            uuid PRIMARY KEY,
  user_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  trip_id       uuid REFERENCES trips(id) ON DELETE CASCADE,
  type          text NOT NULL,
  ref_event_id  uuid,
  payload       jsonb NOT NULL DEFAULT '{}',
  push_status   text CHECK (push_status IN ('pending','sent','failed','skipped')),
  push_attempts int NOT NULL DEFAULT 0,
  created_at    timestamptz NOT NULL DEFAULT now(),
  read_at       timestamptz,
  UNIQUE (user_id, type, ref_event_id)
);
CREATE INDEX notifications_user_idx ON notifications (user_id, created_at DESC);

CREATE TABLE consent_records (
  id           uuid PRIMARY KEY,
  user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind         text NOT NULL CHECK (kind IN ('location_processing','privacy_policy','terms','background_location')),
  version      text NOT NULL,
  granted_at   timestamptz NOT NULL DEFAULT now(),
  withdrawn_at timestamptz
);
CREATE INDEX consent_user_idx ON consent_records (user_id, kind, granted_at DESC);

CREATE TABLE idempotency_keys (
  user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key             text NOT NULL,
  method          text NOT NULL,
  path            text NOT NULL,
  request_hash    bytea NOT NULL,
  response_status int,
  response_body   jsonb,
  created_at      timestamptz NOT NULL DEFAULT now(),
  expires_at      timestamptz NOT NULL,
  PRIMARY KEY (user_id, key)
);
CREATE INDEX idempotency_expiry_idx ON idempotency_keys (expires_at);

-- ===== Outbox (ADR-008) =====
CREATE TABLE outbox (
  id            bigserial PRIMARY KEY,
  topic         text NOT NULL,
  msg_key       text NOT NULL,
  event_id      uuid NOT NULL,
  payload       jsonb NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  published_at  timestamptz
);
CREATE INDEX outbox_unpublished_idx ON outbox (id) WHERE published_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS outbox;
DROP TABLE IF EXISTS idempotency_keys;
DROP TABLE IF EXISTS consent_records;
DROP TABLE IF EXISTS notifications;
DROP TABLE IF EXISTS sos_recipients;
DROP TABLE IF EXISTS sos_events;
DROP TABLE IF EXISTS location_points;
DROP TABLE IF EXISTS tracking_sessions;
DROP TABLE IF EXISTS trip_events;
DROP TABLE IF EXISTS trip_invitations;
DROP TABLE IF EXISTS trip_members;
DROP TABLE IF EXISTS trips;
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS devices;
DROP TABLE IF EXISTS users;
