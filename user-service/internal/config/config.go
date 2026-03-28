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

	// JWT 配置
	JWTConfig struct {
		AccessSecret  string // JWT 签名密钥
		RefreshSecret string // Refresh Token 签名密钥
		AccessExpire  int64  // Access Token 过期时间（秒）
		RefreshExpire int64  // Refresh Token 过期时间（秒）
	}
}
