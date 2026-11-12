// Package tokstore 登录令牌存储：Redis 实现（生产）与内存实现（测试/降级）。
package tokstore

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"trade/internal/types"
)

type RedisStore struct {
	Rdb *redis.Client
}

func (s *RedisStore) Set(ctx context.Context, token, value string, ttl time.Duration) error {
	return s.Rdb.Set(ctx, "token:"+token, value, ttl).Err()
}

func (s *RedisStore) Get(ctx context.Context, token string) (string, error) {
	v, err := s.Rdb.Get(ctx, "token:"+token).Result()
	if errors.Is(err, redis.Nil) {
		return "", types.ErrUnauthorized
	}
	return v, err
}

func (s *RedisStore) Del(ctx context.Context, token string) error {
	return s.Rdb.Del(ctx, "token:"+token).Err()
}

type memItem struct {
	value  string
	expire time.Time
}

type MemoryStore struct {
	mu sync.Mutex
	m  map[string]memItem
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{m: map[string]memItem{}} }

func (s *MemoryStore) Set(_ context.Context, token, value string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[token] = memItem{value: value, expire: time.Now().Add(ttl)}
	return nil
}

func (s *MemoryStore) Get(_ context.Context, token string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.m[token]
	if !ok || time.Now().After(it.expire) {
		return "", types.ErrUnauthorized
	}
	return it.value, nil
}

func (s *MemoryStore) Del(_ context.Context, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, token)
	return nil
}
