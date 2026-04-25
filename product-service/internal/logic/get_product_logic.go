package logic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"seckill-mall/common/product"
	"seckill-mall/product-service/internal/model"
	redisutil "seckill-mall/product-service/internal/redis"
	"seckill-mall/product-service/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetProductLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetProductLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetProductLogic {
	return &GetProductLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetProduct 获取商品详情
// 缓存策略：Cache-Aside + SingleFlight + 随机TTL + 空值缓存
func (l *GetProductLogic) GetProduct(in *product.GetProductRequest) (*product.ProductInfo, error) {
	// 参数校验
	if in.ProductId <= 0 {
		return nil, errors.New("商品ID无效")
	}

	// ① 先查 Redis 缓存
	if l.svcCtx.SeckillRedis != nil {
		if val, found, err := l.svcCtx.SeckillRedis.GetProductCache(l.ctx, in.ProductId); err == nil && found {
			// 空值缓存命中：DB中确认不存在，直接返回（防穿透）
			if val == redisutil.ProductCacheNullValue {
				return nil, errors.New("商品不存在")
			}
			// 有效缓存命中：反序列化直接返回
			var info product.ProductInfo
			if jsonErr := json.Unmarshal([]byte(val), &info); jsonErr == nil {
				return &info, nil
			}
		}
	}

	// ② Cache Miss：SingleFlight 合并并发DB查询，防缓存击穿
	sfKey := fmt.Sprintf("product:detail:%d", in.ProductId)
	result, err := l.svcCtx.SF.Do(sfKey, func() (any, error) {
		// double-check：可能已被同组前一个请求写入缓存
		if l.svcCtx.SeckillRedis != nil {
			if val, found, _ := l.svcCtx.SeckillRedis.GetProductCache(l.ctx, in.ProductId); found {
				if val == redisutil.ProductCacheNullValue {
					return nil, errors.New("商品不存在")
				}
				var info product.ProductInfo
				if jsonErr := json.Unmarshal([]byte(val), &info); jsonErr == nil {
					return &info, nil
				}
			}
		}

		// ③ 查 DB
		existingProduct, dbErr := l.svcCtx.ProductModel.FindOneById(l.ctx, in.ProductId)
		if dbErr != nil {
			if errors.Is(dbErr, model.ErrNotFound) {
				// 缓存空值，防穿透
				if l.svcCtx.SeckillRedis != nil {
					_ = l.svcCtx.SeckillRedis.SetProductCacheNull(l.ctx, in.ProductId)
				}
				return nil, errors.New("商品不存在")
			}
			l.Logger.Errorf("查询商品失败: %v", dbErr)
			return nil, errors.New("系统错误，请稍后重试")
		}

		info := &product.ProductInfo{
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
		}

		// ④ 写入缓存（随机TTL防雪崩）
		if l.svcCtx.SeckillRedis != nil {
			if jsonBytes, jsonErr := json.Marshal(info); jsonErr == nil {
				_ = l.svcCtx.SeckillRedis.SetProductCache(l.ctx, in.ProductId, string(jsonBytes))
			}
		}
		return info, nil
	})

	if err != nil {
		return nil, err
	}
	return result.(*product.ProductInfo), nil
}
