package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	service2 "github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/rest"

	"trade/internal/pkg/sign"
	"trade/internal/repo/fake"
	"trade/internal/service"
	"trade/internal/tokstore"
	"trade/internal/types"
)

const testSecret = "handler-test-secret"

type fakeChannel struct{}

func (fakeChannel) RequestRefund(_ context.Context, _ string, _ int64) (string, error) {
	return "CR-test", nil
}

func newTestSvc(t *testing.T) (*service.Services, *tokstore.MemoryStore) {
	t.Helper()
	store := fake.NewStore()
	toks := tokstore.NewMemoryStore()
	svc := service.New(service.Deps{
		Store:   store,
		Clock:   time.Now,
		Timeout: func() int { return 600 },
		Secret:  testSecret,
		Channel: fakeChannel{},
		Tokens:  toks,
	})
	ctx := context.Background()
	r := store.Repos()
	if err := r.Products.Insert(ctx, types.Product{ID: "P1", Name: "商品A", Price: 10000, Stock: 5, OnSale: true}); err != nil {
		t.Fatal(err)
	}
	if err := r.Products.Insert(ctx, types.Product{ID: "P2", Name: "商品B", Price: 5000, Stock: 10, OnSale: true}); err != nil {
		t.Fatal(err)
	}
	mkUser := func(id, name, role string) {
		if err := r.Users.Insert(ctx, types.User{ID: id, Username: name, PasswordHash: sign.HashPassword("123456"), Role: role}); err != nil {
			t.Fatal(err)
		}
	}
	mkUser("u-b1", "buyer01", types.RoleBuyer)
	mkUser("u-b2", "buyer02", types.RoleBuyer)
	mkUser("u-wh", "wh01", types.RoleWH)
	mkUser("u-cs", "cs01", types.RoleCS)
	mkUser("u-op", "op01", types.RoleOp)
	return svc, toks
}

func doJSON(t *testing.T, method, target, body string) *http.Request {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	r.Header.Set("Content-Type", "application/json")
	return r
}

// withAuth 模拟守卫通过后的上下文
func withAuth(r *http.Request, uid, role string) *http.Request {
	ctx := context.WithValue(r.Context(), KeyUserID, uid)
	return r.WithContext(context.WithValue(ctx, KeyRole, role))
}

type resp struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func parseResp(t *testing.T, w *httptest.ResponseRecorder) resp {
	t.Helper()
	var out resp
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应非 JSON: %s", w.Body.String())
	}
	return out
}

// login 走真实登录拿 token
func loginToken(t *testing.T, h *H, username string) string {
	t.Helper()
	w := httptest.NewRecorder()
	h.login(w, doJSON(t, http.MethodPost, "/api/login", `{"username":"`+username+`","password":"123456"}`))
	out := parseResp(t, w)
	if out.Code != 0 {
		t.Fatalf("登录失败: %s", w.Body.String())
	}
	var data struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(out.Data, &data); err != nil || data.Token == "" {
		t.Fatal("登录未返回 token")
	}
	return data.Token
}

func TestLogin_Logout(t *testing.T) {
	svc, _ := newTestSvc(t)
	h := New(svc)

	// 参数错误（非法 JSON）
	w := httptest.NewRecorder()
	h.login(w, doJSON(t, http.MethodPost, "/api/login", "{bad json"))
	if parseResp(t, w).Code != http.StatusBadRequest {
		t.Fatal("非法 JSON 应 400")
	}
	// 密码错误 → 401
	w = httptest.NewRecorder()
	h.login(w, doJSON(t, http.MethodPost, "/api/login", `{"username":"buyer01","password":"wrong"}`))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("密码错误应 401，实际 %d", w.Code)
	}
	// 正常登录
	tok := loginToken(t, h, "buyer01")
	if tok == "" {
		t.Fatal("应返回 token")
	}
}

