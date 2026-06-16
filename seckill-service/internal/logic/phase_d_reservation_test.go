package logic

import (
	"context"
	"errors"
	"testing"
	"time"

	commonpb "seckill-mall/common/common"
	seckillpb "seckill-mall/common/seckill"
	"seckill-mall/seckill-service/internal/config"
	"seckill-mall/seckill-service/internal/model"
	"seckill-mall/seckill-service/internal/model/entity"
	"seckill-mall/seckill-service/internal/mq"
	redisstore "seckill-mall/seckill-service/internal/redis"
	"seckill-mall/seckill-service/internal/svc"

	"github.com/alicebob/miniredis/v2"
)

type fakeReservationLedger struct {
	persistCalls             []*model.PersistReservationInput
	persistErr               error
	byReservationID          map[string]*entity.SeckillReservation
	byOrderID                map[string]*entity.SeckillReservation
	byUserProduct            map[string]*entity.SeckillReservation
	getReservationErr        error
	findReservationByUserErr error
	releaseCalls             []*model.ReleaseReservationInput
	releaseErr               error
	releaseResult            *entity.SeckillReservation
	advanceCalls             []*model.AdvanceReservationInput
	advanceErr               error
	advanceResult            *entity.SeckillReservation
}

func (f *fakeReservationLedger) PersistReservation(ctx context.Context, in *model.PersistReservationInput) (*model.PersistReservationResult, error) {
	f.persistCalls = append(f.persistCalls, in)
	if f.persistErr != nil {
		return nil, f.persistErr
	}
	reservation := &entity.SeckillReservation{
		ReservationId:    in.ReservationID,
		OrderId:          in.OrderID,
		UserId:           in.UserID,
		SeckillProductId: in.SeckillProductID,
		ProductId:        in.ProductID,
		Quantity:         int(in.Quantity),
		Amount:           in.Amount,
		Status:           entity.ReservationStatusReserved,
		Source:           in.Source,
		Reason:           in.Reason,
		RedisOrderKey:    in.RedisOrderKey,
		ExpireAt:         in.ExpireAt,
	}
	if f.byReservationID == nil {
		f.byReservationID = map[string]*entity.SeckillReservation{}
	}
	if f.byOrderID == nil {
		f.byOrderID = map[string]*entity.SeckillReservation{}
	}
	if f.byUserProduct == nil {
		f.byUserProduct = map[string]*entity.SeckillReservation{}
	}
	f.byReservationID[in.ReservationID] = reservation
	f.byOrderID[in.OrderID] = reservation
	f.byUserProduct[userProductKey(in.UserID, in.SeckillProductID)] = reservation
	return &model.PersistReservationResult{Reservation: reservation}, nil
}

func (f *fakeReservationLedger) GetReservation(ctx context.Context, reservationID, orderID string) (*entity.SeckillReservation, error) {
	if f.getReservationErr != nil {
		return nil, f.getReservationErr
	}
	if reservationID != "" && f.byReservationID != nil {
		if reservation, ok := f.byReservationID[reservationID]; ok {
			return reservation, nil
		}
	}
	if orderID != "" && f.byOrderID != nil {
		if reservation, ok := f.byOrderID[orderID]; ok {
			return reservation, nil
		}
	}
	return nil, model.ErrNotFound
}

func (f *fakeReservationLedger) FindReservationByUserProduct(ctx context.Context, userID, seckillProductID int64) (*entity.SeckillReservation, error) {
	if f.findReservationByUserErr != nil {
		return nil, f.findReservationByUserErr
	}
	if f.byUserProduct == nil {
		return nil, model.ErrNotFound
	}
	reservation, ok := f.byUserProduct[userProductKey(userID, seckillProductID)]
	if !ok {
		return nil, model.ErrNotFound
	}
	return reservation, nil
}

func (f *fakeReservationLedger) ReleaseReservation(ctx context.Context, in *model.ReleaseReservationInput) (*entity.SeckillReservation, error) {
	f.releaseCalls = append(f.releaseCalls, in)
	if f.releaseErr != nil {
		return nil, f.releaseErr
	}
	if f.releaseResult != nil {
		return f.releaseResult, nil
	}
	reservation := &entity.SeckillReservation{
		ReservationId: in.ReservationID,
		OrderId:       in.OrderID,
		Status:        in.TargetStatus,
		Reason:        in.Reason,
		UpdatedAt:     time.Now().Unix(),
	}
	return reservation, nil
}

