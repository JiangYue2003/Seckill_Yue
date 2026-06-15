# 秒杀购买系统改造 TODO

## 1. 目标

当前系统已经具备高并发秒杀准入能力，但秒杀成功后的订单可靠性、可审计性、支付闭环、对账闭环仍然偏弱。

本改造目标是把现有系统从：

- Redis 准入 + MQ 异步落单 + TTL/补偿兜底

提升为：

- Redis 负责热点准入与限流
- MySQL 负责订单事实、预占事实、支付事实、事件事实
- MQ 负责异步解耦，不再单独承担“唯一可靠投递入口”
- 对账从“修 Redis 状态”升级为“修订单账本 + 预占账本 + 支付账本 + 事件账本”

约束前提：

- 保留当前 Gin + go-zero + gRPC + RabbitMQ + Redis + MySQL 技术栈
- 保留 Redis Lua 在秒杀入口的高并发价值
- 假设支付一定成功且立刻成功，但支付接口、支付单、支付回调、支付审计链路必须完整存在

---

## 2. 当前问题审计结论

### 2.1 当前系统能解决的问题

- 能通过 Redis Lua 保证热点秒杀库存不超卖
- 能通过本地预扣减 + Redis 裁决减少大量无效流量
- 能通过 MQ 异步削峰，把下单逻辑从用户请求主链路移出

### 2.2 当前系统的核心缺陷

1. `Redis pending` 实际承担了“预订单状态”的职责，但它不是持久化账本。
2. 秒杀成功后，系统会立刻返回，MQ 异步投递失败时依赖 TTL 自然回收，缺少 durable event record。
3. `orders` 与 `seckill_orders` 不是同事务闭环，`seckill_orders` 插入失败当前被视为 non-critical。
4. 没有 `reservation ledger`，因此无法精确回答“每一份被预占的库存最终去了哪里”。
5. 没有正式支付单、支付回调单、支付审计单，因此系统还不是完整的购买系统。
6. 当前对账偏向“Redis 状态修复”，还不是“订单/支付/预占/事件”多账本核对。

### 2.3 改造原则

1. Redis 只能做高并发准入和热点状态缓存，不能作为最终事实源。
2. MySQL 中必须存在完整的持久化状态机和事件账本。
3. MQ 可靠性要通过 outbox 保证，而不是仅靠内存缓冲和 TTL。
4. 支付必须进入正式状态机，即使当前业务上假设“秒支付且必成功”。
5. 对账必须以数据库账本为中心，Redis 只作为热状态副本参与校验。

---

## 3. 改造后的状态机

### 3.1 秒杀订单主状态机

订单主表 `orders` 建议扩展为如下状态：

| 状态码 | 状态名 | 含义 |
|---|---|---|
| 0 | `INIT` | 已生成业务订单号，但订单事实尚未正式创建 |
| 1 | `RESERVED` | 秒杀资格与库存预占已成功，等待订单正式落库 |
| 2 | `ORDER_CREATED` | 订单已正式落库，等待支付 |
| 3 | `PAYING` | 已发起支付，请求支付网关中 |
| 4 | `PAID` | 支付成功，待履约/待完成 |
| 5 | `COMPLETED` | 订单完成 |
| 6 | `CANCELLED` | 用户取消或系统取消 |
| 7 | `EXPIRED` | 预占过期，未完成支付或订单创建超时 |
| 8 | `FAILED` | 系统性失败，已释放资源 |
| 9 | `REFUNDED` | 发生退款 |

其中：

- 秒杀链路至少会走到 `PAID`
- 因为当前假设“用户一定秒支付成功”，正常主链路应为：
  - `INIT -> RESERVED -> ORDER_CREATED -> PAYING -> PAID -> COMPLETED`

### 3.2 Reservation 状态机

新增表：`seckill_reservations`

Reservation 不是订单本身，而是“秒杀库存与购买资格预占事实”。

建议状态：

| 状态码 | 状态名 | 含义 |
|---|---|---|
| 0 | `RESERVED` | Redis 裁决成功，资格已预占 |
| 1 | `ORDER_CREATING` | 正在由消费者/事务生成订单 |
| 2 | `ORDER_CREATED` | 订单已创建，等待支付 |
| 3 | `PAYING` | 已发起支付 |
| 4 | `PAID` | 支付成功 |
| 5 | `CONSUMED` | 库存预占已经转化为有效成交 |
| 6 | `RELEASED` | 预占已释放，库存回补 |
| 7 | `EXPIRED` | 超时未完成，系统释放 |
| 8 | `FAILED` | 发生不可恢复失败 |

要求：

- Reservation 是“秒杀事实主线”的第一持久化账本
- 一次秒杀请求必须先写 Reservation，再谈订单/支付
- Redis 中的 `pending/success/failed` 只作为热状态镜像，不再是主状态

### 3.3 Payment 状态机

新增表：`payments`

虽然当前业务假设“支付一定立刻成功”，但系统层仍必须具备支付单与支付回调语义。

