package service

import (
	"errors"
	"testing"

	"trade/internal/pkg/sign"
	"trade/internal/types"
)

func hashPassword(pw string) string { return sign.HashPassword(pw) }

// R7-T1：发货成功且仅一次
func TestShip_Success(t *testing.T) {
	f := newFixture(t)
	o := f.createAndPay(t, "u1", "P1", 1)
	if err := f.svc.Ship.Ship(f.ctx, o.OrderNo, "wh1", "顺丰", "SF001"); err != nil {
		t.Fatal(err)
	}
	if got := f.order(t, o.OrderNo); got.Status != types.OrderShipped {
		t.Fatal("订单应已发货")
	}
	sh, err := f.svc.Ship.GetByOrder(f.ctx, o.OrderNo)
	if err != nil || sh.TrackingNo != "SF001" || sh.Operator != "wh1" {
		t.Fatal("发货记录缺失")
	}
	// 重复发货
	if err := f.svc.Ship.Ship(f.ctx, o.OrderNo, "wh1", "圆通", "YT001"); !errors.Is(err, types.ErrStatus) {
		t.Fatal("重复发货应拒绝")
	}
}

func TestShip_Guard(t *testing.T) {
	f := newFixture(t)
	o, _ := f.svc.Order.Create(f.ctx, "u1", "P1", 1)
	// 待付款不可发货
	if err := f.svc.Ship.Ship(f.ctx, o.OrderNo, "wh1", "顺丰", "SF001"); !errors.Is(err, types.ErrStatus) {
		t.Fatal("待付款订单不应发货")
	}
	// 承运方/运单号必填
	o2 := f.createAndPay(t, "u1", "P1", 1)
	if err := f.svc.Ship.Ship(f.ctx, o2.OrderNo, "wh1", "", "SF002"); !errors.Is(err, types.ErrParam) {
		t.Fatal("承运方必填")
	}
}

// R8：运营汇总与穿透查询
func TestOps_SummaryAndDetail(t *testing.T) {
	f := newFixture(t)
	o1 := f.createAndPay(t, "u1", "P1", 1)
	o2 := f.createAndPay(t, "u2", "P1", 2)

	sum, err := f.svc.Ops.Summary(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sum.PaidAmount != o1.Amount+o2.Amount {
		t.Fatalf("已收款应为 %d，实际 %d", o1.Amount+o2.Amount, sum.PaidAmount)
	}

	// 退款一单后汇总
	rf, _ := f.svc.Refund.Apply(f.ctx, o1.OrderNo, "u1", "退款")
	_ = f.svc.Refund.Review(f.ctx, rf.RefundNo, "cs1", true, "同意")
	_, _ = f.svc.Refund.HandleNotify(f.ctx, f.signedRefundNotify(rf.RefundNo, "CR-s", rf.Amount, "success"))
	sum, _ = f.svc.Ops.Summary(f.ctx)
	if sum.RefundedAmount != o1.Amount {
		t.Fatal("已退款汇总错误")
	}

	// 穿透查询
	detail, err := f.svc.Ops.OrderDetail(f.ctx, o1.OrderNo)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Payment == nil || detail.Shipment != nil {
		t.Fatal("穿透数据不完整")
	}
	if len(detail.Refunds) != 1 || len(detail.NotifyLogs) == 0 || len(detail.StockFlows) == 0 {
		t.Fatal("穿透明细缺失")
	}
	if _, err := f.svc.Ops.OrderDetail(f.ctx, "O00000000000000"); !errors.Is(err, types.ErrNotFound) {
		t.Fatal("未知订单应 404")
	}
}

func TestAuth(t *testing.T) {
	f := newFixture(t)
	r := f.svc.d.Store.Repos()
	_ = r.Users.Insert(f.ctx, types.User{ID: "u-cs2", Username: "cs02", PasswordHash: hashPassword("123456"), Role: types.RoleCS})

	// 密码错误
	if _, err := f.svc.Auth.Login(f.ctx, "cs02", "wrong"); !errors.Is(err, types.ErrUnauthorized) {
		t.Fatal("密码错误应拒绝")
	}
	// 用户不存在
	if _, err := f.svc.Auth.Login(f.ctx, "nobody", "123456"); !errors.Is(err, types.ErrUnauthorized) {
		t.Fatal("未知用户应拒绝")
	}
	// 登录成功并校验 token
	res, err := f.svc.Auth.Login(f.ctx, "cs02", "123456")
	if err != nil {
		t.Fatal(err)
	}
	id, role, err := f.svc.Auth.Verify(f.ctx, res.Token)
	if err != nil || id != "u-cs2" || role != types.RoleCS {
		t.Fatal("token 校验失败")
	}
	if _, _, err := f.svc.Auth.Verify(f.ctx, "bad-token"); !errors.Is(err, types.ErrUnauthorized) {
		t.Fatal("非法 token 应拒绝")
	}
	if err := f.svc.Auth.Logout(f.ctx, res.Token); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.svc.Auth.Verify(f.ctx, res.Token); !errors.Is(err, types.ErrUnauthorized) {
		t.Fatal("登出后 token 应失效")
	}
}

func TestWarehouseList(t *testing.T) {
	f := newFixture(t)
	o := f.createAndPay(t, "u1", "P1", 1)
	list, err := f.svc.Order.ListForWarehouse(f.ctx)
	if err != nil || len(list) != 1 || list[0].OrderNo != o.OrderNo {
		t.Fatal("仓库应看到待发货订单")
	}
}
