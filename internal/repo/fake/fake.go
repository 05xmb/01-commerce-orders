// Package fake 提供仓储接口的内存实现，用于单元测试（支持事务快照回滚与并发互斥）。
package fake

import (
	"context"
	"sort"
	"sync"
	"time"

	"trade/internal/repo"
	"trade/internal/types"
)

type state struct {
	products   map[string]types.Product
	flows      []types.StockFlow
	orders     map[string]types.Order
	payments   []types.Payment
	payLogs    []types.PayNotifyLog
	refunds    map[string]types.Refund
	refundLogs []types.RefundNotifyLog
	shipments  []types.Shipment
	exceptions []types.ExceptionPayment
	users      map[string]types.User
	seq        int64
}

func newState() *state {
	return &state{
		products: map[string]types.Product{},
		orders:   map[string]types.Order{},
		refunds:  map[string]types.Refund{},
		users:    map[string]types.User{},
	}
}

func (s *state) clone() *state {
	c := newState()
	for k, v := range s.products {
		c.products[k] = v
	}
	for k, v := range s.orders {
		c.orders[k] = v
	}
	for k, v := range s.refunds {
		c.refunds[k] = v
	}
	for k, v := range s.users {
		c.users[k] = v
	}
	c.flows = append([]types.StockFlow(nil), s.flows...)
	c.payments = append([]types.Payment(nil), s.payments...)
	c.payLogs = append([]types.PayNotifyLog(nil), s.payLogs...)
	c.refundLogs = append([]types.RefundNotifyLog(nil), s.refundLogs...)
	c.shipments = append([]types.Shipment(nil), s.shipments...)
	c.exceptions = append([]types.ExceptionPayment(nil), s.exceptions...)
	c.seq = s.seq
	return c
}

// Store 线程安全内存仓储
type Store struct {
	mu sync.Mutex
	st *state
}

func NewStore() *Store { return &Store{st: newState()} }

func (s *Store) Repos() repo.Repos { return reposWith(s.st) }

func (s *Store) WithinTx(ctx context.Context, fn func(repo.Repos) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := s.st.clone()
	if err := fn(reposWith(snapshot)); err != nil {
		return err // 快照弃用即回滚
	}
	s.st = snapshot
	return nil
}

func reposWith(st *state) repo.Repos {
	return repo.Repos{
		Products:   &productRepo{st},
		StockFlows: &stockFlowRepo{st},
		Orders:     &orderRepo{st},
		Payments:   &paymentRepo{st},
		PayLogs:    &payLogRepo{st},
		Refunds:    &refundRepo{st},
		RefundLogs: &refundLogRepo{st},
		Shipments:  &shipmentRepo{st},
		Exceptions: &exceptionRepo{st},
		Users:      &userRepo{st},
	}
}

// ---------- product ----------
type productRepo struct{ st *state }

func (r *productRepo) Insert(_ context.Context, p types.Product) error {
	if _, ok := r.st.products[p.ID]; ok {
		return types.ErrDuplicate
	}
	r.st.products[p.ID] = p
	return nil
}

func (r *productRepo) Get(_ context.Context, id string) (types.Product, error) {
	p, ok := r.st.products[id]
	if !ok {
		return p, types.ErrNotFound
	}
	return p, nil
}

