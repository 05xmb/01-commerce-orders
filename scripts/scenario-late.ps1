# 场景联调：S4 迟到支付（超时关单→成功通知晚到→异常台账→客服退款→不回补库存）
# 教学时钟演示配置 PayTimeoutSeconds=30；渠道 late 场景延迟 40s 送达
param(
  [string]$Base = "http://127.0.0.1:8888",
  [string]$Channel = "http://127.0.0.1:8902",
  [int]$WaitCloseSec = 33,
  [int]$WaitLateSec = 10
)
$ErrorActionPreference = "Stop"

function Login($user) {
  $json = @{ username = $user; password = "123456" } | ConvertTo-Json -Compress
  $r = Invoke-RestMethod -Method Post "$Base/api/login" -ContentType 'application/json; charset=utf-8' `
    -Body ([Text.Encoding]::UTF8.GetBytes($json))
  return @{ Authorization = "Bearer $($r.data.token)" }
}
function PostJson($url, $headers, $obj) {
  $json = $obj | ConvertTo-Json -Compress
  return Invoke-RestMethod -Method Post $url -Headers $headers -ContentType 'application/json; charset=utf-8' `
    -Body ([Text.Encoding]::UTF8.GetBytes($json))
}

$bh = Login "buyer03"
$ch = Login "cs01"
$op = Login "op01"

Write-Host "== [1] 下单后放弃支付，等超时关单 ==" -ForegroundColor Cyan
$before = (Invoke-RestMethod "$Base/api/ops/summary" -Headers $op).data.products | Where-Object { $_.id -eq "P1" }
$o = PostJson "$Base/api/orders/create" $bh @{ product_id = "P1"; qty = 1 }
$no = $o.data.order_no
$afterOrder = (Invoke-RestMethod "$Base/api/ops/summary" -Headers $op).data.products | Where-Object { $_.id -eq "P1" }
Write-Host "  下单 $no，P1 可售 $($before.stock) → $($afterOrder.stock)（占用 1）"
Write-Host "  等待 ${WaitCloseSec}s 让订单超时关闭..."
Start-Sleep -Seconds $WaitCloseSec
$o1 = Invoke-RestMethod "$Base/api/orders/get?order_no=$no" -Headers $bh
if ($o1.data.status -ne "closed") { throw "应已超时关闭，实际 $($o1.data.status)" }
$afterClose = (Invoke-RestMethod "$Base/api/ops/summary" -Headers $op).data.products | Where-Object { $_.id -eq "P1" }
if ($afterClose.stock -ne $before.stock) { throw "关单后库存应恢复为 $($before.stock)，实际 $($afterClose.stock)" }
Write-Host "  订单已关闭，库存恢复为 $($afterClose.stock)（释放恰好 1 次）"

Write-Host "== [2] 渠道迟到通知（订单关闭后成功支付晚到） ==" -ForegroundColor Cyan
Invoke-RestMethod -Method Post "$Channel/channel/pay" -ContentType 'application/json' `
  -Body (@{ order_no = $no; amount = $o.data.amount; scenario = "success"; delay_ms = 0 } | ConvertTo-Json -Compress) | Out-Null
Start-Sleep -Seconds 2
$o2 = Invoke-RestMethod "$Base/api/orders/get?order_no=$no" -Headers $bh
if ($o2.data.status -ne "closed") { throw "迟到支付不得复活订单，实际 $($o2.data.status)" }
$afterLate = (Invoke-RestMethod "$Base/api/ops/summary" -Headers $op).data.products | Where-Object { $_.id -eq "P1" }
if ($afterLate.stock -ne $before.stock) { throw "迟到支付不得重新占用库存，实际 $($afterLate.stock)" }
Write-Host "  订单仍为 closed，库存未变（不复活、不重占）"

Write-Host "== [3] 异常支付台账出现记录 ==" -ForegroundColor Cyan
$exs = (Invoke-RestMethod "$Base/api/cs/exceptions?status=pending" -Headers $ch).data
$ex = $exs | Where-Object { $_.order_no -eq $no }
if (-not $ex) { throw "异常台账应有该订单记录" }
Write-Host "  异常 #$($ex.id) trade=$($ex.trade_no) 金额 $($ex.amount) 待处理"

Write-Host "== [4] 客服核实并发起退款（type=late，首次失败重试成功） ==" -ForegroundColor Cyan
PostJson "$Base/api/cs/exceptions/refund" $ch @{ id = $ex.id } | Out-Null
Start-Sleep -Milliseconds 1200
# 首次失败 → 重试
$rfList = (Invoke-RestMethod "$Base/api/cs/refunds?status=failed_pending" -Headers $ch).data
$rf = $rfList | Where-Object { $_.order_no -eq $no }
if (-not $rf) { throw "迟到退款首次应失败待处理" }
Write-Host "  首次渠道退款失败（$($rf.refund_no)），客服重试..."
PostJson "$Base/api/cs/refunds/retry" $ch @{ refund_no = $rf.refund_no } | Out-Null
Start-Sleep -Milliseconds 1200
$rfOk = (Invoke-RestMethod "$Base/api/cs/refunds?status=success" -Headers $ch).data | Where-Object { $_.order_no -eq $no }
if (-not $rfOk) { throw "重试后应退款成功" }
Write-Host "  重试成功（attempt=$($rfOk.attempt)）"

Write-Host "== [5] 红线核验：退款不回补库存、异常台账核销、无重复流水 ==" -ForegroundColor Cyan
$final = (Invoke-RestMethod "$Base/api/ops/summary" -Headers $op).data.products | Where-Object { $_.id -eq "P1" }
if ($final.stock -ne $before.stock) { throw "迟到支付退款不得回补库存：应 $($before.stock)，实际 $($final.stock)" }
$exAfter = (Invoke-RestMethod "$Base/api/cs/exceptions" -Headers $ch).data | Where-Object { $_.order_no -eq $no }
if ($exAfter.status -ne "refunded") { throw "异常台账应核销为 refunded，实际 $($exAfter.status)" }
$flows = @((Invoke-RestMethod "$Base/api/ops/stock-flows?product_id=P1" -Headers $op).data | Where-Object { $_.order_no -eq $no })
$occupy = @($flows | Where-Object { $_.change_type -eq "occupy" }).Count
$release = @($flows | Where-Object { $_.change_type -eq "release" }).Count
$restore = @($flows | Where-Object { $_.change_type -eq "restore" }).Count
if ($occupy -ne 1 -or $release -ne 1 -or $restore -ne 0) {
  throw "流水应为 占用1/释放1/回补0，实际 $occupy/$release/$restore"
}
Write-Host "  库存保持 $($final.stock)；流水 占用$occupy/释放$release/回补$restore；异常台账已核销"
Write-Host "`n冒烟通过：S4 迟到支付（不复活、不重占、退款不回补、无重复流水）✅" -ForegroundColor Green
