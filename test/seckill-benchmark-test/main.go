package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	seckillpb "seckill-mall/common/seckill"
)

const (
	keyPrefixSeckillInfo  = "{%d}:sk:info"
	keyPrefixSeckillName  = "{%d}:sk:name"
	keyPrefixSeckillStock = "{%d}:sk:stock:total"
	keyPrefixSeckillShard = "{%d}:sk:stock:%d"
	keyPrefixSeckillMeta  = "{%d}:sk:stock:meta"
	keyPrefixSeckillUser  = "{%d}:sk:user:%d"

	benchmarkUserStart = int64(10000)
	defaultMySQLDSN    = "root:Zz123456@tcp(localhost:3306)/seckill_mall?charset=utf8mb4&parseTime=True&loc=Local"
	defaultTargets     = "127.0.0.1:9083"
	defaultPoolSize    = 64
	defaultShardCount  = 16

	benchmarkModeLegacy = "legacy"
	benchmarkModeBurst  = "burst"
)

var (
	targetsFlag   = flag.String("targets", defaultTargets, "comma-separated seckill grpc targets, e.g. 127.0.0.1:9083,127.0.0.1:19083")
	modeFlag      = flag.String("mode", benchmarkModeLegacy, "benchmark mode: legacy | burst")
	productID     = flag.Int64("product-id", 9101, "seckill product id")
	stock         = flag.Int64("stock", 15000, "initial seckill stock")
	users         = flag.Int64("users", 100000, "unique users")
	rps           = flag.Int("rate", 20000, "target requests per second")
	duration      = flag.Duration("duration", 30*time.Second, "upper bound for burst dispatch window, e.g. 30s")
	workers       = flag.Int("workers", 4096, "worker goroutines")
	queueSize     = flag.Int("queue-size", 20000, "request queue size")
	reqTimeout    = flag.Duration("request-timeout", 3*time.Second, "per-request timeout")
	quantity      = flag.Int64("quantity", 1, "purchase quantity per request")
	redisAddr     = flag.String("redis", "localhost:6379", "redis address")
	poolSize      = flag.Int("pool-size", defaultPoolSize, "grpc client pool size")
	idealMode     = flag.Bool("ideal", false, "auto expand stock/users for near-all-success throughput test")
	totalRequests = flag.Int64("total-requests", 10000, "total requests for legacy mode")
	concurrency   = flag.Int("concurrency", 1000, "max in-flight requests for legacy mode")
	burstWindow   = flag.Duration("burst-window", time.Second, "dispatch window for burst mode")
)

var (
	redisClient *redis.Client
	mysqlDB     *sql.DB
)

type requestTask struct {
	seq int64
}

type result struct {
	success       bool
	bizCode       string
	failReason    string
	systemErr     bool
	systemErrCode string
}

type seckillClient struct {
	conn   *grpc.ClientConn
	client seckillpb.SeckillServiceClient
}

func (c *seckillClient) Close() {
	if c != nil && c.conn != nil {
		_ = c.conn.Close()
	}
}

type connectionPool struct {
	clients []*seckillClient
	counter uint64
}

func (p *connectionPool) Seckill(ctx context.Context, req *seckillpb.SeckillRequest) (*seckillpb.SeckillResponse, error) {
	idx := atomic.AddUint64(&p.counter, 1) % uint64(len(p.clients))
	return p.clients[idx].client.Seckill(ctx, req)
}

func (p *connectionPool) Close() {
	if p == nil {
		return
	}
	for _, client := range p.clients {
		client.Close()
	}
}

