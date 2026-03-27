package logic

import (
	"context"
	"time"

	"seckill-mall/seckill-service/internal/redis"
	"seckill-mall/seckill-service/internal/svc"
	"seckill-mall/seckill-service/seckill"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	OrderStatusPending  = "pending"
	OrderStatusSuccess  = "success"
	OrderStatusFailed   = "failed"
	OrderStatusNotStart = "not_started"
	OrderStatusEnded    = "ended"
	OrderStatusSoldOut  = "sold_out"
)

type GetSeckillStatusLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetSeckillStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetSeckillStatusLogic {
	return &GetSeckillStatusLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetSeckillStatus 查询秒杀状态（轮询接口）
// 职责：
//  1. 检查秒杀活动是否在有效时间范围内
//  2. 检查用户是否已购买（通过 userKey 判断）
//  3. 查询订单处理状态（pending/success/failed/not_started/ended/sold_out）
//  4. 返回完整的订单信息（order_id、product_id、quantity）
func (l *GetSeckillStatusLogic) GetSeckillStatus(in *seckill.SeckillStatusRequest) (*seckill.SeckillStatusResponse, error) {
	// ========== 参数校验 ==========
	if in.UserId <= 0 || in.SeckillProductId <= 0 {
		return &seckill.SeckillStatusResponse{
			Status: OrderStatusFailed,
		}, nil
	}

	now := time.Now().Unix()

	// ========== 检查秒杀活动时间范围 ==========
	_, _, _, startTime, endTime, err := l.svcCtx.Redis.GetSeckillProductInfo(l.ctx, in.SeckillProductId)
	if err != nil {
		l.Logger.Errorf("获取秒杀商品信息失败: seckillProductId=%d, err=%v", in.SeckillProductId, err)
		return &seckill.SeckillStatusResponse{
			Status: OrderStatusPending,
		}, nil
	}

	if startTime > 0 && now < startTime {
		return &seckill.SeckillStatusResponse{
			Status: OrderStatusNotStart,
		}, nil
	}
	if endTime > 0 && now > endTime {
		return &seckill.SeckillStatusResponse{
			Status: OrderStatusEnded,
		}, nil
	}

	// ========== 检查用户购买记录 ==========
	// 通过 Redis userKey 判断用户是否已参与过该秒杀
	userKey := redis.KeyPrefixSeckillUser + redis.FormatSeckillUserKey(in.SeckillProductId, in.UserId)
	exists, err := l.svcCtx.Redis.CheckUserKeyExists(l.ctx, userKey)
	if err != nil {
		l.Logger.Errorf("检查用户购买记录失败: userId=%d, seckillProductId=%d, err=%v",
			in.UserId, in.SeckillProductId, err)
		return &seckill.SeckillStatusResponse{
			Status: OrderStatusPending,
		}, nil
	}

	// 用户未参与过秒杀（userKey 不存在），说明还在排队或已售罄
	if !exists {
		// 检查秒杀商品是否还有库存
		stock, stockErr := l.svcCtx.Redis.GetStock(l.ctx, in.SeckillProductId)
		if stockErr != nil {
			l.Logger.Errorf("查询库存失败: seckillProductId=%d, err=%v", in.SeckillProductId, stockErr)
			return &seckill.SeckillStatusResponse{
				Status: OrderStatusPending,
			}, nil
		}

		// 库存为0说明已售罄
		if stock == 0 {
			return &seckill.SeckillStatusResponse{
				Status: OrderStatusSoldOut,
			}, nil
		}

		// 库存还有，用户还未购买，说明在排队中
		return &seckill.SeckillStatusResponse{
			Status: OrderStatusPending,
		}, nil
	}

	// ========== 用户已参与过秒杀，查询订单状态 ==========
	// userKey 的值就是 orderId
	orderId, err := l.svcCtx.Redis.GetUserOrderId(l.ctx, userKey)
	if err != nil {
		l.Logger.Errorf("获取用户订单号失败: userId=%d, seckillProductId=%d, err=%v",
			in.UserId, in.SeckillProductId, err)
		return &seckill.SeckillStatusResponse{
			Status: OrderStatusPending,
		}, nil
	}

	if orderId == "" {
		return &seckill.SeckillStatusResponse{
			Status: OrderStatusFailed,
		}, nil
	}

	// 从 Redis 获取订单信息
	orderInfo, err := l.svcCtx.Redis.GetOrderInfo(l.ctx, orderId)
	if err != nil {
		l.Logger.Errorf("获取订单信息失败: orderId=%s, err=%v", orderId, err)
		return &seckill.SeckillStatusResponse{
			Status:  OrderStatusSuccess,
			OrderId: orderId,
		}, nil
	}

	// 根据状态返回
	switch orderInfo.Status {
	case OrderStatusSuccess:
		return &seckill.SeckillStatusResponse{
			Status:    OrderStatusSuccess,
			OrderId:   orderId,
			ProductId: orderInfo.ProductId,
			Quantity:  orderInfo.Quantity,
		}, nil
	case OrderStatusFailed:
		return &seckill.SeckillStatusResponse{
			Status:  OrderStatusFailed,
			OrderId: orderId,
		}, nil
	default:
		// pending 或其他状态，说明订单处理中
		return &seckill.SeckillStatusResponse{
			Status:    OrderStatusPending,
			OrderId:   orderId,
			ProductId: orderInfo.ProductId,
			Quantity:  orderInfo.Quantity,
		}, nil
	}
}
