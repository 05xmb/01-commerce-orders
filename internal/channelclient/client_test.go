package channelclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"trade/internal/types"
)

func newServer(t *testing.T, body string) (*Client, func()) {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/channel/refund" {
			t.Errorf("路径错误: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	return &Client{BaseURL: ts.URL, HTTP: ts.Client()}, ts.Close
}

// 正常受理：返回渠道退款号
func TestRequestRefund_OK(t *testing.T) {
	c, closeFn := newServer(t, `{"code":0,"data":{"channel_refund_no":"CR123"}}`)
	defer closeFn()
	no, err := c.RequestRefund(context.Background(), "R1", 100)
	if err != nil || no != "CR123" {
		t.Fatalf("应返回渠道退款号，no=%s err=%v", no, err)
	}
}

// 渠道业务拒绝：code != 0
func TestRequestRefund_BizReject(t *testing.T) {
	c, closeFn := newServer(t, `{"code":500,"msg":"余额不足"}`)
	defer closeFn()
	_, err := c.RequestRefund(context.Background(), "R1", 100)
	if !errors.Is(err, types.ErrChannel) {
		t.Fatalf("应返回渠道错误，实际 %v", err)
	}
}

// 渠道返回非法 JSON
func TestRequestRefund_BadJSON(t *testing.T) {
	c, closeFn := newServer(t, `not-json`)
	defer closeFn()
	if _, err := c.RequestRefund(context.Background(), "R1", 100); !errors.Is(err, types.ErrChannel) {
		t.Fatalf("非法响应应返回渠道错误，实际 %v", err)
	}
}

// 渠道不可达
func TestRequestRefund_Unreachable(t *testing.T) {
	c, closeFn := newServer(t, `{}`)
	closeFn() // 立即关闭，模拟不可达
	if _, err := c.RequestRefund(context.Background(), "R1", 100); !errors.Is(err, types.ErrChannel) {
		t.Fatalf("不可达应返回渠道错误，实际 %v", err)
	}
}

// 默认 HTTP 客户端（HTTP 为 nil 时用 http.DefaultClient）
func TestRequestRefund_DefaultClient(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"channel_refund_no":"CR9"}}`))
	}))
	defer ts.Close()
	c := &Client{BaseURL: ts.URL}
	no, err := c.RequestRefund(context.Background(), "R1", 100)
	if err != nil || no != "CR9" {
		t.Fatalf("默认客户端失败，no=%s err=%v", no, err)
	}
}
