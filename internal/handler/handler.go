// Package handler 交易服务 HTTP 接口层：路由注册、鉴权守卫、参数解析与错误映射。
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/zeromicro/go-zero/rest"

	"trade/internal/service"
	"trade/internal/types"
)

type ctxKey string

const (
	KeyUserID ctxKey = "uid"
	KeyRole   ctxKey = "role"
)

type H struct {
	svc *service.Services
}

func New(svc *service.Services) *H { return &H{svc: svc} }

func uid(r *http.Request) string  { v, _ := r.Context().Value(KeyUserID).(string); return v }
func role(r *http.Request) string { v, _ := r.Context().Value(KeyRole).(string); return v }

func ok(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
}

func fail(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": status, "msg": msg})
}

func handleErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, types.ErrUnauthorized):
		fail(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, types.ErrForbidden):
		fail(w, http.StatusForbidden, err.Error())
	case errors.Is(err, types.ErrNotFound), errors.Is(err, types.ErrRefundNotFound):
		fail(w, http.StatusNotFound, err.Error())
	case errors.Is(err, types.ErrStatus), errors.Is(err, types.ErrOutOfStock),
		errors.Is(err, types.ErrDuplicate), errors.Is(err, types.ErrConflict),
		errors.Is(err, types.ErrInvalidQty), errors.Is(err, types.ErrChannel):
		fail(w, http.StatusConflict, err.Error())
	case errors.Is(err, types.ErrSign):
		fail(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, types.ErrParam):
		fail(w, http.StatusBadRequest, err.Error())
	default:
		fail(w, http.StatusInternalServerError, "内部错误")
	}
}

func decode(r *http.Request, v any) error {
	if r.Body == nil {
		return types.ErrParam
	}
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return types.ErrParam
	}
	return nil
}

func query(r *http.Request, key string) string { return r.URL.Query().Get(key) }

// guard 生成角色守卫中间件：校验 token 与角色，注入用户上下文
func (h *H) guard(roles ...string) rest.Middleware {
	allowed := map[string]bool{}
	for _, ro := range roles {
		allowed[ro] = true
	}
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			id, ro, err := h.svc.Auth.Verify(r.Context(), token)
			if err != nil {
				fail(w, http.StatusUnauthorized, "未登录或登录已过期")
				return
			}
			if !allowed[ro] {
				fail(w, http.StatusForbidden, "当前角色无权访问")
				return
			}
			ctx := context.WithValue(r.Context(), KeyUserID, id)
			ctx = context.WithValue(ctx, KeyRole, ro)
			next(w, r.WithContext(ctx))
		}
	}
}

// Register 注册全部路由；通知与登录免鉴权（通知走 HMAC 验签），其余按角色守卫
func (h *H) Register(server *rest.Server) {
	server.AddRoutes([]rest.Route{
		{Method: http.MethodGet, Path: "/healthz", Handler: func(w http.ResponseWriter, r *http.Request) { ok(w, "ok") }},
		{Method: http.MethodPost, Path: "/api/login", Handler: h.login},
		{Method: http.MethodPost, Path: "/api/notify/pay", Handler: h.notifyPay},
		{Method: http.MethodPost, Path: "/api/notify/refund", Handler: h.notifyRefund},
	})

	server.AddRoutes(rest.WithMiddlewares([]rest.Middleware{h.guard(types.RoleBuyer)}, []rest.Route{
		{Method: http.MethodGet, Path: "/api/products", Handler: h.products},
		{Method: http.MethodPost, Path: "/api/orders/create", Handler: h.createOrder},
		{Method: http.MethodGet, Path: "/api/orders/mine", Handler: h.myOrders},
		{Method: http.MethodGet, Path: "/api/orders/get", Handler: h.getOrder},
		{Method: http.MethodPost, Path: "/api/orders/cancel", Handler: h.cancelOrder},
		{Method: http.MethodPost, Path: "/api/orders/refund/apply", Handler: h.applyRefund},
	}...))

	server.AddRoutes(rest.WithMiddlewares([]rest.Middleware{h.guard(types.RoleWH)}, []rest.Route{
		{Method: http.MethodGet, Path: "/api/wh/orders", Handler: h.whOrders},
		{Method: http.MethodPost, Path: "/api/wh/ship", Handler: h.ship},
	}...))

	server.AddRoutes(rest.WithMiddlewares([]rest.Middleware{h.guard(types.RoleCS)}, []rest.Route{
		{Method: http.MethodGet, Path: "/api/cs/refunds", Handler: h.csRefunds},
		{Method: http.MethodPost, Path: "/api/cs/refunds/review", Handler: h.csReview},
		{Method: http.MethodPost, Path: "/api/cs/refunds/retry", Handler: h.csRetry},
		{Method: http.MethodGet, Path: "/api/cs/exceptions", Handler: h.csExceptions},
		{Method: http.MethodPost, Path: "/api/cs/exceptions/refund", Handler: h.csExceptionRefund},
	}...))

	server.AddRoutes(rest.WithMiddlewares([]rest.Middleware{h.guard(types.RoleOp)}, []rest.Route{
		{Method: http.MethodGet, Path: "/api/ops/products", Handler: h.opsProducts},
		{Method: http.MethodPost, Path: "/api/ops/products/price", Handler: h.opsSetPrice},
		{Method: http.MethodPost, Path: "/api/ops/products/sale", Handler: h.opsSetSale},
		{Method: http.MethodGet, Path: "/api/ops/summary", Handler: h.opsSummary},
		{Method: http.MethodGet, Path: "/api/ops/order-detail", Handler: h.opsOrderDetail},
		{Method: http.MethodGet, Path: "/api/ops/notify-logs", Handler: h.opsNotifyLogs},
		{Method: http.MethodGet, Path: "/api/ops/exceptions", Handler: h.opsExceptions},
		{Method: http.MethodGet, Path: "/api/ops/stock-flows", Handler: h.opsStockFlows},
	}...))
}

