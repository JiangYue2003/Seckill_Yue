#!/bin/bash
# cleanup-test-data.sh
# 清理秒杀测试数据
# 使用方法: bash scripts/cleanup-test-data.sh

set -e

REDIS_HOST="${REDIS_HOST:-localhost:6379}"

echo "========================================"
echo "   清理秒杀测试数据"
echo "========================================"
echo ""
echo "Redis Host: $REDIS_HOST"
echo ""

# 清理秒杀商品数据函数
cleanup_seckill_product() {
    local seckill_product_id=$1
    echo "[清理] 删除秒杀商品 ID=$seckill_product_id"

    # 删除商品信息和库存
    redis-cli -h ${REDIS_HOST%%:*} -p ${REDIS_HOST##*:} DEL "seckill:info:$seckill_product_id" 2>/dev/null || true
    redis-cli -h ${REDIS_HOST%%:*} -p ${REDIS_HOST##*:} DEL "seckill:product_name:$seckill_product_id" 2>/dev/null || true
    redis-cli -h ${REDIS_HOST%%:*} -p ${REDIS_HOST##*:} DEL "seckill:stock:$seckill_product_id" 2>/dev/null || true

    echo "  [OK] 删除完成"
}

# 清理测试商品
for pid in 1 2 3 4 5; do
    cleanup_seckill_product $pid
done

# 清理功能测试使用的商品
for pid in 1001 1002 1003 1004 1005; do
    cleanup_seckill_product $pid
done

# 清理性能测试使用的商品
cleanup_seckill_product 2001

# 清理测试用户记录（功能测试）
test_user_ids=(10001 10002 10021 10022 10031 10041 100001 100002)
for uid in "${test_user_ids[@]}"; do
    for pid in 1 2 3 4 5 1001 1002 1003 1004 1005 2001; do
        redis-cli -h ${REDIS_HOST%%:*} -p ${REDIS_HOST##*:} DEL "seckill:user:$pid:$uid" 2>/dev/null || true
    done
done

# 清理测试订单
redis-cli -h ${REDIS_HOST%%:*} -p ${REDIS_HOST##*:} KEYS "seckill:order:S*" | while read key; do
    redis-cli -h ${REDIS_HOST%%:*} -p ${REDIS_HOST##*:} DEL "$key" 2>/dev/null || true
done

echo ""
echo "========================================"
echo "   测试数据清理完成"
echo "========================================"
