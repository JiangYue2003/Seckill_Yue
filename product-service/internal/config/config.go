package config

import (
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf

	// MySQL 数据库配置
	MySQL struct {
		DataSource string
	}

	// Redis 缓存配置（用于秒杀库存同步）
	SeckillRedis struct {
		Host string
	}
}
