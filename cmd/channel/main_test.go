package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"trade/internal/config"
	"trade/internal/pkg/sign"
	"trade/internal/types"
)

const chSecret = "channel-test-secret"

// notifySink 记录交易系统收到的通知
type notifySink struct {
	t       *testing.T
	payCh   chan types.PayNotification
	refCh   chan types.RefundNotification
	server  *httptest.Server
	closeFn func()
}

func newNotifySink(t *testing.T) *notifySink {
	t.Helper()
	s := &notifySink{t: t, payCh: make(chan types.PayNotification, 10), refCh: make(chan types.RefundNotification, 10)}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/notify/pay":
			var n types.PayNotification
			if err := json.NewDecoder(r.Body).Decode(&n); err == nil {
				s.payCh <- n
			}
		case "/api/notify/refund":
			var n types.RefundNotification
			if err := json.NewDecoder(r.Body).Decode(&n); err == nil {
				s.refCh <- n
			}
		default:
			t.Errorf("未知通知路径: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	s.closeFn = s.server.Close
	return s
}

func (s *notifySink) waitPay(t *testing.T) types.PayNotification {
	t.Helper()
	select {
	case n := <-s.payCh:
		return n
	case <-time.After(3 * time.Second):
		t.Fatal("等待支付通知超时")
		return types.PayNotification{}
	}
}

func (s *notifySink) waitRefund(t *testing.T) types.RefundNotification {
	t.Helper()
	select {
	case n := <-s.refCh:
		return n
	case <-time.After(3 * time.Second):
		t.Fatal("等待退款通知超时")
		return types.RefundNotification{}
	}
}

func newChannelServer(t *testing.T, sink *notifySink) *channelServer {
	t.Helper()
	return &channelServer{
		cfg: config.ChannelConfig{
			ChannelSecret:     chSecret,
			TradeNotifyURL:    sink.server.URL,
			PayNotifyDelayMs:  5,
			LateNotifyDelayMs: 10,
		},
		refundAttempts: map[string]int{},
		client:         &http.Client{Timeout: 2 * time.Second},
	}
}

func postJSON(t *testing.T, handler http.HandlerFunc, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	handler(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
	return w
}

func TestHandlePay_BadRequest(t *testing.T) {
	sink := newNotifySink(t)
	defer sink.closeFn()
	s := newChannelServer(t, sink)

	w := postJSON(t, s.handlePay, "{bad")
	if w.Code != http.StatusOK {
		t.Fatal("渠道固定 200 返回业务码")
	}
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out["code"].(float64) != 400 {
		t.Fatal("非法请求应返回业务码 400")
	}
	// 缺 order_no
	w = postJSON(t, s.handlePay, `{"order_no":"","amount":100}`)
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out["code"].(float64) != 400 {
		t.Fatal("缺 order_no 应返回业务码 400")
	}
}

func TestHandlePay_Scenarios(t *testing.T) {
	sink := newNotifySink(t)
	defer sink.closeFn()
	s := newChannelServer(t, sink)

	// fail 场景：不发通知
	w := postJSON(t, s.handlePay, `{"order_no":"O1","amount":1000,"scenario":"fail"}`)
	var out struct {
		Code int `json:"code"`
		Data struct {
			TradeNo string `json:"trade_no"`
			Result  string `json:"result"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out.Data.Result != "failed" || out.Data.TradeNo == "" {
		t.Fatalf("fail 场景响应错误: %s", w.Body.String())
	}
	select {
	case n := <-sink.payCh:
		t.Fatalf("fail 场景不应发通知，收到 %+v", n)
	case <-time.After(100 * time.Millisecond):
	}

	// success 场景：正确签名与金额
	postJSON(t, s.handlePay, `{"order_no":"O2","amount":1000,"scenario":"success"}`)
	n := sink.waitPay(t)
	if n.OrderNo != "O2" || n.Amount != 1000 || n.TradeNo == "" {
		t.Fatalf("success 通知字段错误: %+v", n)
	}
	if !sign.Verify(chSecret, n.Sign, sign.PaySignParts(n.NotifyID, n.TradeNo, n.OrderNo, n.Amount, n.Ts)...) {
		t.Fatal("success 通知签名应有效")
	}

	// 缺省场景按 success
	postJSON(t, s.handlePay, `{"order_no":"O2b","amount":1000}`)
	n = sink.waitPay(t)
	if n.OrderNo != "O2b" {
		t.Fatal("缺省场景应按 success 处理")
	}

	// duplicate：同一通知送达两次
	postJSON(t, s.handlePay, `{"order_no":"O3","amount":1000,"scenario":"duplicate"}`)
	n1 := sink.waitPay(t)
	n2 := sink.waitPay(t)
	if n1.NotifyID != n2.NotifyID || n1.TradeNo != n2.TradeNo {
		t.Fatal("duplicate 应重复送达同一通知")
	}

	// late：延迟送达（LateNotifyDelayMs 配置为 10ms）
	postJSON(t, s.handlePay, `{"order_no":"O4","amount":1000,"scenario":"late"}`)
	start := time.Now()
	n = sink.waitPay(t)
	if n.OrderNo != "O4" {
		t.Fatal("late 通知订单号错误")
	}
	_ = start

	// amount_mismatch：金额少 1000 分
	postJSON(t, s.handlePay, `{"order_no":"O5","amount":10000,"scenario":"amount_mismatch"}`)
	n = sink.waitPay(t)
	if n.Amount != 9000 {
		t.Fatalf("amount_mismatch 应为 9000，实际 %d", n.Amount)
	}

	// unknown_order：订单号替换为不存在的单号
	postJSON(t, s.handlePay, `{"order_no":"O6","amount":1000,"scenario":"unknown_order"}`)
	n = sink.waitPay(t)
	if n.OrderNo != "O99999999999999" {
		t.Fatalf("unknown_order 应为 O99999999999999，实际 %s", n.OrderNo)
	}

	// bad_sign：签名无效
	postJSON(t, s.handlePay, `{"order_no":"O7","amount":1000,"scenario":"bad_sign"}`)
	n = sink.waitPay(t)
	if sign.Verify(chSecret, n.Sign, sign.PaySignParts(n.NotifyID, n.TradeNo, n.OrderNo, n.Amount, n.Ts)...) {
		t.Fatal("bad_sign 通知签名应无效")
	}
}

func TestHandleRefund(t *testing.T) {
	sink := newNotifySink(t)
	defer sink.closeFn()
	s := newChannelServer(t, sink)

	// 参数错误
	w := postJSON(t, s.handleRefund, `{"refund_no":""}`)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out["code"].(float64) != 400 {
		t.Fatal("缺 refund_no 应返回业务码 400")
	}

	// 第 1 次受理：默认失败（演示"首次失败"）
	w = postJSON(t, s.handleRefund, `{"refund_no":"R1","amount":1000}`)
	var r1 struct {
		Data struct {
			ChannelRefundNo string `json:"channel_refund_no"`
			Attempt         int    `json:"attempt"`
			WillNotify      string `json:"will_notify"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &r1)
	if r1.Data.Attempt != 1 || r1.Data.WillNotify != "fail" {
		t.Fatalf("首次应失败: %s", w.Body.String())
	}
	n := sink.waitRefund(t)
	if n.Result != "fail" || n.RefundNo != "R1" {
		t.Fatalf("首次退款通知应为 fail: %+v", n)
	}
	if !sign.Verify(chSecret, n.Sign, sign.RefundSignParts(n.NotifyID, n.ChannelRefundNo, n.RefundNo, n.Amount, n.Result, n.Ts)...) {
		t.Fatal("退款通知签名应有效")
	}

	// 第 2 次受理：默认成功（演示"重试成功"）
	postJSON(t, s.handleRefund, `{"refund_no":"R1","amount":1000}`)
	n = sink.waitRefund(t)
	if n.Result != "success" {
		t.Fatalf("重试应成功: %+v", n)
	}

	// 显式指定场景
	postJSON(t, s.handleRefund, `{"refund_no":"R2","amount":1000,"scenario":"fail"}`)
	n = sink.waitRefund(t)
	if n.Result != "fail" {
		t.Fatal("显式 fail 场景应失败")
	}
}

// post 失败分支：目标不可达仅记录日志，不 panic
func TestPost_Unreachable(t *testing.T) {
	s := &channelServer{client: &http.Client{Timeout: 100 * time.Millisecond}}
	s.cfg.TradeNotifyURL = "http://127.0.0.1:1"
	s.post("/api/notify/pay", types.PayNotification{})
}

// buildPayNotify 签名开关
func TestBuildPayNotify(t *testing.T) {
	s := &channelServer{cfg: config.ChannelConfig{ChannelSecret: chSecret}}
	valid := s.buildPayNotify("T1", "O1", 100, true)
	if !sign.Verify(chSecret, valid.Sign, sign.PaySignParts(valid.NotifyID, valid.TradeNo, valid.OrderNo, valid.Amount, valid.Ts)...) {
		t.Fatal("validSign=true 应生成有效签名")
	}
	invalid := s.buildPayNotify("T1", "O1", 100, false)
	if sign.Verify(chSecret, invalid.Sign, sign.PaySignParts(invalid.NotifyID, invalid.TradeNo, invalid.OrderNo, invalid.Amount, invalid.Ts)...) {
		t.Fatal("validSign=false 应生成无效签名")
	}
}
