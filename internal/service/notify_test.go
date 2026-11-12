package service

import (
	"errors"
	"testing"
	"time"

	"trade/internal/types"
)

// R3：支付成功通知
func TestPayNotify_Success(t *testing.T) {
	f := newFixture(t)
	o, _ := f.svc.Order.Create(f.ctx, "u1", "P1", 1)
	result, err := f.svc.Notify.PayNotify(f.ctx, f.signedPayNotify("T1", o.OrderNo, o.Amount))
	if err != nil || result != types.NotifySuccess {
		t.Fatalf("result=%s err=%v", result, err)
	}
	if got := f.order(t, o.OrderNo); got.Status != types.OrderToShip || got.PaidAt == nil {
		t.Fatal("订单应转待发货")
	}
	p, err := f.svc.d.Store.Repos().Payments.GetByTradeNo(f.ctx, "T1")
	if err != nil || p.Amount != o.Amount {
		t.Fatal("收款记录缺失")
	}
}

// R3-T1：重复通知不重复记款
func TestPayNotify_Duplicate(t *testing.T) {
	f := newFixture(t)
	o, _ := f.svc.Order.Create(f.ctx, "u1", "P1", 1)
	n := f.signedPayNotify("T2", o.OrderNo, o.Amount)
	if _, err := f.svc.Notify.PayNotify(f.ctx, n); err != nil {
		t.Fatal(err)
	}
	result, err := f.svc.Notify.PayNotify(f.ctx, n)
	if err != nil || result != types.NotifyDuplicate {
		t.Fatalf("重复通知应幂等，result=%s err=%v", result, err)
	}
	sum, _ := f.svc.d.Store.Repos().Payments.SumAmount(f.ctx)
	if sum != o.Amount {
		t.Fatalf("重复记款：sum=%d", sum)
	}
}

// R3-T2：同一交易号挂两个订单 → 冲突
func TestPayNotify_Conflict(t *testing.T) {
	f := newFixture(t)
	o1, _ := f.svc.Order.Create(f.ctx, "u1", "P1", 1)
	o2, _ := f.svc.Order.Create(f.ctx, "u2", "P1", 1)
	if _, err := f.svc.Notify.PayNotify(f.ctx, f.signedPayNotify("T3", o1.OrderNo, o1.Amount)); err != nil {
		t.Fatal(err)
	}
	result, err := f.svc.Notify.PayNotify(f.ctx, f.signedPayNotify("T3", o2.OrderNo, o2.Amount))
	if !errors.Is(err, types.ErrConflict) || result != types.NotifyConflict {
		t.Fatalf("应报冲突，result=%s err=%v", result, err)
	}
	if got := f.order(t, o2.OrderNo); got.Status != types.OrderPending {
		t.Fatal("第二订单不应被支付")
	}
}

// R3-T3：金额不符 / 未知订单 / 验签失败
func TestPayNotify_Rejected(t *testing.T) {
	f := newFixture(t)
	o, _ := f.svc.Order.Create(f.ctx, "u1", "P1", 1)

	result, _ := f.svc.Notify.PayNotify(f.ctx, f.signedPayNotify("T4", o.OrderNo, o.Amount-100))
	if result != types.NotifyAmountMismatch {
		t.Fatal("金额不符应拒绝")
	}
	result, _ = f.svc.Notify.PayNotify(f.ctx, f.signedPayNotify("T5", "O00000000000000", 10000))
	if result != types.NotifyUnknownOrder {
		t.Fatal("未知订单应拒绝")
	}
	n := f.signedPayNotify("T6", o.OrderNo, o.Amount)
	n.Sign = "bad"
	result, err := f.svc.Notify.PayNotify(f.ctx, n)
	if !errors.Is(err, types.ErrSign) || result != types.NotifyBadSign {
		t.Fatal("验签失败应拒绝")
	}
	if got := f.order(t, o.OrderNo); got.Status != types.OrderPending {
		t.Fatal("被拒通知不应改变订单状态")
	}
	// 三类拒绝均有日志可查
	logs, _ := f.svc.d.Store.Repos().PayLogs.List(f.ctx, 10)
	if len(logs) != 3 {
		t.Fatalf("应有 3 条拒绝日志，实际 %d", len(logs))
	}
}