建议状态：

| 状态码 | 状态名 | 含义 |
|---|---|---|
| 0 | `PAY_INIT` | 支付单初始化 |
| 1 | `PAY_REQUESTED` | 已调用支付服务/适配器 |
| 2 | `PAY_SUCCESS` | 支付成功 |
| 3 | `PAY_FAILED` | 支付失败 |
| 4 | `PAY_CLOSED` | 支付关闭 |
| 5 | `PAY_REFUNDED` | 已退款 |

当前默认主链路：

- `PAY_INIT -> PAY_REQUESTED -> PAY_SUCCESS`

注意：

- 即使是“立即成功”，也必须保留：
  - 发起支付接口
  - 第三方回调接口
  - 回调验签/审计记录
  - 幂等回调处理

### 3.4 Outbox 状态机

新增表：`event_outbox`

建议状态：

| 状态码 | 状态名 | 含义 |
|---|---|---|
| 0 | `NEW` | 待投递 |
| 1 | `PUBLISHED` | 已投递到 MQ |
| 2 | `CONSUMED_ACKED` | 下游确认已消费，可选 |
| 3 | `FAILED` | 投递失败待重试 |
| 4 | `DEAD` | 超过最大重试次数，进入人工处理 |

---

## 4. 改造后的表设计

### 4.1 `seckill_reservations`

作用：

- 记录每一笔秒杀资格预占
- 作为 Redis 裁决结果的持久化投影
- 作为后续订单、支付、补偿、对账的起点

建议字段：

| 字段 | 类型 | 说明 |
|---|---|---|
| `reservation_id` | varchar(64) | 预占号，建议与业务订单号关联 |
| `order_id` | varchar(64) | 业务订单号 |
| `user_id` | bigint | 用户 ID |
| `seckill_product_id` | bigint | 秒杀商品 ID |
| `product_id` | bigint | 商品 ID |
| `quantity` | int | 数量 |
| `amount` | bigint | 金额（分） |
| `status` | tinyint | Reservation 状态 |
| `source` | varchar(32) | 来源，如 `gateway` / `reconcile` |
| `reason` | varchar(64) | 状态变更原因 |
| `redis_order_key` | varchar(128) | 热状态 key 快照 |
| `expire_at` | bigint | 预占过期时间 |
| `created_at` | bigint | 创建时间 |
| `updated_at` | bigint | 更新时间 |

约束建议：

- `uk_order_id`
- `idx_user_spid`
- `idx_status_expire_at`

### 4.2 `event_outbox`

作用：

- 解决“业务落库成功但消息未发出去”的问题
- 替代当前“Lua 成功 -> 直接异步 MQ”的脆弱空窗

建议字段：

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | bigint auto_increment | 主键 |
| `event_id` | varchar(64) | 全局事件 ID |
| `aggregate_type` | varchar(32) | `reservation` / `order` / `payment` |
| `aggregate_id` | varchar(64) | 对应主实体 ID |
| `event_type` | varchar(64) | `reservation.created` / `order.created` / `payment.succeeded` 等 |
| `payload_json` | json | 事件内容 |
| `status` | tinyint | outbox 状态 |
| `retry_count` | int | 重试次数 |
| `next_retry_at` | bigint | 下次重试时间 |
| `last_error` | varchar(512) | 最后错误 |
| `created_at` | bigint | 创建时间 |
| `updated_at` | bigint | 更新时间 |

约束建议：

- `uk_event_id`
- `idx_status_next_retry_at`
- `idx_aggregate`

### 4.3 `payments`

作用：

- 记录支付请求、支付成功、支付回调、支付退款
- 提供支付审计与支付对账基础

建议字段：

| 字段 | 类型 | 说明 |
|---|---|---|
| `payment_id` | varchar(64) | 支付单号 |
| `order_id` | varchar(64) | 订单号 |
| `user_id` | bigint | 用户 ID |
| `amount` | bigint | 支付金额 |
| `channel` | varchar(32) | 支付渠道，如 `mock_alipay` |
| `status` | tinyint | 支付状态 |
| `third_party_trade_no` | varchar(64) | 第三方流水号 |
| `request_id` | varchar(64) | 请求幂等号 |
| `paid_at` | bigint | 支付成功时间 |
| `closed_at` | bigint | 关闭时间 |
| `created_at` | bigint | 创建时间 |
| `updated_at` | bigint | 更新时间 |

约束建议：

- `pk_payment_id`
- `uk_order_id`
- `uk_request_id`

### 4.4 `payment_callbacks`

作用：

- 保存支付平台回调原文
- 作为支付回调幂等和审计依据

建议字段：

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | bigint auto_increment | 主键 |
| `payment_id` | varchar(64) | 支付单号 |
| `order_id` | varchar(64) | 订单号 |
| `callback_id` | varchar(64) | 回调幂等号/通知号 |
| `channel` | varchar(32) | 渠道 |
| `raw_payload` | json/text | 原始回调内容 |
| `verify_result` | tinyint | 验签结果 |
| `process_result` | tinyint | 处理结果 |
| `received_at` | bigint | 接收时间 |
| `created_at` | bigint | 创建时间 |

