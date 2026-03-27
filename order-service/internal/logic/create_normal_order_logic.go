package logic

import (
	"context"
	"errors"
	"time"

	"seckill-mall/common/utils"
	"seckill-mall/order-service/internal/model/entity"
	"seckill-mall/order-service/internal/svc"
	"seckill-mall/order-service/order"

	"github.com/zeromicro/go-zero/core/logx"
)

const OrderIdPrefix = "N" // 普通订单号前缀

type CreateNormalOrderLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewCreateNormalOrderLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateNormalOrderLogic {
	return &CreateNormalOrderLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// CreateNormalOrder 创建普通订单
func (l *CreateNormalOrderLogic) CreateNormalOrder(in *order.CreateNormalOrderRequest) (*order.OrderInfo, error) {
	// 参数校验
	if in.UserId <= 0 {
		return nil, errors.New("用户ID无效")
	}
	if in.ProductId <= 0 {
		return nil, errors.New("商品ID无效")
	}
	if in.Quantity <= 0 {
		return nil, errors.New("购买数量必须大于0")
	}

	// 调用 Product-Service 获取商品信息
	productInfo, err := l.svcCtx.ProductServiceRPC.GetProduct(l.ctx, in.ProductId)
	if err != nil {
		l.Logger.Errorf("获取商品信息失败: productId=%d, err=%v", in.ProductId, err)
		return nil, errors.New("获取商品信息失败")
	}

	// 生成订单号
	orderId := utils.GenerateOrderId(OrderIdPrefix)
	// 计算订单金额
	amount := productInfo.Price * in.Quantity

	now := time.Now().Unix()
	orderEntity := &entity.Order{
		OrderId:     orderId,
		UserId:      in.UserId,
		ProductId:   in.ProductId,
		ProductName: productInfo.Name,
		Quantity:    int(in.Quantity),
		Amount:      amount,
		OrderType:   entity.OrderTypeNormal,
		Status:      entity.OrderStatusPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// 扣减物理库存
	if err := l.svcCtx.ProductServiceRPC.DeductStock(l.ctx, in.ProductId, in.Quantity, orderId); err != nil {
		l.Logger.Errorf("扣减库存失败: orderId=%s, err=%v", orderId, err)
		return nil, errors.New("库存扣减失败，请稍后重试")
	}

	if err := l.svcCtx.OrderModel.Insert(l.ctx, orderEntity); err != nil {
		// 插入失败，回滚库存
		l.svcCtx.ProductServiceRPC.RollbackStock(l.ctx, in.ProductId, in.Quantity, orderId)
		l.Logger.Errorf("创建订单失败: userId=%d, productId=%d, err=%v", in.UserId, in.ProductId, err)
		return nil, errors.New("创建订单失败，请稍后重试")
	}

	l.Logger.Infof("普通订单创建成功: orderId=%s, userId=%d, amount=%d", orderId, in.UserId, amount)

	return &order.OrderInfo{
		OrderId:      orderEntity.OrderId,
		UserId:       orderEntity.UserId,
		ProductId:    orderEntity.ProductId,
		ProductName:  orderEntity.ProductName,
		Quantity:     int64(orderEntity.Quantity),
		Amount:       orderEntity.Amount,
		SeckillPrice: orderEntity.SeckillPrice,
		OrderType:    orderEntity.OrderType,
		Status:       orderEntity.Status,
		CreatedAt:    orderEntity.CreatedAt,
		UpdatedAt:    orderEntity.UpdatedAt,
	}, nil
}
