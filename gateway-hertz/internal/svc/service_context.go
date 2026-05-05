package svc

import (
	"seckill-mall/gateway-hertz/internal/client"
	"seckill-mall/gateway-hertz/internal/config"

	"github.com/redis/go-redis/v9"
)

// ServiceContext 服务上下文，持有所有共享依赖
type ServiceContext struct {
	Config  config.Config
	Clients *client.ClientManager
	Redis   *redis.Client
}

// NewServiceContext 创建服务上下文
func NewServiceContext(c config.Config, clients *client.ClientManager) *ServiceContext {
	rdb := redis.NewClient(&redis.Options{
		Addr: c.RedisHost,
	})
	return &ServiceContext{
		Config:  c,
		Clients: clients,
		Redis:   rdb,
	}
}
