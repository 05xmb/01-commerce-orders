package service

import (
	"errors"
	"testing"
	"time"

	"trade/internal/types"
)

// notify.go 覆盖率缺口补测：PayNotify 的错误分支与并发竞态窗口（配套 covergap_test.go）。

func TestCoverPayNotify_OrderGetError(t *testing.T) {
	f, plan := newFailFixture(t)
	o, err := f.svc.Order.Create(f.ctx, "b1", "P1", 1)
	if err != nil {
		t.Fatal(err)
	}
	plan.set("Orders.Get", errDB)
	n := f.signedPayNotify("T-"+o.OrderNo, o.OrderNo, o.Amount)
	if _, err := f.svc.Notify.PayNotify(f.ctx, n); !errors.Is(err, errDB) {
		t.Fatalf("取单失败应透传（50-51）: %v", err)
	}
}

func TestCoverPayNotify_LazyCloseFails(t *testing.T) {
	f, plan := newFailFixture(t)
	o, err := f.svc.Order.Create(f.ctx, "b1", "P1", 1)
	if err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(31 * time.Second) // 超过 30s 支付时限
	plan.set("Orders.ClosePending", errDB)
	n := f.signedPayNotify("T-"+o.OrderNo, o.OrderNo, o.Amount)
	if _, err := f.svc.Notify.PayNotify(f.ctx, n); !errors.Is(err, errDB) {
		t.Fatalf("惰性关单失败必须暴露（60-61）: %v", err)
	}
}

func TestCoverPayNotify_LazyCloseRefetchError(t *testing.T) {
	f, plan := newFailFixture(t)
	o, err := f.svc.Order.Create(f.ctx, "b1", "P1", 1)
	if err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(31 * time.Second)
	// 队列：首查过、closeOne 内部复查过、关单后重取失败（62-63）
	plan.set("Orders.Get", nil, nil, errDB)
	n := f.signedPayNotify("T-"+o.OrderNo, o.OrderNo, o.Amount)
	if _, err := f.svc.Notify.PayNotify(f.ctx, n); !errors.Is(err, errDB) {
		t.Fatalf("关单后重取失败应透传: %v", err)
	}
}

func TestCoverPayNotify_MarkPaidError(t *testing.T) {
	f, plan := newFailFixture(t)
	o, err := f.svc.Order.Create(f.ctx, "b1", "P1", 1)
	if err != nil {
		t.Fatal(err)
	}
	plan.set("Orders.MarkPaid", errDB)
	n := f.signedPayNotify("T-"+o.OrderNo, o.OrderNo, o.Amount)
	if _, err := f.svc.Notify.PayNotify(f.ctx, n); !errors.Is(err, errDB) {
		t.Fatalf("记账失败应透传（71-72）: %v", err)
	}
}

func TestCoverPayNotify_RaceCloseThenLate(t *testing.T) {
	f, _ := newFailFixture(t)
	o, err := f.svc.Order.Create(f.ctx, "b1", "P1", 1)
	if err != nil {
		t.Fatal(err)
	}
	live := f.svc.d.Store
	f.installRace(func() {
		// 并发关单获胜：MarkPaid 事务前订单已被关闭
		_, _ = live.Repos().Orders.ClosePending(f.ctx, o.OrderNo, f.clock.Now(), types.CloseByTimeout)
	})
	n := f.signedPayNotify("T-"+o.OrderNo, o.OrderNo, o.Amount)
	result, err := f.svc.Notify.PayNotify(f.ctx, n)
	if err != nil || result != types.NotifyLate {
		t.Fatalf("并发关单获胜应入异常台账（74-75、93-94）: result=%s err=%v", result, err)
	}
}

func TestCoverPayNotify_RaceDuplicateRecorded(t *testing.T) {
	f, _ := newFailFixture(t)
	o, err := f.svc.Order.Create(f.ctx, "b1", "P1", 1)
	if err != nil {
		t.Fatal(err)
	}
	trade := "T-" + o.OrderNo
	live := f.svc.d.Store
	f.installRace(func() {
		// 并发同交易号通知已抢先记款并置为已支付（已提交状态）
		r := live.Repos()
		_, _ = r.Orders.MarkPaid(f.ctx, o.OrderNo, f.clock.Now())
		_ = r.Payments.Insert(f.ctx, types.Payment{
			OrderNo: o.OrderNo, TradeNo: trade, Amount: o.Amount, Status: "success",
		})
	})
	n := f.signedPayNotify(trade, o.OrderNo, o.Amount)
	result, err := f.svc.Notify.PayNotify(f.ctx, n)
	if err != nil || result != types.NotifyDuplicate {
		t.Fatalf("复查发现同款记录应幂等（84-88）: result=%s err=%v", result, err)
	}
}

