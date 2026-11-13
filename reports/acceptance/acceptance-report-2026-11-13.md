# Agent 验收复审报告 — 题目 01：小型电商的订单交易、发货与售前退款

- 验收时间：2026-11-13（复审会话；对比基线为 2026-11-12 T17 验收报告 `acceptance-report.md`）
- 验收代码版本：`dev/01` @ `e73db49`（T17 基线 `6f3d545` 之后的改动逐笔核过：`392483f` 文档补建、`c73b6bf` 渠道端口 8901→8902、`e73db49` 入口脚本行尾+.gitattributes、工作区仅 `faildb_test.go/misc_test.go` 口令夹具 "123456"→"ut-passw0rd"（`git diff` 核实断言完好，无弱化）；生产代码 `internal/service|handler|repo` 自 T17 后未变动）
- 验收依据：原始题面《Go项目业务场景题.md》题目 01 + 第一部分共同要求；AGENTS.md v0.5；`acceptance-check-01` + `order-trade-consistency-check` Skill
- 验收方式：全部门禁、场景、SQL 取证与活体测试在本会话实际执行并落盘原始输出（`reports/quality/2026-11-13-*`、`reports/consistency/logs/*`）；不采信开发/测试/T17 自述数值——凡复测不符均按实测记录
- **独立性声明（诚实记录）**：开发、测试、T17 验收、本次复审均为同一 AI 引擎的相互独立会话上下文；本次以"全部重跑取证"补强独立性，并实际发现自述数值不可复现 2 处（见 N-1/N-2）。最终由学生本人复核裁决（AGENTS.md 第 3、8 节）

## 总判定

**有条件通过（发布阻断）**：

- 全部通过：业务规则 A1~A8、演示场景功能 B1~B5、数据一致性 C1~C10（独立报告 `reports/consistency/2026-11-13-consistency.md`）、越权安全 D。
- 阻断项：C3 质量门禁按 AGENTS 第 6 节"语句覆盖率"口径**未达标**（N-1：`internal/service` 84.9% < 85%）。
- 待补项：演示视频未录（N-4）、1Panel 部署与对外地址回填（延续 T17 P3）、CI 运行记录留存（延续 T17 P4）。

N-1 整改（或学生/教师裁定覆盖率口径并同步修正 AGENTS 文字与 CI 门禁算法）+ N-4 完成后，对 main 合并版本复验转正式通过。**依 AGENTS 第 6 节，质量检查未全过前不得继续发布。**

> **整改进展（2026-11-13 同日闭环）**：N-1（覆盖率口径与门禁）、N-2（gofmt 行尾）、N-3（文档数值）已全部整改并复测闭环（见整改清单 R-1~R-3）：service 语句覆盖率 84.9%→**99.7%**、整体 93.6%，`gofmt -l .` 无输出，ci.yml 门禁改语句口径。**发布阻断解除**；N-4 中 CI 运行记录（含 -race）已于 2026-11-13 留存闭环（run 21805215519，`reports/quality/2026-11-13-ci-run.md`），仅余演示视频与 1Panel 部署/地址回填待补，完成后对 main 合并版本复验转正式通过。最终由学生本人复核裁决。

---

## A 项：业务规则逐条核验（题面三、业务要求 2~9）

