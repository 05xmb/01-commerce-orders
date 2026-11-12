package mysql

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"trade/internal/repo"
	"trade/internal/types"
)

var errDB = errors.New("db down")

var testTime = time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)

// newMock 返回基于 sqlmock 的仓储与断言工具
func newMock(t *testing.T) (repo.Repos, sqlmock.Sqlmock, *Store) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return reposWith(db), mock, NewStore(db)
}

func done(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// badRows 制造列数不符的行，触发 Scan 错误分支
func badRows(cols ...string) *sqlmock.Rows {
	vals := make([]driver.Value, len(cols))
	for i := range cols {
		vals[i] = "x"
	}
	return sqlmock.NewRows(cols).AddRow(vals...)
}

// ---------- Store / 事务 ----------

func TestWithinTx(t *testing.T) {
	ctx := context.Background()

	// 提交
	_, mock, store := newMock(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE product SET price=? WHERE id=?`).WithArgs(200, "P1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := store.WithinTx(ctx, func(r repo.Repos) error {
		return r.Products.SetPrice(ctx, "P1", 200)
	}); err != nil {
		t.Fatal(err)
	}
	done(t, mock)

	// 回滚
	_, mock, store = newMock(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE product SET price=? WHERE id=?`).WithArgs(300, "P1").
		WillReturnError(errDB)
	mock.ExpectRollback()
	if err := store.WithinTx(ctx, func(r repo.Repos) error {
		return r.Products.SetPrice(ctx, "P1", 300)
	}); !errors.Is(err, errDB) {
		t.Fatal("应传递错误并回滚")
	}
	done(t, mock)

	// 开启事务失败
	_, mock, store = newMock(t)
	mock.ExpectBegin().WillReturnError(errDB)
	if err := store.WithinTx(ctx, func(r repo.Repos) error { return nil }); !errors.Is(err, errDB) {
		t.Fatal("开启失败应返回错误")
	}
	done(t, mock)
}

func TestStore_Repos(t *testing.T) {
	r, _, store := newMock(t)
	if store.Repos().Products == nil || r.Orders == nil || r.Users == nil {
		t.Fatal("Repos 应包含全部子仓储")
	}
}

// ---------- product ----------

func TestProductRepo(t *testing.T) {
	ctx := context.Background()
	r, mock, _ := newMock(t)

	// Insert
	mock.ExpectExec(`INSERT INTO product(id,name,price,stock,on_sale) VALUES(?,?,?,?,?)`).
		WithArgs("P1", "A", int64(100), 5, true).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := r.Products.Insert(ctx, types.Product{ID: "P1", Name: "A", Price: 100, Stock: 5, OnSale: true}); err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec(`INSERT INTO product(id,name,price,stock,on_sale) VALUES(?,?,?,?,?)`).
		WithArgs("P1", "", int64(0), 0, false).WillReturnError(errDB)
	if err := r.Products.Insert(ctx, types.Product{ID: "P1"}); !errors.Is(err, errDB) {
		t.Fatal("应传递错误")
	}

	// Get：命中
	rows := sqlmock.NewRows([]string{"id", "name", "price", "stock", "on_sale"}).
		AddRow("P1", "A", 100, 5, true)
	mock.ExpectQuery(`SELECT id,name,price,stock,on_sale FROM product WHERE id=?`).WithArgs("P1").WillReturnRows(rows)
	p, err := r.Products.Get(ctx, "P1")
	if err != nil || p.Name != "A" {
		t.Fatal("Get 失败")
	}
	// Get：不存在 → ErrNotFound
	mock.ExpectQuery(`SELECT id,name,price,stock,on_sale FROM product WHERE id=?`).WithArgs("none").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "price", "stock", "on_sale"}))
	if _, err := r.Products.Get(ctx, "none"); !errors.Is(err, types.ErrNotFound) {
		t.Fatal("不存在应 ErrNotFound")
	}
	// Get：查询错误
	mock.ExpectQuery(`SELECT id,name,price,stock,on_sale FROM product WHERE id=?`).WithArgs("P1").WillReturnError(errDB)
	if _, err := r.Products.Get(ctx, "P1"); !errors.Is(err, errDB) {
		t.Fatal("应传递错误")
	}

	// List：全部与仅上架
	all := sqlmock.NewRows([]string{"id", "name", "price", "stock", "on_sale"}).
		AddRow("P1", "A", 100, 5, true).AddRow("P2", "B", 50, 10, false)
	mock.ExpectQuery(`SELECT id,name,price,stock,on_sale FROM product ORDER BY id`).WillReturnRows(all)
	list, err := r.Products.List(ctx, false)
	if err != nil || len(list) != 2 {
		t.Fatal("List 全部失败")
	}
	sale := sqlmock.NewRows([]string{"id", "name", "price", "stock", "on_sale"}).
		AddRow("P1", "A", 100, 5, true)
	mock.ExpectQuery(`SELECT id,name,price,stock,on_sale FROM product WHERE on_sale=1 ORDER BY id`).WillReturnRows(sale)
	list, err = r.Products.List(ctx, true)
	if err != nil || len(list) != 1 {
		t.Fatal("List 仅上架失败")
	}
	mock.ExpectQuery(`SELECT id,name,price,stock,on_sale FROM product ORDER BY id`).WillReturnError(errDB)
	if _, err := r.Products.List(ctx, false); !errors.Is(err, errDB) {
		t.Fatal("查询错误应传递")
	}
	// List：Scan 错误
	mock.ExpectQuery(`SELECT id,name,price,stock,on_sale FROM product ORDER BY id`).
		WillReturnRows(badRows("id"))
	if _, err := r.Products.List(ctx, false); err == nil {
		t.Fatal("Scan 错误应传递")
	}

	// TryDeduct：成功 / 无行 / 错误
	mock.ExpectExec(`UPDATE product SET stock=stock-? WHERE id=? AND on_sale=1 AND stock>=?`).
		WithArgs(1, "P1", 1).WillReturnResult(sqlmock.NewResult(0, 1))
	ok, err := r.Products.TryDeduct(ctx, "P1", 1)
	if !ok || err != nil {
		t.Fatal("扣减应成功")
	}
	mock.ExpectExec(`UPDATE product SET stock=stock-? WHERE id=? AND on_sale=1 AND stock>=?`).
		WithArgs(9, "P1", 9).WillReturnResult(sqlmock.NewResult(0, 0))
	ok, err = r.Products.TryDeduct(ctx, "P1", 9)
	if ok || err != nil {
		t.Fatal("库存不足应 false")
	}
	mock.ExpectExec(`UPDATE product SET stock=stock-? WHERE id=? AND on_sale=1 AND stock>=?`).
		WithArgs(1, "P1", 1).WillReturnError(errDB)
	if _, err := r.Products.TryDeduct(ctx, "P1", 1); !errors.Is(err, errDB) {
		t.Fatal("应传递错误")
	}

	// AddStock / SetPrice / SetOnSale
	mock.ExpectExec(`UPDATE product SET stock=stock+? WHERE id=?`).WithArgs(2, "P1").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := r.Products.AddStock(ctx, "P1", 2); err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec(`UPDATE product SET price=? WHERE id=?`).WithArgs(int64(200), "P1").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := r.Products.SetPrice(ctx, "P1", 200); err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec(`UPDATE product SET on_sale=? WHERE id=?`).WithArgs(false, "P1").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := r.Products.SetOnSale(ctx, "P1", false); err != nil {
		t.Fatal(err)
	}
	done(t, mock)
}

