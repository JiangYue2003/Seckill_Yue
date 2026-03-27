package logic

import (
	"context"

	"seckill-mall/seckill-service/internal/redis"
	"seckill-mall/seckill-service/internal/svc"
	"seckill-mall/seckill-service/seckill"

	"github.com/zeromicro/go-zero/core/logx"
)

type UpdateOrderStatusLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUpdateOrderStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateOrderStatusLogic {
	return &UpdateOrderStatusLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// UpdateOrderStatus 更新秒杀订单状态（Order-Service 处理完成后回调）
// 将 Redis 订单状态从 "pending" 更新为 "success" 或 "failed"，使前端轮询能够感知订单已处理完成
func (l *UpdateOrderStatusLogic) UpdateOrderStatus(in *seckill.UpdateOrderStatusRequest) (*seckill.UpdateOrderStatusResponse, error) {
	// 参数校验
	if in.OrderId == "" {
		return &seckill.UpdateOrderStatusResponse{
			Success: false,
			Message: "订单号不能为空",
		}, nil
	}
	if in.Status == "" {
		return &seckill.UpdateOrderStatusResponse{
			Success: false,
			Message: "状态不能为空",
		}, nil
	}

	// 只允许写入合法状态
	if in.Status != redis.OrderStatusSuccess && in.Status != redis.OrderStatusFailed {
		return &seckill.UpdateOrderStatusResponse{
			Success: false,
			Message: "状态值非法，仅支持 success 或 failed",
		}, nil
	}

	// 从 Redis 获取当前订单信息
	orderInfo, err := l.svcCtx.Redis.GetOrderInfo(l.ctx, in.OrderId)
	if err != nil {
		l.Logger.Errorf("获取订单信息失败: orderId=%s, err=%v", in.OrderId, err)
		return &seckill.UpdateOrderStatusResponse{
			Success: false,
			Message: "系统繁忙，请稍后重试",
		}, nil
	}
	if orderInfo == nil {
		return &seckill.UpdateOrderStatusResponse{
			Success: false,
			Message: "订单不存在或已过期",
		}, nil
	}

	// 状态已是终态时直接返回成功（幂等）
	if orderInfo.Status == redis.OrderStatusSuccess || orderInfo.Status == redis.OrderStatusFailed {
		l.Logger.Infof("订单已是终态，跳过更新: orderId=%s, status=%s", in.OrderId, orderInfo.Status)
		return &seckill.UpdateOrderStatusResponse{
			Success: true,
			Message: "订单状态已是终态",
		}, nil
	}

	// 更新状态
	orderInfo.Status = in.Status
	if err := l.svcCtx.Redis.SetOrderInfo(l.ctx, in.OrderId, orderInfo, OrderStatusTTL); err != nil {
		l.Logger.Errorf("更新订单状态失败: orderId=%s, status=%s, err=%v", in.OrderId, in.Status, err)
		return &seckill.UpdateOrderStatusResponse{
			Success: false,
			Message: "更新订单状态失败",
		}, nil
	}

	l.Logger.Infof("订单状态更新成功: orderId=%s, newStatus=%s", in.OrderId, in.Status)
	return &seckill.UpdateOrderStatusResponse{
		Success: true,
		Message: "订单状态更新成功",
	}, nil
}
