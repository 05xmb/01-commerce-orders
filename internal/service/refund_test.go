package service

import (
	"errors"
	"sync"
	"testing"

	"trade/internal/types"
)

// S3 + R6：售前退款全链路（首次失败、重试成功、库存回补一次）
func TestRefundFlow_Presale(t *testing.T) {
	f := newFixture(t)
	o := f.createAndPay(t, "u1", "P1", 2)

	rf, err := f.svc.Refund.Apply(f.ctx, o.OrderNo, "u1", "买重了")
	if err != nil {
		t.Fatal(err)
	}
	if got := f.order(t, o.OrderNo); got.Status != types.OrderRefundReview {
		t.Fatal("申请期间应为退款审核中")
	}
	// 审核缺原因拒绝
	if err := f.svc.Refund.Review(f.ctx, rf.RefundNo, "cs1", true, ""); !errors.Is(err, types.ErrParam) {
		t.Fatal("审核原因必填")
	}
	// 同意 → 渠道受理
	if err := f.svc.Refund.Review(f.ctx, rf.RefundNo, "cs1", true, "同意退款"); err != nil {
		t.Fatal(err)
	}
	if len(f.ch.Calls()) != 1 {
		t.Fatal("应请求渠道一次")
	}
	// 渠道首次失败
	result, err := f.svc.Refund.HandleNotify(f.ctx, f.signedRefundNotify(rf.RefundNo, "CR-x", rf.Amount, "fail"))
	if err != nil || result != types.NotifyFailed {
		t.Fatalf("result=%s err=%v", result, err)
	}
	rfNow, _ := f.svc.d.Store.Repos().Refunds.Get(f.ctx, rf.RefundNo)
	if rfNow.Status != types.RefundFailedPending {
		t.Fatal("应为退款失败待处理")
	}
	// 客服重试
	if err := f.svc.Refund.Retry(f.ctx, rf.RefundNo); err != nil {
		t.Fatal(err)
	}
	if len(f.ch.Calls()) != 2 {
		t.Fatal("重试应再次请求渠道")
	}
	// 渠道成功
	result, err = f.svc.Refund.HandleNotify(f.ctx, f.signedRefundNotify(rf.RefundNo, "CR-y", rf.Amount, "success"))
	if err != nil || result != types.NotifySuccess {
		t.Fatalf("result=%s err=%v", result, err)
	}
	if got := f.order(t, o.OrderNo); got.Status != types.OrderRefunded {
		t.Fatal("订单应已退款")
	}
	if got := f.product(t, "P1").Stock; got != 5 {
		t.Fatalf("库存应回补到 5，实际 %d", got)
	}
	if f.flowCount(t, o.OrderNo, types.FlowRestore) != 1 {
		t.Fatal("回补流水应恰好一条")
	}
	// 重复成功通知不再回补
	result, _ = f.svc.Refund.HandleNotify(f.ctx, f.signedRefundNotify(rf.RefundNo, "CR-z", rf.Amount, "success"))
	if result != types.NotifyDuplicate {
		t.Fatal("重复退款通知应幂等")
	}
	if f.flowCount(t, o.OrderNo, types.FlowRestore) != 1 {
		t.Fatal("重复通知导致重复回补")
	}
}

// R5-T2：客服拒绝后可继续发货
func TestRefundReview_Reject(t *testing.T) {
	f := newFixture(t)
	o := f.createAndPay(t, "u1", "P1", 1)
	rf, _ := f.svc.Refund.Apply(f.ctx, o.OrderNo, "u1", "不想要了")
	if err := f.svc.Refund.Review(f.ctx, rf.RefundNo, "cs1", false, "不符合退款条件"); err != nil {
		t.Fatal(err)
	}
	if got := f.order(t, o.OrderNo); got.Status != types.OrderToShip {
		t.Fatal("拒绝后应回待发货")
	}
	if err := f.svc.Ship.Ship(f.ctx, o.OrderNo, "wh1", "顺丰", "SF001"); err != nil {
		t.Fatal("拒绝退款后应可发货")
	}
}

