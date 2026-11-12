package service

import (
	"context"
	"errors"
	"fmt"

	"trade/internal/pkg/idgen"
	"trade/internal/pkg/sign"
	"trade/internal/repo"
	"trade/internal/types"
)

type RefundService struct{ d Deps }

// Apply 买家申请整单退款：仅待发货订单；申请期间订单转退款审核中，暂停发货（R5）
func (s *RefundService) Apply(ctx context.Context, orderNo, buyerID, reason string) (types.Refund, error) {
	var rf types.Refund
	err := s.d.Store.WithinTx(ctx, func(r repo.Repos) error {
		o, err := r.Orders.Get(ctx, orderNo)
		if err != nil {
			return err
		}
		if o.BuyerID != buyerID {
			return types.ErrForbidden
		}
		ok, err := r.Orders.SetStatusCond(ctx, orderNo, types.OrderToShip, types.OrderRefundReview)
		if err != nil {
			return err
		}
		if !ok {
			return types.ErrStatus // 并发发货获胜或非待发货
		}
		rf = types.Refund{
			RefundNo: idgen.RefundNo(s.d.Clock()), OrderNo: orderNo, Amount: o.Amount,
			Type: types.RefundTypePresale, Status: types.RefundReview,
			Reason: reason, CreatedAt: s.d.Clock(),
		}
		return r.Refunds.Insert(ctx, rf)
	})
	return rf, err
}

// Review 客服审核：同意→请求渠道退款；拒绝→订单回待发货可继续发货（R5）
func (s *RefundService) Review(ctx context.Context, refundNo, csID string, approve bool, note string) error {
	if note == "" {
		return types.ErrParam
	}
	if !approve {
		return s.d.Store.WithinTx(ctx, func(r repo.Repos) error {
			rf, err := r.Refunds.Get(ctx, refundNo)
			if err != nil {
				return err
			}
			ok, err := r.Refunds.Reject(ctx, refundNo, csID, note)
			if err != nil {
				return err
			}
			if !ok {
				return types.ErrStatus
			}
			ok, err = r.Orders.SetStatusCond(ctx, rf.OrderNo, types.OrderRefundReview, types.OrderToShip)
			if err != nil {
				return err
			}
			if !ok {
				return types.ErrStatus // 订单已脱离审核中（审计 L-5）：回滚，不得静默放行
			}
			return nil
		})
	}
	ok, err := s.d.Store.Repos().Refunds.Approve(ctx, refundNo, csID, note)
	if err != nil {
		return err
	}
	if !ok {
		return types.ErrStatus
	}
	return s.requestChannel(ctx, refundNo)
}

// requestChannel 请求渠道退款；渠道不可用/明确失败时保留待处理记录（R6）
func (s *RefundService) requestChannel(ctx context.Context, refundNo string) error {
	rf, err := s.d.Store.Repos().Refunds.Get(ctx, refundNo)
	if err != nil {
		return err
	}
	channelNo, err := s.d.Channel.RequestRefund(ctx, rf.RefundNo, rf.Amount)
	if err != nil {
		// 渠道失败：本地标记为失败待处理；标记本身失败必须暴露（审计 L-1），不得静默 nil
		if _, merr := s.d.Store.Repos().Refunds.MarkFailedPending(ctx, refundNo); merr != nil {
			return fmt.Errorf("渠道调用失败(%v)且本地标记失败: %w", err, merr)
		}
		return nil
	}
	return s.d.Store.Repos().Refunds.AttachChannel(ctx, refundNo, channelNo)
}

// Retry 客服重试退款：覆盖失败待处理与卡住的退款中（审计 M-1：批准与渠道请求间崩溃的恢复入口）
func (s *RefundService) Retry(ctx context.Context, refundNo string) error {
	ok, err := s.d.Store.Repos().Refunds.MarkRefunding(ctx, refundNo)
	if err != nil {
		return err
	}
	if !ok {
		return types.ErrStatus
	}
	return s.requestChannel(ctx, refundNo)
}

