package logic

import (
	"context"

	"seckill-mall/product-service/internal/svc"
	"seckill-mall/product-service/product"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListProductsLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewListProductsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListProductsLogic {
	return &ListProductsLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// ListProducts 商品列表
func (l *ListProductsLogic) ListProducts(in *product.ListProductsRequest) (*product.ListProductsResponse, error) {
	// 设置默认值
	page := in.Page
	if page <= 0 {
		page = 1
	}
	pageSize := in.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	// 查询商品列表
	products, total, err := l.svcCtx.ProductModel.FindByKeyword(l.ctx, in.Keyword, in.Status, page, pageSize)
	if err != nil {
		l.Logger.Errorf("查询商品列表失败: %v", err)
		return nil, err
	}

	// 转换响应
	var productInfos []*product.ProductInfo
	for _, p := range products {
		productInfos = append(productInfos, &product.ProductInfo{
			Id:          p.ID,
			Name:        p.Name,
			Description: p.Description,
			Price:       p.Price,
			Stock:       int64(p.Stock),
			SoldCount:   int64(p.SoldCount),
			CoverImage:  p.CoverImage,
			Status:      p.Status,
			CreatedAt:   p.CreatedAt,
			UpdatedAt:   p.UpdatedAt,
		})
	}

	return &product.ListProductsResponse{
		Products: productInfos,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}
