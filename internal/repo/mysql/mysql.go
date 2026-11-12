// Package mysql 基于标准 database/sql 的 MySQL 仓储实现。
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"trade/internal/repo"
	"trade/internal/types"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) Repos() repo.Repos { return reposWith(s.db) }

func (s *Store) WithinTx(ctx context.Context, fn func(repo.Repos) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(reposWith(tx)); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func isDuplicate(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Duplicate entry")
}

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return types.ErrNotFound
	}
	return err
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func reposWith(q execer) repo.Repos {
	return repo.Repos{
		Products:   productRepo{q},
		StockFlows: stockFlowRepo{q},
		Orders:     orderRepo{q},
		Payments:   paymentRepo{q},
		PayLogs:    payLogRepo{q},
		Refunds:    refundRepo{q},
		RefundLogs: refundLogRepo{q},
		Shipments:  shipmentRepo{q},
		Exceptions: exceptionRepo{q},
		Users:      userRepo{q},
	}
}

// ---------- product ----------
type productRepo struct{ q execer }

func (r productRepo) Insert(ctx context.Context, p types.Product) error {
	_, err := r.q.ExecContext(ctx, `INSERT INTO product(id,name,price,stock,on_sale) VALUES(?,?,?,?,?)`,
		p.ID, p.Name, p.Price, p.Stock, p.OnSale)
	return err
}

func (r productRepo) Get(ctx context.Context, id string) (types.Product, error) {
	var p types.Product
	err := r.q.QueryRowContext(ctx, `SELECT id,name,price,stock,on_sale FROM product WHERE id=?`, id).
		Scan(&p.ID, &p.Name, &p.Price, &p.Stock, &p.OnSale)
	return p, notFound(err)
}

func (r productRepo) List(ctx context.Context, onlyOnSale bool) ([]types.Product, error) {
	query := `SELECT id,name,price,stock,on_sale FROM product`
	if onlyOnSale {
		query += ` WHERE on_sale=1`
	}
	query += ` ORDER BY id`
	rows, err := r.q.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.Product
	for rows.Next() {
		var p types.Product
		if err := rows.Scan(&p.ID, &p.Name, &p.Price, &p.Stock, &p.OnSale); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r productRepo) TryDeduct(ctx context.Context, id string, qty int) (bool, error) {
	res, err := r.q.ExecContext(ctx,
		`UPDATE product SET stock=stock-? WHERE id=? AND on_sale=1 AND stock>=?`, qty, id, qty)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (r productRepo) AddStock(ctx context.Context, id string, delta int) error {
	_, err := r.q.ExecContext(ctx, `UPDATE product SET stock=stock+? WHERE id=?`, delta, id)
	return err
}

func (r productRepo) SetPrice(ctx context.Context, id string, price int64) error {
	_, err := r.q.ExecContext(ctx, `UPDATE product SET price=? WHERE id=?`, price, id)
	return err
}

func (r productRepo) SetOnSale(ctx context.Context, id string, onSale bool) error {
	_, err := r.q.ExecContext(ctx, `UPDATE product SET on_sale=? WHERE id=?`, onSale, id)
	return err
}

// ---------- stock_flow ----------
type stockFlowRepo struct{ q execer }

func (r stockFlowRepo) Insert(ctx context.Context, f types.StockFlow) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO stock_flow(product_id,order_no,change_type,qty,stock_after) VALUES(?,?,?,?,?)`,
		f.ProductID, f.OrderNo, f.ChangeType, f.Qty, f.StockAfter)
	return err
}

func scanFlows(rows *sql.Rows) ([]types.StockFlow, error) {
	defer rows.Close()
	var out []types.StockFlow
	for rows.Next() {
		var f types.StockFlow
		if err := rows.Scan(&f.ID, &f.ProductID, &f.OrderNo, &f.ChangeType, &f.Qty, &f.StockAfter, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (r stockFlowRepo) ListByProduct(ctx context.Context, productID string) ([]types.StockFlow, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id,product_id,order_no,change_type,qty,stock_after,created_at FROM stock_flow WHERE product_id=? ORDER BY id`, productID)
	if err != nil {
		return nil, err
	}
	return scanFlows(rows)
}