约束建议：

- `uk_callback_id`
- `idx_payment_id`

### 4.5 `processed_messages`

作用：

- 消费者幂等表
- 防止 MQ 重复投递导致重复创建订单/重复支付推进

建议字段：

| 字段 | 类型 | 说明 |
|---|---|---|
| `message_id` | varchar(64) | 消息 ID |
| `consumer_name` | varchar(64) | 消费者名 |
| `status` | tinyint | 处理结果 |
| `processed_at` | bigint | 处理时间 |
| `created_at` | bigint | 创建时间 |

联合唯一键：

- `uk_message_consumer(message_id, consumer_name)`

### 4.6 `order_status_logs`

作用：

- 记录订单状态流转
- 支持审计、问题追踪、对账辅助

建议字段：

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | bigint auto_increment | 主键 |
| `order_id` | varchar(64) | 订单号 |
| `from_status` | tinyint | 原状态 |
| `to_status` | tinyint | 新状态 |
| `event_type` | varchar(64) | 驱动事件 |
| `reason` | varchar(128) | 原因 |
| `operator` | varchar(64) | 系统/人工 |
| `trace_id` | varchar(64) | 链路追踪 ID |
| `created_at` | bigint | 创建时间 |

---

## 5. 改造后的核心接口设计

### 5.1 `seckill-service`

#### 对外秒杀接口

- `POST /api/v1/seckill`

行为改造：

1. 先做 Redis Lua 准入
2. Lua 成功后，不直接把 Redis `pending` 当最终事实
3. 必须写入 `seckill_reservations`
4. 同事务写入 `event_outbox(reservation.created)`
5. 返回：
   - `accepted`
   - `reservation_id`
   - `order_id`
   - `status=RESERVED`

#### 状态查询接口

- `GET /api/v1/seckill/status`
- `GET /api/v1/seckill/result`

返回优先级改为：

1. 先读 MySQL 中的 Reservation/Order 事实
2. Redis 只作为热点缓存加速
3. 如 MySQL 与 Redis 不一致，以 MySQL 为准并触发修复任务

### 5.2 `order-service`

新增职责：

- 消费 `reservation.created`
- 幂等创建订单
- 写 `orders`
- 写 `seckill_orders`
- 写 `order_status_logs`
- 同事务写 `event_outbox(order.created)`

### 5.3 `payment-service` 或 `order-service` 内部支付子域

建议新增独立 `payment-service`，如果短期不拆，也至少在 `order-service` 内形成清晰子域：

- `CreatePayment(order_id)`
- `MarkPaymentSuccess(payment_id, third_party_trade_no)`
- `HandlePaymentCallback(callback_payload)`
- `ClosePayment(order_id)`
- `RefundPayment(order_id)`

### 5.4 支付回调接口

新增：

- `POST /api/v1/payment/callback/mock`

即使当前假设“支付必成功、秒成功”，也必须保留：

1. 支付发起
2. 第三方回调
3. 回调验签
4. 回调幂等
5. 回调落库
6. 回调后推进订单状态

### 5.5 对账接口/任务

新增对账任务维度：

- Reservation vs Order
- Order vs Payment
- Payment vs Callback
- Reservation vs Redis
- Outbox vs MQ / ProcessedMessages

---

## 6. 改造后的新链路图

## 6.1 秒杀购买主链路

```text
User
  |
  v
Gateway
  |
  v
SeckillService
  |
  | 1. Redis Lua 准入与预占
  | 2. 写 seckill_reservations(status=RESERVED)
  | 3. 写 event_outbox(event=reservation.created)
  v
HTTP Return: accepted / reservation_id / order_id

OutboxPublisher
  |
  v
RabbitMQ (reservation.created)
  |
  v
OrderService Consumer
  |
  | 4. processed_messages 幂等检查
  | 5. 同事务写:
  |    - orders(status=ORDER_CREATED)
  |    - seckill_orders
  |    - order_status_logs
  |    - seckill_reservations(status=ORDER_CREATED)
  |    - event_outbox(event=order.created)
  v
OutboxPublisher
  |
  v
RabbitMQ (order.created)
  |
  v
PaymentService Consumer / SyncInvoker
  |
  | 6. 创建 payments(status=PAY_INIT)
  | 7. 调用 MockPayAdapter
  | 8. 立即得到成功响应
  | 9. 写 payments(status=PAY_SUCCESS)
  | 10. 写 event_outbox(event=payment.succeeded)
  v
PaymentCallbackHandler
  |
  | 11. 接收并记录 payment_callbacks
  | 12. 幂等确认 payment success
  | 13. 推进:
  |     - orders(status=PAID/COMPLETED)
  |     - seckill_reservations(status=CONSUMED)
  |     - Redis 热状态 success
  v
User Query Result = success
```

