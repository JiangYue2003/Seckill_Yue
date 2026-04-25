package logic

import (
	"context"
	commonpb "seckill-mall/common/common"

	"seckill-mall/common/product"
	"seckill-mall/product-service/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListActiveSeckillProductsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListActiveSeckillProductsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListActiveSeckillProductsLogic {
	return &ListActiveSeckillProductsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// ListActiveSeckillProducts 获取进行中的秒杀商品列表
func (l *ListActiveSeckillProductsLogic) ListActiveSeckillProducts(in *commonpb.Empty, stream product.ProductService_ListActiveSeckillProductsServer) error {
	// 查询进行中的秒杀商品
	seckillProducts, err := l.svcCtx.SeckillProductModel.ListActive(l.ctx)
	if err != nil {
		l.Logger.Errorf("查询秒杀商品列表失败: %v", err)
		return err
	}

	for _, sp := range seckillProducts {
		// 查询关联商品
		productInfo, err := l.svcCtx.ProductModel.FindOneById(l.ctx, sp.ProductId)
		if err != nil {
			l.Logger.Errorf("查询商品失败: productId=%d, err=%v", sp.ProductId, err)
			continue
		}

		info := &product.SeckillProductInfo{
			Id:           sp.ID,
			ProductId:    sp.ProductId,
			SeckillPrice: sp.SeckillPrice,
			SeckillStock: int64(sp.SeckillStock),
			SoldCount:    int64(sp.SoldCount),
			StartTime:    sp.StartTime,
			EndTime:      sp.EndTime,
			PerLimit:     int64(sp.PerLimit),
			Status:       sp.Status,
			CreatedAt:    sp.CreatedAt,
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
		}

		if err := stream.Send(info); err != nil {
			l.Logger.Errorf("发送秒杀商品失败: %v", err)
			return err
		}
	}

	return nil
}
