#!/bin/bash
# init-test-data.sh
# 初始化秒杀测试数据到 Redis
# 使用方法: bash scripts/init-test-data.sh

set -e

REDIS_HOST="${REDIS_HOST:-localhost:6379}"

echo "========================================"
echo "   初始化秒杀测试数据"
echo "========================================"
echo ""
echo "Redis Host: $REDIS_HOST"
echo ""

# 初始化秒杀商品函数
init_seckill_product() {
    local seckill_product_id=$1
    local product_id=$2
    local seckill_price=$3
    local product_name=$4
    local stock=$5
    local start_time=$6
    local end_time=$7

    echo "[初始化] 秒杀商品 ID=$seckill_product_id ($product_name)"
    echo "  - 秒杀价格: $seckill_price 分"
    echo "  - 库存: $stock"
    echo "  - 活动时间: $(date -d @$start_time 2>/dev/null || date -r $start_time 2>/dev/null || echo "$start_time") ~ $(date -d @$end_time 2>/dev/null || date -r $end_time 2>/dev/null || echo "$end_time")"

    # 设置秒杀商品信息
    redis-cli -h ${REDIS_HOST%%:*} -p ${REDIS_HOST##*:} SET "seckill:info:$seckill_product_id" "$product_id:$seckill_price:$start_time:$end_time" EX 86400
    redis-cli -h ${REDIS_HOST%%:*} -p ${REDIS_HOST##*:} SET "seckill:product_name:$seckill_product_id" "$product_name" EX 86400

    # 设置库存
    redis-cli -h ${REDIS_HOST%%:*} -p ${REDIS_HOST##*:} SET "seckill:stock:$seckill_product_id" $stock

    echo "  [OK] 初始化完成"
    echo ""
}

# 获取当前时间戳
NOW=$(date +%s)
START_TIME=$((NOW - 3600))  # 1小时前开始
END_TIME=$((NOW + 86400))   # 24小时后结束

echo "初始化测试商品..."
echo ""

# 商品1: iPhone 15 Pro 秒杀
init_seckill_product 1 1001 599900 "iPhone 15 Pro 256GB" 100 $START_TIME $END_TIME

# 商品2: MacBook Air M3 秒杀
init_seckill_product 2 1002 899900 "MacBook Air M3 8GB" 50 $START_TIME $END_TIME

# 商品3: AirPods Pro 秒杀
init_seckill_product 3 1003 149900 "AirPods Pro 2代" 200 $START_TIME $END_TIME

# 商品4: iPad Pro 秒杀
init_seckill_product 4 1004 599900 "iPad Pro 11寸 256GB" 80 $START_TIME $END_TIME

# 商品5: Apple Watch 秒杀
init_seckill_product 5 1005 299900 "Apple Watch Series 9" 150 $START_TIME $END_TIME

echo "========================================"
echo "   测试数据初始化完成"
echo "========================================"
echo ""
echo "秒杀商品列表:"
echo "  ID | 商品名称                      | 秒杀价格    | 库存"
echo "  ---|-------------------------------|------------|------"
echo "   1 | iPhone 15 Pro 256GB           | ¥5999.00   | 100"
echo "   2 | MacBook Air M3 8GB             | ¥8999.00   |  50"
echo "   3 | AirPods Pro 2代                | ¥1499.00   | 200"
echo "   4 | iPad Pro 11寸 256GB            | ¥5999.00   |  80"
echo "   5 | Apple Watch Series 9           | ¥2999.00   | 150"
echo ""
echo "运行功能测试: cd test/seckill-functional-test && go run ."
echo "运行性能测试: cd test/seckill-benchmark-test && go run ."
echo ""
