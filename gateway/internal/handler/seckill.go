package handler

import (
	"context"
	"net/http"
	"strconv"

	"seckill-mall/gateway/internal/middleware"
	"seckill-mall/seckill-service/seckill"

	"github.com/gin-gonic/gin"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
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
	SeckillProductId int64 `json:"seckillProductId" binding:"required"`
	Quantity         int64 `json:"quantity" binding:"min=1"`
}

// Seckill 秒杀下单
func (h *SeckillHandler) Seckill(c *gin.Context) {
	userId := middleware.GetUserIdFromContext(c)
	if userId == 0 {
		middleware.ErrorWithStatus(c, http.StatusUnauthorized, 401, "请先登录")
		return
	}

	var req SeckillRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.ErrorWithStatus(c, http.StatusBadRequest, 400, "参数错误: "+err.Error())
		return
	}

	if req.Quantity <= 0 {
		req.Quantity = 1
	}

	logx.Infof("秒杀请求: userId=%d, seckillProductId=%d, quantity=%d",
		userId, req.SeckillProductId, req.Quantity)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	resp, err := h.seckillSvc.Seckill(ctx, &seckill.SeckillRequest{
		UserId:           userId,
		SeckillProductId: req.SeckillProductId,
		Quantity:         req.Quantity,
	})
	if err != nil {
		logx.Errorf("秒杀请求失败: %v", err)
		middleware.ErrorWithStatus(c, http.StatusInternalServerError, 500, "秒杀请求失败: "+err.Error())
		return
	}

	middleware.Success(c, gin.H{
		"success": resp.Success,
		"code":    resp.Code,
		"message": resp.Message,
		"orderId": resp.OrderId,
	})
}

// GetSeckillStatus 查询秒杀状态
func (h *SeckillHandler) GetSeckillStatus(c *gin.Context) {
	userId := middleware.GetUserIdFromContext(c)
	if userId == 0 {
		middleware.ErrorWithStatus(c, http.StatusUnauthorized, 401, "请先登录")
		return
	}

	seckillProductId, err := strconv.ParseInt(c.Query("seckillProductId"), 10, 64)
	if err != nil {
		middleware.ErrorWithStatus(c, http.StatusBadRequest, 400, "无效的秒杀商品ID")
		return
	}

	logx.Infof("查询秒杀状态: userId=%d, seckillProductId=%d", userId, seckillProductId)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	resp, err := h.seckillSvc.GetSeckillStatus(ctx, &seckill.SeckillStatusRequest{
		UserId:           userId,
		SeckillProductId: seckillProductId,
	})
	if err != nil {
		logx.Errorf("查询秒杀状态失败: %v", err)
		middleware.ErrorWithStatus(c, http.StatusInternalServerError, 500, "查询秒杀状态失败")
		return
	}

	middleware.Success(c, gin.H{
		"status":    resp.Status,
		"orderId":   resp.OrderId,
		"productId": resp.ProductId,
		"quantity":  resp.Quantity,
	})
}

// GetSeckillResult 查询秒杀结果
func (h *SeckillHandler) GetSeckillResult(c *gin.Context) {
	orderId := c.Query("orderId")
	if orderId == "" {
		middleware.ErrorWithStatus(c, http.StatusBadRequest, 400, "订单号不能为空")
		return
	}

	logx.Infof("查询秒杀结果: orderId=%s", orderId)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	resp, err := h.seckillSvc.GetSeckillResult(ctx, &seckill.SeckillResultRequest{OrderId: orderId})
	if err != nil {
		logx.Errorf("查询秒杀结果失败: %v", err)
		middleware.ErrorWithStatus(c, http.StatusInternalServerError, 500, "查询秒杀结果失败")
		return
	}

	middleware.Success(c, gin.H{
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