func main() {
	flag.Parse()

	if *users <= 0 || *workers <= 0 || *poolSize <= 0 || *quantity <= 0 {
		log.Fatal("invalid args: users/workers/pool-size/quantity must be > 0")
	}
	if *queueSize <= 0 {
		log.Fatal("invalid args: queue-size must be > 0")
	}

	mode, err := normalizeMode(*modeFlag)
	if err != nil {
		log.Fatal(err)
	}

	targets := parseTargets(*targetsFlag)
	if len(targets) == 0 {
		log.Fatal("invalid --targets")
	}

	expectedRequests, err := resolveExpectedRequests(mode)
	if err != nil {
		log.Fatal(err)
	}
	dispatchWindow := effectiveDispatchWindow()
	effectiveStock := *stock
	effectiveUsers := *users
	if *idealMode {
		minRequired := expectedRequests + 1000
		if effectiveStock < minRequired {
			effectiveStock = minRequired
		}
		if effectiveUsers < minRequired {
			effectiveUsers = minRequired
		}
	}

	fmt.Println("========================================")
	fmt.Println(" Seckill 同步入口压测 (gRPC)")
	fmt.Println("========================================")
	fmt.Printf("Mode: %s\n", mode)
	fmt.Printf("Targets: %s\n", strings.Join(targets, ","))
	switch mode {
	case benchmarkModeLegacy:
		fmt.Printf("Config: totalRequests=%d, concurrency=%d, users=%d, stock=%d, pool=%d\n",
			*totalRequests, *concurrency, effectiveUsers, effectiveStock, *poolSize)
	case benchmarkModeBurst:
		fmt.Printf("Config: rate=%d req/s, duration=%s, burstWindow=%s, effectiveDispatchWindow=%s, workers=%d, queue=%d, users=%d, stock=%d, pool=%d\n",
			*rps, duration.String(), burstWindow.String(), dispatchWindow.String(), *workers, *queueSize, effectiveUsers, effectiveStock, *poolSize)
	}
	fmt.Printf("Expected requests: %d\n", expectedRequests)
	if !*idealMode && (*stock < expectedRequests || *users < expectedRequests) {
		fmt.Printf("[WARN] stock/users below expected requests, results will include many SOLD_OUT or duplicates. recommended: --ideal or stock/users >= %d\n", expectedRequests)
	}
	if *idealMode {
		fmt.Println("[OK] ideal mode enabled: stock/users auto-expanded for near-all-success throughput measurement")
	}
	fmt.Println()

	initRedis(*redisAddr)
	initMySQL()
	defer closeMySQL()

	ctx := context.Background()
	prepareScenario(ctx, *productID, effectiveStock, effectiveUsers)
	defer cleanupScenario(ctx, *productID, effectiveUsers)

	pool := initGRPCPool(targets, *poolSize)
	defer pool.Close()

	switch mode {
	case benchmarkModeLegacy:
		runLegacy(pool, effectiveStock)
	case benchmarkModeBurst:
		runBurst(pool, effectiveStock)
	default:
		log.Fatalf("unsupported mode: %s", mode)
	}
}

func initGRPCPool(targets []string, size int) *connectionPool {
	clients := make([]*seckillClient, size)
	for i := range clients {
		target := targets[i%len(targets)]
		conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Fatalf("连接 gRPC 服务器失败: target=%s, err=%v", target, err)
		}
		clients[i] = &seckillClient{
			conn:   conn,
			client: seckillpb.NewSeckillServiceClient(conn),
		}
	}
	fmt.Printf("[OK] gRPC 连接池初始化完成: size=%d, targets=%s\n", size, strings.Join(targets, ","))
	return &connectionPool{clients: clients}
}

func runLegacy(pool *connectionPool, configuredStock int64) {
	metrics := NewMetrics()
	semaphore := make(chan struct{}, *concurrency)

	var wg sync.WaitGroup
	start := time.Now()

	fmt.Println("----------------------------------------")
	fmt.Println("开始压测...")
	fmt.Println("----------------------------------------")

	for i := int64(0); i < *totalRequests; i++ {
		metrics.IncScheduled()
		semaphore <- struct{}{}
		metrics.IncDispatched()
		wg.Add(1)

		go func(seq int64) {
			defer wg.Done()
			defer func() { <-semaphore }()

			startAt := time.Now()
			userID := benchmarkUserStart + seq
			reqCtx, cancel := context.WithTimeout(context.Background(), *reqTimeout)
			resp, err := pool.Seckill(reqCtx, &seckillpb.SeckillRequest{
				UserId:           userID,
				SeckillProductId: *productID,
				Quantity:         *quantity,
			})
			cancel()

			metrics.RecordResult(time.Since(startAt).Milliseconds(), classifySeckillResponse(resp, err))
		}(i)
	}

	wg.Wait()
	totalDuration := time.Since(start)
	actualSold := countReservedOrders(context.Background(), *productID)
	metrics.Report(totalDuration, configuredStock, actualSold)
}

