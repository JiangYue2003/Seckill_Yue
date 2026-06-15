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
	Consumer          *mq.RocketMQOrderConsumer // 主处理队列消费者
	CheckConsumer     *mq.RocketMQCheckConsumer // 超时检查队列消费者
	DLQConsumer       *mq.RocketMQDLQConsumer   // 死信队列监控消费者
	OrderService      *service.OrderService
	ProductServiceRPC *rpc.ProductServiceClient
	SeckillServiceRPC *rpc.SeckillServiceClient
}

func NewServiceContext(c config.Config) *ServiceContext {
	db, err := model.NewDB(c)
	if err != nil {
		logx.Errorf("failed to initialize db: %v", err)
		panic(err)
	}

	orderModel := model.NewOrderModelWithDB(db)
	seckillOrderModel := model.NewSeckillOrderModelWithDB(db)
	seckillOrderTxManager := model.NewSeckillOrderTxManager(db)
	// 初始化订单模型
	_, err = model.NewOrderModel(c)
	if err != nil {
		logx.Errorf("failed to initialize order model: %v", err)
		panic(err)
	}

	// 初始化 Product-Service RPC 客户端
	productSvc, err := rpc.NewProductServiceClient(c)
	if err != nil {
		logx.Errorf("failed to initialize product RPC client: %v", err)
		panic(err)
	}

	// 初始化订单服务（注入事务型落库器）
	orderService := service.NewOrderService(orderModel, seckillOrderModel, seckillOrderTxManager)
	orderService.SetProductServiceRPC(productSvc)

	seckillSvc, err := rpc.NewSeckillServiceClient(c)
	if err != nil {
		logx.Errorf("failed to initialize seckill RPC client: %v", err)
	} else {
		orderService.SetSeckillServiceRPC(seckillSvc)
	}

	rmqCfg := mq.RocketMQConsumerConfig{
		NameServer:         c.RocketMQ.NameServer,
		OrderConsumerGroup: c.RocketMQ.OrderConsumerGroup,
		CheckConsumerGroup: c.RocketMQ.CheckConsumerGroup,
		DLQConsumerGroup:   c.RocketMQ.DLQConsumerGroup,
		OrderTopic:         c.RocketMQ.OrderTopic,
		CheckTopic:         c.RocketMQ.CheckTopic,
	}

	// 初始化主链路消费者
	processFunc := func(msg *mq.SeckillOrderMessage) error {
		return orderService.ProcessSeckillOrder(msg)
	}
	consumer, err := mq.NewRocketMQOrderConsumer(rmqCfg, processFunc)
	if err != nil {
		logx.Errorf("failed to initialize RocketMQ order consumer: %v", err)
		consumer = nil
	}

	// 初始化超时检查消费者
	checkProcessFunc := func(msg *mq.SeckillOrderMessage) error {
		return orderService.ProcessOrderTimeout(msg)
	}
	checkConsumer, err := mq.NewRocketMQCheckConsumer(rmqCfg, checkProcessFunc)
	if err != nil {
		logx.Errorf("failed to initialize RocketMQ check consumer: %v", err)
		checkConsumer = nil
	}

	// 初始化死信队列监控消费者
	dlqConsumer, err := mq.NewRocketMQDLQConsumer(rmqCfg)
	if err != nil {
		logx.Errorf("failed to initialize RocketMQ DLQ consumer: %v", err)
		dlqConsumer = nil
	}

	return &ServiceContext{
		Config:            c,
		OrderModel:        orderModel,
		SeckillOrderModel: seckillOrderModel,
		Consumer:          consumer,
		CheckConsumer:     checkConsumer,
		DLQConsumer:       dlqConsumer,
		OrderService:      orderService,
		ProductServiceRPC: productSvc,
		SeckillServiceRPC: seckillSvc,
	}
}