func (f *fakeReservationLedger) AdvanceReservation(ctx context.Context, in *model.AdvanceReservationInput) (*entity.SeckillReservation, error) {
	f.advanceCalls = append(f.advanceCalls, in)
	if f.advanceErr != nil {
		return nil, f.advanceErr
	}
	if f.advanceResult != nil {
		return f.advanceResult, nil
	}
	if in == nil {
		return nil, model.ErrInvalidParams
	}
	reservation := &entity.SeckillReservation{
		ReservationId: in.ReservationID,
		OrderId:       in.OrderID,
		Status:        in.TargetStatus,
		Reason:        in.Reason,
		UpdatedAt:     time.Now().Unix(),
	}
	if reservation.ReservationId == "" && in.OrderID != "" && f.byOrderID != nil {
		if existing, ok := f.byOrderID[in.OrderID]; ok {
			existing.Status = in.TargetStatus
			existing.Reason = in.Reason
			existing.UpdatedAt = reservation.UpdatedAt
			return existing, nil
		}
	}
	return reservation, nil
}

type fakeOrderProducer struct {
	delayCalls int
	asyncCalls int
}

func (f *fakeOrderProducer) SendDelayOrder(ctx context.Context, msg *mq.SeckillOrderMessage) error {
	f.delayCalls++
	return nil
}

func (f *fakeOrderProducer) SendAsync(ctx context.Context, msg *mq.SeckillOrderMessage) error {
	f.asyncCalls++
	return nil
}

func newTestServiceContext(t *testing.T, ledger model.ReservationLedger, producer svc.OrderProducer) *svc.ServiceContext {
	t.Helper()

	mr := miniredis.RunT(t)
	redisClient, err := redisstore.NewSeckillRedis(redisstore.ClientConfig{
		Mode: "single",
		Addr: mr.Addr(),
	})
	if err != nil {
		t.Fatalf("NewSeckillRedis() error = %v", err)
	}

	ctx := &svc.ServiceContext{
		Config: config.Config{
			LocalQuota: struct {
				Enabled               bool
				BatchSize             int64
				LowWatermark          int64
				LeaseTTLSeconds       int64
				HeartbeatSeconds      int64
				ReaperIntervalSeconds int64
			}{Enabled: false},
		},
		Redis:             redisClient,
		ReservationLedger: ledger,
		OrderProducer:     producer,
		QuotaRefillGate:   svc.NewQuotaRefillGate(),
		InstanceID:        "test-instance",
	}
	return ctx
}

func initSeckillProduct(t *testing.T, ctx *svc.ServiceContext, seckillProductID, productID, stock, price int64) {
	t.Helper()
	now := time.Now().Unix()
	err := ctx.Redis.SetSeckillProductInfo(context.Background(), seckillProductID, productID, price, "测试商品", now-10, now+3600, 3600)
	if err != nil {
		t.Fatalf("SetSeckillProductInfo() error = %v", err)
	}
	if err := ctx.Redis.InitStock(context.Background(), seckillProductID, stock); err != nil {
		t.Fatalf("InitStock() error = %v", err)
	}
}

func userProductKey(userID, seckillProductID int64) string {
	return model.BuildUserProductKey(userID, seckillProductID)
}

func TestSeckillPersistsReservationBeforeReturningSuccess(t *testing.T) {
	ledger := &fakeReservationLedger{}
	producer := &fakeOrderProducer{}
	svcCtx := newTestServiceContext(t, ledger, producer)
	initSeckillProduct(t, svcCtx, 101, 1001, 1, 9900)

	logic := NewSeckillLogic(context.Background(), svcCtx)
	resp, err := logic.Seckill(&seckillpb.SeckillRequest{
		UserId:           2001,
		SeckillProductId: 101,
		Quantity:         1,
	})
	if err != nil {
		t.Fatalf("Seckill() error = %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success response, got %+v", resp)
	}
	if resp.ReservationId == "" || resp.OrderId == "" {
		t.Fatalf("expected reservation_id and order_id, got %+v", resp)
	}
	if resp.ReservationStatus != commonpb.ReservationStatus_RESERVATION_STATUS_RESERVED {
		t.Fatalf("expected reserved status, got %v", resp.ReservationStatus)
	}
	if len(ledger.persistCalls) != 1 {
		t.Fatalf("expected one reservation persistence call, got %d", len(ledger.persistCalls))
	}
	if ledger.persistCalls[0].ReservationID != resp.ReservationId {
		t.Fatalf("expected persisted reservation id %s, got %s", resp.ReservationId, ledger.persistCalls[0].ReservationID)
	}
	wantRedisKey := "{101}:sk:order:" + resp.OrderId
	if ledger.persistCalls[0].RedisOrderKey != wantRedisKey {
		t.Fatalf("expected redis order key %s, got %s", wantRedisKey, ledger.persistCalls[0].RedisOrderKey)
	}
	if producer.delayCalls != 1 || producer.asyncCalls != 0 {
		t.Fatalf("expected only timeout-check mq send, got delay=%d async=%d", producer.delayCalls, producer.asyncCalls)
	}
}

