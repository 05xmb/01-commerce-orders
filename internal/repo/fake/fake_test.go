package fake

import (
	"context"
	"errors"
	"testing"
	"time"

	"trade/internal/repo"
	"trade/internal/types"
)

func newRepos() repo.Repos { return NewStore().Repos() }

// 事务：成功提交生效，失败回滚
func TestWithinTx_CommitAndRollback(t *testing.T) {
	s := NewStore()
	ctx := context.Background()
	if err := s.Repos().Products.Insert(ctx, types.Product{ID: "P1", Price: 100, Stock: 5, OnSale: true}); err != nil {
		t.Fatal(err)
	}
	// 成功：变更生效（提交后 state 指针更新，需重新获取 Repos）
	err := s.WithinTx(ctx, func(tr repo.Repos) error {
		return tr.Products.SetPrice(ctx, "P1", 200)
	})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.Repos().Products.Get(ctx, "P1")
	if p.Price != 200 {
		t.Fatalf("提交后价格应为 200，实际 %d", p.Price)
	}
	// 失败：回滚
	err = s.WithinTx(ctx, func(tr repo.Repos) error {
		if err := tr.Products.SetPrice(ctx, "P1", 300); err != nil {
			t.Fatal(err)
		}
		return errors.New("boom")
	})
	if err == nil {
		t.Fatal("应返回错误")
	}
	p, _ = s.Repos().Products.Get(ctx, "P1")
	if p.Price != 200 {
		t.Fatalf("回滚后价格应为 200，实际 %d", p.Price)
	}
}

func TestProductRepo(t *testing.T) {
	ctx := context.Background()
	r := newRepos()
	// Insert / Get / 重复
	if err := r.Products.Insert(ctx, types.Product{ID: "P1", Name: "A", Price: 100, Stock: 5, OnSale: true}); err != nil {
		t.Fatal(err)
	}
	if err := r.Products.Insert(ctx, types.Product{ID: "P1"}); !errors.Is(err, types.ErrDuplicate) {
		t.Fatal("重复 Insert 应报重复")
	}
	p, err := r.Products.Get(ctx, "P1")
	if err != nil || p.Name != "A" {
		t.Fatal("Get 失败")
	}
	if _, err := r.Products.Get(ctx, "none"); !errors.Is(err, types.ErrNotFound) {
		t.Fatal("不存在应 ErrNotFound")
	}
	// List 过滤上架
	_ = r.Products.Insert(ctx, types.Product{ID: "P2", OnSale: false})
	list, _ := r.Products.List(ctx, true)
	if len(list) != 1 || list[0].ID != "P1" {
		t.Fatal("仅上架应只返回 P1")
	}
	list, _ = r.Products.List(ctx, false)
	if len(list) != 2 {
		t.Fatal("全部应返回 2 个")
	}
	// TryDeduct：库存足 / 不足 / 下架 / 不存在
	ok, err := r.Products.TryDeduct(ctx, "P1", 2)
	if !ok || err != nil {
		t.Fatal("扣减应成功")
	}
	p, _ = r.Products.Get(ctx, "P1")
	if p.Stock != 3 {
		t.Fatalf("库存应为 3，实际 %d", p.Stock)
	}
	ok, _ = r.Products.TryDeduct(ctx, "P1", 4)
	if ok {
		t.Fatal("库存不足应失败")
	}
	ok, _ = r.Products.TryDeduct(ctx, "P2", 1)
	if ok {
		t.Fatal("下架商品应扣减失败")
	}
	if _, err := r.Products.TryDeduct(ctx, "none", 1); !errors.Is(err, types.ErrNotFound) {
		t.Fatal("不存在应 ErrNotFound")
	}
	// AddStock / SetPrice / SetOnSale
	if err := r.Products.AddStock(ctx, "P1", 2); err != nil {
		t.Fatal(err)
	}
	p, _ = r.Products.Get(ctx, "P1")
	if p.Stock != 5 {
		t.Fatalf("回补后应为 5，实际 %d", p.Stock)
	}
	if err := r.Products.AddStock(ctx, "none", 1); !errors.Is(err, types.ErrNotFound) {
		t.Fatal("不存在应 ErrNotFound")
	}
	if err := r.Products.SetPrice(ctx, "P1", 999); err != nil {
		t.Fatal(err)
	}
	if err := r.Products.SetPrice(ctx, "none", 1); !errors.Is(err, types.ErrNotFound) {
		t.Fatal("不存在应 ErrNotFound")
	}
	if err := r.Products.SetOnSale(ctx, "P1", false); err != nil {
		t.Fatal(err)
	}
	p, _ = r.Products.Get(ctx, "P1")
	if p.OnSale {
		t.Fatal("应已下架")
	}
	if err := r.Products.SetOnSale(ctx, "none", true); !errors.Is(err, types.ErrNotFound) {
		t.Fatal("不存在应 ErrNotFound")
	}
}

