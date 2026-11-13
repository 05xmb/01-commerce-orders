package service

import (
	"context"
	"errors"
	"testing"

	"trade/internal/repo"
	"trade/internal/types"
)

// 本文件专补覆盖率缺口（N-1 整改）：并发竞态窗口与数据库错误分支。
// 口径：internal/service 语句覆盖率 ≥85%。配套文件：covergap_notify_test.go / covergap_refund_test.go。

// raceStore 在首个事务开始前改写已提交状态，模拟"并发获胜方抢先提交"的竞态窗口。
// 必须在事务外改写：fake 事务是快照制（失败即弃），且 WithinTx 持有互斥锁，
// 事务内触碰已提交状态会重入同一把锁而死锁。
type raceStore struct {
	repo.Store
	pre  func()
	used bool
}

func (w *raceStore) WithinTx(ctx context.Context, fn func(repo.Repos) error) error {
	if !w.used {
		w.used = true
		w.pre()
	}
	return w.Store.WithinTx(ctx, fn)
}

func (f *fixture) installRace(pre func()) {
	// Deps 按值复制进各子服务，须重建 Services 才能让包装生效
	d := f.svc.d
	d.Store = &raceStore{Store: d.Store, pre: pre}
	f.svc = New(d)
}

// ---------- order.go：Create 重试 / closeOne / Cancel ----------

func TestCoverCreate_OrderNoRetryThenOK(t *testing.T) {
	f, plan := newFailFixture(t)
	plan.set("Orders.Insert", types.ErrDuplicate) // 首次订单号冲突，重试应成功（50-51、60）
	o, err := f.svc.Order.Create(f.ctx, "b1", "P1", 1)
	if err != nil || o.OrderNo == "" {
		t.Fatalf("冲突重试后应成功: %v", err)
	}
}

func TestCoverCreate_OrderNoRetryExhausted(t *testing.T) {
	f, plan := newFailFixture(t)
	plan.set("Orders.Insert",
		types.ErrDuplicate, types.ErrDuplicate, types.ErrDuplicate) // 3 次尝试全部冲突（64）
	_, err := f.svc.Order.Create(f.ctx, "b1", "P1", 1)
	if !errors.Is(err, types.ErrStatus) {
		t.Fatalf("重试耗尽应返回 ErrStatus: %v", err)
	}
}

func TestCoverCloseOne_IdempotentSecondClose(t *testing.T) {
	f, _ := newFailFixture(t)
	o, err := f.svc.Order.Create(f.ctx, "b1", "P1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Order.closeOne(f.ctx, o.OrderNo, "first"); err != nil {
		t.Fatal(err)
	}
	// 重复关闭：幂等返回 nil，不重复释放（75-76）
	if err := f.svc.Order.closeOne(f.ctx, o.OrderNo, "again"); err != nil {
		t.Fatalf("重复关闭应幂等: %v", err)
	}
}

func TestCoverCancel_ConcurrentPayWin(t *testing.T) {
	f, _ := newFailFixture(t)
	o, err := f.svc.Order.Create(f.ctx, "b1", "P1", 1)
	if err != nil {
		t.Fatal(err)
	}
	live := f.svc.d.Store
	f.installRace(func() {
		// 并发支付获胜：取消事务开始前订单已被标记已支付
		_, _ = live.Repos().Orders.MarkPaid(f.ctx, o.OrderNo, f.clock.Now())
	})
	err = f.svc.Order.Cancel(f.ctx, o.OrderNo, "b1")
	if !errors.Is(err, types.ErrStatus) {
		t.Fatalf("并发支付获胜后取消应失败（75-76、116-117）: %v", err)
	}
}

func TestCoverCancel_GetError(t *testing.T) {
	f, plan := newFailFixture(t)
	plan.set("Orders.Get", errDB)
	if err := f.svc.Order.Cancel(f.ctx, "O-none", "b1"); !errors.Is(err, errDB) {
		t.Fatalf("应透传数据库错误（99-100）: %v", err)
	}
}