## 6.2 超时释放链路

```text
ReservationTimeoutScanner
  |
  | 扫描 status in (RESERVED, ORDER_CREATED, PAYING)
  | and expire_at < now
  v
ReleaseWorker
  |
  | 同事务:
  | - seckill_reservations -> EXPIRED / RELEASED
  | - orders -> EXPIRED / FAILED
  | - order_status_logs
  | - event_outbox(reservation.released)
  v
CompensationExecutor
  |
  | - 回补 Redis 秒杀库存
  | - 删除用户预占 key
  | - 修正 Redis 热状态 failed
  v
Finished
```

## 6.3 支付回调链路

```text
MockPayProvider
  |
  v
Payment Callback API
  |
  | 1. 记录原始回调 payment_callbacks
  | 2. callback_id 幂等去重
  | 3. 验签/验参
  | 4. 更新 payments -> PAY_SUCCESS
  | 5. 更新 orders -> PAID / COMPLETED
  | 6. 更新 reservation -> PAID / CONSUMED
  | 7. 更新 Redis 热状态 success
  | 8. 写 order_status_logs
  v
Ack callback
```

---

## 7. 改造后的支付流程

## 7.1 为什么即使“支付一定成功”也必须保留支付域

因为一个完整购买系统必须能回答：

- 用户是否真的支付过
- 第三方支付平台是否回调过
- 支付请求是否被重复调用
- 回调是否被重复投递
- 某一笔订单为什么到了已支付
- 如果后续改成真实支付网关，系统是否不需要重写主流程

因此本系统必须保留完整支付子流程，即使当前 Mock 设定为“必成功且立刻成功”。

## 7.2 建议的支付流程

### 发起支付

1. `order-service` 在订单创建后调用 `payment-service.CreatePayment`
2. `payment-service` 创建 `payments(status=PAY_INIT)`
3. 调用 `MockPayAdapter`
4. `MockPayAdapter` 同步返回 `success`
5. `payment-service` 先把支付单推进到 `PAY_REQUESTED`
6. 再推进到 `PAY_SUCCESS`
7. 同时写入 `event_outbox(payment.succeeded)`

### 回调处理

即使同步成功，也仍然模拟第三方回调：

1. `MockPayAdapter` 或内部回调任务向回调接口发送通知
2. 回调接口把原始内容写入 `payment_callbacks`
3. 以 `callback_id` 做幂等
4. 如果支付已经成功则安全幂等返回
5. 如果未成功，则推进支付与订单状态

### 审计要求

必须能查询：

- `payment_id`
- `order_id`
- `request_id`
- `third_party_trade_no`
- 原始回调载荷
- 订单状态推进日志

---

## 8. 改造后的对账设计

## 8.1 每日/准实时对账维度

### 账本 1：Reservation vs Redis

校验内容：

- `RESERVED / ORDER_CREATED / PAYING` 的 Reservation 是否在 Redis 中仍有热状态
- `CONSUMED / RELEASED / EXPIRED / FAILED` 的 Reservation 是否仍残留错误热状态

### 账本 2：Reservation vs Order

校验内容：

- Reservation 已 `ORDER_CREATED`，但 `orders` 不存在
- `orders` 存在，但 Reservation 仍停在 `RESERVED`
- `orders` 已 `PAID`，但 Reservation 未 `CONSUMED`

### 账本 3：Order vs Payment

校验内容：

- 订单已 `PAID`，支付单却不是 `PAY_SUCCESS`
- 支付单是 `PAY_SUCCESS`，订单却仍 `ORDER_CREATED/PAYING`

### 账本 4：Payment vs Callback

校验内容：

- 支付成功但没有回调记录
- 回调存在但支付状态未更新
- 同一回调被重复处理

### 账本 5：Outbox vs MQ/Consumer

校验内容：

- Outbox 长时间处于 `NEW/FAILED`
- 消息已发布但消费者未记录 `processed_messages`
- `processed_messages` 已存在但订单未推进

## 8.2 自动修复边界

允许自动修复：

- DB 成功但 Redis 热状态未更新
- 支付成功但订单状态未推进
- Reservation 已释放但 Redis 用户 key 未删除
- Outbox 重试恢复

禁止自动修复，需人工介入：

- 多账本核心金额不一致
- 同一订单存在多支付成功记录
- Reservation 与 Order 指向不同商品/数量
- Payment 回调验签失败但订单已成功

---

## 9. 服务边界建议

## 9.1 推荐拆分

### `seckill-service`

负责：

- Redis 热点准入
- 资格预占
- Redis 热状态更新
- 释放/补偿执行器

不负责：

- 最终订单事实
- 最终支付事实

### `order-service`

负责：

- 订单事实
- 秒杀订单映射
- 订单状态机
- 订单状态日志

### `payment-service`

负责：

- 支付单
- 支付回调
- 支付审计
- 退款状态

### `tools/reconcile`

负责：

- 多账本核对
- 自动修复可恢复问题
- 输出人工介入问题

