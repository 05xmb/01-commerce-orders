# 一致性检查报告 — 题目 01：订单交易、发货与售前退款

- 检查时间：2026-11-13（验收复审会话）
- 检查依据：`order-trade-consistency-check` Skill C1~C10；AGENTS.md 业务不变量 1~12
- 代码版本：`dev/01` @ `e73db49`（验收基线 `6f3d545` 之后仅测试口令夹具改动，已核 `git diff` 断言完好）
- 环境：本地开发环境重放（trade-api:8888 / pay-channel:8902，本会话自建进程；MySQL 本地库 `电商订单表1`，真实 InnoDB；Redis 本地；教学时钟 `PayTimeoutSeconds: 30`）
- 取证方式：全部命令在本会话实际执行，原始输出落盘 `reports/consistency/logs/`（S1~S4 场景、活体测试、SQL 取证）；不采信开发/测试自述
- 执行数据：场景造数 2026-11-13，今日订单 `O20261113000001`~`000018` 共 15 单（`0009/0011/0014` 为退款单号，见观察 O-2）

## 一、结论

**通过**：C1~C10 全部 PASS，0 项阻断；3 项口径观察（提示级，不影响一致性结论，建议文档注明）。

## 二、发现清单（观察项）

| 编号 | 位置 | 证据 | 严重度 | 影响 | 建议 |
| --- | --- | --- | --- | --- | --- |
| O-1 | `db/schema.sql` stock_flow 表 qty 字段 | `sql-checks.txt` C1 段：occupy 合计为负、release/restore 为正 | 提示 | qty 采用符号约定（占用为负、释放/回补为正），外部审计按 `SUM(qty)` 即可守恒；按 change_type 正数聚合会算反 | schema 注释或文档注明符号约定 |
| O-2 | `internal/pkg/idgen` | `sql-final.txt` F0：订单号 `0009/0011/0014` 缺号，对应 `R20261113000009/000011/000014` 退款单 | 提示 | 订单号与退款单号共享同一自增序列（O/R 前缀区分），存在跳号但唯一性不受影响（各表主键/唯一键完好） | 文档注明编号规则即可，非缺陷 |
| O-3 | 运营汇总口径 `internal/service/ops.go:62-77` | `sql-final.txt` F5：实收 120000 / 实退 100000，与 SQL 明细和完全一致；差额 20000 含迟到支付退款（4 笔 type=late 退款计入实退，其款项目从未计入实收） | 提示 | 红线 10"汇总与明细核对一致"满足（API 汇总=明细表求和）；但"实收−实退"不等于经营净收入口径 | 文档注明：迟到支付不入实收、其退款入实退 |

## 三、C1~C10 逐项核验

