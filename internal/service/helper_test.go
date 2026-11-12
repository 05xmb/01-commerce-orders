package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"trade/internal/pkg/sign"
	"trade/internal/repo/fake"
	"trade/internal/tokstore"
	"trade/internal/types"
)

const testSecret = "test-secret"

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type fakeChannel struct {
	mu       sync.Mutex
	calls    []string
	failNext bool
}

func (c *fakeChannel) RequestRefund(_ context.Context, refundNo string, amount int64) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failNext {
		c.failNext = false
		return "", types.ErrChannel
	}
	c.calls = append(c.calls, refundNo)
	return "CR-" + refundNo, nil
}

func (c *fakeChannel) Calls() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.calls...)
}

type fixture struct {
	svc   *Services
	clock *fakeClock
	ch    *fakeChannel
	ctx   context.Context
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	clock := &fakeClock{t: time.Date(2026, 11, 12, 10, 0, 0, 0, time.Local)}
	ch := &fakeChannel{}
	store := fake.NewStore()
	ctx := context.Background()
	r := store.Repos()
	if err := r.Products.Insert(ctx, types.Product{ID: "P1", Name: "商品A", Price: 10000, Stock: 5, OnSale: true}); err != nil {
		t.Fatal(err)
	}
	if err := r.Products.Insert(ctx, types.Product{ID: "P2", Name: "商品B", Price: 5000, Stock: 10, OnSale: true}); err != nil {
		t.Fatal(err)
	}
	svc := New(Deps{
		Store:   store,
		Clock:   clock.Now,
		Timeout: func() int { return 30 },
		Secret:  testSecret,
		Channel: ch,
		Tokens:  tokstore.NewMemoryStore(),
	})
	return &fixture{svc: svc, clock: clock, ch: ch, ctx: ctx}
}

// payAndShip 辅助：下单并支付成功
func (f *fixture) createAndPay(t *testing.T, buyerID, productID string, qty int) types.Order {
	t.Helper()
	o, err := f.svc.Order.Create(f.ctx, buyerID, productID, qty)
	if err != nil {
		t.Fatalf("下单失败: %v", err)
	}
	n := f.signedPayNotify("T-"+o.OrderNo, o.OrderNo, o.Amount)
	result, err := f.svc.Notify.PayNotify(f.ctx, n)
	if err != nil || result != types.NotifySuccess {
		t.Fatalf("支付通知失败: result=%s err=%v", result, err)
	}
	return o
}

func (f *fixture) signedPayNotify(tradeNo, orderNo string, amount int64) types.PayNotification {
	n := types.PayNotification{
		NotifyID: "N-" + tradeNo, TradeNo: tradeNo, OrderNo: orderNo,
		Amount: amount, Ts: f.clock.Now().Unix(),
	}
	n.Sign = sign.Sign(testSecret, sign.PaySignParts(n.NotifyID, n.TradeNo, n.OrderNo, n.Amount, n.Ts)...)
	return n
}

func (f *fixture) signedRefundNotify(refundNo, channelRefundNo string, amount int64, result string) types.RefundNotification {
	n := types.RefundNotification{
		NotifyID: "RN-" + refundNo + "-" + result, ChannelRefundNo: channelRefundNo,
		RefundNo: refundNo, Amount: amount, Result: result, Ts: f.clock.Now().Unix(),
	}
	n.Sign = sign.Sign(testSecret, sign.RefundSignParts(n.NotifyID, n.ChannelRefundNo, n.RefundNo, n.Amount, n.Result, n.Ts)...)
	return n
}

func (f *fixture) product(t *testing.T, id string) types.Product {
	t.Helper()
	p, err := f.svc.d.Store.Repos().Products.Get(f.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (f *fixture) order(t *testing.T, orderNo string) types.Order {
	t.Helper()
	o, err := f.svc.d.Store.Repos().Orders.Get(f.ctx, orderNo)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func (f *fixture) flowCount(t *testing.T, orderNo, changeType string) int {
	t.Helper()
	n, err := f.svc.d.Store.Repos().StockFlows.CountByOrderType(f.ctx, orderNo, changeType)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
