package config

import (
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf

	Dev struct {
		ResetLogsOnStart bool `json:"resetLogsOnStart,optional"`
	}

	// MySQL 数据库配置
	MySQL struct {
		DataSource string
	}

	// Redis 缓存配置
	MyCache cache.CacheConf

	// RocketMQ 配置
	RocketMQ struct {
		NameServer         string // NameServer 地址
		OrderConsumerGroup string // 主链路消费者组
		CheckConsumerGroup string // 超时检查消费者组
		DLQConsumerGroup   string // 死信队列监控消费者组
		OrderTopic         string // 主链路 Topic
		CheckTopic         string // 超时检查 Topic
	}

	// Product Service gRPC 配置（通过 etcd 发现）
	ProductService zrpc.RpcClientConf

	// Seckill Service gRPC 配置（通过 etcd 发现）
	SeckillService zrpc.RpcClientConf

	// Fallback 直连配置（etcd 不可用时使用）
	Fallback struct {
		ProductServiceEndpoint string
		SeckillServiceEndpoint string
	}
}
