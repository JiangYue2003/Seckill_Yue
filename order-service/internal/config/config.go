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
		URL         string // amqp://user:pass@host:port/
		Exchange    string // 交换机名称
		RoutingKey  string // 路由键
		ConsumerTag string // 消费者标识
	}

	// Product Service gRPC 配置（通过 etcd 发现）
	ProductService zrpc.RpcClientConf
}
