package idgen

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"sync/atomic"
	"time"
)

var seq uint64

func randSuffix() uint64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err == nil {
		return binary.BigEndian.Uint64(b[:]) % 1000000
	}
	return atomic.AddUint64(&seq, 1) % 1000000
}

// uniqueSuffix 进程内唯一序列（原子自增）：订单号/退款单号以唯一性为先，不用纯随机（生日悖论会碰撞）
func uniqueSuffix() uint64 {
	return atomic.AddUint64(&seq, 1) % 1000000
}

// OrderNo 生成订单号：O + yyyyMMdd + 6 位唯一序列（进程内原子自增；唯一索引兜底，冲突重试）
func OrderNo(now time.Time) string {
	return fmt.Sprintf("O%s%06d", now.Format("20060102"), uniqueSuffix())
}

// RefundNo 生成退款单号：R + yyyyMMdd + 6 位唯一序列
func RefundNo(now time.Time) string {
	return fmt.Sprintf("R%s%06d", now.Format("20060102"), uniqueSuffix())
}

// Token 生成登录 token
func Token() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%x", b[:])
}

// TradeNo 模拟渠道交易号
func TradeNo(prefix string) string {
	return fmt.Sprintf("%s%d%06d", prefix, time.Now().UnixNano()/1e6, randSuffix())
}