func (r stockFlowRepo) ListByOrder(ctx context.Context, orderNo string) ([]types.StockFlow, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id,product_id,order_no,change_type,qty,stock_after,created_at FROM stock_flow WHERE order_no=? ORDER BY id`, orderNo)
	if err != nil {
		return nil, err
	}
	return scanFlows(rows)
}

func (r stockFlowRepo) CountByOrderType(ctx context.Context, orderNo, changeType string) (int, error) {
	var n int
	err := r.q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM stock_flow WHERE order_no=? AND change_type=?`, orderNo, changeType).Scan(&n)
	return n, err
}

// ---------- orders ----------
type orderRepo struct{ q execer }

const orderCols = `order_no,buyer_id,product_id,product_name,price,qty,amount,status,created_at,pay_deadline,paid_at,closed_at,close_reason`

func scanOrder(row interface{ Scan(...any) error }) (types.Order, error) {
	var o types.Order
	err := row.Scan(&o.OrderNo, &o.BuyerID, &o.ProductID, &o.ProductName, &o.Price, &o.Qty,
		&o.Amount, &o.Status, &o.CreatedAt, &o.PayDeadline, &o.PaidAt, &o.ClosedAt, &o.CloseReason)
	return o, err
}

func (r orderRepo) Insert(ctx context.Context, o types.Order) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO orders(`+orderCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		o.OrderNo, o.BuyerID, o.ProductID, o.ProductName, o.Price, o.Qty, o.Amount, o.Status,
		o.CreatedAt, o.PayDeadline, o.PaidAt, o.ClosedAt, o.CloseReason)
	return err
}

func (r orderRepo) Get(ctx context.Context, orderNo string) (types.Order, error) {
	o, err := scanOrder(r.q.QueryRowContext(ctx, `SELECT `+orderCols+` FROM orders WHERE order_no=?`, orderNo))
	return o, notFound(err)
}

func scanOrders(rows *sql.Rows) ([]types.Order, error) {
	defer rows.Close()
	var out []types.Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (r orderRepo) ListByBuyer(ctx context.Context, buyerID string) ([]types.Order, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT `+orderCols+` FROM orders WHERE buyer_id=? ORDER BY created_at DESC`, buyerID)
	if err != nil {
		return nil, err
	}
	return scanOrders(rows)
}

func (r orderRepo) ListByStatus(ctx context.Context, statuses ...string) ([]types.Order, error) {
	if len(statuses) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(statuses)), ",")
	args := make([]any, len(statuses))
	for i, s := range statuses {
		args[i] = s
	}
	rows, err := r.q.QueryContext(ctx,
		`SELECT `+orderCols+` FROM orders WHERE status IN (`+placeholders+`) ORDER BY created_at`, args...)
	if err != nil {
		return nil, err
	}
	return scanOrders(rows)
}

func (r orderRepo) ListExpiredPending(ctx context.Context, now time.Time) ([]types.Order, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT `+orderCols+` FROM orders WHERE status=? AND pay_deadline<=?`, types.OrderPending, now)
	if err != nil {
		return nil, err
	}
	return scanOrders(rows)
}

func (r orderRepo) SetStatusCond(ctx context.Context, orderNo, from, to string) (bool, error) {
	res, err := r.q.ExecContext(ctx, `UPDATE orders SET status=? WHERE order_no=? AND status=?`, to, orderNo, from)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (r orderRepo) MarkPaid(ctx context.Context, orderNo string, paidAt time.Time) (bool, error) {
	res, err := r.q.ExecContext(ctx,
		`UPDATE orders SET status=?, paid_at=? WHERE order_no=? AND status=?`,
		types.OrderToShip, paidAt, orderNo, types.OrderPending)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (r orderRepo) ClosePending(ctx context.Context, orderNo string, closedAt time.Time, reason string) (bool, error) {
	res, err := r.q.ExecContext(ctx,
		`UPDATE orders SET status=?, closed_at=?, close_reason=? WHERE order_no=? AND status=?`,
		types.OrderClosed, closedAt, reason, orderNo, types.OrderPending)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// ---------- payment ----------
type paymentRepo struct{ q execer }

func (r paymentRepo) Insert(ctx context.Context, p types.Payment) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO payment(order_no,trade_no,amount,status) VALUES(?,?,?,?)`,
		p.OrderNo, p.TradeNo, p.Amount, p.Status)
	if isDuplicate(err) {
		return types.ErrDuplicate
	}
	return err
}

