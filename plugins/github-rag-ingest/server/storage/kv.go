package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/TykTechnologies/midsommar/v2/pkg/plugin_sdk"
)

// KVStore wraps the plugin SDK KV service with JSON serialization
type KVStore struct {
	kv plugin_sdk.KVService
}

// NewKVStore creates a new KV store wrapper
func NewKVStore(kv plugin_sdk.KVService) *KVStore {
	return &KVStore{kv: kv}
}

// Write writes a value as JSON
func (s *KVStore) Write(ctx context.Context, key string, value interface{}, ttl *time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	var expireAt *time.Time
	if ttl != nil {
		exp := time.Now().Add(*ttl)
		expireAt = &exp
	}

	_, err = s.kv.Write(ctx, key, data, expireAt)
	return err
}

// Read reads and unmarshals JSON value
func (s *KVStore) Read(ctx context.Context, key string, dest interface{}) error {
	data, err := s.kv.Read(ctx, key)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("failed to unmarshal value: %w", err)
	}

	return nil
}

// Delete deletes a key
func (s *KVStore) Delete(ctx context.Context, key string) error {
	_, err := s.kv.Delete(ctx, key)
	return err
}

// List lists keys with a prefix
func (s *KVStore) List(ctx context.Context, prefix string) ([]string, error) {
	return s.kv.List(ctx, prefix)
}

// ReadRaw reads raw bytes without JSON unmarshaling
func (s *KVStore) ReadRaw(ctx context.Context, key string) ([]byte, error) {
	return s.kv.Read(ctx, key)
}

// WriteRaw writes raw bytes without JSON marshaling
func (s *KVStore) WriteRaw(ctx context.Context, key string, data []byte, ttl *time.Duration) error {
	var expireAt *time.Time
	if ttl != nil {
		exp := time.Now().Add(*ttl)
		expireAt = &exp
	}

	_, err := s.kv.Write(ctx, key, data, expireAt)
	return err
}
