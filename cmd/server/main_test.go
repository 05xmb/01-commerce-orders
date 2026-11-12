package main

import (
	"context"
	"errors"
	"testing"

	"trade/internal/repo"
	"trade/internal/repo/fake"
	"trade/internal/types"
)

// 空库种子：6 账号 + 2 商品
func TestSeed_Empty(t *testing.T) {
	store := fake.NewStore()
	if err := seed(context.Background(), store, "ut-seed-pass"); err != nil {
		t.Fatal(err)
	}
	r := store.Repos()
	n, _ := r.Users.Count(context.Background())
	if n != 6 {
		t.Fatalf("应预置 6 个账号，实际 %d", n)
	}
	u, err := r.Users.GetByUsername(context.Background(), "buyer01")
	if err != nil || u.Role != types.RoleBuyer || u.PasswordHash == "" {
		t.Fatal("buyer01 应存在且带口令散列")
	}
	for _, name := range []string{"op01", "wh01", "cs01"} {
		if _, err := r.Users.GetByUsername(context.Background(), name); err != nil {
			t.Fatalf("%s 应存在", name)
		}
	}
	p1, err := r.Products.Get(context.Background(), "P1")
	if err != nil || p1.Price != 10000 || p1.Stock != 5 || !p1.OnSale {
		t.Fatalf("商品A参数错误: %+v", p1)
	}
	p2, err := r.Products.Get(context.Background(), "P2")
	if err != nil || p2.Price != 5000 || p2.Stock != 10 {
		t.Fatalf("商品B参数错误: %+v", p2)
	}
}

// 幂等：重复执行不报错、不重复插入
func TestSeed_Idempotent(t *testing.T) {
	store := fake.NewStore()
	ctx := context.Background()
	if err := seed(ctx, store, "ut-seed-pass"); err != nil {
		t.Fatal(err)
	}
	if err := seed(ctx, store, "ut-seed-pass"); err != nil {
		t.Fatalf("重复种子应幂等，实际 %v", err)
	}
	n, _ := store.Repos().Users.Count(ctx)
	if n != 6 {
		t.Fatalf("重复执行后仍应为 6 个账号，实际 %d", n)
	}
}

// 已有账号（Count>0）时跳过账号预置，仅补商品
func TestSeed_ExistingUsers(t *testing.T) {
	store := fake.NewStore()
	ctx := context.Background()
	r := store.Repos()
	_ = r.Users.Insert(ctx, types.User{ID: "u-x", Username: "someone", PasswordHash: "x", Role: types.RoleBuyer})
	if err := seed(ctx, store, "ut-seed-pass"); err != nil {
		t.Fatal(err)
	}
	n, _ := r.Users.Count(ctx)
	if n != 1 {
		t.Fatalf("已有账号不应重复预置，实际 %d", n)
	}
	if _, err := r.Products.Get(ctx, "P1"); err != nil {
		t.Fatal("商品仍应预置")
	}
}

// failStore 注入 Count 失败
type failStore struct {
	repo.Store
}

func (s *failStore) Repos() repo.Repos {
	r := s.Store.Repos()
	r.Users = failUsers{r.Users}
	return r
}

type failUsers struct{ repo.UserRepo }

func (failUsers) Count(context.Context) (int, error) { return 0, errors.New("db down") }

// 底层错误应向上传递
func TestSeed_Error(t *testing.T) {
	store := &failStore{Store: fake.NewStore()}
	if err := seed(context.Background(), store, "ut-seed-pass"); err == nil {
		t.Fatal("Count 失败应传递错误")
	}
}
