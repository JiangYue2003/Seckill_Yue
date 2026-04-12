-- 将 stock_logs 的唯一约束从 order_id 升级为 (order_id, change_type)
-- 目的：允许同一订单同时存在“扣减”和“回滚”两条流水，避免回滚日志唯一冲突

ALTER TABLE `stock_logs`
DROP INDEX `uk_order_id`;

ALTER TABLE `stock_logs`
ADD UNIQUE KEY `uk_order_change_type` (`order_id`, `change_type`);

