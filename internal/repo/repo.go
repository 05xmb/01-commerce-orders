package repo

import (
	"context"
	"time"

	"trade/internal/types"
)

type ProductRepo interface {
	Insert(ctx context.Context, p types.Product) error
	Get(ctx context.Context, id string) (types.Product, error)
	List(ctx context.Context, onlyOnSale bool) ([]types.Product, error)
	// TryDeduct 条件更新扣减可售：仅当在售且库存足够才成功（防超卖最终防线）
	TryDeduct(ctx context.Context, id string, qty int) (bool, error)
	AddStock(ctx context.Context, id string, delta int) error
	SetPrice(ctx context.Context, id string, price int64) error
	SetOnSale(ctx context.Context, id string, onSale bool) error
}

type StockFlowRepo interface {
	Insert(ctx context.Context, f types.StockFlow) error
	ListByProduct(ctx context.Context, productID string) ([]types.StockFlow, error)
	ListByOrder(ctx context.Context, orderNo string) ([]types.StockFlow, error)
	CountByOrderType(ctx context.Context, orderNo, changeType string) (int, error)
}

type OrderRepo interface {
	Insert(ctx context.Context, o types.Order) error
	Get(ctx context.Context, orderNo string) (types.Order, error)
	ListByBuyer(ctx context.Context, buyerID string) ([]types.Order, error)
	ListByStatus(ctx context.Context, statuses ...string) ([]types.Order, error)
	ListExpiredPending(ctx context.Context, now time.Time) ([]types.Order, error)
	// SetStatusCond 条件流转：仅当当前状态为 from 时流转为 to，返回是否生效
	SetStatusCond(ctx context.Context, orderNo, from, to string) (bool, error)
	// MarkPaid 待付款 → 待发货（条件流转并记录支付时间）
	MarkPaid(ctx context.Context, orderNo string, paidAt time.Time) (bool, error)
	// ClosePending 待付款 → 已关闭（条件流转并记录关闭时间与原因）
	ClosePending(ctx context.Context, orderNo string, closedAt time.Time, reason string) (bool, error)
}

type PaymentRepo interface {
	Insert(ctx context.Context, p types.Payment) error
	GetByTradeNo(ctx context.Context, tradeNo string) (types.Payment, error)
	GetByOrder(ctx context.Context, orderNo string) (types.Payment, error)
	SumAmount(ctx context.Context) (int64, error)
}

type PayNotifyLogRepo interface {
	Insert(ctx context.Context, l types.PayNotifyLog) error
	List(ctx context.Context, limit int) ([]types.PayNotifyLog, error)
	ListByOrder(ctx context.Context, orderNo string) ([]types.PayNotifyLog, error)
}

type RefundRepo interface {
	Insert(ctx context.Context, r types.Refund) error
	Get(ctx context.Context, refundNo string) (types.Refund, error)
	ListByOrder(ctx context.Context, orderNo string) ([]types.Refund, error)
	ListByStatus(ctx context.Context, statuses ...string) ([]types.Refund, error)
	// Approve 审核通过：review → refunding，记录审核客服与备注
	Approve(ctx context.Context, refundNo, csID, note string) (bool, error)
	// Reject 审核拒绝：review → rejected，记录审核客服与备注
	Reject(ctx context.Context, refundNo, csID, note string) (bool, error)
	AttachChannel(ctx context.Context, refundNo, channelRefundNo string) error
	// MarkRefunding 失败待处理 → 退款中（attempt+1）
	MarkRefunding(ctx context.Context, refundNo string) (bool, error)
	// MarkFailedPending 退款中 → 失败待处理
	MarkFailedPending(ctx context.Context, refundNo string) (bool, error)
	// MarkSuccess 退款中/失败待处理 → 已退款（回补/终态动作以"本次生效"为唯一凭证）
	MarkSuccess(ctx context.Context, refundNo string) (bool, error)
	SumSuccess(ctx context.Context) (int64, error)
}

type RefundNotifyLogRepo interface {
	Insert(ctx context.Context, l types.RefundNotifyLog) error
	ListByRefund(ctx context.Context, refundNo string) ([]types.RefundNotifyLog, error)
}

type ShipmentRepo interface {
	Insert(ctx context.Context, s types.Shipment) error
	GetByOrder(ctx context.Context, orderNo string) (types.Shipment, error)
	List(ctx context.Context) ([]types.Shipment, error)
}

type ExceptionRepo interface {
	// Insert 以 trade_no 唯一约束保证迟到通知只入账一次；重复返回 types.ErrDuplicate
	Insert(ctx context.Context, e types.ExceptionPayment) error
	List(ctx context.Context, status string) ([]types.ExceptionPayment, error)
	ListByOrder(ctx context.Context, orderNo string) ([]types.ExceptionPayment, error)
	// AttachRefund 客服核实发起退款：记录处理人、时间与退款单号
	AttachRefund(ctx context.Context, id int64, handler, refundNo string, at time.Time) (bool, error)
	MarkRefundedByRefundNo(ctx context.Context, refundNo string) error
}

type UserRepo interface {
	Insert(ctx context.Context, u types.User) error
	GetByUsername(ctx context.Context, username string) (types.User, error)
	GetByID(ctx context.Context, id string) (types.User, error)
	Count(ctx context.Context) (int, error)
}

// Repos 聚合全部仓储
type Repos struct {
	Products   ProductRepo
	StockFlows StockFlowRepo
	Orders     OrderRepo
	Payments   PaymentRepo
	PayLogs    PayNotifyLogRepo
	Refunds    RefundRepo
	RefundLogs RefundNotifyLogRepo
	Shipments  ShipmentRepo
	Exceptions ExceptionRepo
	Users      UserRepo
}

// Store 仓储工厂与事务边界
type Store interface {
	Repos() Repos
	// WithinTx 在事务中执行 fn，fn 返回错误则回滚
	WithinTx(ctx context.Context, fn func(Repos) error) error
}
