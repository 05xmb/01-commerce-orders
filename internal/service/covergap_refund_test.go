package service

import (
	"errors"
	"strings"
	"testing"

	"trade/internal/types"
)

// refund.go 覆盖率缺口补测（配套 covergap_test.go）。

func coverRefundOn(t *testing.T, f *fixture) (types.Order, types.Refund) {
	t.Helper()
	o := f.createAndPay(t, "b1", "P1", 1)
	rf, err := f.svc.Refund.Apply(f.ctx, o.OrderNo, "b1", "不想要了")
	if err != nil {
		t.Fatal(err)
	}
	return o, rf
}

func coverRefundRefunding(t *testing.T, f *fixture) (types.Order, types.Refund) {
	t.Helper()
	o, rf := coverRefundOn(t, f)
	if err := f.svc.Refund.Review(f.ctx, rf.RefundNo, "cs1", true, "同意"); err != nil {
		t.Fatal(err)
	}
	return o, rf
}

func coverSeedException(t *testing.T, f *fixture, trade string) int64 {
	t.Helper()
	r := f.svc.d.Store.Repos()
	if err := r.Exceptions.Insert(f.ctx, types.ExceptionPayment{
		OrderNo: "O-X", TradeNo: trade, Amount: 5000, Status: types.ExceptionPending,
	}); err != nil {
		t.Fatal(err)
	}
	exs, err := r.Exceptions.List(f.ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range exs {
		if e.TradeNo == trade {
			return e.ID
		}
	}
	t.Fatal("未找到种子异常记录")
	return 0
}

// ---------- Review 拒绝/批准分支 ----------

func TestCoverRefundReview_RejectRepoError(t *testing.T) {
	f, plan := newFailFixture(t)
	_, rf := coverRefundOn(t, f)
	plan.set("Refunds.Reject", errDB)
	if err := f.svc.Refund.Review(f.ctx, rf.RefundNo, "cs1", false, "拒绝"); !errors.Is(err, errDB) {
		t.Fatalf("拒绝落库失败应透传（57-58）: %v", err)
	}
}

func TestCoverRefundReview_RejectAgain(t *testing.T) {
	f, _ := newFailFixture(t)
	_, rf := coverRefundOn(t, f)
	if err := f.svc.Refund.Review(f.ctx, rf.RefundNo, "cs1", false, "拒绝"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Refund.Review(f.ctx, rf.RefundNo, "cs1", false, "再拒"); !errors.Is(err, types.ErrStatus) {
		t.Fatalf("重复拒绝应报状态错误（60-61）: %v", err)
	}
}

func TestCoverRefundReview_SetStatusError(t *testing.T) {
	f, plan := newFailFixture(t)
	_, rf := coverRefundOn(t, f)
	plan.set("Orders.SetStatusCond", errDB)
	if err := f.svc.Refund.Review(f.ctx, rf.RefundNo, "cs1", false, "拒绝"); !errors.Is(err, errDB) {
		t.Fatalf("订单回流失败应透传（64-65）: %v", err)
	}
}

func TestCoverRefundReview_OrderLeftReview(t *testing.T) {
	f, _ := newFailFixture(t)
	o, rf := coverRefundOn(t, f)
	// 并发把订单挪出审核中：拒绝时订单回流失败必须报错，不得静默放行
	if ok, err := f.svc.d.Store.Repos().Orders.SetStatusCond(f.ctx,
		o.OrderNo, types.OrderRefundReview, types.OrderToShip); err != nil || !ok {
		t.Fatalf("置态失败: ok=%v err=%v", ok, err)
	}
	if err := f.svc.Refund.Review(f.ctx, rf.RefundNo, "cs1", false, "拒绝"); !errors.Is(err, types.ErrStatus) {
		t.Fatalf("订单已脱离审核中应报错（67-68）: %v", err)
	}
}

func TestCoverRefundReview_ApproveNotReview(t *testing.T) {
	f, _ := newFailFixture(t)
	_, rf := coverRefundOn(t, f)
	if err := f.svc.Refund.Review(f.ctx, rf.RefundNo, "cs1", false, "拒绝"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Refund.Review(f.ctx, rf.RefundNo, "cs1", true, "同意"); !errors.Is(err, types.ErrStatus) {
		t.Fatalf("非待审核状态批准应报错（77-78）: %v", err)
	}
}

func TestCoverRefundReview_ChannelFailAndMarkFailError(t *testing.T) {
	f, plan := newFailFixture(t)
	_, rf := coverRefundOn(t, f)
	f.ch.failNext = true
	plan.set("Refunds.MarkFailedPending", errDB)
	err := f.svc.Refund.Review(f.ctx, rf.RefundNo, "cs1", true, "同意")
	if err == nil || !strings.Contains(err.Error(), "本地标记失败") {
		t.Fatalf("渠道失败且本地标记失败必须暴露（92-93）: %v", err)
	}
}

func TestCoverRefundRetry_NotRetryable(t *testing.T) {
	f, _ := newFailFixture(t)
	_, rf := coverRefundOn(t, f) // 仍在审核中，不可重试
	if err := f.svc.Refund.Retry(f.ctx, rf.RefundNo); !errors.Is(err, types.ErrStatus) {
		t.Fatalf("审核中不可重试（106-107）: %v", err)
	}
}

// ---------- HandleNotify 退款通知分支 ----------

func TestCoverRefundNotify_SuccessOnReviewRace(t *testing.T) {
	f, _ := newFailFixture(t)
	_, rf := coverRefundOn(t, f) // 仍在审核中：并发成功通知已被处理过
	n := f.signedRefundNotify(rf.RefundNo, "CR-1", rf.Amount, "success")
	result, err := f.svc.Refund.HandleNotify(f.ctx, n)
	if err != nil || result != types.NotifyDuplicate {
		t.Fatalf("审核中收到成功通知应幂等（162-163、192-194）: result=%s err=%v", result, err)
	}
}

func TestCoverRefundNotify_SetStatusError(t *testing.T) {
	f, plan := newFailFixture(t)
	_, rf := coverRefundRefunding(t, f)
	plan.set("Orders.SetStatusCond", errDB)
	n := f.signedRefundNotify(rf.RefundNo, "CR-1", rf.Amount, "success")
	if _, err := f.svc.Refund.HandleNotify(f.ctx, n); !errors.Is(err, errDB) {
		t.Fatalf("订单回流失败应透传（167-168）: %v", err)
	}
}

func TestCoverRefundNotify_OrderLeftReviewRace(t *testing.T) {
	f, _ := newFailFixture(t)
	o, rf := coverRefundRefunding(t, f)
	if ok, err := f.svc.d.Store.Repos().Orders.SetStatusCond(f.ctx,
		o.OrderNo, types.OrderRefundReview, types.OrderToShip); err != nil || !ok {
		t.Fatalf("置态失败: ok=%v err=%v", ok, err)
	}
	n := f.signedRefundNotify(rf.RefundNo, "CR-1", rf.Amount, "success")
	result, err := f.svc.Refund.HandleNotify(f.ctx, n)
	if err != nil || result != types.NotifyDuplicate {
		t.Fatalf("订单已脱离审核中应判竞态幂等（170-171）: result=%s err=%v", result, err)
	}
}

func TestCoverRefundNotify_OrderGetError(t *testing.T) {
	f, plan := newFailFixture(t)
	_, rf := coverRefundRefunding(t, f)
	plan.set("Orders.Get", errDB)
	n := f.signedRefundNotify(rf.RefundNo, "CR-1", rf.Amount, "success")
	if _, err := f.svc.Refund.HandleNotify(f.ctx, n); !errors.Is(err, errDB) {
		t.Fatalf("取单失败应透传（174-175）: %v", err)
	}
}

func TestCoverRefundNotify_AddStockError(t *testing.T) {
	f, plan := newFailFixture(t)
	_, rf := coverRefundRefunding(t, f)
	plan.set("Products.AddStock", errDB)
	n := f.signedRefundNotify(rf.RefundNo, "CR-1", rf.Amount, "success")
	if _, err := f.svc.Refund.HandleNotify(f.ctx, n); !errors.Is(err, errDB) {
		t.Fatalf("回补库存失败应透传（177-178）: %v", err)
	}
}

func TestCoverRefundNotify_ProductGetError(t *testing.T) {
	f, plan := newFailFixture(t)
	_, rf := coverRefundRefunding(t, f)
	plan.set("Products.Get", errDB)
	n := f.signedRefundNotify(rf.RefundNo, "CR-1", rf.Amount, "success")
	if _, err := f.svc.Refund.HandleNotify(f.ctx, n); !errors.Is(err, errDB) {
		t.Fatalf("取商品失败应透传（181-182）: %v", err)
	}
}

// ---------- RefundException 异常转退款分支 ----------

func TestCoverRefundException_ListError(t *testing.T) {
	f, plan := newFailFixture(t)
	plan.set("Exceptions.List", errDB)
	if _, err := f.svc.Refund.RefundException(f.ctx, 1, "cs1"); !errors.Is(err, errDB) {
		t.Fatalf("查异常失败应透传（213-214）: %v", err)
	}
}

func TestCoverRefundException_NotFound(t *testing.T) {
	f, _ := newFailFixture(t)
	if _, err := f.svc.Refund.RefundException(f.ctx, 999, "cs1"); !errors.Is(err, types.ErrNotFound) {
		t.Fatalf("无此异常应报未找到（222-224）: %v", err)
	}
}

func TestCoverRefundException_AttachError(t *testing.T) {
	f, plan := newFailFixture(t)
	id := coverSeedException(t, f, "TL-1")
	plan.set("Exceptions.AttachRefund", errDB)
	if _, err := f.svc.Refund.RefundException(f.ctx, id, "cs1"); !errors.Is(err, errDB) {
		t.Fatalf("挂接退款失败应透传（233-234）: %v", err)
	}
}

func TestCoverRefundException_AlreadyAttached(t *testing.T) {
	f, _ := newFailFixture(t)
	id := coverSeedException(t, f, "TL-2")
	if _, err := f.svc.Refund.RefundException(f.ctx, id, "cs1"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Refund.RefundException(f.ctx, id, "cs1"); !errors.Is(err, types.ErrStatus) {
		t.Fatalf("重复转退款应报状态错误（236-237）: %v", err)
	}
}

func TestCoverRefundException_InsertRefundError(t *testing.T) {
	f, plan := newFailFixture(t)
	id := coverSeedException(t, f, "TL-3")
	plan.set("Refunds.Insert", errDB)
	if _, err := f.svc.Refund.RefundException(f.ctx, id, "cs1"); !errors.Is(err, errDB) {
		t.Fatalf("退款单落库失败应透传（241-242）: %v", err)
	}
}
