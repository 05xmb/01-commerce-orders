package service

import (
	"errors"
	"sync"
	"testing"
	"time"

	"trade/internal/types"
)

// R1-T3：下单成功占用库存并写流水
func TestCreateOrder_Success(t *testing.T) {
	f := newFixture(t)
	o, err := f.svc.Order.Create(f.ctx, "u1", "P1", 2)
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != types.OrderPending || o.Amount != 20000 || o.Price != 10000 {
		t.Fatalf("订单快照错误: %+v", o)
	}
	if got := f.product(t, "P1").Stock; got != 3 {
		t.Fatalf("库存应为 3，实际 %d", got)
	}
	if f.flowCount(t, o.OrderNo, types.FlowOccupy) != 1 {
		t.Fatal("缺下单占用流水")
	}
	if !o.PayDeadline.Equal(f.clock.Now().Add(30 * time.Second)) {
		t.Fatal("支付截止时间未按教学时钟计算")
	}
}

func TestCreateOrder_InvalidQty(t *testing.T) {
	f := newFixture(t)
	for _, q := range []int{0, 3, -1} {
		if _, err := f.svc.Order.Create(f.ctx, "u1", "P1", q); !errors.Is(err, types.ErrInvalidQty) {
			t.Fatalf("qty=%d 应拒绝，实际 %v", q, err)
		}
	}
}

func TestCreateOrder_OutOfStockAndOffSale(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.Order.Create(f.ctx, "u1", "P1", 3); !errors.Is(err, types.ErrInvalidQty) {
		t.Fatal("超单件数限制")
	}
	// 库存 5：两个 2 件订单占 4，再买 2 件应失败
	if _, err := f.svc.Order.Create(f.ctx, "u1", "P1", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Order.Create(f.ctx, "u2", "P1", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Order.Create(f.ctx, "u3", "P1", 2); !errors.Is(err, types.ErrOutOfStock) {
		t.Fatal("库存不足应拒绝")
	}
	// 下架后不可买
	if err := f.svc.Product.SetOnSale(f.ctx, "P2", false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Order.Create(f.ctx, "u1", "P2", 1); !errors.Is(err, types.ErrOutOfStock) {
		t.Fatal("下架商品应拒绝下单")
	}
}

// S2-T1：20 并发抢购库存 5，不超卖
func TestCreateOrder_ConcurrentNoOversell(t *testing.T) {
	f := newFixture(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := f.svc.Order.Create(f.ctx, "u1", "P1", 1)
			if err == nil {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if success != 5 {
		t.Fatalf("成功订单应为 5，实际 %d", success)
	}
	if got := f.product(t, "P1").Stock; got != 0 {
		t.Fatalf("库存应为 0，实际 %d", got)
	}
}

// R2-T2：重复取消只释放一次
func TestCancelOrder_ReleaseOnce(t *testing.T) {
	f := newFixture(t)
	o, _ := f.svc.Order.Create(f.ctx, "u1", "P1", 2)
	if err := f.svc.Order.Cancel(f.ctx, o.OrderNo, "u1"); err != nil {
		t.Fatal(err)
	}
	if got := f.product(t, "P1").Stock; got != 5 {
		t.Fatalf("库存应恢复 5，实际 %d", got)
	}
	if err := f.svc.Order.Cancel(f.ctx, o.OrderNo, "u1"); !errors.Is(err, types.ErrStatus) {
		t.Fatal("重复取消应拒绝")
	}
	if f.flowCount(t, o.OrderNo, types.FlowRelease) != 1 {
		t.Fatal("释放流水应只有一条")
	}
	if got := f.product(t, "P1").Stock; got != 5 {
		t.Fatal("重复取消导致重复释放")
	}
}

func TestCancelOrder_Forbidden(t *testing.T) {
	f := newFixture(t)
	o, _ := f.svc.Order.Create(f.ctx, "u1", "P1", 1)
	if err := f.svc.Order.Cancel(f.ctx, o.OrderNo, "u2"); !errors.Is(err, types.ErrForbidden) {
		t.Fatal("越权取消应拒绝")
	}
}

// A2：已付款订单不能直接取消
func TestCancelOrder_PaidRejected(t *testing.T) {
	f := newFixture(t)
	o := f.createAndPay(t, "u1", "P1", 1)
	if err := f.svc.Order.Cancel(f.ctx, o.OrderNo, "u1"); !errors.Is(err, types.ErrStatus) {
		t.Fatal("已付款订单取消应拒绝")
	}
}

// R2-T1 + A5：超时关单（定时扫描）
func TestCloseExpired(t *testing.T) {
	f := newFixture(t)
	o, _ := f.svc.Order.Create(f.ctx, "u1", "P1", 1)
	f.clock.Advance(31 * time.Second)
	n, err := f.svc.Order.CloseExpired(f.ctx)
	if err != nil || n != 1 {
		t.Fatalf("应关闭 1 单，实际 %d, err=%v", n, err)
	}
	if got := f.order(t, o.OrderNo); got.Status != types.OrderClosed || got.CloseReason == nil || *got.CloseReason != types.CloseByTimeout {
		t.Fatalf("订单应为超时关闭: %+v", got)
	}
	if f.flowCount(t, o.OrderNo, types.FlowRelease) != 1 {
		t.Fatal("超时释放流水缺失")
	}
	// 重复扫描不再释放
	if n, _ := f.svc.Order.CloseExpired(f.ctx); n != 0 {
		t.Fatal("重复扫描不应再次关闭")
	}
}

// A5：查询时惰性关单
func TestLazyCloseOnGet(t *testing.T) {
	f := newFixture(t)
	o, _ := f.svc.Order.Create(f.ctx, "u1", "P1", 1)
	f.clock.Advance(31 * time.Second)
	got, err := f.svc.Order.Get(f.ctx, o.OrderNo, "u1", types.RoleBuyer)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != types.OrderClosed {
		t.Fatalf("惰性关单未生效: %s", got.Status)
	}
}

// 越权查看他人订单（S5）
func TestGetOrder_Forbidden(t *testing.T) {
	f := newFixture(t)
	o, _ := f.svc.Order.Create(f.ctx, "u1", "P1", 1)
	if _, err := f.svc.Order.Get(f.ctx, o.OrderNo, "u2", types.RoleBuyer); !errors.Is(err, types.ErrForbidden) {
		t.Fatal("买家不应看到他人订单")
	}
	if _, err := f.svc.Order.Get(f.ctx, o.OrderNo, "u9", types.RoleOp); err != nil {
		t.Fatal("运营应可查看任意订单")
	}
}

// R1-T2：下单后改价不影响订单快照
func TestOrderSnapshot_Immutable(t *testing.T) {
	f := newFixture(t)
	o, _ := f.svc.Order.Create(f.ctx, "u1", "P1", 1)
	if err := f.svc.Product.SetPrice(f.ctx, "P1", 8888); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Product.SetOnSale(f.ctx, "P1", false); err != nil {
		t.Fatal(err)
	}
	got := f.order(t, o.OrderNo)
	if got.Price != 10000 || got.Amount != 10000 {
		t.Fatal("改价影响了历史订单")
	}
	if err := f.svc.Product.SetPrice(f.ctx, "P1", -1); !errors.Is(err, types.ErrParam) {
		t.Fatal("非法价格应拒绝")
	}
}
