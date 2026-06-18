package model

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"regexp"
	"reflect"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type reservationJSONContains map[string]any

func (m reservationJSONContains) Match(v driver.Value) bool {
	raw, ok := v.(string)
	if !ok {
		return false
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return false
	}
	for key, want := range m {
		got, exists := payload[key]
		if !exists {
			return false
		}
		switch expected := want.(type) {
		case float64:
			gotNum, ok := got.(float64)
			if !ok || gotNum != expected {
				return false
			}
		default:
			if got != expected {
				return false
			}
		}
	}
	return true
}

func newReservationLedgerForMock(t *testing.T) (*reservationLedger, sqlmock.Sqlmock, func()) {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}

	gdb, err := gorm.Open(mysql.New(mysql.Config{
		Conn:                      sqlDB,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{})
	if err != nil {
		sqlDB.Close()
		t.Fatalf("gorm.Open() error = %v", err)
	}

	cleanup := func() {
		sqlDB.Close()
	}
	return &reservationLedger{db: gdb}, mock, cleanup
}

func setIntFieldForTest(t *testing.T, target any, field string, value int64) {
	t.Helper()

	rv := reflect.ValueOf(target)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		t.Fatalf("target must be non-nil pointer, got %T", target)
	}
	rv = rv.Elem()
	fv := rv.FieldByName(field)
	if !fv.IsValid() {
		t.Fatalf("expected field %q on %T", field, target)
	}
	if !fv.CanSet() {
		t.Fatalf("field %q on %T is not settable", field, target)
	}
	switch fv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		fv.SetInt(value)
	default:
		t.Fatalf("field %q on %T is not int kind, got %s", field, target, fv.Kind())
	}
}

func TestPersistReservationWritesReservationCreatedOutboxPayload(t *testing.T) {
	ledger, mock, cleanup := newReservationLedgerForMock(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `seckill_reservations`")).
		WithArgs(
			"R1",
			"O1",
			1001,
			101,
			2001,
			1,
			9900,
			int64(3),
			0,
			"gateway",
			"reservation.created",
			"redis-order-key",
			int64(1710000300),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `event_outbox`")).
		WithArgs(
			"evt-reservation-created-R1",
			"reservation",
			"R1",
			"reservation.created",
			reservationJSONContains{
				"event_type":         "reservation.created",
				"aggregate_type":     "reservation",
				"message_id":         "O1",
				"reservation_id":     "R1",
				"order_id":           "O1",
				"seckill_product_id": float64(101),
				"shard_no":           float64(3),
			},
			0, 0, 0, "",
			sqlmock.AnyArg(), sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `event_outbox`")).
		WithArgs(
			"evt-reservation-timeout-check-R1",
			"reservation",
			"R1",
			"reservation.timeout.check",
			sqlmock.AnyArg(),
			0, 0, 0, "",
			sqlmock.AnyArg(), sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(2, 1))
	mock.ExpectCommit()

	in := &PersistReservationInput{
		ReservationID:    "R1",
		OrderID:          "O1",
		UserID:           1001,
		SeckillProductID: 101,
		ProductID:        2001,
		Quantity:         1,
		Amount:           9900,
		SeckillPrice:     9900,
		Source:           "gateway",
		Reason:           "reservation.created",
		RedisOrderKey:    "redis-order-key",
		ExpireAt:         1710000300,
	}
	setIntFieldForTest(t, in, "ShardNo", 3)

	got, err := ledger.PersistReservation(context.Background(), in)
	if err != nil {
		t.Fatalf("PersistReservation() error = %v", err)
	}
	if got == nil || got.Reservation == nil || got.Reservation.ReservationId != "R1" {
		t.Fatalf("PersistReservation() got = %+v, want reservation R1", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet() error = %v", err)
	}
}

func TestReleaseReservationWritesReservationReleasedOutboxPayload(t *testing.T) {
	ledger, mock, cleanup := newReservationLedgerForMock(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `seckill_reservations` WHERE reservation_id = ? ORDER BY `seckill_reservations`.`reservation_id` LIMIT ?")).
		WithArgs("R2", 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"reservation_id", "order_id", "user_id", "seckill_product_id", "product_id", "quantity", "amount", "shard_no", "status", "source", "reason", "redis_order_key", "expire_at", "created_at", "updated_at",
		}).AddRow("R2", "O2", 1002, 102, 2002, 1, 10900, 4, 0, "gateway", "reservation.created", "redis-key", 1710000400, 1710000000, 1710000000))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `seckill_reservations` SET")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `order_status_logs`")).
		WithArgs("O2", 0, 6, "reservation.released", "timeout_release", "system", "", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `event_outbox`")).
		WithArgs(
			sqlmock.AnyArg(),
			"reservation",
			"R2",
			"reservation.released",
			reservationJSONContains{
				"event_type":         "reservation.released",
				"aggregate_type":     "reservation",
				"message_id":         "O2",
				"reservation_id":     "R2",
				"order_id":           "O2",
				"status":             float64(6),
				"from_status":        float64(0),
				"shard_no":           float64(4),
			},
			0, 0, 0, "",
			sqlmock.AnyArg(), sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	got, err := ledger.ReleaseReservation(context.Background(), &ReleaseReservationInput{
		ReservationID: "R2",
		Reason:        "timeout_release",
		TargetStatus:  6,
		Operator:      "system",
	})
	if err != nil {
		t.Fatalf("ReleaseReservation() error = %v", err)
	}
	if got == nil || got.ReservationId != "R2" || got.Status != 6 {
		t.Fatalf("ReleaseReservation() got = %+v, want released reservation", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet() error = %v", err)
	}
}

func TestAdvanceReservationWritesReservationAdvancedOutboxPayload(t *testing.T) {
	ledger, mock, cleanup := newReservationLedgerForMock(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `seckill_reservations` WHERE order_id = ? ORDER BY `seckill_reservations`.`reservation_id` LIMIT ?")).
		WithArgs("O3", 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"reservation_id", "order_id", "user_id", "seckill_product_id", "product_id", "quantity", "amount", "shard_no", "status", "source", "reason", "redis_order_key", "expire_at", "created_at", "updated_at",
		}).AddRow("R3", "O3", 1003, 103, 2003, 1, 11900, 5, 3, "gateway", "payment.requested", "redis-key", 1710000500, 1710000000, 1710000000))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `seckill_reservations` SET")).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `order_status_logs`")).
		WithArgs("O3", 3, 5, "reservation.advanced", "payment.succeeded", "payment-callback", "", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `event_outbox`")).
		WithArgs(
			sqlmock.AnyArg(),
			"reservation",
			"R3",
			"reservation.advanced",
			reservationJSONContains{
				"event_type":         "reservation.advanced",
				"aggregate_type":     "reservation",
				"message_id":         "O3",
				"reservation_id":     "R3",
				"order_id":           "O3",
				"payment_id":         "P3",
				"status":             float64(5),
				"from_status":        float64(3),
				"shard_no":           float64(5),
			},
			0, 0, 0, "",
			sqlmock.AnyArg(), sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	got, err := ledger.AdvanceReservation(context.Background(), &AdvanceReservationInput{
		OrderID:      "O3",
		TargetStatus: 5,
		Reason:       "payment.succeeded",
		Operator:     "payment-callback",
		PaymentID:    "P3",
	})
	if err != nil {
		t.Fatalf("AdvanceReservation() error = %v", err)
	}
	if got == nil || got.ReservationId != "R3" || got.Status != 5 {
		t.Fatalf("AdvanceReservation() got = %+v, want advanced reservation", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet() error = %v", err)
	}
}
