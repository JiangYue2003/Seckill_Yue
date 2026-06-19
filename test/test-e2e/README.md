# 秒杀系统端到端测试（E2E）

通过 HTTP API 验证秒杀系统全链路业务流程，并验证 OpenTelemetry 分布式追踪是否正确关联各服务 span。

注意：这个工具当前更适合做“HTTP 业务冒烟”。它的测试数据准备代码仍直接写兼容 Redis key，不是当前库存分片架构的主验证路径。

## 测试流程

```text
[测试程序] -> [Gateway] -> [User-Service]      注册/登录
                     -> [Product-Service]    查询秒杀商品
                     -> [Seckill-Service]    秒杀下单
                          -> (RabbitMQ)
                     -> [Order-Service]      异步创建订单
```

测试程序在执行秒杀前会自动初始化测试数据（MySQL + Redis），无需手动准备。

## 前置条件

运行前请确保所有微服务已启动：

| 服务 | 地址/端口 | 说明 |
|------|----------|------|
| Gateway | `localhost:8888` | API 网关 |
| User-Service | `127.0.0.1:9081` | 用户服务 |
| Product-Service | `127.0.0.1:9082` | 商品服务 |
| Seckill-Service | `127.0.0.1:9083` | 秒杀服务 |
| Order-Service | `127.0.0.1:9084` | 订单服务 |
| Redis | `localhost:6379` | 缓存/库存 |
| MySQL | `localhost:3306` | 持久化存储 |
| RabbitMQ | `localhost:5672` | 消息队列 |
| etcd | `localhost:2379` | 服务发现 |

## 快速开始

```powershell
cd test\test-e2e
go run seckill_flow.go
```

## 数据准备说明

数据写入 MySQL 和 Redis 两处：

- MySQL：`products` / `seckill_products`
- Redis：测试程序当前仍手工写 `seckill:stock:{id}`、`seckill:info:{id}` 等兼容 key

因此：

- 它适合做 HTTP 全链路冒烟
- 不适合直接代表当前“物理库存分片 + 分片补偿”架构的完整验证

如果你的目标是验证当前秒杀核心吞吐和新分片链路，优先使用：

- `test/seckill-benchmark-test`
- 必要时再结合 `test/gateway-benchmark-test`
