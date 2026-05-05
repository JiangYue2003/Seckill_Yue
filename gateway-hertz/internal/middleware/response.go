package middleware

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// Response 统一响应结构
type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// ErrorResponse 错误响应
type ErrorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Success 成功响应
func Success(ctx context.Context, c *app.RequestContext, data interface{}) {
	c.JSON(consts.StatusOK, Response{
		Code:    0,
		Message: "success",
		Data:    data,
	})
}

// Error 错误响应（HTTP 200）
func Error(ctx context.Context, c *app.RequestContext, code int, message string) {
	c.JSON(consts.StatusOK, ErrorResponse{Code: code, Message: message})
}

// ErrorWithStatus 带 HTTP 状态码的错误响应
func ErrorWithStatus(ctx context.Context, c *app.RequestContext, httpStatus int, code int, message string) {
	c.JSON(httpStatus, ErrorResponse{Code: code, Message: message})
}

// GetUserIdFromContext 从 Hertz 上下文获取 userId
func GetUserIdFromContext(c *app.RequestContext) int64 {
	val, exists := c.Get("userId")
	if !exists {
		return 0
	}
	uid, ok := val.(int64)
	if !ok {
		return 0
	}
	return uid
}