func TestSeckillCompensatesRedisWhenReservationPersistFails(t *testing.T) {
	ledger := &fakeReservationLedger{persistErr: errors.New("db unavailable")}
	producer := &fakeOrderProducer{}
	svcCtx := newTestServiceContext(t, ledger, producer)
	initSeckillProduct(t, svcCtx, 102, 1002, 1, 8800)

	logic := NewSeckillLogic(context.Background(), svcCtx)
	resp, err := logic.Seckill(&seckillpb.SeckillRequest{
		UserId:           2002,
		SeckillProductId: 102,
		Quantity:         1,
	})
	if err != nil {
		t.Fatalf("Seckill() error = %v", err)
	}
	if resp.Success {
		t.Fatalf("expected failure response, got %+v", resp)
	}
	if len(ledger.persistCalls) != 1 {
		t.Fatalf("expected one persistence attempt, got %d", len(ledger.persistCalls))
	}
	orderID := ledger.persistCalls[0].OrderID
	stock, stockErr := svcCtx.Redis.GetStock(context.Background(), 102)
	if stockErr != nil {
		t.Fatalf("GetStock() error = %v", stockErr)
	}
	if stock != 1 {
		t.Fatalf("expected stock restored to 1, got %d", stock)
	}
	userKey := redisstore.KeyUser(102, 2002)
	exists, existsErr := svcCtx.Redis.CheckUserKeyExists(context.Background(), userKey)
	if existsErr != nil {
		t.Fatalf("CheckUserKeyExists() error = %v", existsErr)
	}
	if exists {
		t.Fatalf("expected user key to be released after compensation, order_id=%s", orderID)
	}
	if producer.delayCalls != 0 || producer.asyncCalls != 0 {
		t.Fatalf("expected no mq send on persistence failure, got delay=%d async=%d", producer.delayCalls, producer.asyncCalls)
	}
}

func TestGetSeckillStatusPrefersReservationFactOverRedis(t *testing.T) {
	reservation := &entity.SeckillReservation{
		ReservationId:    "R-103",
		OrderId:          "S103_abc",
		UserId:           2003,
		SeckillProductId: 103,
		ProductId:        1003,
		Quantity:         1,
		Amount:           7700,
		Status:           entity.ReservationStatusOrderCreated,
	}
	ledger := &fakeReservationLedger{
		byUserProduct: map[string]*entity.SeckillReservation{
			userProductKey(2003, 103): reservation,
		},
	}
	svcCtx := newTestServiceContext(t, ledger, nil)
	initSeckillProduct(t, svcCtx, 103, 1003, 10, 7700)

	logic := NewGetSeckillStatusLogic(context.Background(), svcCtx)
	resp, err := logic.GetSeckillStatus(&seckillpb.SeckillStatusRequest{
		UserId:           2003,
		SeckillProductId: 103,
	})
	if err != nil {
		t.Fatalf("GetSeckillStatus() error = %v", err)
	}
	if resp.Status != OrderStatusSuccess {
		t.Fatalf("expected status success from reservation fact, got %+v", resp)
	}
	if resp.OrderStatus != commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_ORDER_CREATED {
		t.Fatalf("expected order_created, got %v", resp.OrderStatus)
	}
	if resp.ReservationStatus != commonpb.ReservationStatus_RESERVATION_STATUS_ORDER_CREATED {
		t.Fatalf("expected reservation order_created, got %v", resp.ReservationStatus)
	}
}

func TestGetSeckillResultFallsBackToReservationFactWhenRedisMissing(t *testing.T) {
	reservation := &entity.SeckillReservation{
		ReservationId:    "R-104",
		OrderId:          "S104_xyz",
		UserId:           2004,
		SeckillProductId: 104,
		ProductId:        1004,
		Quantity:         1,
		Amount:           6600,
		Status:           entity.ReservationStatusOrderCreated,
	}
	ledger := &fakeReservationLedger{
		byOrderID: map[string]*entity.SeckillReservation{
			"S104_xyz": reservation,
		},
	}
	svcCtx := newTestServiceContext(t, ledger, nil)

	logic := NewGetSeckillResultLogic(context.Background(), svcCtx)
	resp, err := logic.GetSeckillResult(&seckillpb.SeckillResultRequest{OrderId: "S104_xyz"})
	if err != nil {
		t.Fatalf("GetSeckillResult() error = %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success from reservation fact, got %+v", resp)
	}
	if resp.OrderStatus != commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_ORDER_CREATED {
		t.Fatalf("expected order_created, got %v", resp.OrderStatus)
	}
	if resp.PaymentStatus != commonpb.PaymentStatus_PAYMENT_STATUS_INIT {
		t.Fatalf("expected init payment status, got %v", resp.PaymentStatus)
	}
}

