package service

import (
	"context"
	"errors"
	"time"

	"trade/internal/pkg/idgen"
	"trade/internal/repo"
	"trade/internal/types"
)

type OrderService struct{ d Deps }

// Create 下单：条件更新扣库存 + 事务内创建订单与占用流水（防超卖，R1）
func (s *OrderService) Create(ctx context.Context, buyerID, productID string, qty int) (types.Order, error) {
	if qty < 1 || qty > 2 {
		return types.Order{}, types.ErrInvalidQty
	}
	var created types.Order
	for attempt := 0; attempt < 3; attempt++ {
		err := s.d.Store.WithinTx(ctx, func(r repo.Repos) error {
			ok, err := r.Products.TryDeduct(ctx, productID, qty)
			if err != nil {
				return err
			}
			if !ok {
				return types.ErrOutOfStock
			}
			p, err := r.Products.Get(ctx, productID)
			if err != nil {
				return err
			}
			now := s.d.Clock()
			created = types.Order{
				OrderNo:     idgen.OrderNo(now),
				BuyerID:     buyerID,
				ProductID:   p.ID,
				ProductName: p.Name,
				Price:       p.Price,
				Qty:         qty,
				Amount:      p.Price * int64(qty),
				Status:      types.OrderPending,
				CreatedAt:   now,
				PayDeadline: now.Add(time.Duration(s.d.Timeout()) * time.Second),
			}
			if err := r.Orders.Insert(ctx, created); err != nil {
				if errors.Is(err, types.ErrDuplicate) {
					return errRetry // 订单号冲突重试
				}
				return err
			}
			return r.StockFlows.Insert(ctx, types.StockFlow{
				ProductID: p.ID, OrderNo: created.OrderNo,
				ChangeType: types.FlowOccupy, Qty: -qty, StockAfter: p.Stock,
			})
		})
		if errors.Is(err, errRetry) {
			continue
		}
		return created, err
	}
	return types.Order{}, types.ErrStatus
}

// closeOne 关闭待付款订单并释放占用（幂等：重复关闭不多释放，R2）
func (s *OrderService) closeOne(ctx context.Context, orderNo, reason string) error {
	return s.d.Store.WithinTx(ctx, func(r repo.Repos) error {
		ok, err := r.Orders.ClosePending(ctx, orderNo, s.d.Clock(), reason)
		if err != nil {
			return err
		}
		if !ok {
			return nil // 已关闭或状态已变化：幂等返回
		}
		o, err := r.Orders.Get(ctx, orderNo)
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
			ProductID: o.ProductID, OrderNo: orderNo,
			ChangeType: types.FlowRelease, Qty: o.Qty, StockAfter: p.Stock,
		})
	})
}

// Cancel 买家主动取消（仅待付款；已付款订单须走退款流程，A2）
func (s *OrderService) Cancel(ctx context.Context, orderNo, buyerID string) error {
	o, err := s.d.Store.Repos().Orders.Get(ctx, orderNo)
	if err != nil {
		return err
	}
	if o.BuyerID != buyerID {
		return types.ErrForbidden
	}
	if o.Status != types.OrderPending {
		return types.ErrStatus
	}
	return s.closeOne(ctx, orderNo, types.CloseByCancel)
}

// CloseExpired 超时关单（定时扫描入口，服务端时间判定，A5）
func (s *OrderService) CloseExpired(ctx context.Context) (int, error) {
	expired, err := s.d.Store.Repos().Orders.ListExpiredPending(ctx, s.d.Clock())
	if err != nil {
		return 0, err
	}
	n := 0
	for _, o := range expired {
		if err := s.closeOne(ctx, o.OrderNo, types.CloseByTimeout); err == nil {
			n++
		}
	}
	return n, nil
}

// closeIfExpired 查询时惰性关单（A5 双保险）
func (s *OrderService) closeIfExpired(ctx context.Context, o types.Order) types.Order {
	if o.Status == types.OrderPending && !s.d.Clock().Before(o.PayDeadline) {
		_ = s.closeOne(ctx, o.OrderNo, types.CloseByTimeout)
		if fresh, err := s.d.Store.Repos().Orders.Get(ctx, o.OrderNo); err == nil {
			return fresh
		}
	}
	return o
}

// OrderView 订单详情视图：订单本体 + 发货信息（买家查看物流，R7）
type OrderView struct {
	types.Order
	Shipment *types.Shipment `json:"shipment,omitempty"`
}

func (s *OrderService) Get(ctx context.Context, orderNo, userID, role string) (OrderView, error) {
	o, err := s.d.Store.Repos().Orders.Get(ctx, orderNo)
	if err != nil {
		return OrderView{}, err
	}
	o = s.closeIfExpired(ctx, o)
	if role == types.RoleBuyer && o.BuyerID != userID {
		return OrderView{}, types.ErrForbidden
	}
	v := OrderView{Order: o}
	if o.Status == types.OrderShipped {
		if sh, err := s.d.Store.Repos().Shipments.GetByOrder(ctx, o.OrderNo); err == nil {
			v.Shipment = &sh
		} else if !errors.Is(err, types.ErrNotFound) {
			return OrderView{}, err
		}
	}
	return v, nil
}

func (s *OrderService) ListMine(ctx context.Context, buyerID string) ([]types.Order, error) {
	if _, err := s.CloseExpired(ctx); err != nil {
		return nil, err
	}
	return s.d.Store.Repos().Orders.ListByBuyer(ctx, buyerID)
}

// ListForWarehouse 仓库工作台：待发货 / 退款审核中 / 已发货
func (s *OrderService) ListForWarehouse(ctx context.Context) ([]types.Order, error) {
	return s.d.Store.Repos().Orders.ListByStatus(ctx,
		types.OrderToShip, types.OrderRefundReview, types.OrderShipped)
}