// ---------- 公共 ----------
func (h *H) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decode(r, &req); err != nil {
		handleErr(w, err)
		return
	}
	res, err := h.svc.Auth.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		handleErr(w, err)
		return
	}
	ok(w, res)
}

func (h *H) notifyPay(w http.ResponseWriter, r *http.Request) {
	var n types.PayNotification
	if err := decode(r, &n); err != nil {
		handleErr(w, err)
		return
	}
	result, err := h.svc.Notify.PayNotify(r.Context(), n)
	if err != nil {
		handleErr(w, err)
		return
	}
	ok(w, map[string]string{"result": result})
}

func (h *H) notifyRefund(w http.ResponseWriter, r *http.Request) {
	var n types.RefundNotification
	if err := decode(r, &n); err != nil {
		handleErr(w, err)
		return
	}
	result, err := h.svc.Refund.HandleNotify(r.Context(), n)
	if err != nil {
		handleErr(w, err)
		return
	}
	ok(w, map[string]string{"result": result})
}

// ---------- 买家 ----------
func (h *H) products(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.Product.List(r.Context(), true)
	if err != nil {
		handleErr(w, err)
		return
	}
	ok(w, list)
}

func (h *H) createOrder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProductID string `json:"product_id"`
		Qty       int    `json:"qty"`
	}
	if err := decode(r, &req); err != nil {
		handleErr(w, err)
		return
	}
	o, err := h.svc.Order.Create(r.Context(), uid(r), req.ProductID, req.Qty)
	if err != nil {
		handleErr(w, err)
		return
	}
	ok(w, o)
}

func (h *H) myOrders(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.Order.ListMine(r.Context(), uid(r))
	if err != nil {
		handleErr(w, err)
		return
	}
	ok(w, list)
}

func (h *H) getOrder(w http.ResponseWriter, r *http.Request) {
	o, err := h.svc.Order.Get(r.Context(), query(r, "order_no"), uid(r), role(r))
	if err != nil {
		handleErr(w, err)
		return
	}
	ok(w, o)
}

func (h *H) cancelOrder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrderNo string `json:"order_no"`
	}
	if err := decode(r, &req); err != nil {
		handleErr(w, err)
		return
	}
	if err := h.svc.Order.Cancel(r.Context(), req.OrderNo, uid(r)); err != nil {
		handleErr(w, err)
		return
	}
	ok(w, "已取消")
}

func (h *H) applyRefund(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrderNo string `json:"order_no"`
		Reason  string `json:"reason"`
	}
	if err := decode(r, &req); err != nil {
		handleErr(w, err)
		return
	}
	rf, err := h.svc.Refund.Apply(r.Context(), req.OrderNo, uid(r), req.Reason)
	if err != nil {
		handleErr(w, err)
		return
	}
	ok(w, rf)
}

// ---------- 仓库 ----------
func (h *H) whOrders(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.Order.ListForWarehouse(r.Context())
	if err != nil {
		handleErr(w, err)
		return
	}
	ok(w, list)
}

