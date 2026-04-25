package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	seckill "seckill-mall/common/seckill"
)

const (
	keyPrefixSeckillInfo  = "seckill:info:"
	keyPrefixSeckillName  = "seckill:product_name:"
	keyPrefixSeckillStock = "seckill:stock:"
	keyPrefixSeckillUser  = "seckill:user:"
	benchmarkUserStart    = int64(10000)
	defaultMySQLDSN       = "root:Zz123456@tcp(localhost:3306)/seckill_mall?charset=utf8mb4&parseTime=True&loc=Local"
)

var (
	redisClient *redis.Client
	mysqlDB     *sql.DB
)

// 测试场景配置
var scenarios = []struct {
	name          string
	productId     int64
	totalRequests int64
	concurrency   int
	stock         int64
}{
	{"基准测试 (100用户/50库存)", 2001, 100, 50, 50},
	{"小规模压测 (1000用户/500库存)", 2011, 1000, 500, 500},
	{"中规模压测 (3000用户/1500库存)", 2031, 3000, 1000, 1500},
	{"大规模压测 (5000用户/2000库存)", 2061, 5000, 1500, 2000},
	{"超高并发 (8000用户/3000库存)", 2101, 8000, 2000, 3000},
	{"万级并发 (10000用户/4000库存)", 2201, 10000, 2500, 4000},
	{"1.5万并发 (15000用户/5000库存)", 2401, 15000, 3000, 5000},
	{"2万并发 (20000用户/6000库存)", 2701, 20000, 4000, 6000},
	{"3万并发 (30000用户/8000库存)", 3101, 30000, 5000, 8000},
	{"5万并发 (50000用户/10000库存)", 3501, 50000, 6000, 10000},
	{"10万并发 (100000用户/15000库存)", 4101, 100000, 8000, 15000},
}

const poolSize = 8 // 连接池大小

// SeckillServiceClient gRPC 单连接封装
type SeckillServiceClient struct {
	conn   *grpc.ClientConn
	client seckill.SeckillServiceClient
}

func (c *SeckillServiceClient) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}

// ConnectionPool gRPC 连接池，round-robin 分发请求
type ConnectionPool struct {
	clients []*SeckillServiceClient
	counter uint64
}

func (p *ConnectionPool) Seckill(ctx context.Context, req *seckill.SeckillRequest) (*seckill.SeckillResponse, error) {
	idx := atomic.AddUint64(&p.counter, 1) % uint64(len(p.clients))
	return p.clients[idx].client.Seckill(ctx, req)
}

func (p *ConnectionPool) Close() {
	for _, c := range p.clients {
		c.Close()
	}
}

func main() {
	fmt.Println("========================================")
	fmt.Println("   秒杀系统性能测试")
	fmt.Println("========================================")
	fmt.Println()

	// 初始化 Redis
	initRedis("localhost:6379")
	initMySQL()
	defer closeMySQL()

	// 初始化 gRPC 连接池
	pool := initGrpcPool("127.0.0.1:9083", poolSize)
	defer pool.Close()

	// 运行所有测试场景
	for _, scenario := range scenarios {
		runBenchmark(pool, scenario)
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

func initMySQL() {
	dsn := os.Getenv("BENCHMARK_MYSQL_DSN")
	if dsn == "" {
		dsn = defaultMySQLDSN
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Printf("[WARN] MySQL init failed, skip seckill_orders cleanup: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Printf("[WARN] MySQL ping failed, skip seckill_orders cleanup: %v", err)
		_ = db.Close()
		return
	}

	mysqlDB = db
	fmt.Println("[OK] MySQL connected (seckill_orders cleanup enabled)")
}

func closeMySQL() {
	if mysqlDB != nil {
		_ = mysqlDB.Close()
	}
}

func initGrpcClient(addr string) *SeckillServiceClient {
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("连接 gRPC 服务器失败: %v", err)
	}
	return &SeckillServiceClient{
		conn:   conn,
		client: seckill.NewSeckillServiceClient(conn),
	}
}

func initGrpcPool(addr string, size int) *ConnectionPool {
	clients := make([]*SeckillServiceClient, size)
	for i := range clients {
		clients[i] = initGrpcClient(addr)
	}
	fmt.Printf("[OK] gRPC 连接池初始化完成 (size=%d)\n", size)
	return &ConnectionPool{clients: clients}
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
func runBenchmark(client *ConnectionPool, scenario struct {
	name          string
	productId     int64
	totalRequests int64
	concurrency   int
	stock         int64
}) {
	fmt.Println("\n----------------------------------------")
	fmt.Printf("   场景: %s\n", scenario.name)
	fmt.Printf("   总请求: %d | 并发: %d | 库存: %d\n", scenario.totalRequests, scenario.concurrency, scenario.stock)
	fmt.Println("----------------------------------------")

	seckillProductId := scenario.productId
	now := time.Now().Unix()
	ttl := int64(86400)
	startTime := now - 3600
	endTime := now + 3600

	ctx := context.Background()

	// 清理旧数据并初始化
	cleanupProductData(seckillProductId, scenario.totalRequests)

	if err := setSeckillProductInfo(ctx, seckillProductId, 1, 999, "压测商品", startTime, endTime, ttl); err != nil {
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
		}(benchmarkUserStart + i)
	}

	wg.Wait()
	totalDuration := time.Since(startTimeUnix)

	// 获取最终库存
	finalStock, _ := getStock(ctx, seckillProductId)
	actualSold := scenario.stock - finalStock

	// 清理数据
	cleanupProductData(seckillProductId, totalRequests)

	// 打印报告
	metrics.Report(totalDuration, scenario.stock, actualSold)
}

// cleanupProductData 清理商品测试数据
// userId 从 10000 开始，总量为 totalRequests
func cleanupProductData(seckillProductId int64, totalRequests int64) {
	ctx := context.Background()
	rollbackStock(ctx, seckillProductId, 100000)

	// 用 pipeline 批量删除 user keys
	pipe := redisClient.Pipeline()
	for i := int64(0); i < totalRequests; i++ {
		key := fmt.Sprintf("%s%d:%d", keyPrefixSeckillUser, seckillProductId, benchmarkUserStart+i)
		pipe.Del(ctx, key)
	}
	_, _ = pipe.Exec(ctx)

	if mysqlDB != nil {
		startUserID := benchmarkUserStart
		endUserID := benchmarkUserStart + totalRequests - 1
		if _, err := mysqlDB.ExecContext(
			ctx,
			"DELETE FROM seckill_orders WHERE seckill_product_id = ? AND user_id BETWEEN ? AND ?",
			seckillProductId,
			startUserID,
			endUserID,
		); err != nil {
			log.Printf("[WARN] cleanup seckill_orders failed: product=%d, userRange=[%d,%d], err=%v",
				seckillProductId, startUserID, endUserID, err)
		}
	}
}
