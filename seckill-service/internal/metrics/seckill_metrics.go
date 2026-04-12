package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// SeckillRequestsTotal records final response result of Seckill RPC.
	SeckillRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "seckill_requests_total",
			Help: "Total number of seckill requests by final result.",
		},
		[]string{"result"},
	)

	// SeckillRequestDurationSeconds tracks end-to-end latency for Seckill RPC.
	SeckillRequestDurationSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "seckill_request_duration_seconds",
			Help:    "Latency of seckill requests by final result.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"result"},
	)

	// SeckillLocalStockRejectTotal counts local in-memory stock prefilter rejects.
	SeckillLocalStockRejectTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "seckill_local_stock_reject_total",
			Help: "Total local stock prefilter rejects before Redis Lua execution.",
		},
	)

	// SeckillMQEnqueueTotal counts async MQ enqueue attempts.
	SeckillMQEnqueueTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "seckill_mq_enqueue_total",
			Help: "Total MQ enqueue attempts for seckill flow.",
		},
		[]string{"queue", "result"},
	)

	// SeckillQuotaAllocateTotal counts quota allocation results.
	SeckillQuotaAllocateTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "seckill_quota_allocate_total",
			Help: "Total local quota allocation attempts.",
		},
		[]string{"result"},
	)

	// SeckillQuotaReclaimTotal counts reclaimed quota amount from expired leases.
	SeckillQuotaReclaimTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "seckill_quota_reclaim_total",
			Help: "Total reclaimed quota amount from expired leases.",
		},
	)

	// SeckillQuotaLeaseRenewTotal counts lease renew attempts.
	SeckillQuotaLeaseRenewTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "seckill_quota_lease_renew_total",
			Help: "Total lease renew attempts for local quota.",
		},
		[]string{"result"},
	)
)
