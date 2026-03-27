package middleware

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/zeromicro/go-zero/core/limit"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

// RateLimiter 创建限流中间件
// qps: 每秒请求数
func RateLimiter(redisHost string, qps int) gin.HandlerFunc {
	// 创建限流器
	l := limit.NewTokenLimiter(qps, qps*2, redis.New(redisHost), "ratelimit")

	return func(c *gin.Context) {
		// 使用 IP + Path 作为限流 key
		key := fmt.Sprintf("%s:%s", c.ClientIP(), c.Request.URL.Path)

		// 检查是否允许通过
		if !l.Allow() {
			c.JSON(http.StatusTooManyRequests, ErrorResponse{
				Code:    429,
				Message: "请求过于频繁，请稍后重试",
			})
			c.Abort()
			return
		}

		// 记录限流信息
		_ = key
		c.Next()
	}
}

// IPRateLimiter IP 级别限流
func IPRateLimiter(redisHost string, qps int) gin.HandlerFunc {
	l := limit.NewTokenLimiter(qps, qps*2, redis.New(redisHost), "ip-ratelimit")

	return func(c *gin.Context) {
		if !l.Allow() {
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
