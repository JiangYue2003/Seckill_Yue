package config

import (
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

	// Redis 缓存配置（用于秒杀库存同步）
	SeckillRedis struct {
		Mode     string   // "single"(默认) | "cluster" | "sentinel"
		Addr     string   // single/sentinel: "host:port"（原 Host）
		Addrs    []string // cluster/sentinel: 节点列表
		Password string
	}
}
