package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// SeckillProduct 秒杀商品结构
type SeckillProduct struct {
	Id    int64
	Price int64
	Name  string
	Stock int64
}

var (
	redisHost    string
	redisPort    int
	testProducts = []SeckillProduct{
		{Id: 1, Price: 599900, Name: "iPhone 15 Pro 256GB", Stock: 100},
		{Id: 2, Price: 899900, Name: "MacBook Air M3 8GB", Stock: 50},
		{Id: 3, Price: 149900, Name: "AirPods Pro 2代", Stock: 200},
		{Id: 4, Price: 599900, Name: "iPad Pro 11寸 256GB", Stock: 80},
		{Id: 5, Price: 299900, Name: "Apple Watch Series 9", Stock: 150},
	}
	functionalTestProducts = []int64{1001, 1002, 1003, 1004}
	benchmarkTestProducts  = []int64{2001}
	testUsers              = []int64{10001, 10002, 10021, 10022, 10031, 10041, 100001, 100002}
)

const (
	keyPrefixSeckillInfo  = "seckill:info:"
	keyPrefixSeckillName  = "seckill:product_name:"
	keyPrefixSeckillStock = "seckill:stock:"
	keyPrefixSeckillUser  = "seckill:user:"
	keyPrefixSeckillOrder = "seckill:order:"
)

func main() {
	flag.StringVar(&redisHost, "redis", "localhost", "Redis host")
	flag.IntVar(&redisPort, "port", 6379, "Redis port")
	mode := flag.String("mode", "", "init|cleanup")
	flag.Parse()

	addr := fmt.Sprintf("%s:%d", redisHost, redisPort)
	client := redis.NewClient(&redis.Options{
		Addr: addr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		log.Fatalf("连接 Redis 失败: %v", err)
	}
	fmt.Printf("[OK] Redis 连接成功: %s\n", addr)

	switch *mode {
	case "init":
		initTestData(client)
	case "cleanup":
		cleanupTestData(client)
	default:
		fmt.Println("使用方法:")
		fmt.Println("  初始化数据: go run . -mode=init")
		fmt.Println("  清理数据:   go run . -mode=cleanup")
	}
}

func initTestData(client *redis.Client) {
	fmt.Println("\n========================================")
	fmt.Println("   初始化秒杀测试数据")
	fmt.Println("========================================")

	ctx := context.Background()
	now := time.Now().Unix()
	startTime := now - 3600
	endTime := now + 86400

	// 初始化秒杀商品
	for _, p := range testProducts {
		initProduct(ctx, client, p.Id, p.Id, p.Price, p.Name, p.Stock, startTime, endTime)
	}

	// 初始化功能测试商品
	for _, pid := range functionalTestProducts {
		initProduct(ctx, client, pid, pid, 999, "功能测试商品", 10, startTime, endTime)
	}

	// 初始化性能测试商品
	for _, pid := range benchmarkTestProducts {
		initProduct(ctx, client, pid, pid, 999, "压测商品", 500, startTime, endTime)
	}

	fmt.Println("\n========================================")
	fmt.Println("   测试数据初始化完成")
	fmt.Println("========================================")
	printProductList()
}

func initProduct(ctx context.Context, client *redis.Client, seckillProductId, productId, price int64, name string, stock, startTime, endTime int64) {
	infoKey := keyPrefixSeckillInfo + strconv.FormatInt(seckillProductId, 10)
	infoValue := fmt.Sprintf("%d:%d:%d:%d", productId, price, startTime, endTime)
	if err := client.Set(ctx, infoKey, infoValue, 24*time.Hour).Err(); err != nil {
		log.Printf("[错误] 设置商品信息失败: %v", err)
		return
	}

	nameKey := keyPrefixSeckillName + strconv.FormatInt(seckillProductId, 10)
	if err := client.Set(ctx, nameKey, name, 24*time.Hour).Err(); err != nil {
		log.Printf("[错误] 设置商品名称失败: %v", err)
		return
	}

	stockKey := keyPrefixSeckillStock + strconv.FormatInt(seckillProductId, 10)
	if err := client.Set(ctx, stockKey, stock, 0).Err(); err != nil {
		log.Printf("[错误] 设置库存失败: %v", err)
		return
	}

	fmt.Printf("[OK] 秒杀商品 ID=%d (%s) - 库存: %d\n", seckillProductId, name, stock)
}

func cleanupTestData(client *redis.Client) {
	fmt.Println("\n========================================")
	fmt.Println("   清理秒杀测试数据")
	fmt.Println("========================================")

	ctx := context.Background()

	// 收集所有需要删除的 key
	allProductIds := append([]int64{}, testProducts[0].Id)
	for _, p := range testProducts {
		allProductIds = append(allProductIds, p.Id)
	}
	allProductIds = append(allProductIds, functionalTestProducts...)
	allProductIds = append(allProductIds, benchmarkTestProducts...)

	// 删除商品相关 keys
	for _, pid := range allProductIds {
		pidStr := strconv.FormatInt(pid, 10)
		keys := []string{
			keyPrefixSeckillInfo + pidStr,
			keyPrefixSeckillName + pidStr,
			keyPrefixSeckillStock + pidStr,
		}
		client.Del(ctx, keys...)
		fmt.Printf("[清理] 商品 ID=%d\n", pid)
	}

	// 删除用户购买记录
	for _, uid := range testUsers {
		for _, pid := range allProductIds {
			userKey := keyPrefixSeckillUser + strconv.FormatInt(pid, 10) + ":" + strconv.FormatInt(uid, 10)
			client.Del(ctx, userKey)
		}
	}
	fmt.Printf("[清理] 删除 %d 个用户的购买记录\n", len(testUsers))

	// 删除测试订单
	orderKeys, err := client.Keys(ctx, keyPrefixSeckillOrder+"S*").Result()
	if err == nil && len(orderKeys) > 0 {
		if len(orderKeys) > 0 {
			client.Del(ctx, orderKeys...)
			fmt.Printf("[清理] 删除 %d 个测试订单\n", len(orderKeys))
		}
	}

	// 删除功能测试的额外用户记录
	extraUsers := []int64{10001, 10002, 10021, 10022, 10031, 10041}
	for _, uid := range extraUsers {
		for _, pid := range functionalTestProducts {
			userKey := keyPrefixSeckillUser + strconv.FormatInt(pid, 10) + ":" + strconv.FormatInt(uid, 10)
			client.Del(ctx, userKey)
		}
	}

	fmt.Println("\n========================================")
	fmt.Println("   测试数据清理完成")
	fmt.Println("========================================")
}

func printProductList() {
	fmt.Println("\n秒杀商品列表:")
	fmt.Println("  ID | 商品名称                      | 秒杀价格    | 库存")
	fmt.Println("  ---|-------------------------------|------------|------")
	fmt.Println("   1 | iPhone 15 Pro 256GB           | Y5999.00   | 100")
	fmt.Println("   2 | MacBook Air M3 8GB            | Y8999.00   |  50")
	fmt.Println("   3 | AirPods Pro 2代               | Y1499.00   | 200")
	fmt.Println("   4 | iPad Pro 11寸 256GB           | Y5999.00   |  80")
	fmt.Println("   5 | Apple Watch Series 9          | Y2999.00   | 150")
	fmt.Println("")
	fmt.Println("运行功能测试: cd test/seckill-functional-test && go run .")
	fmt.Println("运行性能测试: cd test/seckill-benchmark-test && go run .")
}
