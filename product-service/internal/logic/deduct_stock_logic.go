package logic

import (
	"context"
	"errors"

	"seckill-mall/common/product"
	"seckill-mall/product-service/internal/model"
	"seckill-mall/product-service/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type DeductStockLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDeductStockLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DeductStockLogic {
	return &DeductStockLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// DeductStock 扣减库存（幂等操作）
func (l *DeductStockLogic) DeductStock(in *product.DeductStockRequest) (*product.StockOperationResponse, error) {
	// 参数校验
	if in.ProductId <= 0 {
		return nil, errors.New("商品ID无效")
	}
	if in.Quantity <= 0 {
		return nil, errors.New("扣减数量必须大于0")
	}
	if in.OrderId == "" {
		return nil, errors.New("订单号不能为空")
	}

	// 查询商品
	existingProduct, err := l.svcCtx.ProductModel.FindOneById(l.ctx, in.ProductId)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return &product.StockOperationResponse{
				Success: false,
				Message: "商品不存在",
			}, nil
		}
		l.Logger.Errorf("查询商品失败: %v", err)
		return nil, errors.New("系统错误，请稍后重试")
	}

	// 检查库存
	if existingProduct.Stock < int(in.Quantity) {
		return &product.StockOperationResponse{
			Success:        false,
			Message:        "库存不足",
			RemainingStock: int64(existingProduct.Stock),
		}, nil
	}

	// 扣减库存（幂等 + 日志写入在 model 事务内完成）
	remainingStock, err := l.svcCtx.ProductModel.DeductStock(l.ctx, in.ProductId, int(in.Quantity), in.OrderId)
	if err != nil {
		if errors.Is(err, model.ErrAlreadyDeducted) {
			// 幂等：已扣减过，直接返回成功
			l.Logger.Infof("库存已扣减，跳过: orderId=%s, remainingStock=%d", in.OrderId, remainingStock)
			return &product.StockOperationResponse{
				Success:        true,
				Message:        "库存已扣减",
				RemainingStock: int64(remainingStock),
			}, nil
		}
		if errors.Is(err, model.ErrStockNotEnough) {
			return &product.StockOperationResponse{
				Success:        false,
				Message:        "库存不足",
				RemainingStock: int64(existingProduct.Stock),
			}, nil
		}
		l.Logger.Errorf("扣减库存失败: %v", err)
		return nil, errors.New("扣减库存失败，请稍后重试")
	}

	l.Logger.Infof("扣减库存成功: productId=%d, quantity=%d, orderId=%s, remainingStock=%d",
		in.ProductId, in.Quantity, in.OrderId, remainingStock)

	return &product.StockOperationResponse{
		Success:        true,
		Message:        "扣减库存成功",
		RemainingStock: int64(remainingStock),
	}, nil
}
