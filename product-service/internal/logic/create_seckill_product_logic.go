package logic

import (
	"context"
	"errors"
	"time"

	"seckill-mall/product-service/internal/model"
	"seckill-mall/product-service/internal/model/entity"
	"seckill-mall/product-service/internal/svc"
	"seckill-mall/product-service/product"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreateSeckillProductLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateSeckillProductLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateSeckillProductLogic {
	return &CreateSeckillProductLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// CreateSeckillProduct 创建秒杀商品
func (l *CreateSeckillProductLogic) CreateSeckillProduct(in *product.CreateSeckillProductRequest) (*product.SeckillProductInfo, error) {
	// 参数校验
	if in.ProductId <= 0 {
		return nil, errors.New("商品ID无效")
	}
	if in.SeckillPrice <= 0 {
		return nil, errors.New("秒杀价格必须大于0")
	}
	if in.SeckillStock <= 0 {
		return nil, errors.New("秒杀库存必须大于0")
	}
	if in.StartTime <= 0 || in.EndTime <= 0 {
		return nil, errors.New("秒杀时间无效")
	}
	if in.EndTime <= in.StartTime {
		return nil, errors.New("结束时间必须大于开始时间")
	}

	// 查询关联商品
	existingProduct, err := l.svcCtx.ProductModel.FindOneById(l.ctx, in.ProductId)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil, errors.New("关联商品不存在")
		}
		l.Logger.Errorf("查询商品失败: %v", err)
		return nil, errors.New("系统错误，请稍后重试")
	}

	// 检查是否已存在秒杀商品
	_, err = l.svcCtx.SeckillProductModel.FindOneByProductId(l.ctx, in.ProductId)
	if err == nil {
		return nil, errors.New("该商品已存在秒杀活动")
	}
	if !errors.Is(err, model.ErrNotFound) {
		l.Logger.Errorf("查询秒杀商品失败: %v", err)
		return nil, errors.New("系统错误，请稍后重试")
	}

	// 创建秒杀商品
	now := time.Now().Unix()
	seckillProduct := &entity.SeckillProduct{
		ProductId:    in.ProductId,
		SeckillPrice: in.SeckillPrice,
		SeckillStock: int(in.SeckillStock),
		StartTime:    in.StartTime,
		EndTime:      in.EndTime,
		PerLimit:     int(in.PerLimit),
		Status:       0, // 默认未开始
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := l.svcCtx.SeckillProductModel.Insert(l.ctx, seckillProduct); err != nil {
		l.Logger.Errorf("创建秒杀商品失败: %v", err)
		return nil, errors.New("创建秒杀商品失败，请稍后重试")
	}

	// ========== 同步秒杀商品信息到 Redis（供 Seckill-Service 使用）==========
	// 计算 TTL：秒杀结束时间 + 1小时缓冲
	ttlSeconds := (seckillProduct.EndTime - now) + 3600
	if ttlSeconds < 3600 {
		ttlSeconds = 3600 // 最小 TTL 为 1 小时
	}

	if l.svcCtx.SeckillRedis != nil {
		if redisErr := l.svcCtx.SeckillRedis.InitSeckillProduct(
			l.ctx,
			seckillProduct.ID,
			existingProduct.ID,
			seckillProduct.SeckillPrice,
			existingProduct.Name,
			int64(seckillProduct.SeckillStock),
			seckillProduct.StartTime,
			seckillProduct.EndTime,
			ttlSeconds,
		); redisErr != nil {
			// Redis 同步失败不影响主流程，只记录日志
			l.Logger.Errorf("同步秒杀商品到 Redis 失败: seckillProductId=%d, err=%v", seckillProduct.ID, redisErr)
		} else {
			l.Logger.Infof("秒杀商品已同步到 Redis: seckillProductId=%d, stock=%d, ttl=%d",
				seckillProduct.ID, seckillProduct.SeckillStock, ttlSeconds)
		}
	}

	l.Logger.Infof("秒杀商品创建成功: seckillProductId=%d, productId=%d", seckillProduct.ID, in.ProductId)

	return &product.SeckillProductInfo{
		Id:           seckillProduct.ID,
		ProductId:    seckillProduct.ProductId,
		SeckillPrice: seckillProduct.SeckillPrice,
		SeckillStock: int64(seckillProduct.SeckillStock),
		SoldCount:    int64(seckillProduct.SoldCount),
		StartTime:    seckillProduct.StartTime,
		EndTime:      seckillProduct.EndTime,
		PerLimit:     int64(seckillProduct.PerLimit),
		Status:       seckillProduct.Status,
		CreatedAt:    seckillProduct.CreatedAt,
		Product: &product.ProductInfo{
			Id:          existingProduct.ID,
			Name:        existingProduct.Name,
			Description: existingProduct.Description,
			Price:       existingProduct.Price,
			Stock:       int64(existingProduct.Stock),
			SoldCount:   int64(existingProduct.SoldCount),
			CoverImage:  existingProduct.CoverImage,
			Status:      existingProduct.Status,
			CreatedAt:   existingProduct.CreatedAt,
			UpdatedAt:   existingProduct.UpdatedAt,
		},
	}, nil
}