func TestMainChainQueriesReturnCompletedAfterPaymentSuccess(t *testing.T) {
	ledger := &fakeReservationLedger{}
	producer := &fakeOrderProducer{}
	svcCtx := newTestServiceContext(t, ledger, producer)
	initSeckillProduct(t, svcCtx, 106, 1006, 1, 4400)

	seckillResp, err := NewSeckillLogic(context.Background(), svcCtx).Seckill(&seckillpb.SeckillRequest{
		UserId:           2006,
		SeckillProductId: 106,
		Quantity:         1,
	})
	if err != nil {
		t.Fatalf("Seckill() error = %v", err)
	}
	if !seckillResp.Success {
		t.Fatalf("expected seckill success, got %+v", seckillResp)
	}

	reservation := ledger.byOrderID[seckillResp.OrderId]
	if reservation == nil {
		t.Fatalf("expected persisted reservation for order %s", seckillResp.OrderId)
	}
	reservation.Status = entity.ReservationStatusConsumed

	updateResp, err := NewUpdateOrderStatusLogic(context.Background(), svcCtx).UpdateOrderStatus(&seckillpb.UpdateOrderStatusRequest{
		OrderId: seckillResp.OrderId,
		Status:  redisstore.OrderStatusSuccess,
	})
	if err != nil {
		t.Fatalf("UpdateOrderStatus() error = %v", err)
	}
	if !updateResp.Success {
		t.Fatalf("expected update success, got %+v", updateResp)
	}

	statusResp, err := NewGetSeckillStatusLogic(context.Background(), svcCtx).GetSeckillStatus(&seckillpb.SeckillStatusRequest{
		UserId:           2006,
		SeckillProductId: 106,
	})
	if err != nil {
		t.Fatalf("GetSeckillStatus() error = %v", err)
	}
	if statusResp.OrderStatus != commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_COMPLETED {
		t.Fatalf("expected completed order status from reservation fact, got %v", statusResp.OrderStatus)
	}
	if statusResp.ReservationStatus != commonpb.ReservationStatus_RESERVATION_STATUS_CONSUMED {
		t.Fatalf("expected consumed reservation status, got %v", statusResp.ReservationStatus)
	}
	if statusResp.PaymentStatus != commonpb.PaymentStatus_PAYMENT_STATUS_SUCCESS {
		t.Fatalf("expected success payment status, got %v", statusResp.PaymentStatus)
	}

	resultResp, err := NewGetSeckillResultLogic(context.Background(), svcCtx).GetSeckillResult(&seckillpb.SeckillResultRequest{
		OrderId: seckillResp.OrderId,
	})
	if err != nil {
		t.Fatalf("GetSeckillResult() error = %v", err)
	}
	if !resultResp.Success {
		t.Fatalf("expected success result, got %+v", resultResp)
	}
	if resultResp.OrderStatus != commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_COMPLETED {
		t.Fatalf("expected completed order status from success hot state, got %v", resultResp.OrderStatus)
	}
	if resultResp.PaymentStatus != commonpb.PaymentStatus_PAYMENT_STATUS_SUCCESS {
		t.Fatalf("expected success payment status, got %v", resultResp.PaymentStatus)
	}
}

