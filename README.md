# Seckill-Mall（Gin + go-zero）

一个面向高并发场景的微服务秒杀系统，目标是同时保证：
- **高并发下不超卖**（Redis Lua 原子裁决）
- **主链路低延迟**（快速返回 + 异步落单）
- **异常可恢复**（延迟检查 + 补偿 + 对账修复）

---

## 1. 系统架构

### 1.1 服务划分

- `gateway`：统一 HTTP 入口，JWT 鉴权，路由转发，可配置限流
- `user-service`：注册 / 登录 / 刷新 token / 用户信息管理
- `product-service`：商品与秒杀商品管理
- `seckill-service`：秒杀核心（Redis Lua + 本地预扣减 + 本地配额 + MQ 投递）
- `order-service`：异步消费秒杀消息，幂等落库，超时补偿触发
- `tools/reconcile`：离线对账与修复工具（DB/Redis/库存流水一致性）
- `test/*`：功能测试、基准压测、网关半链路压测、MQ 可靠性测试

### 1.2 关键技术栈

- 网关：Gin
- 微服务：go-zero（zrpc + etcd 服务发现）
- 通信：gRPC + Protobuf
- 缓存与原子操作：Redis + Lua
- MQ：RabbitMQ
- DB：MySQL
- 可观测：Prometheus + Grafana + Jaeger(OTLP)

---

## 2. 核心链路（当前实现）

### 2.1 秒杀请求链路（快速返回）

1. 客户端调用 `POST /api/v1/seckill`
2. Gateway 做 JWT 校验后转发到 `seckill-service`
3. `seckill-service` 执行：
- 秒杀商品 ID 预过滤（Bloom + 回源兜底）
- 本地库存预扣减（减少 Redis 热点压力）
- Redis Lua 原子裁决（时间窗 + 一人一单 + 扣减 + pending 状态）
4. 成功后异步投递 RabbitMQ（主队列消息 + 延迟检查消息）
5. 接口立即返回“抢购成功，订单处理中”

### 2.2 异步落单链路

1. `order-service` 消费主队列消息
2. 基于 `order_id` 幂等写入（支持批量写入 + 单条回退）
3. 落库成功后回调 `seckill-service`，将 Redis 订单状态更新为 `success`

### 2.3 超时补偿链路

1. 延迟队列 TTL 到期后转入检查队列
2. `order-service` 检查订单是否落库
3. 若未落库，调用 `CompensateFailedOrder`：
- `pending -> failed` 原子状态迁移
- 回补 Redis 库存
- 释放用户占位 key

### 2.4 多实例配额协商（可选）

开启 `LocalQuota.Enabled=true` 后：
- 每个 `seckill-service` 实例按批次向 Redis 申请本地配额
- 通过租约 TTL + 心跳续约 + 过期回收(Reaper)保证配额可回收
- 对同一商品补仓使用单飞门控（`QuotaRefillGate`）避免并发补仓风暴

---

## 3. 默认端口与依赖

### 3.1 业务服务

| 服务 | 协议 | 默认地址 |
|---|---|---|
| gateway | HTTP | `0.0.0.0:8888` |
| user-service | gRPC | `127.0.0.1:9081` |
| product-service | gRPC | `127.0.0.1:9082` |
| seckill-service | gRPC | `127.0.0.1:9083` |
| order-service | gRPC | `127.0.0.1:9084` |

### 3.2 基础设施

| 组件 | 默认地址 |
|---|---|
| etcd | `127.0.0.1:2379` |
| MySQL | `127.0.0.1:3306` |
| Redis | `localhost:6379` |
| RabbitMQ AMQP | `localhost:5672` |
| RabbitMQ 管理台 | `localhost:15672` |

### 3.3 Prometheus 指标端口

| 组件 | 默认端口 |
|---|---|
| gateway | `9180` |
| user-service | `9181` |
| product-service | `9182` |
| seckill-service | `9183` |
| order-service | `9184` |

---

## 4. 启动方式

### 4.1 启动基础设施

```bash
docker compose -f deploy/docker-compose.yml up -d
```

### 4.2 初始化数据库

```bash
mysql -h 127.0.0.1 -u root -p < docs/schema.sql
```

### 4.3 启动微服务

```bash
go run gateway/gateway.go -f gateway/etc/gateway.yaml
go run user-service/user.go -f user-service/etc/user.yaml
go run product-service/product.go -f product-service/etc/product.yaml
go run seckill-service/seckill.go -f seckill-service/etc/seckill.yaml
go run order-service/order.go -f order-service/etc/order.yaml
```

Windows 一键脚本：

```powershell
powershell -ExecutionPolicy Bypass -File scripts/start-all.ps1
powershell -ExecutionPolicy Bypass -File scripts/stop-all.ps1
```

---

## 5. 多实例启动（当前支持）

### 5.1 gateway

```bash
go run gateway/gateway.go -f gateway/etc/gateway.yaml --port=18888 --metrics-port=19180
```

