// Package cache provides Redis-backed caching for temporary data
// such as session state, SMS verification codes, etc.
package cache

import (
	"context"
	"math/rand"

	"BrainForever/infra/captcha"

	"github.com/redis/go-redis/v9"
)

// ============================================================
// Redis adapter for CaptchaStore
// ============================================================

type redisCaptchaStore struct {
	client *redis.Client
}

// NewRedisCaptchaStore creates a Redis-based captcha.ICaptchaStore implementation.
func NewRedisCaptchaStore(client *redis.Client) captcha.ICaptchaStore {
	return &redisCaptchaStore{client: client}
}

func (s *redisCaptchaStore) HSet(ctx context.Context, key, field string, value interface{}) error {
	return s.client.HSet(ctx, key, field, value).Err()
}

func (s *redisCaptchaStore) HGet(ctx context.Context, key, field string) (string, error) {
	return s.client.HGet(ctx, key, field).Result()
}

func (s *redisCaptchaStore) HRandField(ctx context.Context, key string, count int) ([]string, error) {
	return s.client.HRandField(ctx, key, count).Result()
}

func (s *redisCaptchaStore) Del(ctx context.Context, key ...string) error {
	return s.client.Del(ctx, key...).Err()
}

// ============================================================
// In-memory implementation (for dev/test)
// ============================================================

type memoryCaptchaStore struct {
	data map[string]map[string]string // key -> field -> value
}

// NewMemoryCaptchaStore creates an in-memory captcha.ICaptchaStore implementation (for dev/test).
func NewMemoryCaptchaStore() captcha.ICaptchaStore {
	return &memoryCaptchaStore{data: make(map[string]map[string]string)}
}

func (s *memoryCaptchaStore) HSet(ctx context.Context, key, field string, value interface{}) error {
	m, ok := s.data[key]
	if !ok {
		m = make(map[string]string)
		s.data[key] = m
	}
	m[field] = value.(string) // caller guarantees string type
	return nil
}

func (s *memoryCaptchaStore) HGet(ctx context.Context, key, field string) (string, error) {
	m, ok := s.data[key]
	if !ok {
		return "", redis.Nil
	}
	val, ok := m[field]
	if !ok {
		return "", redis.Nil
	}
	return val, nil
}

func (s *memoryCaptchaStore) HRandField(ctx context.Context, key string, count int) ([]string, error) {
	m, ok := s.data[key]
	if !ok || len(m) == 0 {
		return []string{}, nil
	}
	fields := make([]string, 0, len(m))
	for f := range m {
		fields = append(fields, f)
	}
	// Shuffle and take the first count items
	rand.Shuffle(len(fields), func(i, j int) {
		fields[i], fields[j] = fields[j], fields[i]
	})
	if count > len(fields) {
		count = len(fields)
	}
	return fields[:count], nil
}

func (s *memoryCaptchaStore) Del(ctx context.Context, key ...string) error {
	for _, k := range key {
		delete(s.data, k)
	}
	return nil
}
