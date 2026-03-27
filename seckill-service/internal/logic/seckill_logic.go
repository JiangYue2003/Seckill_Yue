package logic

import (
	"context"
	"time"

	"seckill-mall/common/utils"
	"seckill-mall/seckill-service/internal/mq"
	"seckill-mall/seckill-service/internal/redis"
	"seckill-mall/seckill-service/internal/svc"
	"seckill-mall/seckill-service/seckill"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	// 秒杀结果码
	SeckillCodeSuccess          = "SUCCESS"
	SeckillCodeSoldOut          = "SOLD_OUT"
	SeckillCodeAlreadyPurchased = "ALREADY_PURCHASED"
	SeckillCodeNotStarted       = "SECKILL_NOT_STARTED"
	SeckillCodeEnded            = "SECKILL_ENDED"
	SeckillCodeSystemError      = "SYSTEM_ERROR"

	// 秒杀订单号前缀
	OrderIdPrefix = "S"

	// 订单状态过期时间（秒）
	OrderStatusTTL = 86400 // 24小时，用于 seckill:order:{orderId}

	// 用户预占过期时间（秒）：MQ 处理超时兜底，消息失败时库存自动归还 Redis
	// 5分钟 = 300秒，足够 MQ 正常重试 3 次（每次 < 2 分钟）
	UserPreemptTTL = 300
)

type SeckillLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewSeckillLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SeckillLogic {
	return &SeckillLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// Seckill 秒杀下单（核心接口）
// 职责边界：
// 1. 只操作 Redis 和 RabbitMQ，绝对禁止直接连接 MySQL
// 2. 通过 Redis Lua 脚本原子性完成"校验库存+用户防重+预扣减库存"
// 3. 成功后，将抢购成功消息 Push 到 RabbitMQ，立即返回响应
func (l *SeckillLogic) Seckill(in *seckill.SeckillRequest) (*seckill.SeckillResponse, error) {
	// ========== 参数校验 ==========
	if in.UserId <= 0 {
		return &seckill.SeckillResponse{
			Success: false,
			Code:    SeckillCodeSystemError,
			Message: "用户ID无效",
		}, nil
	}
	if in.SeckillProductId <= 0 {
		return &seckill.SeckillResponse{
			Success: false,
			Code:    SeckillCodeSystemError,
			Message: "秒杀商品ID无效",
		}, nil
	}

	// 默认购买数量为1
	quantity := int64(1)
	if in.Quantity > 0 {
		quantity = in.Quantity
	}

	// 从 Redis 获取秒杀商品信息（含时间范围）
	productId, seckillPrice, productName, startTime, endTime := l.getSeckillProductInfo(l.ctx, in.SeckillProductId)
	if productId == 0 && seckillPrice == 0 {
		return &seckill.SeckillResponse{
			Success: false,
			Code:    SeckillCodeSystemError,
			Message: "秒杀活动不存在或已过期",
		}, nil
	}

	// 生成订单号
	orderId := utils.GenerateOrderId(OrderIdPrefix)

	// 秒杀 Lua 脚本执行（携带时间校验参数）
	// 注意：TTL 用于用户预占 Key，设置为 5 分钟（UserPreemptTTL）
	// MQ 处理超时或失败时，TTL 自动释放 Redis 库存，无需手动回滚
	seckillReq := &redis.SeckillRequest{
		SeckillProductId: in.SeckillProductId,
		UserId:           in.UserId,
		Quantity:         quantity,
		OrderId:          orderId,
		TTL:              UserPreemptTTL,
		StartTime:        startTime,
		EndTime:          endTime,
	}

	// 执行 Redis Lua 脚本（原子性操作）
	result, err := l.svcCtx.Redis.DoSeckill(l.ctx, seckillReq)
	if err != nil {
		l.Logger.Errorf("执行秒杀失败: userId=%d, seckillProductId=%d, err=%v",
			in.UserId, in.SeckillProductId, err)
		return &seckill.SeckillResponse{
			Success: false,
			Code:    SeckillCodeSystemError,
			Message: "系统繁忙，请稍后重试",
		}, nil
	}

	// ========== 处理秒杀结果 ==========
	switch result.Code {
	case redis.LuaResultNotStarted:
		return &seckill.SeckillResponse{
			Success: false,
			Code:    SeckillCodeNotStarted,
			Message: "秒杀活动尚未开始",
		}, nil

	case redis.LuaResultEnded:
		return &seckill.SeckillResponse{
			Success: false,
			Code:    SeckillCodeEnded,
			Message: "秒杀活动已结束",
		}, nil

	case redis.LuaResultAlreadyBought:
		// 用户已购买过该秒杀商品
		l.Logger.Infof("用户已购买过该商品: userId=%d, seckillProductId=%d",
			in.UserId, in.SeckillProductId)
		return &seckill.SeckillResponse{
			Success: false,
			Code:    SeckillCodeAlreadyPurchased,
			Message: "您已购买过该商品，每人限购一次",
		}, nil

	case redis.LuaResultStockNotEnough:
		// 库存不足
		l.Logger.Infof("秒杀库存不足: userId=%d, seckillProductId=%d, remainingStock=%d",
			in.UserId, in.SeckillProductId, result.Stock)
		return &seckill.SeckillResponse{
			Success: false,
			Code:    SeckillCodeSoldOut,
			Message: "商品已售罄",
		}, nil

	case redis.LuaResultSuccess:
		// 秒杀成功，发送 RabbitMQ 消息
		l.Logger.Infof("秒杀成功，准备发送RabbitMQ消息: userId=%d, seckillProductId=%d, orderId=%s",
			in.UserId, in.SeckillProductId, orderId)

		amount := seckillPrice * quantity

		// 构建秒杀成功消息
		seckillMsg := &mq.SeckillOrderMessage{
			OrderId:          orderId,
			UserId:           in.UserId,
			SeckillProductId: in.SeckillProductId,
			ProductId:        productId,
			Quantity:         quantity,
			SeckillPrice:     seckillPrice,
			Amount:           amount,
			CreatedAt:        time.Now().Unix(),
		}

		// 发送 RabbitMQ 消息
		if err := l.svcCtx.Producer.SendSeckillOrder(l.ctx, seckillMsg); err != nil {
			// RabbitMQ 发送失败，回滚 Redis 库存
			l.Logger.Errorf("发送RabbitMQ消息失败，回滚库存: orderId=%s, err=%v", orderId, err)
			if rollbackErr := l.svcCtx.Redis.RollbackStock(l.ctx, in.SeckillProductId, quantity); rollbackErr != nil {
				l.Logger.Errorf("回滚库存失败: seckillProductId=%d, quantity=%d, err=%v",
					in.SeckillProductId, quantity, rollbackErr)
			}
			// 删除用户购买记录
			if delErr := l.svcCtx.Redis.DeleteUserKey(l.ctx, in.SeckillProductId, in.UserId); delErr != nil {
				l.Logger.Errorf("删除用户购买记录失败: userId=%d, seckillProductId=%d, err=%v",
					in.UserId, in.SeckillProductId, delErr)
			}

			return &seckill.SeckillResponse{
				Success: false,
				Code:    SeckillCodeSystemError,
				Message: "系统繁忙，请稍后重试",
			}, nil
		}

		// 设置订单状态为处理中（存储完整订单信息用于后续查询）
		if setErr := l.svcCtx.Redis.SetOrderInfo(l.ctx, orderId, &redis.OrderInfo{
			Status:      OrderStatusPending,
			ProductId:   productId,
			Quantity:    quantity,
			Amount:      amount,
			ProductName: productName,
		}, OrderStatusTTL); setErr != nil {
			l.Logger.Errorf("设置订单状态失败: orderId=%s, err=%v", orderId, setErr)
		}

		l.Logger.Infof("秒杀成功: userId=%d, seckillProductId=%d, orderId=%s",
			in.UserId, in.SeckillProductId, orderId)

		return &seckill.SeckillResponse{
			Success: true,
			Code:    SeckillCodeSuccess,
			Message: "抢购成功，订单正在处理中",
			OrderId: orderId,
		}, nil

	default:
		l.Logger.Errorf("未知的秒杀结果: code=%d", result.Code)
		return &seckill.SeckillResponse{
			Success: false,
			Code:    SeckillCodeSystemError,
			Message: "系统繁忙，请稍后重试",
		}, nil
	}
}

// getSeckillProductInfo 从 Redis 获取秒杀商品信息（productId, seckillPrice, productName, startTime, endTime）
// 实际生产环境应在秒杀开始前将商品信息预加载到 Redis
func (l *SeckillLogic) getSeckillProductInfo(ctx context.Context, seckillProductId int64) (int64, int64, string, int64, int64) {
	productId, seckillPrice, productName, startTime, endTime, err := l.svcCtx.Redis.GetSeckillProductInfo(ctx, seckillProductId)
	if err != nil {
		l.Logger.Errorf("获取秒杀商品信息失败: seckillProductId=%d, err=%v", seckillProductId, err)
		return 0, 0, "", 0, 0
	}
	return productId, seckillPrice, productName, startTime, endTime
}
