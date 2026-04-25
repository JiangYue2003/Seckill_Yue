package logic

import (
	"context"
	"errors"
	"time"

	"seckill-mall/common/product"
	"seckill-mall/product-service/internal/model/entity"
	"seckill-mall/product-service/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type CreateProductLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateProductLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateProductLogic {
	return &CreateProductLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// CreateProduct 创建商品
func (l *CreateProductLogic) CreateProduct(in *product.CreateProductRequest) (*product.ProductInfo, error) {
	// 参数校验
	if in.Name == "" {
		return nil, errors.New("商品名称不能为空")
	}
	if in.Price <= 0 {
		return nil, errors.New("商品价格必须大于0")
	}
	if in.Stock < 0 {
		return nil, errors.New("库存数量不能为负数")
	}

	now := time.Now().Unix()
	newProduct := &entity.Product{
		Name:        in.Name,
		Description: in.Description,
		Price:       in.Price,
		Stock:       int(in.Stock),
		CoverImage:  in.CoverImage,
		Status:      in.Status,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := l.svcCtx.ProductModel.Insert(l.ctx, newProduct); err != nil {
		l.Logger.Errorf("创建商品失败: %v", err)
		return nil, errors.New("创建商品失败，请稍后重试")
	}

	l.Logger.Infof("商品创建成功: productId=%d, name=%s", newProduct.ID, newProduct.Name)

	return &product.ProductInfo{
		Id:          newProduct.ID,
		Name:        newProduct.Name,
		Description: newProduct.Description,
		Price:       newProduct.Price,
		Stock:       int64(newProduct.Stock),
		SoldCount:   int64(newProduct.SoldCount),
		CoverImage:  newProduct.CoverImage,
		Status:      newProduct.Status,
		CreatedAt:   newProduct.CreatedAt,
		UpdatedAt:   newProduct.UpdatedAt,
	}, nil
}
