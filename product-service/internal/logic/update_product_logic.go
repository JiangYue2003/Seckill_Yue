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

type UpdateProductLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewUpdateProductLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UpdateProductLogic {
	return &UpdateProductLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// UpdateProduct 更新商品
func (l *UpdateProductLogic) UpdateProduct(in *product.UpdateProductRequest) (*commonpb.BoolResponse, error) {
	// 参数校验
	if in.Id <= 0 {
		return nil, errors.New("商品ID无效")
	}

	// 查询商品
	existingProduct, err := l.svcCtx.ProductModel.FindOneById(l.ctx, in.Id)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil, errors.New("商品不存在")
		}
		l.Logger.Errorf("查询商品失败: %v", err)
		return nil, errors.New("系统错误，请稍后重试")
	}

	// 更新字段
	if in.Name != "" {
		existingProduct.Name = in.Name
	}
	if in.Description != "" {
		existingProduct.Description = in.Description
	}
	if in.Price > 0 {
		existingProduct.Price = in.Price
	}
	if in.Stock >= 0 {
		existingProduct.Stock = int(in.Stock)
	}
	if in.CoverImage != "" {
		existingProduct.CoverImage = in.CoverImage
	}
	if in.Status > 0 {
		existingProduct.Status = in.Status
	}

	existingProduct.UpdatedAt = time.Now().Unix()

	// 保存更新
	if err := l.svcCtx.ProductModel.Update(l.ctx, existingProduct); err != nil {
		l.Logger.Errorf("更新商品失败: %v", err)
		return nil, errors.New("更新商品失败，请稍后重试")
	}

	// 主动失效缓存，保证下次读取到最新数据
	if l.svcCtx.SeckillRedis != nil {
		_ = l.svcCtx.SeckillRedis.DeleteProductCache(l.ctx, in.Id)
	}

	l.Logger.Infof("商品更新成功: productId=%d", in.Id)

	return &commonpb.BoolResponse{
		Success: true,
		Message: "更新成功",
	}, nil
}
