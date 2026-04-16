# Seckill-Mall（Gin + go-zero）

一个前后端分离的微服务秒杀系统，核心目标是**高并发下的正确性与可用性**：
- 秒杀请求走 Redis Lua 原子裁决（防重 + 扣减 + 状态写入）
- 订单创建走 RabbitMQ 异步落库
- 超时订单走延迟队列 + 补偿回滚，避免库存悬挂

---

## 1. 技术栈

- **网关层**：Gin（HTTP）
- **微服务框架**：go-zero（zrpc + etcd 服务发现）
- **通信协议**：gRPC + Protobuf
- **缓存/原子操作**：Redis 7 + Lua
- **消息队列**：RabbitMQ 3.12
- **数据库**：MySQL 8.0
- **观测性**：Prometheus + Grafana + OTLP（Jaeger）

---

## 2. 微服务划分

- `gateway`：统一 HTTP 入口、JWT 鉴权、路由转发
- `user-service`：用户注册/登录/JWT 刷新/资料管理
- `product-service`：商品与秒杀商品管理、库存/活动元数据维护
- `seckill-service`：秒杀核心服务（Redis Lua + 异步投递）
- `order-service`：消费秒杀消息、幂等落单、超时补偿
- `tools/reconcile`：对账工具
- `test/*`：功能、E2E、压测、MQ 拓扑/可靠性测试

---

## 3. 端口与依赖

### 3.1 业务服务

| 服务 | 协议 | 默认地址 |
|---|---|---|
| gateway | HTTP | `0.0.0.0:8888` |
| user-service | gRPC | `127.0.0.1:9081` |
| product-service | gRPC | `127.0.0.1:9082` |
| seckill-service | gRPC | `127.0.0.1:9083` |
| order-service | gRPC | `127.0.0.1:9084` |

### 3.2 基础设施

| 组件 | 默认端口 |
|---|---|
| etcd | `2379` |
| MySQL | `3306` |
| Redis | `6379` |
| RabbitMQ AMQP | `5672` |
| RabbitMQ 管理台 | `15672` |

### 3.3 观测端口

| 组件 | 默认端口 |
|---|---|
| gateway metrics | `9180` |
| user-service metrics | `9181` |
| product-service metrics | `9182` |
| seckill-service metrics | `9183` |
| order-service metrics | `9184` |
| Prometheus | `9090` |
| Grafana | `3000` |
| Jaeger UI | `16686` |

---

## 4. 秒杀核心链路

### 4.1 同步抢购链路（快速返回）

1. 客户端请求 `POST /api/v1/seckill`
2. Gateway 鉴权后调用 `seckill-service`
3. `seckill-service` 先做本地库存快速预过滤，再执行 Redis Lua 原子脚本：
   - 活动时间校验
   - 用户防重校验
   - 库存校验与扣减
   - 写入用户抢购标记和订单状态（pending）
4. 成功后异步投递 MQ（正常订单消息 + 延迟检查消息）
5. 接口快速返回（避免同步阻塞 DB）

### 4.2 异步订单链路（最终一致）

1. `order-service` 消费秒杀消息
2. 基于 `order_id` 做幂等校验
3. 批量落单（失败回退单条插入）
4. 落库成功后回调 `seckill-service`：`pending -> success`

### 4.3 超时补偿链路

1. 延迟队列（TTL）到期后转入检查队列
2. `order-service` 检查订单是否已落库
3. 若未落库，调用 `CompensateFailedOrder`：
   - 原子 `pending -> failed`
   - 回补 Redis 秒杀库存
   - 释放用户防重 key

---

## 5. 快速启动

> 建议在项目根目录执行命令。

### 5.1 启动基础设施

```bash
docker compose -f deploy/docker-compose.yml up -d
```

### 5.2 初始化数据库

```bash
mysql -h 127.0.0.1 -u root -p < docs/schema.sql
```

### 5.3 启动微服务

```bash
go run gateway/gateway.go -f gateway/etc/gateway.yaml
go run user-service/user.go -f user-service/etc/user.yaml
go run product-service/product.go -f product-service/etc/product.yaml
go run seckill-service/seckill.go -f seckill-service/etc/seckill.yaml
go run order-service/order.go -f order-service/etc/order.yaml
```

Windows 可使用：

```powershell
powershell -ExecutionPolicy Bypass -File scripts/start-all.ps1
```

停止：

```powershell
powershell -ExecutionPolicy Bypass -File scripts/stop-all.ps1
```

### 5.4 （可选）启动观测组件

```bash
docker compose -f deploy/observability-compose.yml up -d
```

---

## 6. 主要 HTTP API（Gateway）

### 用户
- `POST /api/v1/user/register`
- `POST /api/v1/user/login`
- `POST /api/v1/user/refresh`
- `GET /api/v1/user/info`
- `PUT /api/v1/user/info`
- `POST /api/v1/user/password`

### 商品
- `GET /api/v1/product/:id`
- `GET /api/v1/products`
- `GET /api/v1/seckill/products`

### 秒杀
- `POST /api/v1/seckill`
- `GET /api/v1/seckill/status`
- `GET /api/v1/seckill/result`

### 订单
- `POST /api/v1/order`
- `GET /api/v1/order/:orderId`
- `GET /api/v1/orders`
- `POST /api/v1/order/:orderId/cancel`
- `POST /api/v1/order/pay`
- `POST /api/v1/order/:orderId/refund`

---

## 7. 测试与压测

- 功能测试：`test/seckill-functional-test`
- E2E：`test/test-e2e`
- K6 压测：`test/k6-seckill-test`
- 基准压测：`test/seckill-benchmark-test`
- 失败补偿测试：`test/failed-compensation-test`
- MQ 拓扑/可靠性测试：`test/mq-topology-test`、`test/mq-reliability-test`

常用脚本：
- `scripts/init-test-data.sh`
- `scripts/cleanup-test-data.sh`

---

## 8. 已知注意事项（基于当前代码）

1. **秒杀限流中间件当前在网关中被注释关闭**（用于压测场景），上线前建议恢复。  
2. **配置文件包含本地开发用明文凭据与 JWT Secret**，生产环境必须改为安全配置（环境变量/密钥管理）。  
3. `product-service` 更新 Redis 秒杀信息与 `seckill-service` 读取格式存在潜在不一致风险（可能影响时间窗字段）。

---

## 9. 项目目录

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