func TestStockFlowRepo(t *testing.T) {
	ctx := context.Background()
	r := newRepos()
	f1 := types.StockFlow{ProductID: "P1", OrderNo: "O1", ChangeType: types.FlowOccupy, Qty: 1}
	f2 := types.StockFlow{ProductID: "P1", OrderNo: "O1", ChangeType: types.FlowRestore, Qty: 1}
	f3 := types.StockFlow{ProductID: "P2", OrderNo: "O2", ChangeType: types.FlowOccupy, Qty: 1}
	for _, f := range []types.StockFlow{f1, f2, f3} {
		if err := r.StockFlows.Insert(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	list, _ := r.StockFlows.ListByProduct(ctx, "P1")
	if len(list) != 2 {
		t.Fatalf("P1 应有 2 条流水，实际 %d", len(list))
	}
	list, _ = r.StockFlows.ListByOrder(ctx, "O1")
	if len(list) != 2 {
		t.Fatal("O1 应有 2 条流水")
	}
	n, _ := r.StockFlows.CountByOrderType(ctx, "O1", types.FlowRestore)
	if n != 1 {
		t.Fatalf("O1 回补应为 1 次，实际 %d", n)
	}
	// 自增 ID 与创建时间
	if list[0].ID == 0 || list[0].CreatedAt.IsZero() {
		t.Fatal("流水应有自增 ID 与创建时间")
	}
}

func TestOrderRepo(t *testing.T) {
	ctx := context.Background()
	r := newRepos()
	now := time.Now()
	o1 := types.Order{OrderNo: "O1", BuyerID: "b1", Status: types.OrderPending, PayDeadline: now.Add(-time.Minute), CreatedAt: now}
	o2 := types.Order{OrderNo: "O2", BuyerID: "b1", Status: types.OrderToShip, PayDeadline: now.Add(time.Minute), CreatedAt: now.Add(time.Second)}
	o3 := types.Order{OrderNo: "O3", BuyerID: "b2", Status: types.OrderPending, PayDeadline: now.Add(-time.Minute), CreatedAt: now.Add(2 * time.Second)}
	for _, o := range []types.Order{o1, o2, o3} {
		if err := r.Orders.Insert(ctx, o); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Orders.Insert(ctx, o1); !errors.Is(err, types.ErrDuplicate) {
		t.Fatal("重复应报错")
	}
	o, err := r.Orders.Get(ctx, "O1")
	if err != nil || o.BuyerID != "b1" {
		t.Fatal("Get 失败")
	}
	if _, err := r.Orders.Get(ctx, "none"); !errors.Is(err, types.ErrNotFound) {
		t.Fatal("不存在应 ErrNotFound")
	}
	// ListByBuyer：按创建时间倒序
	list, _ := r.Orders.ListByBuyer(ctx, "b1")
	if len(list) != 2 || list[0].OrderNo != "O2" {
		t.Fatal("ListByBuyer 排序错误")
	}
	// ListByStatus
	list, _ = r.Orders.ListByStatus(ctx, types.OrderPending)
	if len(list) != 2 {
		t.Fatal("待付款应有 2 单")
	}
	// ListExpiredPending
	list, _ = r.Orders.ListExpiredPending(ctx, now)
	if len(list) != 2 {
		t.Fatalf("过期待付款应有 2 单，实际 %d", len(list))
	}
	// SetStatusCond
	ok, err := r.Orders.SetStatusCond(ctx, "O2", types.OrderToShip, types.OrderShipped)
	if !ok || err != nil {
		t.Fatal("条件流转应成功")
	}
	ok, _ = r.Orders.SetStatusCond(ctx, "O2", types.OrderToShip, types.OrderShipped)
	if ok {
		t.Fatal("状态不符应失败")
	}
	if _, err := r.Orders.SetStatusCond(ctx, "none", "a", "b"); !errors.Is(err, types.ErrNotFound) {
		t.Fatal("不存在应 ErrNotFound")
	}
	// MarkPaid
	paidAt := now
	ok, err = r.Orders.MarkPaid(ctx, "O1", paidAt)
	if !ok || err != nil {
		t.Fatal("MarkPaid 应成功")
	}
	o, _ = r.Orders.Get(ctx, "O1")
	if o.Status != types.OrderToShip || o.PaidAt == nil {
		t.Fatal("应已支付")
	}
	ok, _ = r.Orders.MarkPaid(ctx, "O1", paidAt)
	if ok {
		t.Fatal("非待付款不应重复支付")
	}
	if _, err := r.Orders.MarkPaid(ctx, "none", paidAt); !errors.Is(err, types.ErrNotFound) {
		t.Fatal("不存在应 ErrNotFound")
	}
	// ClosePending
	ok, err = r.Orders.ClosePending(ctx, "O3", now, "超时关闭")
	if !ok || err != nil {
		t.Fatal("ClosePending 应成功")
	}
	o, _ = r.Orders.Get(ctx, "O3")
	if o.Status != types.OrderClosed || o.CloseReason == nil || *o.CloseReason != "超时关闭" {
		t.Fatal("应已关闭并记录原因")
	}
	ok, _ = r.Orders.ClosePending(ctx, "O3", now, "重复")
	if ok {
		t.Fatal("重复关闭应失败")
	}
	if _, err := r.Orders.ClosePending(ctx, "none", now, "x"); !errors.Is(err, types.ErrNotFound) {
		t.Fatal("不存在应 ErrNotFound")
	}
}

func TestPaymentRepo(t *testing.T) {
	ctx := context.Background()
	r := newRepos()
	p1 := types.Payment{OrderNo: "O1", TradeNo: "T1", Amount: 100, Status: "success"}
	p2 := types.Payment{OrderNo: "O2", TradeNo: "T2", Amount: 200, Status: "fail"}
	for _, p := range []types.Payment{p1, p2} {
		if err := r.Payments.Insert(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Payments.Insert(ctx, types.Payment{OrderNo: "O3", TradeNo: "T1"}); !errors.Is(err, types.ErrDuplicate) {
		t.Fatal("交易号重复应报错")
	}
	p, err := r.Payments.GetByTradeNo(ctx, "T1")
	if err != nil || p.OrderNo != "O1" {
		t.Fatal("GetByTradeNo 失败")
	}
	if _, err := r.Payments.GetByTradeNo(ctx, "none"); !errors.Is(err, types.ErrNotFound) {
		t.Fatal("不存在应 ErrNotFound")
	}
	p, err = r.Payments.GetByOrder(ctx, "O2")
	if err != nil || p.TradeNo != "T2" {
		t.Fatal("GetByOrder 失败")
	}
	if _, err := r.Payments.GetByOrder(ctx, "none"); !errors.Is(err, types.ErrNotFound) {
		t.Fatal("不存在应 ErrNotFound")
	}
	sum, _ := r.Payments.SumAmount(ctx)
	if sum != 100 {
		t.Fatalf("成功支付合计应为 100，实际 %d", sum)
	}
}

func TestPayLogRepo(t *testing.T) {
	ctx := context.Background()
	r := newRepos()
	for i := 0; i < 3; i++ {
		if err := r.PayLogs.Insert(ctx, types.PayNotifyLog{NotifyID: "N", OrderNo: "O1", Result: "success"}); err != nil {
			t.Fatal(err)
		}
	}
	list, _ := r.PayLogs.List(ctx, 2)
	if len(list) != 2 {
		t.Fatal("limit 应生效")
	}
	if list[0].ID < list[1].ID {
		t.Fatal("应按 ID 倒序")
	}
	list, _ = r.PayLogs.List(ctx, 0)
	if len(list) != 3 {
		t.Fatal("默认 limit 应返回全部")
	}
	list, _ = r.PayLogs.ListByOrder(ctx, "O1")
	if len(list) != 3 {
		t.Fatal("ListByOrder 应返回 3 条")
	}
	list, _ = r.PayLogs.ListByOrder(ctx, "none")
	if len(list) != 0 {
		t.Fatal("不存在应空")
	}
}

func TestRefundRepo(t *testing.T) {
	ctx := context.Background()
	r := newRepos()
	rf := types.Refund{RefundNo: "R1", OrderNo: "O1", Amount: 100, Status: types.RefundReview, CreatedAt: time.Now()}
	if err := r.Refunds.Insert(ctx, rf); err != nil {
		t.Fatal(err)
	}
	if err := r.Refunds.Insert(ctx, rf); !errors.Is(err, types.ErrDuplicate) {
		t.Fatal("重复应报错")
	}
	got, err := r.Refunds.Get(ctx, "R1")
	if err != nil || got.Amount != 100 {
		t.Fatal("Get 失败")
	}
	if _, err := r.Refunds.Get(ctx, "none"); !errors.Is(err, types.ErrRefundNotFound) {
		t.Fatal("不存在应 ErrRefundNotFound")
	}
	// ListByOrder / ListByStatus
	list, _ := r.Refunds.ListByOrder(ctx, "O1")
	if len(list) != 1 {
		t.Fatal("ListByOrder 应返回 1 条")
	}
	list, _ = r.Refunds.ListByStatus(ctx, types.RefundReview)
	if len(list) != 1 {
		t.Fatal("ListByStatus 应返回 1 条")
	}
	// Approve：审核中 → 退款中（Attempt+1）
	ok, err := r.Refunds.Approve(ctx, "R1", "同意")
	if !ok || err != nil {
		t.Fatal("Approve 应成功")
	}
	got, _ = r.Refunds.Get(ctx, "R1")
	if got.Status != types.RefundRefunding || got.Attempt != 1 || got.CsNote != "同意" {
		t.Fatalf("Approve 状态错误: %+v", got)
	}
	ok, _ = r.Refunds.Approve(ctx, "R1", "重复")
	if ok {
		t.Fatal("非审核中不应重复同意")
	}
	if _, err := r.Refunds.Approve(ctx, "none", "x"); !errors.Is(err, types.ErrRefundNotFound) {
		t.Fatal("不存在应 ErrRefundNotFound")
	}
	// AttachChannel
	if err := r.Refunds.AttachChannel(ctx, "R1", "CR1"); err != nil {
		t.Fatal(err)
	}
	got, _ = r.Refunds.Get(ctx, "R1")
	if got.ChannelRefundNo != "CR1" {
		t.Fatal("应记录渠道退款号")
	}
	if err := r.Refunds.AttachChannel(ctx, "none", "CR"); !errors.Is(err, types.ErrRefundNotFound) {
		t.Fatal("不存在应 ErrRefundNotFound")
	}
	// MarkFailedPending：退款中 → 失败待处理
	ok, err = r.Refunds.MarkFailedPending(ctx, "R1")
	if !ok || err != nil {
		t.Fatal("MarkFailedPending 应成功")
	}
	ok, _ = r.Refunds.MarkFailedPending(ctx, "R1")
	if ok {
		t.Fatal("非退款中不应重复标记")
	}
	if _, err := r.Refunds.MarkFailedPending(ctx, "none"); !errors.Is(err, types.ErrRefundNotFound) {
		t.Fatal("不存在应 ErrRefundNotFound")
	}
	// MarkRefunding：失败待处理 → 退款中（Attempt+1）
	ok, err = r.Refunds.MarkRefunding(ctx, "R1")
	if !ok || err != nil {
		t.Fatal("MarkRefunding 应成功")
	}
	got, _ = r.Refunds.Get(ctx, "R1")
	if got.Status != types.RefundRefunding || got.Attempt != 2 {
		t.Fatalf("重试应退款中且 Attempt=2: %+v", got)
	}
	ok, _ = r.Refunds.MarkRefunding(ctx, "R1")
	if ok {
		t.Fatal("非失败待处理不应重试")
	}
	// MarkSuccess
	ok, err = r.Refunds.MarkSuccess(ctx, "R1")
	if !ok || err != nil {
		t.Fatal("MarkSuccess 应成功")
	}
	ok, _ = r.Refunds.MarkSuccess(ctx, "R1")
	if ok {
		t.Fatal("已成功不应重复")
	}
	if _, err := r.Refunds.MarkSuccess(ctx, "none"); !errors.Is(err, types.ErrRefundNotFound) {
		t.Fatal("不存在应 ErrRefundNotFound")
	}
	// Reject（用新单）
	rf2 := types.Refund{RefundNo: "R2", OrderNo: "O1", Amount: 50, Status: types.RefundReview, CreatedAt: time.Now()}
	_ = r.Refunds.Insert(ctx, rf2)
	ok, err = r.Refunds.Reject(ctx, "R2", "拒绝")
	if !ok || err != nil {
		t.Fatal("Reject 应成功")
	}
	got, _ = r.Refunds.Get(ctx, "R2")
	if got.Status != types.RefundRejected {
		t.Fatal("应已拒绝")
	}
	ok, _ = r.Refunds.Reject(ctx, "R2", "重复")
	if ok {
		t.Fatal("非审核中不应重复拒绝")
	}
	if _, err := r.Refunds.Reject(ctx, "none", "x"); !errors.Is(err, types.ErrRefundNotFound) {
		t.Fatal("不存在应 ErrRefundNotFound")
	}
	// SumSuccess
	sum, _ := r.Refunds.SumSuccess(ctx)
	if sum != 100 {
		t.Fatalf("成功退款合计应为 100，实际 %d", sum)
	}
}

func TestRefundLogRepo(t *testing.T) {
	ctx := context.Background()
	r := newRepos()
	_ = r.RefundLogs.Insert(ctx, types.RefundNotifyLog{NotifyID: "RN1", RefundNo: "R1", Result: "success"})
	_ = r.RefundLogs.Insert(ctx, types.RefundNotifyLog{NotifyID: "RN2", RefundNo: "R2", Result: "fail"})
	list, _ := r.RefundLogs.ListByRefund(ctx, "R1")
	if len(list) != 1 || list[0].ID == 0 || list[0].CreatedAt.IsZero() {
		t.Fatal("ListByRefund 应返回带 ID 的日志")
	}
	list, _ = r.RefundLogs.ListByRefund(ctx, "none")
	if len(list) != 0 {
		t.Fatal("不存在应空")
	}
}

func TestShipmentRepo(t *testing.T) {
	ctx := context.Background()
	r := newRepos()
	sh := types.Shipment{OrderNo: "O1", Carrier: "顺丰", TrackingNo: "SF123"}
	if err := r.Shipments.Insert(ctx, sh); err != nil {
		t.Fatal(err)
	}
	if err := r.Shipments.Insert(ctx, sh); !errors.Is(err, types.ErrDuplicate) {
		t.Fatal("一单多发货记录应报错")
	}
	got, err := r.Shipments.GetByOrder(ctx, "O1")
	if err != nil || got.TrackingNo != "SF123" || got.ID == 0 {
		t.Fatal("GetByOrder 失败")
	}
	if _, err := r.Shipments.GetByOrder(ctx, "none"); !errors.Is(err, types.ErrNotFound) {
		t.Fatal("不存在应 ErrNotFound")
	}
	list, _ := r.Shipments.List(ctx)
	if len(list) != 1 {
		t.Fatal("List 应返回 1 条")
	}
}

func TestExceptionRepo(t *testing.T) {
	ctx := context.Background()
	r := newRepos()
	ex := types.ExceptionPayment{OrderNo: "O1", TradeNo: "T1", Amount: 100, Status: types.ExceptionPending}
	if err := r.Exceptions.Insert(ctx, ex); err != nil {
		t.Fatal(err)
	}
	if err := r.Exceptions.Insert(ctx, ex); !errors.Is(err, types.ErrDuplicate) {
		t.Fatal("交易号重复应报错（幂等）")
	}
	list, _ := r.Exceptions.List(ctx, "")
	if len(list) != 1 || list[0].ID == 0 {
		t.Fatal("List 应返回 1 条带 ID")
	}
	list, _ = r.Exceptions.List(ctx, types.ExceptionPending)
	if len(list) != 1 {
		t.Fatal("按状态过滤应返回 1 条")
	}
	list, _ = r.Exceptions.List(ctx, types.ExceptionRefunded)
	if len(list) != 0 {
		t.Fatal("无已退款应空")
	}
	list, _ = r.Exceptions.ListByOrder(ctx, "O1")
	if len(list) != 1 {
		t.Fatal("ListByOrder 应返回 1 条")
	}
	// AttachRefund
	now := time.Now()
	ok, err := r.Exceptions.AttachRefund(ctx, 1, "cs1", "R1", now)
	if !ok || err != nil {
		t.Fatal("AttachRefund 应成功")
	}
	list, _ = r.Exceptions.List(ctx, "")
	if list[0].Handler == nil || *list[0].RefundNo != "R1" || list[0].HandledAt == nil {
		t.Fatal("应记录处理人、退款单号与处理时间")
	}
	ok, _ = r.Exceptions.AttachRefund(ctx, 1, "cs1", "R2", now)
	if ok {
		t.Fatal("非待处理不应重复处理")
	}
	if _, err := r.Exceptions.AttachRefund(ctx, 999, "cs1", "R1", now); !errors.Is(err, types.ErrNotFound) {
		t.Fatal("不存在应 ErrNotFound")
	}
	// MarkRefundedByRefundNo
	if err := r.Exceptions.MarkRefundedByRefundNo(ctx, "R1"); err != nil {
		t.Fatal(err)
	}
	list, _ = r.Exceptions.List(ctx, types.ExceptionRefunded)
	if len(list) != 1 {
		t.Fatal("应核销为已退款")
	}
}

func TestUserRepo(t *testing.T) {
	ctx := context.Background()
	r := newRepos()
	u := types.User{ID: "u1", Username: "buyer01", Role: types.RoleBuyer}
	if err := r.Users.Insert(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := r.Users.Insert(ctx, types.User{ID: "u2", Username: "buyer01"}); !errors.Is(err, types.ErrDuplicate) {
		t.Fatal("用户名重复应报错")
	}
	got, err := r.Users.GetByUsername(ctx, "buyer01")
	if err != nil || got.ID != "u1" {
		t.Fatal("GetByUsername 失败")
	}
	if _, err := r.Users.GetByUsername(ctx, "none"); !errors.Is(err, types.ErrNotFound) {
		t.Fatal("不存在应 ErrNotFound")
	}
	got, err = r.Users.GetByID(ctx, "u1")
	if err != nil || got.Username != "buyer01" {
		t.Fatal("GetByID 失败")
	}
	if _, err := r.Users.GetByID(ctx, "none"); !errors.Is(err, types.ErrNotFound) {
		t.Fatal("不存在应 ErrNotFound")
	}
	n, _ := r.Users.Count(ctx)
	if n != 1 {
		t.Fatalf("应有 1 个用户，实际 %d", n)
	}
}

// 并发事务互斥（-race 下验证）
func TestWithinTx_Concurrent(t *testing.T) {
	s := NewStore()
	ctx := context.Background()
	r := s.Repos()
	_ = r.Products.Insert(ctx, types.Product{ID: "P1", Price: 100, Stock: 100, OnSale: true})
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 20; j++ {
				_ = s.WithinTx(ctx, func(tr repo.Repos) error {
					_, err := tr.Products.TryDeduct(ctx, "P1", 1)
					return err
				})
			}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}
