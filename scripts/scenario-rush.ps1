# 场景联调：S2 缺货与弃单（20 并发抢购、防超卖、超时释放后他人可买）
# 把 P1 库存重置为 5，20 并发抢，验证成交+占用 ≤5；未付款超时释放后库存回满
param(
  [string]$Base = "http://127.0.0.1:8888",
  [int]$Parallel = 20,
  [int]$WaitCloseSec = 33
)
$ErrorActionPreference = "Stop"

function Login($user) {
  $json = @{ username = $user; password = "123456" } | ConvertTo-Json -Compress
  $r = Invoke-RestMethod -Method Post "$Base/api/login" -ContentType 'application/json; charset=utf-8' `
    -Body ([Text.Encoding]::UTF8.GetBytes($json))
  return @{ Authorization = "Bearer $($r.data.token)" }
}

$op = Login "op01"

Write-Host "== 重置 P1 库存为 5（清空其历史订单的占用干扰） ==" -ForegroundColor Cyan
# 通过运营接口的旁路无法改库存，直接以数据库重置（联调准备步骤，非业务路径）
# 中文库名走 source 文件，避免命令行编码问题
[System.IO.File]::WriteAllText("C:\Temp\rush_reset.sql", "USE ``电商订单表1``; UPDATE product SET stock=5 WHERE id='P1';")
# 原生命令的 stderr 密码警告不应中断脚本，临时降级错误处理
$ErrorActionPreference = "Continue"
& "C:\Program Files\MySQL\MySQL Server 8.0\bin\mysql.exe" -h 127.0.0.1 -u root -p123456 --default-character-set=utf8mb4 `
  -e "source C:/Temp/rush_reset.sql" 2>$null | Out-Null
$ErrorActionPreference = "Stop"
$p = (Invoke-RestMethod "$Base/api/ops/summary" -Headers $op).data.products | Where-Object { $_.id -eq "P1" }
Write-Host "  P1 可售 = $($p.stock)（应为 5）"
if ($p.stock -ne 5) { throw "库存重置失败" }

Write-Host "== 20 并发抢购库存 5 ==" -ForegroundColor Cyan
$jobs = 1..$Parallel | ForEach-Object {
  $i = $_
  Start-Job -ScriptBlock {
    param($base, $buyer)
    $json = @{ username = $buyer; password = "123456" } | ConvertTo-Json -Compress
    $r = Invoke-RestMethod -Method Post "$base/api/login" -ContentType 'application/json; charset=utf-8' `
      -Body ([Text.Encoding]::UTF8.GetBytes($json))
    $h = @{ Authorization = "Bearer $($r.data.token)" }
    $body = @{ product_id = "P1"; qty = 1 } | ConvertTo-Json -Compress
    try {
      $o = Invoke-RestMethod -Method Post "$base/api/orders/create" -Headers $h -ContentType 'application/json; charset=utf-8' `
        -Body ([Text.Encoding]::UTF8.GetBytes($body))
      return "OK:$($o.data.order_no)"
    } catch {
      return "FAIL:$($_.ErrorDetails.Message)"
    }
  } -ArgumentList $Base, ("buyer0" + (($i % 3) + 1))
}
$results = $jobs | Wait-Job | Receive-Job
$jobs | Remove-Job
$ok = @($results | Where-Object { $_ -like "OK:*" })
$fail = @($results | Where-Object { $_ -like "FAIL:*" })
Write-Host "  成功下单 $($ok.Count)，被拒 $($fail.Count)"

$after = (Invoke-RestMethod "$Base/api/ops/summary" -Headers $op).data.products | Where-Object { $_.id -eq "P1" }
Write-Host "  抢购后 P1 可售 = $($after.stock)"
# 核心断言：成交数 = 减少的库存，且不超卖
$sold = 5 - $after.stock
if ($ok.Count -ne $sold) { throw "超卖检测：成功 $($ok.Count) 单，但库存只减少 $sold（不一致）" }
if ($after.stock -lt 0) { throw "库存为负，超卖！" }
Write-Host "  ✅ 无超卖：$($ok.Count) 单全部有库存支撑，剩余 $($after.stock)"

Write-Host "== 未付款订单等待超时释放 ==" -ForegroundColor Cyan
Write-Host "  等待 ${WaitCloseSec}s..."
Start-Sleep -Seconds $WaitCloseSec
# 触发一次惰性关单（查询商品列表即触发）
Invoke-RestMethod "$Base/api/products" -Headers (Login "buyer01") | Out-Null
Start-Sleep -Milliseconds 1500
$final = (Invoke-RestMethod "$Base/api/ops/summary" -Headers $op).data.products | Where-Object { $_.id -eq "P1" }
Write-Host "  超时释放后 P1 可售 = $($final.stock)（应回满 5）"
if ($final.stock -ne 5) { throw "超时未正确释放，应为 5，实际 $($final.stock)" }

# 释放后别人能买到
$newBody = @{ product_id = "P1"; qty = 1 } | ConvertTo-Json -Compress
$o = Invoke-RestMethod -Method Post "$Base/api/orders/create" -Headers (Login "buyer01") -ContentType 'application/json; charset=utf-8' `
  -Body ([Text.Encoding]::UTF8.GetBytes($newBody))
Write-Host "  释放后新买家下单成功：$($o.data.order_no)"

# 清理：立即取消该验证单，避免遗留待支付订单在后续场景的等待窗口里超时释放（脚本间状态隔离）
$cancelBody = @{ order_no = $o.data.order_no } | ConvertTo-Json -Compress
Invoke-RestMethod -Method Post "$Base/api/orders/cancel" -Headers (Login "buyer01") -ContentType 'application/json; charset=utf-8' `
  -Body ([Text.Encoding]::UTF8.GetBytes($cancelBody)) | Out-Null
Write-Host "  已取消验证单 $($o.data.order_no)（清理现场，不遗留待支付订单）"

Write-Host "`n冒烟通过：S2 缺货与弃单（20 并发不超卖、超时释放回满、他人可购）✅" -ForegroundColor Green
