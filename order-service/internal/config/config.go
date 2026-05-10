package config

import (
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf

	// MySQL 数据库配置
	MySQL struct {
		DataSource string
	}

	// Redis 缓存配置
	MyCache cache.CacheConf

	// RocketMQ 配置
	RocketMQ struct {
		NameServer         string `json:",optional"` // NameServer 地址
		OrderConsumerGroup string `json:",optional"` // 主链路消费者组
		CheckConsumerGroup string `json:",optional"` // 超时检查消费者组
		DLQConsumerGroup   string `json:",optional"` // 死信队列监控消费者组
		OrderTopic         string `json:",optional"` // 主链路 Topic
		CheckTopic         string `json:",optional"` // 超时检查 Topic
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
