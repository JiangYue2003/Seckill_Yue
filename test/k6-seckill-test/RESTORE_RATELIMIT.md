# 压测后恢复限流的步骤

## 1. 取消注释限流代码

编辑 `/f/sec1.1/gateway/gateway.go`，恢复第 80-88 行的限流中间件：

```go
seckillStrategy, err := middleware.NewRateLimitStrategy(c.RedisHost, middleware.RateLimitConfig{
    Strategy: c.RateLimit.Strategy,
    QPS:      c.RateLimit.QPS,
    Capacity: c.RateLimit.Capacity,
})
if err != nil {
    panic(fmt.Sprintf("初始化限流策略失败: %v", err))
}
seckillGroup.Use(middleware.RateLimitMiddleware(seckillStrategy))
```

## 2. 重启 gateway

```bash
cd /f/sec1.1/gateway
go run gateway.go -f etc/gateway.yaml
```

## 3. 验证限流是否生效

```bash
# 快速发送多个请求，应该看到 429 错误
for i in {1..10}; do
  curl -X POST http://localhost:8888/api/v1/seckill \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer <token>" \
    -d '{"seckillProductId":1,"quantity":1}'
done
```

预期输出：
```json
{"code":429,"message":"请求过于频繁，请稍后重试"}
```
