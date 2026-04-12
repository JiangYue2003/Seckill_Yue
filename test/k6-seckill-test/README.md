# k6 秒杀压测指南

## 安装 k6

### Windows
```bash
choco install k6
# 或者下载：https://github.com/grafana/k6/releases
```

### Linux
```bash
sudo gpg -k
sudo gpg --no-default-keyring --keyring /usr/share/keyrings/k6-archive-keyring.gpg --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D69
echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" | sudo tee /etc/apt/sources.list.d/k6.list
sudo apt-get update
sudo apt-get install k6
```

### Mac
```bash
brew install k6
```

## 运行压测

### 1. 基础测试（10 并发，5秒）
```bash
cd /f/sec1.1/test/k6-seckill-test
k6 run --vus 10 --duration 5s seckill_test.js
```

### 2. 完整压测（默认配置）
```bash
k6 run seckill_test.js
```

### 3. 自定义参数
```bash
# 指定 gateway 地址
k6 run -e GATEWAY_URL=http://localhost:8888 seckill_test.js

# 指定秒杀商品 ID
k6 run -e SECKILL_PRODUCT_ID=1 seckill_test.js

# 自定义并发和持续时间
k6 run --vus 5000 --duration 30s seckill_test.js
```

### 4. 分布式压测（多机发压）
```bash
# 机器 1
k6 run --out json=results1.json seckill_test.js

# 机器 2
k6 run --out json=results2.json seckill_test.js

# 合并结果
k6 inspect results1.json results2.json
```

### 5. 实时监控（Grafana）
```bash
# 启动 k6 with InfluxDB 输出
k6 run --out influxdb=http://localhost:8086/k6 seckill_test.js

# 在 Grafana 中查看实时指标
```

## 压测场景

### 场景 1：瞬时高并发（秒杀开始瞬间）
- 10000 req/s，持续 10 秒
- 模拟秒杀开始时的流量洪峰

### 场景 2：持续压力（秒杀进行中）
- 5000 req/s，持续 30 秒
- 模拟秒杀进行中的稳定流量

## 路由说明

- **秒杀接口**: `POST /api/v1/seckill`
- **需要 JWT 认证**: `Authorization: Bearer <token>`
- **请求体**:
  ```json
  {
    "seckillProductId": 1,
    "quantity": 1
  }
  ```

## JWT Token 生成

脚本会自动生成真实的 JWT token：
- 密钥: `seckill-mall-jwt-secret-key-2026`（从 gateway.yaml 获取）
- 算法: HMAC-SHA256
- 过期时间: 15 分钟
- Payload: `{ userId, exp, iat, jti }`

## 预期结果

```
========================================
   秒杀压测报告 (k6)
========================================

总请求数: 250000
成功数: 15000
失败数: 235000

延迟统计 (ms):
  平均: 120.50
  P50: 95.00
  P95: 350.00
  P99: 500.00

TPS: 6250.00
========================================
```

## 注意事项

1. **预先准备数据**：确保 Redis 中有足够的库存
2. **清理测试数据**：每次测试后清理 MySQL 中的订单数据
3. **监控系统资源**：观察 CPU、内存、网络、磁盘 IO
4. **逐步加压**：从小并发开始，逐步增加到目标并发
5. **多次测试**：至少运行 3 次取平均值

## 对比 benchmark

| 指标 | 当前 benchmark | k6 压测 |
|------|---------------|---------|
| 连接数 | 8 个 gRPC 连接 | 5000 个 HTTP 连接 |
| 协议 | gRPC | HTTP (通过 gateway) |
| 认证 | 无 | JWT token |
| 分布式 | 单机 | 支持多机 |
| 真实性 | 60% | 95% |
