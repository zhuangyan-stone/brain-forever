package captcha

import (
	"context"
	"math/rand"

	"github.com/redis/go-redis/v9"
)

// ============================================================
// Redis 适配器
// ============================================================

type redisStore struct {
	client *redis.Client
}

// NewRedisStore 创建基于 Redis 的 CaptchaStore 实现。
func NewRedisStore(client *redis.Client) CaptchaStore {
	return &redisStore{client: client}
}

func (s *redisStore) HSet(ctx context.Context, key, field string, value interface{}) error {
	return s.client.HSet(ctx, key, field, value).Err()
}

func (s *redisStore) HGet(ctx context.Context, key, field string) (string, error) {
	return s.client.HGet(ctx, key, field).Result()
}

func (s *redisStore) HRandField(ctx context.Context, key string, count int) ([]string, error) {
	return s.client.HRandField(ctx, key, count).Result()
}

func (s *redisStore) Del(ctx context.Context, key ...string) error {
	return s.client.Del(ctx, key...).Err()
}

// ============================================================
// 内存实现（开发/测试用）
// ============================================================

type memoryStore struct {
	data map[string]map[string]string // key -> field -> value
}

// NewMemoryStore 创建基于内存的 CaptchaStore 实现（开发/测试用）。
func NewMemoryStore() CaptchaStore {
	return &memoryStore{data: make(map[string]map[string]string)}
}

func (s *memoryStore) HSet(ctx context.Context, key, field string, value interface{}) error {
	m, ok := s.data[key]
	if !ok {
		m = make(map[string]string)
		s.data[key] = m
	}
	m[field] = value.(string) // 调用方确保传 string
	return nil
}

func (s *memoryStore) HGet(ctx context.Context, key, field string) (string, error) {
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

func (s *memoryStore) HRandField(ctx context.Context, key string, count int) ([]string, error) {
	m, ok := s.data[key]
	if !ok || len(m) == 0 {
		return []string{}, nil
	}
	fields := make([]string, 0, len(m))
	for f := range m {
		fields = append(fields, f)
	}
	// 随机打乱取前 count 个
	rand.Shuffle(len(fields), func(i, j int) {
		fields[i], fields[j] = fields[j], fields[i]
	})
	if count > len(fields) {
		count = len(fields)
	}
	return fields[:count], nil
}

func (s *memoryStore) Del(ctx context.Context, key ...string) error {
	for _, k := range key {
		delete(s.data, k)
	}
	return nil
}
