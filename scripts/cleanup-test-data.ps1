# cleanup-test-data.ps1
# 清理秒杀测试数据
# 使用方法: .\cleanup-test-data.ps1

param(
    [string]$RedisHost = "localhost:6379"
)

$ErrorActionPreference = "Stop"

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "   清理秒杀测试数据" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "Redis Host: $RedisHost" -ForegroundColor Yellow
Write-Host ""

# 分割 Redis 地址
$redisParts = $RedisHost -split ':'
$redisAddr = $redisParts[0]
$redisPort = if ($redisParts.Length -gt 1) { $redisParts[1] } else { 6379 }

# 清理秒杀商品数据函数
function Cleanup-SeckillProduct {
    param([int64]$SeckillProductId)

    Write-Host "[清理] 删除秒杀商品 ID=$SeckillProductId" -ForegroundColor Yellow

    # 删除商品信息和库存
    $infoKey = "seckill:info:$SeckillProductId"
    $nameKey = "seckill:product_name:$SeckillProductId"
    $stockKey = "seckill:stock:$SeckillProductId"

    redis-cli -h $redisAddr -p $redisPort DEL $infoKey 2>$null | Out-Null
    redis-cli -h $redisAddr -p $redisPort DEL $nameKey 2>$null | Out-Null
    redis-cli -h $redisAddr -p $redisPort DEL $stockKey 2>$null | Out-Null

    Write-Host "  [OK] 删除完成" -ForegroundColor Green
}

# 清理测试商品
$productIds = @(1, 2, 3, 4, 5, 1001, 1002, 1003, 1004, 1005, 2001)
foreach ($pid in $productIds) {
    Cleanup-SeckillProduct -SeckillProductId $pid
}

# 清理测试用户记录
Write-Host "[清理] 删除用户购买记录..." -ForegroundColor Yellow
$testUserIds = @(10001, 10002, 10021, 10022, 10031, 10041, 100001, 100002)
foreach ($uid in $testUserIds) {
    foreach ($pid in $productIds) {
        $userKey = "seckill:user:$pid`:$uid"
        redis-cli -h $redisAddr -p $redisPort DEL $userKey 2>$null | Out-Null
    }
}

# 清理测试订单
Write-Host "[清理] 删除测试订单..." -ForegroundColor Yellow
$keys = redis-cli -h $redisAddr -p $redisPort KEYS "seckill:order:S*" 2>$null
if ($keys) {
    $orderKeys = $keys -split "`n" | Where-Object { $_ -ne "" }
    foreach ($key in $orderKeys) {
        redis-cli -h $redisAddr -p $redisPort DEL $key 2>$null | Out-Null
    }
}

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "   测试数据清理完成" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
