package svc

import (
	"seckill-mall/product-service/internal/config"
	"seckill-mall/product-service/internal/model"
	redisutil "seckill-mall/product-service/internal/redis"

	"github.com/zeromicro/go-zero/core/logx"
)

type ServiceContext struct {
	Config              config.Config
	ProductModel        model.ProductModel
	SeckillProductModel model.SeckillProductModel
	StockLogModel       model.StockLogModel
	SeckillRedis        *redisutil.SeckillRedis
}

func NewServiceContext(c config.Config) *ServiceContext {
	productModel, err := model.NewProductModel(c)
	if err != nil {
		logx.Errorf("failed to initialize product model: %v", err)
		panic(err)
	}

	seckillProductModel, err := model.NewSeckillProductModel(c)
	if err != nil {
		logx.Errorf("failed to initialize seckill product model: %v", err)
		panic(err)
	}

	stockLogModel, err := model.NewStockLogModel(c)
	if err != nil {
		logx.Errorf("failed to initialize stock log model: %v", err)
		panic(err)
	}

	seckillRedis, err := redisutil.NewSeckillRedis(c.SeckillRedis.Host)
	if err != nil {
		logx.Errorf("failed to initialize seckill redis: %v", err)
		seckillRedis = nil
	}

	return &ServiceContext{
		Config:              c,
		ProductModel:        productModel,
		SeckillProductModel: seckillProductModel,
		StockLogModel:       stockLogModel,
		SeckillRedis:        seckillRedis,
	}
}