func runBurst(pool *connectionPool, configuredStock int64) {
	metrics := NewMetrics()
	tasks := make(chan requestTask, *queueSize)

	var wg sync.WaitGroup
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range tasks {
				start := time.Now()
				userID := benchmarkUserStart + task.seq

				reqCtx, cancel := context.WithTimeout(context.Background(), *reqTimeout)
				resp, err := pool.Seckill(reqCtx, &seckillpb.SeckillRequest{
					UserId:           userID,
					SeckillProductId: *productID,
					Quantity:         *quantity,
				})
				cancel()

				latencyMs := time.Since(start).Milliseconds()
				metrics.RecordResult(latencyMs, classifySeckillResponse(resp, err))
			}
		}()
	}

	start := time.Now()
	dispatchWindow := effectiveDispatchWindow()
	end := start.Add(dispatchWindow)
	const tickInterval = 10 * time.Millisecond
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	var budget float64

	fmt.Println("----------------------------------------")
	fmt.Println("开始压测 (burst)...")
	fmt.Println("----------------------------------------")

	var seq int64
	for {
		now := time.Now()
		if now.After(end) {
			break
		}
		<-ticker.C

		budget += float64(*rps) * tickInterval.Seconds()
		toDispatch := int(budget)
		if toDispatch <= 0 {
			continue
		}
		budget -= float64(toDispatch)

		for i := 0; i < toDispatch; i++ {
			metrics.IncScheduled()
			task := requestTask{seq: atomic.AddInt64(&seq, 1) - 1}
			select {
			case tasks <- task:
				metrics.IncDispatched()
			default:
				metrics.IncDropped("queue_full")
			}
		}
	}

	close(tasks)
	wg.Wait()
	totalDuration := time.Since(start)

	actualSold := countReservedOrders(context.Background(), *productID)
	metrics.Report(totalDuration, configuredStock, actualSold)
}

func classifySeckillResponse(resp *seckillpb.SeckillResponse, err error) result {
	if err != nil {
		code := classifyRPCError(err)
		return result{
			systemErr:     true,
			systemErrCode: code,
			failReason:    code,
		}
	}
	if resp == nil {
		return result{
			systemErr:     true,
			systemErrCode: "RESP_NIL",
			failReason:    "RESP_NIL",
		}
	}
	if resp.Success || resp.Code == "SUCCESS" {
		return result{success: true, bizCode: "SUCCESS"}
	}

	code := resp.Code
	if code == "" {
		code = "EMPTY_CODE"
	}
	out := result{
		bizCode:    code,
		failReason: "RESP_" + code,
	}
	switch code {
	case "SOLD_OUT", "ALREADY_PURCHASED", "SECKILL_NOT_STARTED", "SECKILL_ENDED":
		return out
	default:
		out.systemErr = true
		out.systemErrCode = "RESP_" + code
		return out
	}
}

func classifyRPCError(err error) string {
	if err == nil {
		return "ERR_UNKNOWN"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "ERR_CONTEXT_DEADLINE_EXCEEDED"
	}
	if errors.Is(err, context.Canceled) {
		return "ERR_CONTEXT_CANCELED"
	}
	if st, ok := status.FromError(err); ok {
		if st.Code() == codes.OK {
			return "ERR_RPC_OK"
		}
		return "ERR_RPC_" + strings.ToUpper(st.Code().String())
	}
	return "ERR_LOCAL"
}