func TestGuard(t *testing.T) {
	svc, _ := newTestSvc(t)
	h := New(svc)
	tok := loginToken(t, h, "buyer01")

	next := func(w http.ResponseWriter, r *http.Request) {
		ok(w, "passed")
	}
	guarded := h.guard(types.RoleBuyer)(next)

	// 无 token → 401
	w := httptest.NewRecorder()
	guarded(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("无 token 应 401，实际 %d", w.Code)
	}
	// 角色不符 → 403
	wrong := h.guard(types.RoleWH)(next)
	w = httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	wrong(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("越权应 403，实际 %d", w.Code)
	}
	// 合法 → 通过且注入身份
	w = httptest.NewRecorder()
	guarded(w, r)
	out := parseResp(t, w)
	if out.Code != 0 {
		t.Fatalf("合法请求应通过: %s", w.Body.String())
	}
}

func TestRegisterRoutes(t *testing.T) {
	svc, _ := newTestSvc(t)
	h := New(svc)
	server := rest.MustNewServer(rest.RestConf{
		ServiceConf: serviceConf(),
		Host:        "127.0.0.1",
		Port:        0,
	})
	h.Register(server)
	if got := len(server.Routes()); got != 25 {
		t.Fatalf("应注册 25 条路由，实际 %d", got)
	}
}

// serviceConf 提供 go-zero 要求的最小服务配置
func serviceConf() service2.ServiceConf {
	return service2.ServiceConf{Name: "handler-test", Log: logx.LogConf{Mode: "console", Encoding: "plain"}, Mode: "dev"}
}

// 买家侧接口
func TestBuyerEndpoints(t *testing.T) {
	svc, _ := newTestSvc(t)
	h := New(svc)
	loginToken(t, h, "buyer01")

	// 商品列表
	w := httptest.NewRecorder()
	h.products(w, withAuth(httptest.NewRequest(http.MethodGet, "/api/products", nil), "u-b1", types.RoleBuyer))
	if parseResp(t, w).Code != 0 {
		t.Fatal("商品列表失败")
	}

	// 下单参数错误
	w = httptest.NewRecorder()
	h.createOrder(w, withAuth(doJSON(t, http.MethodPost, "/api/orders/create", `{"product_id":"","qty":0}`), "u-b1", types.RoleBuyer))
	if parseResp(t, w).Code == 0 {
		t.Fatal("非法下单应失败")
	}

	// 正常下单
	w = httptest.NewRecorder()
	h.createOrder(w, withAuth(doJSON(t, http.MethodPost, "/api/orders/create", `{"product_id":"P1","qty":1}`), "u-b1", types.RoleBuyer))
	out := parseResp(t, w)
	if out.Code != 0 {
		t.Fatalf("下单失败: %s", w.Body.String())
	}
	var order types.Order
	if err := json.Unmarshal(out.Data, &order); err != nil {
		t.Fatal(err)
	}

	// 我的订单
	w = httptest.NewRecorder()
	h.myOrders(w, withAuth(httptest.NewRequest(http.MethodGet, "/api/orders/mine", nil), "u-b1", types.RoleBuyer))
	out = parseResp(t, w)
	if out.Code != 0 {
		t.Fatal("我的订单失败")
	}
	var orders []types.Order
	_ = json.Unmarshal(out.Data, &orders)
	if len(orders) != 1 {
		t.Fatalf("应有 1 单，实际 %d", len(orders))
	}

	// 订单详情（本人）
	w = httptest.NewRecorder()
	h.getOrder(w, withAuth(httptest.NewRequest(http.MethodGet, "/api/orders/get?order_no="+order.OrderNo, nil), "u-b1", types.RoleBuyer))
	if parseResp(t, w).Code != 0 {
		t.Fatal("订单详情失败")
	}

	// 越权：他人订单 → 404（不泄露存在性）
	w = httptest.NewRecorder()
	h.getOrder(w, withAuth(httptest.NewRequest(http.MethodGet, "/api/orders/get?order_no="+order.OrderNo, nil), "u-b2", types.RoleBuyer))
	if parseResp(t, w).Code == 0 {
		t.Fatal("越权访问他人订单应失败")
	}

	// 取消订单
	w = httptest.NewRecorder()
	h.cancelOrder(w, withAuth(doJSON(t, http.MethodPost, "/api/orders/cancel", `{"order_no":"`+order.OrderNo+`"}`), "u-b1", types.RoleBuyer))
	if parseResp(t, w).Code != 0 {
		t.Fatal("取消订单失败")
	}
	// 重复取消 → 409
	w = httptest.NewRecorder()
	h.cancelOrder(w, withAuth(doJSON(t, http.MethodPost, "/api/orders/cancel", `{"order_no":"`+order.OrderNo+`"}`), "u-b1", types.RoleBuyer))
	if parseResp(t, w).Code != http.StatusConflict {
		t.Fatal("重复取消应 409")
	}
}

