# 秒杀系统端到端测试（E2E）

通过 HTTP API 验证秒杀系统全链路业务流程，并验证 OpenTelemetry 分布式追踪是否正确关联各服务 span。

## 测试流程

```
[测试程序] → [Gateway] → [User-Service]      注册/登录
                      → [Product-Service]    查询秒杀商品
                      → [Seckill-Service]    秒杀下单
                           ↓ (RabbitMQ)
                      → [Order-Service]      异步创建订单
                      → [Product-Service]    扣减物理库存
```

测试程序在执行秒杀前会自动初始化测试数据（MySQL + Redis），无需手动准备。

## 文件说明

| 文件 | 说明 |
|------|------|
| `seckill_flow.go` | 端到端测试程序源码 |
| `seckill_flow.exe` | 已编译的可执行文件（Windows） |
| `go.mod` | 依赖管理 |
| `go.sum` | 依赖锁文件 |

## 前置条件

运行前请确保**所有微服务**已启动：

| 服务 | 地址/端口 | 说明 |
|------|----------|------|
| Gateway | `localhost:8888` | API 网关 |
| User-Service | `127.0.0.1:8081` | 用户服务 |
| Product-Service | `127.0.0.1:8082` | 商品服务 |
| Seckill-Service | `127.0.0.1:8083` | 秒杀服务 |
| Order-Service | `127.0.0.1:8084` | 订单服务 |
| Redis | `localhost:6379` | 缓存/库存 |
| MySQL | `localhost:3306` | 持久化存储 |
| RabbitMQ | `localhost:5672` | 消息队列 |
| etcd | `localhost:2379` | 服务发现 |

## 快速开始

### 方式一：直接运行编译好的 exe（推荐）

```powershell
cd test-e2e
.\seckill_flow.exe
```

### 方式二：源码运行

```powershell
cd test-e2e
go mod tidy
go run seckill_flow.go
```

## 测试流程说明

| 步骤 | 接口 | 验证内容 |
|------|------|----------|
| 0. 初始化数据 | MySQL + Redis | 自动创建商品/秒杀商品/缓存 |
| 1. 用户注册 | `POST /api/v1/user/register` | 注册成功，获取 userId |
| 2. 用户登录 | `POST /api/v1/user/login` | 登录成功，获取 JWT token |
| 3. 查询秒杀商品 | `GET /api/v1/seckill/products` | 获取进行中的秒杀商品列表 |
| 4. 执行秒杀 | `POST /api/v1/seckill` | 发起秒杀，获取订单号 |

## 预期输出示例

```
========================================
  秒杀系统端到端测试 - 全链路验证
========================================

✅ 测试商品已就绪 (productId=999001)
✅ 秒杀商品已就绪 (seckillProductId=999001)
✅ Redis 缓存已同步 (活动有效期至 2026-04-03 21:06:31)

[流程 1/4] 用户注册: testuser_1775214391677
✅ 注册成功: userId=43

[流程 2/4] 用户登录
✅ 登录成功: userId=43, token=eyJhbGciOiJIUzI1NiIs......

[流程 3/4] 获取秒杀商品列表
✅ 获取到 1 个秒杀商品
   [1] 测试秒杀商品-iPhone16 (id=999001, 秒杀价=4999.00, 库存=50)

[流程 4/4] 执行秒杀: 测试秒杀商品-iPhone16 (seckillProductId=999001)
✅ 秒杀完成: success=true, code=SUCCESS, message=抢购成功，订单正在处理中, orderId=S298412951092592640

========================================
  全链路测试完成！
========================================
```

## 初始化数据说明

测试程序使用固定 ID，便于幂等执行：

| 类型 | ID | 说明 |
|------|-----|------|
| 测试商品 | `999001` | iPhone 16 原价 ¥6999.00 |
| 秒杀商品 | `999001` | 秒杀价 ¥4999.00，库存 50 |
| 活动有效期 | 当前 - 1 分钟 ~ 当前 + 2 小时 | 确保秒杀在进行中 |

数据写入 MySQL 和 Redis 两处：

- **MySQL** (`products` / `seckill_products`)：Product-Service 查询秒杀商品列表时从 MySQL 读取
- **Redis** (`seckill:stock:{id}` 等)：Seckill-Service 执行秒杀时从 Redis 读取库存

## 验证 OpenTelemetry 链路追踪

测试完成后，检查各微服务的控制台日志，应能看到相同 `traceId` 的 span 串联起全链路：

```
seckill-service: trace=936172950645dc2a8c3bdb59f5ffbb44
  → /seckill.SeckillService/Seckill (1.6ms)

order-service: trace=6df96b34b72b34c2552e17dcf1ee4bd9
  → 消费 MQ 消息，创建订单

product-service: trace=560048d1400fbd68df3742005309f8d0
  → /product.ProductService/DeductStock (9.0ms)
```

> 注意：秒杀下单（同步）和 MQ 消费（异步）属于两个独立 trace，因为异步回调通过 Redis key 而非 gRPC metadata 传递 context。