// ---------- stock_flow ----------

func TestStockFlowRepo(t *testing.T) {
	ctx := context.Background()
	r, mock, _ := newMock(t)

	mock.ExpectExec(`INSERT INTO stock_flow(product_id,order_no,change_type,qty,stock_after) VALUES(?,?,?,?,?)`).
		WithArgs("P1", "O1", "occupy", 1, 4).WillReturnResult(sqlmock.NewResult(1, 1))
	if err := r.StockFlows.Insert(ctx, types.StockFlow{ProductID: "P1", OrderNo: "O1", ChangeType: "occupy", Qty: 1, StockAfter: 4}); err != nil {
		t.Fatal(err)
	}

	cols := []string{"id", "product_id", "order_no", "change_type", "qty", "stock_after", "created_at"}
	rows := sqlmock.NewRows(cols).AddRow(1, "P1", "O1", "occupy", 1, 4, testTime)
	mock.ExpectQuery(`SELECT id,product_id,order_no,change_type,qty,stock_after,created_at FROM stock_flow WHERE product_id=? ORDER BY id`).
		WithArgs("P1").WillReturnRows(rows)
	list, err := r.StockFlows.ListByProduct(ctx, "P1")
	if err != nil || len(list) != 1 || list[0].StockAfter != 4 {
		t.Fatal("ListByProduct 失败")
	}
	mock.ExpectQuery(`SELECT id,product_id,order_no,change_type,qty,stock_after,created_at FROM stock_flow WHERE order_no=? ORDER BY id`).
		WithArgs("O1").WillReturnRows(sqlmock.NewRows(cols).AddRow(1, "P1", "O1", "occupy", 1, 4, testTime))
	list, err = r.StockFlows.ListByOrder(ctx, "O1")
	if err != nil || len(list) != 1 {
		t.Fatal("ListByOrder 失败")
	}
	mock.ExpectQuery(`SELECT id,product_id,order_no,change_type,qty,stock_after,created_at FROM stock_flow WHERE order_no=? ORDER BY id`).
		WithArgs("O1").WillReturnError(errDB)
	if _, err := r.StockFlows.ListByOrder(ctx, "O1"); !errors.Is(err, errDB) {
		t.Fatal("应传递错误")
	}
	mock.ExpectQuery(`SELECT id,product_id,order_no,change_type,qty,stock_after,created_at FROM stock_flow WHERE product_id=? ORDER BY id`).
		WithArgs("P1").WillReturnRows(badRows("id"))
	if _, err := r.StockFlows.ListByProduct(ctx, "P1"); err == nil {
		t.Fatal("Scan 错误应传递")
	}

	mock.ExpectQuery(`SELECT COUNT(*) FROM stock_flow WHERE order_no=? AND change_type=?`).
		WithArgs("O1", "restore").WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(1))
	n, err := r.StockFlows.CountByOrderType(ctx, "O1", "restore")
	if err != nil || n != 1 {
		t.Fatalf("应为 1，实际 %d", n)
	}
	done(t, mock)
}

// ---------- orders ----------

var orderColList = []string{"order_no", "buyer_id", "product_id", "product_name", "price", "qty", "amount", "status", "created_at", "pay_deadline", "paid_at", "closed_at", "close_reason"}

func orderRow() *sqlmock.Rows {
	return sqlmock.NewRows(orderColList).
		AddRow("O1", "b1", "P1", "A", 100, 1, 100, "pending", testTime, testTime, nil, nil, nil)
}

