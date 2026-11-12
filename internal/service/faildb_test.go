package service

import (
	"errors"
	"testing"
	"time"

	"trade/internal/pkg/sign"
	"trade/internal/repo/fake"
	"trade/internal/tokstore"
	"trade/internal/types"
)

// newFailFixture 在内存仓储外层织入一次性故障注入，用于覆盖数据库错误分支
func newFailFixture(t *testing.T) (*fixture, *failPlan) {
	t.Helper()
	plan := newFailPlan()
	f := newFixture(t)
	fs := &failStore{Store: fake.NewStore(), p: plan}
	r := fs.Repos()
	_ = r.Products.Insert(f.ctx, types.Product{ID: "P1", Name: "商品A", Price: 10000, Stock: 5, OnSale: true})
	_ = r.Products.Insert(f.ctx, types.Product{ID: "P2", Name: "商品B", Price: 5000, Stock: 10, OnSale: true})
	f.svc = New(Deps{
		Store:   fs,
		Clock:   f.clock.Now,
		Timeout: func() int { return 30 },
		Secret:  testSecret,
		Channel: f.ch,
		Tokens:  tokstore.NewMemoryStore(),
	})
	return f, plan
}

// TestDBErrorBranches 注入一次性数据库故障，验证服务层正确向上传递错误
func TestDBErrorBranches(t *testing.T) {
	dbErr := errors.New("db down")

	// 下单链路（事务内：扣减 → 商品快照 → 建单 → 占用流水）
	t.Run("Order_Create", func(t *testing.T) {
		f, plan := newFailFixture(t)
		plan.set("Products.TryDeduct", dbErr)
		if _, err := f.svc.Order.Create(f.ctx, "b1", "P1", 1); !errors.Is(err, dbErr) {
			t.Fatalf("扣减失败应传递错误，实际 %v", err)
		}
		plan.set("Products.Get", dbErr)
		if _, err := f.svc.Order.Create(f.ctx, "b1", "P1", 1); !errors.Is(err, dbErr) {
			t.Fatalf("快照失败应传递错误，实际 %v", err)
		}
		plan.set("Orders.Insert", dbErr)
		if _, err := f.svc.Order.Create(f.ctx, "b1", "P1", 1); !errors.Is(err, dbErr) {
			t.Fatalf("建单失败应传递错误，实际 %v", err)
		}
		plan.set("StockFlows.Insert", dbErr)
		if _, err := f.svc.Order.Create(f.ctx, "b1", "P1", 1); !errors.Is(err, dbErr) {
			t.Fatalf("写流水失败应传递错误，实际 %v", err)
		}
	})

	// 订单查询
	t.Run("Order_Get", func(t *testing.T) {
		f, plan := newFailFixture(t)
		o := f.createAndPay(t, "b1", "P1", 1)
		plan.set("Orders.Get", dbErr)
		if _, err := f.svc.Order.Get(f.ctx, o.OrderNo, "b1", types.RoleBuyer); !errors.Is(err, dbErr) {
			t.Fatalf("订单查询应传递错误，实际 %v", err)
		}
	})

	t.Run("Order_ListMine", func(t *testing.T) {
		f, plan := newFailFixture(t)
		f.createAndPay(t, "b1", "P1", 1)
		plan.set("Orders.ListByBuyer", dbErr)
		if _, err := f.svc.Order.ListMine(f.ctx, "b1"); !errors.Is(err, dbErr) {
			t.Fatalf("我的订单列表应传递错误，实际 %v", err)
		}
		plan.set("Orders.ListExpiredPending", dbErr)
		if _, err := f.svc.Order.ListMine(f.ctx, "b1"); !errors.Is(err, dbErr) {
			t.Fatalf("惰性关单扫描应传递错误，实际 %v", err)
		}
	})

	// 超时关单扫描
	t.Run("Order_CloseExpired", func(t *testing.T) {
		f, plan := newFailFixture(t)
		plan.set("Orders.ListExpiredPending", dbErr)
		if _, err := f.svc.Order.CloseExpired(f.ctx); !errors.Is(err, dbErr) {
			t.Fatalf("关单扫描应传递错误，实际 %v", err)
		}
	})

	// 买家取消（关闭链路：ClosePending → AddStock → 流水）
	t.Run("Order_Cancel", func(t *testing.T) {
		f, plan := newFailFixture(t)
		o, err := f.svc.Order.Create(f.ctx, "b1", "P1", 1)
		if err != nil {
			t.Fatal(err)
		}
		plan.set("Orders.ClosePending", dbErr)
		if err := f.svc.Order.Cancel(f.ctx, o.OrderNo, "b1"); !errors.Is(err, dbErr) {
			t.Fatalf("关闭失败应传递错误，实际 %v", err)
		}
		plan.set("Products.AddStock", dbErr)
		if err := f.svc.Order.Cancel(f.ctx, o.OrderNo, "b1"); !errors.Is(err, dbErr) {
			t.Fatalf("回补失败应传递错误，实际 %v", err)
		}
	})

	// 支付通知
	t.Run("Notify_Pay", func(t *testing.T) {
		f, plan := newFailFixture(t)
		o, err := f.svc.Order.Create(f.ctx, "b1", "P1", 1)
		if err != nil {
			t.Fatal(err)
		}
		n := f.signedPayNotify("T-X1", o.OrderNo, o.Amount)
		plan.set("Payments.GetByTradeNo", dbErr)
		if _, err := f.svc.Notify.PayNotify(f.ctx, n); !errors.Is(err, dbErr) {
			t.Fatalf("幂等查询失败应传递错误，实际 %v", err)
		}
		plan.set("Payments.Insert", dbErr)
		if _, err := f.svc.Notify.PayNotify(f.ctx, n); !errors.Is(err, dbErr) {
			t.Fatalf("记款失败应传递错误，实际 %v", err)
		}
	})

	// 退款申请（事务内：订单查询 → 状态流转 → 退款单）
	t.Run("Refund_Apply", func(t *testing.T) {
		f, plan := newFailFixture(t)
		o := f.createAndPay(t, "b1", "P1", 1)
		plan.set("Orders.Get", dbErr)
		if _, err := f.svc.Refund.Apply(f.ctx, o.OrderNo, "b1", "不想要了"); !errors.Is(err, dbErr) {
			t.Fatalf("退款申请应传递错误，实际 %v", err)
		}
		plan.set("Orders.SetStatusCond", dbErr)
		if _, err := f.svc.Refund.Apply(f.ctx, o.OrderNo, "b1", "不想要了"); !errors.Is(err, dbErr) {
			t.Fatalf("状态流转失败应传递错误，实际 %v", err)
		}
		plan.set("Refunds.Insert", dbErr)
		if _, err := f.svc.Refund.Apply(f.ctx, o.OrderNo, "b1", "不想要了"); !errors.Is(err, dbErr) {
			t.Fatalf("退款单落库失败应传递错误，实际 %v", err)
		}
	})

	// 退款审核
	t.Run("Refund_Review", func(t *testing.T) {
		f, plan := newFailFixture(t)
		o := f.createAndPay(t, "b1", "P1", 1)
		rf, err := f.svc.Refund.Apply(f.ctx, o.OrderNo, "b1", "不想要了")
		if err != nil {
			t.Fatal(err)
		}
		// 同意：Approve 失败
		plan.set("Refunds.Approve", dbErr)
		if err := f.svc.Refund.Review(f.ctx, rf.RefundNo, "cs1", true, "同意"); !errors.Is(err, dbErr) {
			t.Fatalf("审核失败应传递错误，实际 %v", err)
		}
		// 同意后请求渠道：Refunds.Get 失败
		plan.set("Refunds.Get", dbErr)
		if err := f.svc.Refund.Review(f.ctx, rf.RefundNo, "cs1", true, "同意"); !errors.Is(err, dbErr) {
			t.Fatalf("渠道请求前查询失败应传递错误，实际 %v", err)
		}
		// 拒绝：事务内 Refunds.Get 失败
		plan.set("Refunds.Get", dbErr)
		if err := f.svc.Refund.Review(f.ctx, rf.RefundNo, "cs1", false, "拒绝"); !errors.Is(err, dbErr) {
			t.Fatalf("拒绝审核应传递错误，实际 %v", err)
		}
	})

	// 退款失败重试
	t.Run("Refund_Retry", func(t *testing.T) {
		f, plan := newFailFixture(t)
		o := f.createAndPay(t, "b1", "P1", 1)
		rf, err := f.svc.Refund.Apply(f.ctx, o.OrderNo, "b1", "不想要了")
		if err != nil {
			t.Fatal(err)
		}
		plan.set("Refunds.MarkRefunding", dbErr)
		if err := f.svc.Refund.Retry(f.ctx, rf.RefundNo); !errors.Is(err, dbErr) {
			t.Fatalf("重试失败应传递错误，实际 %v", err)
		}
	})

	// 退款通知
	t.Run("Refund_Notify", func(t *testing.T) {
		f, plan := newFailFixture(t)
		o := f.createAndPay(t, "b1", "P1", 1)
		rf, err := f.svc.Refund.Apply(f.ctx, o.OrderNo, "b1", "不想要了")
		if err != nil {
			t.Fatal(err)
		}
		n := f.signedRefundNotify(rf.RefundNo, "CR-1", rf.Amount, "success")
		plan.set("Refunds.Get", dbErr)
		if _, err := f.svc.Refund.HandleNotify(f.ctx, n); !errors.Is(err, dbErr) {
			t.Fatalf("退款通知查询失败应传递错误，实际 %v", err)
		}
		plan.set("Refunds.MarkSuccess", dbErr)
		if _, err := f.svc.Refund.HandleNotify(f.ctx, n); !errors.Is(err, dbErr) {
			t.Fatalf("退款成功落账失败应传递错误，实际 %v", err)
		}
		fail := f.signedRefundNotify(rf.RefundNo, "CR-1", rf.Amount, "fail")
		plan.set("Refunds.MarkFailedPending", dbErr)
		if _, err := f.svc.Refund.HandleNotify(f.ctx, fail); !errors.Is(err, dbErr) {
			t.Fatalf("失败标记应传递错误，实际 %v", err)
		}
	})

	// 发货
	t.Run("Ship", func(t *testing.T) {
		f, plan := newFailFixture(t)
		o := f.createAndPay(t, "b1", "P1", 1)
		plan.set("Orders.SetStatusCond", dbErr)
		if err := f.svc.Ship.Ship(f.ctx, o.OrderNo, "wh1", "顺丰", "SF123"); !errors.Is(err, dbErr) {
			t.Fatalf("发货状态流转失败应传递错误，实际 %v", err)
		}
		plan.set("Shipments.Insert", dbErr)
		if err := f.svc.Ship.Ship(f.ctx, o.OrderNo, "wh1", "顺丰", "SF123"); !errors.Is(err, dbErr) {
			t.Fatalf("发货记录落库失败应传递错误，实际 %v", err)
		}
	})

	// 运营核查
	t.Run("Ops", func(t *testing.T) {
		f, plan := newFailFixture(t)
		o := f.createAndPay(t, "b1", "P1", 1)
		plan.set("Payments.SumAmount", dbErr)
		if _, err := f.svc.Ops.Summary(f.ctx); !errors.Is(err, dbErr) {
			t.Fatalf("汇总查询失败应传递错误，实际 %v", err)
		}
		plan.set("Exceptions.List", dbErr)
		if _, err := f.svc.Ops.Exceptions(f.ctx, ""); !errors.Is(err, dbErr) {
			t.Fatalf("异常列表查询失败应传递错误，实际 %v", err)
		}
		plan.set("PayLogs.ListByOrder", dbErr)
		if _, err := f.svc.Ops.OrderDetail(f.ctx, o.OrderNo); !errors.Is(err, dbErr) {
			t.Fatalf("审计穿透应传递错误，实际 %v", err)
		}
		plan.set("Orders.Get", dbErr)
		if _, err := f.svc.Ops.OrderDetail(f.ctx, o.OrderNo); !errors.Is(err, dbErr) {
			t.Fatalf("审计订单查询应传递错误，实际 %v", err)
		}
	})

	// 商品管理
	t.Run("Product", func(t *testing.T) {
		f, plan := newFailFixture(t)
		plan.set("Products.SetPrice", dbErr)
		if err := f.svc.Product.SetPrice(f.ctx, "P1", 100); !errors.Is(err, dbErr) {
			t.Fatalf("改价失败应传递错误，实际 %v", err)
		}
		plan.set("Products.SetOnSale", dbErr)
		if err := f.svc.Product.SetOnSale(f.ctx, "P1", false); !errors.Is(err, dbErr) {
			t.Fatalf("下架失败应传递错误，实际 %v", err)
		}
		plan.set("Products.List", dbErr)
		if _, err := f.svc.Product.List(f.ctx, false); !errors.Is(err, dbErr) {
			t.Fatalf("列表查询失败应传递错误，实际 %v", err)
		}
	})

	// 登录（查询错误统一转 ErrUnauthorized，不泄露用户是否存在）
	t.Run("Auth", func(t *testing.T) {
		f, plan := newFailFixture(t)
		r := f.svc.d.Store.Repos()
		if err := r.Users.Insert(f.ctx, types.User{ID: "u1", Username: "cs01", PasswordHash: sign.HashPassword("123456"), Role: types.RoleCS}); err != nil {
			t.Fatal(err)
		}
		plan.set("Users.GetByUsername", dbErr)
		if _, err := f.svc.Auth.Login(f.ctx, "cs01", "123456"); !errors.Is(err, types.ErrUnauthorized) {
			t.Fatalf("登录查询失败应转未授权，实际 %v", err)
		}
	})
}

var _ = time.Second
