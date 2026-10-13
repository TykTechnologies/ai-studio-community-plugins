package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
	redis "github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrKeyNotFound is returned when a key does not exist in the store.
var ErrKeyNotFound = errors.New("key not found")

// ErrVersionConflict is returned when an optimistic lock detects a concurrent modification.
var ErrVersionConflict = errors.New("version conflict: data was modified concurrently")

// WindowState is the counter for a single time bucket.
type WindowState struct {
	Count     int   `json:"c"`
	UpdatedAt int64 `json:"u"`
}

// ConcurrentState tracks in-flight requests for a dimension key.
type ConcurrentState struct {
	Count     int   `json:"c"`
	UpdatedAt int64 `json:"u"`
}

// RequestState links the post_auth phase to the response phase.
type RequestState struct {
	TokenRuleKeys []TokenRuleRef `json:"tk,omitempty"` // rules needing token counting
	ConcRuleKeys  []string       `json:"ck,omitempty"` // concurrent keys that were incremented
	BucketEpoch   int64          `json:"be"`           // window bucket epoch at post_auth time
	Timestamp     int64          `json:"ts"`
}

// TokenRuleRef identifies a rule + dimension key that needs token counting on response.
type TokenRuleRef struct {
	RuleID       string `json:"ri"`
	DimensionKey string `json:"dk"`
}

// Store abstracts KV vs Redis backends for rate limit state.
type Store interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error

	// EvalSlidingWindow atomically reads the previous and current window buckets,
	// computes the sliding window count, and optionally increments the current bucket.
	// Returns the effective count BEFORE the increment.
	// For kvStore this falls back to non-atomic read (protected by caller's lock).
	// For redisStore this uses a Lua script for cross-instance atomicity.
	EvalSlidingWindow(ctx context.Context, currentKey, previousKey string, windowSeconds int, nowUnix int64, incrDelta int, ttl time.Duration) (effectiveCount int, err error)

	// IncrementIfBelow atomically increments a concurrent counter only if its
	// current value is below the given limit. Returns the current count (before
	// increment) and whether the increment was applied.
	// Both backends store JSON {"c":<count>,"u":<timestamp>} for format consistency.
	// For kvStore: read-modify-write (caller must hold a lock).
	// For redisStore: Lua script for cross-instance atomicity.
	IncrementIfBelow(ctx context.Context, key string, limit int, ttl time.Duration) (currentCount int, allowed bool, err error)

	// DecrementCounter atomically decrements a concurrent counter, flooring at 0.
	// Both backends operate on JSON {"c":<count>,"u":<timestamp>}.
	// For kvStore: read-modify-write (caller must hold a lock).
	// For redisStore: Lua script for cross-instance atomicity.
	DecrementCounter(ctx context.Context, key string, ttl time.Duration) (newCount int, err error)

	// Close releases the backend's connections (the Redis client); a no-op for kvStore.
	Close() error
}

// errCorruptCounter marks a stored counter that does not decode. It is a
// storage error, so the fail_open setting decides whether the request passes,
// the same as when Redis' Lua scripts fail to decode a counter.
var errCorruptCounter = errors.New("corrupt rate limit counter")

// isKVNotFound reports whether a plugin KV read failed only because the key
// does not exist. Both the AI Studio and the Microgateway KV services answer
// a missing key with a gRPC NotFound status (wrapped by the SDK).
func isKVNotFound(err error) bool {
	return status.Code(err) == codes.NotFound
}

// decodeCounter decodes a stored {"c":<count>,...} counter. A missing key is
// zero. A failed read (store unreachable, scope denied) and an undecodable
// value are errors naming the key, so fail_open decides; reading them as zero
// would allow every request whatever fail_open says.
func decodeCounter(key string, data []byte, readErr error) (int, error) {
	if readErr != nil && !isKVNotFound(readErr) {
		return 0, fmt.Errorf("read rate limit counter %q: %w", key, readErr)
	}
	if readErr != nil || len(data) == 0 {
		return 0, nil
	}
	var state WindowState // same JSON shape as ConcurrentState
	if err := json.Unmarshal(data, &state); err != nil {
		return 0, fmt.Errorf("%w %q: %v", errCorruptCounter, key, err)
	}
	return state.Count, nil
}

// --- Sliding Window ---

// BucketEpoch returns the start epoch of the current bucket for the given window size.
func BucketEpoch(now time.Time, windowSeconds int) int64 {
	unix := now.Unix()
	w := int64(windowSeconds)
	return unix - (unix % w)
}

// PreviousBucketEpoch returns the start epoch of the previous bucket.
func PreviousBucketEpoch(now time.Time, windowSeconds int) int64 {
	return BucketEpoch(now, windowSeconds) - int64(windowSeconds)
}

