# ============================================================
# Seckill-Mall 微服务项目
# 高并发秒杀系统 - Go + go-zero 微服务架构
# ============================================================

## 项目概览

本项目是一个高并发电商秒杀系统，采用 Go 语言 + go-zero 微服务框架开发。

### 技术栈

| 组件 | 技术选型 |
|------|----------|
| 网关层 | Gin HTTP 网关 |
| 微服务框架 | go-zero |
| 数据库 | MySQL 8.0 + GORM |
| 缓存 | Redis 7 (含 Lua 脚本) |
| 消息队列 | RabbitMQ 3.12 |
| 通信协议 | gRPC + Protobuf |

### 微服务划分

```
seckill-mall/
├── gateway/          # HTTP 网关 (路由/鉴权/限流)
├── user-service/    # 用户服务 (注册/登录/JWT)
├── product-service/ # 商品与库存服务 (CRUD/库存扣减)
├── seckill-service/ # 秒杀服务 (Redis Lua/RabbitMQ)
├── order-service/   # 订单服务 (RabbitMQ消费/订单处理)
├── common/          # 公共模块 (Proto/Utils/Constants)
├── deploy/          # 部署配置 (Docker Compose)
└── docs/            # 文档与SQL脚本
```

## 核心流程

### 秒杀链路（高性能）

```
1. Gateway (HTTP) 
   ↓
2. Seckill-Service (gRPC) 
   ↓
3. Redis Lua (原子性校验库存 + 防重 + 预扣减)
   ↓
4. RabbitMQ (投递秒杀成功消息)
   ↓
5. 返回 HTTP 200 "抢购排队中"
```

### 异步订单链路（一致性）

```
1. Order-Service (RabbitMQ Consumer)
   ↓
2. 幂等性校验 (基于 order_id)
   ↓
3. Product-Service (gRPC 扣减 DB 库存)
   ↓
4. MySQL (生成真实订单)
   ↓
5. 更新 Redis 订单状态
```

## 服务端口

| 服务 | 端口 |
|------|------|
| Gateway | 8888 |
| User-Service | 8081 |
| Product-Service | 8082 |
| Seckill-Service | 8083 |
| Order-Service | 8084 |
| MySQL | 3306 |
| Redis | 6379 |
| RabbitMQ | 5672 |
| RabbitMQ Management | 15672 |

## 快速开始

### 1. 启动基础设施

```powershell
cd deploy
docker-compose up -d
```

### 2. 初始化数据库

```powershell
mysql -h localhost -u root -proot123456 < docs/schema.sql
```

### 3. 编译服务

```powershell
cd user-service; go build -o user.exe .
cd product-service; go build -o product.exe .
cd seckill-service; go build -o seckill.exe .
cd order-service; go build -o order.exe .
cd gateway; go build -o gateway.exe .
```

### 4. 启动服务

```powershell
go run user-service/user.go -f user-service/user/etc/user.yaml
go run product-service/product.go -f product-service/product/etc/product.yaml
go run seckill-service/seckill.go -f seckill-service/seckill/etc/seckill.yaml
go run order-service/order.go -f order-service/order/etc/order.yaml
go run gateway/gateway.go -f gateway/etc/gateway.yaml
```

## 已完成功能

### Phase 1 - 基础工程体系 ✅
- [x] Proto 定义文件
- [x] 公共错误码与常量
- [x] 雪花算法 ID 生成器
- [x] SQL 初始化脚本
- [x] Docker Compose 部署文件

### Phase 2 - 基础 CRUD 服务 ✅
- [x] User-Service 实现 (注册/登录/用户管理)
- [x] Product-Service 实现 (商品CRUD/库存管理)

### Phase 3 - 网关层 ✅
- [x] Gin HTTP 网关
- [x] JWT 认证中间件
- [x] 限流中间件
- [x] 路由配置
- [ ] gRPC 客户端封装（待完善）

### Phase 4 - 秒杀核心 ✅
- [x] Redis Lua 脚本 (原子性扣减库存)
- [x] RabbitMQ 生产者 (投递秒杀消息)
- [x] 高并发防重机制

### Phase 5 - 异步订单 ✅
- [x] RabbitMQ Consumer (消费秒杀消息)
- [x] 幂等性校验 (基于 order_id)
- [x] 订单 CRUD

## API 接口

### 用户接口

| 方法 | 路径 | 说明 | 认证 |
|------|------|------|------|
| POST | /api/v1/user/register | 用户注册 | 否 |
| POST | /api/v1/user/login | 用户登录 | 否 |
| GET | /api/v1/user/info | 获取用户信息 | 是 |

### 商品接口

| 方法 | 路径 | 说明 | 认证 |
|------|------|------|------|
| GET | /api/v1/product/:id | 获取商品详情 | 是 |
| GET | /api/v1/products | 商品列表 | 是 |
| GET | /api/v1/seckill/products | 秒杀商品列表 | 是 |

### 秒杀接口

| 方法 | 路径 | 说明 | 认证 |
|------|------|------|------|
| POST | /api/v1/seckill | 秒杀下单 | 是 |
| GET | /api/v1/seckill/status | 查询秒杀状态 | 是 |
| GET | /api/v1/seckill/result | 查询秒杀结果 | 是 |

### 订单接口

| 方法 | 路径 | 说明 | 认证 |
|------|------|------|------|
| GET | /api/v1/order/:orderId | 获取订单详情 | 是 |
| GET | /api/v1/orders | 订单列表 | 是 |
| POST | /api/v1/order/:orderId/cancel | 取消订单 | 是 |
| POST | /api/v1/order/pay | 支付订单 | 是 |

## 项目结构

```
seckill-mall/
├── common/
│   ├── proto/           # .proto 定义文件
│   ├── constants/       # Redis Key、RabbitMQ 配置等常量
│   ├── errors/          # 全局错误码定义
│   └── utils/           # 工具函数 (雪花算法等)
│
├── gateway/             # HTTP 网关
│   ├── etc/            # 配置文件
│   ├── internal/
│   │   ├── config/      # 配置
│   │   ├── handler/     # HTTP 处理器
│   │   ├── middleware/  # 中间件 (JWT/限流)
│   │   └── client/      # gRPC 客户端
│   └── gateway.go       # 入口文件
│
├── user-service/        # 用户服务
│   ├── user/           # Protobuf 生成代码
│   ├── internal/
│   │   ├── config/      # 配置
│   │   ├── model/       # 数据模型层
│   │   ├── logic/      # 业务逻辑层
│   │   ├── server/     # gRPC 服务端
│   │   └── svc/        # 服务上下文
│   └── user.go          # 入口文件
│
├── product-service/    # 商品与库存服务
├── seckill-service/    # 秒杀服务 (核心)
├── order-service/      # 订单服务
│
├── deploy/
│   └── docker-compose.yml  # Docker 部署配置
│
└── docs/
    └── schema.sql      # 数据库初始化脚本
```

## License

MIT
