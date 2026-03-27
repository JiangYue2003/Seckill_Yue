package client

import (
	"fmt"

	"seckill-mall/gateway/internal/config"
	order "seckill-mall/order-service/order"
	product "seckill-mall/product-service/product"
	seckill "seckill-mall/seckill-service/seckill"
	user "seckill-mall/user-service/user"

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

// newUserServiceClient 创建用户服务客户端
func newUserServiceClient(conf zrpc.RpcClientConf) (user.UserServiceClient, error) {
	client, err := zrpc.NewClient(conf)
	if err != nil {
		return nil, fmt.Errorf("new user service client: %w", err)
	}
	return user.NewUserServiceClient(client.Conn()), nil
}

// newProductServiceClient 创建商品服务客户端
func newProductServiceClient(conf zrpc.RpcClientConf) (product.ProductServiceClient, error) {
	client, err := zrpc.NewClient(conf)
	if err != nil {
		return nil, fmt.Errorf("new product service client: %w", err)
	}
	return product.NewProductServiceClient(client.Conn()), nil
}

// newSeckillServiceClient 创建秒杀服务客户端
func newSeckillServiceClient(conf zrpc.RpcClientConf) (seckill.SeckillServiceClient, error) {
	client, err := zrpc.NewClient(conf)
	if err != nil {
		return nil, fmt.Errorf("new seckill service client: %w", err)
	}
	return seckill.NewSeckillServiceClient(client.Conn()), nil
}

// newOrderServiceClient 创建订单服务客户端
func newOrderServiceClient(conf zrpc.RpcClientConf) (order.OrderServiceClient, error) {
	client, err := zrpc.NewClient(conf)
	if err != nil {
		return nil, fmt.Errorf("new order service client: %w", err)
	}
	return order.NewOrderServiceClient(client.Conn()), nil
}

// GetConn 返回指定服务的 grpc 连接（用于流式 RPC）
func (m *ClientManager) GetConn(service string) (*grpc.ClientConn, error) {
	switch service {
	case "user":
		return m.UserService.(interface{ GetConn() *grpc.ClientConn }).GetConn(), nil
	case "product":
		return m.ProductService.(interface{ GetConn() *grpc.ClientConn }).GetConn(), nil
	case "seckill":
		return m.SeckillService.(interface{ GetConn() *grpc.ClientConn }).GetConn(), nil
	case "order":
		return m.OrderService.(interface{ GetConn() *grpc.ClientConn }).GetConn(), nil
	default:
		return nil, fmt.Errorf("unknown service: %s", service)
	}
}