// 支付并推进到待发货的辅助流程
func payOrder(t *testing.T, h *H, orderNo string, amount int64) {
	t.Helper()
	n := types.PayNotification{
		NotifyID: "N-test-" + orderNo,
		TradeNo:  "T-test-" + orderNo,
		OrderNo:  orderNo,
		Amount:   amount,
		Ts:       1700000000,
	}
	n.Sign = sign.Sign(testSecret, sign.PaySignParts(n.NotifyID, n.TradeNo, n.OrderNo, n.Amount, n.Ts)...)
	body, _ := json.Marshal(n)
	w := httptest.NewRecorder()
	h.notifyPay(w, doJSON(t, http.MethodPost, "/api/notify/pay", string(body)))
	out := parseResp(t, w)
	if out.Code != 0 {
		t.Fatalf("支付通知失败: %s", w.Body.String())
	}
}

func TestNotifyPay_Endpoints(t *testing.T) {
	svc, _ := newTestSvc(t)
	h := New(svc)

	// 建单
	w := httptest.NewRecorder()
	h.createOrder(w, withAuth(doJSON(t, http.MethodPost, "/api/orders/create", `{"product_id":"P1","qty":1}`), "u-b1", types.RoleBuyer))
	var order types.Order
	_ = json.Unmarshal(parseResp(t, w).Data, &order)

	// 验签失败
	n := types.PayNotification{NotifyID: "N1", TradeNo: "T1", OrderNo: order.OrderNo, Amount: order.Amount, Ts: 1, Sign: "bad"}
	body, _ := json.Marshal(n)
	w = httptest.NewRecorder()
	h.notifyPay(w, doJSON(t, http.MethodPost, "/api/notify/pay", string(body)))
	if parseResp(t, w).Code != http.StatusUnauthorized {
		t.Fatal("验签失败应 401")
	}

	// 正常通知
	payOrder(t, h, order.OrderNo, order.Amount)

	// 重复通知幂等
	payOrder(t, h, order.OrderNo, order.Amount)

	// 通知 JSON 非法
	w = httptest.NewRecorder()
	h.notifyPay(w, doJSON(t, http.MethodPost, "/api/notify/pay", "{bad"))
	if parseResp(t, w).Code != http.StatusBadRequest {
		t.Fatal("非法通知应 400")
	}

	// 退款通知（无此退款单 → unknown，不报错）
	rn := types.RefundNotification{NotifyID: "RN1", ChannelRefundNo: "CR1", RefundNo: "R-none", Amount: 100, Result: "success", Ts: 1}
	rn.Sign = sign.Sign(testSecret, sign.RefundSignParts(rn.NotifyID, rn.ChannelRefundNo, rn.RefundNo, rn.Amount, rn.Result, rn.Ts)...)
	body, _ = json.Marshal(rn)
	w = httptest.NewRecorder()
	h.notifyRefund(w, doJSON(t, http.MethodPost, "/api/notify/refund", string(body)))
	out := parseResp(t, w)
	if out.Code != 0 {
		t.Fatal("未知退款通知应 accepted")
	}

	// 退款通知非法 JSON
	w = httptest.NewRecorder()
	h.notifyRefund(w, doJSON(t, http.MethodPost, "/api/notify/refund", "{bad"))
	if parseResp(t, w).Code != http.StatusBadRequest {
		t.Fatal("非法退款通知应 400")
	}
}

