package service

import (
	"errors"
	"testing"

	"trade/internal/types"
)

// Ops.NotifyLogs：通知日志查询
func TestOps_NotifyLogs(t *testing.T) {
	f := newFixture(t)
	o := f.createAndPay(t, "b1", "P1", 1)
	logs, err := f.svc.Ops.NotifyLogs(f.ctx, 10)
	if err != nil || len(logs) == 0 {
		t.Fatalf("应返回通知日志，err=%v", err)
	}
	if logs[0].OrderNo != o.OrderNo {
		t.Fatal("日志应关联订单")
	}
}

// Product.Flows：库存流水查询
func TestProduct_Flows(t *testing.T) {
	f := newFixture(t)
	o := f.createAndPay(t, "b1", "P1", 1)
	flows, err := f.svc.Product.Flows(f.ctx, "P1")
	if err != nil || len(flows) == 0 {
		t.Fatalf("应返回占用流水，err=%v", err)
	}
	if flows[0].OrderNo != o.OrderNo {
		t.Fatal("流水应关联订单")
	}
}

// Refund.ListByStatus / ListExceptions：退款与异常列表查询
func TestRefund_Lists(t *testing.T) {
	f := newFixture(t)
	o := f.createAndPay(t, "b1", "P1", 1)
	rf, err := f.svc.Refund.Apply(f.ctx, o.OrderNo, "b1", "不想要了")
	if err != nil {
		t.Fatal(err)
	}
	list, err := f.svc.Refund.ListByStatus(f.ctx, types.RefundReview)
	if err != nil || len(list) != 1 || list[0].RefundNo != rf.RefundNo {
		t.Fatalf("退款列表错误：len=%d err=%v", len(list), err)
	}
	exs, err := f.svc.Refund.ListExceptions(f.ctx, types.ExceptionPending)
	if err != nil {
		t.Fatal(err)
	}
	if len(exs) != 0 {
		t.Fatal("不应有异常记录")
	}
}

// HandleNotify 剩余分支：未知退款单、金额不符、非法结果、重复标记竞争
func TestRefundNotify_EdgeBranches(t *testing.T) {
	dbErr := errors.New("db down")

	// 未知退款单 → NotifyUnknownOrder
	t.Run("UnknownRefund", func(t *testing.T) {
		f := newFixture(t)
		n := f.signedRefundNotify("R-none", "CR1", 100, "success")
		result, err := f.svc.Refund.HandleNotify(f.ctx, n)
		if err != nil || result != types.NotifyUnknownOrder {
			t.Fatalf("未知退款单应返回 unknown，result=%s err=%v", result, err)
		}
	})

	// 金额不符 → NotifyAmountMismatch
	t.Run("AmountMismatch", func(t *testing.T) {
		f := newFixture(t)
		o := f.createAndPay(t, "b1", "P1", 1)
		rf, _ := f.svc.Refund.Apply(f.ctx, o.OrderNo, "b1", "不想要了")
		n := f.signedRefundNotify(rf.RefundNo, "CR1", rf.Amount+1, "success")
		result, err := f.svc.Refund.HandleNotify(f.ctx, n)
		if err != nil || result != types.NotifyAmountMismatch {
			t.Fatalf("金额不符应返回 mismatch，result=%s err=%v", result, err)
		}
	})

	// 非法结果 → ErrParam
	t.Run("BadResult", func(t *testing.T) {
		f := newFixture(t)
		o := f.createAndPay(t, "b1", "P1", 1)
		rf, _ := f.svc.Refund.Apply(f.ctx, o.OrderNo, "b1", "不想要了")
		n := f.signedRefundNotify(rf.RefundNo, "CR1", rf.Amount, "unknown")
		if _, err := f.svc.Refund.HandleNotify(f.ctx, n); !errors.Is(err, types.ErrParam) {
			t.Fatalf("非法结果应拒绝，实际 %v", err)
		}
	})

	// 失败通知竞争：MarkFailedPending 返回 false → NotifyDuplicate
	t.Run("FailRace", func(t *testing.T) {
		f := newFixture(t)
		o := f.createAndPay(t, "b1", "P1", 1)
		rf, _ := f.svc.Refund.Apply(f.ctx, o.OrderNo, "b1", "不想要了")
		if err := f.svc.Refund.Review(f.ctx, rf.RefundNo, "cs1", true, "同意"); err != nil {
			t.Fatal(err)
		}
		n := f.signedRefundNotify(rf.RefundNo, "CR1", rf.Amount, "fail")
		if result, err := f.svc.Refund.HandleNotify(f.ctx, n); err != nil || result != types.NotifyFailed {
			t.Fatalf("首次失败通知应 accepted，result=%s err=%v", result, err)
		}
		// 第二次 fail：状态已为失败待处理，MarkFailedPending 竞争返回 false
		result, err := f.svc.Refund.HandleNotify(f.ctx, n)
		if err != nil || result != types.NotifyDuplicate {
			t.Fatalf("重复失败通知应幂等，result=%s err=%v", result, err)
		}
	})

	// Refunds.Get 数据库错误
	t.Run("DBError", func(t *testing.T) {
		f, plan := newFailFixture(t)
		plan.set("Refunds.Get", dbErr)
		n := f.signedRefundNotify("R-x", "CR1", 100, "success")
		if _, err := f.svc.Refund.HandleNotify(f.ctx, n); !errors.Is(err, dbErr) {
			t.Fatalf("查询失败应传递错误，实际 %v", err)
		}
	})
}

