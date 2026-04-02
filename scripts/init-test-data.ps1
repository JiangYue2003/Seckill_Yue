# init-test-data.ps1
# 初始化秒杀测试数据到 Redis
# 使用方法: .\init-test-data.ps1

param(
    [string]$RedisHost = "localhost:6379"
)

$ErrorActionPreference = "Stop"

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "   初始化秒杀测试数据" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "Redis Host: $RedisHost" -ForegroundColor Yellow
Write-Host ""

# 分割 Redis 地址
$redisParts = $RedisHost -split ':'
$redisAddr = $redisParts[0]
$redisPort = if ($redisParts.Length -gt 1) { $redisParts[1] } else { 6379 }

# 定义秒杀商品数据
$seckillProducts = @(
    @{Id=1; ProductId=1001; Price=599900; Name="iPhone 15 Pro 256GB"; Stock=100},
    @{Id=2; ProductId=1002; Price=899900; Name="MacBook Air M3 8GB"; Stock=50},
    @{Id=3; ProductId=1003; Price=149900; Name="AirPods Pro 2代"; Stock=200},
    @{Id=4; ProductId=1004; Price=599900; Name="iPad Pro 11寸 256GB"; Stock=80},
    @{Id=5; ProductId=1005; Price=299900; Name="Apple Watch Series 9"; Stock=150}
)

# 获取当前时间戳
$now = [int64](Get-Date -UFormat "%s")
$startTime = $now - 3600   # 1小时前开始
$endTime = $now + 86400    # 24小时后结束

# 初始化秒杀商品函数
function Init-SeckillProduct {
    param(
        [int64]$SeckillProductId,
        [int64]$ProductId,
        [int64]$SeckillPrice,
        [string]$ProductName,
        [int64]$Stock,
        [int64]$StartTime,
        [int64]$EndTime
    )

    Write-Host "[初始化] 秒杀商品 ID=$SeckillProductId ($ProductName)" -ForegroundColor Green
    Write-Host "  - 秒杀价格: $SeckillPrice 分"
    Write-Host "  - 库存: $Stock"

    # 设置秒杀商品信息
    $infoKey = "seckill:info:$SeckillProductId"
    $infoValue = "$ProductId`:$SeckillPrice`:$StartTime`:$EndTime"
    redis-cli -h $redisAddr -p $redisPort SET $infoKey $infoValue EX 86400 | Out-Null

    $nameKey = "seckill:product_name:$SeckillProductId"
    redis-cli -h $redisAddr -p $redisPort SET $nameKey $ProductName EX 86400 | Out-Null

    # 设置库存
    $stockKey = "seckill:stock:$SeckillProductId"
    redis-cli -h $redisAddr -p $redisPort SET $stockKey $Stock | Out-Null

    Write-Host "  [OK] 初始化完成" -ForegroundColor Green
    Write-Host ""
}

Write-Host "初始化测试商品..." -ForegroundColor Yellow
Write-Host ""

foreach ($product in $seckillProducts) {
    Init-SeckillProduct `
        -SeckillProductId $product.Id `
        -ProductId $product.ProductId `
        -SeckillPrice $product.Price `
        -ProductName $product.Name `
        -Stock $product.Stock `
        -StartTime $startTime `
        -EndTime $endTime
}

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "   测试数据初始化完成" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "秒杀商品列表:" -ForegroundColor Yellow
Write-Host "  ID | 商品名称                      | 秒杀价格    | 库存"
Write-Host "  ---|-------------------------------|------------|------"
Write-Host "   1 | iPhone 15 Pro 256GB           | Y5999.00   | 100"
Write-Host "   2 | MacBook Air M3 8GB            | Y8999.00   |  50"
Write-Host "   3 | AirPods Pro 2代               | Y1499.00   | 200"
Write-Host "   4 | iPad Pro 11寸 256GB           | Y5999.00   |  80"
Write-Host "   5 | Apple Watch Series 9          | Y2999.00   | 150"
Write-Host ""
Write-Host "运行功能测试: cd test/seckill-functional-test; go run ." -ForegroundColor Green
Write-Host "运行性能测试: cd test/seckill-benchmark-test; go run ." -ForegroundColor Green
Write-Host ""