// R5-T3：退款申请与发货并发，只能有一种合法结果
func TestConcurrent_RefundVsShip(t *testing.T) {
	for round := 0; round < 10; round++ {
		f := newFixture(t)
		o := f.createAndPay(t, "u1", "P1", 1)
		var wg sync.WaitGroup
		var mu sync.Mutex
		refundOK, shipOK := 0, 0
		for i := 0; i < 5; i++ {
			wg.Add(2)
			go func() {
				defer wg.Done()
				if _, err := f.svc.Refund.Apply(f.ctx, o.OrderNo, "u1", "退款"); err == nil {
					mu.Lock()
					refundOK++
					mu.Unlock()
				}
			}()
			go func() {
				defer wg.Done()
				if err := f.svc.Ship.Ship(f.ctx, o.OrderNo, "wh1", "顺丰", "SF001"); err == nil {
					mu.Lock()
					shipOK++
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		got := f.order(t, o.OrderNo)
		if refundOK > 0 && shipOK > 0 {
			t.Fatal("退款与发货同时成功，违反互斥")
		}
		if refundOK > 0 && got.Status != types.OrderRefundReview {
			t.Fatalf("退款成功但状态为 %s", got.Status)
		}
		if shipOK > 1 {
			t.Fatal("重复发货")
		}
		if shipOK > 0 && got.Status != types.OrderShipped {
			t.Fatalf("发货成功但状态为 %s", got.Status)
		}
	}
}

// R5-T1：退款审核中禁止发货
func TestShip_BlockedDuringReview(t *testing.T) {
	f := newFixture(t)
	o := f.createAndPay(t, "u1", "P1", 1)
	if _, err := f.svc.Refund.Apply(f.ctx, o.OrderNo, "u1", "退款"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Ship.Ship(f.ctx, o.OrderNo, "wh1", "顺丰", "SF001"); !errors.Is(err, types.ErrStatus) {
		t.Fatal("审核中发货应被拒")
	}
}

// R7-T2：已发货不受理售前退款
func TestRefundApply_ShippedRejected(t *testing.T) {
	f := newFixture(t)
	o := f.createAndPay(t, "u1", "P1", 1)
	if err := f.svc.Ship.Ship(f.ctx, o.OrderNo, "wh1", "顺丰", "SF001"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Refund.Apply(f.ctx, o.OrderNo, "u1", "退款"); !errors.Is(err, types.ErrStatus) {
		t.Fatal("已发货订单退款应拒绝")
	}
}

func TestRefundApply_Guard(t *testing.T) {
	f := newFixture(t)
	o, _ := f.svc.Order.Create(f.ctx, "u1", "P1", 1)
	// 待付款不可申请
	if _, err := f.svc.Refund.Apply(f.ctx, o.OrderNo, "u1", "退款"); !errors.Is(err, types.ErrStatus) {
		t.Fatal("待付款订单不应走退款")
	}
	// 越权申请
	o2 := f.createAndPay(t, "u1", "P1", 1)
	if _, err := f.svc.Refund.Apply(f.ctx, o2.OrderNo, "u2", "退款"); !errors.Is(err, types.ErrForbidden) {
		t.Fatal("越权退款应拒绝")
	}
}

// R4-T2：迟到支付退款不回补库存
func TestRefundException_Late(t *testing.T) {
	f := newFixture(t)
	o, _ := f.svc.Order.Create(f.ctx, "u1", "P1", 1)
	if err := f.svc.Order.Cancel(f.ctx, o.OrderNo, "u1"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Notify.PayNotify(f.ctx, f.signedPayNotify("T-late", o.OrderNo, o.Amount)); err != nil {
		t.Fatal(err)
	}
	exs, _ := f.svc.d.Store.Repos().Exceptions.List(f.ctx, types.ExceptionPending)
	if len(exs) != 1 {
		t.Fatal("异常台账缺失")
	}
	stockBefore := f.product(t, "P1").Stock
	rf, err := f.svc.Refund.RefundException(f.ctx, exs[0].ID, "cs1")
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.svc.Refund.HandleNotify(f.ctx, f.signedRefundNotify(rf.RefundNo, "CR-late", rf.Amount, "success"))
	if err != nil || result != types.NotifySuccess {
		t.Fatalf("result=%s err=%v", result, err)
	}
	if got := f.product(t, "P1").Stock; got != stockBefore {
		t.Fatal("迟到支付退款不得回补库存")
	}
	if f.flowCount(t, o.OrderNo, types.FlowRestore) != 0 {
		t.Fatal("迟到支付退款不应产生回补流水")
	}
	exs, _ = f.svc.d.Store.Repos().Exceptions.List(f.ctx, types.ExceptionRefunded)
	if len(exs) != 1 {
		t.Fatal("异常台账应核销为已退款")
	}
	sum, _ := f.svc.d.Store.Repos().Refunds.SumSuccess(f.ctx)
	if sum != o.Amount {
		t.Fatal("退款金额应计入已退款汇总")
	}
}

// 渠道不可用时保留待处理记录
func TestRefund_ChannelDown(t *testing.T) {
	f := newFixture(t)
	f.ch.failNext = true
	o := f.createAndPay(t, "u1", "P1", 1)
	rf, _ := f.svc.Refund.Apply(f.ctx, o.OrderNo, "u1", "退款")
	if err := f.svc.Refund.Review(f.ctx, rf.RefundNo, "cs1", true, "同意"); err != nil {
		t.Fatal(err)
	}
	rfNow, _ := f.svc.d.Store.Repos().Refunds.Get(f.ctx, rf.RefundNo)
	if rfNow.Status != types.RefundFailedPending {
		t.Fatalf("渠道失败应保留待处理，实际 %s", rfNow.Status)
	}
	// 重试成功
	if err := f.svc.Refund.Retry(f.ctx, rf.RefundNo); err != nil {
		t.Fatal(err)
	}
	rfNow, _ = f.svc.d.Store.Repos().Refunds.Get(f.ctx, rf.RefundNo)
	if rfNow.Status != types.RefundRefunding || rfNow.ChannelRefundNo == "" {
		t.Fatal("重试应回到退款中并记录渠道退款号")
	}
}

func TestRefundNotify_BadSign(t *testing.T) {
	f := newFixture(t)
	n := f.signedRefundNotify("R1", "CR1", 100, "success")
	n.Sign = "bad"
	if _, err := f.svc.Refund.HandleNotify(f.ctx, n); !errors.Is(err, types.ErrSign) {
		t.Fatal("验签失败应拒绝")
	}
}
