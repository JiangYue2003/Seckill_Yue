package logic

import (
	"context"
	"errors"

	"seckill-mall/product-service/internal/model"
	"seckill-mall/product-service/internal/svc"
	"seckill-mall/product-service/product"

	"github.com/zeromicro/go-zero/core/logx"
)

type DeleteProductLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDeleteProductLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeleteProductLogic {
	return &DeleteProductLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// DeleteProduct 删除商品
func (l *DeleteProductLogic) DeleteProduct(in *product.IdRequest) (*product.BoolResponse, error) {
	// 参数校验
	if in.Id <= 0 {
		return nil, errors.New("商品ID无效")
	}

	// 查询商品
	_, err := l.svcCtx.ProductModel.FindOneById(l.ctx, in.Id)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil, errors.New("商品不存在")
		}
		l.Logger.Errorf("查询商品失败: %v", err)
		return nil, errors.New("系统错误，请稍后重试")
	}

	// 删除商品
	if err := l.svcCtx.ProductModel.Delete(l.ctx, in.Id); err != nil {
		l.Logger.Errorf("删除商品失败: %v", err)
		return nil, errors.New("删除商品失败，请稍后重试")
	}

	l.Logger.Infof("商品删除成功: productId=%d", in.Id)

	return &product.BoolResponse{
		Success: true,
		Message: "删除成功",
	}, nil
}
