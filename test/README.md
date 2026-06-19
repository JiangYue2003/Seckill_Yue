# 测试目录说明

本目录下的工具已经分成两类：

- 当前主验证路径：围绕 `seckill-service` 直连 `gRPC` 的 benchmark / 回归
- 兼容或对比工具：保留 `gateway`、老式 Redis 夹具、HTTP 冒烟脚本

不要默认所有测试工具都已经和“物理库存分片 + RabbitMQ 单轨 + 直接 gRPC 基准”完全同步。

## 1. 推荐使用顺序

### 1.1 首选：秒杀核心吞吐

目录：`test/seckill-benchmark-test`

这是当前最应该使用的性能测试工具，直接打：

`client -> seckill-service (gRPC)`

如果你通过 `scripts/start-all.ps1` 以默认副本启动，推荐：

```powershell
cd test\seckill-benchmark-test
go run . --targets="127.0.0.1:9083,127.0.0.1:19083" --pool-size=128 --mode=burst --ideal
```

### 1.2 其次：评估 gateway 开销

目录：

- `test/gateway-benchmark-test`
- `test/k6-seckill-test`

这些工具主要回答：

- JWT 鉴权带来多少开销
- `gateway -> seckill-service` 半链路吞吐是多少

它们是对比链路，不是当前主基准口径。

### 1.3 兼容/历史工具

目录：

- `test/seckill-functional-test`
- `test/seckill-data-tools`
- `test/test-e2e`

这些工具仍有价值，但其中一部分夹具仍直接写旧兼容 Redis key，更适合冒烟或历史回归，不适合作为“当前分片库存架构”的唯一验证依据。

## 2. 前置条件

运行大多数测试前，请确保以下依赖已启动：

| 组件 | 默认地址 |
|---|---|
| `Redis` | `127.0.0.1:6379` |
| `RabbitMQ` | `127.0.0.1:5672` |
| `MySQL` | `127.0.0.1:3306` |
| `etcd` | `127.0.0.1:2379` |
| `seckill-service` | `127.0.0.1:9083` |
| `order-service` | `127.0.0.1:9084` |
| `product-service` | `127.0.0.1:9082` |
| `gateway` | `127.0.0.1:8888` |

如果用默认 `start-all.ps1`，还会额外有：

- `seckill-service-2`: `127.0.0.1:19083`
- `order-service-2`: `127.0.0.1:19084`

## 3. 快速入口

### 3.1 秒杀核心 benchmark

```powershell
cd test\seckill-benchmark-test
go run .
```

### 3.2 Gateway 半链路 benchmark

```powershell
cd test\gateway-benchmark-test
go run . --gateway-targets=http://127.0.0.1:8888
```

### 3.3 功能测试

```powershell
cd test\seckill-functional-test
go run .
```

## 4. 现状提醒

- `seckill-benchmark-test` 是当前性能测试主路径
- `gateway-benchmark-test` 和 `k6` 更适合做入口链路对比
- `seckill-functional-test`、`seckill-data-tools`、`test-e2e` 仍包含旧兼容 Redis 夹具思路，文档和结果都要结合代码理解
