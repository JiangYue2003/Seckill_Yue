package redis

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// Seckill Lua 脚本结果
	LuaResultSuccess        = 1
	LuaResultStockNotEnough = 0
	LuaResultAlreadyBought  = -1
	LuaResultNotStarted     = -3
	LuaResultEnded          = -4

	// Key 前缀
	KeyPrefixSeckillStock       = "seckill:stock:"        // 秒杀库存
	KeyPrefixSeckillUser        = "seckill:user:"         // 用户预占记录（短期TTL，自动释放悬空库存）
	KeyPrefixSeckillOrder       = "seckill:order:"        // 秒杀订单状态
	KeyPrefixSeckillInfo        = "seckill:info:"         // 秒杀商品信息 (productId:seckillPrice:startTime:endTime)
	KeyPrefixSeckillProductName = "seckill:product_name:" // 秒杀商品名称

	// 订单状态常量（与 logic 包保持一致）
	OrderStatusPending  = "pending"
	OrderStatusSuccess  = "success"
	OrderStatusFailed   = "failed"
	OrderStatusNotStart = "not_started"
	OrderStatusEnded    = "ended"
)

// seckillLuaScript 秒杀 Lua 脚本
// 功能：原子性完成 时间校验 + 防重 + 库存扣减 + 预锁定
// 返回值：{结果码, 剩余库存}
// ARGV[1]: quantity, ARGV[2]: orderId, ARGV[3]: TTL, ARGV[4]: startTime, ARGV[5]: endTime
var seckillLuaScript = `
local stockKey = KEYS[1]
local userKey = KEYS[2]
local quantity = tonumber(ARGV[1])
local orderId = ARGV[2]
local ttl = tonumber(ARGV[3])
local startTime = tonumber(ARGV[4])
local endTime = tonumber(ARGV[5])
local now = tonumber(ARGV[6])

-- 0. 时间校验
if startTime > 0 and now < startTime then
    local currentStock = tonumber(redis.call('GET', stockKey) or 0)
    return {-3, currentStock}  -- 秒杀未开始
end
if endTime > 0 and now > endTime then
    local currentStock = tonumber(redis.call('GET', stockKey) or 0)
    return {-4, currentStock}  -- 秒杀已结束
end

-- 1. 检查用户是否已购买
local alreadyBought = redis.call('EXISTS', userKey)
if alreadyBought == 1 then
    local currentStock = tonumber(redis.call('GET', stockKey) or 0)
    return {-1, currentStock}
end

-- 2. 检查库存
local currentStock = tonumber(redis.call('GET', stockKey) or 0)
if currentStock < quantity then
    return {0, currentStock}
end

-- 3. 扣减库存
local newStock = redis.call('DECRBY', stockKey, quantity)
if newStock < 0 then
    redis.call('INCRBY', stockKey, quantity)
    return {0, currentStock}
end

-- 4. 记录用户购买记录（TTL 需足够覆盖订单处理时间）
redis.call('SETEX', userKey, ttl, orderId)

return {1, newStock}
`

// SeckillRedis Redis 客户端封装
type SeckillRedis struct {
	client     *redis.Client
	localStock sync.Map // key: seckillProductId(int64) → value: *atomic.Int64，本地库存计数器
}

// NewSeckillRedis 创建 SeckillRedis 实例
func NewSeckillRedis(host string) (*SeckillRedis, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     host,
		Password: "",
		DB:       0,
	})

	return &SeckillRedis{
		client: client,
	}, nil
}

// SeckillRequest 秒杀请求参数
type SeckillRequest struct {
	SeckillProductId int64
	UserId           int64
	Quantity         int64
	OrderId          string
	TTL              int64 // 过期时间(秒)
	StartTime        int64 // 秒杀开始时间戳（秒）
	EndTime          int64 // 秒杀结束时间戳（秒）
}

// SeckillResult 秒杀结果
type SeckillResult struct {
	Code  int   // 结果码: 1=成功, 0=库存不足, -1=已购买, -2=超限
	Stock int64 // 剩余库存
}

// DoSeckill 执行秒杀（原子性操作）
func (r *SeckillRedis) DoSeckill(ctx context.Context, req *SeckillRequest) (*SeckillResult, error) {
	stockKey := KeyPrefixSeckillStock + strconv.FormatInt(req.SeckillProductId, 10)
	userKey := KeyPrefixSeckillUser + strconv.FormatInt(req.SeckillProductId, 10) + ":" + strconv.FormatInt(req.UserId, 10)

	keys := []string{stockKey, userKey}
	argv := []interface{}{
		req.Quantity,
		req.OrderId,
		req.TTL,
		req.StartTime,
		req.EndTime,
		time.Now().Unix(),
	}

	// Lua 脚本返回 {code, stock}
	result, err := r.client.Eval(ctx, seckillLuaScript, keys, argv...).Result()
	if err != nil {
		return nil, fmt.Errorf("执行秒杀Lua脚本失败: %w", err)
	}

	// 解析返回值：Redis Lua 返回数组为 []interface{}
	arr, ok := result.([]interface{})
	if !ok || len(arr) != 2 {
		return nil, fmt.Errorf("Lua脚本返回值格式错误: %v", result)
	}

	code, ok1 := arr[0].(int64)
	stock, ok2 := arr[1].(int64)
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("Lua脚本返回值类型错误: code=%v, stock=%v", arr[0], arr[1])
	}

	return &SeckillResult{
		Code:  int(code),
		Stock: stock,
	}, nil
}

