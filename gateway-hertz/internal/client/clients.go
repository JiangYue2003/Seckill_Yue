package client

import (
	"fmt"

	order "seckill-mall/common/order"
	product "seckill-mall/common/product"
	seckill "seckill-mall/common/seckill"
	user "seckill-mall/common/user"
	"seckill-mall/gateway-hertz/internal/config"

	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
)

// ClientManager gRPC 客户端管理器
type ClientManager struct {
	UserService    user.UserServiceClient
	ProductService product.ProductServiceClient
	SeckillService seckill.SeckillServiceClient
	OrderService   order.OrderServiceClient
}

// NewClientManager 创建客户端管理器
func NewClientManager(c config.Config) (*ClientManager, error) {
	userSvc, err := newUserServiceClient(c.UserService)
	if err != nil {
		return nil, fmt.Errorf("user service: %w", err)
	}
	productSvc, err := newProductServiceClient(c.ProductService)
	if err != nil {
		return nil, fmt.Errorf("product service: %w", err)
	}
	seckillSvc, err := newSeckillServiceClient(c.SeckillService)
	if err != nil {
		return nil, fmt.Errorf("seckill service: %w", err)
	}
	orderSvc, err := newOrderServiceClient(c.OrderService)
	if err != nil {
		return nil, fmt.Errorf("order service: %w", err)
	}

	return &ClientManager{
		UserService:    userSvc,
		ProductService: productSvc,
		SeckillService: seckillSvc,
		OrderService:   orderSvc,
	}, nil
}

func newUserServiceClient(conf zrpc.RpcClientConf) (user.UserServiceClient, error) {
	c, err := zrpc.NewClient(conf)
	if err != nil {
		return nil, err
	}
	return user.NewUserServiceClient(c.Conn()), nil
}

func newProductServiceClient(conf zrpc.RpcClientConf) (product.ProductServiceClient, error) {
	c, err := zrpc.NewClient(conf)
	if err != nil {
		return nil, err
	}
	return product.NewProductServiceClient(c.Conn()), nil
}

func newSeckillServiceClient(conf zrpc.RpcClientConf) (seckill.SeckillServiceClient, error) {
	c, err := zrpc.NewClient(conf, zrpc.WithDialOption(
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`),
	))
	if err != nil {
		return nil, err
	}
	return seckill.NewSeckillServiceClient(c.Conn()), nil
}

func newOrderServiceClient(conf zrpc.RpcClientConf) (order.OrderServiceClient, error) {
	c, err := zrpc.NewClient(conf)
	if err != nil {
		return nil, err
	}
	return order.NewOrderServiceClient(c.Conn()), nil
}
