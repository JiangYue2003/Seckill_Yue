package handler

import (
	"context"
	"strconv"
	"time"

	"seckill-mall/common/seckill"
	"seckill-mall/gateway-hertz/internal/middleware"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// SeckillHandler 秒杀相关接口
type SeckillHandler struct {
	seckillSvc seckill.SeckillServiceClient
}

func NewSeckillHandler(svc seckill.SeckillServiceClient) *SeckillHandler {
	return &SeckillHandler{seckillSvc: svc}
}

// SeckillRequest 秒杀请求
type SeckillRequest struct {
	SeckillProductId int64 `json:"seckillProductId"`
	Quantity         int64 `json:"quantity"`
}

// Seckill 秒杀下单
func (h *SeckillHandler) Seckill(ctx context.Context, c *app.RequestContext) {
	userId := middleware.GetUserIdFromContext(c)
	if userId == 0 {
		middleware.ErrorWithStatus(ctx, c, consts.StatusUnauthorized, 401, "请先登录")
		return
	}

	var req SeckillRequest

	// 优先从 query 取 seckillProductId（限流中间件也走 query，避免 body 被解析两次）
	if pidStr := string(c.QueryArgs().Peek("seckillProductId")); pidStr != "" {
		if pid, err := strconv.ParseInt(pidStr, 10, 64); err == nil {
			req.SeckillProductId = pid
		}
	}

	var bodyReq SeckillRequest
	if err := c.BindJSON(&bodyReq); err == nil {
		if req.SeckillProductId == 0 {
			req.SeckillProductId = bodyReq.SeckillProductId
		}
		if bodyReq.Quantity > 0 {
			req.Quantity = bodyReq.Quantity
		}
	}

	if req.SeckillProductId <= 0 {
		middleware.ErrorWithStatus(ctx, c, consts.StatusBadRequest, 400, "参数错误: seckillProductId 不能为空")
		return
	}
	if req.Quantity <= 0 {
		req.Quantity = 1
	}

	hlog.CtxDebugf(ctx, "秒杀请求: userId=%d, seckillProductId=%d, quantity=%d",
		userId, req.SeckillProductId, req.Quantity)

	rpcCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	resp, err := h.seckillSvc.Seckill(rpcCtx, &seckill.SeckillRequest{
		UserId:           userId,
		SeckillProductId: req.SeckillProductId,
		Quantity:         req.Quantity,
	})
	if err != nil {
		hlog.CtxErrorf(ctx, "秒杀请求失败: %v", err)
		middleware.ErrorWithStatus(ctx, c, consts.StatusInternalServerError, 500, "秒杀请求失败: "+err.Error())
		return
	}

	middleware.Success(ctx, c, map[string]interface{}{
		"success": resp.Success,
		"code":    resp.Code,
		"message": resp.Message,
		"orderId": resp.OrderId,
	})
}

// GetSeckillStatus 查询秒杀状态
func (h *SeckillHandler) GetSeckillStatus(ctx context.Context, c *app.RequestContext) {
	userId := middleware.GetUserIdFromContext(c)
	if userId == 0 {
		middleware.ErrorWithStatus(ctx, c, consts.StatusUnauthorized, 401, "请先登录")
		return
	}

	seckillProductId, err := strconv.ParseInt(string(c.QueryArgs().Peek("seckillProductId")), 10, 64)
	if err != nil {
		middleware.ErrorWithStatus(ctx, c, consts.StatusBadRequest, 400, "无效的秒杀商品ID")
		return
	}

	hlog.CtxDebugf(ctx, "查询秒杀状态: userId=%d, seckillProductId=%d", userId, seckillProductId)

	rpcCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	resp, err := h.seckillSvc.GetSeckillStatus(rpcCtx, &seckill.SeckillStatusRequest{
		UserId:           userId,
		SeckillProductId: seckillProductId,
	})
	if err != nil {
		hlog.CtxErrorf(ctx, "查询秒杀状态失败: %v", err)
		middleware.ErrorWithStatus(ctx, c, consts.StatusInternalServerError, 500, "查询秒杀状态失败")
		return
	}

	middleware.Success(ctx, c, map[string]interface{}{
		"status":    resp.Status,
		"orderId":   resp.OrderId,
		"productId": resp.ProductId,
		"quantity":  resp.Quantity,
	})
}

// GetSeckillResult 查询秒杀结果
func (h *SeckillHandler) GetSeckillResult(ctx context.Context, c *app.RequestContext) {
	orderId := string(c.QueryArgs().Peek("orderId"))
	if orderId == "" {
		middleware.ErrorWithStatus(ctx, c, consts.StatusBadRequest, 400, "订单号不能为空")
		return
	}

	hlog.CtxDebugf(ctx, "查询秒杀结果: orderId=%s", orderId)

	rpcCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	resp, err := h.seckillSvc.GetSeckillResult(rpcCtx, &seckill.SeckillResultRequest{OrderId: orderId})
	if err != nil {
		hlog.CtxErrorf(ctx, "查询秒杀结果失败: %v", err)
		middleware.ErrorWithStatus(ctx, c, consts.StatusInternalServerError, 500, "查询秒杀结果失败")
		return
	}

	middleware.Success(ctx, c, map[string]interface{}{
		"success":     resp.Success,
		"orderId":     resp.OrderId,
		"productId":   resp.ProductId,
		"productName": resp.ProductName,
		"quantity":    resp.Quantity,
		"amount":      resp.Amount,
		"status":      resp.Status,
		"message":     resp.Message,
	})
}