// InitStock 初始化秒杀库存（活动开始前调用）
func (r *SeckillRedis) InitStock(ctx context.Context, seckillProductId int64, stock int64) error {
	key := KeyPrefixSeckillStock + strconv.FormatInt(seckillProductId, 10)
	return r.client.Set(ctx, key, stock, 0).Err()
}

// GetStock 获取秒杀库存
func (r *SeckillRedis) GetStock(ctx context.Context, seckillProductId int64) (int64, error) {
	key := KeyPrefixSeckillStock + strconv.FormatInt(seckillProductId, 10)
	val, err := r.client.Get(ctx, key).Int64()
	if err != nil {
		if err == redis.Nil {
			return 0, nil
		}
		return 0, err
	}
	return val, nil
}

// SetOrderStatus 设置订单状态
func (r *SeckillRedis) SetOrderStatus(ctx context.Context, orderId string, status string, ttl int64) error {
	key := KeyPrefixSeckillOrder + orderId
	return r.client.Set(ctx, key, status, time.Duration(ttl)*time.Second).Err()
}

// GetOrderStatus 获取订单状态
func (r *SeckillRedis) GetOrderStatus(ctx context.Context, orderId string) (string, error) {
	key := KeyPrefixSeckillOrder + orderId
	val, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return "", nil
		}
		return "", err
	}
	return val, nil
}

// RollbackStock 回滚库存（秒杀失败时调用）
func (r *SeckillRedis) RollbackStock(ctx context.Context, seckillProductId int64, quantity int64) error {
	key := KeyPrefixSeckillStock + strconv.FormatInt(seckillProductId, 10)
	return r.client.IncrBy(ctx, key, quantity).Err()
}

// DeleteUserKey 删除用户购买记录（秒杀失败时调用）
func (r *SeckillRedis) DeleteUserKey(ctx context.Context, seckillProductId, userId int64) error {
	key := KeyPrefixSeckillUser + strconv.FormatInt(seckillProductId, 10) + ":" + strconv.FormatInt(userId, 10)
	return r.client.Del(ctx, key).Err()
}

// SetSeckillProductInfo 设置秒杀商品信息（活动开始前调用）
// productId:seckillPrice:startTime:endTime 存储在 info key 中
// productName 单独存储，避免商品名称中包含冒号导致解析错误
func (r *SeckillRedis) SetSeckillProductInfo(ctx context.Context, seckillProductId, productId, seckillPrice int64, productName string, startTime, endTime int64, ttlSeconds int64) error {
	infoKey := KeyPrefixSeckillInfo + strconv.FormatInt(seckillProductId, 10)
	infoValue := fmt.Sprintf("%d:%d:%d:%d", productId, seckillPrice, startTime, endTime)
	if err := r.client.Set(ctx, infoKey, infoValue, time.Duration(ttlSeconds)*time.Second).Err(); err != nil {
		return err
	}

	nameKey := KeyPrefixSeckillProductName + strconv.FormatInt(seckillProductId, 10)
	return r.client.Set(ctx, nameKey, productName, time.Duration(ttlSeconds)*time.Second).Err()
}

// GetSeckillProductInfo 获取秒杀商品信息（返回 productId, seckillPrice, productName, startTime, endTime）
func (r *SeckillRedis) GetSeckillProductInfo(ctx context.Context, seckillProductId int64) (int64, int64, string, int64, int64, error) {
	infoKey := KeyPrefixSeckillInfo + strconv.FormatInt(seckillProductId, 10)
	val, err := r.client.Get(ctx, infoKey).Result()
	if err != nil {
		if err == redis.Nil {
			return 0, 0, "", 0, 0, nil
		}
		return 0, 0, "", 0, 0, err
	}
	// 格式: productId:seckillPrice:startTime:endTime
	parts := strings.Split(val, ":")
	var productId, seckillPrice, startTime, endTime int64
	if len(parts) >= 1 {
		productId, _ = strconv.ParseInt(parts[0], 10, 64)
	}
	if len(parts) >= 2 {
		seckillPrice, _ = strconv.ParseInt(parts[1], 10, 64)
	}
	if len(parts) >= 3 {
		startTime, _ = strconv.ParseInt(parts[2], 10, 64)
	}
	if len(parts) >= 4 {
		endTime, _ = strconv.ParseInt(parts[3], 10, 64)
	}

	nameKey := KeyPrefixSeckillProductName + strconv.FormatInt(seckillProductId, 10)
	productName, _ := r.client.Get(ctx, nameKey).Result()
	if productName == "" {
		productName = "秒杀商品"
	}

	return productId, seckillPrice, productName, startTime, endTime, nil
}