func (r paymentRepo) GetByTradeNo(ctx context.Context, tradeNo string) (types.Payment, error) {
	var p types.Payment
	err := r.q.QueryRowContext(ctx,
		`SELECT id,order_no,trade_no,amount,status,created_at FROM payment WHERE trade_no=?`, tradeNo).
		Scan(&p.ID, &p.OrderNo, &p.TradeNo, &p.Amount, &p.Status, &p.CreatedAt)
	return p, notFound(err)
}

func (r paymentRepo) GetByOrder(ctx context.Context, orderNo string) (types.Payment, error) {
	var p types.Payment
	err := r.q.QueryRowContext(ctx,
		`SELECT id,order_no,trade_no,amount,status,created_at FROM payment WHERE order_no=?`, orderNo).
		Scan(&p.ID, &p.OrderNo, &p.TradeNo, &p.Amount, &p.Status, &p.CreatedAt)
	return p, notFound(err)
}

func (r paymentRepo) SumAmount(ctx context.Context) (int64, error) {
	var sum sql.NullInt64
	err := r.q.QueryRowContext(ctx, `SELECT SUM(amount) FROM payment WHERE status='success'`).Scan(&sum)
	return sum.Int64, err
}

// ---------- pay_notify_log ----------
type payLogRepo struct{ q execer }

func (r payLogRepo) Insert(ctx context.Context, l types.PayNotifyLog) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO pay_notify_log(notify_id,order_no,trade_no,amount,verify_ok,result,raw) VALUES(?,?,?,?,?,?,?)`,
		l.NotifyID, l.OrderNo, l.TradeNo, l.Amount, l.VerifyOK, l.Result, l.Raw)
	return err
}

func scanPayLogs(rows *sql.Rows) ([]types.PayNotifyLog, error) {
	defer rows.Close()
	var out []types.PayNotifyLog
	for rows.Next() {
		var l types.PayNotifyLog
		if err := rows.Scan(&l.ID, &l.NotifyID, &l.OrderNo, &l.TradeNo, &l.Amount, &l.VerifyOK, &l.Result, &l.Raw, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (r payLogRepo) List(ctx context.Context, limit int) ([]types.PayNotifyLog, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := r.q.QueryContext(ctx,
		`SELECT id,notify_id,order_no,trade_no,amount,verify_ok,result,raw,created_at FROM pay_notify_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	return scanPayLogs(rows)
}