func TestMainChainRedisSuccessMirrorReturnsCompleted(t *testing.T) {
	svcCtx := newTestServiceContext(t, nil, nil)
	initSeckillProduct(t, svcCtx, 107, 1007, 1, 3300)

	orderID := redisstore.FormatOrderId(107, "S50002")
	_, err := svcCtx.Redis.DoSeckill(context.Background(), &redisstore.SeckillRequest{
		SeckillProductId: 107,
		UserId:           2007,
		Quantity:         1,
		OrderId:          orderID,
		TTL:              300,
		StartTime:        time.Now().Unix() - 10,
		EndTime:          time.Now().Unix() + 3600,
		OrderStatusTTL:   OrderStatusTTL,
	})
	if err != nil {
		t.Fatalf("DoSeckill() error = %v", err)
	}

	if err := svcCtx.Redis.SetOrderInfo(context.Background(), 107, orderID, &redisstore.OrderInfo{
		Status:      redisstore.OrderStatusPending,
		OrderId:     orderID,
		ProductId:   1007,
		Quantity:    1,
		Amount:      3300,
		ProductName: "测试商品",
	}, OrderStatusTTL); err != nil {
		t.Fatalf("SetOrderInfo() error = %v", err)
	}

	updateResp, err := NewUpdateOrderStatusLogic(context.Background(), svcCtx).UpdateOrderStatus(&seckillpb.UpdateOrderStatusRequest{
		OrderId: orderID,
		Status:  redisstore.OrderStatusSuccess,
	})
	if err != nil {
		t.Fatalf("UpdateOrderStatus() error = %v", err)
	}
	if !updateResp.Success {
		t.Fatalf("expected update success, got %+v", updateResp)
	}

	statusResp, err := NewGetSeckillStatusLogic(context.Background(), svcCtx).GetSeckillStatus(&seckillpb.SeckillStatusRequest{
		UserId:           2007,
		SeckillProductId: 107,
	})
	if err != nil {
		t.Fatalf("GetSeckillStatus() error = %v", err)
	}
	if statusResp.OrderStatus != commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_COMPLETED {
		t.Fatalf("expected completed order status from redis mirror, got %v", statusResp.OrderStatus)
	}
	if statusResp.ReservationStatus != commonpb.ReservationStatus_RESERVATION_STATUS_CONSUMED {
		t.Fatalf("expected consumed reservation status from redis mirror, got %v", statusResp.ReservationStatus)
	}
	if statusResp.PaymentStatus != commonpb.PaymentStatus_PAYMENT_STATUS_SUCCESS {
		t.Fatalf("expected success payment status from redis mirror, got %v", statusResp.PaymentStatus)
	}

	resultResp, err := NewGetSeckillResultLogic(context.Background(), svcCtx).GetSeckillResult(&seckillpb.SeckillResultRequest{
		OrderId: orderID,
	})
	if err != nil {
		t.Fatalf("GetSeckillResult() error = %v", err)
	}
	if !resultResp.Success {
		t.Fatalf("expected success result, got %+v", resultResp)
	}
	if resultResp.OrderStatus != commonpb.OrderLifecycleStatus_ORDER_LIFECYCLE_STATUS_COMPLETED {
		t.Fatalf("expected completed order status from redis result query, got %v", resultResp.OrderStatus)
	}
	if resultResp.ReservationStatus != commonpb.ReservationStatus_RESERVATION_STATUS_CONSUMED {
		t.Fatalf("expected consumed reservation status from redis result query, got %v", resultResp.ReservationStatus)
	}
	if resultResp.PaymentStatus != commonpb.PaymentStatus_PAYMENT_STATUS_SUCCESS {
		t.Fatalf("expected success payment status from redis result query, got %v", resultResp.PaymentStatus)
	}
}

func TestCompensateFailedOrderReleasesReservationLedger(t *testing.T) {
	ledger := &fakeReservationLedger{}
	svcCtx := newTestServiceContext(t, ledger, nil)
	initSeckillProduct(t, svcCtx, 105, 1005, 1, 5500)

	orderID := redisstore.FormatOrderId(105, "S50001")
	_, err := svcCtx.Redis.DoSeckill(context.Background(), &redisstore.SeckillRequest{
		SeckillProductId: 105,
		UserId:           2005,
		Quantity:         1,
		OrderId:          orderID,
		TTL:              300,
		StartTime:        time.Now().Unix() - 10,
		EndTime:          time.Now().Unix() + 3600,
		OrderStatusTTL:   OrderStatusTTL,
	})
	if err != nil {
		t.Fatalf("DoSeckill() error = %v", err)
	}

	logic := NewCompensateFailedOrderLogic(context.Background(), svcCtx)
	resp, rpcErr := logic.CompensateFailedOrder(&seckillpb.CompensateFailedOrderRequest{
		OrderId:          orderID,
		SeckillProductId: 105,
		UserId:           2005,
		Quantity:         1,
		Reason:           "timeout",
	})
	if rpcErr != nil {
		t.Fatalf("CompensateFailedOrder() error = %v", rpcErr)
	}
	if !resp.Success {
		t.Fatalf("expected successful compensation, got %+v", resp)
	}
	if len(ledger.releaseCalls) != 1 {
		t.Fatalf("expected reservation ledger release once, got %d", len(ledger.releaseCalls))
	}
	if ledger.releaseCalls[0].TargetStatus != entity.ReservationStatusFailed {
		t.Fatalf("expected target failed status, got %d", ledger.releaseCalls[0].TargetStatus)
	}
}
