package logic

import (
	"context"
	"errors"
	"time"

	"seckill-mall/product-service/internal/model"
	"seckill-mall/product-service/internal/model/entity"
	"seckill-mall/product-service/internal/svc"
	"seckill-mall/product-service/product"

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

	// 幂等检查：已扣减则直接返回成功
	_, err := l.svcCtx.StockLogModel.FindByOrderId(l.ctx, in.OrderId)
	if err == nil {
		l.Logger.Infof("库存已扣减，跳过: orderId=%s", in.OrderId)
		stock, _ := l.svcCtx.ProductModel.GetStock(l.ctx, in.ProductId)
		return &product.StockOperationResponse{
			Success:        true,
			Message:        "库存已扣减",
			RemainingStock: int64(stock),
		}, nil
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

	beforeStock := existingProduct.Stock

	// 扣减库存（乐观锁）
	remainingStock, err := l.svcCtx.ProductModel.DeductStock(l.ctx, in.ProductId, int(in.Quantity), in.OrderId)
	if err != nil {
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

	// 写入库存流水
	stockLog := &entity.StockLog{
		ProductID:   in.ProductId,
		OrderID:     in.OrderId,
		ChangeType:  entity.StockChangeTypeDeduct,
		Quantity:    int(in.Quantity),
		BeforeStock: beforeStock,
		AfterStock:  remainingStock,
		CreatedAt:   time.Now().Unix(),
	}
	if err := l.svcCtx.StockLogModel.Insert(l.ctx, stockLog); err != nil {
		l.Logger.Errorf("写入库存流水失败: %v (不影响主流程)", err)
	}

	l.Logger.Infof("扣减库存成功: productId=%d, quantity=%d, orderId=%s, remainingStock=%d",
		in.ProductId, in.Quantity, in.OrderId, remainingStock)

	return &product.StockOperationResponse{
		Success:        true,
		Message:        "扣减库存成功",
		RemainingStock: int64(remainingStock),
	}, nil
}
