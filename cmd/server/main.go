// 交易系统入口：加载配置、初始化 MySQL/Redis、种子数据、超时关单扫描、启动 HTTP 服务。
package main

import (
	"context"
	"database/sql"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/rest"

	"trade/internal/channelclient"
	"trade/internal/config"
	"trade/internal/handler"
	"trade/internal/pkg/sign"
	"trade/internal/repo"
	"trade/internal/repo/mysql"
	"trade/internal/service"
	"trade/internal/tokstore"
	"trade/internal/types"
)

func main() {
	cfgPath := flag.String("f", "etc/server.yaml", "配置文件路径")
	flag.Parse()

	c, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	db, err := sql.Open("mysql", c.MysqlDSN)
	if err != nil {
		log.Fatalf("连接配置错误: %v", err)
	}
	db.SetMaxOpenConns(20)
	db.SetConnMaxLifetime(5 * time.Minute)
	if err := db.Ping(); err != nil {
		log.Fatalf("MySQL 不可用: %v", err)
	}
	store := mysql.NewStore(db)

	seedPass := os.Getenv("SEED_PASSWORD")
	if seedPass == "" {
		log.Fatal("SEED_PASSWORD 未配置：预置账号口令经环境变量注入（本地 .env / 部署运行环境），仓库不存明文口令")
	}
	if err := seed(context.Background(), store, seedPass); err != nil {
		log.Fatalf("种子数据失败: %v", err)
	}

	rdb := redis.NewClient(&redis.Options{Addr: c.RedisAddr, Password: c.RedisPass})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("Redis 不可用: %v", err)
	}

	timeout := config.NewTimeoutProvider(*cfgPath)
	svc := service.New(service.Deps{
		Store:   store,
		Clock:   time.Now,
		Timeout: timeout.Seconds,
		Secret:  c.ChannelSecret,
		Channel: &channelclient.Client{BaseURL: c.ChannelBaseURL, HTTP: &http.Client{Timeout: 3 * time.Second}},
		Tokens:  &tokstore.RedisStore{Rdb: rdb},
	})

	// 超时关单定时扫描（A5：与惰性检查共用 closeOne 逻辑）
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for range tick.C {
			_, _ = svc.Order.CloseExpired(context.Background())
		}
	}()

	server := rest.MustNewServer(c.RestConf)
	defer server.Stop()
	handler.New(svc).Register(server)

	log.Printf("trade-api 启动于 %s:%d（支付超时 %ds，改配置免重启）", c.Host, c.Port, timeout.Seconds())
	server.Start()
}

// seed 预置账号与商品（幂等：已有数据则跳过）；口令经环境变量注入，仅落散列
func seed(ctx context.Context, store repo.Store, seedPass string) error {
	r := store.Repos()
	n, err := r.Users.Count(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		users := []types.User{
			{ID: "u-buyer01", Username: "buyer01", Name: "买家一", Role: types.RoleBuyer},
			{ID: "u-buyer02", Username: "buyer02", Name: "买家二", Role: types.RoleBuyer},
			{ID: "u-buyer03", Username: "buyer03", Name: "买家三", Role: types.RoleBuyer},
			{ID: "u-op01", Username: "op01", Name: "运营", Role: types.RoleOp},
			{ID: "u-wh01", Username: "wh01", Name: "仓库", Role: types.RoleWH},
			{ID: "u-cs01", Username: "cs01", Name: "客服", Role: types.RoleCS},
		}
		for _, u := range users {
			u.PasswordHash = sign.HashPassword(seedPass) // 口令仅落散列，明文不入库
			if err := r.Users.Insert(ctx, u); err != nil {
				return err
			}
		}
	}
	if _, err := r.Products.Get(ctx, "P1"); err != nil {
		if err := r.Products.Insert(ctx, types.Product{ID: "P1", Name: "商品A（活动款）", Price: 10000, Stock: 5, OnSale: true}); err != nil {
			return err
		}
	}
	if _, err := r.Products.Get(ctx, "P2"); err != nil {
		if err := r.Products.Insert(ctx, types.Product{ID: "P2", Name: "商品B（陪衬）", Price: 5000, Stock: 10, OnSale: true}); err != nil {
			return err
		}
	}
	return nil
}
