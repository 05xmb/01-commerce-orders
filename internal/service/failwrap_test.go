package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"trade/internal/repo"
	"trade/internal/types"
)

var errDB = errors.New("db down")

// failPlan 按"方法名→错误队列"注入一次性故障，用于覆盖数据库错误分支
type failPlan struct {
	mu    sync.Mutex
	rules map[string][]error
}

func newFailPlan() *failPlan { return &failPlan{rules: map[string][]error{}} }

func (p *failPlan) set(method string, errs ...error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rules[method] = errs
}

// take 每次调用弹出队首错误，队列空则放行
func (p *failPlan) take(method string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	q := p.rules[method]
	if len(q) == 0 {
		return nil
	}
	e := q[0]
	p.rules[method] = q[1:]
	return e
}

// failStore 包装内存仓储，把故障注入织入每个方法
type failStore struct {
	repo.Store
	p *failPlan
}

func (s *failStore) Repos() repo.Repos { return s.wrap(s.Store.Repos()) }

func (s *failStore) WithinTx(ctx context.Context, fn func(repo.Repos) error) error {
	return s.Store.WithinTx(ctx, func(r repo.Repos) error { return fn(s.wrap(r)) })
}

func (s *failStore) wrap(r repo.Repos) repo.Repos {
	r.Products = &failProducts{r.Products, s.p}
	r.Orders = &failOrders{r.Orders, s.p}
	r.StockFlows = &failFlows{r.StockFlows, s.p}
	r.Payments = &failPayments{r.Payments, s.p}
	r.Refunds = &failRefunds{r.Refunds, s.p}
	r.Shipments = &failShipments{r.Shipments, s.p}
	r.Exceptions = &failExceptions{r.Exceptions, s.p}
	r.PayLogs = &failPayLogs{r.PayLogs, s.p}
	r.Users = &failUsers{r.Users, s.p}
	return r
}

type failUsers struct {
	repo.UserRepo
	p *failPlan
}

func (w *failUsers) Insert(ctx context.Context, u types.User) error {
	if err := w.p.take("Users.Insert"); err != nil {
		return err
	}
	return w.UserRepo.Insert(ctx, u)
}

func (w *failUsers) GetByUsername(ctx context.Context, username string) (types.User, error) {
	if err := w.p.take("Users.GetByUsername"); err != nil {
		return types.User{}, err
	}
	return w.UserRepo.GetByUsername(ctx, username)
}

type failProducts struct {
	repo.ProductRepo
	p *failPlan
}

func (w *failProducts) Get(ctx context.Context, id string) (types.Product, error) {
	if err := w.p.take("Products.Get"); err != nil {
		return types.Product{}, err
	}
	return w.ProductRepo.Get(ctx, id)
}

func (w *failProducts) Insert(ctx context.Context, prod types.Product) error {
	if err := w.p.take("Products.Insert"); err != nil {
		return err
	}
	return w.ProductRepo.Insert(ctx, prod)
}

func (w *failProducts) TryDeduct(ctx context.Context, id string, qty int) (bool, error) {
	if err := w.p.take("Products.TryDeduct"); err != nil {
		return false, err
	}
	return w.ProductRepo.TryDeduct(ctx, id, qty)
}

func (w *failProducts) AddStock(ctx context.Context, id string, delta int) error {
	if err := w.p.take("Products.AddStock"); err != nil {
		return err
	}
	return w.ProductRepo.AddStock(ctx, id, delta)
}

func (w *failProducts) List(ctx context.Context, onlyOnSale bool) ([]types.Product, error) {
	if err := w.p.take("Products.List"); err != nil {
		return nil, err
	}
	return w.ProductRepo.List(ctx, onlyOnSale)
}

func (w *failProducts) SetPrice(ctx context.Context, id string, price int64) error {
	if err := w.p.take("Products.SetPrice"); err != nil {
		return err
	}
	return w.ProductRepo.SetPrice(ctx, id, price)
}

func (w *failProducts) SetOnSale(ctx context.Context, id string, onSale bool) error {
	if err := w.p.take("Products.SetOnSale"); err != nil {
		return err
	}
	return w.ProductRepo.SetOnSale(ctx, id, onSale)
}

type failOrders struct {
	repo.OrderRepo
	p *failPlan
}

func (w *failOrders) Get(ctx context.Context, orderNo string) (types.Order, error) {
	if err := w.p.take("Orders.Get"); err != nil {
		return types.Order{}, err
	}
	return w.OrderRepo.Get(ctx, orderNo)
}

