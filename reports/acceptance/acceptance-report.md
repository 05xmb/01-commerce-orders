# Agent 验收报告 — 题目 01：小型电商的订单交易、发货与售前退款

- 验收时间：2026-11-12
- 验收代码版本：`dev/01` @ `6f3d5456bf1839eb60e8e4d16a5eca37edc84619`（工作区干净，验收过程未改动任何代码）
- 验收依据：原始题面《Go项目业务场景题.md》第一部分共同要求 1–10 节 + 题目 01 全文（验收会话实际读取原文，未凭记忆）；`acceptance-check-01` Skill
- 验收方式：全部门禁与场景在验收会话本地重跑取证（gofmt/vet/test/覆盖率、S1–S4 场景脚本、B5/D 临时实测），不采信开发/测试会话的自述结论；代码证据均定位到 `file:line`
- 临时实测脚本 `scripts/_tmp_acceptance_b5.ps1` 执行后已删除，不入库
- **独立性声明（诚实记录）**：开发、测试、验收三个阶段由同一 AI 引擎在相互独立的会话上下文完成（符合"独立会话"字面要求），但并非独立 LLM/人工。为补强独立性，验收会话重跑了一切可重跑的证据。最终由学生本人复核裁决（AGENTS.md 第 3、8 节）

## 总判定

**有条件通过**：业务规则（A1–A8）、演示场景（B1–B5）、越权安全抽查（D）全部通过；通用要求 C1–C5、C7 通过/部分通过。遗留 **2 项整改（P1 口令明文、P2 文档断链）** 与 **1 项流程挂起（C6 部署后补验）**，整改复验后转为正式通过。

---

## A 项：业务规则逐条核验（题面三、业务要求 2–9）

| # | 规则（原文要点） | 结论 | 证据位置 | 判定说明 |
| --- | --- | --- | --- | --- |
| A1 | 下单快照、在售校验、库存占用不超卖 | 通过 | `internal/repo/mysql/mysql.go:105-113`（TryDeduct 条件更新含 `on_sale=1 AND stock>=?`）；`internal/service/order.go:18-58`（事务内建单+占用流水）、`:36-47`（快照落库）；单测 `TestCreateOrder_Success/InvalidQty/OutOfStockAndOffSale/ConcurrentNoOversell`、`TestOrderSnapshot_Immutable`（order_test.go:13-104,179） | S2 复跑：20 并发抢 5，成交+占用 ≤5 零超卖；改价不影响已建单由快照测试覆盖 |
| A2 | 取消/超时释放，重复关闭不重复释放 | 通过 | `order.go:96-119`（取消，本人校验+竞争复查）、`:67-93`（ClosePending 条件更新保证释放一次）、`:122-136`（服务端时间超时扫描）、`:139-147`（惰性关单）；单测 `TestCancelOrder_ReleaseOnce`、`TestCloseExpired`、`TestLazyCloseOnGet` | S2 超时回满、S4 关单释放复跑通过；流水 `占用1/释放1` 对平 |
| A3 | 支付通知鉴权、金额匹配、幂等、异常留痕 | 通过 | `internal/service/notify.go:28-31`（验签失败留痕）、`:34-43`（同 trade_no 幂等/冲突）、`:45-48`（未知订单）、`:52-55`（金额不符）；单测 `TestPayNotify_Success/Duplicate/Conflict/Rejected`（notify_test.go:12-92） | B5 实测：重复通知只记款一次；金额不符/未知订单/验签失败均可查询留痕 |
| A4 | 迟到支付不复活不占库，入异常台账，退款不回补 | 通过 | `notify.go:105-106,122-135`（closed→late 异常台账，不动订单/库存）；`refund.go:183-189`（迟到退款只记异常、不回补）；单测 `TestPayNotify_LateAfterClose/LateAfterTimeout`、`TestRefundException_Late` | S4 独立复跑：订单保持关闭、异常 1 条、restore=0 |
| A5 | 售前退款停发、审核拒绝可续发、备注必填、cs_id 留痕 | 通过 | `refund.go:27-33`（ToShip→RefundReview 条件更新）、`:46-48`（备注必填）、`:50-70`（拒绝回滚 ToShip+cs_id）；`shipment.go:20-32`（发货条件更新，与退款申请并发互斥）；单测 `TestConcurrent_RefundVsShip`、`TestShip_BlockedDuringReview`、`TestRefundReview_Reject`、`TestRefund_ReviewRecordsCS` | S3 复跑：申请后发货 409 拦截；并发二选一由并发测试保证 |
| A6 | 退款经渠道确认、失败待处理可重试、回补一次、重复通知幂等 | 通过 | `refund.go:112-205`（渠道结果确认）、`:144-154`（fail→failed_pending）、`:157-163`（MarkSuccess）、`:164-187`（回补恰一次）、`:139-142`（重复退款通知幂等）、`:100-109`（failed_pending/refunding 卡死重试入口）；单测 `TestRefundFlow_Presale`、`TestRefund_ChannelDown`、`TestRefund_RetryFromStuckRefunding` | S3 复跑：首败重试成功、restore 恰 1；B5 实测重复退款通知 restore 不增 |
| A7 | 发货唯一、已发货不受理售前退款 | 通过 | `shipment.go:20-32`（ToShip→Shipped 条件更新，重复发货 ErrStatus）；`refund.go:27-33`（Apply 同为条件更新，shipped 态失败）；单测 `TestShip_Success/Guard`、`TestRefundApply_ShippedRejected` | S1 发货成功、S4 不可发货断言通过 |
| A8 | 运营核查可穿透、汇总与明细一致、异常独立不可消 | 通过 | `internal/service/ops.go:23-53`（订单穿透：支付/退款/发货/异常/流水/通知日志）、`:62-77`（汇总）、`:186-190`（MarkRefundedByRefundNo，仅改异常台账）；单测 `TestOps_SummaryAndDetail` | B5 期间汇总 delta 断言通过；异常台账独立表，订单状态无法使其消失 |

