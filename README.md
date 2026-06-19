# Seckill-Mall（Gin + go-zero）

一个面向高并发秒杀场景的微服务系统。当前仓库的真实运行基调是：

- `RabbitMQ` 单轨异步链路
- `Redis` 物理库存分片 + 热状态裁决
- `MySQL` 承载 `reservation / order / payment / outbox / processed_messages` 事实账本
- 压测主口径以 `seckill-service` 直连 `gRPC` 为准，`gateway` 作为业务入口和对比链路保留

如果文档和代码冲突，以代码为准，并应优先修正文档。

---

## 1. 当前架构

### 1.1 服务划分

- `gateway`：HTTP 入口、JWT 鉴权、路由转发、可选限流
- `user-service`：注册、登录、刷新 token、用户信息
- `product-service`：商品管理、秒杀商品管理、秒杀库存源头分片与 Redis 装载
- `seckill-service`：秒杀同步准入、Redis 热路径、Reservation 落库、Outbox 发事件
- `order-service`：RabbitMQ 消费、订单事务化落库、支付账本、超时补偿
- `tools/reconcile`：多账本对账与修复

### 1.2 技术栈

- 网关：Gin
- 服务治理：go-zero + gRPC + etcd
- 缓存与热点裁决：Redis
- 消息队列：RabbitMQ
- 数据库：MySQL
- 可观测：Prometheus、日志、可选 OTLP/Jaeger

---

## 2. 秒杀主链路

当前真实同步链路不是“单段 Lua 把所有逻辑一次做完”，而是“本地过滤 + Redis 分片预占 + MySQL Reservation/Outbox”。

### 2.1 业务入口

业务入口仍支持：

`client -> gateway -> seckill-service`

但当前性能基准默认使用：

`benchmark client -> seckill-service (gRPC)`

因为这样可以剥离 `gateway` 的 JWT、HTTP 编解码和转发开销，更接近秒杀核心服务本身的吞吐上限。

### 2.2 同步准入链路

`seckill-service` 当前大致按下面顺序处理请求：

1. 本地元数据/Bloom 过滤，校验商品是否存在、是否在秒杀时间窗内
2. 可选本地库存配额与本地预扣，减少 Redis 热点压力
3. Redis 热路径执行：
   - 用户占位 `user claim`
   - 基于 `userId % 16` 的主分片选择
   - 按当前分片状态做 Go 层探测顺序生成
   - 单分片 `reserve` Lua 预占库存
   - `finalize` Lua 扣减总库存、写热状态 `pending`、记录 `shard_no`
4. Redis 成功后，`seckill-service` 同步写入：
   - `seckill_reservations`
   - `event_outbox`
5. Outbox Publisher 通过 RabbitMQ 发布：
   - `reservation.created`
   - `reservation.timeout.check`
6. 接口快速返回“抢购成功，订单处理中”

### 2.3 异步推进链路

`order-service` 消费 `reservation.created` 后：

1. 用 `processed_messages` 做消费幂等
2. 在同一事务里落库：
   - `orders`
   - `seckill_orders`
   - `seckill_reservations`
   - `order_status_logs`
   - `event_outbox(order.created)`
3. 支付子域继续推进：
   - `payments`
   - `payment_callbacks`
   - `payment.requested`
   - `payment.succeeded`
   - `order.completed`
4. 成功支付后通过 RPC 回写 `seckill-service`：
   - 热状态 `pending -> success`
   - Reservation 状态推进

### 2.4 超时补偿链路

`reservation.timeout.check` 通过 RabbitMQ 的 `TTL + DLX` 进入检查队列后：

1. `order-service` 查询 MySQL 是否已有订单事实
2. 如果订单不存在，则调用 `CompensateFailedOrder`
3. `seckill-service` 按原始 `shard_no` 精确补偿：
   - 热状态 `pending -> failed`
   - 回补总库存与原分片库存
   - 删除用户占位
   - 释放分片 reserve 标记
   - 推进 Reservation 失败/释放态

---

## 3. 库存模型

### 3.1 源头物理分片

当前秒杀库存已经在源头做物理分片：

- MySQL 表：`seckill_stock_shards`
- 固定分片数：`16`
- 每个秒杀商品创建/更新时会把总库存拆成 16 份

这层分片的意义是把库存事实按固定 `shard_no` 编码下来，后续 Redis 预占和补偿都能带着 `shard_no` 走完整链路。

### 3.2 Redis 运行时模型

Redis 当前不是单一库存 key，而是“控制面 + 物理分片面”：

- 总库存：`{%d}:sk:stock:total`
- 分片元数据：`{%d}:sk:stock:meta`
- 分片可用状态：`{%d}:sk:stock:state`
- 分片库存：`{%d:slot:%d}:sk:stock`
- 分片预占标记：`{%d:slot:%d}:sk:reserve:%s`
- 用户占位：`{%d}:sk:user:%d`
- 订单热状态：`{%d}:sk:order:%s`

其中：

- 控制面 key 共用 `{spid}` hash tag
- 物理分片 key 使用 `{spid:slot:n}`，用于打散热点
- 成功预占的 `shard_no` 会进入 Reservation、MQ、Order、Payment、补偿全链路

---

## 4. 一致性语义