func (r *productRepo) List(_ context.Context, onlyOnSale bool) ([]types.Product, error) {
	var out []types.Product
	for _, p := range r.st.products {
		if onlyOnSale && !p.OnSale {
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (r *productRepo) TryDeduct(_ context.Context, id string, qty int) (bool, error) {
	p, ok := r.st.products[id]
	if !ok {
		return false, types.ErrNotFound
	}
	if !p.OnSale || p.Stock < qty {
		return false, nil
	}
	p.Stock -= qty
	r.st.products[id] = p
	return true, nil
}

func (r *productRepo) AddStock(_ context.Context, id string, delta int) error {
	p, ok := r.st.products[id]
	if !ok {
		return types.ErrNotFound
	}
	p.Stock += delta
	r.st.products[id] = p
	return nil
}

func (r *productRepo) SetPrice(_ context.Context, id string, price int64) error {
	p, ok := r.st.products[id]
	if !ok {
		return types.ErrNotFound
	}
	p.Price = price
	r.st.products[id] = p
	return nil
}

func (r *productRepo) SetOnSale(_ context.Context, id string, onSale bool) error {
	p, ok := r.st.products[id]
	if !ok {
		return types.ErrNotFound
	}
	p.OnSale = onSale
	r.st.products[id] = p
	return nil
}

// ---------- stock_flow ----------
type stockFlowRepo struct{ st *state }

func (r *stockFlowRepo) Insert(_ context.Context, f types.StockFlow) error {
	r.st.seq++
	f.ID = r.st.seq
	f.CreatedAt = time.Now()
	r.st.flows = append(r.st.flows, f)
	return nil
}

func (r *stockFlowRepo) ListByProduct(_ context.Context, productID string) ([]types.StockFlow, error) {
	var out []types.StockFlow
	for _, f := range r.st.flows {
		if f.ProductID == productID {
			out = append(out, f)
		}
	}
	return out, nil
}

func (r *stockFlowRepo) ListByOrder(_ context.Context, orderNo string) ([]types.StockFlow, error) {
	var out []types.StockFlow
	for _, f := range r.st.flows {
		if f.OrderNo == orderNo {
			out = append(out, f)
		}
	}
	return out, nil
}

func (r *stockFlowRepo) CountByOrderType(_ context.Context, orderNo, changeType string) (int, error) {
	n := 0
	for _, f := range r.st.flows {
		if f.OrderNo == orderNo && f.ChangeType == changeType {
			n++
		}
	}
	return n, nil
}

// ---------- orders ----------
type orderRepo struct{ st *state }

func (r *orderRepo) Insert(_ context.Context, o types.Order) error {
	if _, ok := r.st.orders[o.OrderNo]; ok {
		return types.ErrDuplicate
	}
	r.st.orders[o.OrderNo] = o
	return nil
}

func (r *orderRepo) Get(_ context.Context, orderNo string) (types.Order, error) {
	o, ok := r.st.orders[orderNo]
	if !ok {
		return o, types.ErrNotFound
	}
	return o, nil
}

func (r *orderRepo) ListByBuyer(_ context.Context, buyerID string) ([]types.Order, error) {
	var out []types.Order
	for _, o := range r.st.orders {
		if o.BuyerID == buyerID {
			out = append(out, o)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (r *orderRepo) ListByStatus(_ context.Context, statuses ...string) ([]types.Order, error) {
	set := map[string]bool{}
	for _, s := range statuses {
		set[s] = true
	}
	var out []types.Order
	for _, o := range r.st.orders {
		if set[o.Status] {
			out = append(out, o)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (r *orderRepo) ListExpiredPending(_ context.Context, now time.Time) ([]types.Order, error) {
	var out []types.Order
	for _, o := range r.st.orders {
		if o.Status == types.OrderPending && !o.PayDeadline.After(now) {
			out = append(out, o)
		}
	}
	return out, nil
}

func (r *orderRepo) SetStatusCond(_ context.Context, orderNo, from, to string) (bool, error) {
	o, ok := r.st.orders[orderNo]
	if !ok {
		return false, types.ErrNotFound
	}
	if o.Status != from {
		return false, nil
	}
	o.Status = to
	r.st.orders[orderNo] = o
	return true, nil
}

func (r *orderRepo) MarkPaid(_ context.Context, orderNo string, paidAt time.Time) (bool, error) {
	o, ok := r.st.orders[orderNo]
	if !ok {
		return false, types.ErrNotFound
	}
	if o.Status != types.OrderPending {
		return false, nil
	}
	o.Status = types.OrderToShip
	o.PaidAt = &paidAt
	r.st.orders[orderNo] = o
	return true, nil
}

func (r *orderRepo) ClosePending(_ context.Context, orderNo string, closedAt time.Time, reason string) (bool, error) {
	o, ok := r.st.orders[orderNo]
	if !ok {
		return false, types.ErrNotFound
	}
	if o.Status != types.OrderPending {
		return false, nil
	}
	o.Status = types.OrderClosed
	o.ClosedAt = &closedAt
	o.CloseReason = &reason
	r.st.orders[orderNo] = o
	return true, nil
}

// ---------- payment ----------
type paymentRepo struct{ st *state }

func (r *paymentRepo) Insert(_ context.Context, p types.Payment) error {
	for _, x := range r.st.payments {
		if x.TradeNo == p.TradeNo {
			return types.ErrDuplicate
		}
	}
	r.st.seq++
	p.ID = r.st.seq
	p.CreatedAt = time.Now()
	r.st.payments = append(r.st.payments, p)
	return nil
}

func (r *paymentRepo) GetByTradeNo(_ context.Context, tradeNo string) (types.Payment, error) {
	for _, x := range r.st.payments {
		if x.TradeNo == tradeNo {
			return x, nil
		}
	}
	return types.Payment{}, types.ErrNotFound
}

func (r *paymentRepo) GetByOrder(_ context.Context, orderNo string) (types.Payment, error) {
	for _, x := range r.st.payments {
		if x.OrderNo == orderNo {
			return x, nil
		}
	}
	return types.Payment{}, types.ErrNotFound
}

func (r *paymentRepo) SumAmount(_ context.Context) (int64, error) {
	var sum int64
	for _, x := range r.st.payments {
		if x.Status == "success" {
			sum += x.Amount
		}
	}
	return sum, nil
}

// ---------- pay_notify_log ----------
type payLogRepo struct{ st *state }

func (r *payLogRepo) Insert(_ context.Context, l types.PayNotifyLog) error {
	r.st.seq++
	l.ID = r.st.seq
	l.CreatedAt = time.Now()
	r.st.payLogs = append(r.st.payLogs, l)
	return nil
}

func (r *payLogRepo) List(_ context.Context, limit int) ([]types.PayNotifyLog, error) {
	if limit <= 0 {
		limit = 200
	}
	out := append([]types.PayNotifyLog(nil), r.st.payLogs...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *payLogRepo) ListByOrder(_ context.Context, orderNo string) ([]types.PayNotifyLog, error) {
	var out []types.PayNotifyLog
	for _, l := range r.st.payLogs {
		if l.OrderNo == orderNo {
			out = append(out, l)
		}
	}
	return out, nil
}

// ---------- refund ----------
type refundRepo struct{ st *state }

func (r *refundRepo) Insert(_ context.Context, rf types.Refund) error {
	if _, ok := r.st.refunds[rf.RefundNo]; ok {
		return types.ErrDuplicate
	}
	r.st.refunds[rf.RefundNo] = rf
	return nil
}

func (r *refundRepo) Get(_ context.Context, refundNo string) (types.Refund, error) {
	rf, ok := r.st.refunds[refundNo]
	if !ok {
		return rf, types.ErrRefundNotFound
	}
	return rf, nil
}

func (r *refundRepo) ListByOrder(_ context.Context, orderNo string) ([]types.Refund, error) {
	var out []types.Refund
	for _, rf := range r.st.refunds {
		if rf.OrderNo == orderNo {
			out = append(out, rf)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (r *refundRepo) ListByStatus(_ context.Context, statuses ...string) ([]types.Refund, error) {
	set := map[string]bool{}
	for _, s := range statuses {
		set[s] = true
	}
	var out []types.Refund
	for _, rf := range r.st.refunds {
		if set[rf.Status] {
			out = append(out, rf)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (r *refundRepo) Approve(_ context.Context, refundNo, note string) (bool, error) {
	rf, ok := r.st.refunds[refundNo]
	if !ok {
		return false, types.ErrRefundNotFound
	}
	if rf.Status != types.RefundReview {
		return false, nil
	}
	rf.Status = types.RefundRefunding
	rf.CsNote = note
	rf.Attempt++
	r.st.refunds[refundNo] = rf
	return true, nil
}

func (r *refundRepo) Reject(_ context.Context, refundNo, note string) (bool, error) {
	rf, ok := r.st.refunds[refundNo]
	if !ok {
		return false, types.ErrRefundNotFound
	}
	if rf.Status != types.RefundReview {
		return false, nil
	}
	rf.Status = types.RefundRejected
	rf.CsNote = note
	r.st.refunds[refundNo] = rf
	return true, nil
}

func (r *refundRepo) AttachChannel(_ context.Context, refundNo, channelRefundNo string) error {
	rf, ok := r.st.refunds[refundNo]
	if !ok {
		return types.ErrRefundNotFound
	}
	rf.ChannelRefundNo = channelRefundNo
	r.st.refunds[refundNo] = rf
	return nil
}

func (r *refundRepo) MarkRefunding(_ context.Context, refundNo string) (bool, error) {
	rf, ok := r.st.refunds[refundNo]
	if !ok {
		return false, types.ErrRefundNotFound
	}
	if rf.Status != types.RefundFailedPending {
		return false, nil
	}
	rf.Status = types.RefundRefunding
	rf.Attempt++
	r.st.refunds[refundNo] = rf
	return true, nil
}

func (r *refundRepo) MarkFailedPending(_ context.Context, refundNo string) (bool, error) {
	rf, ok := r.st.refunds[refundNo]
	if !ok {
		return false, types.ErrRefundNotFound
	}
	if rf.Status != types.RefundRefunding {
		return false, nil
	}
	rf.Status = types.RefundFailedPending
	r.st.refunds[refundNo] = rf
	return true, nil
}

func (r *refundRepo) MarkSuccess(_ context.Context, refundNo string) (bool, error) {
	rf, ok := r.st.refunds[refundNo]
	if !ok {
		return false, types.ErrRefundNotFound
	}
	if rf.Status != types.RefundRefunding && rf.Status != types.RefundFailedPending {
		return false, nil
	}
	rf.Status = types.RefundSuccess
	r.st.refunds[refundNo] = rf
	return true, nil
}

func (r *refundRepo) SumSuccess(_ context.Context) (int64, error) {
	var sum int64
	for _, rf := range r.st.refunds {
		if rf.Status == types.RefundSuccess {
			sum += rf.Amount
		}
	}
	return sum, nil
}

// ---------- refund_notify_log ----------
type refundLogRepo struct{ st *state }

func (r *refundLogRepo) Insert(_ context.Context, l types.RefundNotifyLog) error {
	r.st.seq++
	l.ID = r.st.seq
	l.CreatedAt = time.Now()
	r.st.refundLogs = append(r.st.refundLogs, l)
	return nil
}

func (r *refundLogRepo) ListByRefund(_ context.Context, refundNo string) ([]types.RefundNotifyLog, error) {
	var out []types.RefundNotifyLog
	for _, l := range r.st.refundLogs {
		if l.RefundNo == refundNo {
			out = append(out, l)
		}
	}
	return out, nil
}

// ---------- shipment ----------
type shipmentRepo struct{ st *state }

func (r *shipmentRepo) Insert(_ context.Context, s types.Shipment) error {
	for _, x := range r.st.shipments {
		if x.OrderNo == s.OrderNo {
			return types.ErrDuplicate
		}
	}
	r.st.seq++
	s.ID = r.st.seq
	s.CreatedAt = time.Now()
	r.st.shipments = append(r.st.shipments, s)
	return nil
}

func (r *shipmentRepo) GetByOrder(_ context.Context, orderNo string) (types.Shipment, error) {
	for _, x := range r.st.shipments {
		if x.OrderNo == orderNo {
			return x, nil
		}
	}
	return types.Shipment{}, types.ErrNotFound
}

func (r *shipmentRepo) List(_ context.Context) ([]types.Shipment, error) {
	return append([]types.Shipment(nil), r.st.shipments...), nil
}

// ---------- exception_payment ----------
type exceptionRepo struct{ st *state }

func (r *exceptionRepo) Insert(_ context.Context, e types.ExceptionPayment) error {
	for _, x := range r.st.exceptions {
		if x.TradeNo == e.TradeNo {
			return types.ErrDuplicate
		}
	}
	r.st.seq++
	e.ID = r.st.seq
	e.CreatedAt = time.Now()
	r.st.exceptions = append(r.st.exceptions, e)
	return nil
}

func (r *exceptionRepo) List(_ context.Context, status string) ([]types.ExceptionPayment, error) {
	var out []types.ExceptionPayment
	for i := len(r.st.exceptions) - 1; i >= 0; i-- {
		x := r.st.exceptions[i]
		if status == "" || x.Status == status {
			out = append(out, x)
		}
	}
	return out, nil
}

func (r *exceptionRepo) ListByOrder(_ context.Context, orderNo string) ([]types.ExceptionPayment, error) {
	var out []types.ExceptionPayment
	for _, x := range r.st.exceptions {
		if x.OrderNo == orderNo {
			out = append(out, x)
		}
	}
	return out, nil
}

func (r *exceptionRepo) AttachRefund(_ context.Context, id int64, handler, refundNo string, at time.Time) (bool, error) {
	for i, x := range r.st.exceptions {
		if x.ID == id {
			if x.Status != types.ExceptionPending {
				return false, nil
			}
			x.Handler = &handler
			x.RefundNo = &refundNo
			x.HandledAt = &at
			x.Status = types.ExceptionRefunding
			r.st.exceptions[i] = x
			return true, nil
		}
	}
	return false, types.ErrNotFound
}

func (r *exceptionRepo) MarkRefundedByRefundNo(_ context.Context, refundNo string) error {
	for i, x := range r.st.exceptions {
		if x.RefundNo != nil && *x.RefundNo == refundNo {
			x.Status = types.ExceptionRefunded
			r.st.exceptions[i] = x
		}
	}
	return nil
}

// ---------- user ----------
type userRepo struct{ st *state }

func (r *userRepo) Insert(_ context.Context, u types.User) error {
	for _, x := range r.st.users {
		if x.Username == u.Username {
			return types.ErrDuplicate
		}
	}
	r.st.users[u.ID] = u
	return nil
}

func (r *userRepo) GetByUsername(_ context.Context, username string) (types.User, error) {
	for _, x := range r.st.users {
		if x.Username == username {
			return x, nil
		}
	}
	return types.User{}, types.ErrNotFound
}

func (r *userRepo) GetByID(_ context.Context, id string) (types.User, error) {
	u, ok := r.st.users[id]
	if !ok {
		return u, types.ErrNotFound
	}
	return u, nil
}

func (r *userRepo) Count(_ context.Context) (int, error) {
	return len(r.st.users), nil
}
