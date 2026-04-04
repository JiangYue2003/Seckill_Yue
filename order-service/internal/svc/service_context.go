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
	Consumer          *mq.Consumer // 主处理队列消费者
	CheckConsumer     *mq.Consumer // 超时检查队列消费者
	OrderService      *service.OrderService
	ProductServiceRPC *rpc.ProductServiceClient
	SeckillServiceRPC *rpc.SeckillServiceClient
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
	productSvc, err := rpc.NewProductServiceClient(c)
	if err != nil {
		logx.Errorf("failed to initialize product RPC client: %v", err)
		panic(err)
	}

	// 初始化订单服务（注入 ProductServiceRPC + SeckillServiceRPC）
	orderService := service.NewOrderService(orderModel, seckillOrderModel)
	orderService.SetProductServiceRPC(productSvc)

	seckillSvc, err := rpc.NewSeckillServiceClient(c)
	if err != nil {
		logx.Errorf("failed to initialize seckill RPC client: %v", err)
	} else {
		orderService.SetSeckillServiceRPC(seckillSvc)
	}

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

	// 初始化超时检查队列消费者（消费 seckill_order_check_queue）
	checkProcessFunc := func(msg *mq.SeckillOrderMessage) error {
		return orderService.ProcessOrderTimeout(msg)
	}
	checkConsumer, err := mq.NewConsumer(
		c.RabbitMQ.URL,
		c.RabbitMQ.Exchange,
		mq.RoutingKeyCheck,
		mq.SeckillCheckQueueName,
		c.RabbitMQ.ConsumerTag+"_check",
		checkProcessFunc,
	)
	if err != nil {
		logx.Errorf("failed to initialize check consumer: %v", err)
		checkConsumer = nil
	}

	return &ServiceContext{
		Config:            c,
		OrderModel:        orderModel,
		SeckillOrderModel: seckillOrderModel,
		Consumer:          consumer,
		CheckConsumer:     checkConsumer,
		OrderService:      orderService,
		ProductServiceRPC: productSvc,
		SeckillServiceRPC: seckillSvc,
	}
}
