package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// JWTAuth JWT 认证中间件
func JWTAuth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取 Token
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, ErrorResponse{
				Code:    401,
				Message: "未提供认证信息",
			})
			c.Abort()
			return
		}

		// 解析 Bearer Token
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

		// 验证 Token
		claims, err := ParseToken(tokenString, secret)
		if err != nil || claims == nil {
			c.JSON(http.StatusUnauthorized, ErrorResponse{
				Code:    401,
				Message: "无效的 Token",
			})
			c.Abort()
			return
		}

		// 将用户ID存入上下文
		if claims.UserId > 0 {
			c.Set("userId", claims.UserId)
		}

		c.Next()
	}
}
