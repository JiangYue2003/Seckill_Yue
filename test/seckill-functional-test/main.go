package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	seckill "seckill-mall/common/seckill"
)

const (
	// Redis key 前缀
	keyPrefixSeckillInfo  = "seckill:info:"
	keyPrefixSeckillName  = "seckill:product_name:"
	keyPrefixSeckillStock = "seckill:stock:"
	keyPrefixSeckillUser  = "seckill:user:"
)

var (
	redisClient *redis.Client
)

func main() {
	fmt.Println("========================================")
	fmt.Println("   秒杀系统功能测试")
	fmt.Println("========================================")
	fmt.Println()

	// 初始化 Redis
	initRedis("localhost:6379")

	// 初始化 gRPC 客户端
	client := initGrpcClient("127.0.0.1:9083")

	// 运行测试
	runTests(client)

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("   功能测试完成")
	fmt.Println("========================================")
}

func initRedis(addr string) {
	redisClient = redis.NewClient(&redis.Options{
		Addr: addr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("连接 Redis 失败: %v", err)
	}
	fmt.Println("[OK] Redis 连接成功")
}

// SeckillServiceClient gRPC 客户端封装
type SeckillServiceClient struct {
	conn   *grpc.ClientConn
	client seckill.SeckillServiceClient
}

func (c *SeckillServiceClient) Seckill(ctx context.Context, req *seckill.SeckillRequest) (*seckill.SeckillResponse, error) {
	return c.client.Seckill(ctx, req)
}

func (c *SeckillServiceClient) GetSeckillResult(ctx context.Context, req *seckill.SeckillResultRequest) (*seckill.SeckillResultResponse, error) {
	return c.client.GetSeckillResult(ctx, req)
}

func (c *SeckillServiceClient) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}

func initGrpcClient(addr string) *SeckillServiceClient {
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("连接 gRPC 服务器失败: %v", err)
	}

	client := seckill.NewSeckillServiceClient(conn)
	fmt.Println("[OK] gRPC 客户端连接成功")

	return &SeckillServiceClient{
		conn:   conn,
		client: client,
	}
}

// ============ Redis 操作函数 (替代 internal/redis) ============

// initStock 初始化商品库存
func initStock(ctx context.Context, seckillProductId, stock int64) error {
	key := keyPrefixSeckillStock + strconv.FormatInt(seckillProductId, 10)
	return redisClient.Set(ctx, key, stock, 0).Err()
}

// rollbackStock 回滚库存
func rollbackStock(ctx context.Context, seckillProductId, amount int64) {
	key := keyPrefixSeckillStock + strconv.FormatInt(seckillProductId, 10)
	redisClient.IncrBy(ctx, key, amount)
}

// getStock 获取当前库存
func getStock(ctx context.Context, seckillProductId int64) (int64, error) {
	key := keyPrefixSeckillStock + strconv.FormatInt(seckillProductId, 10)
	val, err := redisClient.Get(ctx, key).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return val, err
}

// deleteUserKey 删除用户购买记录
func deleteUserKey(ctx context.Context, seckillProductId, userId int64) {
	key := fmt.Sprintf("%s%d:%d", keyPrefixSeckillUser, seckillProductId, userId)
	redisClient.Del(ctx, key)
}

// setSeckillProductInfo 设置秒杀商品信息
func setSeckillProductInfo(ctx context.Context, seckillProductId, productId, price int64, name string, startTime, endTime, ttl int64) error {
	infoKey := keyPrefixSeckillInfo + strconv.FormatInt(seckillProductId, 10)
	infoValue := fmt.Sprintf("%d:%d:%d:%d", productId, price, startTime, endTime)
	if err := redisClient.Set(ctx, infoKey, infoValue, time.Duration(ttl)*time.Second).Err(); err != nil {
		return err
	}

	nameKey := keyPrefixSeckillName + strconv.FormatInt(seckillProductId, 10)
	return redisClient.Set(ctx, nameKey, name, time.Duration(ttl)*time.Second).Err()
}