### 5.2 seckill-service

```bash
go run seckill-service/seckill.go -f seckill-service/etc/seckill.yaml --port=19083 --metrics-port=19183
```

### 5.3 order-service

```bash
go run order-service/order.go -f order-service/etc/order.yaml --port=19084 --metrics-port=19184
```

说明：`user-service`、`product-service` 当前仍使用 yaml 中端口。

---

## 6. 主要 HTTP API（Gateway）

### 6.1 无需登录

- `POST /api/v1/user/register`
- `POST /api/v1/user/login`
- `POST /api/v1/user/refresh`
- `GET /health`

### 6.2 需 JWT

用户：
- `GET /api/v1/user/info`
- `PUT /api/v1/user/info`
- `POST /api/v1/user/password`

商品：
- `GET /api/v1/product/:id`
- `GET /api/v1/products`
- `GET /api/v1/seckill/products`

秒杀：
- `POST /api/v1/seckill`
- `GET /api/v1/seckill/status`
- `GET /api/v1/seckill/result`

订单：
- `POST /api/v1/order`
- `GET /api/v1/order/:orderId`
- `GET /api/v1/orders`
- `POST /api/v1/order/:orderId/cancel`
- `POST /api/v1/order/pay`
- `POST /api/v1/order/:orderId/refund`

---

## 7. 配置要点

### 7.1 通用

各服务 `etc/*.yaml` 中常见配置：
- `Mode`：`dev/test/prod`
- `Dev.ResetLogsOnStart`：仅在 `dev/test` 下生效，启动时清空该服务日志目录
- `Log`：日志落盘路径、级别、压缩、保留天数
- `Telemetry`：OTLP 链路追踪上报
- `Prometheus`：指标暴露端口

### 7.2 gateway

- `RateLimit.Enabled`：是否启用秒杀限流
- `RateLimit.Strategy`：`token_bucket | sliding_window | ip_token_bucket`
- `RedisHost`：JWT 黑名单/限流 Redis 地址
- `UserService/ProductService/SeckillService/OrderService`：etcd 服务发现配置

### 7.3 seckill-service

- `SeckillRedis`：秒杀核心 Redis 连接池
- `ProductMetaCache`：秒杀商品元数据本地缓存刷新
- `Bloom`：商品 ID 预过滤器参数（当前实现为 Bloom）
- `RabbitMQ` + `AsyncProducer`：异步投递参数
- `LocalQuota`：多实例本地配额协商开关与参数

### 7.4 order-service

- `RabbitMQ`：主消费/检查消费
- `ProductService` / `SeckillService`：下游 RPC
- `Fallback`：etcd 不可用时的直连地址

---

## 8. 压测与测试

### 8.1 秒杀服务基准压测（gRPC直连）

目录：`test/seckill-benchmark-test`

```bash
cd test/seckill-benchmark-test
go run . --targets=127.0.0.1:9083,127.0.0.1:19083
```

### 8.2 网关半链路压测（Go open-loop）

目录：`test/gateway-benchmark-test`

```bash
cd test/gateway-benchmark-test
go run . --gateway-targets=http://127.0.0.1:8888,http://127.0.0.1:18888 --rate=20000 --duration=30s
```

仅压网关转发能力（不做秒杀数据准备）：

```bash
go run . --mode=gateway --gateway-targets=http://127.0.0.1:8888,http://127.0.0.1:18888 --rate=20000 --duration=30s
```

### 8.3 其他测试

- 功能测试：`test/seckill-functional-test`
- 端到端：`test/test-e2e`
- K6 压测：`test/k6-seckill-test`
- MQ 可靠性：`test/mq-reliability-test`
- MQ 拓扑：`test/mq-topology-test`
- 失败补偿：`test/failed-compensation-test`

---

## 9. 对账修复工具

目录：`tools/reconcile`

作用：扫描订单窗口，识别并修复典型不一致：
- DB 成功但 Redis 非 success
- DB 失败但 Redis 仍 pending/success
- 库存流水异常（缺回滚、异常回滚组合）

示例：

```bash
cd tools/reconcile
go run . \
  --order-config ../../order-service/etc/order.yaml \
  --seckill-config ../../seckill-service/etc/seckill.yaml \
  --dry-run=true
```

---

## 10. 当前一致性语义与边界

- 秒杀库存权威在 Redis（Lua 原子裁决）
- 订单持久化是异步最终一致（MQ + 补偿 + 对账）
- 消费侧采用“处理成功后 ACK”，避免消费成功前误确认
- 生产侧仍存在极短窗口：Redis 已扣减但消息未入队，依赖 TTL 补偿与对账兜底

---

## 11. 目录结构

```text
seckill-mall/
├── gateway/
├── user-service/
├── product-service/
├── seckill-service/
├── order-service/
├── common/
├── deploy/
├── docs/
├── scripts/
├── test/
└── tools/
```

---

## License

MIT
