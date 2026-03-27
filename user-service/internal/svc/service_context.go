package svc

import (
	"seckill-mall/user-service/internal/config"
	"seckill-mall/user-service/internal/model"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

type ServiceContext struct {
	Config    config.Config
	UserModel model.UserModel
	Redis     *redis.Redis
}

func NewServiceContext(c config.Config) *ServiceContext {
	// 初始化用户模型
	userModel, err := model.NewUserModel(c)
	if err != nil {
		logx.Errorf("failed to initialize user model: %v", err)
		panic(err)
	}

	// 初始化 Redis
	redisConf := c.MyCache[0]
	redisClient := redis.New(redisConf.Host)

	return &ServiceContext{
		Config:    c,
		UserModel: userModel,
		Redis:     redisClient,
	}
}