func TestWarehouseEndpoints(t *testing.T) {
	svc, _ := newTestSvc(t)
	h := New(svc)

	// 建单 + 支付
	w := httptest.NewRecorder()
	h.createOrder(w, withAuth(doJSON(t, http.MethodPost, "/api/orders/create", `{"product_id":"P1","qty":1}`), "u-b1", types.RoleBuyer))
	var order types.Order
	_ = json.Unmarshal(parseResp(t, w).Data, &order)
	payOrder(t, h, order.OrderNo, order.Amount)

	// 待发货列表
	w = httptest.NewRecorder()
	h.whOrders(w, withAuth(httptest.NewRequest(http.MethodGet, "/api/wh/orders", nil), "u-wh", types.RoleWH))
	out := parseResp(t, w)
	if out.Code != 0 {
		t.Fatal("待发货列表失败")
	}
	var list []types.Order
	_ = json.Unmarshal(out.Data, &list)
	if len(list) != 1 {
		t.Fatalf("应有 1 单待发货，实际 %d", len(list))
	}

	// 发货参数错误
	w = httptest.NewRecorder()
	h.ship(w, withAuth(doJSON(t, http.MethodPost, "/api/wh/ship", `{"order_no":"","carrier":"","tracking_no":""}`), "u-wh", types.RoleWH))
	if parseResp(t, w).Code == 0 {
		t.Fatal("非法发货应失败")
	}

	// 正常发货
	w = httptest.NewRecorder()
	h.ship(w, withAuth(doJSON(t, http.MethodPost, "/api/wh/ship", `{"order_no":"`+order.OrderNo+`","carrier":"顺丰","tracking_no":"SF123"}`), "u-wh", types.RoleWH))
	if parseResp(t, w).Code != 0 {
		t.Fatalf("发货失败: %s", w.Body.String())
	}

	// 重复发货 → 409
	w = httptest.NewRecorder()
	h.ship(w, withAuth(doJSON(t, http.MethodPost, "/api/wh/ship", `{"order_no":"`+order.OrderNo+`","carrier":"顺丰","tracking_no":"SF456"}`), "u-wh", types.RoleWH))
	if parseResp(t, w).Code != http.StatusConflict {
		t.Fatal("重复发货应 409")
	}
}

