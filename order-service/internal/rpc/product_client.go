package rpc

import (
	"context"
	"fmt"
	"time"

	"seckill-mall/order-service/internal/config"
	"seckill-mall/product-service/product"

	"github.com/zeromicro/go-zero/zrpc"
)

// ProductServiceClient 商品服务 RPC 客户端
type ProductServiceClient struct {
	client product.ProductServiceClient
}

// NewProductServiceClient 创建商品服务客户端
// 优先使用 etcd 发现，fallback 到 localhost 直连
func NewProductServiceClient(c config.Config) (*ProductServiceClient, error) {
	client, err := buildProductClient(c.ProductService, 8082)
	if err != nil {
		return nil, err
	}

	return &ProductServiceClient{
		client: product.NewProductServiceClient(client.Conn()),
	}, nil
}

// buildProductClient 构建 product-service 客户端：优先 etcd，fallback 到 localhost
func buildProductClient(conf zrpc.RpcClientConf, port int) (zrpc.Client, error) {
	if len(conf.Etcd.Hosts) == 0 {
		conf.Endpoints = []string{fmt.Sprintf("127.0.0.1:%d", port)}
	}
	client, err := zrpc.NewClient(conf)
	if err != nil {
		return nil, err
	}
	return client, nil
}

// DeductStock 扣减库存
func (c *ProductServiceClient) DeductStock(ctx context.Context, productId, quantity int64, orderId string) error {
	if c == nil || c.client == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := c.client.DeductStock(ctx, &product.DeductStockRequest{
		ProductId: productId,
		Quantity:  quantity,
		OrderId:   orderId,
	})
	return err
}

// RollbackStock 回滚库存
func (c *ProductServiceClient) RollbackStock(ctx context.Context, productId, quantity int64, orderId string) error {
	if c == nil || c.client == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := c.client.RollbackStock(ctx, &product.RollbackStockRequest{
		ProductId: productId,
		Quantity:  quantity,
		OrderId:   orderId,
	})
	return err
}

// GetProduct 获取商品信息
func (c *ProductServiceClient) GetProduct(ctx context.Context, productId int64) (*product.ProductInfo, error) {
	if c == nil || c.client == nil {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return c.client.GetProduct(ctx, &product.GetProductRequest{ProductId: productId})
}