func TestOrderRepo(t *testing.T) {
	ctx := context.Background()
	r, mock, _ := newMock(t)
	orderCols := `order_no,buyer_id,product_id,product_name,price,qty,amount,status,created_at,pay_deadline,paid_at,closed_at,close_reason`

	// Insert
	mock.ExpectExec(`INSERT INTO orders(`+orderCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`).
		WithArgs("O1", "b1", "P1", "A", int64(100), 1, int64(100), "pending", testTime, testTime, nil, nil, nil).
		WillReturnResult(sqlmock.NewResult(0, 1))
	o := types.Order{OrderNo: "O1", BuyerID: "b1", ProductID: "P1", ProductName: "A", Price: 100, Qty: 1, Amount: 100, Status: "pending", CreatedAt: testTime, PayDeadline: testTime}
	if err := r.Orders.Insert(ctx, o); err != nil {
		t.Fatal(err)
	}

	// Get
	mock.ExpectQuery(`SELECT ` + orderCols + ` FROM orders WHERE order_no=?`).WithArgs("O1").WillReturnRows(orderRow())
	got, err := r.Orders.Get(ctx, "O1")
	if err != nil || got.OrderNo != "O1" {
		t.Fatal("Get 失败")
	}
	mock.ExpectQuery(`SELECT ` + orderCols + ` FROM orders WHERE order_no=?`).WithArgs("none").
		WillReturnRows(sqlmock.NewRows(orderColList))
	if _, err := r.Orders.Get(ctx, "none"); !errors.Is(err, types.ErrNotFound) {
		t.Fatal("不存在应 ErrNotFound")
	}
	mock.ExpectQuery(`SELECT ` + orderCols + ` FROM orders WHERE order_no=?`).WithArgs("O1").WillReturnError(errDB)
	if _, err := r.Orders.Get(ctx, "O1"); !errors.Is(err, errDB) {
		t.Fatal("应传递错误")
	}

	// ListByBuyer
	mock.ExpectQuery(`SELECT ` + orderCols + ` FROM orders WHERE buyer_id=? ORDER BY created_at DESC`).WithArgs("b1").WillReturnRows(orderRow())
	list, err := r.Orders.ListByBuyer(ctx, "b1")
	if err != nil || len(list) != 1 {
		t.Fatal("ListByBuyer 失败")
	}
	mock.ExpectQuery(`SELECT ` + orderCols + ` FROM orders WHERE buyer_id=? ORDER BY created_at DESC`).WithArgs("b1").
		WillReturnRows(badRows("order_no"))
	if _, err := r.Orders.ListByBuyer(ctx, "b1"); err == nil {
		t.Fatal("Scan 错误应传递")
	}

	// ListByStatus：空状态直接返回
	list, err = r.Orders.ListByStatus(ctx)
	if err != nil || list != nil {
		t.Fatal("空状态应返回 nil")
	}
	mock.ExpectQuery(`SELECT `+orderCols+` FROM orders WHERE status IN (?,?) ORDER BY created_at`).
		WithArgs("pending", "to_ship").WillReturnRows(orderRow())
	list, err = r.Orders.ListByStatus(ctx, "pending", "to_ship")
	if err != nil || len(list) != 1 {
		t.Fatal("ListByStatus 失败")
	}
	mock.ExpectQuery(`SELECT ` + orderCols + ` FROM orders WHERE status IN (?) ORDER BY created_at`).WithArgs("pending").WillReturnError(errDB)
	if _, err := r.Orders.ListByStatus(ctx, "pending"); !errors.Is(err, errDB) {
		t.Fatal("应传递错误")
	}

	// ListExpiredPending
	mock.ExpectQuery(`SELECT `+orderCols+` FROM orders WHERE status=? AND pay_deadline<=?`).
		WithArgs("pending", testTime).WillReturnRows(orderRow())
	list, err = r.Orders.ListExpiredPending(ctx, testTime)
	if err != nil || len(list) != 1 {
		t.Fatal("ListExpiredPending 失败")
	}
	mock.ExpectQuery(`SELECT `+orderCols+` FROM orders WHERE status=? AND pay_deadline<=?`).
		WithArgs("pending", testTime).WillReturnError(errDB)
	if _, err := r.Orders.ListExpiredPending(ctx, testTime); !errors.Is(err, errDB) {
		t.Fatal("应传递错误")
	}

	// SetStatusCond：1/0/错误
	mock.ExpectExec(`UPDATE orders SET status=? WHERE order_no=? AND status=?`).
		WithArgs("to_ship", "O1", "pending").WillReturnResult(sqlmock.NewResult(0, 1))
	ok, err := r.Orders.SetStatusCond(ctx, "O1", "pending", "to_ship")
	if !ok || err != nil {
		t.Fatal("应流转成功")
	}
	mock.ExpectExec(`UPDATE orders SET status=? WHERE order_no=? AND status=?`).
		WithArgs("to_ship", "O1", "pending").WillReturnResult(sqlmock.NewResult(0, 0))
	ok, err = r.Orders.SetStatusCond(ctx, "O1", "pending", "to_ship")
	if ok || err != nil {
		t.Fatal("无匹配行应 false")
	}
	mock.ExpectExec(`UPDATE orders SET status=? WHERE order_no=? AND status=?`).
		WithArgs("to_ship", "O1", "pending").WillReturnError(errDB)
	if _, err := r.Orders.SetStatusCond(ctx, "O1", "pending", "to_ship"); !errors.Is(err, errDB) {
		t.Fatal("应传递错误")
	}

	// MarkPaid
	mock.ExpectExec(`UPDATE orders SET status=?, paid_at=? WHERE order_no=? AND status=?`).
		WithArgs("to_ship", testTime, "O1", "pending").WillReturnResult(sqlmock.NewResult(0, 1))
	ok, err = r.Orders.MarkPaid(ctx, "O1", testTime)
	if !ok || err != nil {
		t.Fatal("MarkPaid 应成功")
	}
	mock.ExpectExec(`UPDATE orders SET status=?, paid_at=? WHERE order_no=? AND status=?`).
		WithArgs("to_ship", testTime, "O1", "pending").WillReturnError(errDB)
	if _, err := r.Orders.MarkPaid(ctx, "O1", testTime); !errors.Is(err, errDB) {
		t.Fatal("应传递错误")
	}

	// ClosePending
	mock.ExpectExec(`UPDATE orders SET status=?, closed_at=?, close_reason=? WHERE order_no=? AND status=?`).
		WithArgs("closed", testTime, "超时关闭", "O1", "pending").WillReturnResult(sqlmock.NewResult(0, 1))
	ok, err = r.Orders.ClosePending(ctx, "O1", testTime, "超时关闭")
	if !ok || err != nil {
		t.Fatal("ClosePending 应成功")
	}
	mock.ExpectExec(`UPDATE orders SET status=?, closed_at=?, close_reason=? WHERE order_no=? AND status=?`).
		WithArgs("closed", testTime, "超时关闭", "O1", "pending").WillReturnError(errDB)
	if _, err := r.Orders.ClosePending(ctx, "O1", testTime, "超时关闭"); !errors.Is(err, errDB) {
		t.Fatal("应传递错误")
	}
	done(t, mock)
}

// ---------- payment ----------

func TestPaymentRepo(t *testing.T) {
	ctx := context.Background()
	r, mock, _ := newMock(t)

	// Insert：成功 / 重复 / 其他错误
	mock.ExpectExec(`INSERT INTO payment(order_no,trade_no,amount,status) VALUES(?,?,?,?)`).
		WithArgs("O1", "T1", int64(100), "success").WillReturnResult(sqlmock.NewResult(1, 1))
	if err := r.Payments.Insert(ctx, types.Payment{OrderNo: "O1", TradeNo: "T1", Amount: 100, Status: "success"}); err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec(`INSERT INTO payment(order_no,trade_no,amount,status) VALUES(?,?,?,?)`).
		WithArgs("O2", "T1", int64(100), "success").WillReturnError(errors.New("Error 1062: Duplicate entry 'T1' for key 'trade_no'"))
	if err := r.Payments.Insert(ctx, types.Payment{OrderNo: "O2", TradeNo: "T1", Amount: 100, Status: "success"}); !errors.Is(err, types.ErrDuplicate) {
		t.Fatal("重复键应映射 ErrDuplicate")
	}
	mock.ExpectExec(`INSERT INTO payment(order_no,trade_no,amount,status) VALUES(?,?,?,?)`).
		WithArgs("O2", "T2", int64(100), "success").WillReturnError(errDB)
	if err := r.Payments.Insert(ctx, types.Payment{OrderNo: "O2", TradeNo: "T2", Amount: 100, Status: "success"}); !errors.Is(err, errDB) {
		t.Fatal("应传递错误")
	}

	payCols := []string{"id", "order_no", "trade_no", "amount", "status", "created_at"}
	// GetByTradeNo
	mock.ExpectQuery(`SELECT id,order_no,trade_no,amount,status,created_at FROM payment WHERE trade_no=?`).
		WithArgs("T1").WillReturnRows(sqlmock.NewRows(payCols).AddRow(1, "O1", "T1", 100, "success", testTime))
	p, err := r.Payments.GetByTradeNo(ctx, "T1")
	if err != nil || p.OrderNo != "O1" {
		t.Fatal("GetByTradeNo 失败")
	}
	mock.ExpectQuery(`SELECT id,order_no,trade_no,amount,status,created_at FROM payment WHERE trade_no=?`).
		WithArgs("none").WillReturnRows(sqlmock.NewRows(payCols))
	if _, err := r.Payments.GetByTradeNo(ctx, "none"); !errors.Is(err, types.ErrNotFound) {
		t.Fatal("不存在应 ErrNotFound")
	}
	// GetByOrder
	mock.ExpectQuery(`SELECT id,order_no,trade_no,amount,status,created_at FROM payment WHERE order_no=?`).
		WithArgs("O1").WillReturnRows(sqlmock.NewRows(payCols).AddRow(1, "O1", "T1", 100, "success", testTime))
	p, err = r.Payments.GetByOrder(ctx, "O1")
	if err != nil || p.TradeNo != "T1" {
		t.Fatal("GetByOrder 失败")
	}
	mock.ExpectQuery(`SELECT id,order_no,trade_no,amount,status,created_at FROM payment WHERE order_no=?`).
		WithArgs("O1").WillReturnError(errDB)
	if _, err := r.Payments.GetByOrder(ctx, "O1"); !errors.Is(err, errDB) {
		t.Fatal("应传递错误")
	}

	// SumAmount
	mock.ExpectQuery(`SELECT SUM(amount) FROM payment WHERE status='success'`).
		WillReturnRows(sqlmock.NewRows([]string{"sum"}).AddRow(1000))
	sum, err := r.Payments.SumAmount(ctx)
	if err != nil || sum != 1000 {
		t.Fatalf("应为 1000，实际 %d", sum)
	}
	mock.ExpectQuery(`SELECT SUM(amount) FROM payment WHERE status='success'`).WillReturnError(errDB)
	if _, err := r.Payments.SumAmount(ctx); !errors.Is(err, errDB) {
		t.Fatal("应传递错误")
	}
	done(t, mock)
}

// ---------- pay_notify_log ----------

func TestPayLogRepo(t *testing.T) {
	ctx := context.Background()
	r, mock, _ := newMock(t)

	mock.ExpectExec(`INSERT INTO pay_notify_log(notify_id,order_no,trade_no,amount,verify_ok,result,raw) VALUES(?,?,?,?,?,?,?)`).
		WithArgs("N1", "O1", "T1", int64(100), true, "success", "{}").WillReturnResult(sqlmock.NewResult(1, 1))
	if err := r.PayLogs.Insert(ctx, types.PayNotifyLog{NotifyID: "N1", OrderNo: "O1", TradeNo: "T1", Amount: 100, VerifyOK: true, Result: "success", Raw: "{}"}); err != nil {
		t.Fatal(err)
	}

	logCols := []string{"id", "notify_id", "order_no", "trade_no", "amount", "verify_ok", "result", "raw", "created_at"}
	// List：显式 limit
	mock.ExpectQuery(`SELECT id,notify_id,order_no,trade_no,amount,verify_ok,result,raw,created_at FROM pay_notify_log ORDER BY id DESC LIMIT ?`).
		WithArgs(10).WillReturnRows(sqlmock.NewRows(logCols).AddRow(1, "N1", "O1", "T1", 100, true, "success", "{}", testTime))
	list, err := r.PayLogs.List(ctx, 10)
	if err != nil || len(list) != 1 {
		t.Fatal("List 失败")
	}
	// List：默认 limit 200
	mock.ExpectQuery(`SELECT id,notify_id,order_no,trade_no,amount,verify_ok,result,raw,created_at FROM pay_notify_log ORDER BY id DESC LIMIT ?`).
		WithArgs(200).WillReturnRows(sqlmock.NewRows(logCols))
	list, err = r.PayLogs.List(ctx, 0)
	if err != nil || len(list) != 0 {
		t.Fatal("默认 limit 应生效")
	}
	mock.ExpectQuery(`SELECT id,notify_id,order_no,trade_no,amount,verify_ok,result,raw,created_at FROM pay_notify_log ORDER BY id DESC LIMIT ?`).
		WithArgs(200).WillReturnRows(badRows("id"))
	if _, err := r.PayLogs.List(ctx, 0); err == nil {
		t.Fatal("Scan 错误应传递")
	}

	// ListByOrder
	mock.ExpectQuery(`SELECT id,notify_id,order_no,trade_no,amount,verify_ok,result,raw,created_at FROM pay_notify_log WHERE order_no=? ORDER BY id`).
		WithArgs("O1").WillReturnRows(sqlmock.NewRows(logCols).AddRow(1, "N1", "O1", "T1", 100, true, "success", "{}", testTime))
	list, err = r.PayLogs.ListByOrder(ctx, "O1")
	if err != nil || len(list) != 1 {
		t.Fatal("ListByOrder 失败")
	}
	mock.ExpectQuery(`SELECT id,notify_id,order_no,trade_no,amount,verify_ok,result,raw,created_at FROM pay_notify_log WHERE order_no=? ORDER BY id`).
		WithArgs("O1").WillReturnError(errDB)
	if _, err := r.PayLogs.ListByOrder(ctx, "O1"); !errors.Is(err, errDB) {
		t.Fatal("应传递错误")
	}
	done(t, mock)
}

// ---------- refund ----------

var refundColList = []string{"refund_no", "order_no", "amount", "type", "status", "reason", "cs_id", "cs_note", "channel_refund_no", "attempt", "created_at"}

func TestRefundRepo(t *testing.T) {
	ctx := context.Background()
	r, mock, _ := newMock(t)
	refundCols := `refund_no,order_no,amount,type,status,reason,cs_id,cs_note,channel_refund_no,attempt,created_at`

	// Insert
	mock.ExpectExec(`INSERT INTO refund(`+refundCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?)`).
		WithArgs("R1", "O1", int64(100), "presale", "review", "不想要了", "", "", "", 0, testTime).
		WillReturnResult(sqlmock.NewResult(0, 1))
	rf := types.Refund{RefundNo: "R1", OrderNo: "O1", Amount: 100, Type: "presale", Status: "review", Reason: "不想要了", CreatedAt: testTime}
	if err := r.Refunds.Insert(ctx, rf); err != nil {
		t.Fatal(err)
	}

	// Get：命中 / 不存在→ErrRefundNotFound / 错误
	mock.ExpectQuery(`SELECT ` + refundCols + ` FROM refund WHERE refund_no=?`).WithArgs("R1").
		WillReturnRows(sqlmock.NewRows(refundColList).AddRow("R1", "O1", 100, "presale", "review", "不想要了", "", "", "", 0, testTime))
	got, err := r.Refunds.Get(ctx, "R1")
	if err != nil || got.RefundNo != "R1" {
		t.Fatal("Get 失败")
	}
	mock.ExpectQuery(`SELECT ` + refundCols + ` FROM refund WHERE refund_no=?`).WithArgs("none").
		WillReturnRows(sqlmock.NewRows(refundColList))
	if _, err := r.Refunds.Get(ctx, "none"); !errors.Is(err, types.ErrRefundNotFound) {
		t.Fatal("不存在应 ErrRefundNotFound")
	}
	mock.ExpectQuery(`SELECT ` + refundCols + ` FROM refund WHERE refund_no=?`).WithArgs("R1").WillReturnError(errDB)
	if _, err := r.Refunds.Get(ctx, "R1"); !errors.Is(err, errDB) {
		t.Fatal("应传递错误")
	}

	// ListByOrder
	mock.ExpectQuery(`SELECT ` + refundCols + ` FROM refund WHERE order_no=? ORDER BY created_at`).WithArgs("O1").
		WillReturnRows(sqlmock.NewRows(refundColList).AddRow("R1", "O1", 100, "presale", "review", "x", "cs1", "", "", 0, testTime))
	list, err := r.Refunds.ListByOrder(ctx, "O1")
	if err != nil || len(list) != 1 {
		t.Fatal("ListByOrder 失败")
	}
	mock.ExpectQuery(`SELECT ` + refundCols + ` FROM refund WHERE order_no=? ORDER BY created_at`).WithArgs("O1").WillReturnRows(badRows("refund_no"))
	if _, err := r.Refunds.ListByOrder(ctx, "O1"); err == nil {
		t.Fatal("Scan 错误应传递")
	}

	// ListByStatus：空状态与多状态
	list, err = r.Refunds.ListByStatus(ctx)
	if err != nil || list != nil {
		t.Fatal("空状态应返回 nil")
	}
	mock.ExpectQuery(`SELECT ` + refundCols + ` FROM refund WHERE status IN (?) ORDER BY created_at`).WithArgs("review").
		WillReturnRows(sqlmock.NewRows(refundColList).AddRow("R1", "O1", 100, "presale", "review", "x", "cs1", "", "", 0, testTime))
	list, err = r.Refunds.ListByStatus(ctx, "review")
	if err != nil || len(list) != 1 {
		t.Fatal("ListByStatus 失败")
	}
	mock.ExpectQuery(`SELECT ` + refundCols + ` FROM refund WHERE status IN (?) ORDER BY created_at`).WithArgs("review").WillReturnError(errDB)
	if _, err := r.Refunds.ListByStatus(ctx, "review"); !errors.Is(err, errDB) {
		t.Fatal("应传递错误")
	}

	// Approve / Reject（记录审核客服 cs_id）
	mock.ExpectExec(`UPDATE refund SET status=?, cs_id=?, cs_note=?, attempt=attempt+1 WHERE refund_no=? AND status=?`).
		WithArgs("refunding", "cs1", "同意", "R1", "review").WillReturnResult(sqlmock.NewResult(0, 1))
	ok, err := r.Refunds.Approve(ctx, "R1", "cs1", "同意")
	if !ok || err != nil {
		t.Fatal("Approve 应成功")
	}
	mock.ExpectExec(`UPDATE refund SET status=?, cs_id=?, cs_note=? WHERE refund_no=? AND status=?`).
		WithArgs("rejected", "cs1", "拒绝", "R1", "review").WillReturnResult(sqlmock.NewResult(0, 0))
	ok, err = r.Refunds.Reject(ctx, "R1", "cs1", "拒绝")
	if ok || err != nil {
		t.Fatal("无匹配应 false")
	}

	// AttachChannel
	mock.ExpectExec(`UPDATE refund SET channel_refund_no=? WHERE refund_no=?`).WithArgs("CR1", "R1").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := r.Refunds.AttachChannel(ctx, "R1", "CR1"); err != nil {
		t.Fatal(err)
	}

	// MarkRefunding / MarkFailedPending / MarkSuccess
	mock.ExpectExec(`UPDATE refund SET status=?, attempt=attempt+1 WHERE refund_no=? AND status IN (?,?)`).
		WithArgs("refunding", "R1", "failed_pending", "refunding").WillReturnResult(sqlmock.NewResult(0, 1))
	ok, err = r.Refunds.MarkRefunding(ctx, "R1")
	if !ok || err != nil {
		t.Fatal("MarkRefunding 应成功")
	}
	mock.ExpectExec(`UPDATE refund SET status=? WHERE refund_no=? AND status=?`).
		WithArgs("failed_pending", "R1", "refunding").WillReturnResult(sqlmock.NewResult(0, 1))
	ok, err = r.Refunds.MarkFailedPending(ctx, "R1")
	if !ok || err != nil {
		t.Fatal("MarkFailedPending 应成功")
	}
	mock.ExpectExec(`UPDATE refund SET status=? WHERE refund_no=? AND status IN (?,?)`).
		WithArgs("success", "R1", "refunding", "failed_pending").WillReturnResult(sqlmock.NewResult(0, 1))
	ok, err = r.Refunds.MarkSuccess(ctx, "R1")
	if !ok || err != nil {
		t.Fatal("MarkSuccess 应成功")
	}

	// SumSuccess
	mock.ExpectQuery(`SELECT SUM(amount) FROM refund WHERE status=?`).WithArgs("success").
		WillReturnRows(sqlmock.NewRows([]string{"sum"}).AddRow(500))
	sum, err := r.Refunds.SumSuccess(ctx)
	if err != nil || sum != 500 {
		t.Fatalf("应为 500，实际 %d", sum)
	}
	done(t, mock)
}

// ---------- refund_notify_log ----------

func TestRefundLogRepo(t *testing.T) {
	ctx := context.Background()
	r, mock, _ := newMock(t)

	mock.ExpectExec(`INSERT INTO refund_notify_log(notify_id,refund_no,channel_refund_no,amount,verify_ok,result,raw) VALUES(?,?,?,?,?,?,?)`).
		WithArgs("RN1", "R1", "CR1", int64(100), true, "success", "{}").WillReturnResult(sqlmock.NewResult(1, 1))
	if err := r.RefundLogs.Insert(ctx, types.RefundNotifyLog{NotifyID: "RN1", RefundNo: "R1", ChannelRefundNo: "CR1", Amount: 100, VerifyOK: true, Result: "success", Raw: "{}"}); err != nil {
		t.Fatal(err)
	}

	logCols := []string{"id", "notify_id", "refund_no", "channel_refund_no", "amount", "verify_ok", "result", "raw", "created_at"}
	mock.ExpectQuery(`SELECT id,notify_id,refund_no,channel_refund_no,amount,verify_ok,result,raw,created_at FROM refund_notify_log WHERE refund_no=? ORDER BY id`).
		WithArgs("R1").WillReturnRows(sqlmock.NewRows(logCols).AddRow(1, "RN1", "R1", "CR1", 100, true, "success", "{}", testTime))
	list, err := r.RefundLogs.ListByRefund(ctx, "R1")
	if err != nil || len(list) != 1 {
		t.Fatal("ListByRefund 失败")
	}
	mock.ExpectQuery(`SELECT id,notify_id,refund_no,channel_refund_no,amount,verify_ok,result,raw,created_at FROM refund_notify_log WHERE refund_no=? ORDER BY id`).
		WithArgs("R1").WillReturnError(errDB)
	if _, err := r.RefundLogs.ListByRefund(ctx, "R1"); !errors.Is(err, errDB) {
		t.Fatal("应传递错误")
	}
	mock.ExpectQuery(`SELECT id,notify_id,refund_no,channel_refund_no,amount,verify_ok,result,raw,created_at FROM refund_notify_log WHERE refund_no=? ORDER BY id`).
		WithArgs("R1").WillReturnRows(badRows("id"))
	if _, err := r.RefundLogs.ListByRefund(ctx, "R1"); err == nil {
		t.Fatal("Scan 错误应传递")
	}
	done(t, mock)
}

// ---------- shipment ----------

func TestShipmentRepo(t *testing.T) {
	ctx := context.Background()
	r, mock, _ := newMock(t)

	mock.ExpectExec(`INSERT INTO shipment(order_no,carrier,tracking_no,operator) VALUES(?,?,?,?)`).
		WithArgs("O1", "顺丰", "SF123", "wh1").WillReturnResult(sqlmock.NewResult(1, 1))
	if err := r.Shipments.Insert(ctx, types.Shipment{OrderNo: "O1", Carrier: "顺丰", TrackingNo: "SF123", Operator: "wh1"}); err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec(`INSERT INTO shipment(order_no,carrier,tracking_no,operator) VALUES(?,?,?,?)`).
		WithArgs("O1", "顺丰", "SF456", "wh1").WillReturnError(errors.New("Error 1062: Duplicate entry 'O1' for key 'order_no'"))
	if err := r.Shipments.Insert(ctx, types.Shipment{OrderNo: "O1", Carrier: "顺丰", TrackingNo: "SF456", Operator: "wh1"}); !errors.Is(err, types.ErrDuplicate) {
		t.Fatal("一单多发货应映射 ErrDuplicate")
	}

	shipCols := []string{"id", "order_no", "carrier", "tracking_no", "operator", "created_at"}
	mock.ExpectQuery(`SELECT id,order_no,carrier,tracking_no,operator,created_at FROM shipment WHERE order_no=?`).
		WithArgs("O1").WillReturnRows(sqlmock.NewRows(shipCols).AddRow(1, "O1", "顺丰", "SF123", "wh1", testTime))
	sh, err := r.Shipments.GetByOrder(ctx, "O1")
	if err != nil || sh.TrackingNo != "SF123" {
		t.Fatal("GetByOrder 失败")
	}
	mock.ExpectQuery(`SELECT id,order_no,carrier,tracking_no,operator,created_at FROM shipment WHERE order_no=?`).
		WithArgs("none").WillReturnRows(sqlmock.NewRows(shipCols))
	if _, err := r.Shipments.GetByOrder(ctx, "none"); !errors.Is(err, types.ErrNotFound) {
		t.Fatal("不存在应 ErrNotFound")
	}

	mock.ExpectQuery(`SELECT id,order_no,carrier,tracking_no,operator,created_at FROM shipment ORDER BY id`).
		WillReturnRows(sqlmock.NewRows(shipCols).AddRow(1, "O1", "顺丰", "SF123", "wh1", testTime))
	list, err := r.Shipments.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatal("List 失败")
	}
	mock.ExpectQuery(`SELECT id,order_no,carrier,tracking_no,operator,created_at FROM shipment ORDER BY id`).WillReturnError(errDB)
	if _, err := r.Shipments.List(ctx); !errors.Is(err, errDB) {
		t.Fatal("应传递错误")
	}
	mock.ExpectQuery(`SELECT id,order_no,carrier,tracking_no,operator,created_at FROM shipment ORDER BY id`).WillReturnRows(badRows("id"))
	if _, err := r.Shipments.List(ctx); err == nil {
		t.Fatal("Scan 错误应传递")
	}
	done(t, mock)
}

