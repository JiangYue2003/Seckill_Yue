package config

import (
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	// HTTP 服务配置
	Host string `json:"host"`
	Port int    `json:"port"`

	// JWT 配置
	JWT struct {
		AccessSecret  string `json:"accessSecret"`
		AccessExpire  int64  `json:"accessExpire"`
		RefreshSecret string `json:"refreshSecret"`
		RefreshExpire int64  `json:"refreshExpire"`
	} `json:"jwt"`

	// Redis 缓存（用于限流）
	Redis cache.CacheConf `json:"redis"`

	// Redis 连接串（用于限流器）
	RedisHost string `json:"redisHost"`

	// gRPC 服务（通过 etcd 服务发现）
	UserService    zrpc.RpcClientConf `json:"userService"`
	ProductService zrpc.RpcClientConf `json:"productService"`
	SeckillService zrpc.RpcClientConf `json:"seckillService"`
	OrderService   zrpc.RpcClientConf `json:"orderService"`
}
