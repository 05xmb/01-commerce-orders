// Package sign 提供交易系统与模拟渠道之间的 HMAC-SHA256 报文签名与口令散列。
package sign

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

func hmacHex(secret string, parts ...string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(mac.Sum(nil))
}

// PaySignParts 支付通知签名域
func PaySignParts(notifyID, tradeNo, orderNo string, amount, ts int64) []string {
	return []string{notifyID, tradeNo, orderNo, fmt.Sprint(amount), fmt.Sprint(ts)}
}

// RefundSignParts 退款通知签名域
func RefundSignParts(notifyID, channelRefundNo, refundNo string, amount int64, result string, ts int64) []string {
	return []string{notifyID, channelRefundNo, refundNo, fmt.Sprint(amount), result, fmt.Sprint(ts)}
}

// Sign 计算签名
func Sign(secret string, parts ...string) string { return hmacHex(secret, parts...) }

// Verify 常量时间比较签名
func Verify(secret, expect string, parts ...string) bool {
	return hmac.Equal([]byte(hmacHex(secret, parts...)), []byte(expect))
}

// HashPassword 教学演示口令散列（sha256 + 固定盐，预置账号专用）
func HashPassword(pw string) string {
	sum := sha256.Sum256([]byte("trade-salt|" + pw))
	return hex.EncodeToString(sum[:])
}
