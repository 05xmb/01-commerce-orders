// Package config 提供服务配置加载与支付超时时长的动态读取（教学时钟）。
package config

import (
	"os"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/rest"
)

type Config struct {
	rest.RestConf
	MysqlDSN          string `json:",optional"`
	RedisAddr         string `json:",optional"`
	RedisPass         string `json:",optional"`
	ChannelSecret     string `json:",optional"`
	ChannelBaseURL    string `json:",optional"`
	PayTimeoutSeconds int    `json:",default=30"`
}

// Load 读取 YAML 并展开环境变量占位符（${VAR}）
func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var c Config
	err = conf.LoadFromYamlBytes([]byte(os.ExpandEnv(string(raw))), &c)
	return c, err
}

// TimeoutProvider 支付超时时长提供者：支持修改配置文件免重启生效
type TimeoutProvider struct {
	path   string
	mu     sync.Mutex
	cached int
	expire time.Time
	now    func() time.Time
}

func NewTimeoutProvider(path string) *TimeoutProvider {
	return &TimeoutProvider{path: path, now: time.Now}
}

// Seconds 返回当前支付超时时长（秒）；1 秒内缓存，避免每次请求读盘
func (p *TimeoutProvider) Seconds() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.now().Before(p.expire) && p.cached > 0 {
		return p.cached
	}
	c, err := Load(p.path)
	if err == nil && c.PayTimeoutSeconds > 0 {
		p.cached = c.PayTimeoutSeconds
	}
	if p.cached <= 0 {
		p.cached = 600 // 兜底：生产默认 10 分钟
	}
	p.expire = p.now().Add(time.Second)
	return p.cached
}

// ChannelConfig 模拟渠道配置
type ChannelConfig struct {
	rest.RestConf
	ChannelSecret     string `json:",optional"`
	TradeNotifyURL    string `json:",optional"`
	PayNotifyDelayMs  int    `json:",default=0"`
	LateNotifyDelayMs int    `json:",default=40000"`
}

func LoadChannel(path string) (ChannelConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ChannelConfig{}, err
	}
	var c ChannelConfig
	err = conf.LoadFromYamlBytes([]byte(os.ExpandEnv(string(raw))), &c)
	return c, err
}
