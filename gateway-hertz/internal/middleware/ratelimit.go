package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/zeromicro/go-zero/core/limit"
	gozeroredis "github.com/zeromicro/go-zero/core/stores/redis"
)

// RateLimitKeyGenerator 从请求上下文提取限流 key
type RateLimitKeyGenerator func(c *app.RequestContext) string

// RateLimitStrategy 限流策略接口
type RateLimitStrategy interface {
	Allow(ctx context.Context, c *app.RequestContext) (bool, error)
}

// ===================== 策略1: 滑动窗口（IP 维度） =====================

type SlidingWindowLimiter struct {
	limiter *limit.PeriodLimit
	keyGen  RateLimitKeyGenerator
}

func NewSlidingWindowLimiter(redisHost string, qps int, prefix string, keyGen RateLimitKeyGenerator) *SlidingWindowLimiter {
	if keyGen == nil {
		keyGen = func(c *app.RequestContext) string { return c.ClientIP() }
	}
	l := limit.NewPeriodLimit(1, qps, gozeroredis.New(redisHost), prefix)
	return &SlidingWindowLimiter{limiter: l, keyGen: keyGen}
}

func (l *SlidingWindowLimiter) Allow(_ context.Context, c *app.RequestContext) (bool, error) {
	code, err := l.limiter.Take(l.keyGen(c))
	return code != limit.OverQuota && err == nil, err
}

// ===================== 策略2: 令牌桶（userId:productId 维度） =====================

const tokenLuaScript = `
local rate = tonumber(ARGV[1])
local capacity = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])
local fill_time = capacity/rate
local ttl = math.floor(fill_time*2)
local last_tokens = tonumber(redis.call("get", KEYS[1]))
if last_tokens == nil then
    last_tokens = capacity
end
local last_refreshed = tonumber(redis.call("get", KEYS[2]))
if last_refreshed == nil then
    last_refreshed = 0
end
local delta = math.max(0, now-last_refreshed)
local filled_tokens = math.min(capacity, last_tokens+(delta*rate))
local allowed = filled_tokens >= requested
local new_tokens = filled_tokens
if allowed then
    new_tokens = filled_tokens - requested
end
redis.call("setex", KEYS[1], ttl, new_tokens)
redis.call("setex", KEYS[2], ttl, now)
return allowed
`

type RedisTokenLimiter struct {
	store  *gozeroredis.Redis
	rate   int
	burst  int
	keyGen RateLimitKeyGenerator
	script *gozeroredis.Script
}

func NewRedisTokenLimiter(redisHost string, burst, rate int, keyGen RateLimitKeyGenerator) *RedisTokenLimiter {
	return &RedisTokenLimiter{
		store:  gozeroredis.New(redisHost),
		rate:   rate,
		burst:  burst,
		keyGen: keyGen,
		script: gozeroredis.NewScript(tokenLuaScript),
	}
}

func (l *RedisTokenLimiter) Allow(ctx context.Context, c *app.RequestContext) (bool, error) {
	key := l.keyGen(c)
	if key == "" {
		return false, fmt.Errorf("无法生成限流 key")
	}
	tokensKey := fmt.Sprintf("{%s}.tokens", key)
	tsKey := fmt.Sprintf("{%s}.ts", key)
	now := time.Now()

	resp, err := l.store.ScriptRunCtx(ctx, l.script,
		[]string{tokensKey, tsKey},
		[]string{
			strconv.Itoa(l.rate),
			strconv.Itoa(l.burst),
			strconv.FormatInt(now.Unix(), 10),
			"1",
		})
	if err != nil {
		return false, err
	}
	code, ok := resp.(int64)
	if !ok {
		return false, fmt.Errorf("lua 脚本返回值类型错误: %T", resp)
	}
	return code == 1, nil
}

// ===================== 策略3: IP 令牌桶（降级用） =====================

type IPTokenBucketLimiter struct {
	inner *RedisTokenLimiter
}

func NewIPTokenBucketLimiter(redisHost string, capacity, rate int) *IPTokenBucketLimiter {
	return &IPTokenBucketLimiter{
		inner: NewRedisTokenLimiter(redisHost, capacity, rate,
			func(c *app.RequestContext) string { return c.ClientIP() }),
	}
}

func (l *IPTokenBucketLimiter) Allow(ctx context.Context, c *app.RequestContext) (bool, error) {
	return l.inner.Allow(ctx, c)
}

// ===================== 工厂函数 =====================

type RateLimitConfig struct {
	Strategy string
	QPS      int
	Capacity int
}

func NewRateLimitStrategy(redisHost string, cfg RateLimitConfig) (RateLimitStrategy, error) {
	switch cfg.Strategy {
	case "token_bucket":
		return NewRedisTokenLimiter(redisHost, cfg.Capacity, cfg.QPS,
			func(c *app.RequestContext) string {
				userId := GetUserIdFromContext(c)
				if userId == 0 {
					return ""
				}
				productId := extractProductId(c)
				return fmt.Sprintf("seckill:tokens:%d:%d", userId, productId)
			}), nil
	case "sliding_window":
		return NewSlidingWindowLimiter(redisHost, cfg.QPS, "seckill-ratelimit", nil), nil
	case "ip_token_bucket":
		return NewIPTokenBucketLimiter(redisHost, cfg.Capacity, cfg.QPS), nil
	default:
		return nil, fmt.Errorf("未知的限流策略: %s", cfg.Strategy)
	}
}

// extractProductId 从 query 或 body 提取 seckillProductId（body 读后恢复）
func extractProductId(c *app.RequestContext) int64 {
	if pidStr := string(c.QueryArgs().Peek("seckillProductId")); pidStr != "" {
		if pid, err := strconv.ParseInt(pidStr, 10, 64); err == nil {
			return pid
		}
	}

	body := c.Request.Body()
	if len(body) == 0 {
		return 0
	}

	// Hertz 的 Body() 返回的是内部 buffer，不需要恢复
	// 但 BodyStream 可能已消费，用 SetBodyRaw 恢复
	c.Request.SetBodyRaw(body)

	var req struct {
		SeckillProductId int64 `json:"seckillProductId"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return 0
	}
	return req.SeckillProductId
}

// RateLimitMiddleware 限流中间件适配器
func RateLimitMiddleware(strategy RateLimitStrategy) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		allowed, err := strategy.Allow(ctx, c)
		if err != nil || !allowed {
			c.JSON(consts.StatusTooManyRequests, ErrorResponse{
				Code:    429,
				Message: "请求过于频繁，请稍后重试",
			})
			c.Abort()
			return
		}
		c.Next(ctx)
	}
}

// bodyReader 用于 Hertz body 恢复（兼容 io.Reader 接口）
type bodyReader struct {
	*bytes.Reader
}

func (b *bodyReader) Close() error { return nil }

func newBodyReader(data []byte) io.ReadCloser {
	return &bodyReader{bytes.NewReader(data)}
}
