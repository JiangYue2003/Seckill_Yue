package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	seckill "seckill-mall/seckill-service/seckill"
)

const (
	keyPrefixSeckillInfo  = "seckill:info:"
	keyPrefixSeckillName  = "seckill:product_name:"
	keyPrefixSeckillStock = "seckill:stock:"
	keyPrefixSeckillUser  = "seckill:user:"
)

var (
	redisClient *redis.Client
)

// 测试场景配置
var scenarios = []struct {
	name          string
	totalRequests int64
	concurrency   int
	stock         int64
}{
	{"基准测试 (100用户/50库存)", 100, 50, 50},
	{"极限压测 (500用户/200库存)", 500, 200, 200},
	{"热点测试 (1000用户/500库存)", 1000, 500, 500},
	{"大规模压测 (2000用户/800库存)", 2000, 800, 800},
	{"超高并发 (3000用户/1000库存)", 3000, 1000, 1000},
	{"极端压力 (5000用户/2000库存)", 5000, 2000, 2000},
}

// SeckillServiceClient gRPC 客户端封装
type SeckillServiceClient struct {
	conn   *grpc.ClientConn
	client seckill.SeckillServiceClient
}

func (c *SeckillServiceClient) Seckill(ctx context.Context, req *seckill.SeckillRequest) (*seckill.SeckillResponse, error) {
	return c.client.Seckill(ctx, req)
}

func (c *SeckillServiceClient) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}

func main() {
	fmt.Println("========================================")
	fmt.Println("   秒杀系统性能测试")
	fmt.Println("========================================")
	fmt.Println()

	// 初始化 Redis
	initRedis("localhost:6379")

	// 初始化 gRPC 客户端
	client := initGrpcClient("127.0.0.1:8083")
	defer client.Close()

	// 运行所有测试场景
	for _, scenario := range scenarios {
		runBenchmark(client, scenario)
		time.Sleep(2 * time.Second)
	}

	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("   性能测试完成")
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

// Redis 操作函数
func initStock(ctx context.Context, seckillProductId, stock int64) error {
	key := keyPrefixSeckillStock + strconv.FormatInt(seckillProductId, 10)
	return redisClient.Set(ctx, key, stock, 0).Err()
}

func getStock(ctx context.Context, seckillProductId int64) (int64, error) {
	key := keyPrefixSeckillStock + strconv.FormatInt(seckillProductId, 10)
	val, err := redisClient.Get(ctx, key).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return val, err
}

func rollbackStock(ctx context.Context, seckillProductId, amount int64) {
	key := keyPrefixSeckillStock + strconv.FormatInt(seckillProductId, 10)
	redisClient.IncrBy(ctx, key, amount)
}

func deleteUserKey(ctx context.Context, seckillProductId, userId int64) {
	key := fmt.Sprintf("%s%d:%d", keyPrefixSeckillUser, seckillProductId, userId)
	redisClient.Del(ctx, key)
}

func setSeckillProductInfo(ctx context.Context, seckillProductId, productId, price int64, name string, startTime, endTime, ttl int64) error {
	infoKey := keyPrefixSeckillInfo + strconv.FormatInt(seckillProductId, 10)
	infoValue := fmt.Sprintf("%d:%d:%d:%d", productId, price, startTime, endTime)
	if err := redisClient.Set(ctx, infoKey, infoValue, time.Duration(ttl)*time.Second).Err(); err != nil {
		return err
	}

	nameKey := keyPrefixSeckillName + strconv.FormatInt(seckillProductId, 10)
	return redisClient.Set(ctx, nameKey, name, time.Duration(ttl)*time.Second).Err()
}

// runBenchmark 运行单个压测场景
func runBenchmark(client *SeckillServiceClient, scenario struct {
	name          string
	totalRequests int64
	concurrency   int
	stock         int64
}) {
	fmt.Println("\n----------------------------------------")
	fmt.Printf("   场景: %s\n", scenario.name)
	fmt.Printf("   总请求: %d | 并发: %d | 库存: %d\n", scenario.totalRequests, scenario.concurrency, scenario.stock)
	fmt.Println("----------------------------------------")

	seckillProductId := int64(2001)
	now := time.Now().Unix()
	ttl := int64(86400)
	startTime := now - 3600
	endTime := now + 3600

	ctx := context.Background()

	// 清理旧数据并初始化
	cleanupProductData(seckillProductId)

	if err := setSeckillProductInfo(ctx, seckillProductId, seckillProductId, 999, "压测商品", startTime, endTime, ttl); err != nil {
		log.Printf("初始化商品信息失败: %v", err)
		return
	}

	if err := initStock(ctx, seckillProductId, scenario.stock); err != nil {
		log.Printf("初始化库存失败: %v", err)
		return
	}

	// 指标收集器
	metrics := NewMetrics()

	// 信号量控制并发
	semaphore := make(chan struct{}, scenario.concurrency)

	startTimeUnix := time.Now()
	totalRequests := scenario.totalRequests

	var wg sync.WaitGroup

	for i := int64(0); i < totalRequests; i++ {
		wg.Add(1)
		semaphore <- struct{}{}

		go func(userId int64) {
			defer wg.Done()
			defer func() { <-semaphore }()

			reqStart := time.Now()

			reqCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			resp, err := client.Seckill(reqCtx, &seckill.SeckillRequest{
				UserId:           userId,
				SeckillProductId: seckillProductId,
				Quantity:         1,
			})

			latency := time.Since(reqStart).Milliseconds()

			metrics.Record(latency, resp, err)
		}(10000 + i)
	}

	wg.Wait()
	totalDuration := time.Since(startTimeUnix)

	// 获取最终库存
	finalStock, _ := getStock(ctx, seckillProductId)
	actualSold := scenario.stock - finalStock

	// 清理数据
	cleanupProductData(seckillProductId)

	// 打印报告
	metrics.Report(totalDuration, scenario.stock, actualSold)
}

// cleanupProductData 清理商品测试数据
func cleanupProductData(seckillProductId int64) {
	ctx := context.Background()
	rollbackStock(ctx, seckillProductId, 100000)

	// 用 pipeline 批量删除 user keys (10000 ~ 20000)
	pipe := redisClient.Pipeline()
	for i := int64(0); i < 10000; i++ {
		key := fmt.Sprintf("%s%d:%d", keyPrefixSeckillUser, seckillProductId, 10000+i)
		pipe.Del(ctx, key)
	}
	pipe.Exec(ctx)
}
