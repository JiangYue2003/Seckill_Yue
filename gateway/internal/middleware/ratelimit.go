package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/zeromicro/go-zero/core/limit"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

// RateLimiter 创建限流中间件（基于 IP，滑动窗口算法）
// 每个 IP 每秒最多 qps 次请求
func RateLimiter(redisHost string, qps int) gin.HandlerFunc {
	redisClient := redis.New(redisHost)
	l := limit.NewPeriodLimit(1, qps, redisClient, "seckill-ratelimit")

	return func(c *gin.Context) {
		code, err := l.Take(c.ClientIP())
		if err != nil || code == limit.OverQuota {
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

// IPRateLimiter IP 级别限流（通用滑动窗口版）
func IPRateLimiter(redisHost string, qps int) gin.HandlerFunc {
	redisClient := redis.New(redisHost)
	l := limit.NewPeriodLimit(1, qps, redisClient, "ip-ratelimit")

	return func(c *gin.Context) {
		code, err := l.Take(c.ClientIP())
		if err != nil || code == limit.OverQuota {
			c.JSON(http.StatusTooManyRequests, ErrorResponse{
				Code:    429,
				Message: "IP 请求过于频繁，请稍后重试",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}