---

## 10. 实施优先级

## Phase A：先补事实账本

1. 新增 `seckill_reservations`
2. 新增 `event_outbox`
3. 新增 `processed_messages`
4. 新增 `order_status_logs`

### 目标

- 先把“秒杀成功但没有 durable fact”的问题解决

## Phase B：改造秒杀主链路

1. `SeckillLogic` 不再只写 Redis `pending`
2. Lua 成功后必须写 Reservation + Outbox
3. MQ 改为消费 `reservation.created`

### 目标

- Redis 不再单独承担预订单事实

## Phase C：改造订单持久化

1. `orders` 与 `seckill_orders` 同事务
2. 增加 `processed_messages` 幂等消费
3. 增加 `order_status_logs`

### 目标

- 订单事实具备事务闭环

## Phase D：补齐支付域

1. 新增 `payments`
2. 新增 `payment_callbacks`
3. 新增支付接口、支付回调接口、Mock 支付适配器
4. 支付成功后推进订单与 Reservation

### 目标

- 系统成为完整购买系统，而不仅是“下单系统”

## Phase E：升级对账

1. 修复 `tools/reconcile` 当前 Redis key 兼容问题
2. 对账对象扩展到 Reservation/Order/Payment/Outbox/Redis
3. 增加自动修复和人工告警边界

### 目标

- 建立真实生产可用的账务闭环

---

## 11. 需要同步修正的当前代码问题

1. `seckill-service` 当前把 MQ 投递失败交给 TTL 自然回收，这部分后续应由 Reservation + Outbox 接管。
2. `order-service` 当前把 `seckill_orders` 插入失败视为 non-critical，后续必须改为同事务事实。
3. `tools/reconcile` 当前仍按旧 Redis key `seckill:order:{orderId}` 读取状态，需要兼容新格式。
4. 当前 `README.md` 中“秒杀库存权威在 Redis”应修正为：
   - Redis 是热点裁决源
   - Reservation/Order/Payment 是最终事实源

---

## 12. 最终目标态

改造完成后，系统应满足：

1. Redis 只负责热点准入、热状态与短期裁决。
2. 所有订单、支付、预占、事件、状态流转都有持久化账本。
3. MQ 发布失败不会再导致“只有 Redis 成功”的长尾不确定状态。
4. 支付虽可 Mock 成即时成功，但全套支付/回调/审计流程完整存在。
5. 对账能够回答：
   - 哪些库存被预占了
   - 哪些预占变成了订单
   - 哪些订单完成了支付
   - 哪些支付回调被处理了
   - 哪些异常需要自动修复，哪些需要人工介入

这时该系统才算一个完整的秒杀购买系统，而不是仅仅一个高并发秒杀入口系统。

---

## 13. Codex 实施指南

本节是给后续 Codex 或工程师直接执行的实施手册，目标不是讨论方向，而是明确“下一步具体改哪些模块、按什么顺序改、如何验证改对了”。

执行原则：

1. 每次只做一个阶段，阶段内保持可编译。
2. 先加新表、新结构、新接口，再迁移旧逻辑，最后删除旧路径。
3. 所有新状态流转必须先定义常量、枚举、状态日志，再写业务逻辑。
4. 所有跨服务推进必须保留幂等键和审计日志。
5. 每个阶段结束都要补测试和回归验证，不允许一次性大爆改。

### 13.1 建议实施顺序

严格按下面顺序执行：

1. Phase A：数据模型与 SQL 迁移
2. Phase B：proto 与 common 契约扩展
3. Phase C：`order-service` 订单事实闭环
4. Phase D：`seckill-service` 从 Redis-only 改为 Reservation-first
5. Phase E：`payment-service` 或支付子域落地
6. Phase F：Gateway 支付与查询接口扩展
7. Phase G：对账工具升级
8. Phase H：旧逻辑兼容清理

不要跳顺序。尤其不能先改秒杀主链路而没有 Reservation 表和 Outbox 表。

---

## 14. Phase A：数据模型与 SQL 迁移

### 14.1 目标

先把数据库账本基础打好，使后续代码不再被迫依赖 Redis 临时状态。

### 14.2 需要新增的表

在 [docs/schema.sql](F:/sec1.1/docs/schema.sql:1) 后续新增：

- `seckill_reservations`
- `event_outbox`
- `processed_messages`
- `payments`
- `payment_callbacks`
- `order_status_logs`

### 14.3 需要修改的既有表

#### `orders`

扩展字段：

- `reservation_id`
- `pay_status`
- `closed_at`
- `expired_at`
- `version`

说明：

- `status` 保留，但改用新的完整状态集
- `pay_status` 独立于订单状态，避免支付推进与订单推进混在一个字段里

#### `seckill_orders`

新增字段：

- `reservation_id`
- `status`

目的：

- 不再只是“购买记录映射表”
- 成为订单与预占之间的辅助索引

### 14.4 SQL 约束要求

必须补齐：