| # | 规则（原文要点） | 结论 | 证据位置 | 判定说明 |
| --- | --- | --- | --- | --- |
| A1 | 下单商品/价格快照；改价、上下架不影响已建订单金额；只购在售且足量 | 通过 | 活体 Batch4（`logs/live-checks.txt`）：改价 5000→6000 后旧单金额不变、新单按 6000、超量 999 拒绝、下架后拒绝；`sql-final.txt` F0：O20261113000018 保持 6000 快照（现价已回 5000）；S2 占用即减可售（`logs/s2-rush.txt`） | 行为级实测全过；快照落库代码定位见 T17 报告 A1（生产代码未变动，引用有效） |
| A2 | 主动取消/超时关闭释放占用，以服务端时间判定，重复关闭不重复释放 | 通过 | 活体 Batch2：取消成功、重复关闭被拒、流水"占用1/释放1"（`logs/live-checks.txt`）；S2：33s 超时回满（`logs/s2-rush.txt`）；SQL C2 段 7 张关闭单各恰好 1 条 release（`logs/sql-checks.txt`）；教学时钟 `etc/server.yaml` PayTimeoutSeconds=30 动态生效（生产 600 口径见 AGENTS 第 2 节） | 服务端时间判定由 S2/S4 实测（33s 后关闭）佐证 |
| A3 | 支付通知鉴权、匹配订单与金额；重复通知不重复记款；一号不得两单；拒绝有记录 | 通过 | 活体 Batch1（`logs/live-checks.txt`）：amount_mismatch/bad_sign/unknown_order 注入均不改状态且留痕、duplicate 通知仅记款一次；SQL F1：payment 行=1、trade_no=1、四类通知结果齐全；C3 段：trade_no 归属唯一（空集）、关闭单无 payment 行 | 验签失败 verify_ok=false 留痕可查（cmd/channel 签名实现本次已读源码核实） |
| A4 | 迟到支付：不复活、不重占、入异常台账；该退款不回补库存 | 通过 | S4 独立复跑（`logs/s4-late.txt`）：订单保持 closed、流水"占用1/释放1/回补0"、异常台账 1 条、退款 type=late restore=0；SQL C4 段（`logs/sql-checks.txt`） | 与红线 5 逐句对应 |
| A5 | 售前退款停发；拒绝后可续发；同意/拒绝均留原因与审核客服 | 通过 | S3（`logs/s3-refund.txt`）：审核中发货 409；活体 Batch2：拒绝后回到 to_ship 并成功发货；SQL F3（`logs/sql-final.txt`）：3 张退款单 cs_id=u-cs01 + cs_note 均非空（含拒绝单"验收：拒绝并留原因"） | 申请期间暂停发货、拒绝可续发均为行为级实测 |
| A6 | 退款经渠道确认；失败保留待处理可重试；回补一次；重复退款通知不重复退款；买家可见结果 | 通过 | S3：首败→failed_pending→重试成功 attempt=2、restore 恰 1；活体 Batch5：重复退款通知记 duplicate、restore 仍 1（`logs/live-checks.txt`）；SQL C6/F2 段 | 买家可见退款结果由订单详情含 Refunds 实现（T17 L-3 闭环，测试 `TestOrderGet_IncludesRefunds`） |
| A7 | 仓库仅对合法待发货单登记一次发货；已发货不受理售前退款 | 通过 | 活体 Batch2：重复发货被拒、已发货申请退款被拒（`logs/live-checks.txt`）；S1 单次发货成功（`logs/s1-smoke.txt`）；SQL C7 段无一单多发 + `shipment.order_no` UNIQUE | 重复/非法状态均行为级实测 |
| A8 | 运营订单穿透、汇总与明细一致、异常独立不可消 | 通过 | 活体 Batch1/4：`/api/ops/order-detail` 穿透支付/流水/通知日志；SQL F5：API 汇总=SQL 明细和（120000/100000）；C8 段：异常台账 4 行独立留存，无订单状态可使其消失 | 红线 10 满足；实收/实退口径观察见一致性报告 O-3 |

## B 项：演示场景逐个核验（题面三、演示要求 1~5）

| # | 场景 | 结论 | 证据位置 | 判定说明 |
| --- | --- | --- | --- | --- |
| B1 | 正常下单→支付→待发货→发货→待收货 | 通过 | `logs/s1-smoke.txt`（exit=0） | 全链路状态流转与题面一致 |
| B2 | 缺货与弃单：并发抢购/重复提交/超时关单释放/释放后他人购 | 通过 | `logs/s2-rush.txt`（exit=0） | 20 并发 5 成 15 拒、零超卖、超时回满、他人可购、现场清理 |
| B3 | 售前退款：停发/失败/重试/回补一次/拒绝续发 | 通过 | `logs/s3-refund.txt` + 活体 Batch2 | 409 拦截、首败转待处理、重试成功、回补恰 1、拒绝后发货成功 |
| B4 | 迟到支付：关单/入异常/不复活/退款不回补 | 通过 | `logs/s4-late.txt`（exit=0） | 本单流水权威断言（占用1/释放1/回补0）全过 |
| B5 | 重复支付/重复退款通知、越权访问、正常支付走正常发货 | 功能通过（视频待补） | 活体 Batch1/3/5（`logs/live-checks.txt`）：重复支付记款一次、重复退款不重复退款、7 项越权全拒；S1 正常发货 | **演示视频未录制**（题面要求），记入 N-4 待补 |

## C 项：通用要求核验（共同要求 1~10 节）

