package handler

import (
	"context"
	"errors"
	"time"

	"trade/internal/repo"
	"trade/internal/types"
)

var errTest = errors.New("db down")

// errStore 包装内存仓储，把指定方法替换为失败，用于覆盖 handler 的错误分支
type errStore struct {
	repo.Store
}

func (s *errStore) Repos() repo.Repos { return s.wrap(s.Store.Repos()) }

func (s *errStore) WithinTx(ctx context.Context, fn func(repo.Repos) error) error {
	return s.Store.WithinTx(ctx, func(r repo.Repos) error { return fn(s.wrap(r)) })
}

func (s *errStore) wrap(r repo.Repos) repo.Repos {
	r.Products = &eProducts{r.Products}
	r.Orders = &eOrders{r.Orders}
	r.Payments = &ePayments{r.Payments}
	r.PayLogs = &ePayLogs{r.PayLogs}
	r.Refunds = &eRefunds{r.Refunds}
	r.Exceptions = &eExceptions{r.Exceptions}
	r.StockFlows = &eStockFlows{r.StockFlows}
	return r
}

type eProducts struct{ repo.ProductRepo }

func (e *eProducts) List(context.Context, bool) ([]types.Product, error) { return nil, errTest }
func (e *eProducts) TryDeduct(context.Context, string, int) (bool, error) {
	return false, errTest
}
func (e *eProducts) SetPrice(context.Context, string, int64) error { return errTest }
func (e *eProducts) SetOnSale(context.Context, string, bool) error { return errTest }

type eOrders struct{ repo.OrderRepo }

func (e *eOrders) Get(context.Context, string) (types.Order, error) { return types.Order{}, errTest }
func (e *eOrders) ListByBuyer(context.Context, string) ([]types.Order, error) {
	return nil, errTest
}
func (e *eOrders) ListExpiredPending(context.Context, time.Time) ([]types.Order, error) {
	return nil, errTest
}
func (e *eOrders) ListByStatus(context.Context, ...string) ([]types.Order, error) {
	return nil, errTest
}
func (e *eOrders) SetStatusCond(context.Context, string, string, string) (bool, error) {
	return false, errTest
}

type ePayments struct{ repo.PaymentRepo }

func (e *ePayments) SumAmount(context.Context) (int64, error) { return 0, errTest }
func (e *ePayments) GetByTradeNo(context.Context, string) (types.Payment, error) {
	return types.Payment{}, errTest
}

type ePayLogs struct{ repo.PayNotifyLogRepo }

func (e *ePayLogs) List(context.Context, int) ([]types.PayNotifyLog, error) { return nil, errTest }

type eRefunds struct{ repo.RefundRepo }

func (e *eRefunds) Get(context.Context, string) (types.Refund, error) {
	return types.Refund{}, errTest
}
func (e *eRefunds) ListByStatus(context.Context, ...string) ([]types.Refund, error) {
	return nil, errTest
}
func (e *eRefunds) Approve(context.Context, string, string) (bool, error) { return false, errTest }
func (e *eRefunds) MarkRefunding(context.Context, string) (bool, error) {
	return false, errTest
}

type eExceptions struct{ repo.ExceptionRepo }

func (e *eExceptions) List(context.Context, string) ([]types.ExceptionPayment, error) {
	return nil, errTest
}

type eStockFlows struct{ repo.StockFlowRepo }

func (e *eStockFlows) ListByProduct(context.Context, string) ([]types.StockFlow, error) {
	return nil, errTest
}
