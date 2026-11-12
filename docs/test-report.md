# 测试报告 — 题目01 订单交易、发货与售前退款

> 阶段：Agent 测试（阶段 5）｜日期：2026-11-12｜对象分支：`dev/01`
> 测试结论：**通过**（1 项高危、2 项中危、9 项低危缺陷已全部修复并回归验证；覆盖率门禁达标；S1~S4 端到端复测全过）

## 0. 流程偏离记录（诚实性声明）

- 考试流程要求"测试 Agent 为独立会话"。本测试**在开发会话内执行**（学生明确要求在此会话完成任务 5），属流程偏离。
- 缓解措施：引入**全新上下文的独立审计子代理**（不携带开发记忆、只读题面与代码）做对抗性审计，产出 11 项疑点（1 高 / 2 中 / 8 低）；主测试角色对每条疑点逐一读代码复核后再修复，未盲信子代理结论。
- 本报告如实记录全部证据，不伪造独立会话痕迹。若教师要求补做独立会话测试，可直接复跑本文所列脚本与用例。

## 1. 测试范围与方法

| 层次 | 方法 | 工具/证据 |
|---|---|---|
| 对抗性代码审计 | 独立子代理按题面 8 条业务规则逐条攻击（并发、幂等、越权、状态机、事务） | 11 项疑点清单（第 2 节） |
| 单元测试 | 全量重跑 + 针对性回归用例（并发 16 协程、故障注入、状态机边界） | `go test ./... -count=1` |
| 覆盖率门禁 | 整体 ≥85%、核心业务包分别 ≥85% | `go tool cover -func` |
| 端到端场景 | 真实 MySQL（`电商订单表1`）+ Redis + 双服务，S1~S4 全链路复测 | `scripts/smoke.ps1`、`scenario-rush/refund/late.ps1` |

## 2. 缺陷清单与闭环（审计 11 项 + 测试期新发现 3 项）

### 高危

| # | 缺陷 | 位置 | 修复 | 回归用例 |
|---|---|---|---|---|
| H-1 | 并发重复支付通知产生幽灵异常台账：两条同 trade_no 并发通知都通过预检，一个 MarkPaid 成功、另一个走迟到分支入异常台账 → 可被误退款且照常发货（违反 R3/R4） | `internal/service/notify.go` | MarkPaid 失败（errNotPending）后复查支付记录：同单同额→幂等重复；否则才入异常台账。修复中另发现**旧快照陷阱**（事务后 `repos` 为旧状态），改为 `s.d.Store.Repos()` 重取 | `TestPayNotify_ConcurrentDuplicate_NoGhostException`（16 并发恰好 1 成功、0 幽灵异常） |

### 中危

| # | 缺陷 | 位置 | 修复 | 回归用例 |
|---|---|---|---|---|
| M-1 | 退款在"批准后、渠道请求前"崩溃则卡死 `refunding`，无恢复入口（`MarkRefunding` 只认 `failed_pending`） | `repo/mysql`、`repo/fake`、`service/refund.go` | `MarkRefunding` 接受 `failed_pending ∪ refunding`（卡住态），`Retry` 作为恢复入口 | `TestRefund_RetryFromStuckRefunding` |
| M-2 | 已支付订单收到**不同交易号**成功通知（买家重复付款）被静默当重复丢弃，第二笔资金不可见、不可退（违反 R3） | `service/notify.go` | 同 trade_no→幂等；不同 trade_no→入异常台账，客服可发起 `late` 退款（不回补库存） | `TestPayNotify_SecondPaymentGoesException` |

### 低危

