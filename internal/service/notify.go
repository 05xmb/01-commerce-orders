package service

import (
	"context"
	"errors"
	"fmt"

	"trade/internal/pkg/sign"
	"trade/internal/repo"
	"trade/internal/types"
)

type NotifyService struct {
	d     Deps
	order *OrderService
}

// PayNotify 处理支付通知：验签 → 幂等 → 匹配 → 按订单状态分流（R3、R4）
func (s *NotifyService) PayNotify(ctx context.Context, n types.PayNotification) (string, error) {
	raw := fmt.Sprintf(`{"notify_id":%q,"trade_no":%q,"order_no":%q,"amount":%d,"ts":%d}`,
		n.NotifyID, n.TradeNo, n.OrderNo, n.Amount, n.Ts)
	logResult := func(verifyOK bool, result string) {
		_ = s.d.Store.Repos().PayLogs.Insert(ctx, types.PayNotifyLog{
			NotifyID: n.NotifyID, OrderNo: n.OrderNo, TradeNo: n.TradeNo,
			Amount: n.Amount, VerifyOK: verifyOK, Result: result, Raw: raw,
		})
	}
	if !sign.Verify(s.d.Secret, n.Sign, sign.PaySignParts(n.NotifyID, n.TradeNo, n.OrderNo, n.Amount, n.Ts)...) {
		logResult(false, types.NotifyBadSign)
		return types.NotifyBadSign, types.ErrSign
	}
	repos := s.d.Store.Repos()
	// 幂等：同一渠道交易号已记款
	if p, err := repos.Payments.GetByTradeNo(ctx, n.TradeNo); err == nil {
		if p.OrderNo == n.OrderNo && p.Amount == n.Amount {
			logResult(true, types.NotifyDuplicate)
			return types.NotifyDuplicate, nil
		}
		logResult(true, types.NotifyConflict) // 同一交易号归属两个订单
		return types.NotifyConflict, types.ErrConflict
	} else if !errors.Is(err, types.ErrNotFound) {
		return "", err
	}
	o, err := repos.Orders.Get(ctx, n.OrderNo)
	if errors.Is(err, types.ErrNotFound) {
		logResult(true, types.NotifyUnknownOrder)
		return types.NotifyUnknownOrder, nil
	}
	if err != nil {
		return "", err
	}
	if o.Amount != n.Amount {
		logResult(true, types.NotifyAmountMismatch)
		return types.NotifyAmountMismatch, nil
	}
	// 惰性超时：通知到达时订单已过支付时限，先按超时关闭再按迟到支付处理
	if o.Status == types.OrderPending && !s.d.Clock().Before(o.PayDeadline) {
		// 关闭失败必须暴露：不得继续把已过期的订单标记为已支付（审计 L-7）
		if err := s.order.closeOne(ctx, o.OrderNo, types.CloseByTimeout); err != nil {
			return "", err
		}
		if o, err = repos.Orders.Get(ctx, n.OrderNo); err != nil {
			return "", err
		}
	}
	switch o.Status {
	case types.OrderPending:
		err := s.d.Store.WithinTx(ctx, func(r repo.Repos) error {
			ok, err := r.Orders.MarkPaid(ctx, o.OrderNo, s.d.Clock())
			if err != nil {
				return err
			}
			if !ok {
				return errNotPending // 并发关单获胜
			}
			return r.Payments.Insert(ctx, types.Payment{
				OrderNo: o.OrderNo, TradeNo: n.TradeNo, Amount: n.Amount, Status: "success",
			})
		})
		if errors.Is(err, errNotPending) {
			// MarkPaid 失败两种成因（审计 H-1）：并发关单（真迟到）或并发重复通知（钱已记）。
			// 入异常台账前必须复查支付记录，否则幽灵异常会引发"误退款+照发货"。
			// 复查必须重新取仓储读最新已提交状态：函数开头捕获的 repos 可能是旧快照。
			if p, perr := s.d.Store.Repos().Payments.GetByTradeNo(ctx, n.TradeNo); perr == nil {
				if p.OrderNo == n.OrderNo && p.Amount == n.Amount {
					logResult(true, types.NotifyDuplicate)
					return types.NotifyDuplicate, nil
				}
				logResult(true, types.NotifyConflict)
				return types.NotifyConflict, types.ErrConflict
			} else if !errors.Is(perr, types.ErrNotFound) {
				return "", perr
			}
			return s.late(ctx, n, logResult)
		}
		if errors.Is(err, types.ErrDuplicate) {
			logResult(true, types.NotifyDuplicate)
			return types.NotifyDuplicate, nil
		}
		if err != nil {
			return "", err
		}
		logResult(true, types.NotifySuccess)
		return types.NotifySuccess, nil
	case types.OrderClosed:
		return s.late(ctx, n, logResult)
	default: // 已支付/已发货/退款中等状态收到通知
		// 同交易号：幂等重复；不同交易号：订单已收款又来一笔资金，必须入异常台账可查询（审计 M-2，R3）
		if p, perr := repos.Payments.GetByOrder(ctx, o.OrderNo); perr == nil {
			if p.TradeNo == n.TradeNo {
				logResult(true, types.NotifyDuplicate)
				return types.NotifyDuplicate, nil
			}
		} else if !errors.Is(perr, types.ErrNotFound) {
			return "", perr
		}
		return s.late(ctx, n, logResult)
	}
}

// late 迟到支付：入异常台账（trade_no 唯一幂等），不恢复订单、不占用库存（R4）
func (s *NotifyService) late(ctx context.Context, n types.PayNotification, logResult func(bool, string)) (string, error) {
	err := s.d.Store.Repos().Exceptions.Insert(ctx, types.ExceptionPayment{
		OrderNo: n.OrderNo, TradeNo: n.TradeNo, Amount: n.Amount, Status: types.ExceptionPending,
	})
	if errors.Is(err, types.ErrDuplicate) {
		logResult(true, types.NotifyDuplicate)
		return types.NotifyDuplicate, nil
	}
	if err != nil {
		return "", err
	}
	logResult(true, types.NotifyLate)
	return types.NotifyLate, nil
}
