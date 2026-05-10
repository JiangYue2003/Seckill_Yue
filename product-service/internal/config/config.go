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
		Mode     string   `json:",optional"` // "single"(默认) | "cluster" | "sentinel"
		Addr     string   `json:",optional"` // single/sentinel: "host:port"
		Addrs    []string `json:",optional"` // cluster/sentinel: 节点列表
		Password string   `json:",optional"`
	}
}