| # | 要求 | 结论 | 证据位置 | 判定说明 |
| --- | --- | --- | --- | --- |
| C1 | 题面功能/规则全覆盖 | 通过 | `web/src/views/`：Shop（商品列表/抢购/下单/模拟支付）、Orders（订单列表/取消/退款/运单）、Admin（仓库/客服/运营）、Login；状态机六态实测（pending/to_ship/shipped/refund_review/refunded/closed 均出现于取证数据）；售前整单范围/仅单件少量/预置账号 token 登录均落地 | 用语统一"商品列表/抢购"，未用"秒杀" |
| C2 | 架构：业务服务+模拟渠道+数据库+前端分离 | 通过 | handler/service/repo 三层；渠道独立进程（`cmd/channel/main.go`，通知驱动+HMAC 验签）；超时关单在业务服务内（`internal/service/order.go`）；SPA 由 Nginx 独立托管反代 /api、/channel/ | 与 AGENTS 2.2 架构一致 |
| C3 | 代码质量与测试门禁 | **不通过** | 见整改 N-1/N-2/N-3：service 语句覆盖率 84.9%<85%；"gofmt 无输出"不可复现且原证据文件缺失；报告数值与实测不符 | 详见附录 1 门禁实测；`go vet`/全量单测/前端构建本次实测通过 |
| C4 | 并发与竞争检测 | 通过（race 本机缺证） | S2 20 并发实测零超卖；并发单测（16 协程重复通知、退款/发货竞争）；`go test -race` 本机无 gcc/cgo 不可执行（AGENTS 第 6 节已注明），CI -race 历史检出并修复 fake.Store 竞态（commit e90cf90） | race 原始输出须随 CI 记录留存（N-4） |
| C5 | 演示准备 | 部分通过 | 20 并发+库存 5 脚本（scenario-rush）、30s 教学时钟、四类通知+三类异常注入、S1~S4 脚本幂等可复跑（本次全部 exit=0） | 演示视频待补（N-4） |
| C6 | 流水线与部署 | 证据缺失（挂起） | `.github/workflows/ci.yml` 齐全（单测+覆盖率门禁+前端构建+tag 发布+1Panel 升级+冒烟）；本机无 gh 无法核验 Actions 运行记录；1Panel 部署未执行（镜像 tar 已备 `C:\Temp\trade-app-v0.1.0.tar`，等 SSH 与安全组端口） | 延续 T17 P3/P4；部署后须对 main 合并版本重跑 A/B 冒烟并回填 AGENTS"应用最终对外访问地址" |
| C7 | 安全：密钥不入库、越权防线、渠道密钥 | 通过 | 全仓 grep 无硬编码凭据（仅测试夹具假口令）；`.env` 不跟踪、`.env.example` 占位；SEED_PASSWORD 环境变量注入（T17 P1 已闭环）；渠道密钥走 env（`cmd/channel/main.go` 缺失即拒启动） | 截图/视频脱敏要求随 N-4 录制时执行 |

## D 项：越权与安全抽查

| 检查 | 结论 | 证据位置 |
| --- | --- | --- |
| 跨买家查看他人订单 | 通过（拒绝） | 活体 Batch3（`logs/live-checks.txt`）；单测 `TestGetOrder_Forbidden` |
| 买家调仓库/客服/运营接口 | 通过（全部拒绝） | 活体 Batch3 |
| 未登录访问 | 通过（拒绝） | 活体 Batch3 |
| 请求体伪造 buyer_id/user_id | 通过（归属=token 身份） | 活体 Batch3：伪造下单后归属仍 u-buyer01；`internal/handler` 身份仅从 token 解析（禁信请求体，见 T17 D 引用，代码未变动） |

## 证据缺口与整改清单（本次复审新增；T17 遗留项状态同步）

