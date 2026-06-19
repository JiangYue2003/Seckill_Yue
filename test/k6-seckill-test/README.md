# k6 秒杀压测指南

这个目录保留的是 HTTP 入口压测方案，主要用于评估 `gateway` 入口的真实 HTTP/JWT 成本。
如果你的目标是测当前秒杀核心服务本身的吞吐，请优先使用 `test/seckill-benchmark-test`。

## 安装 k6

### Windows

```bash
choco install k6
```

### Linux

```bash
sudo apt-get install k6
```

### Mac

```bash
brew install k6
```

## 运行压测

### 1. 基础测试

```bash
cd /f/sec1.1/test/k6-seckill-test
k6 run --vus 10 --duration 5s seckill_test.js
```

### 2. 自定义参数

```bash
k6 run -e GATEWAY_URL=http://localhost:8888 seckill_test.js
```

```bash
# 指定多个 gateway 地址（逗号分隔，脚本会轮询分发；需要你手工起多个 gateway）
k6 run -e GATEWAY_URLS=http://127.0.0.1:8888,http://127.0.0.1:18888 seckill_test.js
```

## 路由说明

- 秒杀接口：`POST /api/v1/seckill`
- 需要 JWT 认证：`Authorization: Bearer <token>`

## 多 gateway 说明

- 若未设置 `GATEWAY_URLS`，脚本使用 `GATEWAY_URL`
- 若设置了 `GATEWAY_URLS`，脚本会在多个地址之间做轮询分发

## JWT Token

脚本会自动生成真实 JWT token：

- 密钥：`seckill-mall-jwt-secret-key-2026`
- 算法：HMAC-SHA256

## 对比 benchmark

| 指标 | 当前 benchmark | k6 压测 |
|------|---------------|---------|
| 主用途 | 秒杀核心吞吐 | Gateway 入口压测 |
| 协议 | gRPC | HTTP（通过 gateway） |
| 认证 | 无 | JWT token |
| 分布式 | 单机 | 支持多机 |
| 适合问题 | 服务核心瓶颈 | 入口链路和网关瓶颈 |
