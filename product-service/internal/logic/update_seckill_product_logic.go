package logic

import (
	"context"
	"errors"
	commonpb "seckill-mall/common/common"
	"time"

	"seckill-mall/common/product"
	"seckill-mall/product-service/internal/model"
	"seckill-mall/product-service/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type UpdateSeckillProductLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUpdateSeckillProductLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateSeckillProductLogic {
	return &UpdateSeckillProductLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// UpdateSeckillProduct 更新秒杀商品
func (l *UpdateSeckillProductLogic) UpdateSeckillProduct(in *product.UpdateSeckillProductRequest) (*commonpb.BoolResponse, error) {
	// 参数校验
	if in.Id <= 0 {
		return nil, errors.New("秒杀商品ID无效")
	}

	// 查询秒杀商品
	seckillProduct, err := l.svcCtx.SeckillProductModel.FindOneById(l.ctx, in.Id)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil, errors.New("秒杀商品不存在")
		}
		l.Logger.Errorf("查询秒杀商品失败: %v", err)
		return nil, errors.New("系统错误，请稍后重试")
	}

	// 更新字段
	if in.SeckillPrice > 0 {
		seckillProduct.SeckillPrice = in.SeckillPrice
	}
	if in.SeckillStock >= 0 {
		seckillProduct.SeckillStock = int(in.SeckillStock)
	}
	if in.StartTime > 0 {
		seckillProduct.StartTime = in.StartTime
	}
	if in.EndTime > 0 {
		seckillProduct.EndTime = in.EndTime
	}
	if in.PerLimit > 0 {
		seckillProduct.PerLimit = int(in.PerLimit)
	}
	if in.Status > 0 {
		seckillProduct.Status = in.Status
	}

	seckillProduct.UpdatedAt = time.Now().Unix()

	// 保存更新
	if err := l.svcCtx.SeckillProductModel.Update(l.ctx, seckillProduct); err != nil {
		l.Logger.Errorf("更新秒杀商品失败: %v", err)
		return nil, errors.New("更新秒杀商品失败，请稍后重试")
	}

	// ========== 同步变更到 Redis（供 Seckill-Service 使用）==========
	now := time.Now().Unix()
	if l.svcCtx.SeckillRedis != nil {
		// 计算 TTL：秒杀结束时间 + 1小时缓冲
		ttlSeconds := (seckillProduct.EndTime - now) + 3600
		if ttlSeconds < 3600 {
			ttlSeconds = 3600
		}

		// 如果库存变更，同步库存
		if in.SeckillStock >= 0 {
			if redisErr := l.svcCtx.SeckillRedis.UpdateSeckillStock(
				l.ctx, seckillProduct.ID, int64(seckillProduct.SeckillStock), ttlSeconds,
			); redisErr != nil {
				l.Logger.Errorf("同步秒杀库存到 Redis 失败: seckillProductId=%d, err=%v", seckillProduct.ID, redisErr)
			}
		}

		// 如果秒杀价格变更，同步商品信息
		if in.SeckillPrice > 0 {
			if redisErr := l.svcCtx.SeckillRedis.UpdateSeckillInfo(
				l.ctx, seckillProduct.ID, seckillProduct.ProductId, seckillProduct.SeckillPrice, ttlSeconds,
			); redisErr != nil {
				l.Logger.Errorf("同步秒杀价格到 Redis 失败: seckillProductId=%d, err=%v", seckillProduct.ID, redisErr)
			}
		}
	}

	l.Logger.Infof("秒杀商品更新成功: seckillProductId=%d", in.Id)

	return &commonpb.BoolResponse{
		Success: true,
		Message: "更新成功",
	}, nil
}
