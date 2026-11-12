// 模拟支付渠道入口：受理付款/退款，异步向交易系统发送签名通知。
// 支持场景注入：成功、失败、重复通知、迟到通知、金额不符、未知订单、验签失败。
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"trade/internal/config"
	"trade/internal/pkg/idgen"
	"trade/internal/pkg/sign"
	"trade/internal/types"
)

type channelServer struct {
	cfg config.ChannelConfig
	mu  sync.Mutex
	// refundAttempts 记录每个退款单的受理次数：默认第 1 次通知失败、第 2 次起成功（演示"首次失败、重试成功"）
	refundAttempts map[string]int
	client         *http.Client
}

func main() {
	cfgPath := flag.String("f", "etc/channel.yaml", "配置文件路径")
	flag.Parse()
	c, err := config.LoadChannel(*cfgPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	s := &channelServer{
		cfg:            c,
		refundAttempts: map[string]int{},
		client:         &http.Client{Timeout: 3 * time.Second},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/channel/pay", s.handlePay)
	mux.HandleFunc("/channel/refund", s.handleRefund)
	mux.HandleFunc("/channel/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 0, "ok", nil)
	})
	addr := fmt.Sprintf("%s:%d", c.Host, c.Port)
	log.Printf("pay-channel 启动于 %s（通知目标 %s）", addr, c.TradeNotifyURL)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func writeJSON(w http.ResponseWriter, code int, msg string, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "msg": msg, "data": data})
}

type payRequest struct {
	OrderNo  string `json:"order_no"`
	Amount   int64  `json:"amount"`
	Scenario string `json:"scenario"` // success/fail/duplicate/late/amount_mismatch/unknown_order/bad_sign
	DelayMs  int    `json:"delay_ms"`
}

func (s *channelServer) handlePay(w http.ResponseWriter, r *http.Request) {
	var req payRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OrderNo == "" {
		writeJSON(w, 400, "参数错误", nil)
		return
	}
	tradeNo := idgen.TradeNo("T")
	delay := time.Duration(req.DelayMs) * time.Millisecond
	if req.DelayMs == 0 {
		delay = time.Duration(s.cfg.PayNotifyDelayMs) * time.Millisecond
	}
	scenario := req.Scenario
	if scenario == "" {
		scenario = "success"
	}

	switch scenario {
	case "fail":
		// 支付失败：渠道不发成功通知，订单保持待付款直至超时
		writeJSON(w, 0, "支付失败", map[string]string{"trade_no": tradeNo, "result": "failed"})
		return
	case "duplicate":
		n := s.buildPayNotify(tradeNo, req.OrderNo, req.Amount, true)
		s.schedulePay(n, delay)
		s.schedulePay(n, delay+200*time.Millisecond) // 同一通知补发一次
	case "late":
		n := s.buildPayNotify(tradeNo, req.OrderNo, req.Amount, true)
		s.schedulePay(n, time.Duration(s.cfg.LateNotifyDelayMs)*time.Millisecond)
	case "amount_mismatch":
		n := s.buildPayNotify(tradeNo, req.OrderNo, req.Amount-1000, true)
		s.schedulePay(n, delay)
	case "unknown_order":
		n := s.buildPayNotify(tradeNo, "O99999999999999", req.Amount, true)
		s.schedulePay(n, delay)
	case "bad_sign":
		n := s.buildPayNotify(tradeNo, req.OrderNo, req.Amount, false)
		s.schedulePay(n, delay)
	default: // success
		n := s.buildPayNotify(tradeNo, req.OrderNo, req.Amount, true)
		s.schedulePay(n, delay)
	}
	writeJSON(w, 0, "已受理", map[string]any{
		"trade_no": tradeNo, "scenario": scenario, "notify_in_ms": delay.Milliseconds(),
	})
}

func (s *channelServer) buildPayNotify(tradeNo, orderNo string, amount int64, validSign bool) types.PayNotification {
	n := types.PayNotification{
		NotifyID: idgen.TradeNo("N"),
		TradeNo:  tradeNo,
		OrderNo:  orderNo,
		Amount:   amount,
		Ts:       time.Now().Unix(),
	}
	parts := sign.PaySignParts(n.NotifyID, n.TradeNo, n.OrderNo, n.Amount, n.Ts)
	if validSign {
		n.Sign = sign.Sign(s.cfg.ChannelSecret, parts...)
	} else {
		n.Sign = "invalid-signature"
	}
	return n
}

func (s *channelServer) schedulePay(n types.PayNotification, delay time.Duration) {
	go func() {
		time.Sleep(delay)
		s.post("/api/notify/pay", n)
	}()
}

func (s *channelServer) scheduleRefund(n types.RefundNotification, delay time.Duration) {
	go func() {
		time.Sleep(delay)
		s.post("/api/notify/refund", n)
	}()
}

func (s *channelServer) post(path string, payload any) {
	body, _ := json.Marshal(payload)
	url := s.cfg.TradeNotifyURL + path
	resp, err := s.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("通知发送失败 %s: %v（交易系统应视为未送达）", url, err)
		return
	}
	defer resp.Body.Close()
	log.Printf("通知已送达 %s", url)
}

type refundRequest struct {
	RefundNo string `json:"refund_no"`
	Amount   int64  `json:"amount"`
	Scenario string `json:"scenario"` // 可选：success/fail；缺省按受理次数（首次失败、重试成功）
	DelayMs  int    `json:"delay_ms"`
}

func (s *channelServer) handleRefund(w http.ResponseWriter, r *http.Request) {
	var req refundRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefundNo == "" {
		writeJSON(w, 400, "参数错误", nil)
		return
	}
	s.mu.Lock()
	s.refundAttempts[req.RefundNo]++
	attempt := s.refundAttempts[req.RefundNo]
	s.mu.Unlock()

	result := "success"
	if req.Scenario != "" {
		result = req.Scenario
	} else if attempt == 1 {
		result = "fail"
	}
	channelRefundNo := idgen.TradeNo("CR")
	n := types.RefundNotification{
		NotifyID:        idgen.TradeNo("RN"),
		ChannelRefundNo: channelRefundNo,
		RefundNo:        req.RefundNo,
		Amount:          req.Amount,
		Result:          result,
		Ts:              time.Now().Unix(),
	}
	n.Sign = sign.Sign(s.cfg.ChannelSecret,
		sign.RefundSignParts(n.NotifyID, n.ChannelRefundNo, n.RefundNo, n.Amount, n.Result, n.Ts)...)
	delay := time.Duration(req.DelayMs) * time.Millisecond
	s.scheduleRefund(n, delay)
	writeJSON(w, 0, "已受理", map[string]any{
		"channel_refund_no": channelRefundNo, "attempt": attempt, "will_notify": result,
	})
}
