package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
)

const (
	baseURL   = "http://localhost:8888"
	mysqlDSN  = "root:Zz123456@tcp(localhost:3306)/seckill_mall?charset=utf8mb4&parseTime=False&loc=Local"
	redisAddr = "localhost:6379"
)

// APIResponse 统一响应格式（匹配 middleware.Success 的格式）
type APIResponse struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// RegisterData 注册响应
type RegisterData struct {
	Id       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
}

// LoginData 登录响应
type LoginData struct {
	UserId       int64  `json:"userId"`
	Username     string `json:"username"`
	Email        string `json:"email"`
	AccessToken  string `json:"accessToken"`
	AccessExpire int64  `json:"accessExpireAt"`
	RefreshToken string `json:"refreshToken"`
}

// SeckillProduct 秒杀商品
type SeckillProduct struct {
	Id           int64   `json:"id"`
	ProductId    int64   `json:"productId"`
	SeckillPrice float64 `json:"seckillPrice"`
	SeckillStock int64   `json:"seckillStock"`
	SoldCount    int64   `json:"soldCount"`
	PerLimit     int32   `json:"perLimit"`
	Status       int32   `json:"status"`
	ProductName  string  `json:"productName"`
	ProductPrice float64 `json:"productPrice"`
}

// SeckillData 秒杀响应
type SeckillData struct {
	Success bool   `json:"success"`
	Code    any    `json:"code"`
	Message string `json:"message"`
	OrderId string `json:"orderId"`
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

func main() {
	fmt.Println("========================================")
	fmt.Println("  秒杀系统端到端测试 - 全链路验证")
	fmt.Println("========================================")
	fmt.Println()

	// 0. 初始化测试数据（确保有秒杀商品可测）
	if err := setupTestData(); err != nil {
		fmt.Printf("⚠️  初始化测试数据失败: %v\n", err)
		fmt.Println("   继续执行，可能查询不到秒杀商品...")
		fmt.Println()
	}

	// 生成唯一用户名（时间戳避免冲突）
	timestamp := time.Now().UnixNano() / 1e6
	testUsername := fmt.Sprintf("testuser_%d", timestamp)
	testPassword := "Test123456"
	testEmail := fmt.Sprintf("test_%d@example.com", timestamp)

	fmt.Printf("[流程 1/4] 用户注册: %s\n", testUsername)

	// 1. 注册
	registerResp, err := doRegister(testUsername, testPassword, testEmail)
	if err != nil {
		fmt.Printf("❌ 注册失败: %v\n", err)
		return
	}
	fmt.Printf("✅ 注册成功: userId=%d\n\n", registerResp.Id)

	// 2. 登录
	fmt.Println("[流程 2/4] 用户登录")
	loginResp, err := doLogin(testUsername, testPassword)
	if err != nil {
		fmt.Printf("❌ 登录失败: %v\n", err)
		return
	}
	fmt.Printf("✅ 登录成功: userId=%d, token=%s...\n\n", loginResp.UserId, truncate(loginResp.AccessToken, 20))

	// 3. 获取秒杀商品列表
	fmt.Println("[流程 3/4] 获取秒杀商品列表")
	products, err := doListSeckillProducts(loginResp.AccessToken)
	if err != nil {
		fmt.Printf("❌ 获取秒杀商品失败: %v\n", err)
		return
	}

	if len(products) == 0 {
		fmt.Println("⚠️  当前没有正在秒杀的商品，跳过秒杀步骤")
		fmt.Println()
	} else {
		fmt.Printf("✅ 获取到 %d 个秒杀商品\n", len(products))
		for i, p := range products {
			fmt.Printf("   [%d] %s (id=%d, 秒杀价=%.2f, 库存=%d)\n",
				i+1, p.ProductName, p.Id, p.SeckillPrice, p.SeckillStock)
		}
		fmt.Println()

		// 4. 执行秒杀（使用第一个商品）
		firstProduct := products[0]
		fmt.Printf("[流程 4/4] 执行秒杀: %s (seckillProductId=%d)\n", firstProduct.ProductName, firstProduct.Id)
		seckillResp, err := doSeckill(firstProduct.Id, 1, loginResp.AccessToken)
		if err != nil {
			fmt.Printf("❌ 秒杀请求失败: %v\n", err)
			return
		}
		fmt.Printf("✅ 秒杀完成: success=%v, code=%v, message=%s, orderId=%s\n\n",
			seckillResp.Success, seckillResp.Code, seckillResp.Message, seckillResp.OrderId)
	}

	fmt.Println("========================================")
	fmt.Println("  全链路测试完成！")
	fmt.Println("========================================")
	fmt.Println()
	fmt.Println("请检查各服务的控制台输出，验证 OpenTelemetry 链路追踪是否正常：")
	fmt.Println("  - gateway          (应有 HTTP span)")
	fmt.Println("  - user-service     (应有 gRPC server span + gRPC client span)")
	fmt.Println("  - product-service  (应有 gRPC server span)")
	fmt.Println("  - seckill-service  (应有 gRPC server span)")
	fmt.Println("  - order-service    (应有 gRPC server span)")
}

// setupTestData 初始化测试数据：创建秒杀商品并同步到 Redis
func setupTestData() error {
	ctx := context.Background()
	now := time.Now().Unix()
	// 秒杀活动持续 2 小时
	startTime := now - 60              // 1 分钟前开始（确保在进行中）
	endTime := now + 2*60*60           // 2 小时后结束
	ttlSeconds := int64(2*60*60 + 120) // Redis TTL 比活动结束多 2 分钟

	// 测试商品 ID（固定，便于幂等）
	testProductId := int64(999001)
	testSeckillProductId := int64(999001)

	// 1. 连接 MySQL
	db, err := sql.Open("mysql", mysqlDSN)
	if err != nil {
		return fmt.Errorf("连接 MySQL 失败: %w", err)
	}
	defer db.Close()
	db.SetConnMaxLifetime(5 * time.Second)

	if err := db.Ping(); err != nil {
		return fmt.Errorf("MySQL Ping 失败: %w", err)
	}

	// 2. 创建测试商品（如果不存在）
	_, err = db.Exec(`
		INSERT IGNORE INTO products (id, name, description, price, stock, sold_count, cover_image, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		testProductId,
		"测试秒杀商品-iPhone16",
		"全链路测试专用商品",
		699900, // 原价 6999.00 元
		100,
		0,
		"https://example.com/iphone16.jpg",
		1, // 上架
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("创建测试商品失败: %w", err)
	}
	fmt.Println("✅ 测试商品已就绪 (productId=999001)")

	// 3. 创建秒杀商品（如果不存在则创建，存在则更新时间和状态）
	_, err = db.Exec(`
		INSERT INTO seckill_products (id, product_id, seckill_price, seckill_stock, sold_count, start_time, end_time, per_limit, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			seckill_price = VALUES(seckill_price),
			seckill_stock = VALUES(seckill_stock),
			sold_count = 0,
			start_time = VALUES(start_time),
			end_time = VALUES(end_time),
			status = VALUES(status),
			updated_at = VALUES(updated_at)`,
		testSeckillProductId,
		testProductId,
		499900, // 秒杀价 4999.00 元
		50,
		0,
		startTime,
		endTime,
		1, // 每人限购 1 件
		1, // 状态: 进行中
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("创建秒杀商品失败: %w", err)
	}
	fmt.Println("✅ 秒杀商品已就绪 (seckillProductId=999001)")

	// 4. 同步到 Redis（seckill-service 从 Redis 读取库存）
	rdb := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})
	defer rdb.Close()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("Redis Ping 失败: %w", err)
	}

	// 4.1 设置秒杀库存
	stockKey := fmt.Sprintf("seckill:stock:%d", testSeckillProductId)
	if err := rdb.Set(ctx, stockKey, 50, time.Duration(ttlSeconds)*time.Second).Err(); err != nil {
		return fmt.Errorf("设置秒杀库存 Redis 失败: %w", err)
	}

	// 4.2 设置秒杀商品信息 (productId:seckillPrice:startTime:endTime)
	infoKey := fmt.Sprintf("seckill:info:%d", testSeckillProductId)
	infoValue := fmt.Sprintf("%d:%d:%d:%d", testProductId, 499900, startTime, endTime)
	if err := rdb.Set(ctx, infoKey, infoValue, time.Duration(ttlSeconds)*time.Second).Err(); err != nil {
		return fmt.Errorf("设置秒杀商品信息 Redis 失败: %w", err)
	}

	// 4.3 设置秒杀商品名称
	nameKey := fmt.Sprintf("seckill:product_name:%d", testSeckillProductId)
	if err := rdb.Set(ctx, nameKey, "测试秒杀商品-iPhone16", time.Duration(ttlSeconds)*time.Second).Err(); err != nil {
		return fmt.Errorf("设置秒杀商品名称 Redis 失败: %w", err)
	}

	fmt.Printf("✅ Redis 缓存已同步 (活动有效期至 %s)\n", time.Unix(endTime, 0).Format("2006-01-02 15:04:05"))
	fmt.Println()

	return nil
}

// doRegister POST /api/v1/user/register
func doRegister(username, password, email string) (*RegisterData, error) {
	body := map[string]string{
		"username": username,
		"password": password,
		"email":    email,
	}
	respBody, statusCode, err := doRequest("POST", baseURL+"/api/v1/user/register", body, "")
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", statusCode, respBody)
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}
	if apiResp.Code != 0 {
		return nil, fmt.Errorf("业务错误: %s", apiResp.Msg)
	}

	var data RegisterData
	if err := json.Unmarshal(apiResp.Data, &data); err != nil {
		return nil, fmt.Errorf("解析data失败: %v", err)
	}
	return &data, nil
}

// doLogin POST /api/v1/user/login
func doLogin(username, password string) (*LoginData, error) {
	body := map[string]string{
		"username": username,
		"password": password,
	}
	respBody, statusCode, err := doRequest("POST", baseURL+"/api/v1/user/login", body, "")
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", statusCode, respBody)
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}
	if apiResp.Code != 0 {
		return nil, fmt.Errorf("业务错误: %s", apiResp.Msg)
	}

	var data LoginData
	if err := json.Unmarshal(apiResp.Data, &data); err != nil {
		return nil, fmt.Errorf("解析data失败: %v", err)
	}
	return &data, nil
}

// doListSeckillProducts GET /api/v1/seckill/products
func doListSeckillProducts(token string) ([]SeckillProduct, error) {
	respBody, statusCode, err := doRequest("GET", baseURL+"/api/v1/seckill/products", nil, token)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", statusCode, respBody)
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}
	if apiResp.Code != 0 {
		return nil, fmt.Errorf("业务错误: %s", apiResp.Msg)
	}

	// data 结构: { "products": [...] }
	var wrapper struct {
		Products []SeckillProduct `json:"products"`
	}
	if err := json.Unmarshal(apiResp.Data, &wrapper); err != nil {
		return nil, fmt.Errorf("解析data失败: %v", err)
	}
	return wrapper.Products, nil
}

// doSeckill POST /api/v1/seckill
func doSeckill(seckillProductId int64, quantity int, token string) (*SeckillData, error) {
	body := map[string]interface{}{
		"seckillProductId": seckillProductId,
		"quantity":         quantity,
	}
	respBody, statusCode, err := doRequest("POST", baseURL+"/api/v1/seckill", body, token)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", statusCode, respBody)
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}
	if apiResp.Code != 0 {
		return nil, fmt.Errorf("业务错误: %s", apiResp.Msg)
	}

	var data SeckillData
	if err := json.Unmarshal(apiResp.Data, &data); err != nil {
		return nil, fmt.Errorf("解析data失败: %v", err)
	}
	return &data, nil
}

// doRequest 统一 HTTP 请求封装
func doRequest(method, url string, body interface{}, token string) ([]byte, int, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("序列化body失败: %v", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("创建请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("读取响应失败: %v", err)
	}
	return respBody, resp.StatusCode, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// 消除未使用的警告
var _ = strconv.FormatInt