- 唯一键：`orders.order_id`
- 唯一键：`seckill_reservations.order_id`
- 唯一键：`payments.order_id`
- 唯一键：`payments.request_id`
- 唯一键：`payment_callbacks.callback_id`
- 联合唯一键：`processed_messages(message_id, consumer_name)`
- 索引：所有状态 + 时间扫描路径

### 14.5 Phase A 验收

- `docs/schema.sql` 包含所有新表
- 新表字段、唯一键、索引定义齐全
- 字段注释写明语义
- 文档与状态机中的状态值一一对应

---

## 15. Phase B：Proto 与 common 契约扩展

### 15.1 目标

把 Reservation、支付、回调、对账修复所需的 RPC 契约先扩出来，让后续服务改造不再靠内部硬编码。

### 15.2 需要修改的 proto

#### [common/proto/seckill.proto](F:/sec1.1/common/proto/seckill.proto:1)

新增/改造：

- `SeckillResponse` 增加：
  - `reservation_id`
  - `reservation_status`
- `SeckillStatusResponse` 增加：
  - `reservation_status`
  - `order_status`
  - `payment_status`
- 新增：
  - `ReleaseReservationRequest`
  - `ReleaseReservationResponse`
  - `GetReservationRequest`
  - `ReservationInfo`

#### [common/proto/order.proto](F:/sec1.1/common/proto/order.proto:1)

新增/改造：

- `OrderInfo` 增加：
  - `reservation_id`
  - `reservation_status`
  - `payment_status`
  - `expired_at`
- `PayOrderRequest` 不再直接要求前端传 `payment_id`
  - 改成 `order_id`
  - 可选 `pay_channel`
  - 可选 `request_id`
- 新增：
  - `CreatePaymentRequest`
  - `CreatePaymentResponse`
  - `HandlePaymentCallbackRequest`
  - `PaymentInfo`
  - `GetPaymentRequest`

### 15.3 common 枚举建议

在 [common/proto/common.proto](F:/sec1.1/common/proto/common.proto:1) 或拆出新公共 proto 时新增：

- `ReservationStatus`
- `OrderLifecycleStatus`
- `PaymentStatus`
- `OutboxStatus`

要求：

- 所有服务共享同一组枚举，不允许字符串散落在各服务里

### 15.4 Phase B 验收

- 所有 proto 重新生成成功
- `gateway`、`seckill-service`、`order-service`、`tools/reconcile` 均引用新的 common 契约
- 不再新增字符串硬编码状态值

---

## 16. Phase C：Order-Service 改造

### 16.1 目标

让 `order-service` 成为订单事实中心，而不是“MQ 消费后插一条订单记录再回写 Redis”的轻量消费者。

### 16.2 必改模块

#### 模型层

需要新增模型：

- `reservation_model.go`
- `payment_model.go`
- `payment_callback_model.go`
- `outbox_model.go`
- `processed_message_model.go`
- `order_status_log_model.go`

建议位置：

- `order-service/internal/model/`

#### Service 层

重点重构 [order-service/internal/service/order_service.go](F:/sec1.1/order-service/internal/service/order_service.go:1)

当前问题：

- 秒杀订单落库成功后只调用 `markSeckillOrderSuccess`
- 没有 Reservation 状态推进
- 没有 Outbox 写入
- 没有 Payment 创建

改造后要求：

1. 消费 `reservation.created`
2. 先写 `processed_messages`
3. 在同一个 DB 事务中写：
   - `orders`
   - `seckill_orders`
   - `seckill_reservations`
   - `order_status_logs`
   - `event_outbox(order.created)`
4. 提交成功后才 ACK 消息

#### 批量写入器

[order-service/internal/batch/batch_writer.go](F:/sec1.1/order-service/internal/batch/batch_writer.go:1) 必须改。

当前问题：

- `orders` 与 `seckill_orders` 不是强事务闭环
- `seckill_orders` 失败被视为 non-critical

改造要求：

- 要么废弃 BatchWriter
- 要么升级为“事务型批处理器”

优先建议：

- 秒杀订单链路先放弃这层异步批刷盘，改为严格事务落库
- 等闭环建立后，再评估是否重新引入批量写入

### 16.3 支付接口改造

[order-service/internal/logic/pay_order_logic.go](F:/sec1.1/order-service/internal/logic/pay_order_logic.go:1) 当前只是直接把订单更新为已支付。

必须改为：

1. 校验订单状态是否允许支付
2. 创建支付单 `payments`
3. 调用支付子域
4. 等支付回调或同步成功后推进订单

即：

- `PayOrder` 不再等价于“改订单状态”
- `PayOrder` 应等价于“创建支付请求”

### 16.4 Order-Service 验收

- 秒杀订单消费成功后，`orders`、`seckill_orders`、`seckill_reservations`、`order_status_logs` 同事务落库
- MQ 重复消息不会重复建单
- `PayOrder` 不再直接修改订单为已支付

---

## 17. Phase D：Seckill-Service 改造

