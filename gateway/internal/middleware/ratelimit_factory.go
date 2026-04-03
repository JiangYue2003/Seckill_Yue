package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/zeromicro/go-zero/core/limit"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

// RateLimitKeyGenerator 定义如何从请求上下文中提取限流 key
type RateLimitKeyGenerator func(c *gin.Context) string

// RateLimitStrategy 限流策略接口
type RateLimitStrategy interface {
	Allow(c *gin.Context) (bool, error)
	Close()
}

// ===================== 策略1: 滑动窗口（IP维度，保留原逻辑） =====================
type SlidingWindowLimiter struct {
	limiter *limit.PeriodLimit
	keyGen  RateLimitKeyGenerator
}

func NewSlidingWindowLimiter(redisHost string, qps int, prefix string, keyGen RateLimitKeyGenerator) *SlidingWindowLimiter {
	if keyGen == nil {
		keyGen = func(c *gin.Context) string { return c.ClientIP() }
	}
	l := limit.NewPeriodLimit(1, qps, redis.New(redisHost), prefix)
	return &SlidingWindowLimiter{limiter: l, keyGen: keyGen}
}

func (l *SlidingWindowLimiter) Allow(c *gin.Context) (bool, error) {
	code, err := l.limiter.Take(l.keyGen(c))
	return code != limit.OverQuota && err == nil, err
}

func (l *SlidingWindowLimiter) Close() {}

// ===================== 策略2: 令牌桶（自定义 key，支持 userId:productId 维度） =====================

// tokenLuaScript go-zero 官方令牌桶 Lua 脚本，直接复用于自定义 key 场景
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

// RedisTokenLimiter 基于 Redis Lua 脚本的令牌桶，支持动态 key
type RedisTokenLimiter struct {
	store  *redis.Redis
	rate   int
	burst  int
	keyGen RateLimitKeyGenerator
	script *redis.Script
}

func NewRedisTokenLimiter(redisHost string, burst, rate int, keyGen RateLimitKeyGenerator) *RedisTokenLimiter {
	return &RedisTokenLimiter{
		store:  redis.New(redisHost),
		rate:   rate,
		burst:  burst,
		keyGen: keyGen,
		script: redis.NewScript(tokenLuaScript),
	}
}

func (l *RedisTokenLimiter) Allow(c *gin.Context) (bool, error) {
	key := l.keyGen(c)
	if key == "" {
		return false, fmt.Errorf("无法生成限流 key")
	}

	tokensKey := fmt.Sprintf("{%s}.tokens", key)
	tsKey := fmt.Sprintf("{%s}.ts", key)
	now := time.Now()

	resp, err := l.store.ScriptRunCtx(context.Background(), l.script,
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

func (l *RedisTokenLimiter) Close() {}

// ===================== 策略3: IP令牌桶（降级用） =====================
type IPTokenBucketLimiter struct {
	inner *RedisTokenLimiter
}

func NewIPTokenBucketLimiter(redisHost string, capacity, rate int) *IPTokenBucketLimiter {
	inner := NewRedisTokenLimiter(redisHost, capacity, rate,
		func(c *gin.Context) string { return c.ClientIP() })
	return &IPTokenBucketLimiter{inner: inner}
}

func (l *IPTokenBucketLimiter) Allow(c *gin.Context) (bool, error) {
	return l.inner.Allow(c)
}

func (l *IPTokenBucketLimiter) Close() {}

// ===================== 工厂函数 =====================
type RateLimitConfig struct {
	Strategy string // "token_bucket" | "sliding_window" | "ip_token_bucket"
	QPS      int    // 令牌桶: 每秒补充令牌数; 滑动窗口: 每秒最大请求数
	Capacity int    // 令牌桶: 桶容量(突发上限); 滑动窗口: 忽略
}

func NewRateLimitStrategy(redisHost string, cfg RateLimitConfig) (RateLimitStrategy, error) {
	switch cfg.Strategy {
	case "token_bucket":
		// userId:productId 维度的令牌桶
		return NewRedisTokenLimiter(redisHost, cfg.Capacity, cfg.QPS,
			func(c *gin.Context) string {
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

// extractProductId 从 query string 或 JSON body 中提取 seckillProductId
func extractProductId(c *gin.Context) int64 {
	// 先从 query 取
	if pidStr := c.Query("seckillProductId"); pidStr != "" {
		if pid, err := strconv.ParseInt(pidStr, 10, 64); err == nil {
			return pid
		}
	}
	// 从 body 取（POST 请求走这里）
	var body struct {
		SeckillProductId int64 `json:"seckillProductId"`
	}
	// 只尝试解析，不影响已有的 body binding
	_ = c.ShouldBindJSON(&body)
	return body.SeckillProductId
}

// ===================== 网关中间件适配器 =====================
func RateLimitMiddleware(strategy RateLimitStrategy) gin.HandlerFunc {
	return func(c *gin.Context) {
		allowed, err := strategy.Allow(c)
		if err != nil || !allowed {
			c.JSON(http.StatusTooManyRequests, ErrorResponse{
				Code:    429,
				Message: "请求过于频繁，请稍后重试",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}