func TestCoverPayNotify_RaceConflict(t *testing.T) {
	f, _ := newFailFixture(t)
	o, err := f.svc.Order.Create(f.ctx, "b1", "P1", 1)
	if err != nil {
		t.Fatal(err)
	}
	trade := "T-" + o.OrderNo
	live := f.svc.d.Store
	f.installRace(func() {
		// 同交易号已归属另一订单（不同订单号）：必须判冲突，不得记款
		r := live.Repos()
		_, _ = r.Orders.MarkPaid(f.ctx, o.OrderNo, f.clock.Now())
		_ = r.Payments.Insert(f.ctx, types.Payment{
			OrderNo: "O-OTHER", TradeNo: trade, Amount: o.Amount, Status: "success",
		})
	})
	n := f.signedPayNotify(trade, o.OrderNo, o.Amount)
	result, err := f.svc.Notify.PayNotify(f.ctx, n)
	if result != types.NotifyConflict || !errors.Is(err, types.ErrConflict) {
		t.Fatalf("同交易号异单应冲突（89-90）: result=%s err=%v", result, err)
	}
}

func TestCoverPayNotify_RecheckGetError(t *testing.T) {
	f, plan := newFailFixture(t)
	o, err := f.svc.Order.Create(f.ctx, "b1", "P1", 1)
	if err != nil {
		t.Fatal(err)
	}
	live := f.svc.d.Store
	f.installRace(func() {
		_, _ = live.Repos().Orders.ClosePending(f.ctx, o.OrderNo, f.clock.Now(), types.CloseByTimeout)
	})
	// 队列：首查未找到（放行），复查数据库错误必须暴露（91-92）
	plan.set("Payments.GetByTradeNo", types.ErrNotFound, errDB)
	n := f.signedPayNotify("T-"+o.OrderNo, o.OrderNo, o.Amount)
	if _, err := f.svc.Notify.PayNotify(f.ctx, n); !errors.Is(err, errDB) {
		t.Fatalf("复查失败必须暴露: %v", err)
	}
}

func TestCoverPayNotify_InsertDuplicate(t *testing.T) {
	f, plan := newFailFixture(t)
	o, err := f.svc.Order.Create(f.ctx, "b1", "P1", 1)
	if err != nil {
		t.Fatal(err)
	}
	plan.set("Payments.Insert", types.ErrDuplicate)
	n := f.signedPayNotify("T-"+o.OrderNo, o.OrderNo, o.Amount)
	result, err := f.svc.Notify.PayNotify(f.ctx, n)
	if err != nil || result != types.NotifyDuplicate {
		t.Fatalf("记款冲突应幂等（96-98）: result=%s err=%v", result, err)
	}
}

func TestCoverPayNotify_SameTradeOnPaidOrder(t *testing.T) {
	f, plan := newFailFixture(t)
	o := f.createAndPay(t, "b1", "P1", 1)
	// 首查故意放过（注入未找到），终态分支按订单维度复查同交易号 → 幂等（111-113）
	plan.set("Payments.GetByTradeNo", types.ErrNotFound)
	n := f.signedPayNotify("T-"+o.OrderNo, o.OrderNo, o.Amount)
	result, err := f.svc.Notify.PayNotify(f.ctx, n)
	if err != nil || result != types.NotifyDuplicate {
		t.Fatalf("同交易号重复通知应幂等: result=%s err=%v", result, err)
	}
}

func TestCoverPayNotify_PaidStateGetByOrderError(t *testing.T) {
	f, plan := newFailFixture(t)
	o := f.createAndPay(t, "b1", "P1", 1)
	plan.set("Payments.GetByTradeNo", types.ErrNotFound)
	plan.set("Payments.GetByOrder", errDB)
	n := f.signedPayNotify("T-"+o.OrderNo, o.OrderNo, o.Amount)
	if _, err := f.svc.Notify.PayNotify(f.ctx, n); !errors.Is(err, errDB) {
		t.Fatalf("终态按单复查失败应透传（114-115）: %v", err)
	}
}
