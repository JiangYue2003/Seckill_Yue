package logic

import (
	"context"
	"time"

	"seckill-mall/common/seckill"
	"seckill-mall/common/utils"
	"seckill-mall/seckill-service/internal/metrics"
	"seckill-mall/seckill-service/internal/mq"
	"seckill-mall/seckill-service/internal/redis"
	"seckill-mall/seckill-service/internal/svc"

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

	defaultQuotaBatchSize       = 200
	defaultQuotaLowWatermark    = 40
	defaultQuotaLeaseTTLSeconds = 15
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
	start := time.Now()
	resultLabel := "system_error"
	defer func() {
		metrics.SeckillRequestsTotal.WithLabelValues(resultLabel).Inc()
		metrics.SeckillRequestDurationSeconds.WithLabelValues(resultLabel).Observe(time.Since(start).Seconds())
	}()

	// ========== 参数校验 ==========
	if in.UserId <= 0 {
		resultLabel = "invalid_user"
		return &seckill.SeckillResponse{
			Success: false,
			Code:    SeckillCodeSystemError,
			Message: "用户ID无效",
		}, nil
	}
	if in.SeckillProductId <= 0 {
		resultLabel = "invalid_product"
		return &seckill.SeckillResponse{
			Success: false,
			Code:    SeckillCodeSystemError,
			Message: "秒杀商品ID无效",
		}, nil
	}

	exists, filterErr := l.svcCtx.MayExistSeckillProduct(l.ctx, in.SeckillProductId)
	if filterErr != nil {
		l.Logger.Errorf("Bloom fallback verify failed (fail-open): seckillProductId=%d, err=%v", in.SeckillProductId, filterErr)
	}
	if !exists {
		resultLabel = "product_not_found"
		return &seckill.SeckillResponse{
			Success: false,
			Code:    SeckillCodeSystemError,
			Message: "秒杀活动不存在或已过期",
		}, nil
	}

	// 默认购买数量为1
	quantity := int64(1)
	if in.Quantity > 0 {
		quantity = in.Quantity
	}

	// 从 Redis 获取秒杀商品信息（含时间范围）
	productId, seckillPrice, _, startTime, endTime := l.getSeckillProductInfo(l.ctx, in.SeckillProductId)
	if productId == 0 && seckillPrice == 0 {
		resultLabel = "product_not_found"
		return &seckill.SeckillResponse{
			Success: false,
			Code:    SeckillCodeSystemError,
			Message: "秒杀活动不存在或已过期",
		}, nil
	}

	// 活动时间前置校验：减少不必要的 Redis Lua 执行
	nowUnix := time.Now().Unix()
	if startTime > 0 && nowUnix < startTime {
		resultLabel = "not_started"
		return &seckill.SeckillResponse{
			Success: false,
			Code:    SeckillCodeNotStarted,
			Message: "秒杀活动尚未开始",
		}, nil
	}
	if endTime > 0 && nowUnix > endTime {
		resultLabel = "ended"
		return &seckill.SeckillResponse{
			Success: false,
			Code:    SeckillCodeEnded,
			Message: "秒杀活动已结束",
		}, nil
	}

	quotaEnabled := l.svcCtx.Config.LocalQuota.Enabled
	if quotaEnabled {
		// 配额模式：本地计数器从 0 起步，仅在不足时领取，避免每请求打 Redis
		l.svcCtx.Redis.GetOrInitLocalStockWithValue(in.SeckillProductId, 0)
		if l.svcCtx.Redis.GetLocalStock(in.SeckillProductId) <= 0 {
			if allocated, _, err := l.svcCtx.Redis.EnsureQuota(
				l.ctx,
				in.SeckillProductId,
				l.svcCtx.InstanceID,
				l.quotaBatchSize(),
				l.quotaLeaseTTLSeconds(),
			); err != nil {
				metrics.SeckillQuotaAllocateTotal.WithLabelValues("failed").Inc()
				l.Logger.Errorf("initial quota allocate failed: spid=%d, err=%v", in.SeckillProductId, err)
			} else if allocated > 0 {
				metrics.SeckillQuotaAllocateTotal.WithLabelValues("ok").Inc()
				l.svcCtx.Redis.IncrLocalStock(in.SeckillProductId, allocated)
			} else {
				metrics.SeckillQuotaAllocateTotal.WithLabelValues("empty").Inc()
			}
		}
	} else {
		// 旧模式：懒初始化本地库存计数器（首次请求时从 Redis 同步库存）
		counter, _ := l.svcCtx.Redis.GetOrInitLocalStock(l.ctx, in.SeckillProductId)
		// 兜底：服务长期运行后，若本地计数器耗尽但 Redis 仍有库存（例如压测复跑重置了 Redis），
		// 则按 Redis 权威值回填，避免“必须重启服务”才能恢复。
		if counter != nil && counter.Load() <= 0 {
			if stock, stockErr := l.svcCtx.Redis.GetStock(l.ctx, in.SeckillProductId); stockErr == nil && stock > 0 {
				counter.Store(stock)
			}
		}
	}

	// 本地原子计数器预过滤：库存耗尽时快速拒绝，不打 Redis（纳秒级）
	remaining := l.svcCtx.Redis.DecrLocalStock(in.SeckillProductId, quantity)
	if remaining < 0 {
		l.svcCtx.Redis.IncrLocalStock(in.SeckillProductId, quantity)

		// 配额模式：尝试快速补一批本地配额再重试一次
		if quotaEnabled {
			allocated, _, err := l.svcCtx.Redis.EnsureQuota(
				l.ctx,
				in.SeckillProductId,
				l.svcCtx.InstanceID,
				l.quotaBatchSize(),
				l.quotaLeaseTTLSeconds(),
			)
			if err != nil {
				metrics.SeckillQuotaAllocateTotal.WithLabelValues("failed").Inc()
				l.Logger.Errorf("quota allocate failed: spid=%d, err=%v", in.SeckillProductId, err)
			} else if allocated > 0 {
				metrics.SeckillQuotaAllocateTotal.WithLabelValues("ok").Inc()
				l.svcCtx.Redis.IncrLocalStock(in.SeckillProductId, allocated)
				remaining = l.svcCtx.Redis.DecrLocalStock(in.SeckillProductId, quantity)
				if remaining < 0 {
					l.svcCtx.Redis.IncrLocalStock(in.SeckillProductId, quantity)
				}
			} else {
				metrics.SeckillQuotaAllocateTotal.WithLabelValues("empty").Inc()
			}
		}
	}

	if remaining < 0 {
		metrics.SeckillLocalStockRejectTotal.Inc()
		resultLabel = "sold_out_local"
		return &seckill.SeckillResponse{
			Success: false,
			Code:    SeckillCodeSoldOut,
			Message: "商品已售罄",
		}, nil
	}

	// 生成订单号
	orderId := utils.GenerateOrderId(OrderIdPrefix)
	amount := seckillPrice * quantity

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
		OrderStatusTTL:   OrderStatusTTL,
	}

	// 执行 Redis Lua 脚本（原子性操作）
	var result *redis.SeckillResult
	var err error
	if quotaEnabled {
		result, err = l.svcCtx.Redis.DoSeckillWithQuota(l.ctx, seckillReq, l.svcCtx.InstanceID)
	} else {
		result, err = l.svcCtx.Redis.DoSeckill(l.ctx, seckillReq)
	}
	if err != nil {
		// Lua 执行失败时，回滚本地预扣计数，避免本地计数与 Redis 权威库存长期偏移。
		l.svcCtx.Redis.IncrLocalStock(in.SeckillProductId, quantity)
		l.Logger.Errorf("执行秒杀失败: userId=%d, seckillProductId=%d, err=%v",
			in.UserId, in.SeckillProductId, err)
		resultLabel = "redis_error"
		return &seckill.SeckillResponse{
			Success: false,
			Code:    SeckillCodeSystemError,
			Message: "系统繁忙，请稍后重试",
		}, nil
	}

	// ========== 处理秒杀结果 ==========
	switch result.Code {
	case redis.LuaResultNotStarted:
		l.svcCtx.Redis.IncrLocalStock(in.SeckillProductId, quantity)
		resultLabel = "not_started"
		return &seckill.SeckillResponse{
			Success: false,
			Code:    SeckillCodeNotStarted,
			Message: "秒杀活动尚未开始",
		}, nil

	case redis.LuaResultEnded:
		l.svcCtx.Redis.IncrLocalStock(in.SeckillProductId, quantity)
		resultLabel = "ended"
		return &seckill.SeckillResponse{
			Success: false,
			Code:    SeckillCodeEnded,
			Message: "秒杀活动已结束",
		}, nil

	case redis.LuaResultAlreadyBought:
		l.svcCtx.Redis.IncrLocalStock(in.SeckillProductId, quantity)
		// 用户已购买过该秒杀商品
		l.Logger.Debugf("用户已购买过该商品: userId=%d, seckillProductId=%d",
			in.UserId, in.SeckillProductId)
		resultLabel = "already_purchased"
		return &seckill.SeckillResponse{
			Success: false,
			Code:    SeckillCodeAlreadyPurchased,
			Message: "您已购买过该商品，每人限购一次",
		}, nil

	case redis.LuaResultStockNotEnough:
		// Redis 是权威来源，说明本地计数器与 Redis 存在短暂偏差，回滚本地计数
		l.svcCtx.Redis.IncrLocalStock(in.SeckillProductId, quantity)
		l.Logger.Debugf("秒杀库存不足: userId=%d, seckillProductId=%d, remainingStock=%d",
			in.UserId, in.SeckillProductId, result.Stock)
		resultLabel = "sold_out"
		return &seckill.SeckillResponse{
			Success: false,
			Code:    SeckillCodeSoldOut,
			Message: "商品已售罄",
		}, nil

	case redis.LuaResultSuccess:
		if quotaEnabled && remaining <= l.quotaLowWatermark() {
			go func(spid int64) {
				allocated, _, allocErr := l.svcCtx.Redis.EnsureQuota(
					context.Background(),
					spid,
					l.svcCtx.InstanceID,
					l.quotaBatchSize(),
					l.quotaLeaseTTLSeconds(),
				)
				if allocErr != nil {
					metrics.SeckillQuotaAllocateTotal.WithLabelValues("failed").Inc()
					return
				}
				if allocated > 0 {
					metrics.SeckillQuotaAllocateTotal.WithLabelValues("ok").Inc()
					l.svcCtx.Redis.IncrLocalStock(spid, allocated)
				} else {
					metrics.SeckillQuotaAllocateTotal.WithLabelValues("empty").Inc()
				}
			}(in.SeckillProductId)
		}

		// 秒杀成功，异步发送 RabbitMQ 消息（不阻塞用户响应）
		// 注意：异步模式下 MQ 投递失败不再触发同步回滚，依赖 UserPreemptTTL（300s）自动兜底
		l.Logger.Debugf("秒杀成功，准备异步发送RabbitMQ消息: userId=%d, seckillProductId=%d, orderId=%s",
			in.UserId, in.SeckillProductId, orderId)

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

		// 发送延迟兜底消息（5分钟后检查订单是否仍 pending，非致命）
		if delayErr := l.svcCtx.AsyncProducer.SendDelayOrder(l.ctx, seckillMsg); delayErr != nil {
			metrics.SeckillMQEnqueueTotal.WithLabelValues("delay", "failed").Inc()
			l.Logger.Errorf("发送延迟检查消息失败（非致命，TTL兜底）: orderId=%s, err=%v", orderId, delayErr)
		} else {
			metrics.SeckillMQEnqueueTotal.WithLabelValues("delay", "ok").Inc()
		}

		// 异步投递消息，立即返回（不等待 MQ 确认）
		if err := l.svcCtx.AsyncProducer.SendAsync(l.ctx, seckillMsg); err != nil {
			metrics.SeckillMQEnqueueTotal.WithLabelValues("async", "failed").Inc()
			// 缓冲区满，说明系统严重过载（Channel 积压超过阈值）
			// 此时 Lua 已扣减库存，但无法异步投递消息，降级处理：
			// 不触发库存回滚（避免雪崩），依赖 UserPreemptTTL 300s 自然归还
			l.Logger.Errorf("异步MQ缓冲区满，降级处理（库存依赖TTL自然归还）: orderId=%s, err=%v", orderId, err)
		} else {
			metrics.SeckillMQEnqueueTotal.WithLabelValues("async", "ok").Inc()
		}

		l.Logger.Debugf("秒杀成功: userId=%d, seckillProductId=%d, orderId=%s",
			in.UserId, in.SeckillProductId, orderId)
		resultLabel = "success"

		return &seckill.SeckillResponse{
			Success: true,
			Code:    SeckillCodeSuccess,
			Message: "抢购成功，订单正在处理中",
			OrderId: orderId,
		}, nil

	default:
		l.svcCtx.Redis.IncrLocalStock(in.SeckillProductId, quantity)
		l.Logger.Errorf("未知的秒杀结果: code=%d", result.Code)
		resultLabel = "unknown_result"
		return &seckill.SeckillResponse{
			Success: false,
			Code:    SeckillCodeSystemError,
			Message: "系统繁忙，请稍后重试",
		}, nil
	}
}

