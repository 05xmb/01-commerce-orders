package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"trade/internal/pkg/sign"
	"trade/internal/repo/fake"
	"trade/internal/service"
	"trade/internal/tokstore"
	"trade/internal/types"
)

// newErrHandler 全部仓储方法失败的服务，用于覆盖 handler 错误分支
func newErrHandler(t *testing.T) *H {
	t.Helper()
	base := fake.NewStore()
	svc := service.New(service.Deps{
		Store:   &errStore{Store: base},
		Clock:   time.Now,
		Timeout: func() int { return 600 },
		Secret:  testSecret,
		Channel: fakeChannel{},
		Tokens:  tokstore.NewMemoryStore(),
	})
	return New(svc)
}

// 所有读类端点在服务层报错时应返回 500
func TestErrorBranches(t *testing.T) {
	h := newErrHandler(t)

	get := func(target, uid, role string) (*httptest.ResponseRecorder, *http.Request) {
		return httptest.NewRecorder(), withAuth(httptest.NewRequest(http.MethodGet, target, nil), uid, role)
	}
	assert500 := func(name string, w *httptest.ResponseRecorder) {
		t.Helper()
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("%s 服务错误应 500，实际 %d", name, w.Code)
		}
	}

	w, r := get("/api/products", "u-b1", types.RoleBuyer)
	h.products(w, r)
	assert500("products", w)

	w, r = get("/api/orders/mine", "u-b1", types.RoleBuyer)
	h.myOrders(w, r)
	assert500("myOrders", w)

	w, r = get("/api/orders/get?order_no=O1", "u-b1", types.RoleBuyer)
	h.getOrder(w, r)
	assert500("getOrder", w)

	w, r = get("/api/wh/orders", "u-wh", types.RoleWH)
	h.whOrders(w, r)
	assert500("whOrders", w)

	w, r = get("/api/cs/refunds", "u-cs", types.RoleCS)
	h.csRefunds(w, r)
	assert500("csRefunds", w)

	w, r = get("/api/cs/exceptions", "u-cs", types.RoleCS)
	h.csExceptions(w, r)
	assert500("csExceptions", w)

	w, r = get("/api/ops/products", "u-op", types.RoleOp)
	h.opsProducts(w, r)
	assert500("opsProducts", w)

	w, r = get("/api/ops/summary", "u-op", types.RoleOp)
	h.opsSummary(w, r)
	assert500("opsSummary", w)

	w, r = get("/api/ops/order-detail?order_no=O1", "u-op", types.RoleOp)
	h.opsOrderDetail(w, r)
	assert500("opsOrderDetail", w)

	w, r = get("/api/ops/notify-logs", "u-op", types.RoleOp)
	h.opsNotifyLogs(w, r)
	assert500("opsNotifyLogs", w)

	w, r = get("/api/ops/exceptions", "u-op", types.RoleOp)
	h.opsExceptions(w, r)
	assert500("opsExceptions", w)

	w, r = get("/api/ops/stock-flows?product_id=P1", "u-op", types.RoleOp)
	h.opsStockFlows(w, r)
	assert500("opsStockFlows", w)
}

// 写类端点在服务层报错时应返回 500
func TestErrorBranchesWrite(t *testing.T) {
	h := newErrHandler(t)
	assert500 := func(name string, w *httptest.ResponseRecorder) {
		t.Helper()
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("%s 服务错误应 500，实际 %d body=%s", name, w.Code, w.Body.String())
		}
	}

	post := func(target, body, uid, role string) (*httptest.ResponseRecorder, *http.Request) {
		return httptest.NewRecorder(), withAuth(doJSON(t, http.MethodPost, target, body), uid, role)
	}

	w, r := post("/api/orders/create", `{"product_id":"P1","qty":1}`, "u-b1", types.RoleBuyer)
	h.createOrder(w, r)
	assert500("createOrder", w)

	w, r = post("/api/orders/cancel", `{"order_no":"O1"}`, "u-b1", types.RoleBuyer)
	h.cancelOrder(w, r)
	assert500("cancelOrder", w)

	w, r = post("/api/orders/refund/apply", `{"order_no":"O1","reason":"x"}`, "u-b1", types.RoleBuyer)
	h.applyRefund(w, r)
	assert500("applyRefund", w)

	w, r = post("/api/wh/ship", `{"order_no":"O1","carrier":"SF","tracking_no":"T1"}`, "u-wh", types.RoleWH)
	h.ship(w, r)
	assert500("ship", w)

	w, r = post("/api/cs/refunds/review", `{"refund_no":"R1","approve":true,"note":"同意"}`, "u-cs", types.RoleCS)
	h.csReview(w, r)
	assert500("csReview", w)

	w, r = post("/api/cs/refunds/retry", `{"refund_no":"R1"}`, "u-cs", types.RoleCS)
	h.csRetry(w, r)
	assert500("csRetry", w)

	w, r = post("/api/cs/exceptions/refund", `{"id":1}`, "u-cs", types.RoleCS)
	h.csExceptionRefund(w, r)
	assert500("csExceptionRefund", w)

	w, r = post("/api/ops/products/price", `{"id":"P1","price":100}`, "u-op", types.RoleOp)
	h.opsSetPrice(w, r)
	assert500("opsSetPrice", w)

	w, r = post("/api/ops/products/sale", `{"id":"P1","on_sale":false}`, "u-op", types.RoleOp)
	h.opsSetSale(w, r)
	assert500("opsSetSale", w)
}

// 通知端点在服务层报错时应返回 500
func TestErrorBranchesNotify(t *testing.T) {
	h := newErrHandler(t)

	n := types.PayNotification{NotifyID: "N1", TradeNo: "T1", OrderNo: "O1", Amount: 100, Ts: 1}
	n.Sign = sign.Sign(testSecret, sign.PaySignParts(n.NotifyID, n.TradeNo, n.OrderNo, n.Amount, n.Ts)...)
	body, _ := json.Marshal(n)
	w := httptest.NewRecorder()
	h.notifyPay(w, doJSON(t, http.MethodPost, "/api/notify/pay", string(body)))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("pay 服务错误应 500，实际 %d", w.Code)
	}

	rn := types.RefundNotification{NotifyID: "RN1", ChannelRefundNo: "CR1", RefundNo: "R1", Amount: 100, Result: "success", Ts: 1}
	rn.Sign = sign.Sign(testSecret, sign.RefundSignParts(rn.NotifyID, rn.ChannelRefundNo, rn.RefundNo, rn.Amount, rn.Result, rn.Ts)...)
	body, _ = json.Marshal(rn)
	w = httptest.NewRecorder()
	h.notifyRefund(w, doJSON(t, http.MethodPost, "/api/notify/refund", string(body)))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("refund 服务错误应 500，实际 %d", w.Code)
	}
}
