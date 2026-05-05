package handler

import (
	"context"
	"strconv"
	"time"

	"seckill-mall/common/order"
	"seckill-mall/gateway-hertz/internal/middleware"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// OrderHandler 订单相关接口
type OrderHandler struct {
	orderSvc order.OrderServiceClient
}

func NewOrderHandler(svc order.OrderServiceClient) *OrderHandler {
	return &OrderHandler{orderSvc: svc}
}

// GetOrder 获取订单详情
func (h *OrderHandler) GetOrder(ctx context.Context, c *app.RequestContext) {
	userId := middleware.GetUserIdFromContext(c)
	if userId == 0 {
		middleware.ErrorWithStatus(ctx, c, consts.StatusUnauthorized, 401, "请先登录")
		return
	}

	orderId := c.Param("orderId")
	if orderId == "" {
		middleware.ErrorWithStatus(ctx, c, consts.StatusBadRequest, 400, "订单号不能为空")
		return
	}

	hlog.CtxInfof(ctx, "获取订单详情: userId=%d, orderId=%s", userId, orderId)

	rpcCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := h.orderSvc.GetOrder(rpcCtx, &order.GetOrderRequest{OrderId: orderId})
	if err != nil {
		hlog.CtxErrorf(ctx, "获取订单详情失败: %v", err)
		middleware.ErrorWithStatus(ctx, c, consts.StatusInternalServerError, 500, "获取订单详情失败")
		return
	}

	middleware.Success(ctx, c, map[string]interface{}{
		"orderId":      resp.OrderId,
		"userId":       resp.UserId,
		"productId":    resp.ProductId,
		"productName":  resp.ProductName,
		"quantity":     resp.Quantity,
		"amount":       resp.Amount,
		"seckillPrice": resp.SeckillPrice,
		"orderType":    resp.OrderType,
		"status":       resp.Status,
		"paymentId":    resp.PaymentId,
		"paidAt":       resp.PaidAt,
		"createdAt":    resp.CreatedAt,
		"updatedAt":    resp.UpdatedAt,
	})
}

// ListOrders 订单列表
func (h *OrderHandler) ListOrders(ctx context.Context, c *app.RequestContext) {
	userId := middleware.GetUserIdFromContext(c)
	if userId == 0 {
		middleware.ErrorWithStatus(ctx, c, consts.StatusUnauthorized, 401, "请先登录")
		return
	}

	page, _ := strconv.ParseInt(string(c.DefaultQuery("page", "1")), 10, 64)
	pageSize, _ := strconv.ParseInt(string(c.DefaultQuery("pageSize", "20")), 10, 64)
	status, _ := strconv.ParseInt(string(c.DefaultQuery("status", "0")), 10, 64)

	hlog.CtxInfof(ctx, "获取订单列表: userId=%d, page=%d, pageSize=%d", userId, page, pageSize)

	rpcCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := h.orderSvc.ListUserOrders(rpcCtx, &order.ListUserOrdersRequest{
		UserId:   userId,
		Status:   int32(status),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		hlog.CtxErrorf(ctx, "获取订单列表失败: %v", err)
		middleware.ErrorWithStatus(ctx, c, consts.StatusInternalServerError, 500, "获取订单列表失败")
		return
	}

	orders := make([]map[string]interface{}, 0, len(resp.Orders))
	for _, o := range resp.Orders {
		orders = append(orders, map[string]interface{}{
			"orderId":     o.OrderId,
			"productId":   o.ProductId,
			"productName": o.ProductName,
			"quantity":    o.Quantity,
			"amount":      o.Amount,
			"orderType":   o.OrderType,
			"status":      o.Status,
			"createdAt":   o.CreatedAt,
		})
	}

	middleware.Success(ctx, c, map[string]interface{}{
		"orders":   orders,
		"total":    resp.Total,
		"page":     page,
		"pageSize": pageSize,
	})
}

// CancelOrder 取消订单
func (h *OrderHandler) CancelOrder(ctx context.Context, c *app.RequestContext) {
	userId := middleware.GetUserIdFromContext(c)
	if userId == 0 {
		middleware.ErrorWithStatus(ctx, c, consts.StatusUnauthorized, 401, "请先登录")
		return
	}

	orderId := c.Param("orderId")
	if orderId == "" {
		middleware.ErrorWithStatus(ctx, c, consts.StatusBadRequest, 400, "订单号不能为空")
		return
	}

	hlog.CtxInfof(ctx, "取消订单: userId=%d, orderId=%s", userId, orderId)

	rpcCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := h.orderSvc.CancelOrder(rpcCtx, &order.CancelOrderRequest{
		OrderId: orderId,
		UserId:  userId,
	})
	if err != nil {
		hlog.CtxErrorf(ctx, "取消订单失败: %v", err)
		middleware.ErrorWithStatus(ctx, c, consts.StatusInternalServerError, 500, "取消订单失败")
		return
	}
	if !resp.Success {
		middleware.ErrorWithStatus(ctx, c, consts.StatusBadRequest, 400, resp.Message)
		return
	}

	middleware.Success(ctx, c, map[string]interface{}{"success": true, "message": resp.Message})
}

// PayOrderRequest 支付订单请求
type PayOrderRequest struct {
	OrderId   string `json:"orderId"`
	PaymentId string `json:"paymentId"`
}

// PayOrder 支付订单
func (h *OrderHandler) PayOrder(ctx context.Context, c *app.RequestContext) {
	userId := middleware.GetUserIdFromContext(c)
	if userId == 0 {
		middleware.ErrorWithStatus(ctx, c, consts.StatusUnauthorized, 401, "请先登录")
		return
	}

	var req PayOrderRequest
	if err := c.BindJSON(&req); err != nil {
		middleware.ErrorWithStatus(ctx, c, consts.StatusBadRequest, 400, "参数错误: "+err.Error())
		return
	}
	if req.OrderId == "" || req.PaymentId == "" {
		middleware.ErrorWithStatus(ctx, c, consts.StatusBadRequest, 400, "orderId 和 paymentId 不能为空")
		return
	}

	hlog.CtxInfof(ctx, "支付订单: userId=%d, orderId=%s", userId, req.OrderId)

	rpcCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := h.orderSvc.PayOrder(rpcCtx, &order.PayOrderRequest{
		OrderId:   req.OrderId,
		PaymentId: req.PaymentId,
	})
	if err != nil {
		hlog.CtxErrorf(ctx, "支付订单失败: %v", err)
		middleware.ErrorWithStatus(ctx, c, consts.StatusInternalServerError, 500, "支付订单失败")
		return
	}
	if !resp.Success {
		middleware.ErrorWithStatus(ctx, c, consts.StatusBadRequest, 400, resp.Message)
		return
	}

	middleware.Success(ctx, c, map[string]interface{}{"success": true, "message": resp.Message})
}

// CreateNormalOrderRequest 创建普通订单请求
type CreateNormalOrderRequest struct {
	ProductId int64 `json:"productId"`
	Quantity  int64 `json:"quantity"`
}

// CreateNormalOrder 创建普通订单
func (h *OrderHandler) CreateNormalOrder(ctx context.Context, c *app.RequestContext) {
	userId := middleware.GetUserIdFromContext(c)
	if userId == 0 {
		middleware.ErrorWithStatus(ctx, c, consts.StatusUnauthorized, 401, "请先登录")
		return
	}

	var req CreateNormalOrderRequest
	if err := c.BindJSON(&req); err != nil {
		middleware.ErrorWithStatus(ctx, c, consts.StatusBadRequest, 400, "参数错误: "+err.Error())
		return
	}
	if req.ProductId <= 0 || req.Quantity <= 0 {
		middleware.ErrorWithStatus(ctx, c, consts.StatusBadRequest, 400, "productId 和 quantity 不能为空")
		return
	}

	hlog.CtxInfof(ctx, "创建普通订单: userId=%d, productId=%d, quantity=%d", userId, req.ProductId, req.Quantity)

	rpcCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := h.orderSvc.CreateNormalOrder(rpcCtx, &order.CreateNormalOrderRequest{
		UserId:    userId,
		ProductId: req.ProductId,
		Quantity:  req.Quantity,
	})
	if err != nil {
		hlog.CtxErrorf(ctx, "创建普通订单失败: %v", err)
		middleware.ErrorWithStatus(ctx, c, consts.StatusInternalServerError, 500, "创建普通订单失败: "+err.Error())
		return
	}

	middleware.Success(ctx, c, map[string]interface{}{
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
func (h *OrderHandler) RefundOrder(ctx context.Context, c *app.RequestContext) {
	userId := middleware.GetUserIdFromContext(c)
	if userId == 0 {
		middleware.ErrorWithStatus(ctx, c, consts.StatusUnauthorized, 401, "请先登录")
		return
	}

	orderId := c.Param("orderId")
	if orderId == "" {
		middleware.ErrorWithStatus(ctx, c, consts.StatusBadRequest, 400, "订单号不能为空")
		return
	}

	reason := "用户申请退款"
	var reqBody struct {
		Reason string `json:"reason"`
	}
	if err := c.BindJSON(&reqBody); err == nil && reqBody.Reason != "" {
		reason = reqBody.Reason
	}

	hlog.CtxInfof(ctx, "退款: userId=%d, orderId=%s, reason=%s", userId, orderId, reason)

	rpcCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := h.orderSvc.RefundOrder(rpcCtx, &order.RefundOrderRequest{
		OrderId: orderId,
		UserId:  userId,
		Reason:  reason,
	})
	if err != nil {
		hlog.CtxErrorf(ctx, "退款失败: %v", err)
		middleware.ErrorWithStatus(ctx, c, consts.StatusInternalServerError, 500, "退款失败: "+err.Error())
		return
	}
	if !resp.Success {
		middleware.ErrorWithStatus(ctx, c, consts.StatusBadRequest, 400, resp.Message)
		return
	}

	middleware.Success(ctx, c, map[string]interface{}{"success": true, "message": resp.Message})
}