## B 项：演示场景逐个核验（题面三、演示要求 1–5）

| # | 场景 | 结论 | 证据位置 | 判定说明 |
| --- | --- | --- | --- | --- |
| B1 | 正常下单→支付→待发货→发货→待收货 | 通过 | `scripts/smoke.ps1`（验收复跑 exit=0） | 全链路状态流转与题面一致 |
| B2 | 缺货与弃单：并发抢购/重复提交/超时关单释放 | 通过 | `scripts/scenario-rush.ps1`（验收复跑 exit=0） | 20 并发 5 成 15 拒、零超卖、超时回满、他人可购、现场清理 |
| B3 | 售前退款：停发/失败/重试/回补一次 | 通过 | `scripts/scenario-refund.ps1`（验收复跑 exit=0） | 409 拦截、首次失败转待处理、重试成功、回补恰 1 次 |
| B4 | 迟到支付：关单/迟到入异常/不复活/退款不回补 | 通过 | `scripts/scenario-late.ps1`（验收复跑 exit=0） | 本单流水计数权威断言（占用1/释放1/回补0）全过 |
| B5 | 操作竞争与重复通知：幂等/无幽灵异常/退款不重复 | 通过 | 验收临时实测（脚本已删，输出留存会话记录）；单测 `TestPayNotify_ConcurrentDuplicate_NoGhostException`、`TestPayNotify_AlreadyPaid`、`TestPayNotify_SecondPaymentGoesException`、`TestConcurrent_RefundVsShip` | 实测：重复支付记款增量=单次金额、三类异常注入可查、重复退款通知不重复退款 |

## C 项：通用要求核验（共同要求 1–10 节）

| # | 要求 | 结论 | 证据位置 | 判定说明 |
| --- | --- | --- | --- | --- |
| C1 | 核心数据落库，重启后业务结果可查 | 通过 | 验收实测：停掉并重启 `C:\Temp\trade-api.exe`/`pay-channel.exe`（环境变量经项目根 `.env` UTF-8 注入），重启前后订单快照对比 `refunded/10000/1退款/2流水/4通知日志` 完全一致；异常/任务状态同落库（notify/exception/refund 表） | 双服务真实重启，数据无丢失 |
| C2 | 页面功能与测试报告/验收清单同名同义 | 通过 | `web/src/views/`：Login/Shop/Orders/Admin（商品列表/抢购、订单、后台工作台、仓库/客服/运营）；`pnpm build` 4.85s exit=0 | 命名统一用"商品列表/抢购"，全仓仅 AGENTS.md 规则条文出现"秒杀"字样 |
| C3 | 代码质量与测试门禁 | 通过* | 验收重跑：`gofmt -l .` 无输出、`go vet ./...` exit=0、`go test ./... -count=1` 全部 ok、`go tool cover -func` 整体 **89.7%**（与 docs/test-report.md 一致；核心包明细见该报告覆盖率表，均 ≥85%） | *`-race` 本机无 gcc/cgo，AGENTS.md 第 6 节已记录，须 CI ubuntu 执行（遗留 P4）。未见删测试/弱化断言：`2632b4e` 未动任何 `_test.go`，`f8cc694` 未动生产代码 |
| C4 | 模拟支付渠道覆盖四类通知+异常注入 | 通过 | `cmd/channel/main.go`（7 类场景注入）；单测 `internal/channelclient/client_test.go`（5 例）+ `TestRefundNotify_BadSign`；实测 success/duplicate/amount_mismatch/unknown_order/bad_sign/late/fail 由 S3/S4/B5 覆盖 | 签名校验失败留痕 verify_ok=false 可查 |
| C5 | Git 流程与提交规范 | 通过 | `main` 仅 Initial commit（无直推）；`dev/01` 独立 13 提交；提交格式 `[类型] [版本号] 中文描述` 实测合规；`git ls-files` 仅含 `.env.example` 无凭据入库；`.gitignore:2` 忽略 `.env` | meegle 编号留空系学生决策豁免（AGENTS.md v0.5 第 5 节注，有留痕），非 AI 擅自放宽 |
| C6 | 流水线部署、健康检查、业务冒烟 | 证据缺失（流程挂起） | 部署属任务 7（T18–T20），验收时点尚未执行 | 部署后须对 main 合并版本重跑门禁+冒烟，并回填 AGENTS.md"应用最终对外访问地址"决策点 |
| C7 | 代码版本=测试版本=部署版本，命名一致 | 部分通过 | 测试与验收同锚定 `dev/01`；页面命名与测试报告一致 | 文档断链已闭环（P2：补建 `docs/service-accounts.md`）；部署版本待 C6 补验后闭环 |

