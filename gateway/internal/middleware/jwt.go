package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"seckill-mall/common/user"

	"github.com/gin-gonic/gin"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

// JWTAuth JWT 认证中间件
// 验证 Access Token 签名和过期，同时检查 Redis 黑名单（jti）
func JWTAuth(accessSecret string, redisHost string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, ErrorResponse{
				Code:    401,
				Message: "未提供认证信息",
			})
			c.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			c.JSON(http.StatusUnauthorized, ErrorResponse{
				Code:    401,
				Message: "Token 格式错误",
			})
			c.Abort()
			return
		}

		tokenString := parts[1]

		claims, err := ParseToken(tokenString, accessSecret)
		if err != nil || claims == nil {
			c.JSON(http.StatusUnauthorized, ErrorResponse{
				Code:    401,
				Message: "无效的 Token",
			})
			c.Abort()
			return
		}

		// 检查 Access Token 是否在黑名单（被动刷新 / 注销后失效）
		if claims.Jti != "" {
			blacklistKey := "user:blacklist:" + claims.Jti
			r := redis.New(redisHost)
			exists, _ := r.Exists(blacklistKey)
			if exists {
				c.JSON(http.StatusUnauthorized, ErrorResponse{
					Code:    401,
					Message: "Token 已失效",
				})
				c.Abort()
				return
			}
		}

		if claims.UserId > 0 {
			c.Set("userId", claims.UserId)
		}

		c.Next()
	}
}

// RefreshToken 调用 user-service 的 RefreshToken RPC，换取新 Token
func RefreshToken(ctx context.Context, refreshToken string, userSvc user.UserServiceClient) (*user.RefreshTokenResponse, error) {
	ctx2, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return userSvc.RefreshToken(ctx2, &user.RefreshTokenRequest{
		RefreshToken: refreshToken,
	})
}
