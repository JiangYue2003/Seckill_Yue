package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
)

const (
	keyPrefixSeckillInfo  = "seckill:info:"
	keyPrefixSeckillName  = "seckill:product_name:"
	keyPrefixSeckillStock = "seckill:stock:"
	keyPrefixSeckillUser  = "seckill:user:"

	benchmarkUserStart = int64(10000)
	defaultMySQLDSN    = "root:Zz123456@tcp(localhost:3306)/seckill_mall?charset=utf8mb4&parseTime=True&loc=Local"
	defaultTargets     = "http://127.0.0.1:8888"
	defaultJWTSecret   = "seckill-mall-jwt-secret-key-2026"
	defaultGatewayPath = "/api/v1/seckill/status"
)

var (
	targetsFlag = flag.String("gateway-targets", defaultTargets, "comma-separated gateway targets, e.g. http://127.0.0.1:8888,http://127.0.0.1:18888")
	modeFlag    = flag.String("mode", "seckill", "benchmark mode: seckill | gateway")
	pathFlag    = flag.String("path", defaultGatewayPath, "path used in gateway mode, e.g. /api/v1/seckill/status")
	productID   = flag.Int64("product-id", 9101, "seckill product id")
	stock       = flag.Int64("stock", 15000, "initial seckill stock")
	users       = flag.Int64("users", 100000, "unique users/token pool size")
	rps         = flag.Int("rate", 20000, "target requests per second")
	duration    = flag.Duration("duration", 30*time.Second, "test duration, e.g. 30s")
	workers     = flag.Int("workers", 4096, "worker goroutines")
	queueSize   = flag.Int("queue-size", 20000, "request queue size")
	reqTimeout  = flag.Duration("request-timeout", 3*time.Second, "per-request timeout")
	quantity    = flag.Int64("quantity", 1, "purchase quantity per request")
	redisAddr   = flag.String("redis", "localhost:6379", "redis address")
	jwtSecret   = flag.String("jwt-secret", defaultJWTSecret, "jwt secret for gateway auth")
	idealMode   = flag.Bool("ideal", false, "auto expand stock/users for near-all-success throughput test")
)

var (
	redisClient *redis.Client
	mysqlDB     *sql.DB
)

type requestTask struct {
	seq int64
}

type gatewayResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type seckillData struct {
	Success bool   `json:"success"`
	Code    string `json:"code"`
	Message string `json:"message"`
	OrderID string `json:"orderId"`
}

