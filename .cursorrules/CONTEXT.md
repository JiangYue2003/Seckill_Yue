# Cursor Context: Seckill-Mall (高并发秒杀系统)

> **AI 角色与任务约束**：
> 你现在是本项目的首席 Go 后端研发架构师。接下来的所有代码生成、目录创建、逻辑实现，都必须严格遵循本文件的架构设计与约束原则。你需要协助我一步一步、分模块地将这个系统落地。
>
> > **操作系统与终端约束 (OS & Terminal Constraints)**：
> 本项目在 **Windows 环境**下进行开发与运行。你在后续对话中提供的所有命令行指令（例如 go-zero 的 `goctl` 生成命令、Docker 启动命令、文件操作等），**必须全部基于 PowerShell 语法**。
> - 严禁使用仅限 Linux/macOS 的 Bash 专属命令（如 `rm -rf`, `ls`, `grep`, `export` 等）。
> - 涉及到环境变量设置，请使用 `$env:VAR_NAME="value"`。
> - 涉及到路径拼接，请注意 Windows 的路径斜杠规范或使用跨平台写法。
>
> **请你参考zero-skills执行与go-zero相关的指令、实现相关文件的生成**
> **请你参考.cursorrules文件夹下的.md文件作为go-zero项目生成范式的参考**

## 一、 项目全局概览 (Overview)
- **项目定位**：支撑瞬间高并发、防止超卖、保证分布式最终一致性的电商秒杀后端系统。
- **技术栈声明**（所有代码生成均需基于此栈）：
  - **核心语言**：Go (>= 1.20)
  - **网关层**：Gin
  - **微服务框架**：go-zero (用于内部 RPC 治理与基础代码脚手架生成)
  - **ORM / 数据库**：GORM + MySQL
  - **缓存与高并发核心**：Redis (需使用 go-redis，配合 Lua 脚本实现原子性)
  - **消息队列**：Kafka (用于异步削峰与服务解耦)
  - **通信协议**：gRPC + Protobuf

## 二、 目录结构规范 (Monorepo Structure)
在生成文件时，必须严格放置在对应的目录下，不要自行创造新的根目录：

    seckill-mall/
    ├── gateway/                # Gin HTTP 网关 (路由/参数校验/鉴权/限流/gRPC Client)
    ├── user-service/           # 用户 RPC 服务 (go-zero)
    ├── product-service/        # 商品与库存 RPC 服务 (go-zero)
    ├── seckill-service/        # 秒杀 RPC 服务 (go-zero) - 核心并发节点
    ├── order-service/          # 订单 RPC 服务 (go-zero)
    ├── common/                 # 全局公共模块
    │   ├── proto/              # 所有 .proto 定义集合
    │   ├── middleware/         # 跨服务共用中间件
    │   ├── utils/              # 唯一 ID 生成器(雪花算法)、时间工具等
    │   └── constants/          # Redis Key、Kafka Topic 等全局常量
    ├── deploy/                 # 容器化部署脚本 (docker-compose)
    └── docs/                   # SQL 初始化脚本、架构图等

## 三、 微服务职责边界 (Service Boundaries)
编写各服务代码时，**严禁越权调用或跨越边界**：

1. **Gateway**：只做 HTTP 接收、JWT 校验、限流（Rate Limit）和参数基础校验，业务逻辑必须全部转发给下游对应的 RPC 服务。
2. **User-Service**：只负责用户注册、登录（JWT 签发）、信息查询。密码入库必须用 bcrypt 哈希。
3. **Product-Service**：商品的基础 CRUD。**重要**：负责接收订单服务的 gRPC 调用，执行 MySQL 物理库存的最终扣减（需带乐观锁或版本号）。
4. **Seckill-Service (绝对核心)**：
   - 只能操作 Redis 和 Kafka，**绝对禁止直接连接或查询 MySQL**。
   - 接收秒杀请求后，通过 Redis Lua 脚本原子性完成“校验库存+用户防重+预扣减库存”。
   - 成功后，将抢购成功消息（`user_id`, `product_id`, `order_id`）Push 到 Kafka，立即返回响应。
5. **Order-Service**：
   - 扮演 Kafka 消费者（Consumer）。
   - 负责消费秒杀成功消息，基于 `order_id` 做幂等性校验。
   - 发起跨服务 gRPC 调用给 `Product-Service` 扣物理库存。
   - 最终将订单持久化到 MySQL。

## 四、 核心工作流 (Core Workflows)
AI 在生成核心业务链路代码时，需严格遵循此时序：

* **主干秒杀链路**：
  `Gateway (HTTP)` -> `Seckill-Service (gRPC)` -> `Redis (Lua预扣减)` -> `Kafka (生产消息)` -> `Gateway (返回 HTTP 200, 抢购排队中)`
* **异步兜底与一致性链路**：
  `Order-Service (Kafka消费)` -> `校验数据库幂等` -> `Product-Service (gRPC扣减DB库存)` -> `Order-Service (MySQL生成真实订单)`

## 五、 AI 编码强制约束 (Strict Coding Rules)
1. **防幻觉设定**：`Seckill-Service` 处理秒杀请求的过程中，**严禁**同步调用 `Order-Service` 或 `Product-Service`，必须使用 Kafka 解耦。
2. **错误处理**：所有数据库查询、Redis 操作、Kafka 投递、gRPC 调用必须进行 `err != nil` 检查，并使用 `zap` 或 `go-zero` 自带日志打印错误上下文。
3. **代码完整性**：要求给出可直接编译运行的代码，包含必要的 struct 定义、常量定义和 import 包。如果是部分更新，请明确指出修改位置。
4. **注释规范**：核心业务逻辑（尤其是 Lua 脚本执行、Kafka 消费幂等、gRPC 连接池）必须加上详尽的中文注释，说明设计意图。

## 六、 渐进式开发路径 (Action Plan)
在实际开发中，我们将按照以下步骤逐一击破，AI 需等待我的指令再进入下一步：
* **Phase 1**: 初始化基础工程体系 (编写统一定义的 `.proto` 文件，生成 `common` 目录下的常量与错误码)。
* **Phase 2**: 搭建基础 CRUD 服务 (`User-Service` 与 `Product-Service` 的 Model/Dao/Logic 层，配合 GORM)。
* **Phase 3**: 搭建统一入口 (`Gateway` 中间件、鉴权、gRPC 客户端封装)。
* **Phase 4**: 攻克高并发核心 (`Seckill-Service` 的 Redis Lua 脚本编写与 Kafka 投递逻辑)。
* **Phase 5**: 实现异步闭环 (`Order-Service` 的 Kafka 消费、幂等校验、分布式事务一致性处理)。