package logic

import (
	"context"
	"errors"
	commonpb "seckill-mall/common/common"

	"seckill-mall/common/product"
	"seckill-mall/product-service/internal/model"
	"seckill-mall/product-service/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetStockLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetStockLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetStockLogic {
	return &GetStockLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetStock 查询库存
func (l *GetStockLogic) GetStock(in *commonpb.IdRequest) (*product.StockOperationResponse, error) {
	// 参数校验
	if in.Id <= 0 {
		return nil, errors.New("商品ID无效")
	}

	// 查询库存
	stock, err := l.svcCtx.ProductModel.GetStock(l.ctx, in.Id)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return &product.StockOperationResponse{
				Success: false,
				Message: "商品不存在",
			}, nil
		}
		l.Logger.Errorf("查询库存失败: %v", err)
		return nil, errors.New("系统错误，请稍后重试")
	}

	return &product.StockOperationResponse{
		Success:        true,
		Message:        "查询成功",
		RemainingStock: int64(stock),
	}, nil
}
