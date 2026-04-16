package config

import (
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/trace"
	"github.com/zeromicro/go-zero/zrpc"
)

// RateLimitConfig 限流配置
type RateLimitConfig struct {
	Enabled  bool   `json:"enabled"`  // 是否启用限流
	Strategy string `json:"strategy"` // "token_bucket" | "sliding_window" | "ip_token_bucket"
	QPS      int    `json:"qps"`      // 令牌桶: 每秒补充令牌数; 滑动窗口: 每秒最大请求数
	Capacity int    `json:"capacity"` // 令牌桶: 桶容量(突发上限); 滑动窗口: 忽略
}

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

	// 限流配置
	RateLimit RateLimitConfig `json:"rateLimit"`

	// 链路追踪配置
	Telemetry trace.Config `json:"telemetry"`

	// gRPC 服务（通过 etcd 服务发现）
	UserService    zrpc.RpcClientConf `json:"userService"`
	ProductService zrpc.RpcClientConf `json:"productService"`
	SeckillService zrpc.RpcClientConf `json:"seckillService"`
	OrderService   zrpc.RpcClientConf `json:"orderService"`
}