func TestCS_Endpoints(t *testing.T) {
	svc, _ := newTestSvc(t)
	h := New(svc)

	// 建单 + 支付
	w := httptest.NewRecorder()
	h.createOrder(w, withAuth(doJSON(t, http.MethodPost, "/api/orders/create", `{"product_id":"P1","qty":1}`), "u-b1", types.RoleBuyer))
	var order types.Order
	_ = json.Unmarshal(parseResp(t, w).Data, &order)
	payOrder(t, h, order.OrderNo, order.Amount)

	// 申请退款
	w = httptest.NewRecorder()
	h.applyRefund(w, withAuth(doJSON(t, http.MethodPost, "/api/orders/refund/apply", `{"order_no":"`+order.OrderNo+`","reason":"不想要了"}`), "u-b1", types.RoleBuyer))
	out := parseResp(t, w)
	if out.Code != 0 {
		t.Fatalf("申请退款失败: %s", w.Body.String())
	}
	var refund types.Refund
	_ = json.Unmarshal(out.Data, &refund)

	// 重复申请 → 409
	w = httptest.NewRecorder()
	h.applyRefund(w, withAuth(doJSON(t, http.MethodPost, "/api/orders/refund/apply", `{"order_no":"`+order.OrderNo+`","reason":"重复"}`), "u-b1", types.RoleBuyer))
	if parseResp(t, w).Code != http.StatusConflict {
		t.Fatal("重复申请应 409")
	}

	// 退款列表（默认多状态）
	w = httptest.NewRecorder()
	h.csRefunds(w, withAuth(httptest.NewRequest(http.MethodGet, "/api/cs/refunds", nil), "u-cs", types.RoleCS))
	out = parseResp(t, w)
	if out.Code != 0 {
		t.Fatal("退款列表失败")
	}
	var refunds []types.Refund
	_ = json.Unmarshal(out.Data, &refunds)
	if len(refunds) != 1 {
		t.Fatalf("应有 1 条退款，实际 %d", len(refunds))
	}

	// 按状态过滤
	w = httptest.NewRecorder()
	h.csRefunds(w, withAuth(httptest.NewRequest(http.MethodGet, "/api/cs/refunds?status="+types.RefundReview, nil), "u-cs", types.RoleCS))
	if parseResp(t, w).Code != 0 {
		t.Fatal("按状态过滤失败")
	}

	// 审核同意（渠道为假实现，会返回成功受理）
	w = httptest.NewRecorder()
	h.csReview(w, withAuth(doJSON(t, http.MethodPost, "/api/cs/refunds/review", `{"refund_no":"`+refund.RefundNo+`","approve":true,"note":"同意"}`), "u-cs", types.RoleCS))
	if parseResp(t, w).Code != 0 {
		t.Fatalf("审核失败: %s", w.Body.String())
	}

	// 重试：refunding 卡住态允许重试恢复（M-1 修复后的新语义，渠道假实现受理成功）
	w = httptest.NewRecorder()
	h.csRetry(w, withAuth(doJSON(t, http.MethodPost, "/api/cs/refunds/retry", `{"refund_no":"`+refund.RefundNo+`"}`), "u-cs", types.RoleCS))
	if parseResp(t, w).Code != 0 {
		t.Fatalf("refunding 卡住态应可重试: %s", w.Body.String())
	}
	// 不存在的退款单重试 → 404
	w = httptest.NewRecorder()
	h.csRetry(w, withAuth(doJSON(t, http.MethodPost, "/api/cs/refunds/retry", `{"refund_no":"R-none"}`), "u-cs", types.RoleCS))
	if parseResp(t, w).Code != http.StatusNotFound {
		t.Fatal("未知退款单重试应 404")
	}

	// 异常列表
	w = httptest.NewRecorder()
	h.csExceptions(w, withAuth(httptest.NewRequest(http.MethodGet, "/api/cs/exceptions", nil), "u-cs", types.RoleCS))
	if parseResp(t, w).Code != 0 {
		t.Fatal("异常列表失败")
	}

	// 异常退款（不存在 → 404）
	w = httptest.NewRecorder()
	h.csExceptionRefund(w, withAuth(doJSON(t, http.MethodPost, "/api/cs/exceptions/refund", `{"id":999}`), "u-cs", types.RoleCS))
	if parseResp(t, w).Code != http.StatusNotFound {
		t.Fatal("未知异常应 404")
	}
}

