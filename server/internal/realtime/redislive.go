package realtime

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Key layout: docs/design/08 §2. Hash tags {trip} keep a trip's keys together should Redis ever be clustered.
func kLoc(t, u uuid.UUID) string { return fmt.Sprintf("ml:loc:{%s}:%s", t, u) }
func kPres(t uuid.UUID) string   { return fmt.Sprintf("ml:pres:{%s}", t) }
func kConn(u uuid.UUID) string   { return "ml:conn:" + u.String() }

// loc_set: write only if (sid, seq) is newer (docs/design/08 §4.1). sid is a lowercase UUIDv7, so string
// comparison equals time order of session creation.
var luaLocSet = redis.NewScript(`
local cur_sid = redis.call('HGET', KEYS[1], 'sid')
if cur_sid then
  local cur_seq = tonumber(redis.call('HGET', KEYS[1], 'seq'))
  local new_seq = tonumber(ARGV[2])
  if cur_sid > ARGV[1] or (cur_sid == ARGV[1] and cur_seq >= new_seq) then
    return 0
  end
end
redis.call('HSET', KEYS[1], 'sid', ARGV[1], 'seq', ARGV[2], 'lat', ARGV[3], 'lng', ARGV[4],
           'acc', ARGV[5], 'spd', ARGV[6], 'hdg', ARGV[7], 'sts', ARGV[8])
redis.call('EXPIRE', KEYS[1], tonumber(ARGV[9]))
return 1`)

var luaConnClaim = redis.NewScript(`
local old = redis.call('GET', KEYS[1])
redis.call('SET', KEYS[1], ARGV[1], 'EX', tonumber(ARGV[2]))
if old then return old end
return ''`)

var luaConnTouch = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  redis.call('EXPIRE', KEYS[1], tonumber(ARGV[2]))
  return 1
