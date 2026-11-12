package fake

import (
	"context"
	"time"

	"trade/internal/repo"
	"trade/internal/types"
)

// shared* 包装：把对活状态的每次仓储访问都置于 Store.mu 之内。
// 普通访问与 WithinTx（持锁贯穿 clone/fn/commit）完全串行，消除并发竞态；
// 事务内 fn 拿到的仍是私有快照上的裸仓储（reposWith），无锁且互不影响。
// 每次调用都在锁内重新解析当前活状态，长时间持有的 Repos 句柄不会读到过期快照。
// 修复前 -race 实测竞态：fake.go:78 提交换指针 ↔ fake.go:69/32 Repos() 直读（regression_test.go 并发回归触发）。

func sharedReposWith(s *Store) repo.Repos {
	return repo.Repos{
		Products:   sharedProducts{s},
		StockFlows: sharedStockFlows{s},
		Orders:     sharedOrders{s},
		Payments:   sharedPayments{s},
		PayLogs:    sharedPayLogs{s},
		Refunds:    sharedRefunds{s},
		RefundLogs: sharedRefundLogs{s},
		Shipments:  sharedShipments{s},
		Exceptions: sharedExceptions{s},
		Users:      sharedUsers{s},
	}
}

var (
	_ repo.ProductRepo         = sharedProducts{}
	_ repo.StockFlowRepo       = sharedStockFlows{}
	_ repo.OrderRepo           = sharedOrders{}
	_ repo.PaymentRepo         = sharedPayments{}
	_ repo.PayNotifyLogRepo    = sharedPayLogs{}
	_ repo.RefundRepo          = sharedRefunds{}
	_ repo.RefundNotifyLogRepo = sharedRefundLogs{}
	_ repo.ShipmentRepo        = sharedShipments{}
	_ repo.ExceptionRepo       = sharedExceptions{}
	_ repo.UserRepo            = sharedUsers{}
)

type sharedProducts struct{ s *Store }

func (x sharedProducts) Insert(ctx context.Context, p types.Product) error {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&productRepo{x.s.st}).Insert(ctx, p)
}

func (x sharedProducts) Get(ctx context.Context, id string) (types.Product, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&productRepo{x.s.st}).Get(ctx, id)
}

func (x sharedProducts) List(ctx context.Context, onlyOnSale bool) ([]types.Product, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&productRepo{x.s.st}).List(ctx, onlyOnSale)
}

func (x sharedProducts) TryDeduct(ctx context.Context, id string, qty int) (bool, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&productRepo{x.s.st}).TryDeduct(ctx, id, qty)
}

func (x sharedProducts) AddStock(ctx context.Context, id string, delta int) error {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&productRepo{x.s.st}).AddStock(ctx, id, delta)
}

func (x sharedProducts) SetPrice(ctx context.Context, id string, price int64) error {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&productRepo{x.s.st}).SetPrice(ctx, id, price)
}

func (x sharedProducts) SetOnSale(ctx context.Context, id string, onSale bool) error {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&productRepo{x.s.st}).SetOnSale(ctx, id, onSale)
}

type sharedStockFlows struct{ s *Store }

func (x sharedStockFlows) Insert(ctx context.Context, f types.StockFlow) error {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&stockFlowRepo{x.s.st}).Insert(ctx, f)
}

func (x sharedStockFlows) ListByProduct(ctx context.Context, productID string) ([]types.StockFlow, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&stockFlowRepo{x.s.st}).ListByProduct(ctx, productID)
}

func (x sharedStockFlows) ListByOrder(ctx context.Context, orderNo string) ([]types.StockFlow, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&stockFlowRepo{x.s.st}).ListByOrder(ctx, orderNo)
}

func (x sharedStockFlows) CountByOrderType(ctx context.Context, orderNo, changeType string) (int, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&stockFlowRepo{x.s.st}).CountByOrderType(ctx, orderNo, changeType)
}

type sharedOrders struct{ s *Store }

func (x sharedOrders) Insert(ctx context.Context, o types.Order) error {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&orderRepo{x.s.st}).Insert(ctx, o)
}

func (x sharedOrders) Get(ctx context.Context, orderNo string) (types.Order, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&orderRepo{x.s.st}).Get(ctx, orderNo)
}

func (x sharedOrders) ListByBuyer(ctx context.Context, buyerID string) ([]types.Order, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&orderRepo{x.s.st}).ListByBuyer(ctx, buyerID)
}

func (x sharedOrders) ListByStatus(ctx context.Context, statuses ...string) ([]types.Order, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&orderRepo{x.s.st}).ListByStatus(ctx, statuses...)
}

func (x sharedOrders) ListExpiredPending(ctx context.Context, now time.Time) ([]types.Order, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&orderRepo{x.s.st}).ListExpiredPending(ctx, now)
}

func (x sharedOrders) SetStatusCond(ctx context.Context, orderNo, from, to string) (bool, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&orderRepo{x.s.st}).SetStatusCond(ctx, orderNo, from, to)
}

func (x sharedOrders) MarkPaid(ctx context.Context, orderNo string, paidAt time.Time) (bool, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&orderRepo{x.s.st}).MarkPaid(ctx, orderNo, paidAt)
}

func (x sharedOrders) ClosePending(ctx context.Context, orderNo string, closedAt time.Time, reason string) (bool, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&orderRepo{x.s.st}).ClosePending(ctx, orderNo, closedAt, reason)
}

type sharedPayments struct{ s *Store }

func (x sharedPayments) Insert(ctx context.Context, p types.Payment) error {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&paymentRepo{x.s.st}).Insert(ctx, p)
}

