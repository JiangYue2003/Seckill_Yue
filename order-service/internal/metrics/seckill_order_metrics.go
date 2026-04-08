package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// OrderSeckillProcessTotal records the final result of seckill message processing.
	OrderSeckillProcessTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "order_seckill_process_total",
			Help: "Total number of seckill order consume results.",
		},
		[]string{"result"},
	)

	// OrderSeckillProcessDurationSeconds tracks processing latency for one seckill message.
	OrderSeckillProcessDurationSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "order_seckill_process_duration_seconds",
			Help:    "Latency of seckill order processing by result.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"result"},
	)

	// OrderSeckillTimeoutTotal records timeout-compensation outcomes.
	OrderSeckillTimeoutTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "order_seckill_timeout_total",
			Help: "Total timeout compensation outcomes for seckill orders.",
		},
		[]string{"result"},
	)
)
