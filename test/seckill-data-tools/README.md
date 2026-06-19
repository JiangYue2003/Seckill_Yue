# 秒杀系统数据初始化工具

用于初始化和清理 Redis 中的兼容测试数据。

注意：它不是当前物理库存分片链路的标准夹具工具。当前它主要服务于旧式功能测试/冒烟脚本，不建议把它当成新版 benchmark 的必要前置步骤。

## 功能特性

- **幂等操作**：使用 `SET`/`DEL` 命令，可重复执行不会污染数据
- **批量操作**：使用 Redis Pipeline 批量删除用户记录，提升清理效率
- **支持清理**：测试完成后清理兼容测试数据

## 文件说明

| 文件 | 说明 |
|------|------|
| `main.go` | 程序入口，解析命令行参数，执行 init/cleanup |
| `go.mod` | 依赖管理（仅依赖 go-redis） |
| `go.sum` | 依赖锁文件 |

## 前置条件

| 服务 | 地址 | 说明 |
|------|------|------|
| Redis | `localhost:6379` | 唯一依赖 |

## 快速开始

### 初始化测试数据

```powershell
cd test\seckill-data-tools
go run . -mode=init
```

### 清理测试数据

```powershell
go run . -mode=cleanup
```

### 查看帮助

```powershell
go run . -help
```

## 命令行参数

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `-mode` | 运行模式：`init` 或 `cleanup` | （必填） |
| `-redis` | Redis 主机地址 | `localhost` |
| `-port` | Redis 端口 | `6379` |

## 当前写入的 Redis Key

### 秒杀商品

| Key | 格式 | 示例值 |
|-----|------|--------|
| 商品信息 | `seckill:info:{id}` | `1:1:599900:1735689600:1745776000` |
| 商品名称 | `seckill:product_name:{id}` | `iPhone 15 Pro 256GB` |
| 库存 | `seckill:stock:{id}` | `100` |

> info 格式：`{productId}:{price}:{startTime}:{endTime}`（均为 Unix 时间戳，单位秒）

这些 key 反映的是兼容夹具写法，不代表当前生产热路径的完整 Redis 结构。当前真实秒杀热路径已经使用：

- 总库存 key
- 分片状态 key
- 16 个物理分片库存 key
- 分片 reserve 标记
- 用户占位和订单热状态 key

### 功能测试商品

| 秒杀商品 ID | 关联商品 ID | 库存 | 用途 |
|-----------|------------|------|------|
| 1001 | 1001 | 10 | TC-01 秒杀成功 |
| 1002 | 1002 | 1 | TC-02 库存不足 |
| 1003 | 1003 | 10 | TC-03 用户防重 |
| 1004 | 1004 | 10 | TC-04 订单状态查询 |

### 性能测试商品

| 秒杀商品 ID | 关联商品 ID | 库存 | 用途 |
|-----------|------------|------|------|
| 2001 | 2001 | 500 | 旧式兼容压测夹具 |

## 与当前推荐路径的关系

- `test/seckill-benchmark-test` 已经会自行准备更贴近当前架构的压测场景，通常不需要先运行本工具
- 本工具更适合：
  - `test/seckill-functional-test`
  - 老式手工 Redis 冒烟
  - 兼容回归

## 与 test-e2e 的区别

| 工具 | 目标存储 | 适用场景 |
|------|---------|---------|
| `seckill-data-tools` | Redis only | 兼容夹具、本地冒烟 |
| `test-e2e/seckill_flow` | MySQL + Redis | HTTP 入口冒烟 |

`test-e2e` 会在每次运行时自动初始化数据，无需手动调用 `seckill-data-tools`。
