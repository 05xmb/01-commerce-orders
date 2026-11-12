package service

import (
	"context"
	"errors"
	"strings"

	"trade/internal/repo"
	"trade/internal/types"
)

type ShipmentService struct{ d Deps }

// Ship 仓库发货：仅待发货订单，每单一次；退款审核中条件流转失败即被拦（R5、R7）
func (s *ShipmentService) Ship(ctx context.Context, orderNo, operator, carrier, tracking string) error {
	if strings.TrimSpace(carrier) == "" || strings.TrimSpace(tracking) == "" {
		return types.ErrParam
	}
	return s.d.Store.WithinTx(ctx, func(r repo.Repos) error {
		ok, err := r.Orders.SetStatusCond(ctx, orderNo, types.OrderToShip, types.OrderShipped)
		if err != nil {
			return err
		}
		if !ok {
			return types.ErrStatus // 非待发货（含退款审核中）：明确拒绝
		}
		err = r.Shipments.Insert(ctx, types.Shipment{
			OrderNo: orderNo, Carrier: carrier, TrackingNo: tracking, Operator: operator,
		})
		if errors.Is(err, types.ErrDuplicate) {
			return types.ErrStatus // 每单至多一条发货记录
		}
		return err
	})
}

func (s *ShipmentService) GetByOrder(ctx context.Context, orderNo string) (types.Shipment, error) {
	return s.d.Store.Repos().Shipments.GetByOrder(ctx, orderNo)
}