// SlidingWindowCount computes the effective count across two buckets.
func SlidingWindowCount(previousCount, currentCount int, now time.Time, windowSeconds int) int {
	w := int64(windowSeconds)
	currentStart := BucketEpoch(now, windowSeconds)
	elapsed := now.Unix() - currentStart
	if elapsed < 0 {
		elapsed = 0
	}
	if elapsed >= w {
		return currentCount
	}
	weightPrevious := float64(w-elapsed) / float64(w)
	return int(float64(previousCount)*weightPrevious) + currentCount
}

// SlidingWindowCountFromUnix is like SlidingWindowCount but takes raw unix timestamp.
func SlidingWindowCountFromUnix(previousCount, currentCount int, nowUnix int64, windowSeconds int) int {
	w := int64(windowSeconds)
	currentStart := nowUnix - (nowUnix % w)
	elapsed := nowUnix - currentStart
	if elapsed < 0 {
		elapsed = 0
	}
	if elapsed >= w {
		return currentCount
	}
	weightPrevious := float64(w-elapsed) / float64(w)
	return int(float64(previousCount)*weightPrevious) + currentCount
}

// --- kvStore ---

type kvStore struct {
	kv plugin_sdk.KVService
}

func newKVStore(kv plugin_sdk.KVService) Store {
	return &kvStore{kv: kv}
}

func (s *kvStore) Get(ctx context.Context, key string) ([]byte, error) {
	data, err := s.kv.Read(ctx, key)
	if err != nil && !isKVNotFound(err) {
		return nil, fmt.Errorf("read %q: %w", key, err)
	}
	if err != nil || len(data) == 0 {
		return nil, ErrKeyNotFound
	}
	return data, nil
}

func (s *kvStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if ttl <= 0 {
		// TTL=0 means persist forever; WriteWithTTL(0) would expire immediately
		_, err := s.kv.Write(ctx, key, value, nil)
		return err
	}
	_, err := s.kv.WriteWithTTL(ctx, key, value, ttl)
	return err
}

func (s *kvStore) Delete(ctx context.Context, key string) error {
	_, err := s.kv.Delete(ctx, key)
	return err
}

// IncrementIfBelow for kvStore does a read-modify-write. Caller must hold a lock.
func (s *kvStore) IncrementIfBelow(ctx context.Context, key string, limit int, ttl time.Duration) (int, bool, error) {
	data, err := s.kv.Read(ctx, key)
	count, err := decodeCounter(key, data, err)
	if err != nil {
		return 0, false, err
	}
	state := ConcurrentState{Count: count}
	if state.Count >= limit {
		return state.Count, false, nil
	}
	state.Count++
	state.UpdatedAt = time.Now().Unix()
	out, _ := json.Marshal(state)
	_, err = s.kv.WriteWithTTL(ctx, key, out, ttl)
	return state.Count - 1, true, err // return pre-increment count
}

// DecrementCounter for kvStore does a read-modify-write. Caller must hold a lock.
func (s *kvStore) DecrementCounter(ctx context.Context, key string, ttl time.Duration) (int, error) {
	data, err := s.kv.Read(ctx, key)
	count, err := decodeCounter(key, data, err)
	if err != nil {
		return 0, err
	}
	state := ConcurrentState{Count: count}
	state.Count--
	if state.Count < 0 {
		state.Count = 0
	}
	state.UpdatedAt = time.Now().Unix()
	out, _ := json.Marshal(state)
	_, err = s.kv.WriteWithTTL(ctx, key, out, ttl)
	return state.Count, err
}

// EvalSlidingWindow for kvStore does read + optional increment. Caller must hold a lock.
// Returns the effective count BEFORE the increment (consistent with redisStore Lua script).
func (s *kvStore) EvalSlidingWindow(ctx context.Context, currentKey, previousKey string, windowSeconds int, nowUnix int64, incrDelta int, ttl time.Duration) (int, error) {
	prev, err := s.readCounter(ctx, previousKey)
	if err != nil {
		return 0, err
	}
	cur, err := s.readCounter(ctx, currentKey)
	if err != nil {
		return 0, err
	}
	effective := SlidingWindowCountFromUnix(prev, cur, nowUnix, windowSeconds)

	if incrDelta != 0 {
		newCount := cur + incrDelta
		state := WindowState{Count: newCount, UpdatedAt: nowUnix}
		data, _ := json.Marshal(state)
		if _, err := s.kv.WriteWithTTL(ctx, currentKey, data, ttl); err != nil {
			return effective, err
		}
	}
	return effective, nil
}

func (s *kvStore) readCounter(ctx context.Context, key string) (int, error) {
	data, err := s.kv.Read(ctx, key)
	return decodeCounter(key, data, err)
}

func (s *kvStore) Close() error { return nil }

// --- redisStore ---

type redisStore struct {
	client           *redis.Client
	slidingWindowLua *redis.Script
	incrIfBelowLua   *redis.Script
	decrCounterLua   *redis.Script
}