| 编号 | 级别 | 问题 | 整改要求（闭环路径） | 状态 |
| --- | --- | --- | --- | --- |
| N-1 | M（质量门禁红线） | `internal/service` 语句覆盖率实测 **84.9%**（320/377，in-package 与全测试聚合两口径一致）< 85%；三处口径/数值并存互相矛盾：docs/test-report.md 85.9%、AGENTS 第 6 节记录 91.1%、ci.yml 分包门禁逐函数平均 92.97%——其中 85.9% 两种语句口径均**不可复现**，ci.yml 的 awk 平均法会掩盖语句口径不达标 | ①补 `internal/service` 单测使语句口径 ≥85%（`go tool cover -func` total 复验）；或②学生/教师裁定口径：若采纳函数平均口径，须同步修正 AGENTS 第 6 节文字（现写明"语句覆盖率"）与 docs 数值，并说明理由留痕；③ci.yml 分包门禁改为语句口径（按 numStmt 加权）或与裁定一致 | **已闭环**（2026-11-13，见整改闭环记录 R-1） |
| N-2 | L（证据缺失/口径） | test-report 与 T17 报告声称"`gofmt -l .` 无输出"，本工作区复测**列出 42 个 .go 文件**（根因：Windows 工作区 CRLF、git 索引 LF；`.gitattributes` 仅固定 *.sh/*.ps1）；T17 引用的原始输出文件 `reports/quality/2026-11-12-gofmt-vet.txt` **不存在** | ①`.gitattributes` 增补 `*.go text eol=lf`（或文档注明 Windows 口径差异与 CI Linux 口径）；②留存原始输出（本次已补 `reports/quality/2026-11-13-gofmt-vet.txt`：vet exit=0） | **已闭环**（2026-11-13，见 R-2） |
| N-3 | L（文档） | docs/test-report.md 覆盖率数值（service 85.9%/整体 89.7%/cmd/server 25.4%/fake 98.2%）与当前版本实测（84.9%/90.2%/23.9%/98.8%）不一致且未标注测量版本 | 报告数值标注 commit 哈希，或按复验实测更新；随 N-1 口径一并修订 | **已闭环**（2026-11-13，见 R-3） |
| N-4 | L（交付完备，含 T17 P3/P4 延续） | ①演示视频未录制；②GitHub Actions 运行记录本机不可核验（gh 未装，含 -race 输出）；③1Panel 部署未执行、"应用最终对外访问地址"未回填 | ①按题面录制演示视频（注意脱敏）；②部署后对 main 合并版本重跑质量门禁+A/B 冒烟，留存 CI 运行输出（含 race）；③回填 AGENTS.md 决策点并更新本报告 C6/C7 | **部分闭环**：②已闭环（2026-11-13 dev/01 推送触发 run 21805215519 成功，quality 含 -race，记录在 `reports/quality/2026-11-13-ci-run.md`）；①③挂起/待补 |
| — | 已闭环（T17） | P1 预置口令明文入库、P2 文档断链 | T17 整改已复验：SEED_PASSWORD 环境变量注入、`docs/service-accounts.md` 补建（commit 392483f） | 已闭环（本次抽查确认：全仓跟踪文件无凭据明文） |

### 整改闭环记录（2026-11-13，按 AGENTS 第 8 节"问题描述→修正要求→修复版本→复测结果"留痕）

**R-1（对应 N-1，发布阻断解除）**
- 问题：`internal/service` 语句覆盖率 84.9%（320/377）< 85% 门禁；三处口径/数值矛盾；ci.yml 逐函数平均法掩盖语句口径不达标。
- 修正要求：补单测使语句口径 ≥85%；ci.yml 门禁改语句口径；文档数值同步。
- 修复版本：新增 42 个单测（`internal/service/covergap_test.go`、`covergap_notify_test.go`、`covergap_refund_test.go`、`covergap_misc_test.go`，含竞态夹具 `raceStore`：事务前置改写已提交状态模拟并发获胜方，兼容 fake 事务快照制）；`ci.yml` 分包门禁改为 numStmt 加权；`docs/test-report.md` §3、`AGENTS.md` §6 回填整改后实测并声明语句口径。无删除/弱化既有测试。
- 复测结果：`go test ./... -count=1 -coverprofile` → service **99.7%（376/377）**、整体 **93.6%（1533/1638）**（语句口径）；门禁 awk 程序以 git awk 对同一 profile 仿真输出 service 100%（取整）/handler 93%/mysql 98%，全部 ≥85%；全量单测 ok。

**R-2（对应 N-2）**
- 问题：`.gitattributes` 未固定 `*.go` 行尾，Windows 工作区 CRLF 使 `gofmt -l .` 列出 42 个文件，"无输出"声称不可复现。
- 修正要求：`.gitattributes` 增补 `*.go text eol=lf`；工作区归一；留存原始输出。
- 修复版本：`.gitattributes` 追加 `*.go text eol=lf`；46 个 .go 文件行尾归一 LF。
- 复测结果：`gofmt -l .` **无输出**（exit=0）；`go vet ./...` exit=0；输出留存 `reports/quality/2026-11-13-gofmt-vet.txt`（整改后追加复测记录）。

**R-3（对应 N-3）**
- 问题：docs/test-report.md 覆盖率数值与实测不一致且未标注口径。
- 修正要求：按复验实测更新并随 N-1 口径修订。
- 修复版本：test-report.md §3 整表按语句口径回填（标注整改前 84.9% 对比）；AGENTS.md §6 保留 2026-11-12 历史值并追加 2026-11-13 复测行。
- 复测结果：文档数值与 `C:\Temp\covfinal.out` 实测逐包一致（service 99.7%/handler 92.7%/mysql 97.7%/fake 98.8%/config 95.8%/channelclient 90.0%/整体 93.6%）。

**遗留（N-4 不变）**：演示视频、CI 运行记录（含 -race）、1Panel 部署与对外地址回填仍待补；完成后对 main 合并版本复验转正式通过。

## 附录 1：本次门禁实测摘要（2026-11-13，原始输出在 `reports/quality/`）

```text
gofmt -l .                     → 列出 42 个 .go 文件（CRLF 根因，与"无输出"声称不符，N-2）
go vet ./...                   → exit=0
go test ./... -count=1         → 全部 ok
go test ./... -coverprofile    → 整体 90.2%（语句口径）
  internal/service             → 84.9%（320/377 语句，两口径一致）← 门禁未达，N-1
  internal/handler             → 92.7%   internal/repo/mysql → 97.7%
  internal/repo/fake           → 98.8%   internal/config     → 95.8%
  internal/channelclient       → 90.0%   internal/tokstore   → 100%
  cmd/channel → 81.6%（main 装配，豁免口径同 AGENTS 第 6 节）
go test -race                  → 本机不可执行（-race requires cgo，无 gcc）；CI 口径执行
pnpm build (web)               → exit=0
S1~S4 场景脚本                  → 全部 exit=0（logs/s1~s4）
活体测试 33 项                   → 全部 PASS（logs/live-checks.txt）
```

## 附录 2：与 T17 验收报告的结论对比

| 项 | T17（2026-11-12，基线 6f3d545） | 本次复审（2026-11-13，e73db49） |
| --- | --- | --- |
| A1~A8 / B1~B5 / D | 全部通过 | 全部通过（全部场景与活体重跑复核一致） |
| C3 门禁 | 通过*（引用 85.9%/89.7%，gofmt 无输出） | **不通过**：service 84.9% 不可达门禁，85.9% 声称不可复现（N-1/N-2/N-3） |
| C6 部署 | 证据缺失（挂起 P3） | 证据缺失（挂起，延续） |
| P1/P2 | 整改闭环 | 抽查确认已闭环 |
| 总判定 | 有条件通过 | 有条件通过（发布阻断，新增 N-1） |

- 逐项口径：无证据不判通过；自述数值复测不符按实测记录（AGENTS 第 8 节）。

## 附录 2：整改后复测摘要（2026-11-13 同日，R-1~R-3 闭环取证）

```
gofmt -l .                     → 无输出（exit=0；46 个 .go 已归一 LF，.gitattributes 固定 *.go eol=lf）← R-2 闭环
go vet ./...                   → exit=0
go test ./... -count=1         → 全部 ok（新增 42 个 service 补测同批通过）
go test ./... -coverprofile    → 整体 93.6%（1533/1638 语句口径）← R-1 闭环
  internal/service             → 99.7%（376/377，整改前 84.9%）
  internal/handler             → 92.7%   internal/repo/mysql → 97.7%
  internal/repo/fake           → 98.8%   internal/config     → 95.8%
  internal/channelclient       → 90.0%   cmd/channel         → 81.6%（豁免）
  cmd/server                   → 23.9%（main 装配豁免）
ci.yml 门禁 awk 仿真（git awk，同一 profile）→ service 100%（取整）/ handler 93% / mysql 98%，全部 ≥85%
```

- 原始覆盖率 profile：`C:\Temp\covfinal.out`（临时位置；CI 以 `-coverprofile=coverage.out` 现场生成，数值可由上列命令复现）。
- 无测试删除/断言弱化；口径统一为语句数加权（numStmt），test-report §3 与 AGENTS §6 已同步声明。
