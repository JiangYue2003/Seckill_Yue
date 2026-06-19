# 秒杀系统功能测试

通过 gRPC 直连 `seckill-service`，验证秒杀核心业务逻辑的正确性，包括库存控制、用户防重、订单查询等场景。

注意：这个工具当前更适合做基础冒烟。它的测试夹具仍直接写兼容 Redis key，不是当前“物理库存分片 + 控制面/分片面 key”链路的标准装载方式。

## 文件说明

| 文件 | 说明 |
|------|------|
| `main.go` | 程序入口：初始化 Redis 和 gRPC 客户端，运行所有测试用例，最后清理数据 |
| `test_cases.go` | 5 个功能测试用例的具体实现 |
| `go.mod` | 依赖管理（`seckill-service/seckill` 通过 `replace` 指向本地路径） |
| `go.sum` | 依赖锁文件 |

## 前置条件

| 服务 | 地址 | 说明 |
|------|------|------|
| Redis | `localhost:6379` | 存储秒杀库存和用户购买记录 |
| Seckill-Service | `127.0.0.1:9083` | 秒杀核心服务（gRPC） |
| Order-Service | `127.0.0.1:9084` | 订单服务（TC-04 状态查询需要） |

> 不需要启动 Gateway、User-Service、Product-Service，因为本测试直接连接 seckill-service。

## 快速开始

```powershell
cd test\seckill-functional-test
go run .
```

## 测试用例

| 用例编号 | 名称 | 测试内容 | 预期结果 |
|---------|------|---------|---------|
| TC-01 | 秒杀成功 | 单用户发起秒杀请求 | 返回 SUCCESS，获取订单号 |
| TC-02 | 库存不足 | 库存=1，用户A抢到，用户B失败 | 用户A成功，用户B返回 SOLD_OUT |
| TC-03 | 用户防重 | 同一用户对同一商品发起两次秒杀 | 第一次成功，第二次返回 ALREADY_PURCHASED |
| TC-04 | 订单状态查询 | 秒杀成功后轮询查询订单状态 | 订单状态从 pending → success |
| TC-05 | 查询不存在的订单 | 用无效订单号查询 | 返回“订单不存在” |

## 测试数据说明

每个测试用例使用独立的秒杀商品 ID，测试完成后自动清理：

| 用例 | 秒杀商品 ID | 用户 ID | 库存 | 说明 |
|------|-----------|---------|------|------|
| TC-01 | `1001` | `10001` | 10 | 单用户秒杀 |
| TC-02 | `1002` | `10021`, `10022` | 1 | 双用户抢单 |
| TC-03 | `1003` | `10031` | 10 | 用户重复购买 |
| TC-04 | `1004` | `10041` | 10 | 订单状态轮询 |
| TC-05 | - | - | - | 无需预置数据 |

当前测试程序直接写入的兼容 Redis key：

```text
seckill:info:{id}
seckill:product_name:{id}
seckill:stock:{id}
seckill:user:{id}:{uid}
```

这组 key 只反映测试程序的夹具写法，不代表当前生产热路径的完整 Redis 结构。当前真实热路径已使用分片总库存、分片状态、分片库存和 reserve 标记。

## TC-04 注意事项

TC-04 依赖 Order-Service 处理 RabbitMQ 消息并更新订单状态。如果 Order-Service 未启动，测试会等待 5 秒后仍显示 `pending` 状态，但该用例会标记为 `PASS`，因为查询功能本身正常，只是后端异步处理未完成。

## 使用建议

- 需要测当前新架构吞吐和延迟时，优先使用 `test/seckill-benchmark-test`
- 需要验证 HTTP 入口开销时，使用 `test/gateway-benchmark-test` 或 `test/k6-seckill-test`
- 这个功能测试更适合保留做快速冒烟和历史行为回归

## 依赖说明

本模块直接引用 `seckill-service/seckill` proto 包，通过 `go.mod` 中的 `replace` 指令指向本地路径：

```go
replace seckill-mall/seckill-service/seckill => ../../seckill-service
```

确保 `seckill-service` 目录下的 proto 文件已生成：

```powershell
cd seckill-service
go generate ./...
```
