package logic

import (
	"context"
	"errors"
	"time"

	commonpb "seckill-mall/common/common"
	"seckill-mall/common/seckill"
	"seckill-mall/seckill-service/internal/model"
	"seckill-mall/seckill-service/internal/model/entity"
	"seckill-mall/seckill-service/internal/redis"
	"seckill-mall/seckill-service/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	OrderStatusPending  = "pending"
	OrderStatusSuccess  = "success"
	OrderStatusFailed   = "failed"
	OrderStatusNotStart = "not_started"
	OrderStatusEnded    = "ended"
	OrderStatusSoldOut  = "sold_out"
)

type GetSeckillStatusLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetSeckillStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetSeckillStatusLogic {
	return &GetSeckillStatusLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// GetSeckillStatus 查询秒杀状态（轮询接口）
// 职责：
//  1. 检查秒杀活动是否在有效时间范围内
//  2. 检查用户是否已购买（通过 userKey 判断）
//  3. 查询订单处理状态（pending/success/failed/not_started/ended/sold_out）
//  4. 返回完整的订单信息（order_id、product_id、quantity）
func (l *GetSeckillStatusLogic) GetSeckillStatus(in *seckill.SeckillStatusRequest) (*seckill.SeckillStatusResponse, error) {
	// ========== 参数校验 ==========
	if in.UserId <= 0 || in.SeckillProductId <= 0 {
		return &seckill.SeckillStatusResponse{
			Status:        OrderStatusFailed,
			OrderStatus:   commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_FAILED,
			PaymentStatus: commonpb.PaymentStatus_PAYMENT_STATUS_FAILED,
		}, nil
	}

	now := time.Now().Unix()

	// ========== 检查秒杀活动时间范围 ==========
	meta, err := l.svcCtx.GetSeckillProductMeta(l.ctx, in.SeckillProductId)
	if err != nil {
		l.Logger.Errorf("获取秒杀商品信息失败: seckillProductId=%d, err=%v", in.SeckillProductId, err)
		return &seckill.SeckillStatusResponse{
			Status: OrderStatusPending,
		}, nil
	}
	var startTime, endTime int64
	if meta != nil {
		startTime = meta.StartTime
		endTime = meta.EndTime
	}

	if startTime > 0 && now < startTime {
		return &seckill.SeckillStatusResponse{
			Status:        OrderStatusNotStart,
			OrderStatus:   commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_INIT,
			PaymentStatus: commonpb.PaymentStatus_PAYMENT_STATUS_INIT,
		}, nil
	}
	if endTime > 0 && now > endTime {
		return &seckill.SeckillStatusResponse{
			Status:        OrderStatusEnded,
			OrderStatus:   commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_EXPIRED,
			PaymentStatus: commonpb.PaymentStatus_PAYMENT_STATUS_CLOSED,
		}, nil
	}

	if l.svcCtx.ReservationLedger != nil {
		reservation, reservationErr := l.svcCtx.ReservationLedger.FindReservationByUserProduct(l.ctx, in.UserId, in.SeckillProductId)
		if reservationErr == nil && reservation != nil {
			status := OrderStatusPending
			if isReservationSuccess(reservation.Status) {
				status = OrderStatusSuccess
			}
			if reservation.Status == entity.ReservationStatusFailed {
				status = OrderStatusFailed
			}
			return &seckill.SeckillStatusResponse{
				Status:            status,
				OrderId:           reservation.OrderId,
				ProductId:         reservation.ProductId,
				Quantity:          int64(reservation.Quantity),
				ReservationId:     reservation.ReservationId,
				ReservationStatus: mapReservationStatus(reservation.Status),
				OrderStatus:       mapReservationOrderLifecycle(reservation.Status),
				PaymentStatus:     mapReservationPaymentStatus(reservation.Status),
			}, nil
		}
		if reservationErr != nil && !errors.Is(reservationErr, model.ErrNotFound) {
			l.Logger.Errorf("查询 reservation 事实失败: userId=%d, seckillProductId=%d, err=%v", in.UserId, in.SeckillProductId, reservationErr)
		}
	}

	// ========== 检查用户购买记录 ==========
	// 通过 Redis userKey 判断用户是否已参与过该秒杀
	userKey := redis.KeyUser(in.SeckillProductId, in.UserId)
	exists, err := l.svcCtx.Redis.CheckUserKeyExists(l.ctx, userKey)
	if err != nil {
		l.Logger.Errorf("检查用户购买记录失败: userId=%d, seckillProductId=%d, err=%v",
			in.UserId, in.SeckillProductId, err)
		return &seckill.SeckillStatusResponse{
			Status: OrderStatusPending,
		}, nil
	}

	// 用户未参与过秒杀（userKey 不存在），说明还在排队或已售罄
	if !exists {
		// 检查秒杀商品是否还有库存
		stock, stockErr := l.svcCtx.Redis.GetStock(l.ctx, in.SeckillProductId)
		if stockErr != nil {
			l.Logger.Errorf("查询库存失败: seckillProductId=%d, err=%v", in.SeckillProductId, stockErr)
			return &seckill.SeckillStatusResponse{
				Status:        OrderStatusPending,
				OrderStatus:   commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_RESERVED,
				PaymentStatus: commonpb.PaymentStatus_PAYMENT_STATUS_INIT,
			}, nil
		}

		// 库存为0说明已售罄
		if stock == 0 {
			return &seckill.SeckillStatusResponse{
				Status:        OrderStatusSoldOut,
				OrderStatus:   commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_FAILED,
				PaymentStatus: commonpb.PaymentStatus_PAYMENT_STATUS_FAILED,
			}, nil
		}

		// 库存还有，用户还未购买，说明在排队中
		return &seckill.SeckillStatusResponse{
			Status:        OrderStatusPending,
			OrderStatus:   commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_RESERVED,
			PaymentStatus: commonpb.PaymentStatus_PAYMENT_STATUS_INIT,
		}, nil
	}

	// ========== 用户已参与过秒杀，查询订单状态 ==========
	// userKey 的值就是 orderId
	orderId, err := l.svcCtx.Redis.GetUserOrderId(l.ctx, userKey)
	if err != nil {
		l.Logger.Errorf("获取用户订单号失败: userId=%d, seckillProductId=%d, err=%v",
			in.UserId, in.SeckillProductId, err)
		return &seckill.SeckillStatusResponse{
			Status:        OrderStatusPending,
			OrderStatus:   commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_RESERVED,
			PaymentStatus: commonpb.PaymentStatus_PAYMENT_STATUS_INIT,
		}, nil
	}

	if orderId == "" {
		return &seckill.SeckillStatusResponse{
			Status:        OrderStatusFailed,
			OrderStatus:   commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_FAILED,
			PaymentStatus: commonpb.PaymentStatus_PAYMENT_STATUS_FAILED,
		}, nil
	}

	// 从 Redis 获取订单信息
	orderInfo, err := l.svcCtx.Redis.GetOrderInfo(l.ctx, orderId)
	if err != nil {
		l.Logger.Errorf("获取订单信息失败: orderId=%s, err=%v", orderId, err)
		return &seckill.SeckillStatusResponse{
			Status:            OrderStatusSuccess,
			OrderId:           orderId,
			ReservationId:     orderId,
			ReservationStatus: commonpb.ReservationStatus_RESERVATION_STATUS_ORDER_CREATED,
			OrderStatus:       commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_ORDER_CREATED,
			PaymentStatus:     commonpb.PaymentStatus_PAYMENT_STATUS_INIT,
		}, nil
	}

	// 根据状态返回
	switch orderInfo.Status {
	case OrderStatusSuccess:
		return &seckill.SeckillStatusResponse{
			Status:            OrderStatusSuccess,
			OrderId:           orderId,
			ProductId:         orderInfo.ProductId,
			Quantity:          orderInfo.Quantity,
			ReservationId:     orderId,
			ReservationStatus: commonpb.ReservationStatus_RESERVATION_STATUS_CONSUMED,
			OrderStatus:       commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_COMPLETED,
			PaymentStatus:     commonpb.PaymentStatus_PAYMENT_STATUS_SUCCESS,
		}, nil
	case OrderStatusFailed:
		return &seckill.SeckillStatusResponse{
			Status:            OrderStatusFailed,
			OrderId:           orderId,
			ReservationId:     orderId,
			ReservationStatus: commonpb.ReservationStatus_RESERVATION_STATUS_FAILED,
			OrderStatus:       commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_FAILED,
			PaymentStatus:     commonpb.PaymentStatus_PAYMENT_STATUS_FAILED,
		}, nil
	default:
		// pending 或其他状态，说明订单处理中
		return &seckill.SeckillStatusResponse{
			Status:            OrderStatusPending,
			OrderId:           orderId,
			ProductId:         orderInfo.ProductId,
			Quantity:          orderInfo.Quantity,
			ReservationId:     orderId,
			ReservationStatus: commonpb.ReservationStatus_RESERVATION_STATUS_RESERVED,
			OrderStatus:       commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_RESERVED,
			PaymentStatus:     commonpb.PaymentStatus_PAYMENT_STATUS_INIT,
		}, nil
	}
}
