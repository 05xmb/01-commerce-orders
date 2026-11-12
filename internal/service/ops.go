package service

import (
	"context"
	"errors"

	"trade/internal/types"
)

type OpsService struct{ d Deps }

// OrderDetail 运营按订单穿透：收款 / 退款 / 发货 / 异常 / 库存流水 / 通知日志（R8）
type OrderDetail struct {
	Order      types.Order              `json:"order"`
	Payment    *types.Payment           `json:"payment,omitempty"`
	Refunds    []types.Refund           `json:"refunds"`
	Shipment   *types.Shipment          `json:"shipment,omitempty"`
	Exceptions []types.ExceptionPayment `json:"exceptions"`
	StockFlows []types.StockFlow        `json:"stock_flows"`
	NotifyLogs []types.PayNotifyLog     `json:"notify_logs"`
}

func (s *OpsService) OrderDetail(ctx context.Context, orderNo string) (OrderDetail, error) {
	r := s.d.Store.Repos()
	o, err := r.Orders.Get(ctx, orderNo)
	if err != nil {
		return OrderDetail{}, err
	}
	detail := OrderDetail{Order: o}
	if p, err := r.Payments.GetByOrder(ctx, orderNo); err == nil {
		detail.Payment = &p
	} else if !errors.Is(err, types.ErrNotFound) {
		return OrderDetail{}, err
	}
	if detail.Refunds, err = r.Refunds.ListByOrder(ctx, orderNo); err != nil {
		return OrderDetail{}, err
	}
	if sh, err := r.Shipments.GetByOrder(ctx, orderNo); err == nil {
		detail.Shipment = &sh
	} else if !errors.Is(err, types.ErrNotFound) {
		return OrderDetail{}, err
	}
	if detail.Exceptions, err = r.Exceptions.ListByOrder(ctx, orderNo); err != nil {
		return OrderDetail{}, err
	}
	if detail.StockFlows, err = r.StockFlows.ListByOrder(ctx, orderNo); err != nil {
		return OrderDetail{}, err
	}
	if detail.NotifyLogs, err = r.PayLogs.ListByOrder(ctx, orderNo); err != nil {
		return OrderDetail{}, err
	}
	return detail, nil
}

type Summary struct {
	PaidAmount     int64           `json:"paid_amount"`
	RefundedAmount int64           `json:"refunded_amount"`
	Products       []types.Product `json:"products"`
}

// Summary 运营汇总：已收款 / 已退款 / 各商品可售数量（R8）
func (s *OpsService) Summary(ctx context.Context) (Summary, error) {
	r := s.d.Store.Repos()
	paid, err := r.Payments.SumAmount(ctx)
	if err != nil {
		return Summary{}, err
	}
	refunded, err := r.Refunds.SumSuccess(ctx)
	if err != nil {
		return Summary{}, err
	}
	products, err := r.Products.List(ctx, false)
	if err != nil {
		return Summary{}, err
	}
	return Summary{PaidAmount: paid, RefundedAmount: refunded, Products: products}, nil
}

func (s *OpsService) NotifyLogs(ctx context.Context, limit int) ([]types.PayNotifyLog, error) {
	return s.d.Store.Repos().PayLogs.List(ctx, limit)
}

func (s *OpsService) Exceptions(ctx context.Context, status string) ([]types.ExceptionPayment, error) {
	return s.d.Store.Repos().Exceptions.List(ctx, status)
}
