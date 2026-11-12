# 场景联调：S3 售前退款（申请→审核→渠道首次失败→客服重试成功→库存回补）
# 依赖：trade-api(8888) 与 pay-channel(8902) 已启动；渠道默认"首次退款失败、重试成功"
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
function PostJson($url, $headers, $obj) {
  $json = $obj | ConvertTo-Json -Compress
  return Invoke-RestMethod -Method Post $url -Headers $headers -ContentType 'application/json; charset=utf-8' `
    -Body ([Text.Encoding]::UTF8.GetBytes($json))
}

$bh = Login "buyer02"
$ch = Login "cs01"
$op = Login "op01"

Write-Host "== 准备：下单并支付（P2 陪衬商品，不影响活动库存） ==" -ForegroundColor Cyan
$o = PostJson "$Base/api/orders/create" $bh @{ product_id = "P2"; qty = 2 }
$no = $o.data.order_no
$before = (Invoke-RestMethod "$Base/api/ops/summary" -Headers $op).data.products | Where-Object { $_.id -eq "P2" }
Write-Host "  下单 $no（2 件）后 P2 可售 $($before.stock)"
Invoke-RestMethod -Method Post "$Channel/channel/pay" -ContentType 'application/json' `
  -Body (@{ order_no = $no; amount = $o.data.amount; scenario = "success" } | ConvertTo-Json -Compress) | Out-Null
Start-Sleep -Milliseconds 1200
$o1 = Invoke-RestMethod "$Base/api/orders/get?order_no=$no" -Headers $bh
if ($o1.data.status -ne "to_ship") { throw "支付失败，状态 $($o1.data.status)" }
Write-Host "  已支付，状态 to_ship"

Write-Host "== [1] 买家申请售前退款 ==" -ForegroundColor Cyan
$rf = PostJson "$Base/api/orders/refund/apply" $bh @{ order_no = $no; reason = "买多了，不想要了" }
$rfNo = $rf.data.refund_no
Write-Host "  退款单 $rfNo 状态 $($rf.data.status)"
$o2 = Invoke-RestMethod "$Base/api/orders/get?order_no=$no" -Headers $bh
if ($o2.data.status -ne "refund_review") { throw "订单应转退款审核中，实际 $($o2.data.status)" }

Write-Host "== [2] 仓库此时发货应被拒绝（R5 停发） ==" -ForegroundColor Cyan
$wh = Login "wh01"
try {
  PostJson "$Base/api/wh/ship" $wh @{ order_no = $no; carrier = "中通快递"; tracking_no = "ZT000" }
  throw "发货未被拦截——严重缺陷"
} catch {
  if ($_.ErrorDetails.Message -match "状态不允许") {
    Write-Host "  发货被正确拦截：$($_.ErrorDetails.Message)"
  } else { throw }
}

Write-Host "== [3] 客服审核同意（渠道第 1 次：默认通知失败） ==" -ForegroundColor Cyan
PostJson "$Base/api/cs/refunds/review" $ch @{ refund_no = $rfNo; approve = $true; note = "同意退款" } | Out-Null
Start-Sleep -Milliseconds 1200
$rfList = (Invoke-RestMethod "$Base/api/cs/refunds?status=failed_pending" -Headers $ch).data
$mine = $rfList | Where-Object { $_.refund_no -eq $rfNo }
if (-not $mine) { throw "退款应进入失败待处理，实际不在列表" }
Write-Host "  首次渠道退款失败，状态 failed_pending（待客服重试）"

Write-Host "== [4] 客服重试（渠道第 2 次：默认通知成功） ==" -ForegroundColor Cyan
PostJson "$Base/api/cs/refunds/retry" $ch @{ refund_no = $rfNo } | Out-Null
Start-Sleep -Milliseconds 1200
$rfList2 = (Invoke-RestMethod "$Base/api/cs/refunds?status=success" -Headers $ch).data
$mine2 = $rfList2 | Where-Object { $_.refund_no -eq $rfNo }
if (-not $mine2) { throw "重试后应退款成功" }
Write-Host "  重试成功，退款完成（attempt=$($mine2.attempt)）"

Write-Host "== [5] 核验：订单已退款 + 库存回补一次 ==" -ForegroundColor Cyan
$o3 = Invoke-RestMethod "$Base/api/orders/get?order_no=$no" -Headers $bh
if ($o3.data.status -ne "refunded") { throw "订单应为 refunded，实际 $($o3.data.status)" }
$after = (Invoke-RestMethod "$Base/api/ops/summary" -Headers $op).data.products | Where-Object { $_.id -eq "P2" }
if ($after.stock -ne ($before.stock + 2)) { throw "库存回补错误：下单后 $($before.stock)，退款后应为 $($before.stock + 2)，实际 $($after.stock)" }
$flows = @((Invoke-RestMethod "$Base/api/ops/stock-flows?product_id=P2" -Headers $op).data | Where-Object { $_.order_no -eq $no })
$restore = @($flows | Where-Object { $_.change_type -eq "restore" }).Count
$occupy = @($flows | Where-Object { $_.change_type -eq "occupy" }).Count
if ($restore -ne 1 -or $occupy -ne 1) { throw "流水应占用/回补各恰好 1 次，实际 occupy=$occupy restore=$restore" }
Write-Host "  订单 refunded，库存 $($before.stock) → $($after.stock)（回补恰好 1 次）"

$sum = Invoke-RestMethod "$Base/api/ops/summary" -Headers $op
Write-Host "  运营汇总: 实收 $($sum.data.paid_amount) 实退 $($sum.data.refunded_amount)"
Write-Host "`n冒烟通过：S3 售前退款（首次失败重试成功 + 停发拦截 + 库存回补一次）✅" -ForegroundColor Green
