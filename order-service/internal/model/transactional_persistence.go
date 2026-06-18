package model

import "context"

type PersistSeckillOrderInput struct {
	MessageID        string
	ConsumerName     string
	OrderID          string
	UserID           int64
	SeckillProductID int64
	ProductID        int64
	Quantity         int64
	Amount           int64
	SeckillPrice     int64
	ShardNo          int32
}

type PersistSeckillOrderResult struct {
	AlreadyProcessed bool
	OrderPersisted   bool
}

type SeckillOrderTxManager interface {
	PersistSeckillOrder(ctx context.Context, in *PersistSeckillOrderInput) (*PersistSeckillOrderResult, error)
}
