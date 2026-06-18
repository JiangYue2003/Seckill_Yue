package events

import (
	"encoding/json"
	"testing"
)

func assertPayloadHasShardNo(t *testing.T, payloadJSON string) {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if _, ok := payload["shard_no"]; !ok {
		t.Fatalf("expected key %q in payload, got %v", "shard_no", payload)
	}
}

func TestBuildReservationCreatedPayloadIncludesUnifiedEnvelope(t *testing.T) {
	payloadJSON, err := BuildReservationCreatedPayload(ReservationCreatedInput{
		EventID:          "evt-reservation-created-R1",
		EventType:        "reservation.created",
		OccurredAt:       1710000000,
		AggregateID:      "R1",
		TraceID:          "trace-R1",
		Source:           "seckill-service",
		Version:          1,
		MessageID:        "O1",
		ReservationID:    "R1",
		OrderID:          "O1",
		UserID:           7,
		SeckillProductID: 101,
		ProductID:        1001,
		Quantity:         1,
		SeckillPrice:     9900,
		Amount:           9900,
		Status:           0,
		ExpireAt:         1710000300,
	})
	if err != nil {
		t.Fatalf("BuildReservationCreatedPayload() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	required := []string{
		"event_id",
		"event_type",
		"occurred_at",
		"aggregate_type",
		"aggregate_id",
		"trace_id",
		"source",
		"version",
		"message_id",
		"reservation_id",
		"order_id",
		"user_id",
		"seckill_product_id",
		"product_id",
		"quantity",
		"seckill_price",
		"amount",
		"status",
		"expire_at",
	}
	for _, key := range required {
		if _, ok := payload[key]; !ok {
			t.Fatalf("expected key %q in payload, got %v", key, payload)
		}
	}
	if got := payload["aggregate_type"]; got != "reservation" {
		t.Fatalf("expected aggregate_type=reservation, got %v", got)
	}
	assertPayloadHasShardNo(t, payloadJSON)
}

func TestBuildOrderCreatedPayloadIncludesUnifiedEnvelope(t *testing.T) {
	payloadJSON, err := BuildOrderCreatedPayload(OrderCreatedInput{
		EventID:          "evt-M1",
		EventType:        "order.created",
		OccurredAt:       1710000001,
		AggregateID:      "O1",
		TraceID:          "trace-O1",
		Source:           "order-service",
		Version:          1,
		MessageID:        "M1",
		ReservationID:    "R1",
		OrderID:          "O1",
		UserID:           8,
		SeckillProductID: 102,
		ProductID:        1002,
		Quantity:         2,
		Amount:           18800,
		Status:           2,
		OrderType:        1,
		PayStatus:        0,
	})
	if err != nil {
		t.Fatalf("BuildOrderCreatedPayload() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	required := []string{
		"event_id",
		"event_type",
		"occurred_at",
		"aggregate_type",
		"aggregate_id",
		"trace_id",
		"source",
		"version",
		"message_id",
		"reservation_id",
		"order_id",
		"user_id",
		"seckill_product_id",
		"product_id",
		"quantity",
		"amount",
		"status",
		"order_type",
		"pay_status",
	}
	for _, key := range required {
		if _, ok := payload[key]; !ok {
			t.Fatalf("expected key %q in payload, got %v", key, payload)
		}
	}
	if got := payload["aggregate_type"]; got != "order" {
		t.Fatalf("expected aggregate_type=order, got %v", got)
	}
	assertPayloadHasShardNo(t, payloadJSON)
}

func TestBuildPaymentSucceededPayloadIncludesUnifiedEnvelope(t *testing.T) {
	payloadJSON, err := BuildPaymentSucceededPayload(PaymentSucceededInput{
		EventID:           "evt-payment-succeeded-C1",
		EventType:         "payment.succeeded",
		OccurredAt:        1710000002,
		AggregateID:       "P1",
		TraceID:           "trace-P1",
		Source:            "order-service",
		Version:           1,
		MessageID:         "O1",
		ReservationID:     "R1",
		OrderID:           "O1",
		PaymentID:         "P1",
		UserID:            9,
		SeckillProductID:  103,
		ProductID:         1003,
		Quantity:          1,
		Amount:            8800,
		Status:            2,
		Channel:           "mock_alipay",
		ThirdPartyTradeNo: "trade-1",
		PaidAt:            1710000002,
		CallbackID:        "C1",
	})
	if err != nil {
		t.Fatalf("BuildPaymentSucceededPayload() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	required := []string{
		"event_id",
		"event_type",
		"occurred_at",
		"aggregate_type",
		"aggregate_id",
		"trace_id",
		"source",
		"version",
		"message_id",
		"reservation_id",
		"order_id",
		"payment_id",
		"user_id",
		"seckill_product_id",
		"product_id",
		"quantity",
		"amount",
		"status",
		"channel",
		"third_party_trade_no",
		"paid_at",
		"callback_id",
	}
	for _, key := range required {
		if _, ok := payload[key]; !ok {
			t.Fatalf("expected key %q in payload, got %v", key, payload)
		}
	}
	if got := payload["aggregate_type"]; got != "payment" {
		t.Fatalf("expected aggregate_type=payment, got %v", got)
	}
	assertPayloadHasShardNo(t, payloadJSON)
}

func TestBuildOrderCompletedPayloadIncludesUnifiedEnvelope(t *testing.T) {
	payloadJSON, err := BuildOrderCompletedPayload(OrderCompletedInput{
		EventID:          "evt-order-completed-O1",
		EventType:        "order.completed",
		OccurredAt:       1710000003,
		AggregateID:      "O1",
		TraceID:          "trace-O1",
		Source:           "order-service",
		Version:          1,
		MessageID:        "O1",
		ReservationID:    "R1",
		OrderID:          "O1",
		PaymentID:        "P1",
		UserID:           10,
		SeckillProductID: 104,
		ProductID:        1004,
		Quantity:         1,
		Amount:           10800,
		Status:           5,
		OrderType:        1,
		PayStatus:        2,
		PaidAt:           1710000003,
	})
	if err != nil {
		t.Fatalf("BuildOrderCompletedPayload() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	required := []string{
		"event_id",
		"event_type",
		"occurred_at",
		"aggregate_type",
		"aggregate_id",
		"trace_id",
		"source",
		"version",
		"message_id",
		"reservation_id",
		"order_id",
		"payment_id",
		"user_id",
		"seckill_product_id",
		"product_id",
		"quantity",
		"amount",
		"status",
		"order_type",
		"pay_status",
		"paid_at",
	}
	for _, key := range required {
		if _, ok := payload[key]; !ok {
			t.Fatalf("expected key %q in payload, got %v", key, payload)
		}
	}
	if got := payload["aggregate_type"]; got != "order" {
		t.Fatalf("expected aggregate_type=order, got %v", got)
	}
	assertPayloadHasShardNo(t, payloadJSON)
}

func TestBuildReservationReleasedPayloadIncludesUnifiedEnvelope(t *testing.T) {
	payloadJSON, err := BuildReservationReleasedPayload(ReservationReleasedInput{
		EventID:          "evt-reservation-release-R2",
		EventType:        "reservation.released",
		OccurredAt:       1710000004,
		AggregateID:      "R2",
		TraceID:          "trace-R2",
		Source:           "seckill-service",
		Version:          1,
		MessageID:        "O2",
		ReservationID:    "R2",
		OrderID:          "O2",
		UserID:           11,
		SeckillProductID: 105,
		ProductID:        1005,
		Quantity:         1,
		Amount:           11800,
		FromStatus:       0,
		Status:           6,
		Reason:           "timeout_release",
	})
	if err != nil {
		t.Fatalf("BuildReservationReleasedPayload() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	required := []string{
		"event_id", "event_type", "occurred_at", "aggregate_type", "aggregate_id", "trace_id", "source", "version",
		"message_id", "reservation_id", "order_id", "user_id", "seckill_product_id", "product_id", "quantity", "amount",
		"from_status", "status", "reason",
	}
	for _, key := range required {
		if _, ok := payload[key]; !ok {
			t.Fatalf("expected key %q in payload, got %v", key, payload)
		}
	}
	assertPayloadHasShardNo(t, payloadJSON)
}

func TestBuildReservationAdvancedPayloadIncludesUnifiedEnvelope(t *testing.T) {
	payloadJSON, err := BuildReservationAdvancedPayload(ReservationAdvancedInput{
		EventID:          "evt-reservation-advanced-R3",
		EventType:        "reservation.advanced",
		OccurredAt:       1710000005,
		AggregateID:      "R3",
		TraceID:          "trace-R3",
		Source:           "seckill-service",
		Version:          1,
		MessageID:        "O3",
		ReservationID:    "R3",
		OrderID:          "O3",
		PaymentID:        "P3",
		UserID:           12,
		SeckillProductID: 106,
		ProductID:        1006,
		Quantity:         1,
		Amount:           12800,
		FromStatus:       3,
		Status:           5,
		Reason:           "payment.succeeded",
	})
	if err != nil {
		t.Fatalf("BuildReservationAdvancedPayload() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	required := []string{
		"event_id", "event_type", "occurred_at", "aggregate_type", "aggregate_id", "trace_id", "source", "version",
		"message_id", "reservation_id", "order_id", "payment_id", "user_id", "seckill_product_id", "product_id", "quantity", "amount",
		"from_status", "status", "reason",
	}
	for _, key := range required {
		if _, ok := payload[key]; !ok {
			t.Fatalf("expected key %q in payload, got %v", key, payload)
		}
	}
	assertPayloadHasShardNo(t, payloadJSON)
}

func TestBuildPaymentRequestedPayloadIncludesUnifiedEnvelope(t *testing.T) {
	payloadJSON, err := BuildPaymentRequestedPayload(PaymentRequestedInput{
		EventID:          "evt-payment-requested-P4",
		EventType:        "payment.requested",
		OccurredAt:       1710000006,
		AggregateID:      "P4",
		TraceID:          "trace-P4",
		Source:           "order-service",
		Version:          1,
		MessageID:        "O4",
		ReservationID:    "R4",
		OrderID:          "O4",
		PaymentID:        "P4",
		UserID:           13,
		SeckillProductID: 107,
		ProductID:        1007,
		Quantity:         1,
		Amount:           13800,
		Status:           1,
		Channel:          "mock_alipay",
	})
	if err != nil {
		t.Fatalf("BuildPaymentRequestedPayload() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	required := []string{
		"event_id", "event_type", "occurred_at", "aggregate_type", "aggregate_id", "trace_id", "source", "version",
		"message_id", "reservation_id", "order_id", "payment_id", "user_id", "seckill_product_id", "product_id", "quantity", "amount",
		"status", "channel",
	}
	for _, key := range required {
		if _, ok := payload[key]; !ok {
			t.Fatalf("expected key %q in payload, got %v", key, payload)
		}
	}
	assertPayloadHasShardNo(t, payloadJSON)
}