func (w *failOrders) Insert(ctx context.Context, o types.Order) error {
	if err := w.p.take("Orders.Insert"); err != nil {
		return err
	}
	return w.OrderRepo.Insert(ctx, o)
}

func (w *failOrders) ClosePending(ctx context.Context, orderNo string, at time.Time, reason string) (bool, error) {
	if err := w.p.take("Orders.ClosePending"); err != nil {
		return false, err
	}
	return w.OrderRepo.ClosePending(ctx, orderNo, at, reason)
}

func (w *failOrders) SetStatusCond(ctx context.Context, orderNo, from, to string) (bool, error) {
	if err := w.p.take("Orders.SetStatusCond"); err != nil {
		return false, err
	}
	return w.OrderRepo.SetStatusCond(ctx, orderNo, from, to)
}

func (w *failOrders) MarkPaid(ctx context.Context, orderNo string, at time.Time) (bool, error) {
	if err := w.p.take("Orders.MarkPaid"); err != nil {
		return false, err
	}
	return w.OrderRepo.MarkPaid(ctx, orderNo, at)
}

func (w *failOrders) ListByBuyer(ctx context.Context, buyerID string) ([]types.Order, error) {
	if err := w.p.take("Orders.ListByBuyer"); err != nil {
		return nil, err
	}
	return w.OrderRepo.ListByBuyer(ctx, buyerID)
}

func (w *failOrders) ListExpiredPending(ctx context.Context, now time.Time) ([]types.Order, error) {
	if err := w.p.take("Orders.ListExpiredPending"); err != nil {
		return nil, err
	}
	return w.OrderRepo.ListExpiredPending(ctx, now)
}

type failFlows struct {
	repo.StockFlowRepo
	p *failPlan
}

func (w *failFlows) Insert(ctx context.Context, f types.StockFlow) error {
	if err := w.p.take("StockFlows.Insert"); err != nil {
		return err
	}
	return w.StockFlowRepo.Insert(ctx, f)
}

func (w *failFlows) ListByProduct(ctx context.Context, productID string) ([]types.StockFlow, error) {
	if err := w.p.take("StockFlows.ListByProduct"); err != nil {
		return nil, err
	}
	return w.StockFlowRepo.ListByProduct(ctx, productID)
}

func (w *failFlows) ListByOrder(ctx context.Context, orderNo string) ([]types.StockFlow, error) {
	if err := w.p.take("StockFlows.ListByOrder"); err != nil {
		return nil, err
	}
	return w.StockFlowRepo.ListByOrder(ctx, orderNo)
}

type failPayments struct {
	repo.PaymentRepo
	p *failPlan
}

func (w *failPayments) GetByTradeNo(ctx context.Context, tradeNo string) (types.Payment, error) {
	if err := w.p.take("Payments.GetByTradeNo"); err != nil {
		return types.Payment{}, err
	}
	return w.PaymentRepo.GetByTradeNo(ctx, tradeNo)
}

func (w *failPayments) GetByOrder(ctx context.Context, orderNo string) (types.Payment, error) {
	if err := w.p.take("Payments.GetByOrder"); err != nil {
		return types.Payment{}, err
	}
	return w.PaymentRepo.GetByOrder(ctx, orderNo)
}

func (w *failPayments) Insert(ctx context.Context, pm types.Payment) error {
	if err := w.p.take("Payments.Insert"); err != nil {
		return err
	}
	return w.PaymentRepo.Insert(ctx, pm)
}

func (w *failPayments) SumAmount(ctx context.Context) (int64, error) {
	if err := w.p.take("Payments.SumAmount"); err != nil {
		return 0, err
	}
	return w.PaymentRepo.SumAmount(ctx)
}

type failRefunds struct {
	repo.RefundRepo
	p *failPlan
}

func (w *failRefunds) Insert(ctx context.Context, rf types.Refund) error {
	if err := w.p.take("Refunds.Insert"); err != nil {
		return err
	}
	return w.RefundRepo.Insert(ctx, rf)
}

func (w *failRefunds) Get(ctx context.Context, refundNo string) (types.Refund, error) {
	if err := w.p.take("Refunds.Get"); err != nil {
		return types.Refund{}, err
	}
	return w.RefundRepo.Get(ctx, refundNo)
}