| # | 缺陷 | 修复 | 回归/证据 |
|---|---|---|---|
| L-1 | 渠道退款失败后本地 `MarkFailedPending` 失败被静默吞掉（`return nil`） | 错误透传（组合错误信息） | 故障注入用例 |
| L-2 | 退款审核未记录审核客服，R8 审计链缺失 | `cs_id` 贯穿 schema/types/mysql/fake/service（Approve/Reject/异常退款） | `TestRefund_ReviewRecordsCS` |
| L-3 | 买家看不到退款进度与拒绝原因 | `OrderView` 增加 `Refunds`（含 cs_id/cs_note） | `TestOrderGet_IncludesRefunds` |
| L-4 | 非法退款结果通知不留任何日志 | 记 `invalid_result` 日志后返回 ErrParam | `TestRefundNotify_InvalidResult_Logged` |
| L-5 | 退款批准/拒绝与成功落账的 `SetStatusCond` 返回 `ok` 未检查，订单脱链时账实分离 | 检查 `ok`，不满足即 `ErrStatus`/回滚 | 服务层条件更新全部核对 |
| L-6 | 超时关单失败静默（仅计数），不可观测 | `log.Printf` 记录失败订单号与原因 | `service/order.go` |
| L-7 | 惰性超时 `closeOne` 失败被忽略，可能把已过期订单标为已支付 | 关闭失败即返回错误 | `service/notify.go` |
| L-8 | 并发支付获胜时买家取消仍返回成功，误导"已取消" | `Cancel` 后复查订单状态，非 closed 返回 `ErrStatus` | `TestCancel_AfterPaid_ReturnsConflict` |
| T-1 | **测试期新发现**：idgen 订单号/退款单号 6 位纯随机后缀生日悖论碰撞（千次抽样碰撞率约 39%），测试间歇失败、生产有唯一键冲突风险 | 改进程内原子自增序列（唯一性优先） | `internal/pkg/idgen`，S1~S4 订单号已呈序列化（O…000017） |
| T-2 | **测试期新发现**：两条过期测试断言（同 trade_no 重复通知语义、refunding 重试语义）随 H-1/M-1 修复变化 | 更新断言为新语义 | `notify_test.go`、`fake_test.go`、`handler_test.go` |
| T-3 | **测试期新发现（脚本缺陷，非产品缺陷）**：S2 结尾遗留待支付验证单，其超时释放落进 S4 的 33s 等待窗口，触发 S4 绝对库存断言误报。取证：O7/O10 各自占用1/释放1，产品行为正确 | S2 结尾取消验证单（现场清理）；S4 断言以本单流水为权威红线（占用1/释放1/回补0），绝对库存对并发释放容错 | 复跑 S2/S4 通过 |

开发期（任务 4）另修复 2 项真实缺陷：渠道退款号信封解析错位（`channelclient` 平铺 vs `data` 嵌套）、异常支付重复退款窗口（`AttachRefund` 引入 `refunding` 中间态防重）。

## 3. 覆盖率门禁（`go test ./... -count=1 -coverprofile`）

| 包 | 覆盖率 | 门禁 |
|---|---|---|
| internal/service（业务核心） | 85.9% | ≥85% ✅ |
| internal/handler | 92.7% | ≥85% ✅ |
| internal/repo/mysql | 97.7% | ≥85% ✅ |
| internal/repo/fake | 98.2% | ≥85% ✅ |
| internal/channelclient | 90.0% | ≥85% ✅ |
| internal/config | 95.8% | ≥85% ✅ |
| internal/pkg/idgen / sign | 90.9% / 100% | ✅ |
| internal/tokstore | 100% | ✅ |
| cmd/server（main 装配） | 25.4% | 豁免（AGENTS.md 约定仅 main 入口不计核心包） |
| **整体** | **89.7%** | **≥85% ✅** |

静态检查：`gofmt -l .` 无输出；`go vet ./...` 无告警；`go test ./... -count=1` 全部 ok。

## 4. 端到端复测（修复后二进制 + 真实 MySQL/Redis）

| 场景 | 结果 | 关键红线验证 |
|---|---|---|
| S1 正常成交 | ✅ | 下单→异步通知→待发货→发货→买家查物流→运营穿透（订单/支付/发货/流水/通知日志齐全） |
| S2 缺货与弃单 | ✅ | 20 并发抢 5：成功 5 / 拒绝 15 / 零超卖；33s 超时回满；他人可购；现场已清理 |
| S3 售前退款 | ✅ | 退款中发货被 409 拦截；渠道首次失败→重试成功（attempt=2）；库存回补恰好 1 次 |
| S4 迟到支付 | ✅ | 订单不复活、本单无新增流水、异常台账可查、客服退款 type=late、**回补 0**、流水 占用1/释放1/回补0 |

## 5. 遗留事项

1. `go test -race` 需 cgo/gcc，本机（Windows）无 gcc，将在 CI ubuntu runner 执行（AGENTS.md 命令基线已注明）。
2. 独立会话补测（如教师要求）：复跑本文脚本即可，脚本均幂等、自带断言。
3. 服务当前以后台运行（trade-api:8888 / pay-channel:8902），供人工体验与验收阶段使用。
