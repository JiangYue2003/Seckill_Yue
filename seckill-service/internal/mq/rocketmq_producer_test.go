package mq

import "testing"

func TestDecodeSeckillOrderMessageSupportsUnifiedEnvelopePayload(t *testing.T) {
	payload := `{
		"event_id":"evt-reservation-created-R1",
		"event_type":"reservation.created",
		"occurred_at":1710000000,
		"aggregate_type":"reservation",
		"aggregate_id":"R1",
		"trace_id":"trace-R1",
		"source":"seckill-service",
		"version":1,
		"message_id":"O1",
		"reservation_id":"R1",
		"order_id":"O1",
		"user_id":7,
		"seckill_product_id":101,
		"product_id":1001,
		"quantity":1,
		"seckill_price":9900,
		"amount":9900,
		"status":0,
		"expire_at":1710000300
	}`

	msg, err := decodeSeckillOrderMessage(payload)
	if err != nil {
		t.Fatalf("decodeSeckillOrderMessage() error = %v", err)
	}
	if msg.OrderId != "O1" {
		t.Fatalf("expected order_id O1, got %s", msg.OrderId)
	}
	if msg.MessageId != "O1" {
		t.Fatalf("expected message_id O1, got %s", msg.MessageId)
	}
	if msg.SeckillProductId != 101 || msg.ProductId != 1001 || msg.Quantity != 1 || msg.Amount != 9900 {
		t.Fatalf("unexpected decoded message: %+v", msg)
	}
}