// Lua script for atomic IncrementIfBelow on a JSON counter.
// KEYS[1] = counter key
// ARGV[1] = limit
// ARGV[2] = current unix timestamp
// ARGV[3] = TTL in seconds
// Returns: [currentCount, allowed] where allowed=1 if incremented, 0 if at/above limit
const incrIfBelowLuaScript = `
local data = redis.call('GET', KEYS[1])
local count = 0
if data then
    local state = cjson.decode(data)
    count = tonumber(state.c) or 0
end

local limit = tonumber(ARGV[1])
local now = tonumber(ARGV[2])
local ttl_sec = tonumber(ARGV[3])

if count >= limit then
    return {count, 0}
end

-- Increment and save as JSON
count = count + 1
local new_state = cjson.encode({c = count, u = now})
redis.call('SET', KEYS[1], new_state, 'EX', ttl_sec)
return {count - 1, 1}
`

// Lua script for atomic DecrementCounter on a JSON counter.
// KEYS[1] = counter key
// ARGV[1] = current unix timestamp
// ARGV[2] = TTL in seconds
// Returns: new count (floored at 0)
const decrCounterLuaScript = `
local data = redis.call('GET', KEYS[1])
local count = 0
if data then
    local state = cjson.decode(data)
    count = tonumber(state.c) or 0
end

local now = tonumber(ARGV[1])
local ttl_sec = tonumber(ARGV[2])

count = count - 1
if count < 0 then count = 0 end

local new_state = cjson.encode({c = count, u = now})
redis.call('SET', KEYS[1], new_state, 'EX', ttl_sec)
return count
`

// Lua script for atomic sliding window evaluation + optional increment.
// KEYS[1] = current bucket key
// KEYS[2] = previous bucket key
// ARGV[1] = window size in seconds
// ARGV[2] = current unix timestamp
// ARGV[3] = increment delta (0 for read-only)
// ARGV[4] = TTL in seconds
// Returns: effective sliding window count BEFORE the increment.
// The increment is applied atomically but the returned value reflects the
// pre-increment state so the caller can compare against the limit correctly
// (i.e., limit=100 means 100 requests are allowed, not 99).
const slidingWindowLuaScript = `
local cur_data = redis.call('GET', KEYS[1])
local prev_data = redis.call('GET', KEYS[2])

local cur_count = 0
local prev_count = 0

if cur_data then
    local cur = cjson.decode(cur_data)
    cur_count = tonumber(cur.c) or 0
end
if prev_data then
    local prev = cjson.decode(prev_data)
    prev_count = tonumber(prev.c) or 0
end

local W = tonumber(ARGV[1])
local now = tonumber(ARGV[2])
local delta = tonumber(ARGV[3])
local ttl_sec = tonumber(ARGV[4])

-- Compute sliding window BEFORE increment
local current_start = now - (now % W)
local elapsed = now - current_start
if elapsed < 0 then elapsed = 0 end

local weight_prev = 0
if elapsed < W then
    weight_prev = (W - elapsed) / W
end
local effective = math.floor(prev_count * weight_prev) + cur_count

-- Increment current bucket if requested (after computing effective)
if delta ~= 0 then
    cur_count = cur_count + delta
    local state = cjson.encode({c = cur_count, u = now})
    redis.call('SET', KEYS[1], state, 'EX', ttl_sec)
end

return effective
`

// resolveRedisPassword resolves the redis_password config value:
//
//   - $SECRET/NAME is resolved by AI Studio 2.3 and later before the plugin
//     starts. One still present here means an older AI Studio, a secret that
//     does not exist, or an edge gateway (edges receive the reference, not
//     the value), so it is an error rather than a password.
//   - $ENV/NAME is read from the plugin's own environment (on AI Studio 2.3
//     and later, pass the variable with PLUGIN_ENV_PASSTHROUGH).
//
// Any other value is returned unchanged.
func resolveRedisPassword(v string) (string, error) {
	switch {
	case strings.HasPrefix(v, "$SECRET/"):
		return "", errors.New("redis_password: secret reference was not resolved by AI Studio")
	case strings.HasPrefix(v, "$ENV/"):
		name := strings.TrimPrefix(v, "$ENV/")
		if val := os.Getenv(name); val != "" {
			return val, nil
		}
		return "", fmt.Errorf("redis_password: environment variable %s is not set", name)
	}
	return v, nil
}

// redisOptions parses redisURL and applies password, which overrides any
// password in the URL. Errors never include the URL, as it may carry
// credentials.
func redisOptions(redisURL, password string) (*redis.Options, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		// url.Error's message repeats the whole URL; its cause does not.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return nil, fmt.Errorf("invalid redis URL: %w", err)
	}
	password, err = resolveRedisPassword(password)
	if err != nil {
		return nil, err
	}
	if password != "" {
		opt.Password = password
	}
	return opt, nil
}

