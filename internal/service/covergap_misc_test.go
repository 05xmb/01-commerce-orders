package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"trade/internal/pkg/sign"
	"trade/internal/tokstore"
	"trade/internal/types"
)

// auth/ops/shipment 覆盖率缺口补测（配套 covergap_test.go）。

// errTokenStore 让 Token.Set 恒失败，覆盖登录落 token 失败分支
type errTokenStore struct{ TokenStore }

func (errTokenStore) Set(context.Context, string, string, time.Duration) error { return errDB }

func TestCoverLogin_TokenSetError(t *testing.T) {
	f, _ := newFailFixture(t)
	r := f.svc.d.Store.Repos()
	if err := r.Users.Insert(f.ctx, types.User{
		ID: "u-alice", Username: "alice",
		PasswordHash: sign.HashPassword("pw123"), Name: "Alice", Role: types.RoleBuyer,
	}); err != nil {
		t.Fatal(err)
	}
	// Deps 按值复制进各子服务，须重建 Services 才能让失败 token 仓储生效
	d := f.svc.d
	d.Tokens = errTokenStore{TokenStore: tokstore.NewMemoryStore()}
	f.svc = New(d)
	if _, err := f.svc.Auth.Login(f.ctx, "alice", "pw123"); !errors.Is(err, errDB) {
		t.Fatalf("落 token 失败应透传（30-31）: %v", err)
	}
}

func TestCoverOpsDetail_ShipmentVisible(t *testing.T) {
	f, _ := newFailFixture(t)
	o := f.createAndPay(t, "b1", "P1", 1)
	if err := f.svc.Ship.Ship(f.ctx, o.OrderNo, "wh1", "顺丰", "SF-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Ops.OrderDetail(f.ctx, o.OrderNo); err != nil {
		t.Fatalf("运营视角应带发货单: %v", err)
	}
}

func TestCoverOpsDetail_ShipmentGetError(t *testing.T) {
	f, plan := newFailFixture(t)
	o := f.createAndPay(t, "b1", "P1", 1)
	if err := f.svc.Ship.Ship(f.ctx, o.OrderNo, "wh1", "顺丰", "SF-1"); err != nil {
		t.Fatal(err)
	}
	plan.set("Shipments.GetByOrder", errDB)
	if _, err := f.svc.Ops.OrderDetail(f.ctx, o.OrderNo); !errors.Is(err, errDB) {
		t.Fatalf("发货单查询失败应透传（40-41）: %v", err)
	}
}

func TestCoverShip_InsertDuplicate(t *testing.T) {
	f, plan := newFailFixture(t)
	o := f.createAndPay(t, "b1", "P1", 1)
	plan.set("Shipments.Insert", types.ErrDuplicate)
	err := f.svc.Ship.Ship(f.ctx, o.OrderNo, "wh1", "顺丰", "SF-1")
	if !errors.Is(err, types.ErrStatus) {
		t.Fatalf("发货记录冲突应报状态错误（31-32）: %v", err)
	}
}
