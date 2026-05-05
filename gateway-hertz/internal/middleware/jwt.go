package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/redis/go-redis/v9"
)

// JWTClaims JWT 声明
type JWTClaims struct {
	UserId int64  `json:"userId"`
	Jti    string `json:"jti,omitempty"`
}

// JWTAuth JWT 认证中间件，验证 Access Token 签名和过期，同时检查 Redis 黑名单（jti）
func JWTAuth(accessSecret string, rdb *redis.Client) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		authHeader := string(c.GetHeader("Authorization"))
		if authHeader == "" {
			c.JSON(consts.StatusUnauthorized, ErrorResponse{Code: 401, Message: "未提供认证信息"})
			c.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			c.JSON(consts.StatusUnauthorized, ErrorResponse{Code: 401, Message: "Token 格式错误"})
			c.Abort()
			return
		}

		claims, err := ParseToken(parts[1], accessSecret)
		if err != nil || claims == nil {
			c.JSON(consts.StatusUnauthorized, ErrorResponse{Code: 401, Message: "无效的 Token"})
			c.Abort()
			return
		}

		// 检查黑名单
		if claims.Jti != "" {
			exists, _ := rdb.Exists(ctx, "user:blacklist:"+claims.Jti).Result()
			if exists > 0 {
				c.JSON(consts.StatusUnauthorized, ErrorResponse{Code: 401, Message: "Token 已失效"})
				c.Abort()
				return
			}
		}

		if claims.UserId > 0 {
			c.Set("userId", claims.UserId)
		}

		c.Next(ctx)
	}
}

// ParseToken 解析并验证 JWT Token（与 gateway-gin 完全一致的自实现逻辑）
func ParseToken(tokenString string, secret string) (*JWTClaims, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid token format")
	}

	headerB64, payloadB64, signatureB64 := parts[0], parts[1], parts[2]

	sig := hmacSha256(headerB64+"."+payloadB64, secret)
	sigBytes, err := base64.RawURLEncoding.DecodeString(signatureB64)
	if err != nil {
		return nil, errors.New("invalid signature encoding")
	}
	if !hmac.Equal(sig, sigBytes) {
		return nil, errors.New("invalid signature")
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return nil, errors.New("invalid payload encoding")
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return nil, errors.New("invalid payload format")
	}

	if exp, ok := payload["exp"].(float64); ok {
		if int64(exp) < time.Now().Unix() {
			return nil, errors.New("token expired")
		}
	}

	userId := int64(0)
	if uid, ok := payload["userId"].(float64); ok {
		userId = int64(uid)
	}
	jti := ""
	if j, ok := payload["jti"].(string); ok {
		jti = j
	}

	return &JWTClaims{UserId: userId, Jti: jti}, nil
}

func hmacSha256(message, secret string) []byte {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(message))
	return h.Sum(nil)
}

// RequestLogger 请求日志中间件
func RequestLogger() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		start := time.Now()
		c.Next(ctx)
		latency := time.Since(start).Milliseconds()
		hlog.CtxInfof(ctx, "http_request method=%s path=%s status=%d latency_ms=%d client_ip=%s",
			string(c.Method()), string(c.Path()), c.Response.StatusCode(), latency, c.ClientIP())
	}
}

// CORS 跨域中间件
func CORS() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		c.Header("Access-Control-Max-Age", "86400")

		if string(c.Method()) == http.MethodOptions {
			c.AbortWithStatus(consts.StatusNoContent)
			return
		}
		c.Next(ctx)
	}
}
