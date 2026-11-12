package idgen

import (
	"strings"
	"testing"
	"time"
)

func TestOrderNo(t *testing.T) {
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.Local)
	no := OrderNo(now)
	if !strings.HasPrefix(no, "O20261112") {
		t.Fatalf("订单号格式错误: %s", no)
	}
	if len(no) != 15 {
		t.Fatalf("订单号长度应为 15，实际 %d", len(no))
	}
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		n := OrderNo(now)
		if seen[n] {
			t.Fatal("订单号重复")
		}
		seen[n] = true
	}
}

func TestRefundNoAndToken(t *testing.T) {
	if !strings.HasPrefix(RefundNo(time.Now()), "R") {
		t.Fatal("退款单号前缀错误")
	}
	if Token() == Token() {
		t.Fatal("token 应随机")
	}
	if !strings.HasPrefix(TradeNo("T"), "T") {
		t.Fatal("交易号前缀错误")
	}
}
