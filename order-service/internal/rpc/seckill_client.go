package rpc

import (
	"context"
	"errors"
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
	client, err := buildSeckillClient(c.SeckillService, c.Fallback.SeckillServiceEndpoint)
	if err != nil {
		return nil, err
	}

	return &SeckillServiceClient{
		client: seckill.NewSeckillServiceClient(client.Conn()),
	}, nil
}

// buildSeckillClient 构建 seckill-service 客户端：优先 etcd，fallback 到 localhost
func buildSeckillClient(conf zrpc.RpcClientConf, fallbackEndpoint string) (zrpc.Client, error) {
	if len(conf.Etcd.Hosts) == 0 {
		if fallbackEndpoint == "" {
			fallbackEndpoint = fmt.Sprintf("127.0.0.1:%d", 9083)
		}
		conf.Endpoints = []string{fallbackEndpoint}
	}
	client, err := zrpc.NewClient(conf)
	if err != nil {
		return nil, err
	}
	return client, nil
}

// UpdateOrderStatus 更新秒杀订单状态（将 Redis 订单从 pending -> success/failed）
// allowRecover=true 时，允许 failed -> success 受控自愈
func (c *SeckillServiceClient) UpdateOrderStatus(ctx context.Context, orderId, status string, allowRecover bool) error {
	if c == nil || c.client == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := c.client.UpdateOrderStatus(ctx, &seckill.UpdateOrderStatusRequest{
		OrderId:      orderId,
		Status:       status,
		AllowRecover: allowRecover,
	})
	if err != nil {
		return err
	}
	if resp == nil {
		return errors.New("update order status returned nil response")
	}
	if !resp.Success {
		return fmt.Errorf("update order status rejected: %s", resp.Message)
	}
	return nil
}

// CompensateFailedOrder 超时失败补偿：
// 仅当订单当前状态为 pending 时，原子执行 pending->failed + 回补 Redis 库存 + 删除 userKey。
func (c *SeckillServiceClient) CompensateFailedOrder(
	ctx context.Context,
	orderId string,
	seckillProductId int64,
	userId int64,
	quantity int64,
	reason string,
) (*seckill.CompensateFailedOrderResponse, error) {
	if c == nil || c.client == nil {
		return &seckill.CompensateFailedOrderResponse{
			Success: true,
			Result:  "client_nil",
			Message: "seckill rpc client unavailable, skip",
		}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := c.client.CompensateFailedOrder(ctx, &seckill.CompensateFailedOrderRequest{
		OrderId:          orderId,
		SeckillProductId: seckillProductId,
		UserId:           userId,
		Quantity:         quantity,
		Reason:           reason,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("compensate failed order returned nil response")
	}
	if !resp.Success {
		return resp, fmt.Errorf("compensate failed order rejected: %s", resp.Message)
	}
	return resp, nil
}
