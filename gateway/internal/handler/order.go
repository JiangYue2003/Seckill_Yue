package handler

import (
	"context"
	"net/http"
	"strconv"

	"seckill-mall/common/order"
	"seckill-mall/gateway/internal/middleware"

	"github.com/gin-gonic/gin"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// OrderHandler 订单相关接口
type OrderHandler struct {
	orderSvc order.OrderServiceClient
}

func NewOrderHandler(svc order.OrderServiceClient) *OrderHandler {
	return &OrderHandler{orderSvc: svc}
}

// GetOrder 获取订单详情
func (h *OrderHandler) GetOrder(c *gin.Context) {
	userId := middleware.GetUserIdFromContext(c)
	if userId == 0 {
		middleware.ErrorWithStatus(c, http.StatusUnauthorized, 401, "请先登录")
		return
	}

	orderId := c.Param("orderId")
	if orderId == "" {
		middleware.ErrorWithStatus(c, http.StatusBadRequest, 400, "订单号不能为空")
		return
	}

	logx.Infof("获取订单详情: userId=%d, orderId=%s", userId, orderId)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.orderSvc.GetOrder(ctx, &order.GetOrderRequest{OrderId: orderId})
	if err != nil {
		handleRPCError(c, err, "获取订单详情")
		return
	}

	middleware.Success(c, gin.H{
		"orderId":           resp.OrderId,
		"userId":            resp.UserId,
		"productId":         resp.ProductId,
		"productName":       resp.ProductName,
		"quantity":          resp.Quantity,
		"amount":            resp.Amount,
		"seckillPrice":      resp.SeckillPrice,
		"orderType":         resp.OrderType,
		"status":            resp.Status,
		"paymentId":         resp.PaymentId,
		"reservationId":     resp.ReservationId,
		"reservationStatus": resp.ReservationStatus.String(),
		"paymentStatus":     resp.PaymentStatus.String(),
		"paidAt":            resp.PaidAt,
		"expiredAt":         resp.ExpiredAt,
		"createdAt":         resp.CreatedAt,
		"updatedAt":         resp.UpdatedAt,
	})
}

// ListOrders 订单列表
func (h *OrderHandler) ListOrders(c *gin.Context) {
	userId := middleware.GetUserIdFromContext(c)
	if userId == 0 {
		middleware.ErrorWithStatus(c, http.StatusUnauthorized, 401, "请先登录")
		return
	}

	page, _ := strconv.ParseInt(c.DefaultQuery("page", "1"), 10, 64)
	pageSize, _ := strconv.ParseInt(c.DefaultQuery("pageSize", "20"), 10, 64)
	status, _ := strconv.ParseInt(c.DefaultQuery("status", "0"), 10, 64)

	logx.Infof("获取订单列表: userId=%d, page=%d, pageSize=%d", userId, page, pageSize)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.orderSvc.ListUserOrders(ctx, &order.ListUserOrdersRequest{
		UserId:   userId,
		Status:   int32(status),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		handleRPCError(c, err, "获取订单列表")
		return
	}

	orders := make([]gin.H, 0, len(resp.Orders))
	for _, o := range resp.Orders {
		orders = append(orders, gin.H{
			"orderId":           o.OrderId,
			"productId":         o.ProductId,
			"productName":       o.ProductName,
			"quantity":          o.Quantity,
			"amount":            o.Amount,
			"orderType":         o.OrderType,
			"status":            o.Status,
			"reservationId":     o.ReservationId,
			"reservationStatus": o.ReservationStatus.String(),
			"paymentStatus":     o.PaymentStatus.String(),
			"createdAt":         o.CreatedAt,
		})
	}

	middleware.Success(c, gin.H{
		"orders":   orders,
		"total":    resp.Total,
		"page":     page,
		"pageSize": pageSize,
	})
}

// CancelOrder 取消订单
func (h *OrderHandler) CancelOrder(c *gin.Context) {
	userId := middleware.GetUserIdFromContext(c)
	if userId == 0 {
		middleware.ErrorWithStatus(c, http.StatusUnauthorized, 401, "请先登录")
		return
	}

	orderId := c.Param("orderId")
	if orderId == "" {
		middleware.ErrorWithStatus(c, http.StatusBadRequest, 400, "订单号不能为空")
		return
	}

	logx.Infof("取消订单: userId=%d, orderId=%s", userId, orderId)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.orderSvc.CancelOrder(ctx, &order.CancelOrderRequest{
		OrderId: orderId,
		UserId:  userId,
	})
	if err != nil {
		handleRPCError(c, err, "取消订单")
		return
	}

	if !resp.Success {
		middleware.ErrorWithStatus(c, http.StatusBadRequest, 400, resp.Message)
		return
	}

	middleware.Success(c, gin.H{
		"success": true,
		"message": resp.Message,
	})
}

// PayOrderRequest 支付订单请求
type PayOrderRequest struct {
	OrderId   string `json:"orderId" binding:"required"`
	Channel   string `json:"channel"`
	RequestId string `json:"requestId"`
}

// PayOrder 支付订单
func (h *OrderHandler) PayOrder(c *gin.Context) {
	userId := middleware.GetUserIdFromContext(c)
	if userId == 0 {
		middleware.ErrorWithStatus(c, http.StatusUnauthorized, 401, "请先登录")
		return
	}

	var req PayOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.ErrorWithStatus(c, http.StatusBadRequest, 400, "参数错误: "+err.Error())
		return
	}

	logx.Infof("支付订单: userId=%d, orderId=%s", userId, req.OrderId)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.orderSvc.PayOrder(ctx, &order.PayOrderRequest{
		OrderId:   req.OrderId,
		Channel:   req.Channel,
		RequestId: req.RequestId,
	})
	if err != nil {
		handleRPCError(c, err, "支付订单")
		return
	}

	if !resp.Success {
		middleware.ErrorWithStatus(c, http.StatusBadRequest, 400, resp.Message)
		return
	}

	middleware.Success(c, gin.H{
		"success": true,
		"message": resp.Message,
	})
}

type MockPaymentCallbackRequest struct {
	PaymentId         string `json:"paymentId" binding:"required"`
	OrderId           string `json:"orderId" binding:"required"`
	CallbackId        string `json:"callbackId" binding:"required"`
	Channel           string `json:"channel"`
	ThirdPartyTradeNo string `json:"thirdPartyTradeNo"`
	RawPayload        string `json:"rawPayload"`
}

func (h *OrderHandler) HandleMockPaymentCallback(c *gin.Context) {
	var req MockPaymentCallbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.ErrorWithStatus(c, http.StatusBadRequest, 400, "参数错误: "+err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.orderSvc.HandlePaymentCallback(ctx, &order.HandlePaymentCallbackRequest{
		PaymentId:         req.PaymentId,
		OrderId:           req.OrderId,
		CallbackId:        req.CallbackId,
		Channel:           req.Channel,
		ThirdPartyTradeNo: req.ThirdPartyTradeNo,
		RawPayload:        req.RawPayload,
	})
	if err != nil {
		handleRPCError(c, err, "处理支付回调")
		return
	}
	if !resp.Success {
		middleware.ErrorWithStatus(c, http.StatusBadRequest, 400, resp.Message)
		return
	}

	middleware.Success(c, gin.H{
		"success": true,
		"message": resp.Message,
	})
}

// CreateNormalOrderRequest 创建普通订单请求
type CreateNormalOrderRequest struct {
	ProductId int64 `json:"productId" binding:"required"`
	Quantity  int64 `json:"quantity" binding:"required,min=1"`
}

// CreateNormalOrder 创建普通订单
func (h *OrderHandler) CreateNormalOrder(c *gin.Context) {
	userId := middleware.GetUserIdFromContext(c)
	if userId == 0 {
		middleware.ErrorWithStatus(c, http.StatusUnauthorized, 401, "请先登录")
		return
	}

	var req CreateNormalOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.ErrorWithStatus(c, http.StatusBadRequest, 400, "参数错误: "+err.Error())
		return
	}

	logx.Infof("创建普通订单: userId=%d, productId=%d, quantity=%d", userId, req.ProductId, req.Quantity)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.orderSvc.CreateNormalOrder(ctx, &order.CreateNormalOrderRequest{
		UserId:    userId,
		ProductId: req.ProductId,
		Quantity:  req.Quantity,
	})
	if err != nil {
		handleRPCError(c, err, "创建普通订单")
		return
	}

	middleware.Success(c, gin.H{
		"orderId":     resp.OrderId,
		"productId":   resp.ProductId,
		"productName": resp.ProductName,
		"quantity":    resp.Quantity,
		"amount":      resp.Amount,
		"status":      resp.Status,
		"createdAt":   resp.CreatedAt,
	})
}

// RefundOrder 退款
func (h *OrderHandler) RefundOrder(c *gin.Context) {
	userId := middleware.GetUserIdFromContext(c)
	if userId == 0 {
		middleware.ErrorWithStatus(c, http.StatusUnauthorized, 401, "请先登录")
		return
	}

	orderId := c.Param("orderId")
	if orderId == "" {
		middleware.ErrorWithStatus(c, http.StatusBadRequest, 400, "订单号不能为空")
		return
	}

	// reason 为可选字段，优先从 JSON body 获取，否则使用默认值
	reason := "用户申请退款"
	var reqBody struct {
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&reqBody); err == nil && reqBody.Reason != "" {
		reason = reqBody.Reason
	}

	logx.Infof("退款: userId=%d, orderId=%s, reason=%s", userId, orderId, reason)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	resp, err := h.orderSvc.RefundOrder(ctx, &order.RefundOrderRequest{
		OrderId: orderId,
		UserId:  userId,
		Reason:  reason,
	})
	if err != nil {
		handleRPCError(c, err, "退款")
		return
	}

	if !resp.Success {
		middleware.ErrorWithStatus(c, http.StatusBadRequest, 400, resp.Message)
		return
	}

	middleware.Success(c, gin.H{
		"success": true,
		"message": resp.Message,
	})
}
