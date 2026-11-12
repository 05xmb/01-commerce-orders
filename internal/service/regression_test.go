package service

import (
	"errors"
	"sync"
	"testing"

	"trade/internal/types"
)

// 审计 H-1 回归：并发重复支付通知不得制造幽灵异常台账
// 触发链：两条同 trade_no 通知并发 → 都过 GetByTradeNo 预检 → 一个 MarkPaid 成功，
// 另一个 errNotPending → 必须复查支付记录并返回 duplicate，而不是入异常台账（否则误退款+照发货）
func TestPayNotify_ConcurrentDuplicate_NoGhostException(t *testing.T) {
	f := newFixture(t)
	o, err := f.svc.Order.Create(f.ctx, "u1", "P1", 1)
	if err != nil {
		t.Fatal(err)
	}
	n := f.signedPayNotify("T-race", o.OrderNo, o.Amount)

	const N = 16
	var wg sync.WaitGroup
	results := make([]string, N)
	errs := make([]error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = f.svc.Notify.PayNotify(f.ctx, n)
		}(i)
	}
	wg.Wait()

	// 统计：恰好 1 次 success，其余 duplicate；不得出现 late（幽灵异常）
	success, dup, late := 0, 0, 0
	for i := 0; i < N; i++ {
		if errs[i] != nil {
			t.Fatalf("通知处理不应报错: %v", errs[i])
		}
		switch results[i] {
		case types.NotifySuccess:
			success++
		case types.NotifyDuplicate:
			dup++
		case types.NotifyLate:
			late++
		}
	}
	if success != 1 {
		t.Fatalf("应恰好 1 次记款成功，实际 %d", success)
	}
	if late != 0 {
		t.Fatalf("并发重复通知产生了 %d 条幽灵异常台账（H-1 缺陷）", late)
	}
	// 支付记录恰好 1 条；订单正常待发货；无异常台账
	if got := f.order(t, o.OrderNo); got.Status != types.OrderToShip {
		t.Fatalf("订单应为待发货，实际 %s", got.Status)
	}
	if f.order(t, o.OrderNo).PaidAt == nil {
		t.Fatal("应有支付时间")
	}
	exs, _ := f.svc.d.Store.Repos().Exceptions.List(f.ctx, "")
	if len(exs) != 0 {
		t.Fatalf("不应产生异常台账: %+v", exs)
	}
}

// 审计 M-1 回归：退款在 refunding 状态卡住（批准与渠道请求间崩溃）时可由客服 Retry 恢复
func TestRefund_RetryFromStuckRefunding(t *testing.T) {
	f := newFixture(t)
	o := f.createAndPay(t, "u1", "P1", 1)
	rf, err := f.svc.Refund.Apply(f.ctx, o.OrderNo, "u1", "不想要了")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Refund.Review(f.ctx, rf.RefundNo, "cs1", true, "同意"); err != nil {
		t.Fatal(err)
	}
	// 此刻状态 refunding（渠道请求已发出）。模拟"渠道请求其实未送达/卡住"：直接再 Retry
	if err := f.svc.Refund.Retry(f.ctx, rf.RefundNo); err != nil {
		t.Fatalf("卡住的 refunding 应可重试恢复（M-1），实际 %v", err)
	}
	got, _ := f.svc.d.Store.Repos().Refunds.Get(f.ctx, rf.RefundNo)
	if got.Status != types.RefundRefunding {
		t.Fatalf("重试后应为退款中，实际 %s", got.Status)
	}
	if got.Attempt < 2 {
		t.Fatalf("重试应递增尝试次数，实际 %d", got.Attempt)
	}
}

// 审计 L-8 回归：已支付订单（并发支付获胜）买家取消应如实报错，不得误导"已取消"
func TestCancel_AfterPaid_ReturnsConflict(t *testing.T) {
	f := newFixture(t)
	o := f.createAndPay(t, "u1", "P1", 1)
	err := f.svc.Order.Cancel(f.ctx, o.OrderNo, "u1")
	if !errors.Is(err, types.ErrStatus) {
		t.Fatalf("已支付订单取消应返回状态冲突，实际 %v", err)
	}
	// 库存未被误释放
	if got := f.flowCount(t, o.OrderNo, types.FlowRelease); got != 0 {
		t.Fatal("已支付订单取消不得释放库存")
	}
	if got := f.order(t, o.OrderNo); got.Status != types.OrderToShip {
		t.Fatalf("订单应保持待发货，实际 %s", got.Status)
	}
}

// 审计 L-3 回归：买家可在订单详情中看到退款进度与审核信息
func TestOrderGet_IncludesRefunds(t *testing.T) {
	f := newFixture(t)
	o := f.createAndPay(t, "u1", "P1", 1)
	rf, err := f.svc.Refund.Apply(f.ctx, o.OrderNo, "u1", "买错了")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Refund.Review(f.ctx, rf.RefundNo, "cs1", false, "不符合退款条件"); err != nil {
		t.Fatal(err)
	}
	// 买家查询本人订单，应看到退款记录与拒绝原因、审核客服
	v, err := f.svc.Order.Get(f.ctx, o.OrderNo, "u1", types.RoleBuyer)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Refunds) != 1 {
		t.Fatalf("订单详情应含退款记录，实际 %d 条", len(v.Refunds))
	}
	r := v.Refunds[0]
	if r.Status != types.RefundRejected || r.CsNote != "不符合退款条件" || r.CsID != "cs1" {
		t.Fatalf("退款详情缺少拒绝原因或审核客服: %+v", r)
	}
	// 拒绝后订单回待发货
	if v.Status != types.OrderToShip {
		t.Fatalf("拒绝后订单应回待发货，实际 %s", v.Status)
	}
}

// 审计 L-2 回归：退款批准也记录审核客服
func TestRefund_ReviewRecordsCS(t *testing.T) {
	f := newFixture(t)
	o := f.createAndPay(t, "u1", "P1", 1)
	rf, err := f.svc.Refund.Apply(f.ctx, o.OrderNo, "u1", "不想要了")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Refund.Review(f.ctx, rf.RefundNo, "cs-zhang", true, "同意"); err != nil {
		t.Fatal(err)
	}
	got, _ := f.svc.d.Store.Repos().Refunds.Get(f.ctx, rf.RefundNo)
	if got.CsID != "cs-zhang" {
		t.Fatalf("批准应记录审核客服，实际 %q", got.CsID)
	}
}

// 审计 L-4 回归：非法退款结果必须留日志
func TestRefundNotify_InvalidResult_Logged(t *testing.T) {
	f := newFixture(t)
	o := f.createAndPay(t, "u1", "P1", 1)
	rf, err := f.svc.Refund.Apply(f.ctx, o.OrderNo, "u1", "不想要了")
	if err != nil {
		t.Fatal(err)
	}
	n := f.signedRefundNotify(rf.RefundNo, "CR1", rf.Amount, "weird")
	if _, err := f.svc.Refund.HandleNotify(f.ctx, n); !errors.Is(err, types.ErrParam) {
		t.Fatalf("非法结果应拒绝，实际 %v", err)
	}
	logs, _ := f.svc.d.Store.Repos().RefundLogs.ListByRefund(f.ctx, rf.RefundNo)
	found := false
	for _, l := range logs {
		if l.Result == "invalid_result" {
			found = true
		}
	}
	if !found {
		t.Fatal("非法结果未留日志（L-4 缺陷）")
	}
}