func normalizeMode(raw string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	if mode == "" {
		return benchmarkModeLegacy, nil
	}
	switch mode {
	case benchmarkModeLegacy, benchmarkModeBurst:
		return mode, nil
	default:
		return "", fmt.Errorf("invalid --mode=%q, allowed values: %s | %s", raw, benchmarkModeLegacy, benchmarkModeBurst)
	}
}

func resolveExpectedRequests(mode string) (int64, error) {
	switch mode {
	case benchmarkModeLegacy:
		if *totalRequests <= 0 || *concurrency <= 0 {
			return 0, fmt.Errorf("legacy mode requires total-requests > 0 and concurrency > 0")
		}
		return *totalRequests, nil
	case benchmarkModeBurst:
		if *rps <= 0 || *duration <= 0 || *burstWindow <= 0 {
			return 0, fmt.Errorf("burst mode requires rate > 0, duration > 0 and burst-window > 0")
		}
		return int64(math.Ceil(float64(*rps) * effectiveDispatchWindow().Seconds())), nil
	default:
		return 0, fmt.Errorf("unsupported mode: %s", mode)
	}
}

func effectiveDispatchWindow() time.Duration {
	if *burstWindow < *duration {
		return *burstWindow
	}
	return *duration
}

func parseTargets(raw string) []string {
	parts := strings.Split(raw, ",")
	targets := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t == "" {
			continue
		}
		targets = append(targets, t)
	}
	return targets
}

func initRedis(addr string) {
	redisClient = redis.NewClient(&redis.Options{Addr: addr})
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
		log.Printf("[WARN] MySQL init failed, skip benchmark cleanup: %v", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Printf("[WARN] MySQL ping failed, skip benchmark cleanup: %v", err)
		_ = db.Close()
		return
	}
	mysqlDB = db
	fmt.Println("[OK] MySQL connected (benchmark cleanup enabled)")
}

func closeMySQL() {
	if mysqlDB != nil {
		_ = mysqlDB.Close()
	}
}

func prepareScenario(ctx context.Context, seckillProductId, initStock, userCount int64) {
	cleanupScenario(ctx, seckillProductId, userCount)

	now := time.Now().Unix()
	startTime := now - 3600
	endTime := now + 3600
	ttl := int64(86400)
	if err := setSeckillProductInfo(ctx, seckillProductId, 1, 999, "seckill benchmark product", startTime, endTime, ttl); err != nil {
		log.Fatalf("初始化秒杀商品信息失败: %v", err)
	}
	if err := initStockValue(ctx, seckillProductId, initStock); err != nil {
		log.Fatalf("初始化库存失败: %v", err)
	}
	fmt.Printf("[OK] 场景已初始化: productId=%d, stock=%d\n", seckillProductId, initStock)

	fmt.Printf("[WAIT] 等待 seckill-service 缓存刷新 (5s)...\n")
	time.Sleep(5 * time.Second)
	fmt.Printf("[OK] 缓存刷新等待完成\n")
}

