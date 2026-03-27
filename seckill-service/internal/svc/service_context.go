package svc

import (
	"seckill-mall/seckill-service/internal/config"
	"seckill-mall/seckill-service/internal/mq"
	"seckill-mall/seckill-service/internal/redis"

	"github.com/zeromicro/go-zero/core/logx"
)

type ServiceContext struct {
	Config   config.Config
	Redis    *redis.SeckillRedis
	Producer *mq.Producer
}

func NewServiceContext(c config.Config) *ServiceContext {
	// 初始化 Redis
	redisClient, err := redis.NewSeckillRedis(c.SeckillRedis.Host)
	if err != nil {
		logx.Errorf("failed to initialize redis: %v", err)
		panic(err)
	}

	// 初始化 RabbitMQ 生产者
	producer, err := mq.NewProducer(c.RabbitMQ.URL, c.RabbitMQ.Exchange, c.RabbitMQ.RoutingKey)
	if err != nil {
		logx.Errorf("failed to initialize RabbitMQ producer: %v", err)
		panic(err)
	}

	return &ServiceContext{
		Config:   c,
		Redis:    redisClient,
		Producer: producer,
	}
}
