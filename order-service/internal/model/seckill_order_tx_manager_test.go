package model

import (
	"context"
	"regexp"
	"testing"

	mysqlerr "github.com/go-sql-driver/mysql"
	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func newMockSeckillOrderTxManager(t *testing.T) (*seckillOrderTxManager, sqlmock.Sqlmock, func()) {
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
	return &seckillOrderTxManager{db: gdb}, mock, cleanup
}

func TestPersistSeckillOrderReturnsAlreadyProcessedWhenMessageExists(t *testing.T) {
	manager, mock, cleanup := newMockSeckillOrderTxManager(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `processed_messages` WHERE message_id = ? AND consumer_name = ? ORDER BY `processed_messages`.`id` LIMIT ?")).
		WithArgs("msg-existing", "order-service.seckill-order", 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "message_id", "consumer_name", "status", "processed_at", "created_at",
		}).AddRow(1, "msg-existing", "order-service.seckill-order", 1, 1710000001, 1710000000))
	mock.ExpectCommit()

	got, err := manager.PersistSeckillOrder(context.Background(), &PersistSeckillOrderInput{
		MessageID:        "msg-existing",
		ConsumerName:     "order-service.seckill-order",
		OrderID:          "order-existing",
		UserID:           1001,
		SeckillProductID: 2001,
		ProductID:        3001,
		Quantity:         1,
		Amount:           999,
		SeckillPrice:     999,
	})
	if err != nil {
		t.Fatalf("PersistSeckillOrder() error = %v", err)
	}
	if got == nil || !got.AlreadyProcessed {
		t.Fatalf("expected already processed result, got %+v", got)
	}
	if got.OrderPersisted {
		t.Fatalf("expected no order persistence on duplicate processed message, got %+v", got)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet() error = %v", err)
	}
}

func TestPersistSeckillOrderTreatsMySQLDuplicateInsertAsAlreadyProcessed(t *testing.T) {
	manager, mock, cleanup := newMockSeckillOrderTxManager(t)
	defer cleanup()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `processed_messages` WHERE message_id = ? AND consumer_name = ? ORDER BY `processed_messages`.`id` LIMIT ?")).
		WithArgs("msg-race", "order-service.seckill-order", 1).
		WillReturnError(gorm.ErrRecordNotFound)
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `processed_messages`")).
		WillReturnError(&mysqlerr.MySQLError{Number: 1062, Message: "Duplicate entry"})
	mock.ExpectCommit()

	got, err := manager.PersistSeckillOrder(context.Background(), &PersistSeckillOrderInput{
		MessageID:        "msg-race",
		ConsumerName:     "order-service.seckill-order",
		OrderID:          "order-race",
		UserID:           1002,
		SeckillProductID: 2002,
		ProductID:        3002,
		Quantity:         1,
		Amount:           1999,
		SeckillPrice:     1999,
	})
	if err != nil {
		t.Fatalf("PersistSeckillOrder() error = %v", err)
	}
	if got == nil || !got.AlreadyProcessed {
		t.Fatalf("expected duplicate insert to be treated as already processed, got %+v", got)
	}
	if got.OrderPersisted {
		t.Fatalf("expected no order persistence after duplicate insert short-circuit, got %+v", got)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet() error = %v", err)
	}
}
