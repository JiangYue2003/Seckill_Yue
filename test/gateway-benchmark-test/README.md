# Gateway 半链路压测（Go 原生）

这个工具走真实前端入口 `POST /api/v1/seckill`，用于评估 `gateway -> seckill-service` 的同步半链路吞吐能力。

它的定位是“入口链路对比工具”，不是当前仓库的主性能基准。主基准请优先使用 `test/seckill-benchmark-test`。

## 特性

- 固定到达率（open-loop）压测模型
- 支持多 gateway 地址轮询分发
- 支持 `--mode=gateway` 测网关转发吞吐（默认走 `/api/v1/seckill/status`）
- 预生成 JWT token 池，避免压测端签名开销干扰
- 自动初始化/清理 Redis 秒杀数据
- 可选清理 `seckill_orders`（通过 `BENCHMARK_MYSQL_DSN`）

## 快速开始

```powershell
cd test\gateway-benchmark-test
go run .
```

## 常用参数

```powershell
go run . `
  --gateway-targets=http://127.0.0.1:8888 `
  --product-id=9101 `
  --stock=15000 `
  --users=100000 `
  --rate=20000 `
  --duration=30s `
  --workers=4096 `
  --queue-size=20000 `
  --request-timeout=3s
```

理想吞吐（库存充足、用户池充足）建议直接加：

```powershell
go run . --ideal --gateway-targets=http://127.0.0.1:8888 --rate=20000 --duration=30s
```

`--ideal` 会自动把 `stock/users` 至少提升到 `rate * duration + 1000`，避免过早 `SOLD_OUT` 或用户重复导致 TPS 被稀释。

仅测网关转发吞吐：

```powershell
go run . --mode=gateway --gateway-targets=http://127.0.0.1:8888 --rate=20000 --duration=30s
```

说明：

- `mode=gateway` 默认路径是 `/api/v1/seckill/status`
- 压测器会自动带 JWT，并自动拼接 `seckillProductId` 查询参数
- 若你想测试其它路径，可用 `--path` 覆盖
- 默认 `start-all.ps1` 只会起一个 `gateway`。如果你想传多个 `gateway-targets`，需要额外手工启动更多 gateway 实例

## MySQL 清理（可选）

若需要自动清理 `seckill_orders`，设置：

```powershell
$env:BENCHMARK_MYSQL_DSN='root:Zz123456@tcp(localhost:3306)/seckill_mall?charset=utf8mb4&parseTime=True&loc=Local'
go run .
```

## 结果口径

- `QPS`：完成请求数 / 总耗时
- `TPS (整体)`：业务成功数 / 总耗时
- `TPS (秒杀阶段)`：业务成功数 / 成功窗口时长（首个成功到最后一个成功）
- 业务成功判定：`data.success == true` 或 `data.code == SUCCESS`
- 业务失败分类：`SOLD_OUT`、`ALREADY_PURCHASED` 等
- 系统失败分类：HTTP 错误、请求超时、JSON 解析失败等

## 何时使用

- 想测秒杀核心吞吐：用 `test/seckill-benchmark-test`
- 想测 JWT、HTTP 编解码、网关转发成本：用本工具