func main() {
	flag.Parse()

	if *users <= 0 || *rps <= 0 || *duration <= 0 || *workers <= 0 || *queueSize <= 0 {
		log.Fatal("invalid args: users/rate/duration/workers/queue-size must be > 0")
	}
	mode := strings.ToLower(strings.TrimSpace(*modeFlag))
	if mode != "seckill" && mode != "gateway" {
		log.Fatal("invalid --mode, allowed values: seckill | gateway")
	}
	gatewayPath := normalizePath(*pathFlag)

	targets := parseTargets(*targetsFlag)
	if len(targets) == 0 {
		log.Fatal("invalid --gateway-targets")
	}
	expectedRequests := int64(math.Ceil(float64(*rps) * duration.Seconds()))
	effectiveStock := *stock
	effectiveUsers := *users
	if *idealMode && mode == "seckill" {
		minRequired := expectedRequests + 1000
		if effectiveStock < minRequired {
			effectiveStock = minRequired
		}
		if effectiveUsers < minRequired {
			effectiveUsers = minRequired
		}
	}

	fmt.Println("========================================")
	fmt.Println(" Gateway 半链路压测 (Go Open-Loop)")
	fmt.Println("========================================")
	fmt.Printf("Mode: %s\n", mode)
	if mode == "gateway" {
		fmt.Printf("Path: %s\n", gatewayPath)
	}
	fmt.Printf("Targets: %s\n", strings.Join(targets, ","))
	fmt.Printf("Config: rate=%d req/s, duration=%s, workers=%d, queue=%d, users=%d, stock=%d\n",
		*rps, duration.String(), *workers, *queueSize, effectiveUsers, effectiveStock)
	fmt.Printf("Expected requests (target): %d\n", expectedRequests)
	if mode == "seckill" && !*idealMode && (*stock < expectedRequests || *users < expectedRequests) {
		fmt.Printf("[WARN] stock/users below expected requests, results will include many SOLD_OUT or duplicates. recommended: --ideal or stock/users >= %d\n", expectedRequests)
	}
	if *idealMode && mode == "seckill" {
		fmt.Println("[OK] ideal mode enabled: stock/users auto-expanded for near-all-success throughput measurement")
	} else if *idealMode && mode == "gateway" {
		fmt.Println("[WARN] --ideal is ignored in gateway mode")
	}
	fmt.Println()

	var jwtPool []UserToken
	if mode == "seckill" {
		initRedis(*redisAddr)
		initMySQL()
		defer closeMySQL()

		jwtPool = BuildJWTPool(benchmarkUserStart, effectiveUsers, *jwtSecret)
		fmt.Printf("[OK] JWT token pool generated: %d users\n", len(jwtPool))

		ctx := context.Background()
		prepareScenario(ctx, *productID, effectiveStock, effectiveUsers)
		defer cleanupScenario(ctx, *productID, effectiveUsers)
	} else {
		// gateway转发能力模式：仍准备JWT池，确保通过鉴权并触发真实转发RPC。
		jwtPool = BuildJWTPool(benchmarkUserStart, effectiveUsers, *jwtSecret)
		fmt.Printf("[OK] JWT token pool generated: %d users\n", len(jwtPool))
		fmt.Println("[OK] gateway mode: skip redis/mysql seckill data preparation")
	}

	client := &http.Client{
		Timeout: *reqTimeout,
		Transport: &http.Transport{
			MaxIdleConns:        *workers * 2,
			MaxIdleConnsPerHost: *workers,
			MaxConnsPerHost:     *workers * 2,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	runOpenLoop(mode, gatewayPath, targets, client, jwtPool, effectiveStock)
}

func runOpenLoop(mode, gatewayPath string, targets []string, client *http.Client, jwtPool []UserToken, configuredStock int64) {
	var payload []byte
	if mode == "seckill" {
		payload, _ = json.Marshal(map[string]any{
			"seckillProductId": *productID,
			"quantity":         *quantity,
		})
	}

	metrics := NewMetrics()
	tasks := make(chan requestTask, *queueSize)

	var targetCounter uint64
	var wg sync.WaitGroup
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range tasks {
				start := time.Now()
				target := targets[atomic.AddUint64(&targetCounter, 1)%uint64(len(targets))]
				var req *http.Request
				var err error
				if mode == "gateway" {
					url := strings.TrimRight(target, "/") + withSeckillProductID(gatewayPath, *productID)
					req, err = http.NewRequest(http.MethodGet, url, nil)
					if err == nil && len(jwtPool) > 0 {
						uidx := int(task.seq % int64(len(jwtPool)))
						user := jwtPool[uidx]
						req.Header.Set("Authorization", "Bearer "+user.Token)
					}
				} else {
					uidx := int(task.seq % int64(len(jwtPool)))
					user := jwtPool[uidx]
					url := strings.TrimRight(target, "/") + "/api/v1/seckill"
					req, err = http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
					if err == nil {
						req.Header.Set("Content-Type", "application/json")
						req.Header.Set("Authorization", "Bearer "+user.Token)
					}
				}
				if err != nil {
					metrics.RecordSystemFail(time.Since(start).Milliseconds(), "ERR_NEW_REQUEST")
					continue
				}

				resp, err := client.Do(req)
				if err != nil {
					metrics.RecordSystemFail(time.Since(start).Milliseconds(), classifyHTTPDoError(err))
					continue
				}
				body, readErr := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				latencyMs := time.Since(start).Milliseconds()
				if readErr != nil {
					metrics.RecordSystemFail(latencyMs, "ERR_READ_BODY")
					continue
				}

				if resp.StatusCode != http.StatusOK {
					metrics.RecordSystemFail(latencyMs, "HTTP_"+strconv.Itoa(resp.StatusCode))
					continue
				}

				if mode == "gateway" {
					metrics.RecordResult(latencyMs, classifyGatewayOnlyResult(body))
					continue
				}
				r := classifyGatewayResult(body)
				metrics.RecordResult(latencyMs, r)
			}
		}()
	}

	start := time.Now()
	end := start.Add(*duration)
	const tickInterval = 10 * time.Millisecond
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	var budget float64

	fmt.Println("----------------------------------------")
	fmt.Println("开始压测...")
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

	var totalStock, actualSold int64
	if mode == "seckill" {
		finalStock, _ := getStock(context.Background(), *productID)
		totalStock = configuredStock
		actualSold = configuredStock - finalStock
	}
	metrics.Report(totalDuration, totalStock, actualSold)
}

type result struct {
	success       bool
	bizCode       string
	failReason    string
	systemErr     bool
	systemErrCode string
}

