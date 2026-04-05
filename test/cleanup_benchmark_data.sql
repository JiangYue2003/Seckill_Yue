-- ============================================================
-- 压测数据全量清理脚本
-- 清空所有订单、库存流水、秒杀记录，并重置商品库存
--
-- 使用方式：
--   mysql -u root -pZz123456 seckill_mall < cleanup_benchmark_data.sql
--   或在 MySQL 客户端执行：source cleanup_benchmark_data.sql
-- ============================================================

USE seckill_mall;

SET FOREIGN_KEY_CHECKS = 0;

-- ① 清空订单表
TRUNCATE TABLE orders;
SELECT '清空 orders 完成' AS '状态';

-- ② 清空库存流水表
TRUNCATE TABLE stock_logs;
SELECT '清空 stock_logs 完成' AS '状态';

-- ③ 清空秒杀购买记录表
TRUNCATE TABLE seckill_orders;
SELECT '清空 seckill_orders 完成' AS '状态';

-- ④ 重置秒杀商品已售数量
UPDATE seckill_products SET sold_count = 0, updated_at = UNIX_TIMESTAMP();
SELECT CONCAT('重置 seckill_products sold_count，影响 ', ROW_COUNT(), ' 行') AS '状态';

-- ⑤ 重置普通商品已售数量
UPDATE products SET sold_count = 0, updated_at = UNIX_TIMESTAMP();
SELECT CONCAT('重置 products sold_count，影响 ', ROW_COUNT(), ' 行') AS '状态';

SET FOREIGN_KEY_CHECKS = 1;

-- 最终确认
SELECT
    (SELECT COUNT(*) FROM orders)          AS 'orders 剩余行数',
    (SELECT COUNT(*) FROM stock_logs)      AS 'stock_logs 剩余行数',
    (SELECT COUNT(*) FROM seckill_orders)  AS 'seckill_orders 剩余行数';