func (r payLogRepo) ListByOrder(ctx context.Context, orderNo string) ([]types.PayNotifyLog, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id,notify_id,order_no,trade_no,amount,verify_ok,result,raw,created_at FROM pay_notify_log WHERE order_no=? ORDER BY id`, orderNo)
	if err != nil {
		return nil, err
	}
	return scanPayLogs(rows)
}

// ---------- refund ----------
type refundRepo struct{ q execer }

const refundCols = `refund_no,order_no,amount,type,status,reason,cs_note,channel_refund_no,attempt,created_at`

func (r refundRepo) Insert(ctx context.Context, rf types.Refund) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO refund(`+refundCols+`) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		rf.RefundNo, rf.OrderNo, rf.Amount, rf.Type, rf.Status, rf.Reason, rf.CsNote, rf.ChannelRefundNo, rf.Attempt, rf.CreatedAt)
	return err
}

func scanRefund(row interface{ Scan(...any) error }) (types.Refund, error) {
	var rf types.Refund
	err := row.Scan(&rf.RefundNo, &rf.OrderNo, &rf.Amount, &rf.Type, &rf.Status, &rf.Reason,
		&rf.CsNote, &rf.ChannelRefundNo, &rf.Attempt, &rf.CreatedAt)
	return rf, err
}

func (r refundRepo) Get(ctx context.Context, refundNo string) (types.Refund, error) {
	rf, err := scanRefund(r.q.QueryRowContext(ctx, `SELECT `+refundCols+` FROM refund WHERE refund_no=?`, refundNo))
	if err != nil {
		err = notFound(err)
		if errors.Is(err, types.ErrNotFound) {
			err = types.ErrRefundNotFound
		}
	}
	return rf, err
}

func scanRefunds(rows *sql.Rows) ([]types.Refund, error) {
	defer rows.Close()
	var out []types.Refund
	for rows.Next() {
		rf, err := scanRefund(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rf)
	}
	return out, rows.Err()
}

func (r refundRepo) ListByOrder(ctx context.Context, orderNo string) ([]types.Refund, error) {
	rows, err := r.q.QueryContext(ctx, `SELECT `+refundCols+` FROM refund WHERE order_no=? ORDER BY created_at`, orderNo)
	if err != nil {
		return nil, err
	}
	return scanRefunds(rows)
}

func (r refundRepo) ListByStatus(ctx context.Context, statuses ...string) ([]types.Refund, error) {
	if len(statuses) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(statuses)), ",")
	args := make([]any, len(statuses))
	for i, s := range statuses {
		args[i] = s
	}
	rows, err := r.q.QueryContext(ctx,
		`SELECT `+refundCols+` FROM refund WHERE status IN (`+placeholders+`) ORDER BY created_at`, args...)
	if err != nil {
		return nil, err
	}
	return scanRefunds(rows)
}

func condUpdate(ctx context.Context, q execer, query string, args ...any) (bool, error) {
	res, err := q.ExecContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (r refundRepo) Approve(ctx context.Context, refundNo, note string) (bool, error) {
	return condUpdate(ctx, r.q,
		`UPDATE refund SET status=?, cs_note=?, attempt=attempt+1 WHERE refund_no=? AND status=?`,
		types.RefundRefunding, note, refundNo, types.RefundReview)
}

func (r refundRepo) Reject(ctx context.Context, refundNo, note string) (bool, error) {
	return condUpdate(ctx, r.q,
		`UPDATE refund SET status=?, cs_note=? WHERE refund_no=? AND status=?`,
		types.RefundRejected, note, refundNo, types.RefundReview)
}

func (r refundRepo) AttachChannel(ctx context.Context, refundNo, channelRefundNo string) error {
	_, err := r.q.ExecContext(ctx,
		`UPDATE refund SET channel_refund_no=? WHERE refund_no=?`, channelRefundNo, refundNo)
	return err
}

func (r refundRepo) MarkRefunding(ctx context.Context, refundNo string) (bool, error) {
	return condUpdate(ctx, r.q,
		`UPDATE refund SET status=?, attempt=attempt+1 WHERE refund_no=? AND status=?`,
		types.RefundRefunding, refundNo, types.RefundFailedPending)
}

func (r refundRepo) MarkFailedPending(ctx context.Context, refundNo string) (bool, error) {
	return condUpdate(ctx, r.q,
		`UPDATE refund SET status=? WHERE refund_no=? AND status=?`,
		types.RefundFailedPending, refundNo, types.RefundRefunding)
}

func (r refundRepo) MarkSuccess(ctx context.Context, refundNo string) (bool, error) {
	return condUpdate(ctx, r.q,
		`UPDATE refund SET status=? WHERE refund_no=? AND status IN (?,?)`,
		types.RefundSuccess, refundNo, types.RefundRefunding, types.RefundFailedPending)
}

func (r refundRepo) SumSuccess(ctx context.Context) (int64, error) {
	var sum sql.NullInt64
	err := r.q.QueryRowContext(ctx,
		`SELECT SUM(amount) FROM refund WHERE status=?`, types.RefundSuccess).Scan(&sum)
	return sum.Int64, err
}

// ---------- refund_notify_log ----------
type refundLogRepo struct{ q execer }

func (r refundLogRepo) Insert(ctx context.Context, l types.RefundNotifyLog) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO refund_notify_log(notify_id,refund_no,channel_refund_no,amount,verify_ok,result,raw) VALUES(?,?,?,?,?,?,?)`,
		l.NotifyID, l.RefundNo, l.ChannelRefundNo, l.Amount, l.VerifyOK, l.Result, l.Raw)
	return err
}

func (r refundLogRepo) ListByRefund(ctx context.Context, refundNo string) ([]types.RefundNotifyLog, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id,notify_id,refund_no,channel_refund_no,amount,verify_ok,result,raw,created_at FROM refund_notify_log WHERE refund_no=? ORDER BY id`, refundNo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.RefundNotifyLog
	for rows.Next() {
		var l types.RefundNotifyLog
		if err := rows.Scan(&l.ID, &l.NotifyID, &l.RefundNo, &l.ChannelRefundNo, &l.Amount, &l.VerifyOK, &l.Result, &l.Raw, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ---------- shipment ----------
type shipmentRepo struct{ q execer }

func (r shipmentRepo) Insert(ctx context.Context, s types.Shipment) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO shipment(order_no,carrier,tracking_no,operator) VALUES(?,?,?,?)`,
		s.OrderNo, s.Carrier, s.TrackingNo, s.Operator)
	if isDuplicate(err) {
		return types.ErrDuplicate
	}
	return err
}