## D 项：越权与安全抽查

| 检查 | 结论 | 证据位置 |
| --- | --- | --- |
| 跨买家查看/取消他人订单 | 通过（403） | 验收实测 D1/D2；单测 `TestCancelOrder_Forbidden`、`TestGetOrder_Forbidden` |
| 跨角色访问（仓库→运营接口） | 通过（403） | 验收实测 D3；单测 `TestManageForbid/TestSellerForbid/TestOpsForbid/TestOpsManageForbid`（handler_test.go）、`TestAuth` |
| 请求体伪造 user_id/buyer_id | 通过（归属=token 身份） | 验收实测 D4（伪造下单后归属仍为 buyer01，buyer02 读取 403）；`internal/handler/handler.go:78-102`（身份仅从 token 注入上下文） |
| 密钥/凭据保护 | 通过（一处遗留见 P1） | DB/渠道密钥走 `${ENV}`（`internal/config/config.go` os.ExpandEnv + `.env` 不跟踪）；签名密钥不硬编码；git 历史无 `.env`/凭据文件；第 10 节红线中"密钥禁入仓库/日志/截图"满足 |

## 证据缺口与整改清单

| 编号 | 级别 | 问题 | 整改要求 | 状态 |
| --- | --- | --- | --- | --- |
| P1 | M（交付红线口径） | 预置账号口令以明文字面量入库：`cmd/server/main.go` 种子、`web/src/views/Login.vue`、`scripts/*.ps1`（含 MySQL root 口令）、`.github/workflows/ci.yml` 冒烟登录。DB 内已哈希存储（`sign.HashPassword`），但仓库内明文与共同要求第 10 节"仅存部署环境与本地配置"不符 | 种子口令改由环境变量/本地配置注入，仓库内文档/脚本/前端改占位符或读 env；修复后复测并更新本报告 | **已整改**（整改提交见 dev/01）：种子读 `SEED_PASSWORD` 环境变量（缺失启动失败）、脚本读 env/.env、前端无默认口令、CI 走 `secrets.SEED_PASSWORD`、compose 透传、`.env.example` 占位、测试夹具换假口令。复验：`git grep` 跟踪文件仅剩运单号样例 `SF123456789`（非凭据），全测试通过，本地 `.env`（gitignore）注入后双服务启动正常 |
| P2 | L（文档） | 初版报告误述"`docs/test-report.md` 引用的 `docs/service-accounts.md` 不存在"——复核结论：全仓 `.md` 文件无任何相对链接，该引用并不存在（报告自身误述，据实更正） | 补建账号权限说明文档；校验全仓文档相对路径大小写 | **已闭环**：补建 `docs/service-accounts.md`（账号权限矩阵 + 权限边界 + 口令管理口径，零明文）；链接校验结论：全仓 md 互引均为文字性提及，无失效链接、无大小写不一致 |
| P3 | 流程 | C6 部署未执行，AGENTS.md"应用最终对外访问地址"未回填 | 学生裁决流程：部署合并完成后，针对 main 合并版本执行本报告 A/B 验收用例核验；核验通过后回填 AGENTS.md 线上应用访问地址，更新本报告 C6/C7 为通过 | 挂起（流程已固化，待任务 7 执行） |
| P4 | 遗留 | `go test -race` 本机无 gcc/cgo 未执行 | CI ubuntu 流水线执行并留存输出（AGENTS.md 第 6 节已记录口径） | 待 CI |

## 附录：验收会话门禁实测摘要

```text
gofmt -l .                    → 无输出（通过）
go vet ./...                  → exit=0
go test ./... -count=1 -coverprofile=coverage.out → 全部 ok
go tool cover -func           → total: 89.7%
pnpm build (web)              → ✓ built in 4.85s, exit=0
git branch -a                 → main（仅 Initial commit）/ dev/01（13 提交，领先 main）
git ls-files | grep 凭据      → 仅 .env.example
```

- 逐项口径：任何证据缺失项不判通过（C6 已按此挂起）；所有结论以可复现命令输出为准（AGENTS.md 第 8 节）。