func (h *H) ship(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrderNo    string `json:"order_no"`
		Carrier    string `json:"carrier"`
		TrackingNo string `json:"tracking_no"`
	}
	if err := decode(r, &req); err != nil {
		handleErr(w, err)
		return
	}
	if err := h.svc.Ship.Ship(r.Context(), req.OrderNo, uid(r), req.Carrier, req.TrackingNo); err != nil {
		handleErr(w, err)
		return
	}
	ok(w, "已发货")
}

// ---------- 客服 ----------
func (h *H) csRefunds(w http.ResponseWriter, r *http.Request) {
	status := query(r, "status")
	var list []types.Refund
	var err error
	if status == "" {
		list, err = h.svc.Refund.ListByStatus(r.Context(),
			types.RefundReview, types.RefundRefunding, types.RefundFailedPending, types.RefundSuccess, types.RefundRejected)
	} else {
		list, err = h.svc.Refund.ListByStatus(r.Context(), status)
	}
	if err != nil {
		handleErr(w, err)
		return
	}
	ok(w, list)
}

func (h *H) csReview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefundNo string `json:"refund_no"`
		Approve  bool   `json:"approve"`
		Note     string `json:"note"`
	}
	if err := decode(r, &req); err != nil {
		handleErr(w, err)
		return
	}
	if err := h.svc.Refund.Review(r.Context(), req.RefundNo, uid(r), req.Approve, req.Note); err != nil {
		handleErr(w, err)
		return
	}
	ok(w, "已处理")
}

func (h *H) csRetry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefundNo string `json:"refund_no"`
	}
	if err := decode(r, &req); err != nil {
		handleErr(w, err)
		return
	}
	if err := h.svc.Refund.Retry(r.Context(), req.RefundNo); err != nil {
		handleErr(w, err)
		return
	}
	ok(w, "已重试")
}

func (h *H) csExceptions(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.Refund.ListExceptions(r.Context(), query(r, "status"))
	if err != nil {
		handleErr(w, err)
		return
	}
	ok(w, list)
}

func (h *H) csExceptionRefund(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID int64 `json:"id"`
	}
	if err := decode(r, &req); err != nil {
		handleErr(w, err)
		return
	}
	rf, err := h.svc.Refund.RefundException(r.Context(), req.ID, uid(r))
	if err != nil {
		handleErr(w, err)
		return
	}
	ok(w, rf)
}

// ---------- 运营 ----------
func (h *H) opsProducts(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.Product.List(r.Context(), false)
	if err != nil {
		handleErr(w, err)
		return
	}
	ok(w, list)
}

func (h *H) opsSetPrice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID    string `json:"id"`
		Price int64  `json:"price"`
	}
	if err := decode(r, &req); err != nil {
		handleErr(w, err)
		return
	}
	if err := h.svc.Product.SetPrice(r.Context(), req.ID, req.Price); err != nil {
		handleErr(w, err)
		return
	}
	ok(w, "已改价")
}

func (h *H) opsSetSale(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     string `json:"id"`
		OnSale bool   `json:"on_sale"`
	}
	if err := decode(r, &req); err != nil {
		handleErr(w, err)
		return
	}
	if err := h.svc.Product.SetOnSale(r.Context(), req.ID, req.OnSale); err != nil {
		handleErr(w, err)
		return
	}
	ok(w, "已更新")
}

func (h *H) opsSummary(w http.ResponseWriter, r *http.Request) {
	sum, err := h.svc.Ops.Summary(r.Context())
	if err != nil {
		handleErr(w, err)
		return
	}
	ok(w, sum)
}

func (h *H) opsOrderDetail(w http.ResponseWriter, r *http.Request) {
	detail, err := h.svc.Ops.OrderDetail(r.Context(), query(r, "order_no"))
	if err != nil {
		handleErr(w, err)
		return
	}
	ok(w, detail)
}

func (h *H) opsNotifyLogs(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.Ops.NotifyLogs(r.Context(), 200)
	if err != nil {
		handleErr(w, err)
		return
	}
	ok(w, list)
}

func (h *H) opsExceptions(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.Ops.Exceptions(r.Context(), query(r, "status"))
	if err != nil {
		handleErr(w, err)
		return
	}
	ok(w, list)
}

func (h *H) opsStockFlows(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.Product.Flows(r.Context(), query(r, "product_id"))
	if err != nil {
		handleErr(w, err)
		return
	}
	ok(w, list)
}
