package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
	redis "github.com/redis/go-redis/v9"
)

// ErrKeyNotFound is returned when a key does not exist in the store.
var ErrKeyNotFound = errors.New("key not found")

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
// previousCount is the count from the previous window bucket.
// currentCount is the count from the current window bucket.
// now is the current time, windowSeconds is the window duration.
func SlidingWindowCount(previousCount, currentCount int, now time.Time, windowSeconds int) int {
	w := int64(windowSeconds)
	currentStart := BucketEpoch(now, windowSeconds)
	elapsed := now.Unix() - currentStart
	if elapsed < 0 {
		elapsed = 0
	}
	if elapsed >= w {
		// Entire window elapsed, previous bucket contributes nothing
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
	if err != nil {
		return nil, ErrKeyNotFound
	}
	if len(data) == 0 {
		return nil, ErrKeyNotFound
	}
	return data, nil
}

func (s *kvStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	_, err := s.kv.WriteWithTTL(ctx, key, value, ttl)
	return err
}

func (s *kvStore) Delete(ctx context.Context, key string) error {
	_, err := s.kv.Delete(ctx, key)
	return err
}

// --- redisStore ---

type redisStore struct {
	client *redis.Client
}

func newRedisStore(redisURL string) (Store, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("invalid redis URL: %w", err)
	}
	client := redis.NewClient(opt)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("redis connection failed: %w", err)
	}

	return &redisStore{client: client}, nil
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
