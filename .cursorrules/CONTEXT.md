# Cursor Context: Seckill-Mall（当前实现基线）

> 本文件是当前项目上下文，不是最初建模阶段的历史蓝图。
> 如果这里和代码冲突，以代码为准，并应同步修正文档。

## 1. 环境约束

- 开发环境：Windows + PowerShell
- 所有命令默认按 PowerShell 语法书写
- 与 go-zero / proto 相关的生成操作，优先遵循本仓库既有方式，不要手写替代生成物

## 2. 当前技术基线

- 语言：Go
- 网关：Gin
- 服务治理：go-zero + gRPC + etcd
- 缓存与热点裁决：Redis
- 消息队列：RabbitMQ
- 数据库：MySQL + GORM
- 当前压测主路径：直连 `seckill-service` 的 gRPC benchmark

## 3. 当前架构要点

### 3.1 秒杀库存已经是物理分片模型

- MySQL 源头表：`seckill_stock_shards`
- 固定分片数：`16`
- Redis 运行时包含：
  - 总库存 key
  - 分片状态 key
  - 分片库存 key
  - 分片 reserve key
  - 用户占位 key
  - 订单热状态 key

### 3.2 Seckill-Service 不再是“只碰 Redis、不碰 MySQL”

当前 `seckill-service` 在 Redis 准入成功后，会同步持久化：

- `seckill_reservations`
- `event_outbox`

它仍然不负责正式订单创建，但它已经负责 Reservation 账本和 Outbox 账本。

### 3.3 RabbitMQ 是唯一 MQ

当前运行时已经是 RabbitMQ 单轨：

- `reservation.created`
- `reservation.timeout.check`
- `order.created`
- `payment.requested`
- `payment.succeeded`
- `order.completed`

## 4. 服务边界

1. `gateway`
- 负责 HTTP 接入、JWT、路由转发、可选限流
- 性能测试时不一定是主入口

2. `product-service`
- 负责商品 CRUD
- 负责秒杀商品源头库存拆分与 Redis 装载
- 不再是秒杀主链路里每单必经的同步扣库存点

3. `seckill-service`
- 负责秒杀同步准入
- 负责 Redis 热路径
- 负责 Reservation 和 Outbox 的同步持久化
- 负责失败补偿时按原始 `shard_no` 精确回补

4. `order-service`
- 负责消费 `reservation.created`
- 负责 `processed_messages` 幂等
- 负责订单事务化落库
- 负责支付账本、支付回调、Outbox 事件推进
- 负责超时检查与补偿触发

## 5. 主链路

### 5.1 同步入口

`client -> gateway -> seckill-service`

或性能测试主路径：

`benchmark client -> seckill-service`

`seckill-service` 当前流程：

1. 商品元数据/Bloom 过滤
2. 本地库存/本地配额快速过滤
3. Redis：
   - user claim
   - 分片探测顺序生成
   - 单分片 reserve Lua
   - finalize Lua
4. MySQL：
   - `seckill_reservations`
   - `event_outbox`
5. RabbitMQ 发布 Reservation 事件

### 5.2 异步链路

`order-service` 当前流程：

1. 消费 `reservation.created`
2. 校验 `processed_messages`
3. 事务化写入：
   - `orders`
   - `seckill_orders`
   - `seckill_reservations`
   - `order_status_logs`
   - `event_outbox(order.created)`
4. 支付推进：
   - `payments`
   - `payment_callbacks`
   - `payment.requested`
   - `payment.succeeded`
   - `order.completed`
5. 回写 `seckill-service` 热状态与 Reservation 状态

### 5.3 补偿链路

`reservation.timeout.check` 通过 `TTL + DLX` 进入检查队列后：

1. `order-service` 先查 MySQL 订单事实
2. 若订单不存在，则调用 `CompensateFailedOrder`
3. `seckill-service` 按 `shard_no` 回补：
   - 总库存
   - 原分片库存
   - 用户占位
   - reserve 标记
   - Reservation 状态

## 6. 当前最重要的不变量

1. `shard_no` 不能丢
- 丢了就无法正确补偿

2. Redis 不是最终事实
- MySQL 才是最终业务真相

3. Outbox 是可靠消息边界
- 不要把 RabbitMQ 当成唯一可靠事实

4. 秒杀主链路不要默认再去同步扣 Product DB 库存
- 当前 `ProcessSeckillOrder` 已经去掉这一步

## 7. 推荐阅读顺序

- `README.md`
- `AGENT.md`
- `scripts/start-all.ps1`
- `product-service/internal/model/product_model.go`
- `product-service/internal/redis/product_redis.go`
- `seckill-service/internal/redis/seckill_redis.go`
- `seckill-service/internal/logic/seckill_logic.go`
- `seckill-service/internal/model/reservation_ledger.go`
- `order-service/internal/service/order_service.go`
- `order-service/internal/model/seckill_order_tx_manager.go`
- `order-service/internal/model/payment_ledger.go`

## 8. 当前容易踩坑的旧认知

- 旧说法“项目用 Kafka/RocketMQ”已经不成立
- 旧说法“seckill-service 完全不碰 MySQL”已经不成立
- 旧说法“网关是唯一合理压测入口”不再成立
- 旧说法“Redis 只有单库存 key + 一个大 Lua”也不成立

## 9. 协作要求

- 做实现前先看代码，不盲信旧文档
- 改一致性链路时，同时检查：
  - Redis 热路径
  - MySQL 账本
  - RabbitMQ 事件
  - 补偿逻辑
  - 对账逻辑
  - 文档
