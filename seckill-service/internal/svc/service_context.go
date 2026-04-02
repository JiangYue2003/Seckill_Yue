package svc

import (
	"seckill-mall/seckill-service/internal/config"
	"seckill-mall/seckill-service/internal/mq"
	"seckill-mall/seckill-service/internal/redis"

	"github.com/zeromicro/go-zero/core/logx"
)

type ServiceContext struct {
	Config        config.Config
	Redis         *redis.SeckillRedis
	AsyncProducer *mq.AsyncProducer
}

func NewServiceContext(c config.Config) *ServiceContext {
	// 初始化 Redis
	redisClient, err := redis.NewSeckillRedis(c.SeckillRedis.Host)
	if err != nil {
		logx.Errorf("failed to initialize redis: %v", err)
		panic(err)
	}

	// 初始化 RabbitMQ 同步生产者（底层引擎）
	producer, err := mq.NewProducer(c.RabbitMQ.URL, c.RabbitMQ.Exchange, c.RabbitMQ.RoutingKey)
	if err != nil {
		logx.Errorf("failed to initialize RabbitMQ producer: %v", err)
		panic(err)
	}

	// 初始化异步 MQ 生产者（上层封装，带 Channel 缓冲和后台 Worker）
	asyncProducer := mq.NewAsyncProducer(
		producer,
		c.AsyncProducer.BufferSize,
		c.AsyncProducer.WorkerCount,
		c.AsyncProducer.RetryCount,
		c.AsyncProducer.RetryInterval,
	)

	return &ServiceContext{
		Config:        c,
		Redis:         redisClient,
		AsyncProducer: asyncProducer,
	}
}
