package logic

import (
	"context"

	"seckill-mall/common/order"
	"seckill-mall/order-service/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListUserOrdersLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListUserOrdersLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListUserOrdersLogic {
	return &ListUserOrdersLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// ListUserOrders 用户订单列表
func (l *ListUserOrdersLogic) ListUserOrders(in *order.ListUserOrdersRequest) (*order.ListUserOrdersResponse, error) {
	// 设置默认值
	page := in.Page
	if page <= 0 {
		page = 1
	}
	pageSize := in.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	// 查询订单列表
	orders, total, err := l.svcCtx.OrderModel.FindByUserId(l.ctx, in.UserId, in.Status, page, pageSize)
	if err != nil {
		l.Logger.Errorf("查询订单列表失败: userId=%d, err=%v", in.UserId, err)
		return nil, err
	}

	// 转换响应
	var orderInfos []*order.OrderInfo
	for _, o := range orders {
		orderInfos = append(orderInfos, &order.OrderInfo{
			OrderId:      o.OrderId,
			UserId:       o.UserId,
			ProductId:    o.ProductId,
			ProductName:  o.ProductName,
			Quantity:     int64(o.Quantity),
			Amount:       o.Amount,
			SeckillPrice: o.SeckillPrice,
			OrderType:    o.OrderType,
			Status:       o.Status,
			PaymentId:    o.PaymentId,
			PaidAt:       o.PaidAt,
			CreatedAt:    o.CreatedAt,
			UpdatedAt:    o.UpdatedAt,
		})
	}

	return &order.ListUserOrdersResponse{
		Orders: orderInfos,
		Total:  total,
	}, nil
}