func (w *failRefunds) Approve(ctx context.Context, refundNo, csID, note string) (bool, error) {
	if err := w.p.take("Refunds.Approve"); err != nil {
		return false, err
	}
	return w.RefundRepo.Approve(ctx, refundNo, csID, note)
}

func (w *failRefunds) Reject(ctx context.Context, refundNo, csID, note string) (bool, error) {
	if err := w.p.take("Refunds.Reject"); err != nil {
		return false, err
	}
	return w.RefundRepo.Reject(ctx, refundNo, csID, note)
}

func (w *failRefunds) MarkRefunding(ctx context.Context, refundNo string) (bool, error) {
	if err := w.p.take("Refunds.MarkRefunding"); err != nil {
		return false, err
	}
	return w.RefundRepo.MarkRefunding(ctx, refundNo)
}

func (w *failRefunds) MarkFailedPending(ctx context.Context, refundNo string) (bool, error) {
	if err := w.p.take("Refunds.MarkFailedPending"); err != nil {
		return false, err
	}
	return w.RefundRepo.MarkFailedPending(ctx, refundNo)
}

func (w *failRefunds) MarkSuccess(ctx context.Context, refundNo string) (bool, error) {
	if err := w.p.take("Refunds.MarkSuccess"); err != nil {
		return false, err
	}
	return w.RefundRepo.MarkSuccess(ctx, refundNo)
}

func (w *failRefunds) SumSuccess(ctx context.Context) (int64, error) {
	if err := w.p.take("Refunds.SumSuccess"); err != nil {
		return 0, err
	}
	return w.RefundRepo.SumSuccess(ctx)
}

func (w *failRefunds) ListByOrder(ctx context.Context, orderNo string) ([]types.Refund, error) {
	if err := w.p.take("Refunds.ListByOrder"); err != nil {
		return nil, err
	}
	return w.RefundRepo.ListByOrder(ctx, orderNo)
}

type failShipments struct {
	repo.ShipmentRepo
	p *failPlan
}

func (w *failShipments) Insert(ctx context.Context, sh types.Shipment) error {
	if err := w.p.take("Shipments.Insert"); err != nil {
		return err
	}
	return w.ShipmentRepo.Insert(ctx, sh)
}

func (w *failShipments) GetByOrder(ctx context.Context, orderNo string) (types.Shipment, error) {
	if err := w.p.take("Shipments.GetByOrder"); err != nil {
		return types.Shipment{}, err
	}
	return w.ShipmentRepo.GetByOrder(ctx, orderNo)
}

type failExceptions struct {
	repo.ExceptionRepo
	p *failPlan
}

func (w *failExceptions) Insert(ctx context.Context, e types.ExceptionPayment) error {
	if err := w.p.take("Exceptions.Insert"); err != nil {
		return err
	}
	return w.ExceptionRepo.Insert(ctx, e)
}

func (w *failExceptions) List(ctx context.Context, status string) ([]types.ExceptionPayment, error) {
	if err := w.p.take("Exceptions.List"); err != nil {
		return nil, err
	}
	return w.ExceptionRepo.List(ctx, status)
}

func (w *failExceptions) ListByOrder(ctx context.Context, orderNo string) ([]types.ExceptionPayment, error) {
	if err := w.p.take("Exceptions.ListByOrder"); err != nil {
		return nil, err
	}
	return w.ExceptionRepo.ListByOrder(ctx, orderNo)
}

func (w *failExceptions) AttachRefund(ctx context.Context, id int64, handler, refundNo string, at time.Time) (bool, error) {
	if err := w.p.take("Exceptions.AttachRefund"); err != nil {
		return false, err
	}
	return w.ExceptionRepo.AttachRefund(ctx, id, handler, refundNo, at)
}

func (w *failExceptions) MarkRefundedByRefundNo(ctx context.Context, refundNo string) error {
	if err := w.p.take("Exceptions.MarkRefundedByRefundNo"); err != nil {
		return err
	}
	return w.ExceptionRepo.MarkRefundedByRefundNo(ctx, refundNo)
}

type failPayLogs struct {
	repo.PayNotifyLogRepo
	p *failPlan
}

func (w *failPayLogs) ListByOrder(ctx context.Context, orderNo string) ([]types.PayNotifyLog, error) {
	if err := w.p.take("PayLogs.ListByOrder"); err != nil {
		return nil, err
	}
	return w.PayNotifyLogRepo.ListByOrder(ctx, orderNo)
}
