package logic

import (
	"context"
	"errors"

	"seckill-mall/common/product"
	"seckill-mall/product-service/internal/model"
	"seckill-mall/product-service/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetSeckillProductLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetSeckillProductLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetSeckillProductLogic {
	return &GetSeckillProductLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetSeckillProduct 获取秒杀商品
func (l *GetSeckillProductLogic) GetSeckillProduct(in *product.GetSeckillProductRequest) (*product.SeckillProductInfo, error) {
	// 参数校验
	if in.SeckillProductId <= 0 {
		return nil, errors.New("秒杀商品ID无效")
	}

	// 查询秒杀商品
	seckillProduct, err := l.svcCtx.SeckillProductModel.FindOneById(l.ctx, in.SeckillProductId)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil, errors.New("秒杀商品不存在")
		}
		l.Logger.Errorf("查询秒杀商品失败: %v", err)
		return nil, errors.New("系统错误，请稍后重试")
	}

	// 查询关联商品
	productInfo, err := l.svcCtx.ProductModel.FindOneById(l.ctx, seckillProduct.ProductId)
	if err != nil {
		if !errors.Is(err, model.ErrNotFound) {
			l.Logger.Errorf("查询商品失败: %v", err)
		}
	}

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
			Id:          productInfo.ID,
			Name:        productInfo.Name,
			Description: productInfo.Description,
			Price:       productInfo.Price,
			Stock:       int64(productInfo.Stock),
			SoldCount:   int64(productInfo.SoldCount),
			CoverImage:  productInfo.CoverImage,
			Status:      productInfo.Status,
			CreatedAt:   productInfo.CreatedAt,
			UpdatedAt:   productInfo.UpdatedAt,
		},
	}, nil
}
