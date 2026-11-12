package service

import (
	"context"
	"errors"
	"time"

	"trade/internal/repo"
)

var (
	errRetry      = errors.New("retry")
	errNotPending = errors.New("order not pending")
	errRefundRace = errors.New("refund status race")
)

type Clock func() time.Time

// Timeout 支付超时时长（秒）提供者
type Timeout func() int

// TokenStore 登录令牌存储
type TokenStore interface {
	Set(ctx context.Context, token, value string, ttl time.Duration) error
	Get(ctx context.Context, token string) (string, error)
	Del(ctx context.Context, token string) error
}

// ChannelClient 交易系统调用模拟支付渠道的客户端
type ChannelClient interface {
	RequestRefund(ctx context.Context, refundNo string, amount int64) (string, error)
}

type Deps struct {
	Store   repo.Store
	Clock   Clock
	Timeout Timeout
	Secret  string
	Channel ChannelClient
	Tokens  TokenStore
}

type Services struct {
	d       Deps
	Auth    *AuthService
	Product *ProductService
	Order   *OrderService
	Notify  *NotifyService
	Refund  *RefundService
	Ship    *ShipmentService
	Ops     *OpsService
}

func New(d Deps) *Services {
	order := &OrderService{d: d}
	return &Services{
		d:       d,
		Auth:    &AuthService{d: d},
		Product: &ProductService{d: d},
		Order:   order,
		Notify:  &NotifyService{d: d, order: order},
		Refund:  &RefundService{d: d},
		Ship:    &ShipmentService{d: d},
		Ops:     &OpsService{d: d},
	}
}