func (x sharedPayments) GetByTradeNo(ctx context.Context, tradeNo string) (types.Payment, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&paymentRepo{x.s.st}).GetByTradeNo(ctx, tradeNo)
}

func (x sharedPayments) GetByOrder(ctx context.Context, orderNo string) (types.Payment, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&paymentRepo{x.s.st}).GetByOrder(ctx, orderNo)
}

func (x sharedPayments) SumAmount(ctx context.Context) (int64, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&paymentRepo{x.s.st}).SumAmount(ctx)
}

type sharedPayLogs struct{ s *Store }

func (x sharedPayLogs) Insert(ctx context.Context, l types.PayNotifyLog) error {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&payLogRepo{x.s.st}).Insert(ctx, l)
}

func (x sharedPayLogs) List(ctx context.Context, limit int) ([]types.PayNotifyLog, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&payLogRepo{x.s.st}).List(ctx, limit)
}

func (x sharedPayLogs) ListByOrder(ctx context.Context, orderNo string) ([]types.PayNotifyLog, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&payLogRepo{x.s.st}).ListByOrder(ctx, orderNo)
}

type sharedRefunds struct{ s *Store }

func (x sharedRefunds) Insert(ctx context.Context, r types.Refund) error {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&refundRepo{x.s.st}).Insert(ctx, r)
}

func (x sharedRefunds) Get(ctx context.Context, refundNo string) (types.Refund, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&refundRepo{x.s.st}).Get(ctx, refundNo)
}

func (x sharedRefunds) ListByOrder(ctx context.Context, orderNo string) ([]types.Refund, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&refundRepo{x.s.st}).ListByOrder(ctx, orderNo)
}

func (x sharedRefunds) ListByStatus(ctx context.Context, statuses ...string) ([]types.Refund, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&refundRepo{x.s.st}).ListByStatus(ctx, statuses...)
}

func (x sharedRefunds) Approve(ctx context.Context, refundNo, csID, note string) (bool, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&refundRepo{x.s.st}).Approve(ctx, refundNo, csID, note)
}

func (x sharedRefunds) Reject(ctx context.Context, refundNo, csID, note string) (bool, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&refundRepo{x.s.st}).Reject(ctx, refundNo, csID, note)
}

func (x sharedRefunds) AttachChannel(ctx context.Context, refundNo, channelRefundNo string) error {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&refundRepo{x.s.st}).AttachChannel(ctx, refundNo, channelRefundNo)
}

func (x sharedRefunds) MarkRefunding(ctx context.Context, refundNo string) (bool, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&refundRepo{x.s.st}).MarkRefunding(ctx, refundNo)
}

func (x sharedRefunds) MarkFailedPending(ctx context.Context, refundNo string) (bool, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&refundRepo{x.s.st}).MarkFailedPending(ctx, refundNo)
}

func (x sharedRefunds) MarkSuccess(ctx context.Context, refundNo string) (bool, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&refundRepo{x.s.st}).MarkSuccess(ctx, refundNo)
}

func (x sharedRefunds) SumSuccess(ctx context.Context) (int64, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&refundRepo{x.s.st}).SumSuccess(ctx)
}

type sharedRefundLogs struct{ s *Store }

func (x sharedRefundLogs) Insert(ctx context.Context, l types.RefundNotifyLog) error {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&refundLogRepo{x.s.st}).Insert(ctx, l)
}

func (x sharedRefundLogs) ListByRefund(ctx context.Context, refundNo string) ([]types.RefundNotifyLog, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&refundLogRepo{x.s.st}).ListByRefund(ctx, refundNo)
}

type sharedShipments struct{ s *Store }

func (x sharedShipments) Insert(ctx context.Context, sh types.Shipment) error {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&shipmentRepo{x.s.st}).Insert(ctx, sh)
}

func (x sharedShipments) GetByOrder(ctx context.Context, orderNo string) (types.Shipment, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&shipmentRepo{x.s.st}).GetByOrder(ctx, orderNo)
}

func (x sharedShipments) List(ctx context.Context) ([]types.Shipment, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&shipmentRepo{x.s.st}).List(ctx)
}

type sharedExceptions struct{ s *Store }

func (x sharedExceptions) Insert(ctx context.Context, e types.ExceptionPayment) error {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&exceptionRepo{x.s.st}).Insert(ctx, e)
}

func (x sharedExceptions) List(ctx context.Context, status string) ([]types.ExceptionPayment, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&exceptionRepo{x.s.st}).List(ctx, status)
}

func (x sharedExceptions) ListByOrder(ctx context.Context, orderNo string) ([]types.ExceptionPayment, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&exceptionRepo{x.s.st}).ListByOrder(ctx, orderNo)
}

func (x sharedExceptions) AttachRefund(ctx context.Context, id int64, handler, refundNo string, at time.Time) (bool, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&exceptionRepo{x.s.st}).AttachRefund(ctx, id, handler, refundNo, at)
}

func (x sharedExceptions) MarkRefundedByRefundNo(ctx context.Context, refundNo string) error {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&exceptionRepo{x.s.st}).MarkRefundedByRefundNo(ctx, refundNo)
}

type sharedUsers struct{ s *Store }

func (x sharedUsers) Insert(ctx context.Context, u types.User) error {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&userRepo{x.s.st}).Insert(ctx, u)
}

func (x sharedUsers) GetByUsername(ctx context.Context, username string) (types.User, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&userRepo{x.s.st}).GetByUsername(ctx, username)
}

func (x sharedUsers) GetByID(ctx context.Context, id string) (types.User, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&userRepo{x.s.st}).GetByID(ctx, id)
}

func (x sharedUsers) Count(ctx context.Context) (int, error) {
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	return (&userRepo{x.s.st}).Count(ctx)
}