func classifyGatewayResult(body []byte) result {
	var resp gatewayResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return result{systemErr: true, systemErrCode: "ERR_PARSE_GATEWAY_RESPONSE"}
	}

	if resp.Code != 0 {
		code := "GATEWAY_CODE_" + strconv.Itoa(resp.Code)
		return result{systemErr: true, systemErrCode: code, failReason: code}
	}

	var data seckillData
	if len(resp.Data) == 0 || string(resp.Data) == "null" {
		return result{systemErr: true, systemErrCode: "ERR_EMPTY_DATA"}
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return result{systemErr: true, systemErrCode: "ERR_PARSE_DATA"}
	}

	if data.Success || data.Code == "SUCCESS" {
		return result{success: true, bizCode: "SUCCESS"}
	}

	code := data.Code
	if code == "" {
		code = "EMPTY_CODE"
	}
	out := result{
		success:    false,
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

func classifyGatewayOnlyResult(body []byte) result {
	var resp gatewayResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		// 兼容 /health 等纯文本响应
		return result{success: true, bizCode: "GATEWAY_OK"}
	}
	if resp.Code == 0 {
		return result{success: true, bizCode: "GATEWAY_OK"}
	}
	code := "GATEWAY_CODE_" + strconv.Itoa(resp.Code)
	return result{
		success:       false,
		bizCode:       code,
		failReason:    code,
		systemErr:     true,
		systemErrCode: code,
	}
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

func normalizePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "/health"
	}
	if !strings.HasPrefix(p, "/") {
		return "/" + p
	}
	return p
}

func withSeckillProductID(path string, seckillProductID int64) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "seckillProductId=" + strconv.FormatInt(seckillProductID, 10)
}

func classifyHTTPDoError(err error) string {
	if err == nil {
		return "ERR_HTTP_DO_OTHER"
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if urlErr.Timeout() {
			return "ERR_HTTP_DO_TIMEOUT"
		}
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "ERR_HTTP_DO_TIMEOUT"
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "connection refused") || strings.Contains(msg, "connectex"):
		return "ERR_HTTP_DO_CONNECTION_REFUSED"
	case strings.Contains(msg, "connection reset") || strings.Contains(msg, "reset by peer"):
		return "ERR_HTTP_DO_CONNECTION_RESET"
	case strings.Contains(msg, "forbidden by its access permissions") || strings.Contains(msg, "only one usage of each socket address"):
		return "ERR_HTTP_DO_ADDR_IN_USE"
	case strings.Contains(msg, "no buffer space available"):
		return "ERR_HTTP_DO_NO_BUFFER_SPACE"
	case strings.Contains(msg, "timeout"):
		return "ERR_HTTP_DO_TIMEOUT"
	default:
		return "ERR_HTTP_DO_OTHER"
	}
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

func prepareScenario(ctx context.Context, seckillProductId, initStock, userCount int64) {
	cleanupScenario(ctx, seckillProductId, userCount)

	now := time.Now().Unix()
	startTime := now - 3600
	endTime := now + 3600
	ttl := int64(86400)
	if err := setSeckillProductInfo(ctx, seckillProductId, 1, 999, "gateway压测商品", startTime, endTime, ttl); err != nil {
		log.Fatalf("初始化秒杀商品信息失败: %v", err)
	}
	if err := initStockValue(ctx, seckillProductId, initStock); err != nil {
		log.Fatalf("初始化库存失败: %v", err)
	}
	fmt.Printf("[OK] 场景已初始化: productId=%d, stock=%d\n", seckillProductId, initStock)
}

func cleanupScenario(ctx context.Context, seckillProductId, userCount int64) {
	rollbackStock(ctx, seckillProductId, 1000000)

	pipe := redisClient.Pipeline()
	for i := int64(0); i < userCount; i++ {
		key := fmt.Sprintf("%s%d:%d", keyPrefixSeckillUser, seckillProductId, benchmarkUserStart+i)
		pipe.Del(ctx, key)
	}
	_, _ = pipe.Exec(ctx)

	if mysqlDB != nil {
		startUserID := benchmarkUserStart
		endUserID := benchmarkUserStart + userCount - 1
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

func initStockValue(ctx context.Context, seckillProductId, stock int64) error {
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

func setSeckillProductInfo(ctx context.Context, seckillProductId, productId, price int64, name string, startTime, endTime, ttl int64) error {
	infoKey := keyPrefixSeckillInfo + strconv.FormatInt(seckillProductId, 10)
	infoValue := fmt.Sprintf("%d:%d:%d:%d", productId, price, startTime, endTime)
	if err := redisClient.Set(ctx, infoKey, infoValue, time.Duration(ttl)*time.Second).Err(); err != nil {
		return err
	}
	nameKey := keyPrefixSeckillName + strconv.FormatInt(seckillProductId, 10)
	return redisClient.Set(ctx, nameKey, name, time.Duration(ttl)*time.Second).Err()
}
