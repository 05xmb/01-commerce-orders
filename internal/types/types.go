package types

import (
	"errors"
	"time"
)

// 订单状态
const (
	OrderPending      = "pending"       // 待付款
	OrderToShip       = "to_ship"       // 待发货
	OrderRefundReview = "refund_review" // 退款审核中
	OrderShipped      = "shipped"       // 已发货（终态）
	OrderRefunded     = "refunded"      // 已退款（终态）
	OrderClosed       = "closed"        // 已关闭（终态）
)

// 关闭原因
const (
	CloseByCancel  = "cancel"
	CloseByTimeout = "timeout"
)

// 退款状态
const (
	RefundReview        = "review"
	RefundRejected      = "rejected"
	RefundRefunding     = "refunding"
	RefundSuccess       = "success"
	RefundFailedPending = "failed_pending"
)

// 退款类型
const (
	RefundTypePresale = "presale" // 售前退款（回补库存）
	RefundTypeLate    = "late"    // 迟到支付退款（不回补库存）
)

// 库存流水类型
const (
	FlowOccupy  = "occupy"  // 下单占用
	FlowRelease = "release" // 关闭释放
	FlowRestore = "restore" // 退款回补
)

// 异常支付状态
const (
	ExceptionPending   = "pending"   // 待客服核实处理
	ExceptionRefunding = "refunding" // 已发起退款，待渠道确认（防重复发起）
	ExceptionRefunded  = "refunded"  // 退款完成，台账核销
)

// 通知处理结果
const (
	NotifySuccess        = "success"
	NotifyDuplicate      = "duplicate"
	NotifyAmountMismatch = "amount_mismatch"
	NotifyUnknownOrder   = "unknown_order"
	NotifyBadSign        = "bad_sign"
	NotifyLate           = "late"
	NotifyConflict       = "conflict"
	NotifyFailed         = "failed"
)

// 角色
const (
	RoleBuyer = "buyer"
	RoleOp    = "op"
	RoleWH    = "wh"
	RoleCS    = "cs"
)

var (
	ErrNotFound       = errors.New("记录不存在")
	ErrOutOfStock     = errors.New("库存不足或商品不可购买")
	ErrInvalidQty     = errors.New("每单数量限 1~2 件")
	ErrStatus         = errors.New("当前状态不允许该操作")
	ErrForbidden      = errors.New("无权操作该数据")
	ErrSign           = errors.New("验签失败")
	ErrDuplicate      = errors.New("重复请求")
	ErrConflict       = errors.New("数据冲突")
	ErrChannel        = errors.New("渠道调用失败")
	ErrParam          = errors.New("参数错误")
	ErrUnauthorized   = errors.New("未登录或登录已过期")
	ErrRefundNotFound = errors.New("退款单不存在")
)

type Product struct {
	ID     string `db:"id" json:"id"`
	Name   string `db:"name" json:"name"`
	Price  int64  `db:"price" json:"price"`
	Stock  int    `db:"stock" json:"stock"`
	OnSale bool   `db:"on_sale" json:"on_sale"`
}

type StockFlow struct {
	ID         int64     `db:"id" json:"id"`
	ProductID  string    `db:"product_id" json:"product_id"`
	OrderNo    string    `db:"order_no" json:"order_no"`
	ChangeType string    `db:"change_type" json:"change_type"`
	Qty        int       `db:"qty" json:"qty"`
	StockAfter int       `db:"stock_after" json:"stock_after"`
	CreatedAt  time.Time `db:"created_at" json:"created_at"`
}

type Order struct {
	OrderNo     string     `db:"order_no" json:"order_no"`
	BuyerID     string     `db:"buyer_id" json:"buyer_id"`
	ProductID   string     `db:"product_id" json:"product_id"`
	ProductName string     `db:"product_name" json:"product_name"`
	Price       int64      `db:"price" json:"price"`
	Qty         int        `db:"qty" json:"qty"`
	Amount      int64      `db:"amount" json:"amount"`
	Status      string     `db:"status" json:"status"`
	CreatedAt   time.Time  `db:"created_at" json:"created_at"`
	PayDeadline time.Time  `db:"pay_deadline" json:"pay_deadline"`
	PaidAt      *time.Time `db:"paid_at" json:"paid_at,omitempty"`
	ClosedAt    *time.Time `db:"closed_at" json:"closed_at,omitempty"`
	CloseReason *string    `db:"close_reason" json:"close_reason,omitempty"`
}