### 17.1 目标

让 `seckill-service` 从“Redis 裁决 + 内存异步投递中心”变成“热点准入与预占中心”。

### 17.2 必改模块

#### [seckill-service/internal/logic/seckill_logic.go](F:/sec1.1/seckill-service/internal/logic/seckill_logic.go:1)

当前问题：

- Lua 成功后直接依赖异步 MQ
- 失败靠 TTL
- Redis `pending` 实际承担事实状态

改造要求：

1. 保留 Lua 热点裁决
2. Lua 成功后立刻构造 `reservation_id/order_id`
3. 同步写：
   - `seckill_reservations(status=RESERVED)`
   - `event_outbox(reservation.created)`
4. 再返回前端

注意：

- 这里允许 Redis 先成功、DB 再失败，但此时必须立刻走本地补偿：
  - 写 Reservation 失败时，同步执行 Redis 释放逻辑
  - 不能继续返回“成功，处理中”

#### `AsyncProducer`

[seckill-service/internal/mq/async_producer.go](F:/sec1.1/seckill-service/internal/mq/async_producer.go:1) 不再作为主事实链路。

改造原则：

- 保留它仅作为短期兼容路径或内部加速层
- 主链路改由 Outbox Publisher 负责

因此：

- “消息丢弃后依赖 TTL” 这套语义必须降级为过渡方案
- 文档和日志中不再把它定义为主保障手段

#### 查询逻辑

[seckill-service/internal/logic/get_seckill_status_logic.go](F:/sec1.1/seckill-service/internal/logic/get_seckill_status_logic.go:1)  
[seckill-service/internal/logic/get_seckill_result_logic.go](F:/sec1.1/seckill-service/internal/logic/get_seckill_result_logic.go:1)

改造要求：

查询优先级：

1. MySQL Reservation/Order/Payment
2. Redis 热状态
3. Redis 丢失时返回事实状态，不再把“查不到 Redis”直接等于最终失败

### 17.3 补偿接口

[seckill-service/internal/logic/compensate_failed_order_logic.go](F:/sec1.1/seckill-service/internal/logic/compensate_failed_order_logic.go:1)

改造为：

- 不只是改 Redis
- 必须同时支持：
  - 释放 Reservation
  - 记录原因
  - 更新状态日志

### 17.4 Seckill-Service 验收

- 前端拿到的“成功”实际上表示 `RESERVED`
- Redis 丢失不再意味着事实丢失
- Release/Expire/Failed 路径有 Reservation 账本可追

---

## 18. Phase E：Payment 子域落地

### 18.1 目标

补齐“购买系统”最关键缺口：支付单、回调、幂等、审计。

### 18.2 服务形态

建议新增独立目录：

- `payment-service/`

如果短期不拆，也必须在 `order-service/internal/payment/` 形成独立子域，不允许支付逻辑散落在订单 logic 中。

### 18.3 需要具备的能力

#### 1. 创建支付单

输入：

- `order_id`
- `user_id`
- `amount`
- `channel`
- `request_id`

输出：

- `payment_id`
- `status`
- `pay_url` 或 mock token

#### 2. 模拟支付成功

因为当前假设“总是立刻成功”，Mock 支付适配器必须：

1. 返回同步成功
2. 仍然异步回调一次系统的 callback API

#### 3. 回调幂等

支付回调必须以 `callback_id` 去重，并支持重复通知安全返回。

#### 4. 推进订单

支付成功后必须推进：

- `payments -> PAY_SUCCESS`
- `orders -> PAID -> COMPLETED`
- `seckill_reservations -> PAID/CONSUMED`
- `order_status_logs`
- `event_outbox(payment.succeeded/order.completed)`

### 18.4 Gateway 变更

[gateway/internal/handler/order.go](F:/sec1.1/gateway/internal/handler/order.go:1) 需要修改：

- `PayOrderRequest` 不再要求前端提交 `paymentId`
- 改为：
  - `orderId`
  - `channel`
  - 可选 `requestId`

并新增支付回调 HTTP 路由，例如：

- `POST /api/v1/payment/callback/mock`

### 18.5 Payment 验收

- 用户可触发支付请求
- 系统生成支付单
- 系统收到 mock 回调
- 回调幂等
- 订单状态由支付链路推进，而不是由前端直接提交 `paymentId`

---

## 19. Phase F：Outbox Publisher 与消费幂等

### 19.1 目标

建立可靠事件投递，而不是依赖“业务线程里顺手发 MQ”。

### 19.2 新组件

建议新增：

- `order-service/internal/outbox/publisher.go`
- `seckill-service/internal/outbox/publisher.go`
- 或统一抽到 `common/outbox`

### 19.3 Publisher 行为

Publisher 周期扫描 `event_outbox where status in (NEW, FAILED)`：

1. 获取一批待投递事件
2. 发 MQ
3. 成功则更新 `PUBLISHED`
4. 失败则更新 `FAILED + retry_count + next_retry_at`
5. 超过阈值进入 `DEAD`

