package rpc

import (
	"context"
	"fmt"
	"time"

	"seckill-mall/order-service/internal/config"
	"seckill-mall/seckill-service/seckill"

	"github.com/zeromicro/go-zero/zrpc"
)

// SeckillServiceClient 秒杀服务 RPC 客户端
type SeckillServiceClient struct {
	client seckill.SeckillServiceClient
}

// NewSeckillServiceClient 创建秒杀服务客户端
func NewSeckillServiceClient(c config.Config) (*SeckillServiceClient, error) {
	client, err := buildSeckillClient(c.SeckillService, 8083)
	if err != nil {
		return nil, err
	}

	return &SeckillServiceClient{
		client: seckill.NewSeckillServiceClient(client.Conn()),
	}, nil
}

// buildSeckillClient 构建 seckill-service 客户端：优先 etcd，fallback 到 localhost
func buildSeckillClient(conf zrpc.RpcClientConf, port int) (zrpc.Client, error) {
	if len(conf.Etcd.Hosts) == 0 {
		conf.Endpoints = []string{fmt.Sprintf("127.0.0.1:%d", port)}
	}
	client, err := zrpc.NewClient(conf)
	if err != nil {
		return nil, err
	}
	return client, nil
}

// UpdateOrderStatus 更新秒杀订单状态（将 Redis 订单从 pending → success/failed）
func (c *SeckillServiceClient) UpdateOrderStatus(ctx context.Context, orderId, status string) error {
	if c == nil || c.client == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := c.client.UpdateOrderStatus(ctx, &seckill.UpdateOrderStatusRequest{
		OrderId: orderId,
		Status:  status,
	})
	return err
}
