package service

import (
	"context"

	"trade/internal/types"
)

type ProductService struct{ d Deps }

// List onlyOnSale=true 时仅返回在售商品（买家视图）
func (s *ProductService) List(ctx context.Context, onlyOnSale bool) ([]types.Product, error) {
	return s.d.Store.Repos().Products.List(ctx, onlyOnSale)
}

// SetPrice 改价：仅影响新订单，历史订单按快照结算（R1）
func (s *ProductService) SetPrice(ctx context.Context, id string, price int64) error {
	if price <= 0 {
		return types.ErrParam
	}
	return s.d.Store.Repos().Products.SetPrice(ctx, id, price)
}

func (s *ProductService) SetOnSale(ctx context.Context, id string, onSale bool) error {
	return s.d.Store.Repos().Products.SetOnSale(ctx, id, onSale)
}

func (s *ProductService) Flows(ctx context.Context, productID string) ([]types.StockFlow, error) {
	return s.d.Store.Repos().StockFlows.ListByProduct(ctx, productID)
}