// HandleNotify 处理退款通知：验签、幂等；售前退款回补库存一次，迟到支付退款不回补（R6）
func (s *RefundService) HandleNotify(ctx context.Context, n types.RefundNotification) (string, error) {
	raw := fmt.Sprintf(`{"notify_id":%q,"channel_refund_no":%q,"refund_no":%q,"amount":%d,"result":%q,"ts":%d}`,
		n.NotifyID, n.ChannelRefundNo, n.RefundNo, n.Amount, n.Result, n.Ts)
	logResult := func(verifyOK bool, result string) {
		_ = s.d.Store.Repos().RefundLogs.Insert(ctx, types.RefundNotifyLog{
			NotifyID: n.NotifyID, RefundNo: n.RefundNo, ChannelRefundNo: n.ChannelRefundNo,
			Amount: n.Amount, VerifyOK: verifyOK, Result: result, Raw: raw,
		})
	}
	if !sign.Verify(s.d.Secret, n.Sign,
		sign.RefundSignParts(n.NotifyID, n.ChannelRefundNo, n.RefundNo, n.Amount, n.Result, n.Ts)...) {
		logResult(false, types.NotifyBadSign)
		return types.NotifyBadSign, types.ErrSign
	}
	repos := s.d.Store.Repos()
	rf, err := repos.Refunds.Get(ctx, n.RefundNo)
	if errors.Is(err, types.ErrRefundNotFound) {
		logResult(true, types.NotifyUnknownOrder)
		return types.NotifyUnknownOrder, nil
	}
	if err != nil {
		return "", err
	}
	if rf.Amount != n.Amount {
		logResult(true, types.NotifyAmountMismatch)
		return types.NotifyAmountMismatch, nil
	}
	if rf.Status == types.RefundSuccess {
		logResult(true, types.NotifyDuplicate) // 重复退款通知不重复处理
		return types.NotifyDuplicate, nil
	}
	switch n.Result {
	case "fail":
		ok, err := repos.Refunds.MarkFailedPending(ctx, n.RefundNo)
		if err != nil {
			return "", err
		}
		if !ok {
			logResult(true, types.NotifyDuplicate)
			return types.NotifyDuplicate, nil
		}
		logResult(true, types.NotifyFailed) // 渠道明确失败：保留待处理，客服重试
		return types.NotifyFailed, nil
	case "success":
		err := s.d.Store.WithinTx(ctx, func(r repo.Repos) error {
			ok, err := r.Refunds.MarkSuccess(ctx, n.RefundNo)
			if err != nil {
				return err
			}
			if !ok {
				return errRefundRace // 并发/重复：只认一次成功
			}
			if rf.Type == types.RefundTypePresale {
				ok, err := r.Orders.SetStatusCond(ctx, rf.OrderNo, types.OrderRefundReview, types.OrderRefunded)
				if err != nil {
					return err
				}
				if !ok {
					return errRefundRace // 订单未处于退款审核中（审计 L-5）：整体回滚，不得账实分离
				}
				o, err := r.Orders.Get(ctx, rf.OrderNo)
				if err != nil {
					return err
				}
				if err := r.Products.AddStock(ctx, o.ProductID, o.Qty); err != nil {
					return err
				}
				p, err := r.Products.Get(ctx, o.ProductID)
				if err != nil {
					return err
				}
				return r.StockFlows.Insert(ctx, types.StockFlow{
					ProductID: o.ProductID, OrderNo: o.OrderNo,
					ChangeType: types.FlowRestore, Qty: o.Qty, StockAfter: p.Stock,
				})
			}
			// 迟到支付退款：不回补库存，仅核销异常台账（R4/R6）
			return r.Exceptions.MarkRefundedByRefundNo(ctx, rf.RefundNo)
		})
		if errors.Is(err, errRefundRace) {
			logResult(true, types.NotifyDuplicate)
			return types.NotifyDuplicate, nil
		}
		if err != nil {
			return "", err
		}
		logResult(true, types.NotifySuccess)
		return types.NotifySuccess, nil
	default:
		// 非法结果也必须留痕（审计 L-4），否则渠道重试时无任何可查记录
		logResult(true, "invalid_result")
		return "", types.ErrParam
	}
}

// RefundException 客服核实异常支付（迟到支付）并发起退款（R4）
func (s *RefundService) RefundException(ctx context.Context, exceptionID int64, csID string) (types.Refund, error) {
	var rf types.Refund
	err := s.d.Store.WithinTx(ctx, func(r repo.Repos) error {
		exs, err := r.Exceptions.List(ctx, "")
		if err != nil {
			return err
		}
		var ex *types.ExceptionPayment
		for i := range exs {
			if exs[i].ID == exceptionID {
				ex = &exs[i]
				break
			}
		}
		if ex == nil {
			return types.ErrNotFound
		}
		rf = types.Refund{
			RefundNo: idgen.RefundNo(s.d.Clock()), OrderNo: ex.OrderNo, Amount: ex.Amount,
			Type: types.RefundTypeLate, Status: types.RefundRefunding,
			Reason: "迟到支付退款", CsID: csID, CsNote: "客服核实异常支付发起",
			Attempt: 1, CreatedAt: s.d.Clock(),
		}
		ok, err := r.Exceptions.AttachRefund(ctx, ex.ID, csID, rf.RefundNo, s.d.Clock())
		if err != nil {
			return err
		}
		if !ok {
			return types.ErrStatus
		}
		return r.Refunds.Insert(ctx, rf)
	})
	if err != nil {
		return rf, err
	}
	return rf, s.requestChannel(ctx, rf.RefundNo)
}

func (s *RefundService) ListByStatus(ctx context.Context, statuses ...string) ([]types.Refund, error) {
	return s.d.Store.Repos().Refunds.ListByStatus(ctx, statuses...)
}

func (s *RefundService) ListExceptions(ctx context.Context, status string) ([]types.ExceptionPayment, error) {
	return s.d.Store.Repos().Exceptions.List(ctx, status)
}
