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

type RollbackStockLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewRollbackStockLogic(ctx context.Context, svcCtx *svc.ServiceContext) *RollbackStockLogic {
	return &RollbackStockLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// RollbackStock 回滚库存（幂等操作）
func (l *RollbackStockLogic) RollbackStock(in *product.RollbackStockRequest) (*product.StockOperationResponse, error) {
	// 参数校验
	if in.ProductId <= 0 {
		return nil, errors.New("商品ID无效")
	}
	if in.Quantity <= 0 {
		return nil, errors.New("回滚数量必须大于0")
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

	beforeStock := existingProduct.Stock

	// 回滚库存
	if err := l.svcCtx.ProductModel.RollbackStock(l.ctx, in.ProductId, int(in.Quantity), in.OrderId); err != nil {
		l.Logger.Errorf("回滚库存失败: %v", err)
		return nil, errors.New("回滚库存失败，请稍后重试")
	}

	// 获取更新后的库存
	stock, err := l.svcCtx.ProductModel.GetStock(l.ctx, in.ProductId)
	if err != nil {
		l.Logger.Errorf("查询库存失败: %v", err)
		return nil, errors.New("系统错误，请稍后重试")
	}

	// 写入库存流水
	stockLog := &entity.StockLog{
		ProductID:   in.ProductId,
		OrderID:     in.OrderId,
		ChangeType:  entity.StockChangeTypeRollback,
		Quantity:    int(in.Quantity),
		BeforeStock: beforeStock,
		AfterStock:  stock,
		CreatedAt:   time.Now().Unix(),
	}
	if err := l.svcCtx.StockLogModel.Insert(l.ctx, stockLog); err != nil {
		l.Logger.Errorf("写入库存流水失败: %v (不影响主流程)", err)
	}

	l.Logger.Infof("回滚库存成功: productId=%d, quantity=%d, orderId=%s, remainingStock=%d",
		in.ProductId, in.Quantity, in.OrderId, stock)

	return &product.StockOperationResponse{
		Success:        true,
		Message:        "回滚库存成功",
		RemainingStock: int64(stock),
	}, nil
}
