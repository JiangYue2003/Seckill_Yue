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
}
