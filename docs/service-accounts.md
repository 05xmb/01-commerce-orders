# 预置账号与权限说明

> 口令不落仓库：初始口令经 `SEED_PASSWORD` 环境变量注入（本地 `.env`，已 gitignore；GitHub Actions Secrets / 1Panel 运行时注入），数据库仅存 `sign.HashPassword` 散列（`cmd/server/main.go` 种子）。本文档只描述账号与权限，不含任何口令明文。

## 预置账号（seed 幂等预置，6 账号）

| 账号 | 角色 | 权限范围 |
| --- | --- | --- |
| buyer01 / buyer02 / buyer03 | 买家 | 浏览商品、下单、支付、查看本人订单与物流、取消订单、申请售前退款 |
| op01 | 运营 | 运营核查工作台：汇总、订单穿透（支付/退款/发货/异常/流水/通知日志）、异常台账查询（只读） |
| wh01 | 仓库 | 待发货列表、发货登记（运单号唯一；售前退款审核中/已退款不可发货） |
| cs01 | 客服 | 售前退款审核（拒绝需备注）、退款重试、异常退款处理（异常台账留痕） |

## 权限边界（内部 RBAC + 归属校验）

- 买家：订单接口仅可操作本人订单，跨买家查看/取消返回 403；请求体伪造 `user_id`/`buyer_id` 无效，身份以登录令牌为准（`internal/handler/handler.go`）。
- 仓库 / 客服 / 运营：仅可访问各自角色前缀接口（`/api/wh/*`、`/api/cs/*`、`/api/ops/*`），跨角色访问返回 403。
- 运营核查为只读：无法修改订单/库存/退款状态；异常台账独立存储，订单状态无法使其消失。
- 越权实测记录见 `reports/acceptance/acceptance-report.md` D 项；接口级单测见 `internal/handler/handler_test.go`。

## 口令管理口径（共同要求第 10 节：凭据仅存部署环境与本地配置）

- 初始口令唯一来源：`SEED_PASSWORD` 环境变量。本地开发填项目根 `.env`（gitignore）；部署由 GitHub Actions Secrets / 1Panel 运行时注入（`deploy/docker-compose.yml` 透传）。
- 种子入库即散列（`internal/pkg/sign.HashPassword`），数据库无明文；仓库（含脚本、前端、CI）不留任何口令字面量。
- 登录失败锁定、修改口令等按题目要求仅预留扩展性（AGENTS.md 约束清单）。