// R4-T1：关闭后收到成功通知 → 异常支付，不恢复订单、不占库存
func TestPayNotify_LateAfterClose(t *testing.T) {
	f := newFixture(t)
	o, _ := f.svc.Order.Create(f.ctx, "u1", "P1", 2)
	if err := f.svc.Order.Cancel(f.ctx, o.OrderNo, "u1"); err != nil {
		t.Fatal(err)
	}
	stockAfterClose := f.product(t, "P1").Stock
	result, err := f.svc.Notify.PayNotify(f.ctx, f.signedPayNotify("T7", o.OrderNo, o.Amount))
	if err != nil || result != types.NotifyLate {
		t.Fatalf("应入异常台账，result=%s err=%v", result, err)
	}
	if got := f.order(t, o.OrderNo); got.Status != types.OrderClosed {
		t.Fatal("迟到通知不得恢复订单")
	}
	if got := f.product(t, "P1").Stock; got != stockAfterClose {
		t.Fatal("迟到通知不得重新占用库存")
	}
	exs, _ := f.svc.d.Store.Repos().Exceptions.List(f.ctx, types.ExceptionPending)
	if len(exs) != 1 || exs[0].OrderNo != o.OrderNo {
		t.Fatal("异常支付台账缺失")
	}
	// 重复迟到通知幂等
	result, _ = f.svc.Notify.PayNotify(f.ctx, f.signedPayNotify("T7", o.OrderNo, o.Amount))
	if result != types.NotifyDuplicate {
		t.Fatal("重复迟到通知应幂等")
	}
	exs, _ = f.svc.d.Store.Repos().Exceptions.List(f.ctx, "")
	if len(exs) != 1 {
		t.Fatal("重复迟到通知不应产生第二条异常记录")
	}
}

// R4：超时未关单时通知到达 → 先惰性关单再入异常
func TestPayNotify_LateAfterTimeout(t *testing.T) {
	f := newFixture(t)
	o, _ := f.svc.Order.Create(f.ctx, "u1", "P1", 1)
	f.clock.Advance(31 * time.Second)
	result, err := f.svc.Notify.PayNotify(f.ctx, f.signedPayNotify("T8", o.OrderNo, o.Amount))
	if err != nil || result != types.NotifyLate {
		t.Fatalf("result=%s err=%v", result, err)
	}
	if got := f.order(t, o.OrderNo); got.Status != types.OrderClosed {
		t.Fatal("订单应已超时关闭")
	}
	if f.flowCount(t, o.OrderNo, types.FlowRelease) != 1 {
		t.Fatal("超时释放流水缺失")
	}
}

// 已支付订单再收同交易号通知 → 重复（幂等）
func TestPayNotify_AlreadyPaid(t *testing.T) {
	f := newFixture(t)
	o := f.createAndPay(t, "u1", "P1", 1)
	result, _ := f.svc.Notify.PayNotify(f.ctx, f.signedPayNotify("T-"+o.OrderNo, o.OrderNo, o.Amount))
	if result != types.NotifyDuplicate {
		t.Fatalf("同交易号重复通知应幂等，result=%s", result)
	}
}

// 审计 M-2 回归：已支付订单收到不同交易号的成功通知（买家重复付款）→ 入异常台账，可查询可退款
func TestPayNotify_SecondPaymentGoesException(t *testing.T) {
	f := newFixture(t)
	o := f.createAndPay(t, "u1", "P1", 1)
	stockBefore := f.product(t, "P1").Stock

	// 第二笔不同交易号的成功通知（买家重复支付同一订单）
	result, err := f.svc.Notify.PayNotify(f.ctx, f.signedPayNotify("T-second", o.OrderNo, o.Amount))
	if err != nil || result != types.NotifyLate {
		t.Fatalf("第二笔支付应入异常台账，result=%s err=%v", result, err)
	}
	// 订单状态不变、库存不变
	if got := f.order(t, o.OrderNo); got.Status != types.OrderToShip {
		t.Fatalf("订单应保持待发货，实际 %s", got.Status)
	}
	if got := f.product(t, "P1").Stock; got != stockBefore {
		t.Fatalf("库存不应变化，实际 %d", got)
	}
	// 异常台账可见
	exs, _ := f.svc.d.Store.Repos().Exceptions.List(f.ctx, types.ExceptionPending)
	if len(exs) != 1 || exs[0].TradeNo != "T-second" {
		t.Fatalf("异常台账应有第二笔支付记录: %+v", exs)
	}
	// 客服可对该笔误收款发起退款（type=late，不回补库存）
	rf, err := f.svc.Refund.RefundException(f.ctx, exs[0].ID, "cs1")
	if err != nil {
		t.Fatal(err)
	}
	if rf.Type != types.RefundTypeLate || rf.Amount != o.Amount {
		t.Fatalf("退款类型/金额错误: %+v", rf)
	}
}
