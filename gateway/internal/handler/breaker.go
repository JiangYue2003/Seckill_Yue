package handler

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/zeromicro/go-zero/core/logx"
	"net/http"
	"seckill-mall/gateway/internal/middleware"
)

// isBreakerError 判断是否是熔断器触发的错误
// go-zero 熔断器打开时返回的错误信息包含 "breaker" 关键字
func isBreakerError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "breaker") || strings.Contains(msg, "circuit")
}

// handleRPCError 统一处理 RPC 调用错误，区分熔断和普通错误
// 熔断时返回 503 + 友好提示，普通错误返回 500
func handleRPCError(c *gin.Context, err error, operation string) {
	if isBreakerError(err) {
		logx.Errorf("[breaker] %s 熔断触发: %v", operation, err)
		middleware.ErrorWithStatus(c, http.StatusServiceUnavailable, 503, "系统繁忙，请稍后重试")
		return
	}
	logx.Errorf("%s 失败: %v", operation, err)
	middleware.ErrorWithStatus(c, http.StatusInternalServerError, 500, operation+"失败")
}