func TestCoverCancel_FreshGetError(t *testing.T) {
	f, plan := newFailFixture(t)
	o, err := f.svc.Order.Create(f.ctx, "b1", "P1", 1)
	if err != nil {
		t.Fatal(err)
	}
	// 队列：取消首查过、closeOne 复查过、终态复查失败（113-114）
	plan.set("Orders.Get", nil, nil, errDB)
	if err := f.svc.Order.Cancel(f.ctx, o.OrderNo, "b1"); !errors.Is(err, errDB) {
		t.Fatalf("终态复查失败应透传: %v", err)
	}
}

func TestCoverCloseOne_GetError(t *testing.T) {
	f, plan := newFailFixture(t)
	o, err := f.svc.Order.Create(f.ctx, "b1", "P1", 1)
	if err != nil {
		t.Fatal(err)
	}
	plan.set("Orders.Get", errDB)
	if err := f.svc.Order.closeOne(f.ctx, o.OrderNo, "x"); !errors.Is(err, errDB) {
		t.Fatalf("关闭后取单失败应透传（79-80）: %v", err)
	}
}

func TestCoverCloseOne_ProductGetError(t *testing.T) {
	f, plan := newFailFixture(t)
	o, err := f.svc.Order.Create(f.ctx, "b1", "P1", 1)
	if err != nil {
		t.Fatal(err)
	}
	plan.set("Products.Get", errDB)
	if err := f.svc.Order.closeOne(f.ctx, o.OrderNo, "x"); !errors.Is(err, errDB) {
		t.Fatalf("释放库存取商品失败应透传（86-87）: %v", err)
	}
}

// ---------- order.go：Get 的发货单/退款填充分支 ----------

func TestCoverOrderGet_ShipmentVisible(t *testing.T) {
	f, _ := newFailFixture(t)
	o := f.createAndPay(t, "b1", "P1", 1)
	if err := f.svc.Ship.Ship(f.ctx, o.OrderNo, "wh1", "顺丰", "SF-1"); err != nil {
		t.Fatal(err)
	}
	v, err := f.svc.Order.Get(f.ctx, o.OrderNo, "b1", types.RoleBuyer)
	if err != nil {
		t.Fatal(err)
	}
	if v.Shipment == nil {
		t.Fatal("已发货订单应携带发货单（166-168）")
	}
}

func TestCoverOrderGet_ShippedWithoutShipment(t *testing.T) {
	f, _ := newFailFixture(t)
	o := f.createAndPay(t, "b1", "P1", 1)
	// 直接置为已发货但无发货记录：应容忍（169）
	if ok, err := f.svc.d.Store.Repos().Orders.SetStatusCond(f.ctx,
		o.OrderNo, types.OrderToShip, types.OrderShipped); err != nil || !ok {
		t.Fatalf("置发货态失败: ok=%v err=%v", ok, err)
	}
	v, err := f.svc.Order.Get(f.ctx, o.OrderNo, "b1", types.RoleBuyer)
	if err != nil {
		t.Fatal(err)
	}
	if v.Shipment != nil {
		t.Fatal("无发货记录时发货单应为空")
	}
}

func TestCoverOrderGet_ShipmentGetError(t *testing.T) {
	f, plan := newFailFixture(t)
	o := f.createAndPay(t, "b1", "P1", 1)
	if err := f.svc.Ship.Ship(f.ctx, o.OrderNo, "wh1", "顺丰", "SF-1"); err != nil {
		t.Fatal(err)
	}
	plan.set("Shipments.GetByOrder", errDB)
	if _, err := f.svc.Order.Get(f.ctx, o.OrderNo, "b1", types.RoleBuyer); !errors.Is(err, errDB) {
		t.Fatalf("发货单查询失败应透传（170-171）: %v", err)
	}
}

func TestCoverOrderGet_RefundListError(t *testing.T) {
	f, plan := newFailFixture(t)
	o := f.createAndPay(t, "b1", "P1", 1)
	plan.set("Refunds.ListByOrder", errDB)
	if _, err := f.svc.Order.Get(f.ctx, o.OrderNo, "b1", types.RoleBuyer); !errors.Is(err, errDB) {
		t.Fatalf("退款列表失败应透传（174-175）: %v", err)
	}
}
