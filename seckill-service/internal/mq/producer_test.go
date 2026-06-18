package mq

import (
	"encoding/json"
	"reflect"
	"testing"

	"seckill-mall/seckill-service/internal/model/entity"
)

func setIntField(t *testing.T, target any, field string, value int64) {
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
	switch fv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		fv.SetInt(value)
	default:
		t.Fatalf("field %q on %T is not int kind, got %s", field, target, fv.Kind())
	}
}

func TestBuildEventPublishMessageRoutesReservationCreatedToOrderRoutingKey(t *testing.T) {
	event := entity.EventOutbox{
		EventType: "reservation.created",
		PayloadJSON: `{
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
			"shard_no":3,
			"status":0,
			"expire_at":1710000300
		}`,
	}

	routingKey, body, err := buildEventPublishMessage("custom.order", "custom.delay", event)
	if err != nil {
		t.Fatalf("buildEventPublishMessage() error = %v", err)
	}
	if routingKey != "custom.order" {
		t.Fatalf("expected order routing key, got %s", routingKey)
	}

	var msg SeckillOrderMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
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

	var bodyMap map[string]any
	if err := json.Unmarshal(body, &bodyMap); err != nil {
		t.Fatalf("json.Unmarshal(bodyMap) error = %v", err)
	}
	if got, ok := bodyMap["shard_no"]; !ok || got != float64(3) {
		t.Fatalf("expected shard_no=3 in published body, got %v", bodyMap)
	}
}

func TestBuildEventPublishMessageRoutesReservationTimeoutCheckToDelayRoutingKey(t *testing.T) {
	event := entity.EventOutbox{
		EventType:   "reservation.timeout.check",
		PayloadJSON: `{"message_id":"O2","order_id":"O2","user_id":8,"seckill_product_id":102,"shard_no":9}`,
	}

	routingKey, body, err := buildEventPublishMessage("custom.order", "custom.delay", event)
	if err != nil {
		t.Fatalf("buildEventPublishMessage() error = %v", err)
	}
	if routingKey != "custom.delay" {
		t.Fatalf("expected delay routing key, got %s", routingKey)
	}

	var msg SeckillOrderMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if msg.OrderId != "O2" {
		t.Fatalf("expected order_id O2, got %s", msg.OrderId)
	}

	var bodyMap map[string]any
	if err := json.Unmarshal(body, &bodyMap); err != nil {
		t.Fatalf("json.Unmarshal(bodyMap) error = %v", err)
	}
	if got, ok := bodyMap["shard_no"]; !ok || got != float64(9) {
		t.Fatalf("expected shard_no=9 in published body, got %v", bodyMap)
	}
}

func TestBuildEventPublishMessageRoutesGenericEventsToEventPrefix(t *testing.T) {
	event := entity.EventOutbox{
		EventType:   "payment.succeeded",
		PayloadJSON: `{"payment_id":"pay-1","order_id":"O3"}`,
	}

	routingKey, body, err := buildEventPublishMessage("custom.order", "custom.delay", event)
	if err != nil {
		t.Fatalf("buildEventPublishMessage() error = %v", err)
	}
	if routingKey != "event.payment.succeeded" {
		t.Fatalf("expected generic event routing key, got %s", routingKey)
	}
	if string(body) != event.PayloadJSON {
		t.Fatalf("expected payload passthrough, got %s", string(body))
	}
}
