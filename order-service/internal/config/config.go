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

	// RabbitMQ 配置
	RabbitMQ struct {
		URL              string `json:",optional"` // 连接串
		Exchange         string `json:",optional"` // direct exchange
		OrderRoutingKey  string `json:",optional"` // reservation.created 路由键
		CheckRoutingKey  string `json:",optional"` // reservation.timeout.check 路由键
		OrderQueue       string `json:",optional"` // 主消费队列
		CheckQueue       string `json:",optional"` // 超时检查队列
		DeadQueue        string `json:",optional"` // 死信监控队列
		ConsumerTag      string `json:",optional"` // 主消费 consumer tag
		CheckConsumerTag string `json:",optional"` // 检查消费 consumer tag
		DLQConsumerTag   string `json:",optional"` // 死信消费 consumer tag
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
