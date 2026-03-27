package logic

import (
	"context"

	"seckill-mall/seckill-service/internal/redis"
	"seckill-mall/seckill-service/internal/svc"
	"seckill-mall/seckill-service/seckill"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetSeckillResultLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetSeckillResultLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetSeckillResultLogic {
	return &GetSeckillResultLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetSeckillResult 查询秒杀结果
// 通过 orderId 从 Redis 查询完整订单信息
func (l *GetSeckillResultLogic) GetSeckillResult(in *seckill.SeckillResultRequest) (*seckill.SeckillResultResponse, error) {
	// 参数校验
	if in.OrderId == "" {
		return &seckill.SeckillResultResponse{
			Success: false,
			Message: "订单号不能为空",
		}, nil
	}

	// 从 Redis 获取完整订单信息
	orderInfo, err := l.svcCtx.Redis.GetOrderInfo(l.ctx, in.OrderId)
	if err != nil {
		l.Logger.Errorf("查询订单信息失败: orderId=%s, err=%v", in.OrderId, err)
		return &seckill.SeckillResultResponse{
			Success: false,
			OrderId: in.OrderId,
			Message: "查询订单信息失败",
		}, nil
	}

	// 订单不存在或已过期（TTL 过期后返回 nil）
	if orderInfo == nil {
		return &seckill.SeckillResultResponse{
			Success: false,
			OrderId: in.OrderId,
			Message: "订单不存在或已过期",
		}, nil
	}

	// 根据状态返回结果
	switch orderInfo.Status {
	case redis.OrderStatusSuccess:
		productName := orderInfo.ProductName
		if productName == "" {
			productName = "秒杀商品"
		}
		return &seckill.SeckillResultResponse{
			Success:     true,
			OrderId:     in.OrderId,
			ProductId:   orderInfo.ProductId,
			ProductName: productName,
			Quantity:    orderInfo.Quantity,
			Amount:      orderInfo.Amount,
			Status:      orderInfo.Status,
			Message:     "订单处理成功",
		}, nil

	case redis.OrderStatusPending:
		return &seckill.SeckillResultResponse{
			Success: false,
			OrderId: in.OrderId,
			Status:  orderInfo.Status,
			Message: "订单正在处理中，请稍后查询",
		}, nil

	case redis.OrderStatusFailed:
		return &seckill.SeckillResultResponse{
			Success: false,
			OrderId: in.OrderId,
			Status:  orderInfo.Status,
			Message: "订单处理失败",
		}, nil

	default:
		return &seckill.SeckillResultResponse{
			Success: false,
			OrderId: in.OrderId,
			Message: "未知的订单状态",
		}, nil
	}
}