func cleanupScenario(ctx context.Context, seckillProductId, userCount int64) {
	clearScenarioStock(ctx, seckillProductId)

	pipe := redisClient.Pipeline()
	for i := int64(0); i < userCount; i++ {
		uid := benchmarkUserStart + i
		pipe.Del(ctx, fmt.Sprintf(keyPrefixSeckillUser, seckillProductId, uid))
	}
	_, _ = pipe.Exec(ctx)

	if mysqlDB != nil {
		startUserID := benchmarkUserStart
		endUserID := benchmarkUserStart + userCount - 1

		cleanupStatements := []struct {
			sql  string
			args []any
		}{
			{
				sql:  "DELETE FROM event_outbox WHERE aggregate_type = 'reservation' AND aggregate_id IN (SELECT reservation_id FROM seckill_reservations WHERE seckill_product_id = ? AND user_id BETWEEN ? AND ?)",
				args: []any{seckillProductId, startUserID, endUserID},
			},
			{
				sql:  "DELETE FROM seckill_orders WHERE seckill_product_id = ? AND user_id BETWEEN ? AND ?",
				args: []any{seckillProductId, startUserID, endUserID},
			},
			{
				sql:  "DELETE FROM processed_messages WHERE message_id LIKE ?",
				args: []any{fmt.Sprintf("S%d_%%", seckillProductId)},
			},
			{
				sql:  "DELETE FROM orders WHERE order_id IN (SELECT order_id FROM seckill_reservations WHERE seckill_product_id = ? AND user_id BETWEEN ? AND ?)",
				args: []any{seckillProductId, startUserID, endUserID},
			},
			{
				sql:  "DELETE FROM seckill_reservations WHERE seckill_product_id = ? AND user_id BETWEEN ? AND ?",
				args: []any{seckillProductId, startUserID, endUserID},
			},
		}

		for _, stmt := range cleanupStatements {
			if _, err := mysqlDB.ExecContext(ctx, stmt.sql, stmt.args...); err != nil {
				log.Printf("[WARN] cleanup sql failed: sql=%q, err=%v", stmt.sql, err)
			}
		}
	}
}

func initStockValue(ctx context.Context, seckillProductId, stock int64) error {
	pipe := redisClient.TxPipeline()
	pipe.Set(ctx, fmt.Sprintf(keyPrefixSeckillStock, seckillProductId), stock, 0)
	pipe.Set(ctx, fmt.Sprintf(keyPrefixSeckillMeta, seckillProductId), defaultShardCount, 0)
	for shardNo, shardStock := range splitStockIntoShards(stock) {
		pipe.Set(ctx, fmt.Sprintf(keyPrefixSeckillShard, seckillProductId, shardNo), shardStock, 0)
	}
	_, err := pipe.Exec(ctx)
	return err
}

func countReservedOrders(ctx context.Context, seckillProductId int64) int64 {
	if mysqlDB == nil {
		return 0
	}

	var count int64
	err := mysqlDB.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM seckill_reservations WHERE seckill_product_id = ? AND status IN (0,1,2,3,4,5)",
		seckillProductId,
	).Scan(&count)
	if err != nil {
		log.Printf("[WARN] count seckill_reservations failed: product=%d, err=%v", seckillProductId, err)
		return 0
	}
	return count
}

func clearScenarioStock(ctx context.Context, seckillProductId int64) {
	pipe := redisClient.TxPipeline()
	pipe.Del(ctx, fmt.Sprintf(keyPrefixSeckillStock, seckillProductId))
	pipe.Del(ctx, fmt.Sprintf(keyPrefixSeckillMeta, seckillProductId))
	for shardNo := 0; shardNo < defaultShardCount; shardNo++ {
		pipe.Del(ctx, fmt.Sprintf(keyPrefixSeckillShard, seckillProductId, shardNo))
	}
	_, _ = pipe.Exec(ctx)
}

func setSeckillProductInfo(ctx context.Context, seckillProductId, productId, price int64, name string, startTime, endTime, ttl int64) error {
	infoKey := fmt.Sprintf(keyPrefixSeckillInfo, seckillProductId)
	infoValue := fmt.Sprintf("%d:%d:%d:%d", productId, price, startTime, endTime)
	if err := redisClient.Set(ctx, infoKey, infoValue, time.Duration(ttl)*time.Second).Err(); err != nil {
		return err
	}
	return redisClient.Set(ctx, fmt.Sprintf(keyPrefixSeckillName, seckillProductId), name, time.Duration(ttl)*time.Second).Err()
}

func splitStockIntoShards(total int64) []int64 {
	if total < 0 {
		total = 0
	}
	shards := make([]int64, defaultShardCount)
	base := total / defaultShardCount
	rem := total % defaultShardCount
	for i := range shards {
		shards[i] = base
		if int64(i) < rem {
			shards[i]++
		}
	}
	return shards
}
