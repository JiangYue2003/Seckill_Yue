package config

import (
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf

	// Redis 配置
	SeckillRedis struct {
		Host string
	}

	// RabbitMQ 配置
	RabbitMQ struct {
		URL        string // amqp://user:pass@host:port/
		Exchange   string // 交换机名称
		RoutingKey string // 路由键
	}

	// 异步 MQ 生产者配置
	AsyncProducer struct {
		BufferSize    int // 缓冲队列大小，默认 10000
		WorkerCount   int // 后台 Worker 协程数量，默认 4
		RetryCount    int // 最大重试次数，默认 3
		RetryInterval int // 基础重试间隔(秒)，默认 1（实际退避：1s, 3s, 10s...）
	}

	// 本地配额（批量领取式）配置
	LocalQuota struct {
		Enabled               bool
		BatchSize             int64
		LowWatermark          int64
		LeaseTTLSeconds       int64
		HeartbeatSeconds      int64
		ReaperIntervalSeconds int64
	}
}
