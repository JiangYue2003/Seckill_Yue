package logic

import (
	"context"

	"seckill-mall/seckill-service/internal/redis"
	"seckill-mall/seckill-service/internal/svc"
	"seckill-mall/seckill-service/seckill"

	"github.com/zeromicro/go-zero/core/logx"
)

type CompensateFailedOrderLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCompensateFailedOrderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CompensateFailedOrderLogic {
	return &CompensateFailedOrderLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// CompensateFailedOrder 原子执行超时失败补偿：
// pending -> failed + 回补 Redis 库存 + 释放 userKey
func (l *CompensateFailedOrderLogic) CompensateFailedOrder(in *seckill.CompensateFailedOrderRequest) (*seckill.CompensateFailedOrderResponse, error) {
	if in.OrderId == "" {
		return &seckill.CompensateFailedOrderResponse{
			Success: false,
			Message: "订单号不能为空",
			Result:  "invalid_request",
		}, nil
	}
	if in.SeckillProductId <= 0 || in.UserId <= 0 || in.Quantity <= 0 {
		return &seckill.CompensateFailedOrderResponse{
			Success: false,
			Message: "参数非法",
			Result:  "invalid_request",
		}, nil
	}

	code, stock, err := l.svcCtx.Redis.CompensateFailedOrder(
		l.ctx,
		in.OrderId,
		in.SeckillProductId,
		in.UserId,
		in.Quantity,
		OrderStatusTTL,
	)
	if err != nil {
		l.Logger.Errorf("failed compensation exec error: orderId=%s, spid=%d, userId=%d, reason=%s, err=%v",
			in.OrderId, in.SeckillProductId, in.UserId, in.Reason, err)
		return &seckill.CompensateFailedOrderResponse{
			Success: false,
			Message: "执行补偿失败",
			Result:  "compensate_error",
		}, nil
	}

	switch code {
	case redis.CompensateResultCompensated:
		l.Logger.Infof("failed compensation done: orderId=%s, spid=%d, userId=%d, quantity=%d, stock=%d, reason=%s",
			in.OrderId, in.SeckillProductId, in.UserId, in.Quantity, stock, in.Reason)
		return &seckill.CompensateFailedOrderResponse{
			Success: true,
			Message: "补偿执行成功",
			Result:  "compensated",
		}, nil
	case redis.CompensateResultAlreadyFailed:
		return &seckill.CompensateFailedOrderResponse{
			Success: true,
			Message: "订单已是 failed，幂等跳过",
			Result:  "idempotent_failed",
		}, nil
	case redis.CompensateResultAlreadySuccess:
		return &seckill.CompensateFailedOrderResponse{
			Success: true,
			Message: "订单已是 success，跳过补偿",
			Result:  "already_success",
		}, nil
	case redis.CompensateResultOrderNotFound:
		return &seckill.CompensateFailedOrderResponse{
			Success: true,
			Message: "订单不存在或已过期",
			Result:  "order_not_found",
		}, nil
	default:
		return &seckill.CompensateFailedOrderResponse{
			Success: false,
			Message: "订单状态非法，拒绝补偿",
			Result:  "invalid_status",
		}, nil
	}
}
