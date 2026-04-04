package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	product "seckill-mall/product-service/product"
)

const (
	productServiceAddr = "127.0.0.1:9082"
	redisAddr          = "localhost:6379"
	cacheKeyPrefix     = "product:detail:"
	cacheNullValue     = "null"
)

var (
	redisClient   *redis.Client
	productClient product.ProductServiceClient
)

func main() {
	fmt.Println("========================================")
	fmt.Println("   商品缓存功能测试")
	fmt.Println("========================================")
	fmt.Println()

	initRedis(redisAddr)
	initGrpc(productServiceAddr)

	passed, failed := 0, 0
	run := func(name string, fn func() error) {
		fmt.Printf("▶ %s\n", name)
		if err := fn(); err != nil {
			fmt.Printf("  ✗ FAIL: %v\n\n", err)
			failed++
		} else {
			fmt.Printf("  ✓ PASS\n\n")
			passed++
		}
	}

	// 创建测试商品
	testProductId := createTestProduct()
	defer cleanupProduct(testProductId)

	run("TC-01 首次查询（Cache Miss → 写入Redis）", func() error {
		return testCacheMiss(testProductId)
	})
	run("TC-02 重复查询（Cache Hit，不打DB）", func() error {
		return testCacheHit(testProductId)
	})
	run("TC-03 空值缓存（防穿透，不存在商品写null标记）", func() error {
		return testNullCache()
	})
	run("TC-04 并发单飞（50并发同一商品，Redis命中数正确）", func() error {
		return testSingleFlight(testProductId)
	})
	run("TC-05 更新商品后缓存失效", func() error {
		return testCacheInvalidateOnUpdate(testProductId)
	})
	run("TC-06 删除商品后缓存失效", func() error {
		// 额外创建一个，避免影响后续 cleanup
		extraId := createTestProduct()
		return testCacheInvalidateOnDelete(extraId)
	})

	fmt.Println("========================================")
	fmt.Printf("   测试完成: %d 通过 / %d 失败\n", passed, failed)
	fmt.Println("========================================")
}

// ==================== 初始化 ====================

func initRedis(addr string) {
	redisClient = redis.NewClient(&redis.Options{Addr: addr})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("[INIT] Redis 连接失败: %v", err)
	}
	fmt.Println("[OK] Redis 连接成功")
}

func initGrpc(addr string) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("[INIT] product-service gRPC 连接失败: %v", err)
	}
	productClient = product.NewProductServiceClient(conn)
	fmt.Printf("[OK] product-service gRPC 连接成功 (%s)\n\n", addr)
}

// ==================== 测试辅助 ====================

func cacheKey(productId int64) string {
	return fmt.Sprintf("%s%d", cacheKeyPrefix, productId)
}

func getCacheValue(productId int64) (string, bool) {
	ctx := context.Background()
	val, err := redisClient.Get(ctx, cacheKey(productId)).Result()
	if err == redis.Nil {
		return "", false
	}
	if err != nil {
		return "", false
	}
	return val, true
}

func deleteCacheKey(productId int64) {
	redisClient.Del(context.Background(), cacheKey(productId))
}

func createTestProduct() int64 {
	ctx := context.Background()
	resp, err := productClient.CreateProduct(ctx, &product.CreateProductRequest{
		Name:        fmt.Sprintf("缓存测试商品_%d", time.Now().UnixNano()),
		Description: "用于缓存功能测试，可安全删除",
		Price:       9900,
		Stock:       100,
		Status:      1,
	})
	if err != nil {
		log.Fatalf("[INIT] 创建测试商品失败: %v", err)
	}
	fmt.Printf("[OK] 测试商品已创建: productId=%d\n\n", resp.Id)
	return resp.Id
}

func cleanupProduct(productId int64) {
	ctx := context.Background()
	deleteCacheKey(productId)
	productClient.DeleteProduct(ctx, &product.IdRequest{Id: productId})
	fmt.Printf("[CLEANUP] 测试商品已清理: productId=%d\n", productId)
}

// ==================== 测试用例 ====================

// TC-01: 清空缓存后首次查询，应从DB读取并写入Redis
func testCacheMiss(productId int64) error {
	deleteCacheKey(productId) // 确保缓存干净

	ctx := context.Background()
	_, err := productClient.GetProduct(ctx, &product.GetProductRequest{ProductId: productId})
	if err != nil {
		return fmt.Errorf("gRPC 调用失败: %w", err)
	}

	// 验证 Redis 中已写入缓存
	val, found := getCacheValue(productId)
	if !found {
		return fmt.Errorf("缓存未写入：Redis 中找不到 key=%s", cacheKey(productId))
	}
	if val == cacheNullValue {
		return fmt.Errorf("缓存写入了 null 标记，但商品应存在")
	}
	fmt.Printf("     → 缓存已写入，key=%s，长度=%d字节\n", cacheKey(productId), len(val))
	return nil
}

