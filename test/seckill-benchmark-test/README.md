# Seckill 同步入口压测

这个工具用于评估新架构下 `seckill-service` 的纯同步入口吞吐能力，真实走：

`client -> seckill-service (gRPC Seckill RPC)`

压测结果只统计同步返回，不经过 `gateway`，也不等待 RabbitMQ 异步建单、超时检查或最终订单落库完成。

## 支持两种模式

### `legacy`

沿用旧版 benchmark 的思路：

- 固定总请求数
- 固定最大并发
- 前一个请求返回后，后续请求继续补位

适合：

- 和旧 benchmark 历史结果做横向对比
- 评估“某个并发度下跑完一批请求要多久”

### `burst`

更接近真实秒杀首波流量：

- 固定到达率
- 在短突发窗口内持续放请求
- `burst-window` 是真实发压窗口，`duration` 是其上限
- 服务端扛不住时会出现排队丢弃或超时/错误

适合：

- 评估真实抢购瞬时冲击
- 看系统过载行为和稳定吞吐上限

## 前置条件

- `seckill-service` 已启动
- `Redis` 已启动
- `MySQL` 已启动

默认地址：

- `seckill-service`: `127.0.0.1:9083`
- `Redis`: `localhost:6379`
- `MySQL`: `root:Zz123456@tcp(localhost:3306)/seckill_mall?...`

## 快速开始

默认 `legacy`：

```powershell
cd test\seckill-benchmark-test
go run .
```

显式使用 `legacy`：

```powershell
go run . --mode=legacy --total-requests=10000 --concurrency=1000
```

使用 `burst`：

```powershell
go run . --mode=burst --rate=20000 --duration=10s --burst-window=1s --ideal
```

多实例压测：

```powershell
go run . --targets="127.0.0.1:9083,127.0.0.1:19083" --pool-size=128 --mode=burst --ideal
```

## 常用参数

通用参数：

- `--targets`
- `--mode`
- `--product-id`
- `--stock`
- `--users`
- `--workers`
- `--request-timeout`
- `--quantity`
- `--redis`
- `--pool-size`
- `--ideal`

`legacy` 专用：

- `--total-requests`
- `--concurrency`

`burst` 专用：

- `--rate`
- `--duration`
- `--queue-size`
- `--burst-window`

## 场景准备

每次运行前会自动：

- 初始化 Redis 商品信息和库存
- 清理压测用户对应的 `userKey`
- 尝试清理 `seckill_orders`
- 尝试清理 `seckill_reservations`
- 尝试清理 `event_outbox`
- 尝试清理 `orders`
- 尝试清理 `processed_messages`

注意：

- 这个工具的目标是同步入口压测，不保证把异步链路完全清空
- 如果你要做严格的多轮对比，建议压测前额外执行一次 [`test/cleanup_benchmark_data.sql`](/abs/path/F:/sec1.1/test/cleanup_benchmark_data.sql)

## 结果理解

- `TPS (业务成功)` 代表同步返回 `SUCCESS` 的吞吐
- `TPS (秒杀阶段)` 代表首个成功到最后一个成功之间的成功吞吐，更接近真实成功窗口
- `SOLD_OUT`、`ALREADY_PURCHASED` 属于业务失败，不算系统错误
- `ERR_RPC_*`、超时、连接错误等算系统错误