type Payment struct {
	ID        int64     `db:"id" json:"id"`
	OrderNo   string    `db:"order_no" json:"order_no"`
	TradeNo   string    `db:"trade_no" json:"trade_no"`
	Amount    int64     `db:"amount" json:"amount"`
	Status    string    `db:"status" json:"status"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

type PayNotifyLog struct {
	ID        int64     `db:"id" json:"id"`
	NotifyID  string    `db:"notify_id" json:"notify_id"`
	OrderNo   string    `db:"order_no" json:"order_no"`
	TradeNo   string    `db:"trade_no" json:"trade_no"`
	Amount    int64     `db:"amount" json:"amount"`
	VerifyOK  bool      `db:"verify_ok" json:"verify_ok"`
	Result    string    `db:"result" json:"result"`
	Raw       string    `db:"raw" json:"raw"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

type Refund struct {
	RefundNo        string    `db:"refund_no" json:"refund_no"`
	OrderNo         string    `db:"order_no" json:"order_no"`
	Amount          int64     `db:"amount" json:"amount"`
	Type            string    `db:"type" json:"type"`
	Status          string    `db:"status" json:"status"`
	Reason          string    `db:"reason" json:"reason"`
	CsID            string    `db:"cs_id" json:"cs_id"` // 审核/发起操作的客服（审计追溯，R8）
	CsNote          string    `db:"cs_note" json:"cs_note"`
	ChannelRefundNo string    `db:"channel_refund_no" json:"channel_refund_no"`
	Attempt         int       `db:"attempt" json:"attempt"`
	CreatedAt       time.Time `db:"created_at" json:"created_at"`
}

type RefundNotifyLog struct {
	ID              int64     `db:"id" json:"id"`
	NotifyID        string    `db:"notify_id" json:"notify_id"`
	RefundNo        string    `db:"refund_no" json:"refund_no"`
	ChannelRefundNo string    `db:"channel_refund_no" json:"channel_refund_no"`
	Amount          int64     `db:"amount" json:"amount"`
	VerifyOK        bool      `db:"verify_ok" json:"verify_ok"`
	Result          string    `db:"result" json:"result"`
	Raw             string    `db:"raw" json:"raw"`
	CreatedAt       time.Time `db:"created_at" json:"created_at"`
}

type Shipment struct {
	ID         int64     `db:"id" json:"id"`
	OrderNo    string    `db:"order_no" json:"order_no"`
	Carrier    string    `db:"carrier" json:"carrier"`
	TrackingNo string    `db:"tracking_no" json:"tracking_no"`
	Operator   string    `db:"operator" json:"operator"`
	CreatedAt  time.Time `db:"created_at" json:"created_at"`
}

type ExceptionPayment struct {
	ID        int64      `db:"id" json:"id"`
	OrderNo   string     `db:"order_no" json:"order_no"`
	TradeNo   string     `db:"trade_no" json:"trade_no"`
	Amount    int64      `db:"amount" json:"amount"`
	Status    string     `db:"status" json:"status"`
	Handler   *string    `db:"handler" json:"handler,omitempty"`
	HandledAt *time.Time `db:"handled_at" json:"handled_at,omitempty"`
	RefundNo  *string    `db:"refund_no" json:"refund_no,omitempty"`
	CreatedAt time.Time  `db:"created_at" json:"created_at"`
}

type User struct {
	ID           string `db:"id" json:"id"`
	Username     string `db:"username" json:"username"`
	PasswordHash string `db:"password_hash" json:"-"`
	Name         string `db:"name" json:"name"`
	Role         string `db:"role" json:"role"`
}

// 支付通知（渠道 → 交易系统）
type PayNotification struct {
	NotifyID string `json:"notify_id"`
	TradeNo  string `json:"trade_no"`
	OrderNo  string `json:"order_no"`
	Amount   int64  `json:"amount"`
	Ts       int64  `json:"ts"`
	Sign     string `json:"sign"`
}

// 退款通知（渠道 → 交易系统）
type RefundNotification struct {
	NotifyID        string `json:"notify_id"`
	ChannelRefundNo string `json:"channel_refund_no"`
	RefundNo        string `json:"refund_no"`
	Amount          int64  `json:"amount"`
	Result          string `json:"result"` // success / fail
	Ts              int64  `json:"ts"`
	Sign            string `json:"sign"`
}