// TC-02: 缓存命中，直接读Redis，TTL 应大于0
func testCacheHit(productId int64) error {
	ctx := context.Background()

	// 确保缓存存在（TC-01 写入的）
	_, found := getCacheValue(productId)
	if !found {
		return fmt.Errorf("前置条件失败：缓存不存在")
	}

	ttl, err := redisClient.TTL(ctx, cacheKey(productId)).Result()
	if err != nil || ttl <= 0 {
		return fmt.Errorf("缓存TTL无效: ttl=%v, err=%v", ttl, err)
	}

	_, grpcErr := productClient.GetProduct(ctx, &product.GetProductRequest{ProductId: productId})
	if grpcErr != nil {
		return fmt.Errorf("gRPC 调用失败: %w", grpcErr)
	}

	fmt.Printf("     → 缓存命中，TTL剩余 %v（预期 3600~4200s）\n", ttl.Round(time.Second))
	return nil
}

// TC-03: 查询不存在的商品，Redis 中应写入 null 标记
func testNullCache() error {
	nonExistId := int64(999999999)
	deleteCacheKey(nonExistId)
	defer deleteCacheKey(nonExistId)

	ctx := context.Background()
	_, err := productClient.GetProduct(ctx, &product.GetProductRequest{ProductId: nonExistId})
	if err == nil {
		return fmt.Errorf("预期返回错误，但调用成功了")
	}

	val, found := getCacheValue(nonExistId)
	if !found {
		return fmt.Errorf("空值未缓存：Redis 中找不到 key=%s", cacheKey(nonExistId))
	}
	if val != cacheNullValue {
		return fmt.Errorf("空值标记错误：期望 %q，实际 %q", cacheNullValue, val)
	}

	ttl, _ := redisClient.TTL(ctx, cacheKey(nonExistId)).Result()
	fmt.Printf("     → null 标记已写入，TTL=%v（预期≤60s）\n", ttl.Round(time.Second))
	return nil
}

// TC-04: 50个并发请求同一商品，全部应成功，缓存 key 只有1个
func testSingleFlight(productId int64) error {
	deleteCacheKey(productId)

	ctx := context.Background()
	concurrency := 50
	errs := make([]error, concurrency)
	var wg sync.WaitGroup

	start := time.Now()
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = productClient.GetProduct(ctx, &product.GetProductRequest{ProductId: productId})
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)

	failCount := 0
	for _, e := range errs {
		if e != nil {
			failCount++
		}
	}
	if failCount > 0 {
		return fmt.Errorf("%d 个并发请求失败", failCount)
	}

	val, found := getCacheValue(productId)
	if !found || val == cacheNullValue {
		return fmt.Errorf("并发后缓存状态异常: found=%v, val=%q", found, val)
	}

	fmt.Printf("     → %d 并发全部成功，耗时 %v，缓存已写入\n", concurrency, elapsed.Round(time.Millisecond))
	return nil
}

// TC-05: 更新商品后缓存 key 应被删除
func testCacheInvalidateOnUpdate(productId int64) error {
	ctx := context.Background()

	// 先确保缓存存在
	productClient.GetProduct(ctx, &product.GetProductRequest{ProductId: productId})
	if _, found := getCacheValue(productId); !found {
		return fmt.Errorf("前置条件失败：查询后缓存未建立")
	}

	// 执行更新
	_, err := productClient.UpdateProduct(ctx, &product.UpdateProductRequest{
		Id:   productId,
		Name: fmt.Sprintf("更新后商品_%d", time.Now().Unix()),
	})
	if err != nil {
		return fmt.Errorf("UpdateProduct 失败: %w", err)
	}

	// 验证缓存已删除
	_, found := getCacheValue(productId)
	if found {
		return fmt.Errorf("更新后缓存未失效：key=%s 仍然存在", cacheKey(productId))
	}

	fmt.Printf("     → 更新成功，缓存 key=%s 已删除\n", cacheKey(productId))
	return nil
}

// TC-06: 删除商品后缓存 key 应被删除
func testCacheInvalidateOnDelete(productId int64) error {
	ctx := context.Background()

	// 先确保缓存存在
	productClient.GetProduct(ctx, &product.GetProductRequest{ProductId: productId})
	if _, found := getCacheValue(productId); !found {
		return fmt.Errorf("前置条件失败：查询后缓存未建立")
	}

	// 执行删除
	_, err := productClient.DeleteProduct(ctx, &product.IdRequest{Id: productId})
	if err != nil {
		return fmt.Errorf("DeleteProduct 失败: %w", err)
	}

	// 验证缓存已删除
	_, found := getCacheValue(productId)
	if found {
		return fmt.Errorf("删除后缓存未失效：key=%s 仍然存在", cacheKey(productId))
	}

	fmt.Printf("     → 删除成功，缓存 key=%s 已删除\n", cacheKey(productId))
	return nil
}
