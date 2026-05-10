package handler

import (
	"context"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"seckill-mall/gateway-hertz/internal/middleware"
)

// isBreakerError 判断是否是熔断器触发的错误
func isBreakerError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "breaker") || strings.Contains(msg, "circuit")
}

// handleRPCError 统一处理 RPC 调用错误，区分熔断和普通错误
func handleRPCError(ctx context.Context, c *app.RequestContext, err error, operation string) {
	if isBreakerError(err) {
		hlog.CtxErrorf(ctx, "[breaker] %s 熔断触发: %v", operation, err)
		middleware.ErrorWithStatus(ctx, c, consts.StatusServiceUnavailable, 503, "系统繁忙，请稍后重试")
		return
	}
	hlog.CtxErrorf(ctx, "%s 失败: %v", operation, err)
	middleware.ErrorWithStatus(ctx, c, consts.StatusInternalServerError, 500, operation+"失败")
}
