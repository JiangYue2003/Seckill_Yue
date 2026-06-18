package events

import "encoding/json"

type Envelope struct {
	EventID       string `json:"event_id"`
	EventType     string `json:"event_type"`
	OccurredAt    int64  `json:"occurred_at"`
	AggregateType string `json:"aggregate_type"`
	AggregateID   string `json:"aggregate_id"`
	TraceID       string `json:"trace_id"`
	Source        string `json:"source"`
	Version       int32  `json:"version"`
}

type ReservationCreatedInput struct {
	EventID          string
	EventType        string
	OccurredAt       int64
	AggregateID      string
	TraceID          string
	Source           string
	Version          int32
	MessageID        string
	ReservationID    string
	OrderID          string
	UserID           int64
	SeckillProductID int64
	ProductID        int64
	Quantity         int64
	SeckillPrice     int64
	Amount           int64
	ShardNo          int32
	Status           int32
	ExpireAt         int64
}

type OrderCreatedInput struct {
	EventID          string
	EventType        string
	OccurredAt       int64
	AggregateID      string
	TraceID          string
	Source           string
	Version          int32
	MessageID        string
	ReservationID    string
	OrderID          string
	UserID           int64
	SeckillProductID int64
	ProductID        int64
	Quantity         int64
	Amount           int64
	ShardNo          int32
	Status           int32
	OrderType        int32
	PayStatus        int32
}

type PaymentSucceededInput struct {
	EventID           string
	EventType         string
	OccurredAt        int64
	AggregateID       string
	TraceID           string
	Source            string
	Version           int32
	MessageID         string
	ReservationID     string
	OrderID           string
	PaymentID         string
	UserID            int64
	SeckillProductID  int64
	ProductID         int64
	Quantity          int64
	Amount            int64
	ShardNo           int32
	Status            int32
	Channel           string
	ThirdPartyTradeNo string
	PaidAt            int64
	CallbackID        string
}

type OrderCompletedInput struct {
	EventID          string
	EventType        string
	OccurredAt       int64
	AggregateID      string
	TraceID          string
	Source           string
	Version          int32
	MessageID        string
	ReservationID    string
	OrderID          string
	PaymentID        string
	UserID           int64
	SeckillProductID int64
	ProductID        int64
	Quantity         int64
	Amount           int64
	ShardNo          int32
	Status           int32
	OrderType        int32
	PayStatus        int32
	PaidAt           int64
}

type ReservationReleasedInput struct {
	EventID          string
	EventType        string
	OccurredAt       int64
	AggregateID      string
	TraceID          string
	Source           string
	Version          int32
	MessageID        string
	ReservationID    string
	OrderID          string
	UserID           int64
	SeckillProductID int64
	ProductID        int64
	Quantity         int64
	Amount           int64
	ShardNo          int32
	FromStatus       int32
	Status           int32
	Reason           string
}

type ReservationAdvancedInput struct {
	EventID          string
	EventType        string
	OccurredAt       int64
	AggregateID      string
	TraceID          string
	Source           string
	Version          int32
	MessageID        string
	ReservationID    string
	OrderID          string
	PaymentID        string
	UserID           int64
	SeckillProductID int64
	ProductID        int64
	Quantity         int64
	Amount           int64
	ShardNo          int32
	FromStatus       int32
	Status           int32
	Reason           string
}

type PaymentRequestedInput struct {
	EventID          string
	EventType        string
	OccurredAt       int64
	AggregateID      string
	TraceID          string
	Source           string
	Version          int32
	MessageID        string
	ReservationID    string
	OrderID          string
	PaymentID        string
	UserID           int64
	SeckillProductID int64
	ProductID        int64
	Quantity         int64
	Amount           int64
	ShardNo          int32
	Status           int32
	Channel          string
}

func BuildReservationCreatedPayload(in ReservationCreatedInput) (string, error) {
	return marshalPayload(map[string]any{
		"event_id":           in.EventID,
		"event_type":         chooseEventType(in.EventType, "reservation.created"),
		"occurred_at":        in.OccurredAt,
		"aggregate_type":     "reservation",
		"aggregate_id":       in.AggregateID,
		"trace_id":           in.TraceID,
		"source":             in.Source,
		"version":            chooseVersion(in.Version),
		"message_id":         in.MessageID,
		"reservation_id":     in.ReservationID,
		"order_id":           in.OrderID,
		"user_id":            in.UserID,
		"seckill_product_id": in.SeckillProductID,
		"product_id":         in.ProductID,
		"quantity":           in.Quantity,
		"seckill_price":      in.SeckillPrice,
		"amount":             in.Amount,
		"shard_no":           in.ShardNo,
		"status":             in.Status,
		"expire_at":          in.ExpireAt,
	})
}

func BuildOrderCreatedPayload(in OrderCreatedInput) (string, error) {
	return marshalPayload(map[string]any{
		"event_id":           in.EventID,
		"event_type":         chooseEventType(in.EventType, "order.created"),
		"occurred_at":        in.OccurredAt,
		"aggregate_type":     "order",
		"aggregate_id":       in.AggregateID,
		"trace_id":           in.TraceID,
		"source":             in.Source,
		"version":            chooseVersion(in.Version),
		"message_id":         in.MessageID,
		"reservation_id":     in.ReservationID,
		"order_id":           in.OrderID,
		"user_id":            in.UserID,
		"seckill_product_id": in.SeckillProductID,
		"product_id":         in.ProductID,
		"quantity":           in.Quantity,
		"amount":             in.Amount,
		"shard_no":           in.ShardNo,
		"status":             in.Status,
		"order_type":         in.OrderType,
		"pay_status":         in.PayStatus,
	})
}

