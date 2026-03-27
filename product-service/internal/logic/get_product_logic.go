package logic

import (
	"context"
	"errors"

	"seckill-mall/product-service/internal/model"
	"seckill-mall/product-service/internal/svc"
	"seckill-mall/product-service/product"

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
func (l *GetProductLogic) GetProduct(in *product.GetProductRequest) (*product.ProductInfo, error) {
	// 参数校验
	if in.ProductId <= 0 {
		return nil, errors.New("商品ID无效")
	}

	// 查询商品
	existingProduct, err := l.svcCtx.ProductModel.FindOneById(l.ctx, in.ProductId)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil, errors.New("商品不存在")
		}
		l.Logger.Errorf("查询商品失败: %v", err)
		return nil, errors.New("系统错误，请稍后重试")
	}

	return &product.ProductInfo{
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
	}, nil
}