// ---------- exception_payment ----------

func TestExceptionRepo(t *testing.T) {
	ctx := context.Background()
	r, mock, _ := newMock(t)

	mock.ExpectExec(`INSERT INTO exception_payment(order_no,trade_no,amount,status) VALUES(?,?,?,?)`).
		WithArgs("O1", "T1", int64(100), "pending").WillReturnResult(sqlmock.NewResult(1, 1))
	if err := r.Exceptions.Insert(ctx, types.ExceptionPayment{OrderNo: "O1", TradeNo: "T1", Amount: 100, Status: "pending"}); err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec(`INSERT INTO exception_payment(order_no,trade_no,amount,status) VALUES(?,?,?,?)`).
		WithArgs("O1", "T1", int64(100), "pending").WillReturnError(errors.New("Error 1062: Duplicate entry 'T1' for key 'trade_no'"))
	if err := r.Exceptions.Insert(ctx, types.ExceptionPayment{OrderNo: "O1", TradeNo: "T1", Amount: 100, Status: "pending"}); !errors.Is(err, types.ErrDuplicate) {
		t.Fatal("交易号重复应映射 ErrDuplicate（幂等）")
	}

	exCols := []string{"id", "order_no", "trade_no", "amount", "status", "handler", "handled_at", "refund_no", "created_at"}
	// List：不带过滤
	mock.ExpectQuery(`SELECT id,order_no,trade_no,amount,status,handler,handled_at,refund_no,created_at FROM exception_payment ORDER BY id DESC`).
		WillReturnRows(sqlmock.NewRows(exCols).AddRow(1, "O1", "T1", 100, "pending", nil, nil, nil, testTime))
	list, err := r.Exceptions.List(ctx, "")
	if err != nil || len(list) != 1 {
		t.Fatal("List 失败")
	}
	// List：按状态
	mock.ExpectQuery(`SELECT id,order_no,trade_no,amount,status,handler,handled_at,refund_no,created_at FROM exception_payment WHERE status=? ORDER BY id DESC`).
		WithArgs("pending").WillReturnRows(sqlmock.NewRows(exCols))
	list, err = r.Exceptions.List(ctx, "pending")
	if err != nil || len(list) != 0 {
		t.Fatal("按状态过滤失败")
	}
	mock.ExpectQuery(`SELECT id,order_no,trade_no,amount,status,handler,handled_at,refund_no,created_at FROM exception_payment ORDER BY id DESC`).WillReturnError(errDB)
	if _, err := r.Exceptions.List(ctx, ""); !errors.Is(err, errDB) {
		t.Fatal("应传递错误")
	}
	mock.ExpectQuery(`SELECT id,order_no,trade_no,amount,status,handler,handled_at,refund_no,created_at FROM exception_payment ORDER BY id DESC`).WillReturnRows(badRows("id"))
	if _, err := r.Exceptions.List(ctx, ""); err == nil {
		t.Fatal("Scan 错误应传递")
	}

	// ListByOrder
	mock.ExpectQuery(`SELECT id,order_no,trade_no,amount,status,handler,handled_at,refund_no,created_at FROM exception_payment WHERE order_no=? ORDER BY id`).
		WithArgs("O1").WillReturnRows(sqlmock.NewRows(exCols).AddRow(1, "O1", "T1", 100, "pending", nil, nil, nil, testTime))
	list, err = r.Exceptions.ListByOrder(ctx, "O1")
	if err != nil || len(list) != 1 {
		t.Fatal("ListByOrder 失败")
	}

	// AttachRefund：pending → refunding（防重复发起）
	mock.ExpectExec(`UPDATE exception_payment SET handler=?, refund_no=?, handled_at=?, status=? WHERE id=? AND status=?`).
		WithArgs("cs1", "R1", testTime, "refunding", int64(1), "pending").WillReturnResult(sqlmock.NewResult(0, 1))
	ok, err := r.Exceptions.AttachRefund(ctx, 1, "cs1", "R1", testTime)
	if !ok || err != nil {
		t.Fatal("AttachRefund 应成功")
	}
	mock.ExpectExec(`UPDATE exception_payment SET handler=?, refund_no=?, handled_at=?, status=? WHERE id=? AND status=?`).
		WithArgs("cs1", "R2", testTime, "refunding", int64(1), "pending").WillReturnResult(sqlmock.NewResult(0, 0))
	ok, err = r.Exceptions.AttachRefund(ctx, 1, "cs1", "R2", testTime)
	if ok || err != nil {
		t.Fatal("非待处理应 false（防重复）")
	}

	// MarkRefundedByRefundNo
	mock.ExpectExec(`UPDATE exception_payment SET status=? WHERE refund_no=?`).WithArgs("refunded", "R1").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := r.Exceptions.MarkRefundedByRefundNo(ctx, "R1"); err != nil {
		t.Fatal(err)
	}
	done(t, mock)
}