func BuildPaymentSucceededPayload(in PaymentSucceededInput) (string, error) {
	return marshalPayload(map[string]any{
		"event_id":             in.EventID,
		"event_type":           chooseEventType(in.EventType, "payment.succeeded"),
		"occurred_at":          in.OccurredAt,
		"aggregate_type":       "payment",
		"aggregate_id":         in.AggregateID,
		"trace_id":             in.TraceID,
		"source":               in.Source,
		"version":              chooseVersion(in.Version),
		"message_id":           in.MessageID,
		"reservation_id":       in.ReservationID,
		"order_id":             in.OrderID,
		"payment_id":           in.PaymentID,
		"user_id":              in.UserID,
		"seckill_product_id":   in.SeckillProductID,
		"product_id":           in.ProductID,
		"quantity":             in.Quantity,
		"amount":               in.Amount,
		"shard_no":             in.ShardNo,
		"status":               in.Status,
		"channel":              in.Channel,
		"third_party_trade_no": in.ThirdPartyTradeNo,
		"paid_at":              in.PaidAt,
		"callback_id":          in.CallbackID,
	})
}

func BuildOrderCompletedPayload(in OrderCompletedInput) (string, error) {
	return marshalPayload(map[string]any{
		"event_id":           in.EventID,
		"event_type":         chooseEventType(in.EventType, "order.completed"),
		"occurred_at":        in.OccurredAt,
		"aggregate_type":     "order",
		"aggregate_id":       in.AggregateID,
		"trace_id":           in.TraceID,
		"source":             in.Source,
		"version":            chooseVersion(in.Version),
		"message_id":         in.MessageID,
		"reservation_id":     in.ReservationID,
		"order_id":           in.OrderID,
		"payment_id":         in.PaymentID,
		"user_id":            in.UserID,
		"seckill_product_id": in.SeckillProductID,
		"product_id":         in.ProductID,
		"quantity":           in.Quantity,
		"amount":             in.Amount,
		"shard_no":           in.ShardNo,
		"status":             in.Status,
		"order_type":         in.OrderType,
		"pay_status":         in.PayStatus,
		"paid_at":            in.PaidAt,
	})
}

func BuildReservationReleasedPayload(in ReservationReleasedInput) (string, error) {
	return marshalPayload(map[string]any{
		"event_id":           in.EventID,
		"event_type":         chooseEventType(in.EventType, "reservation.released"),
		"occurred_at":        in.OccurredAt,
		"aggregate_type":     "reservation",
		"aggregate_id":       in.AggregateID,
		"trace_id":           in.TraceID,
		"source":             in.Source,
		"version":            chooseVersion(in.Version),
		"message_id":         in.MessageID,
		"reservation_id":     in.ReservationID,
		"order_id":           in.OrderID,
		"user_id":            in.UserID,
		"seckill_product_id": in.SeckillProductID,
		"product_id":         in.ProductID,
		"quantity":           in.Quantity,
		"amount":             in.Amount,
		"shard_no":           in.ShardNo,
		"from_status":        in.FromStatus,
		"status":             in.Status,
		"reason":             in.Reason,
	})
}

func BuildReservationAdvancedPayload(in ReservationAdvancedInput) (string, error) {
	return marshalPayload(map[string]any{
		"event_id":           in.EventID,
		"event_type":         chooseEventType(in.EventType, "reservation.advanced"),
		"occurred_at":        in.OccurredAt,
		"aggregate_type":     "reservation",
		"aggregate_id":       in.AggregateID,
		"trace_id":           in.TraceID,
		"source":             in.Source,
		"version":            chooseVersion(in.Version),
		"message_id":         in.MessageID,
		"reservation_id":     in.ReservationID,
		"order_id":           in.OrderID,
		"payment_id":         in.PaymentID,
		"user_id":            in.UserID,
		"seckill_product_id": in.SeckillProductID,
		"product_id":         in.ProductID,
		"quantity":           in.Quantity,
		"amount":             in.Amount,
		"shard_no":           in.ShardNo,
		"from_status":        in.FromStatus,
		"status":             in.Status,
		"reason":             in.Reason,
	})
}

func BuildPaymentRequestedPayload(in PaymentRequestedInput) (string, error) {
	return marshalPayload(map[string]any{
		"event_id":           in.EventID,
		"event_type":         chooseEventType(in.EventType, "payment.requested"),
		"occurred_at":        in.OccurredAt,
		"aggregate_type":     "payment",
		"aggregate_id":       in.AggregateID,
		"trace_id":           in.TraceID,
		"source":             in.Source,
		"version":            chooseVersion(in.Version),
		"message_id":         in.MessageID,
		"reservation_id":     in.ReservationID,
		"order_id":           in.OrderID,
		"payment_id":         in.PaymentID,
		"user_id":            in.UserID,
		"seckill_product_id": in.SeckillProductID,
		"product_id":         in.ProductID,
		"quantity":           in.Quantity,
		"amount":             in.Amount,
		"shard_no":           in.ShardNo,
		"status":             in.Status,
		"channel":            in.Channel,
	})
}

func marshalPayload(payload map[string]any) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func chooseEventType(current, fallback string) string {
	if current != "" {
		return current
	}
	return fallback
}

func chooseVersion(version int32) int32 {
	if version > 0 {
		return version
	}
	return 1
}
