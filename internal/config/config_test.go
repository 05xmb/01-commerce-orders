package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "server.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadEnvExpand(t *testing.T) {
	t.Setenv("MYSQL_DSN", "root:x@tcp(127.0.0.1:3306)/db")
	t.Setenv("REDIS_ADDR", "127.0.0.1:6379")
	t.Setenv("REDIS_PASS", "")
	t.Setenv("CHANNEL_SECRET", "s3")
	t.Setenv("CHANNEL_BASE_URL", "http://127.0.0.1:8902")
	p := writeTemp(t, `Name: trade-api
Host: 0.0.0.0
Port: 8888
MysqlDSN: ${MYSQL_DSN}
RedisAddr: ${REDIS_ADDR}
RedisPass: ${REDIS_PASS}
ChannelSecret: ${CHANNEL_SECRET}
ChannelBaseURL: ${CHANNEL_BASE_URL}
PayTimeoutSeconds: 30
`)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.MysqlDSN == "" || c.ChannelSecret != "s3" || c.PayTimeoutSeconds != 30 || c.Port != 8888 {
		t.Fatalf("配置解析错误: %+v", c)
	}
}

// 教学时钟：改配置免重启生效
func TestTimeoutProvider_Dynamic(t *testing.T) {
	p := writeTemp(t, `Name: trade-api
Host: 0.0.0.0
Port: 8888
PayTimeoutSeconds: 30
`)
	tp := NewTimeoutProvider(p)
	if got := tp.Seconds(); got != 30 {
		t.Fatalf("应为 30，实际 %d", got)
	}
	// 修改配置
	if err := os.WriteFile(p, []byte(`Name: trade-api
Host: 0.0.0.0
Port: 8888
PayTimeoutSeconds: 600
`), 0o644); err != nil {
		t.Fatal(err)
	}
	// 越过 1s 缓存
	tp.mu.Lock()
	tp.expire = time.Now().Add(-time.Second)
	tp.mu.Unlock()
	if got := tp.Seconds(); got != 600 {
		t.Fatalf("改配置后应为 600，实际 %d", got)
	}
}

func TestTimeoutProvider_Fallback(t *testing.T) {
	tp := NewTimeoutProvider(filepath.Join(t.TempDir(), "not-exist.yaml"))
	if got := tp.Seconds(); got != 600 {
		t.Fatalf("配置文件缺失应兜底 600，实际 %d", got)
	}
}

// 渠道配置加载（含环境变量展开与文件缺失）
func TestLoadChannel(t *testing.T) {
	t.Setenv("CHANNEL_SECRET", "s3")
	t.Setenv("TRADE_NOTIFY_URL", "http://127.0.0.1:8888")
	p := writeTemp(t, `Name: pay-channel
Host: 0.0.0.0
Port: 8902
ChannelSecret: ${CHANNEL_SECRET}
TradeNotifyURL: ${TRADE_NOTIFY_URL}
PayNotifyDelayMs: 50
LateNotifyDelayMs: 40000
`)
	c, err := LoadChannel(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.ChannelSecret != "s3" || c.TradeNotifyURL != "http://127.0.0.1:8888" || c.PayNotifyDelayMs != 50 || c.LateNotifyDelayMs != 40000 {
		t.Fatalf("渠道配置解析错误: %+v", c)
	}
	// 文件缺失
	if _, err := LoadChannel(filepath.Join(t.TempDir(), "none.yaml")); err == nil {
		t.Fatal("文件缺失应报错")
	}
}
