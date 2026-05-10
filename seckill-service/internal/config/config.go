package config

import (
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf

	Dev struct {
		ResetLogsOnStart bool `json:"resetLogsOnStart,optional"`
	}

	// Redis 配置
	SeckillRedis struct {
		Mode           string   // "single"(默认) | "cluster" | "sentinel"
		Addr           string   // single/sentinel: "host:port"
		Addrs          []string // cluster/sentinel: 节点列表
		MasterName     string   // sentinel 专用
		Password       string
		DB             int
		PoolSize       int
		MinIdleConns   int
		DialTimeoutMs  int
		ReadTimeoutMs  int
		WriteTimeoutMs int
		PoolTimeoutMs  int
	}

	// 秒杀商品元数据本地缓存
	ProductMetaCache struct {
		Enabled        bool
		RefreshSeconds int64
		ScanCount      int64
	}

	// RocketMQ 配置
	RocketMQ struct {
		NameServer    string // NameServer 地址，如 "localhost:9876"
		ProducerGroup string // 生产者组名
		OrderTopic    string // 主链路 Topic
		CheckTopic    string // 超时检查 Topic（延迟消息）
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

	// 秒杀商品ID布隆过滤器配置
	Bloom struct {
		Enabled                 bool
		ExpectedItems           int64
		FalsePositiveRate       float64
		NegativeCacheTTLSeconds int64
		FallbackVerifyEnabled   bool
	}
}