end
return 0`)

var luaPresTouch = redis.NewScript(`
local t = redis.call('TIME')
local now = t[1] * 1000 + math.floor(t[2] / 1000)
local prev = redis.call('ZSCORE', KEYS[1], ARGV[1])
redis.call('ZADD', KEYS[1], now + tonumber(ARGV[2]), ARGV[1])
redis.call('EXPIRE', KEYS[1], 3600)
if (not prev) or tonumber(prev) <= now then return 1 end
return 0`)

// KEYS[1] = conn key, KEYS[2..] = presence zsets; ARGV[1] = "inst|conn", ARGV[2] = user id.
// Returns indexes (1-based into KEYS[2..]) of the zsets the user was really removed from.
var luaConnLeave = redis.NewScript(`
if redis.call('GET', KEYS[1]) ~= ARGV[1] then return {} end
redis.call('DEL', KEYS[1])
local out = {}
for i = 2, #KEYS do
  if redis.call('ZREM', KEYS[i], ARGV[2]) == 1 then out[#out + 1] = i - 1 end
end
return out`)

var luaPresList = redis.NewScript(`
local t = redis.call('TIME')
local now = t[1] * 1000 + math.floor(t[2] / 1000)
return redis.call('ZRANGEBYSCORE', KEYS[1], '(' .. now, '+inf')`)

var luaPresReap = redis.NewScript(`
local t = redis.call('TIME')
local now = t[1] * 1000 + math.floor(t[2] / 1000)
local ex = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', now)
for _, u in ipairs(ex) do redis.call('ZREM', KEYS[1], u) end
return ex`)

type redisStore struct{ c redis.UniversalClient }

func NewRedisStore(c redis.UniversalClient) LiveStore { return &redisStore{c: c} }

func f32(p *float32) string {
	if p == nil {
		return ""
	}
	return strconv.FormatFloat(float64(*p), 'f', -1, 32)
}

func (r *redisStore) SetLocation(ctx context.Context, t, u uuid.UUID, p LivePoint) (bool, error) {
	n, err := luaLocSet.Run(ctx, r.c, []string{kLoc(t, u)},
		p.SessionID, p.Seq, strconv.FormatFloat(p.Lat, 'f', -1, 64), strconv.FormatFloat(p.Lng, 'f', -1, 64),
		f32(p.AccuracyM), f32(p.SpeedMps), f32(p.Heading), p.ServerTs, int(locTTL.Seconds())).Int()
	return n == 1, err
}

func optF32(s string) *float32 {
	if s == "" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 32)
	if err != nil {
		return nil
	}
	f := float32(v)
	return &f
}

func (r *redisStore) Locations(ctx context.Context, t uuid.UUID, users []uuid.UUID) (map[uuid.UUID]LivePoint, error) {
	pipe := r.c.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, len(users))
	for i, u := range users {
		cmds[i] = pipe.HGetAll(ctx, kLoc(t, u))
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	out := map[uuid.UUID]LivePoint{}
	for i, c := range cmds {
		h := c.Val()
		if len(h) == 0 {
			continue
		}
		seq, _ := strconv.ParseInt(h["seq"], 10, 64)
		lat, _ := strconv.ParseFloat(h["lat"], 64)
		lng, _ := strconv.ParseFloat(h["lng"], 64)
		sts, _ := strconv.ParseInt(h["sts"], 10, 64)
		out[users[i]] = LivePoint{SessionID: h["sid"], Seq: seq, Lat: lat, Lng: lng,
			AccuracyM: optF32(h["acc"]), SpeedMps: optF32(h["spd"]), Heading: optF32(h["hdg"]), ServerTs: sts}
	}
	return out, nil
}

func (r *redisStore) DeleteLocation(ctx context.Context, t, u uuid.UUID) error {
	return r.c.Del(ctx, kLoc(t, u)).Err()
}

func (r *redisStore) DeleteTripLocations(ctx context.Context, t uuid.UUID, users []uuid.UUID) error {
	keys := make([]string, 0, len(users)+1)
	for _, u := range users {
		keys = append(keys, kLoc(t, u))
	}
	keys = append(keys, kPres(t))
	return r.c.Del(ctx, keys...).Err()
}

func (r *redisStore) ClaimConn(ctx context.Context, u uuid.UUID, inst, conn string) (string, error) {
	return luaConnClaim.Run(ctx, r.c, []string{kConn(u)}, inst+"|"+conn, int(presenceTTL.Seconds())).Text()
}

func (r *redisStore) TouchConn(ctx context.Context, u uuid.UUID, inst, conn string) (bool, error) {
	n, err := luaConnTouch.Run(ctx, r.c, []string{kConn(u)}, inst+"|"+conn, int(presenceTTL.Seconds())).Int()
	return n == 1, err
}

func (r *redisStore) PresenceTouch(ctx context.Context, t, u uuid.UUID) (bool, error) {
	n, err := luaPresTouch.Run(ctx, r.c, []string{kPres(t)}, u.String(), presenceTTL.Milliseconds()).Int()
	return n == 1, err
}

func (r *redisStore) ConnLeave(ctx context.Context, u uuid.UUID, inst, conn string, trips []uuid.UUID) ([]uuid.UUID, error) {
	keys := []string{kConn(u)}
	for _, t := range trips {
		keys = append(keys, kPres(t))
	}
	idx, err := luaConnLeave.Run(ctx, r.c, keys, inst+"|"+conn, u.String()).Int64Slice()
	if err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, 0, len(idx))
	for _, i := range idx {
		out = append(out, trips[i-1])
	}
	return out, nil
}

func (r *redisStore) Online(ctx context.Context, t uuid.UUID, users []uuid.UUID) (map[uuid.UUID]bool, error) {
	names, err := luaPresList.Run(ctx, r.c, []string{kPres(t)}).StringSlice()
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	out := map[uuid.UUID]bool{}
	for _, u := range users {
		if set[u.String()] {
			out[u] = true
		}
	}
	return out, nil
}

func (r *redisStore) ReapPresence(ctx context.Context, t uuid.UUID) ([]uuid.UUID, error) {
	names, err := luaPresReap.Run(ctx, r.c, []string{kPres(t)}).StringSlice()
	if err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, 0, len(names))
	for _, n := range names {
		if u, err := uuid.Parse(n); err == nil {
			out = append(out, u)
		}
	}
	return out, nil
}

func (r *redisStore) PresenceRemove(ctx context.Context, t, u uuid.UUID) error {
	return r.c.ZRem(ctx, kPres(t), u.String()).Err()
}