func TestOpsEndpoints(t *testing.T) {
	svc, _ := newTestSvc(t)
	h := New(svc)

	// 商品管理列表
	w := httptest.NewRecorder()
	h.opsProducts(w, withAuth(httptest.NewRequest(http.MethodGet, "/api/ops/products", nil), "u-op", types.RoleOp))
	out := parseResp(t, w)
	if out.Code != 0 {
		t.Fatal("运营商品列表失败")
	}
	var products []types.Product
	_ = json.Unmarshal(out.Data, &products)
	if len(products) != 2 {
		t.Fatalf("应有 2 个商品，实际 %d", len(products))
	}

	// 改价
	w = httptest.NewRecorder()
	h.opsSetPrice(w, withAuth(doJSON(t, http.MethodPost, "/api/ops/products/price", `{"id":"P1","price":12000}`), "u-op", types.RoleOp))
	if parseResp(t, w).Code != 0 {
		t.Fatal("改价失败")
	}
	// 非法价格
	w = httptest.NewRecorder()
	h.opsSetPrice(w, withAuth(doJSON(t, http.MethodPost, "/api/ops/products/price", `{"id":"P1","price":-1}`), "u-op", types.RoleOp))
	if parseResp(t, w).Code != http.StatusBadRequest {
		t.Fatal("非法价格应 400")
	}

	// 下架再上架
	w = httptest.NewRecorder()
	h.opsSetSale(w, withAuth(doJSON(t, http.MethodPost, "/api/ops/products/sale", `{"id":"P1","on_sale":false}`), "u-op", types.RoleOp))
	if parseResp(t, w).Code != 0 {
		t.Fatal("下架失败")
	}

	// 建单支付产生数据后核查
	w = httptest.NewRecorder()
	h.createOrder(w, withAuth(doJSON(t, http.MethodPost, "/api/orders/create", `{"product_id":"P2","qty":1}`), "u-b1", types.RoleBuyer))
	var order types.Order
	_ = json.Unmarshal(parseResp(t, w).Data, &order)
	payOrder(t, h, order.OrderNo, order.Amount)

	// 汇总
	w = httptest.NewRecorder()
	h.opsSummary(w, withAuth(httptest.NewRequest(http.MethodGet, "/api/ops/summary", nil), "u-op", types.RoleOp))
	out = parseResp(t, w)
	if out.Code != 0 {
		t.Fatal("汇总失败")
	}
	var sum service.Summary
	if err := json.Unmarshal(out.Data, &sum); err != nil {
		t.Fatal(err)
	}
	if sum.PaidAmount != 5000 {
		t.Fatalf("实收应为 5000，实际 %d", sum.PaidAmount)
	}

	// 订单穿透
	w = httptest.NewRecorder()
	h.opsOrderDetail(w, withAuth(httptest.NewRequest(http.MethodGet, "/api/ops/order-detail?order_no="+order.OrderNo, nil), "u-op", types.RoleOp))
	out = parseResp(t, w)
	if out.Code != 0 {
		t.Fatalf("订单穿透失败: %s", w.Body.String())
	}
	var detail service.OrderDetail
	if err := json.Unmarshal(out.Data, &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Payment == nil || detail.Payment.TradeNo == "" {
		t.Fatal("穿透应包含支付记录")
	}

	// 通知日志
	w = httptest.NewRecorder()
	h.opsNotifyLogs(w, withAuth(httptest.NewRequest(http.MethodGet, "/api/ops/notify-logs", nil), "u-op", types.RoleOp))
	if parseResp(t, w).Code != 0 {
		t.Fatal("通知日志失败")
	}

	// 异常列表
	w = httptest.NewRecorder()
	h.opsExceptions(w, withAuth(httptest.NewRequest(http.MethodGet, "/api/ops/exceptions", nil), "u-op", types.RoleOp))
	if parseResp(t, w).Code != 0 {
		t.Fatal("运营异常列表失败")
	}

	// 库存流水
	w = httptest.NewRecorder()
	h.opsStockFlows(w, withAuth(httptest.NewRequest(http.MethodGet, "/api/ops/stock-flows?product_id=P2", nil), "u-op", types.RoleOp))
	out = parseResp(t, w)
	if out.Code != 0 {
		t.Fatal("库存流水失败")
	}
	var flows []types.StockFlow
	_ = json.Unmarshal(out.Data, &flows)
	if len(flows) == 0 {
		t.Fatal("应有占用流水")
	}
}

// handleErr 错误映射全覆盖
func TestHandleErrMapping(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{types.ErrUnauthorized, http.StatusUnauthorized},
		{types.ErrForbidden, http.StatusForbidden},
		{types.ErrNotFound, http.StatusNotFound},
		{types.ErrRefundNotFound, http.StatusNotFound},
		{types.ErrStatus, http.StatusConflict},
		{types.ErrOutOfStock, http.StatusConflict},
		{types.ErrDuplicate, http.StatusConflict},
		{types.ErrConflict, http.StatusConflict},
		{types.ErrInvalidQty, http.StatusConflict},
		{types.ErrChannel, http.StatusConflict},
		{types.ErrSign, http.StatusUnauthorized},
		{types.ErrParam, http.StatusBadRequest},
		{errors.New("unexpected"), http.StatusInternalServerError},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		handleErr(w, c.err)
		if w.Code != c.want {
			t.Fatalf("%v 应映射 %d，实际 %d", c.err, c.want, w.Code)
		}
	}
}

// decode 边界
func TestDecodeEdge(t *testing.T) {
	svc, _ := newTestSvc(t)
	h := New(svc)
	// 空 body
	r := httptest.NewRequest(http.MethodPost, "/x", nil)
	r.Body = nil
	w := httptest.NewRecorder()
	h.cancelOrder(w, withAuth(r, "u-b1", types.RoleBuyer))
	if parseResp(t, w).Code != http.StatusBadRequest {
		t.Fatal("空 body 应 400")
	}
}
