package server

import (
	"context"
	"testing"
	"time"

	commonpb "seckill-mall/common/common"
	seckillpb "seckill-mall/common/seckill"
	"seckill-mall/seckill-service/internal/model"
	"seckill-mall/seckill-service/internal/model/entity"
	"seckill-mall/seckill-service/internal/svc"
)

type fakeAdvanceReservationLedger struct {
	last *model.AdvanceReservationInput
}

func (f *fakeAdvanceReservationLedger) PersistReservation(ctx context.Context, in *model.PersistReservationInput) (*model.PersistReservationResult, error) {
	return nil, nil
}

func (f *fakeAdvanceReservationLedger) GetReservation(ctx context.Context, reservationID, orderID string) (*entity.SeckillReservation, error) {
	return nil, model.ErrNotFound
}

func (f *fakeAdvanceReservationLedger) FindReservationByUserProduct(ctx context.Context, userID, seckillProductID int64) (*entity.SeckillReservation, error) {
	return nil, model.ErrNotFound
}

func (f *fakeAdvanceReservationLedger) ReleaseReservation(ctx context.Context, in *model.ReleaseReservationInput) (*entity.SeckillReservation, error) {
	return nil, model.ErrNotFound
}

func (f *fakeAdvanceReservationLedger) AdvanceReservation(ctx context.Context, in *model.AdvanceReservationInput) (*entity.SeckillReservation, error) {
	f.last = in
	return &entity.SeckillReservation{
		ReservationId: "R-106",
		OrderId:       in.OrderID,
		Status:        in.TargetStatus,
		Reason:        in.Reason,
		UpdatedAt:     time.Now().Unix(),
	}, nil
}

func TestAdvanceReservationMapsProtoToLedgerAndBack(t *testing.T) {
	ledger := &fakeAdvanceReservationLedger{}
	server := NewSeckillServiceServer(&svc.ServiceContext{
		ReservationLedger: ledger,
	})

	resp, err := server.AdvanceReservation(context.Background(), &seckillpb.AdvanceReservationRequest{
		OrderId:      "S106_xyz",
		TargetStatus: commonpb.ReservationStatus_RESERVATION_STATUS_CONSUMED,
		Reason:       "payment.succeeded",
		Operator:     "payment_callback",
		PaymentId:    "pay-106",
		AllowRecover: true,
	})
	if err != nil {
		t.Fatalf("AdvanceReservation() error = %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success response, got %+v", resp)
	}
	if ledger.last == nil {
		t.Fatal("expected ledger input to be captured")
	}
	if ledger.last.TargetStatus != entity.ReservationStatusConsumed {
		t.Fatalf("expected consumed target status, got %d", ledger.last.TargetStatus)
	}
	if !ledger.last.AllowRecover {
		t.Fatal("expected allow recover to be true")
	}
	if resp.Reservation == nil || resp.Reservation.Status != commonpb.ReservationStatus_RESERVATION_STATUS_CONSUMED {
		t.Fatalf("expected consumed reservation in response, got %+v", resp.Reservation)
	}
}