// OrderDetail 各子查询错误分支
func TestOps_OrderDetail_DBErrors(t *testing.T) {
	dbErr := errors.New("db down")
	cases := []struct {
		name string
		key  string
	}{
		{"Payment", "Payments.GetByOrder"},
		{"Refunds", "Refunds.ListByOrder"},
		{"Shipment", "Shipments.GetByOrder"},
		{"Exceptions", "Exceptions.ListByOrder"},
		{"StockFlows", "StockFlows.ListByOrder"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, plan := newFailFixture(t)
			o := f.createAndPay(t, "b1", "P1", 1)
			plan.set(c.key, dbErr)
			if _, err := f.svc.Ops.OrderDetail(f.ctx, o.OrderNo); !errors.Is(err, dbErr) {
				t.Fatalf("%s 失败应传递错误，实际 %v", c.key, err)
			}
		})
	}
}

// Summary 剩余错误分支
func TestOps_Summary_DBErrors(t *testing.T) {
	dbErr := errors.New("db down")
	t.Run("RefundsSum", func(t *testing.T) {
		f, plan := newFailFixture(t)
		plan.set("Refunds.SumSuccess", dbErr)
		if _, err := f.svc.Ops.Summary(f.ctx); !errors.Is(err, dbErr) {
			t.Fatalf("退款汇总失败应传递错误，实际 %v", err)
		}
	})
	t.Run("Products", func(t *testing.T) {
		f, plan := newFailFixture(t)
		plan.set("Products.List", dbErr)
		if _, err := f.svc.Ops.Summary(f.ctx); !errors.Is(err, dbErr) {
			t.Fatalf("商品列表失败应传递错误，实际 %v", err)
		}
	})
}

// PayNotify 惰性超时后重查订单失败
func TestNotify_LazyClose_RegetError(t *testing.T) {
	dbErr := errors.New("db down")
	f, plan := newFailFixture(t)
	o, err := f.svc.Order.Create(f.ctx, "b1", "P1", 1)
	if err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(31e9) // 过支付时限
	n := f.signedPayNotify("T-lazy", o.OrderNo, o.Amount)
	// Get 调用序列：初次查询(过) → closeOne 内部(过) → 惰性关闭后重查(注入失败)
	plan.set("Orders.Get", nil, nil, dbErr)
	if _, err := f.svc.Notify.PayNotify(f.ctx, n); !errors.Is(err, dbErr) {
		t.Fatalf("重查失败应传递错误，实际 %v", err)
	}
}

// 迟到支付入账失败
func TestNotify_LateInsertError(t *testing.T) {
	f, plan := newFailFixture(t)
	o, _ := f.svc.Order.Create(f.ctx, "b1", "P1", 1)
	if err := f.svc.Order.Cancel(f.ctx, o.OrderNo, "b1"); err != nil {
		t.Fatal(err)
	}
	plan.set("Exceptions.Insert", errors.New("db down"))
	n := f.signedPayNotify("T-late2", o.OrderNo, o.Amount)
	if _, err := f.svc.Notify.PayNotify(f.ctx, n); err == nil {
		t.Fatal("异常入账失败应返回错误")
	}
}

// Verify 非法 token 格式
func TestAuth_VerifyBadFormat(t *testing.T) {
	f := newFixture(t)
	tok := "badfmt-token"
	if err := f.svc.d.Tokens.Set(f.ctx, tok, "no-pipe-value", 3600e9); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.svc.Auth.Verify(f.ctx, tok); !errors.Is(err, types.ErrUnauthorized) {
		t.Fatal("非法格式 token 应拒绝")
	}
}
