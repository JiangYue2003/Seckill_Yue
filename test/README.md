# 秒杀系统测试

本目录包含秒杀系统的功能测试和性能测试代码。

## 目录结构

```
test/
├── seckill-functional-test/   # 功能测试
│   ├── main.go               # 入口文件
│   ├── test_cases.go         # 测试用例
│   └── go.mod                # 依赖管理
│
└── seckill-benchmark-test/   # 性能测试
    ├── main.go               # 入口文件
    ├── metrics.go            # 指标收集
    └── go.mod                # 依赖管理

scripts/
├── init-test-data.sh         # 初始化测试数据
└── cleanup-test-data.sh       # 清理测试数据
```

## 前置条件

运行测试前请确保以下服务已启动：

1. **Redis** - `localhost:6379`
2. **RabbitMQ** - `localhost:5672`
3. **MySQL** - `localhost:3306`
4. **etcd** - `localhost:2379`
5. **秒杀微服务**:
   - seckill-service (端口 8083)
   - order-service (端口 8084)
   - product-service (端口 8082)

## 快速开始

### 1. 初始化测试数据

```bash
# 使用默认 Redis 配置
bash scripts/init-test-data.sh

# 或指定 Redis 地址
REDIS_HOST=localhost:6379 bash scripts/init-test-data.sh
```

### 2. 运行功能测试

```bash
cd test/seckill-functional-test
go mod tidy
go run .
```

### 3. 运行性能测试

```bash
cd test/seckill-benchmark-test
go mod tidy
go run .
```

### 4. 清理测试数据

```bash
bash scripts/cleanup-test-data.sh
```

## 功能测试用例

| 用例编号 | 用例名称 | 测试内容 |
|---------|---------|---------|
| TC-01 | 秒杀成功 | 验证单用户秒杀能成功获取订单 |
| TC-02 | 库存不足 | 验证库存耗尽后返回 SOLD_OUT |
| TC-03 | 用户防重 | 验证同一用户不能重复秒杀 |
| TC-04 | 订单状态查询 | 验证订单状态轮询功能 |
| TC-05 | 查询不存在的订单 | 验证错误处理 |

## 性能测试场景

| 场景 | 用户数 | 库存 | 说明 |
|-----|-------|------|-----|
| 基准测试 | 100 | 50 | 验证基础并发能力 |
| 极限压测 | 500 | 200 | 验证高并发处理 |
| 热点测试 | 1000 | 500 | 验证极限并发 |

## 性能指标说明

| 指标 | 说明 |
|-----|------|
| QPS | 每秒处理的请求数 |
| TPS | 每秒成功的订单数 |
| 成功率 | 成功订单数 / 总请求数 |
| P50/P95/P99 | 响应时间百分位数 |
| 超卖率 | 实际售出数 / 初始库存 |

## 预期测试结果

### 功能测试

- TC-01 ~ TC-05 应全部通过 (PASS)
- TC-04 可能显示 WARN（如果 order-service 未启动）

### 性能测试

- 成功率应接近 (库存数/请求数) × 100%
- 超卖率应 ≤ 100%（理想情况为 100%）
- P99 延迟应 < 100ms（取决于网络和系统性能）