func (r shipmentRepo) GetByOrder(ctx context.Context, orderNo string) (types.Shipment, error) {
	var s types.Shipment
	err := r.q.QueryRowContext(ctx,
		`SELECT id,order_no,carrier,tracking_no,operator,created_at FROM shipment WHERE order_no=?`, orderNo).
		Scan(&s.ID, &s.OrderNo, &s.Carrier, &s.TrackingNo, &s.Operator, &s.CreatedAt)
	return s, notFound(err)
}

func (r shipmentRepo) List(ctx context.Context) ([]types.Shipment, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id,order_no,carrier,tracking_no,operator,created_at FROM shipment ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.Shipment
	for rows.Next() {
		var s types.Shipment
		if err := rows.Scan(&s.ID, &s.OrderNo, &s.Carrier, &s.TrackingNo, &s.Operator, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ---------- exception_payment ----------
type exceptionRepo struct{ q execer }

func (r exceptionRepo) Insert(ctx context.Context, e types.ExceptionPayment) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO exception_payment(order_no,trade_no,amount,status) VALUES(?,?,?,?)`,
		e.OrderNo, e.TradeNo, e.Amount, e.Status)
	if isDuplicate(err) {
		return types.ErrDuplicate
	}
	return err
}

func scanExceptions(rows *sql.Rows) ([]types.ExceptionPayment, error) {
	defer rows.Close()
	var out []types.ExceptionPayment
	for rows.Next() {
		var e types.ExceptionPayment
		if err := rows.Scan(&e.ID, &e.OrderNo, &e.TradeNo, &e.Amount, &e.Status, &e.Handler, &e.HandledAt, &e.RefundNo, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r exceptionRepo) List(ctx context.Context, status string) ([]types.ExceptionPayment, error) {
	query := `SELECT id,order_no,trade_no,amount,status,handler,handled_at,refund_no,created_at FROM exception_payment`
	var args []any
	if status != "" {
		query += ` WHERE status=?`
		args = append(args, status)
	}
	query += ` ORDER BY id DESC`
	rows, err := r.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return scanExceptions(rows)
}

func (r exceptionRepo) ListByOrder(ctx context.Context, orderNo string) ([]types.ExceptionPayment, error) {
	rows, err := r.q.QueryContext(ctx,
		`SELECT id,order_no,trade_no,amount,status,handler,handled_at,refund_no,created_at FROM exception_payment WHERE order_no=? ORDER BY id`, orderNo)
	if err != nil {
		return nil, err
	}
	return scanExceptions(rows)
}

func (r exceptionRepo) AttachRefund(ctx context.Context, id int64, handler, refundNo string, at time.Time) (bool, error) {
	return condUpdate(ctx, r.q,
		`UPDATE exception_payment SET handler=?, refund_no=?, handled_at=?, status=? WHERE id=? AND status=?`,
		handler, refundNo, at, types.ExceptionRefunding, id, types.ExceptionPending)
}

func (r exceptionRepo) MarkRefundedByRefundNo(ctx context.Context, refundNo string) error {
	_, err := r.q.ExecContext(ctx,
		`UPDATE exception_payment SET status=? WHERE refund_no=?`, types.ExceptionRefunded, refundNo)
	return err
}

// ---------- user ----------
type userRepo struct{ q execer }

func (r userRepo) Insert(ctx context.Context, u types.User) error {
	_, err := r.q.ExecContext(ctx,
		`INSERT INTO user(id,username,password_hash,name,role) VALUES(?,?,?,?,?)`,
		u.ID, u.Username, u.PasswordHash, u.Name, u.Role)
	return err
}

func (r userRepo) GetByUsername(ctx context.Context, username string) (types.User, error) {
	var u types.User
	err := r.q.QueryRowContext(ctx,
		`SELECT id,username,password_hash,name,role FROM user WHERE username=?`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Name, &u.Role)
	return u, notFound(err)
}

func (r userRepo) GetByID(ctx context.Context, id string) (types.User, error) {
	var u types.User
	err := r.q.QueryRowContext(ctx,
		`SELECT id,username,password_hash,name,role FROM user WHERE id=?`, id).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Name, &u.Role)
	return u, notFound(err)
}

func (r userRepo) Count(ctx context.Context) (int, error) {
	var n int
	err := r.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM user`).Scan(&n)
	return n, err
}