func (l *SeckillLogic) quotaBatchSize() int64 {
	if l.svcCtx.Config.LocalQuota.BatchSize > 0 {
		return l.svcCtx.Config.LocalQuota.BatchSize
	}
	return defaultQuotaBatchSize
}

func (l *SeckillLogic) quotaLowWatermark() int64 {
	if l.svcCtx.Config.LocalQuota.LowWatermark > 0 {
		return l.svcCtx.Config.LocalQuota.LowWatermark
	}
	return defaultQuotaLowWatermark
}

func (l *SeckillLogic) quotaLeaseTTLSeconds() int64 {
	if l.svcCtx.Config.LocalQuota.LeaseTTLSeconds > 0 {
		return l.svcCtx.Config.LocalQuota.LeaseTTLSeconds
	}
	return defaultQuotaLeaseTTLSeconds
}

// getSeckillProductInfo 从 Redis 获取秒杀商品信息（productId, seckillPrice, productName, startTime, endTime）
// 实际生产环境应在秒杀开始前将商品信息预加载到 Redis
func (l *SeckillLogic) getSeckillProductInfo(ctx context.Context, seckillProductId int64) (int64, int64, string, int64, int64) {
	meta, err := l.svcCtx.GetSeckillProductMeta(ctx, seckillProductId)
	if err != nil {
		l.Logger.Errorf("获取秒杀商品元数据失败: seckillProductId=%d, err=%v", seckillProductId, err)
		return 0, 0, "", 0, 0
	}
	if meta == nil {
		return 0, 0, "", 0, 0
	}
	return meta.ProductId, meta.SeckillPrice, meta.ProductName, meta.StartTime, meta.EndTime
}