// ---------- user ----------

func TestUserRepo(t *testing.T) {
	ctx := context.Background()
	r, mock, _ := newMock(t)

	mock.ExpectExec(`INSERT INTO user(id,username,password_hash,name,role) VALUES(?,?,?,?,?)`).
		WithArgs("u1", "buyer01", "hash", "买家一", "buyer").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := r.Users.Insert(ctx, types.User{ID: "u1", Username: "buyer01", PasswordHash: "hash", Name: "买家一", Role: "buyer"}); err != nil {
		t.Fatal(err)
	}

	userCols := []string{"id", "username", "password_hash", "name", "role"}
	mock.ExpectQuery(`SELECT id,username,password_hash,name,role FROM user WHERE username=?`).
		WithArgs("buyer01").WillReturnRows(sqlmock.NewRows(userCols).AddRow("u1", "buyer01", "hash", "买家一", "buyer"))
	u, err := r.Users.GetByUsername(ctx, "buyer01")
	if err != nil || u.ID != "u1" {
		t.Fatal("GetByUsername 失败")
	}
	mock.ExpectQuery(`SELECT id,username,password_hash,name,role FROM user WHERE username=?`).
		WithArgs("none").WillReturnRows(sqlmock.NewRows(userCols))
	if _, err := r.Users.GetByUsername(ctx, "none"); !errors.Is(err, types.ErrNotFound) {
		t.Fatal("不存在应 ErrNotFound")
	}

	mock.ExpectQuery(`SELECT id,username,password_hash,name,role FROM user WHERE id=?`).
		WithArgs("u1").WillReturnRows(sqlmock.NewRows(userCols).AddRow("u1", "buyer01", "hash", "买家一", "buyer"))
	u, err = r.Users.GetByID(ctx, "u1")
	if err != nil || u.Username != "buyer01" {
		t.Fatal("GetByID 失败")
	}
	mock.ExpectQuery(`SELECT id,username,password_hash,name,role FROM user WHERE id=?`).WithArgs("u1").WillReturnError(errDB)
	if _, err := r.Users.GetByID(ctx, "u1"); !errors.Is(err, errDB) {
		t.Fatal("应传递错误")
	}

	mock.ExpectQuery(`SELECT COUNT(*) FROM user`).WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(6))
	n, err := r.Users.Count(ctx)
	if err != nil || n != 6 {
		t.Fatalf("应为 6，实际 %d", n)
	}
	done(t, mock)
}

// ---------- 辅助函数 ----------

func TestHelpers(t *testing.T) {
	// isDuplicate
	if !isDuplicate(errors.New("Error 1062: Duplicate entry 'x' for key 'y'")) {
		t.Fatal("应识别重复键错误")
	}
	if isDuplicate(errDB) || isDuplicate(nil) {
		t.Fatal("非重复键不应误判")
	}
	// notFound：由调用方注入 sql.ErrNoRows 路径已覆盖；普通错误应透传
	if err := notFound(errDB); !errors.Is(err, errDB) {
		t.Fatal("普通错误应透传")
	}
	if err := notFound(nil); err != nil {
		t.Fatal("nil 应透传")
	}
}