| 检查项 | 结论 | 取证命令 | 证据位置 | 说明 |
| --- | --- | --- | --- | --- |
| C1 库存守恒与不超卖 | PASS | `scripts/scenario-rush.ps1`；SQL C1/F6/F7 段 | `logs/s2-rush.txt`、`logs/sql-checks.txt`、`logs/sql-final.txt` | 20 并发抢 5：成交+占用=5 未超卖；今日每单流水恰好"占用 1"，7 张关闭单各"释放 1"，退款单"回补 1"，在途/发货单"释放 0 回补 0"；链尾校验：最后流水 stock_after=当前库存（P1 5/5、P2 8/8）；S2 超时释放回满、S4"占用1/释放1/回补0"权威断言通过 |
| C2 关闭与释放幂等 | PASS | SQL C2 段（关闭单 release 计数）；活体 Batch2 重复关闭 | `logs/sql-checks.txt`（closed_orders_today=7，违规查询空集）、`logs/live-checks.txt` | 每张关闭单恰好 1 条 release，无重复释放；重复关闭被拒（`ErrStatus`）；S4 迟到通知到达后订单未回退、未重占 |
| C3 支付幂等与拒绝留痕 | PASS | 活体 Batch1（amount_mismatch/bad_sign/unknown_order/duplicate 注入）；SQL C3/F1 段 | `logs/live-checks.txt`、`logs/sql-checks.txt`、`logs/sql-final.txt` | X1（O20261113000012）payment 行=1、trade_no=1；通知日志四结果齐全（amount_mismatch×1、bad_sign×1、success×1、duplicate×1）；trade_no 归属唯一（全表空集）；关闭订单无 payment 行（late_payment_rows=0）；未知订单拒绝记录可查（notify-logs result=unknown_order） |
| C4 迟到支付隔离 | PASS | `scripts/scenario-late.ps1`；SQL C4 段 | `logs/s4-late.txt`、`logs/sql-checks.txt` | 异常台账独立 1 行（trade_no 幂等键），订单保持 closed 未复活、本单无新增流水、无重占；对应退款 type=late success 且 restore=0（不回补） |
| C5 退款/发货互斥 | PASS | `scripts/scenario-refund.ps1`；活体 Batch2；SQL C5 段 | `logs/s3-refund.txt`、`logs/live-checks.txt`、`logs/sql-checks.txt` | refund_review 期间发货 409 拦截（S3）；三类互斥 SQL 查询全部空集（审核中无发货、已退款无发货、售前退款无发货单）；拒绝退款后可继续发货（活体）；并发二选一由单测 `TestConcurrent_RefundVsShip` 保证（CI -race 历史检出竞态并修复，commit e90cf90） |
| C6 退款幂等与回补 | PASS | `scripts/scenario-refund.ps1`；活体 Batch5（重复退款通知）；SQL C6/F2/F3 段 | `logs/s3-refund.txt`、`logs/live-checks.txt`、`logs/sql-checks.txt`、`logs/sql-final.txt` | 首次渠道失败→failed_pending 保留→重试成功（attempt=2）；重复退款通知记 result=duplicate，退款单仍唯一 success、restore 保持 1；退款金额=订单金额；refund_notify_log 每单无重复 success |
| C7 发货唯一 | PASS | 活体 Batch2（重复发货）；SQL C7 段 | `logs/live-checks.txt`、`logs/sql-checks.txt` | 重复发货被拒；shipment.order_no UNIQUE 结构保障 + 全表无一单多发；仅待发货单可发货（审核中/已发货均拒绝） |
| C8 异常独立与汇总对账 | PASS | SQL C8/F5 段；活体 Batch4 汇总 | `logs/sql-checks.txt`、`logs/sql-final.txt`、`logs/live-checks.txt` | 异常台账 4 行独立留存（历史 3+今日 1，均 status=refunded，无一被订单状态变化抹除）；API 汇总（paid_amount=120000/refunded_amount=100000）= SQL 明细求和，逐分核对一致 |
| C9 角色与身份可信 | PASS | 活体 Batch3（7 项越权实测）；单测 `TestGetOrder_Forbidden` 等 | `logs/live-checks.txt` | 跨买家读单、买家调仓库/客服/运营接口、未登录访问全部被拒；请求体伪造 buyer_id/user_id 无效，订单归属 token 身份（SQL 归属分布 u-buyer01×8/u-buyer02×4/u-buyer03×3 与下单行为一致） |
| C10 金额口径 | PASS | SQL C10 段；schema DESCRIBE | `logs/sql-checks.txt` | amount/price/qty 全部 bigint 整数分；今日订单 amount=price×qty 违规空集；支付金额=订单金额违规空集；改价期间快照不变（O20261113000018 保持 6000 快照，现价已回 5000） |

## 四、附录

### 1. 执行过的命令

```text
powershell -File scripts/smoke.ps1                 # S1 正常成交（exit=0）
powershell -File scripts/scenario-rush.ps1         # S2 20并发防超卖+超时释放（exit=0）
powershell -File scripts/scenario-refund.ps1       # S3 售前退款+渠道重试（exit=0）
powershell -File scripts/scenario-late.ps1         # S4 迟到支付入异常台账（exit=0）
powershell -File C:\Temp\acc-live.ps1              # 活体测试 A1/A2/A3/A5/A7/B5/C4/D（33 项全 PASS）
mysql ... -e "source C:/Temp/acc_checks.sql"       # C1~C10 静态取证
mysql ... -e "source C:/Temp/acc_final.sql"        # F0~F7 复核取证
```

### 2. 测试数据（今日）

- 订单 15 张：shipped×2（0001、0013）、to_ship×1（0012）、refunded×1（0008）、closed×11（timeout×6、cancel×5）。
- 支付 4 笔 success（10000×2 + 5000×2）；退款 3 张（presale success、late success、presale rejected）；异常台账 4 行；发货单 2 张。
- 汇总对账：实收 120000 分、实退 100000 分（API=SQL 明细）。

### 3. 关键日志

- `reports/consistency/logs/s1-smoke.txt` ~ `s4-late.txt`：四场景脚本完整输出
- `reports/consistency/logs/live-checks.txt`：33 项活体断言（渠道异常注入、拒绝路径、越权、改价下架、重复退款通知）
- `reports/consistency/logs/sql-checks.txt`、`sql-final.txt`：SQL 取证原始输出