- Redis 是热点准入和热状态镜像，不是最终购买事实来源
- 最终事实以 MySQL 为准，关键表包括：
  - `seckill_reservations`
  - `orders`
  - `seckill_orders`
  - `payments`
  - `payment_callbacks`
  - `event_outbox`
  - `processed_messages`
- RabbitMQ 只做异步解耦，可靠投递依赖 Outbox，不依赖“消息发不出去就等 TTL 自然回收”
- 补偿必须按原始 `shard_no` 回补，不能随机回到其他分片

---

## 5. 默认端口与副本

### 5.1 默认业务端口

| 服务 | 协议 | 默认地址 |
|---|---|---|
| `gateway` | HTTP | `127.0.0.1:8888` |
| `user-service` | gRPC | `127.0.0.1:9081` |
| `product-service` | gRPC | `127.0.0.1:9082` |
| `seckill-service` | gRPC | `127.0.0.1:9083` |
| `order-service` | gRPC | `127.0.0.1:9084` |

### 5.2 `start-all.ps1` 默认副本

当前启动脚本默认：

- `seckill-service = 2` 个副本
- `order-service = 2` 个副本
- `user-service = 1`
- `product-service = 1`
- `gateway = 1`

副本端口按 `+10000` 偏移，因此默认还会有：

| 服务副本 | 地址 |
|---|---|
| `seckill-service-2` | `127.0.0.1:19083` |
| `order-service-2` | `127.0.0.1:19084` |

对应指标端口：

| 组件 | 指标端口 |
|---|---|
| `gateway` | `9180` |
| `user-service` | `9181` |
| `product-service` | `9182` |
| `seckill-service` | `9183` |
| `seckill-service-2` | `19183` |
| `order-service` | `9184` |
| `order-service-2` | `19184` |

### 5.3 基础设施依赖

| 组件 | 默认地址 |
|---|---|
| `etcd` | `127.0.0.1:2379` |
| `MySQL` | `127.0.0.1:3306` |
| `Redis` | `127.0.0.1:6379` |
| `RabbitMQ` | `127.0.0.1:5672` |

---

## 6. 启动方式

### 6.1 先准备基础设施

当前服务运行时需要你先准备好：

- `etcd`
- `MySQL`
- `Redis`
- `RabbitMQ`

注意：

- `scripts/start-all.ps1` 只负责启动业务服务，不负责拉起基础设施
- 当前 `deploy/docker-compose.mq.yml` 仍是历史 `RocketMQ` 编排文件，**不适用于当前 RabbitMQ 运行时**

### 6.2 初始化数据库

```powershell
mysql -h 127.0.0.1 -u root -p < docs/schema.sql
```

### 6.3 一键启动业务服务

```powershell
powershell -ExecutionPolicy Bypass -File scripts/start-all.ps1
```

可显式指定副本数：

```powershell
powershell -ExecutionPolicy Bypass -File scripts/start-all.ps1 -SeckillReplicas 2 -OrderReplicas 2
```

停止：

```powershell
powershell -ExecutionPolicy Bypass -File scripts/stop-all.ps1
```

---

## 7. 压测与测试

### 7.1 推荐基准：直连 `seckill-service`

目录：`test/seckill-benchmark-test`

这是当前仓库下最应该使用的性能测试工具。

```powershell
cd test\seckill-benchmark-test
go run . --targets="127.0.0.1:9083,127.0.0.1:19083" --pool-size=128 --mode=burst --ideal
```

它的意义是直接测秒杀同步入口，不把 `gateway` 的 JWT 和 HTTP 转发成本混进核心吞吐。

### 7.2 对比链路：`gateway` 半链路压测

目录：`test/gateway-benchmark-test`

这个工具主要用于评估：

- `gateway` 的 JWT/HTTP 开销
- `gateway -> seckill-service` 半链路吞吐

如果只用了默认 `start-all.ps1`，通常只有一个 `gateway`：

```powershell
cd test\gateway-benchmark-test
go run . --gateway-targets=http://127.0.0.1:8888 --rate=20000 --duration=30s
```

如果你手工额外起了第二个 `gateway`，再传多个地址。

### 7.3 兼容型/历史测试工具

以下工具仍然保留，但不再是“当前新架构的主验证入口”：

- `test/seckill-functional-test`
- `test/seckill-data-tools`
- `test/test-e2e`
- `test/k6-seckill-test`

原因是它们的夹具准备方式仍带有旧兼容逻辑，部分工具会直接写兼容 Redis key，而不是完整复现当前物理分片库存装载流程。

---

## 8. 建议阅读顺序

如果你要快速理解现在的代码，不要先看旧设计稿，先看这些：

- `seckill-service/internal/redis/seckill_redis.go`
- `seckill-service/internal/logic/seckill_logic.go`
- `seckill-service/internal/model/reservation_ledger.go`
- `product-service/internal/model/product_model.go`
- `product-service/internal/redis/product_redis.go`
- `order-service/internal/service/order_service.go`
- `order-service/internal/model/seckill_order_tx_manager.go`
- `order-service/internal/model/payment_ledger.go`
- `scripts/start-all.ps1`

---

## 9. 目录结构

```text
seckill-mall/
├── gateway/
├── user-service/
├── product-service/
├── seckill-service/
├── order-service/
├── common/
├── docs/
├── scripts/
├── test/
└── tools/
```

## License

MIT
