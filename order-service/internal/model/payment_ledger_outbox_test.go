package model

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"regexp"
	"testing"

	"seckill-mall/order-service/internal/payment"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type jsonContains map[string]any

func (m jsonContains) Match(v driver.Value) bool {
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

func TestMarkPaymentRequestedWritesPaymentRequestedOutboxPayload(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer sqlDB.Close()

	gdb, err := gorm.Open(mysql.New(mysql.Config{
		Conn:                      sqlDB,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}

	ledger := NewPaymentLedger(gdb)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `payments` WHERE payment_id = ? ORDER BY `payments`.`payment_id` LIMIT ?")).
		WithArgs("pay-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"payment_id", "order_id", "user_id", "amount", "channel", "status", "third_party_trade_no", "request_id", "paid_at", "closed_at", "created_at", "updated_at",
		}).AddRow("pay-1", "order-1", 1001, 9900, "mock_alipay", 0, "", "req-1", 0, 0, 1710000000, 1710000000))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `orders` WHERE order_id = ? ORDER BY `orders`.`order_id` LIMIT ?")).
		WithArgs("order-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"order_id", "reservation_id", "user_id", "product_id", "product_name", "quantity", "amount", "seckill_price", "order_type", "status", "pay_status", "payment_id", "paid_at", "closed_at", "expired_at", "version", "created_at", "updated_at",
		}).AddRow("order-1", "order-1", 1001, 2001, "", 1, 9900, 9900, 1, 2, 0, "", 0, 0, 1710000300, 0, 1710000000, 1710000000))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `seckill_reservations` WHERE order_id = ? ORDER BY `seckill_reservations`.`reservation_id` LIMIT ?")).
		WithArgs("order-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"reservation_id", "order_id", "user_id", "seckill_product_id", "product_id", "quantity", "amount", "status", "source", "reason", "redis_order_key", "expire_at", "created_at", "updated_at",
		}).AddRow("order-1", "order-1", 1001, 101, 2001, 1, 9900, 2, "gateway", "order.created", "redis-key", 1710000300, 1710000000, 1710000000))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `payments` SET `channel`=?,`status`=?,`updated_at`=? WHERE payment_id = ? AND status = ?")).
		WithArgs("mock_alipay", 1, sqlmock.AnyArg(), "pay-1", 0).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `orders` SET `pay_status`=?,`payment_id`=?,`status`=?,`updated_at`=? WHERE order_id = ?")).
		WithArgs(1, "pay-1", 3, sqlmock.AnyArg(), "order-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `seckill_reservations` SET `reason`=?,`status`=?,`updated_at`=? WHERE order_id = ?")).
		WithArgs("payment.requested", 3, sqlmock.AnyArg(), "order-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `order_status_logs`")).
		WithArgs("order-1", 2, 3, "payment.requested", "payment requested", "payment", "", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `event_outbox`")).
		WithArgs(
			"evt-payment-requested-pay-1",
			"payment",
			"pay-1",
			"payment.requested",
			jsonContains{
				"event_type":     "payment.requested",
				"aggregate_type": "payment",
				"payment_id":     "pay-1",
				"order_id":       "order-1",
				"message_id":     "order-1",
				"channel":        "mock_alipay",
				"user_id":        float64(1001),
			},
			0,
			0,
			0,
			"",
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `payments` WHERE payment_id = ? ORDER BY `payments`.`payment_id` LIMIT ?")).
		WithArgs("pay-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"payment_id", "order_id", "user_id", "amount", "channel", "status", "third_party_trade_no", "request_id", "paid_at", "closed_at", "created_at", "updated_at",
		}).AddRow("pay-1", "order-1", 1001, 9900, "mock_alipay", 1, "", "req-1", 0, 0, 1710000000, 1710000001))
	mock.ExpectCommit()

	got, err := ledger.MarkPaymentRequested(context.Background(), &payment.MarkPaymentRequestedInput{
		PaymentID: "pay-1",
		Channel:   "mock_alipay",
	})
	if err != nil {
		t.Fatalf("MarkPaymentRequested() error = %v", err)
	}
	if got == nil || got.PaymentId != "pay-1" || got.Status != 1 {
		t.Fatalf("MarkPaymentRequested() got = %+v, want requested payment", got)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet() error = %v", err)
	}
}
