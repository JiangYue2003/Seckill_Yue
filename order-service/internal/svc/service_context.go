package svc

import (
	"seckill-mall/order-service/internal/config"
	"seckill-mall/order-service/internal/model"
	"seckill-mall/order-service/internal/mq"
	"seckill-mall/order-service/internal/rpc"
	"seckill-mall/order-service/internal/service"

	"github.com/zeromicro/go-zero/core/logx"
)

type ServiceContext struct {
	Config            config.Config
	OrderModel        model.OrderModel
	SeckillOrderModel model.SeckillOrderModel
	Consumer          *mq.Consumer
	OrderService      *service.OrderService
	ProductServiceRPC *rpc.ProductServiceClient
}

func NewServiceContext(c config.Config) *ServiceContext {
	// 初始化订单模型
	orderModel, err := model.NewOrderModel(c)
	if err != nil {
		logx.Errorf("failed to initialize order model: %v", err)
		panic(err)
	}

	// 初始化秒杀订单记录模型
	seckillOrderModel, err := model.NewSeckillOrderModel(c)
	if err != nil {
		logx.Errorf("failed to initialize seckill order model: %v", err)
		panic(err)
	}

	// 初始化 Product-Service RPC 客户端
	productSvc, _ := rpc.NewProductServiceClient(c)

	// 初始化订单服务
	orderService := service.NewOrderService(orderModel, seckillOrderModel)
	orderService.SetProductServiceRPC(productSvc)

	// 初始化 RabbitMQ 消费者
	processFunc := func(msg *mq.SeckillOrderMessage) error {
		return orderService.ProcessSeckillOrder(msg)
	}
	consumer, err := mq.NewConsumer(
		c.RabbitMQ.URL,
		c.RabbitMQ.Exchange,
		c.RabbitMQ.RoutingKey,
		mq.SeckillOrderQueueName, // 秒杀订单队列
		c.RabbitMQ.ConsumerTag,
		processFunc,
	)
	if err != nil {
		logx.Errorf("failed to initialize RabbitMQ consumer: %v", err)
		consumer = nil
	}

	return &ServiceContext{
		Config:            c,
		OrderModel:        orderModel,
		SeckillOrderModel: seckillOrderModel,
		Consumer:          consumer,
		OrderService:      orderService,
		ProductServiceRPC: productSvc,
	}
}
