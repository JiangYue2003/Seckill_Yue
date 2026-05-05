package config

import "github.com/zeromicro/go-zero/zrpc"

// RateLimitConfig 限流配置
type RateLimitConfig struct {
	Enabled  bool   `json:"enabled"`
	Strategy string `json:"strategy"` // "token_bucket" | "sliding_window" | "ip_token_bucket"
	QPS      int    `json:"qps"`
	Capacity int    `json:"capacity"`
}

// TelemetryConfig 链路追踪配置
type TelemetryConfig struct {
	Endpoint string  `json:"endpoint"`
	Sampler  float64 `json:"sampler"`
	Disabled bool    `json:"disabled,optional"`
}

// Config 网关配置
type Config struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	Mode string `json:"mode,optional"`

	MetricsPort int `json:"metricsPort,optional"`

	JWT struct {
		AccessSecret  string `json:"accessSecret"`
		AccessExpire  int64  `json:"accessExpire"`
		RefreshSecret string `json:"refreshSecret"`
		RefreshExpire int64  `json:"refreshExpire"`
	} `json:"jwt"`

	RedisHost string `json:"redisHost"`

	RateLimit RateLimitConfig `json:"rateLimit"`

	Telemetry TelemetryConfig `json:"telemetry"`

	UserService    zrpc.RpcClientConf `json:"userService"`
	ProductService zrpc.RpcClientConf `json:"productService"`
	SeckillService zrpc.RpcClientConf `json:"seckillService"`
	OrderService   zrpc.RpcClientConf `json:"orderService"`
}
