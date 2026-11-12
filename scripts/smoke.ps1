# 冒烟脚本：S1 正常成交链路（下单→支付→发货→物流→运营穿透核查）
# 用法：powershell -File scripts/smoke.ps1 [-Base http://127.0.0.1:8888] [-Channel http://127.0.0.1:8902]
param(
  [string]$Base = "http://127.0.0.1:8888",
  [string]$Channel = "http://127.0.0.1:8902"
)
$ErrorActionPreference = "Stop"

# 口令经环境变量注入（本地 .env / 部署运行环境），仓库不存明文口令
if (-not $env:SEED_PASSWORD) {
  $envPath = Join-Path (Split-Path -Parent $PSScriptRoot) '.env'
  if (Test-Path $envPath) {
    Get-Content -Encoding UTF8 $envPath | ForEach-Object {
      if ($_ -match '^\s*([^#=]+)=(.*)$') { Set-Item -Path ('env:' + $matches[1].Trim()) -Value $matches[2].Trim() }
    }
  }
}
if (-not $env:SEED_PASSWORD) { throw '缺少 SEED_PASSWORD：请在本地 .env 或运行环境注入（仓库不存明文口令）' }

function Login($user) {
  $json = @{ username = $user; password = $env:SEED_PASSWORD } | ConvertTo-Json -Compress
  $r = Invoke-RestMethod -Method Post "$Base/api/login" -ContentType 'application/json; charset=utf-8' `
    -Body ([Text.Encoding]::UTF8.GetBytes($json))
  return @{ Authorization = "Bearer $($r.data.token)" }
}

Write-Host "== 健康检查 ==" -ForegroundColor Cyan
$h1 = Invoke-RestMethod "$Base/healthz"
$h2 = Invoke-RestMethod "$Channel/channel/healthz"
if ($h1.code -ne 0 -or $h2.code -ne 0) { throw "服务未就绪" }
Write-Host "  trade-api / pay-channel 均健康"

Write-Host "== [1] 买家下单 ==" -ForegroundColor Cyan
$bh = Login "buyer01"
$o = Invoke-RestMethod -Method Post "$Base/api/orders/create" -Headers $bh -ContentType 'application/json' `
  -Body (@{ product_id = "P1"; qty = 1 } | ConvertTo-Json -Compress)
$no = $o.data.order_no
Write-Host "  订单 $no 金额 $($o.data.amount) 状态 $($o.data.status)"
if ($o.data.status -ne "pending") { throw "下单状态错误" }

Write-Host "== [2] 渠道支付（成功场景，异步通知） ==" -ForegroundColor Cyan
$pay = Invoke-RestMethod -Method Post "$Channel/channel/pay" -ContentType 'application/json' `
  -Body (@{ order_no = $no; amount = $o.data.amount; scenario = "success" } | ConvertTo-Json -Compress)
Write-Host "  渠道受理 trade_no=$($pay.data.trade_no)"
Start-Sleep -Milliseconds 1200

$o2 = Invoke-RestMethod "$Base/api/orders/get?order_no=$no" -Headers $bh
Write-Host "  通知后订单状态: $($o2.data.status)"
if ($o2.data.status -ne "to_ship") { throw "支付后应为 to_ship，实际 $($o2.data.status)" }

Write-Host "== [3] 仓库发货 ==" -ForegroundColor Cyan
$wh = Login "wh01"
$whList = Invoke-RestMethod "$Base/api/wh/orders" -Headers $wh
$mine = $whList.data | Where-Object { $_.order_no -eq $no }
if (-not $mine) { throw "仓库待发货列表找不到该单" }
$shipJson = @{ order_no = $no; carrier = "顺丰速运"; tracking_no = "SF123456789" } | ConvertTo-Json -Compress
Invoke-RestMethod -Method Post "$Base/api/wh/ship" -Headers $wh -ContentType 'application/json; charset=utf-8' `
  -Body ([Text.Encoding]::UTF8.GetBytes($shipJson)) | Out-Null
Write-Host "  已登记发货 顺丰速运 SF123456789"

Write-Host "== [4] 买家查看物流 ==" -ForegroundColor Cyan
$o3 = Invoke-RestMethod "$Base/api/orders/get?order_no=$no" -Headers $bh
if ($o3.data.status -ne "shipped") { throw "应为 shipped，实际 $($o3.data.status)" }
Write-Host "  状态 shipped，物流 $($o3.data.shipment.carrier) $($o3.data.shipment.tracking_no)"

Write-Host "== [5] 运营穿透核查 ==" -ForegroundColor Cyan
$op = Login "op01"
$d = Invoke-RestMethod "$Base/api/ops/order-detail?order_no=$no" -Headers $op
if (-not $d.data.payment) { throw "穿透缺支付记录" }
if (-not $d.data.shipment) { throw "穿透缺发货记录" }
Write-Host "  订单/支付(trade=$($d.data.payment.trade_no))/发货/库存流水($($d.data.stock_flows.Count)条)/通知日志($($d.data.notify_logs.Count)条) 齐全"

$sum = Invoke-RestMethod "$Base/api/ops/summary" -Headers $op
Write-Host "  运营汇总: 实收 $($sum.data.paid_amount) 实退 $($sum.data.refunded_amount)"

Write-Host "`n冒烟通过：S1 正常成交链路完整 ✅" -ForegroundColor Green
