-- ============================================================
-- Seckill-Mall 数据库初始化脚本
-- 数据库名称: seckill_mall
-- 字符集: utf8mb4
-- 排序规则: utf8mb4_unicode_ci
-- ============================================================

-- 创建数据库
CREATE DATABASE IF NOT EXISTS seckill_mall
    DEFAULT CHARACTER SET utf8mb4
    DEFAULT COLLATE utf8mb4_unicode_ci;

USE seckill_mall;

CREATE TABLE IF NOT EXISTS `users` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '用户ID',
    `username` VARCHAR(50) NOT NULL COMMENT '用户名',
    `password` VARCHAR(255) NOT NULL COMMENT '密码（bcrypt加密）',
    `email` VARCHAR(100) NOT NULL COMMENT '邮箱',
    `phone` VARCHAR(20) DEFAULT NULL COMMENT '手机号',
    `status` TINYINT NOT NULL DEFAULT 1 COMMENT '状态: 1=正常, 0=禁用',
    `created_at` BIGINT NOT NULL COMMENT '创建时间戳',
    `updated_at` BIGINT NOT NULL COMMENT '更新时间戳',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_username` (`username`),
    UNIQUE KEY `uk_email` (`email`),
    KEY `idx_status` (`status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='用户表';

CREATE TABLE IF NOT EXISTS `products` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '商品ID',
    `name` VARCHAR(200) NOT NULL COMMENT '商品名称',
    `description` TEXT DEFAULT NULL COMMENT '商品描述',
    `price` BIGINT NOT NULL COMMENT '价格（分）',
    `stock` INT NOT NULL DEFAULT 0 COMMENT '库存数量',
    `sold_count` INT NOT NULL DEFAULT 0 COMMENT '已售数量',
    `cover_image` VARCHAR(500) DEFAULT NULL COMMENT '封面图URL',
    `status` TINYINT NOT NULL DEFAULT 1 COMMENT '状态: 1=上架, 0=下架',
    `created_at` BIGINT NOT NULL COMMENT '创建时间戳',
    `updated_at` BIGINT NOT NULL COMMENT '更新时间戳',
    PRIMARY KEY (`id`),
    KEY `idx_status` (`status`),
    KEY `idx_name` (`name`),
    FULLTEXT KEY `ft_name_desc` (`name`, `description`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='商品表';

CREATE TABLE IF NOT EXISTS `seckill_products` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '秒杀商品ID',
    `product_id` BIGINT UNSIGNED NOT NULL COMMENT '关联商品ID',
    `seckill_price` BIGINT NOT NULL COMMENT '秒杀价格（分）',
    `seckill_stock` INT NOT NULL COMMENT '秒杀库存',
    `sold_count` INT NOT NULL DEFAULT 0 COMMENT '已售数量',
    `start_time` BIGINT NOT NULL COMMENT '开始时间戳',
    `end_time` BIGINT NOT NULL COMMENT '结束时间戳',
    `per_limit` INT NOT NULL DEFAULT 1 COMMENT '每人限购数量',
    `status` TINYINT NOT NULL DEFAULT 0 COMMENT '状态: 0=未开始, 1=进行中, 2=已结束',
    `created_at` BIGINT NOT NULL COMMENT '创建时间戳',
    `updated_at` BIGINT NOT NULL COMMENT '更新时间戳',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_product_id` (`product_id`),
    KEY `idx_status` (`status`),
    KEY `idx_time` (`start_time`, `end_time`),
    CONSTRAINT `fk_seckill_product` FOREIGN KEY (`product_id`) REFERENCES `products` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='秒杀商品表';


CREATE TABLE IF NOT EXISTS `orders` (
    `order_id` VARCHAR(32) NOT NULL COMMENT '订单号',
    `user_id` BIGINT UNSIGNED NOT NULL COMMENT '用户ID',
    `product_id` BIGINT UNSIGNED NOT NULL COMMENT '商品ID',
    `product_name` VARCHAR(200) NOT NULL COMMENT '商品名称（冗余）',
    `quantity` INT NOT NULL DEFAULT 1 COMMENT '购买数量',
    `amount` BIGINT NOT NULL COMMENT '实付金额（分）',
    `seckill_price` BIGINT DEFAULT 0 COMMENT '秒杀价格（分，普通订单为0）',
    `order_type` TINYINT NOT NULL DEFAULT 0 COMMENT '订单类型: 0=普通订单, 1=秒杀订单',
    `status` TINYINT NOT NULL DEFAULT 0 COMMENT '订单状态: 0=待支付, 1=已支付, 2=已取消, 3=已退款, 4=已完成',
    `payment_id` VARCHAR(64) DEFAULT NULL COMMENT '支付流水号',
    `paid_at` BIGINT DEFAULT NULL COMMENT '支付时间戳',
    `created_at` BIGINT NOT NULL COMMENT '创建时间戳',
    `updated_at` BIGINT NOT NULL COMMENT '更新时间戳',
    PRIMARY KEY (`order_id`),
    KEY `idx_user_id` (`user_id`),
    KEY `idx_product_id` (`product_id`),
    KEY `idx_status` (`status`),
    KEY `idx_created_at` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='订单表';

CREATE TABLE IF NOT EXISTS `stock_logs` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '日志ID',
    `product_id` BIGINT UNSIGNED NOT NULL COMMENT '商品ID',
    `order_id` VARCHAR(32) NOT NULL COMMENT '订单号',
    `change_type` TINYINT NOT NULL COMMENT '变更类型: 1=扣减, 2=回滚',
    `quantity` INT NOT NULL COMMENT '变更数量',
    `before_stock` INT NOT NULL COMMENT '变更前库存',
    `after_stock` INT NOT NULL COMMENT '变更后库存',
    `created_at` BIGINT NOT NULL COMMENT '创建时间戳',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_order_id` (`order_id`),
    KEY `idx_product_id` (`product_id`),
    KEY `idx_created_at` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='库存流水表';

CREATE TABLE IF NOT EXISTS `seckill_orders` (
    `id` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '记录ID',
    `user_id` BIGINT UNSIGNED NOT NULL COMMENT '用户ID',
    `seckill_product_id` BIGINT UNSIGNED NOT NULL COMMENT '秒杀商品ID',
    `order_id` VARCHAR(32) NOT NULL COMMENT '订单号',
    `quantity` INT NOT NULL DEFAULT 1 COMMENT '购买数量',
    `created_at` BIGINT NOT NULL COMMENT '创建时间戳',
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_user_seckill` (`user_id`, `seckill_product_id`),
    KEY `idx_seckill_product_id` (`seckill_product_id`),
    KEY `idx_order_id` (`order_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='用户秒杀购买记录表';


-- 插入测试用户（密码都是 123456，使用 bcrypt 加密）
INSERT INTO `users` (`username`, `password`, `email`, `phone`, `status`, `created_at`, `updated_at`) VALUES
('admin', '$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZRGdjGj/n3.cP9J6X.qrQ3S3r4m7K', 'admin@example.com', '13800138000', 1, UNIX_TIMESTAMP(), UNIX_TIMESTAMP()),
('testuser', '$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZRGdjGj/n3.cP9J6X.qrQ3S3r4m7K', 'test@example.com', '13900139000', 1, UNIX_TIMESTAMP(), UNIX_TIMESTAMP());

-- 插入测试商品
INSERT INTO `products` (`name`, `description`, `price`, `stock`, `sold_count`, `cover_image`, `status`, `created_at`, `updated_at`) VALUES
('iPhone 15 Pro', '苹果 iPhone 15 Pro 256GB 钛金色', 799900, 100, 0, 'https://example.com/iphone15.jpg', 1, UNIX_TIMESTAMP(), UNIX_TIMESTAMP()),
('小米14 Ultra', '小米 14 Ultra 16GB+512GB 徕卡影像旗舰', 599900, 200, 0, 'https://example.com/xiaomi14.jpg', 1, UNIX_TIMESTAMP(), UNIX_TIMESTAMP()),
('MacBook Pro 14', 'Apple MacBook Pro 14英寸 M3 Pro芯片', 1699900, 50, 0, 'https://example.com/macbook.jpg', 1, UNIX_TIMESTAMP(), UNIX_TIMESTAMP());

-- 插入秒杀商品（关联上面插入的商品，设置一个较短的秒杀时间窗口）
INSERT INTO `seckill_products` (`product_id`, `seckill_price`, `seckill_stock`, `sold_count`, `start_time`, `end_time`, `per_limit`, `status`, `created_at`, `updated_at`) VALUES
(1, 699900, 50, 0, UNIX_TIMESTAMP(), UNIX_TIMESTAMP() + 86400, 1, 1, UNIX_TIMESTAMP(), UNIX_TIMESTAMP()),
(2, 499900, 100, 0, UNIX_TIMESTAMP(), UNIX_TIMESTAMP() + 86400, 1, 1, UNIX_TIMESTAMP(), UNIX_TIMESTAMP());