func newRedisStore(redisURL, password string) (Store, error) {
	opt, err := redisOptions(redisURL, password)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(opt)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("redis connection failed: %w", err)
	}

	return &redisStore{
		client:           client,
		slidingWindowLua: redis.NewScript(slidingWindowLuaScript),
		incrIfBelowLua:   redis.NewScript(incrIfBelowLuaScript),
		decrCounterLua:   redis.NewScript(decrCounterLuaScript),
	}, nil
}

func (s *redisStore) Get(ctx context.Context, key string) ([]byte, error) {
	data, err := s.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (s *redisStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return s.client.Set(ctx, key, value, ttl).Err()
}

func (s *redisStore) Delete(ctx context.Context, key string) error {
	return s.client.Del(ctx, key).Err()
}

func (s *redisStore) Close() error {
	return s.client.Close()
}

// IncrementIfBelow uses a Lua script to atomically check the concurrent counter
// against the limit and increment only if below. Stores JSON for format consistency.
// Returns (currentCount before increment, allowed, error).
func (s *redisStore) IncrementIfBelow(ctx context.Context, key string, limit int, ttl time.Duration) (int, bool, error) {
	ttlSeconds := int(ttl.Seconds())
	if ttlSeconds < 1 {
		ttlSeconds = 1
	}

	result, err := s.incrIfBelowLua.Run(ctx, s.client,
		[]string{key}, limit, time.Now().Unix(), ttlSeconds,
	).Int64Slice()
	if err != nil {
		return 0, false, fmt.Errorf("IncrementIfBelow lua error: %w", err)
	}
	currentCount := int(result[0])
	allowed := result[1] == 1
	return currentCount, allowed, nil
}

// DecrementCounter uses a Lua script to atomically decrement a concurrent counter,
// flooring at 0. Stores JSON for format consistency.
func (s *redisStore) DecrementCounter(ctx context.Context, key string, ttl time.Duration) (int, error) {
	ttlSeconds := int(ttl.Seconds())
	if ttlSeconds < 1 {
		ttlSeconds = 1
	}

	result, err := s.decrCounterLua.Run(ctx, s.client,
		[]string{key}, time.Now().Unix(), ttlSeconds,
	).Int()
	if err != nil {
		return 0, fmt.Errorf("DecrementCounter lua error: %w", err)
	}
	return result, nil
}

// EvalSlidingWindow uses a Lua script for atomic sliding window evaluation + increment.
func (s *redisStore) EvalSlidingWindow(ctx context.Context, currentKey, previousKey string, windowSeconds int, nowUnix int64, incrDelta int, ttl time.Duration) (int, error) {
	ttlSeconds := int(ttl.Seconds())
	if ttlSeconds < 1 {
		ttlSeconds = 1
	}

	result, err := s.slidingWindowLua.Run(ctx, s.client,
		[]string{currentKey, previousKey},
		windowSeconds, nowUnix, incrDelta, ttlSeconds,
	).Int()
	if err != nil {
		return 0, fmt.Errorf("sliding window lua error: %w", err)
	}
	return result, nil
}

// --- Helpers ---

// ReadWindowState reads a WindowState from the store, returning zero state if not found.
func ReadWindowState(ctx context.Context, store Store, key string) WindowState {
	data, err := store.Get(ctx, key)
	if err != nil {
		return WindowState{}
	}
	var state WindowState
	if err := json.Unmarshal(data, &state); err != nil {
		return WindowState{}
	}
	return state
}

// WriteWindowState writes a WindowState to the store.
func WriteWindowState(ctx context.Context, store Store, key string, state WindowState, ttl time.Duration) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return store.Set(ctx, key, data, ttl)
}

// ReadConcurrentState reads a ConcurrentState from the store, returning zero if not found.
func ReadConcurrentState(ctx context.Context, store Store, key string) ConcurrentState {
	data, err := store.Get(ctx, key)
	if err != nil {
		return ConcurrentState{}
	}
	var state ConcurrentState
	if err := json.Unmarshal(data, &state); err != nil {
		return ConcurrentState{}
	}
	return state
}

// WriteConcurrentState writes a ConcurrentState to the store.
func WriteConcurrentState(ctx context.Context, store Store, key string, state ConcurrentState, ttl time.Duration) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return store.Set(ctx, key, data, ttl)
}

// ReadRequestState reads a RequestState from the store.
func ReadRequestState(ctx context.Context, store Store, key string) (*RequestState, error) {
	data, err := store.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	var state RequestState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// WriteRequestState writes a RequestState to the store.
func WriteRequestState(ctx context.Context, store Store, key string, state *RequestState, ttl time.Duration) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return store.Set(ctx, key, data, ttl)
}