// OrderInfo 订单信息（用于 GetSeckillResult 查询）
// 存储格式: status:productId:quantity:amount:productName
type OrderInfo struct {
	Status      string
	ProductId   int64
	Quantity    int64
	Amount      int64
	ProductName string
}

// FormatSeckillUserKey 格式化用户秒杀Key
func FormatSeckillUserKey(seckillProductId, userId int64) string {
	return strconv.FormatInt(seckillProductId, 10) + ":" + strconv.FormatInt(userId, 10)
}

// CheckUserKeyExists 检查用户购买记录是否存在
func (r *SeckillRedis) CheckUserKeyExists(ctx context.Context, userKey string) (bool, error) {
	exists, err := r.client.Exists(ctx, userKey).Result()
	if err != nil {
		return false, err
	}
	return exists > 0, nil
}

// GetUserOrderId 获取用户对应的订单号（userKey 的 value 即为 orderId）
func (r *SeckillRedis) GetUserOrderId(ctx context.Context, userKey string) (string, error) {
	val, err := r.client.Get(ctx, userKey).Result()
	if err != nil {
		if err == redis.Nil {
			return "", nil
		}
		return "", err
	}
	return val, nil
}

// SetOrderInfo 设置订单完整信息（用于 GetSeckillResult 查询）
// 存储格式: status:productId:quantity:amount:productName
func (r *SeckillRedis) SetOrderInfo(ctx context.Context, orderId string, info *OrderInfo, ttl int64) error {
	key := KeyPrefixSeckillOrder + orderId
	value := fmt.Sprintf("%s:%d:%d:%d:%s", info.Status, info.ProductId, info.Quantity, info.Amount, info.ProductName)
	return r.client.Set(ctx, key, value, time.Duration(ttl)*time.Second).Err()
}

// GetOrderInfo 获取订单完整信息
// 存储格式: status:productId:quantity:amount:productName
// 兼容旧格式: 只有纯状态字符串 ("pending"/"success"/"failed")
func (r *SeckillRedis) GetOrderInfo(ctx context.Context, orderId string) (*OrderInfo, error) {
	key := KeyPrefixSeckillOrder + orderId
	val, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}

	// 兼容旧格式：纯状态字符串
	if val == OrderStatusPending || val == OrderStatusSuccess || val == OrderStatusFailed {
		return &OrderInfo{Status: val}, nil
	}

	// 新格式: status:productId:quantity:amount:productName
	parts := strings.SplitN(val, ":", 5)
	if len(parts) < 1 {
		return nil, fmt.Errorf("invalid order info format: %s", val)
	}

	info := &OrderInfo{
		Status: parts[0],
	}
	if len(parts) >= 2 {
		info.ProductId, _ = strconv.ParseInt(parts[1], 10, 64)
	}
	if len(parts) >= 3 {
		info.Quantity, _ = strconv.ParseInt(parts[2], 10, 64)
	}
	if len(parts) >= 4 {
		info.Amount, _ = strconv.ParseInt(parts[3], 10, 64)
	}
	if len(parts) >= 5 {
		info.ProductName = parts[4]
	}

	return info, nil
}

// GetOrInitLocalStock 懒初始化本地库存计数器
// 若已初始化则直接返回，否则从 Redis 读取当前库存并缓存到内存
func (r *SeckillRedis) GetOrInitLocalStock(ctx context.Context, seckillProductId int64) (*atomic.Int64, error) {
	if v, ok := r.localStock.Load(seckillProductId); ok {
		return v.(*atomic.Int64), nil
	}
	stock, err := r.GetStock(ctx, seckillProductId)
	if err != nil {
		return nil, err
	}
	counter := &atomic.Int64{}
	counter.Store(stock)
	actual, _ := r.localStock.LoadOrStore(seckillProductId, counter)
	return actual.(*atomic.Int64), nil
}

// DecrLocalStock 原子扣减本地计数器，返回扣减后的值
// 若计数器未初始化（服务重启等异常情况），保守放行（返回 1），由 Redis Lua 做最终裁决
func (r *SeckillRedis) DecrLocalStock(seckillProductId int64, quantity int64) int64 {
	v, ok := r.localStock.Load(seckillProductId)
	if !ok {
		return 1
	}
	return v.(*atomic.Int64).Add(-quantity)
}

// IncrLocalStock 回滚本地计数器
// 在 Redis Lua 返回 SOLD_OUT 或本地预扣失败时调用
func (r *SeckillRedis) IncrLocalStock(seckillProductId int64, quantity int64) {
	v, ok := r.localStock.Load(seckillProductId)
	if !ok {
		return
	}
	v.(*atomic.Int64).Add(quantity)
}

// Close 关闭连接
func (r *SeckillRedis) Close() error {
	return r.client.Close()
}
