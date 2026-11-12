package tokstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"trade/internal/types"
)

// RedisStore 全路径（miniredis 内存 Redis 实例）
func TestRedisStore(t *testing.T) {
	mr := miniredis.RunT(t)
	s := &RedisStore{Rdb: redis.NewClient(&redis.Options{Addr: mr.Addr()})}
	ctx := context.Background()

	// 未设置 → 未授权
	if _, err := s.Get(ctx, "none"); !errors.Is(err, types.ErrUnauthorized) {
		t.Fatal("未设置应未授权")
	}
	// 设置 → 读取
	if err := s.Set(ctx, "tok", "uid|role", time.Hour); err != nil {
		t.Fatal(err)
	}
	v, err := s.Get(ctx, "tok")
	if err != nil || v != "uid|role" {
		t.Fatalf("读取失败，v=%s err=%v", v, err)
	}
	// 删除 → 未授权
	if err := s.Del(ctx, "tok"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, "tok"); !errors.Is(err, types.ErrUnauthorized) {
		t.Fatal("删除后应未授权")
	}
}

// MemoryStore 全路径
func TestMemoryStore(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()

	// 未设置 → 未授权
	if _, err := s.Get(ctx, "none"); !errors.Is(err, types.ErrUnauthorized) {
		t.Fatal("未设置应未授权")
	}
	// 设置 → 读取
	if err := s.Set(ctx, "tok", "uid|role", time.Hour); err != nil {
		t.Fatal(err)
	}
	v, err := s.Get(ctx, "tok")
	if err != nil || v != "uid|role" {
		t.Fatalf("读取失败，v=%s err=%v", v, err)
	}
	// 过期 → 未授权
	if err := s.Set(ctx, "expired", "uid|role", -time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, "expired"); !errors.Is(err, types.ErrUnauthorized) {
		t.Fatal("过期应未授权")
	}
	// 删除 → 未授权
	if err := s.Del(ctx, "tok"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, "tok"); !errors.Is(err, types.ErrUnauthorized) {
		t.Fatal("删除后应未授权")
	}
}

// MemoryStore 并发读写安全（-race 下验证）
func TestMemoryStore_Concurrent(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			tok := string(rune('a' + i))
			for j := 0; j < 100; j++ {
				_ = s.Set(ctx, tok, "v", time.Hour)
				_, _ = s.Get(ctx, tok)
				_ = s.Del(ctx, tok)
			}
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}