### 19.4 Consumer 幂等

每个消费者处理第一步必须是：

1. 检查 `processed_messages(message_id, consumer_name)`
2. 已存在则直接 ACK
3. 不存在则开始业务事务
4. 事务成功后写 `processed_messages`

### 19.5 验收

- 发布失败不会丢失业务事件
- 同一消息多次投递不会重复创建订单/重复支付

---

## 20. Phase G：对账工具升级

### 20.1 当前必须先修的问题

[tools/reconcile/main.go](F:/sec1.1/tools/reconcile/main.go:1) 当前仍然按旧 Redis key `seckill:order:{orderId}` 读状态。

第一步必须修复为：

- 根据编码后的 `order_id` 解析 `spid`
- 优先读取新 key
- 兼容旧 key

### 20.2 对账工具升级目标

`tools/reconcile` 从当前的“Redis 状态修补器”升级为“多账本核对器”。

新增对账任务：

1. Reservation 与 Order 一致性
2. Order 与 Payment 一致性
3. Payment 与 Callback 一致性
4. Outbox 与 MQ 投递结果一致性
5. Redis 热状态与事实状态一致性

### 20.3 自动修复策略

允许自动修复：

- DB 状态正确但 Redis 热状态错误
- 支付成功但 Reservation 未推进
- Outbox 可重试事件

禁止自动修复：

- 金额冲突
- 订单与支付归属冲突
- 同一订单多支付成功
- 回调验签失败但已推进状态

### 20.4 验收

- reconcile 报告中能区分：
  - 可自动修复
  - 需人工介入
- 输出内容包含 Reservation/Order/Payment/Outbox 维度

---

## 21. Phase H：兼容与清理

### 21.1 需要保留的兼容期

短期内保留：

- Redis 旧 `pending/success/failed` 状态写入
- 旧查询接口的返回形状
- 旧订单状态兼容映射

### 21.2 需要最终删除的旧语义

最终应删除或废弃：

- “Lua 成功 == 秒杀购买事实成立”
- “MQ 发送失败靠 TTL 就够了”
- “PayOrder 传 `paymentId` 就能支付成功”
- “`seckill_orders` 插入失败不影响主流程”

### 21.3 文档同步

以下文档必须同步更新：

- [README.md](F:/sec1.1/README.md:1)
- `docs/schema.sql`
- 若新增支付服务，则补服务说明文档

---

## 22. 事件定义清单

建议统一定义以下事件：

- `reservation.created`
- `reservation.released`
- `reservation.expired`
- `order.created`
- `order.cancelled`
- `order.expired`
- `payment.created`
- `payment.succeeded`
- `payment.failed`
- `payment.refunded`
- `order.completed`

每个事件 payload 最低要求包含：

- `event_id`
- `trace_id`
- `occurred_at`
- `aggregate_type`
- `aggregate_id`
- `order_id`
- `reservation_id`
- `user_id`
- `seckill_product_id`
- `amount`
- `status`

---

## 23. 测试与验收矩阵

### 23.1 单元测试

必须新增：

- Reservation 状态流转测试
- Outbox 重试测试
- processed_messages 幂等测试
- Payment callback 幂等测试
- 订单状态推进测试

### 23.2 集成测试

必须覆盖：

1. 秒杀成功 -> 订单创建 -> 支付成功 -> 完成
2. 秒杀成功 -> 订单未创建 -> 超时释放
3. Outbox 发布失败 -> 重试成功
4. 回调重复通知 -> 不重复推进
5. MQ 重复消息 -> 不重复建单

### 23.3 回归测试

必须回归：

- 普通订单创建
- 普通订单取消
- 普通订单退款
- 秒杀查询接口
- 网关鉴权和限流

### 23.4 可观测性指标

建议新增 Prometheus 指标：

- `reservation_created_total`
- `reservation_released_total`
- `outbox_publish_total{status=...}`
- `payment_request_total{channel=...,status=...}`
- `payment_callback_total{status=...}`
- `reconcile_anomaly_total{type=...}`

---

## 24. Codex 每阶段输出要求

后续如果由 Codex 分阶段实施，每个阶段的提交必须至少包含：

1. 代码变更
2. SQL/契约变更
3. 对应测试
4. README/TODOList 同步
5. 验证结果

每一阶段结束时，Codex 必须给出：

- 改了哪些服务
- 是否保持兼容
- 哪些旧逻辑仍在过渡期保留
- 本阶段剩余风险

---

## 25. 最终执行建议

后续实际改造时，优先采用以下策略：

1. 先新增，不直接替换
2. 先写事实，再写缓存
3. 先写状态日志，再写状态推进
4. 先让支付成为正式子域，再让订单依赖支付推进完成
5. 先让 reconcile 看懂新账本，再考虑删旧补偿逻辑

一句话原则：

先把系统改造成“事实可落地、事件可追、支付可审、异常可对账”的购买系统，再继续谈更高并发优化。
