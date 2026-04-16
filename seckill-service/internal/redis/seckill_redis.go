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
	KeyPrefixSeckillStock       = "seckill:stock:"             // 秒杀库存
	KeyPrefixSeckillUser        = "seckill:user:"              // 用户预占记录（短期TTL，自动释放悬空库存）
	KeyPrefixSeckillOrder       = "seckill:order:"             // 秒杀订单状态
	KeyPrefixSeckillInfo        = "seckill:info:"              // 秒杀商品信息 (productId:seckillPrice:startTime:endTime)
	KeyPrefixSeckillProductName = "seckill:product_name:"      // 秒杀商品名称
	KeyPrefixQuotaBucket        = "seckill:quota:bucket:"      // 实例配额桶前缀: seckill:quota:bucket:{spid}:{instanceId}
	KeyPrefixQuotaLeaseZSet     = "seckill:quota:lease:zset:"  // 配额租约前缀: seckill:quota:lease:zset:{spid}
	KeyPrefixQuotaReaperLock    = "seckill:quota:reaper:lock:" // 回收锁前缀: seckill:quota:reaper:lock:{spid}
	KeyQuotaProductsSet         = "seckill:quota:products"     // 有活动租约的商品集合

	// 订单状态常量（与 logic 包保持一致）
	OrderStatusPending  = "pending"
	OrderStatusSuccess  = "success"
	OrderStatusFailed   = "failed"
	OrderStatusNotStart = "not_started"
	OrderStatusEnded    = "ended"

	// failed 补偿执行结果码
	CompensateResultCompensated    = 0  // pending -> failed 并完成库存回补
	CompensateResultAlreadyFailed  = 1  // 已是 failed，幂等返回
	CompensateResultAlreadySuccess = 2  // 已是 success，不执行回补
	CompensateResultInvalidStatus  = 3  // 非法中间状态
	CompensateResultOrderNotFound  = -1 // 订单不存在或已过期
)

const (
	defaultPoolSize      = 256
	defaultMinIdleConns  = 32
	defaultDialTimeoutMs = 200
	defaultRWTimeoutMs   = 200
	defaultPoolTimeoutMs = 200
	defaultScanCount     = 500
)

type ClientConfig struct {
	Host           string
	Password       string
	DB             int
	PoolSize       int
	MinIdleConns   int
	DialTimeoutMs  int
	ReadTimeoutMs  int
	WriteTimeoutMs int
	PoolTimeoutMs  int
}

type SeckillProductMeta struct {
	SeckillProductId int64
	ProductId        int64
	SeckillPrice     int64
	ProductName      string
	StartTime        int64
	EndTime          int64
}

// seckillLuaScript 秒杀 Lua 脚本
// 功能：原子性完成 时间校验 + 防重 + 库存扣减 + 预锁定 + 订单 pending 状态写入
// 返回值：{结果码, 剩余库存}
// KEYS[1]: stockKey, KEYS[2]: userKey, KEYS[3]: orderKey
// ARGV[1]: quantity
// ARGV[2]: orderId
// ARGV[3]: userKeyTTLSeconds
// ARGV[4]: startTime
// ARGV[5]: endTime
// ARGV[6]: nowUnix
// ARGV[7]: productId
// ARGV[8]: amount
// ARGV[9]: productName
// ARGV[10]: orderStatusTTLSeconds
var seckillLuaScript = `
local stockKey = KEYS[1]
local userKey = KEYS[2]
local orderKey = KEYS[3]
local quantity = tonumber(ARGV[1])
local orderId = ARGV[2]
local ttl = tonumber(ARGV[3])
local startTime = tonumber(ARGV[4])
local endTime = tonumber(ARGV[5])
local now = tonumber(ARGV[6])
local productId = tonumber(ARGV[7] or "0")
local amount = tonumber(ARGV[8] or "0")
local productName = ARGV[9] or ""
local orderTTL = tonumber(ARGV[10] or "0")

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

-- 5. 写入订单 pending 状态（保持现有格式：status:productId:quantity:amount:productName）
local orderValue = 'pending:' .. tostring(productId) .. ':' .. tostring(quantity) .. ':' .. tostring(amount) .. ':' .. productName
if orderTTL > 0 then
    redis.call('SETEX', orderKey, orderTTL, orderValue)
else
    redis.call('SET', orderKey, orderValue)
end

return {1, newStock}
`

// compensateFailedOrderLuaScript 原子执行超时失败补偿
// KEYS[1]: order status key (seckill:order:{orderId})
// KEYS[2]: stock key (seckill:stock:{seckillProductId})
// KEYS[3]: user key (seckill:user:{seckillProductId}:{userId})
// ARGV[1]: quantity
// ARGV[2]: orderStatusTTLSeconds
// return: {code, stock}
var compensateFailedOrderLuaScript = `
local orderKey = KEYS[1]
local stockKey = KEYS[2]
local userKey = KEYS[3]
local quantity = tonumber(ARGV[1])
local ttl = tonumber(ARGV[2])

local orderVal = redis.call('GET', orderKey)
if not orderVal then
    local currentStock = tonumber(redis.call('GET', stockKey) or 0)
    return {-1, currentStock}
end

local currentStatus = orderVal
local idx = string.find(orderVal, ':')
if idx then
    currentStatus = string.sub(orderVal, 1, idx - 1)
end

if currentStatus == 'failed' then
    local currentStock = tonumber(redis.call('GET', stockKey) or 0)
    return {1, currentStock}
end

if currentStatus == 'success' then
    local currentStock = tonumber(redis.call('GET', stockKey) or 0)
    return {2, currentStock}
end

if currentStatus ~= 'pending' then
    local currentStock = tonumber(redis.call('GET', stockKey) or 0)
    return {3, currentStock}
end

local failedVal = 'failed'
if idx then
    failedVal = 'failed' .. string.sub(orderVal, idx)
end

if ttl > 0 then
    redis.call('SETEX', orderKey, ttl, failedVal)
else
    redis.call('SET', orderKey, failedVal)
end

local newStock = redis.call('INCRBY', stockKey, quantity)
redis.call('DEL', userKey)
return {0, tonumber(newStock)}
`

// quotaAllocateLuaScript 批量领取配额并顺带回收过期租约
// KEYS[1]: global stock key
// KEYS[2]: current instance bucket key
// KEYS[3]: lease zset key
// KEYS[4]: products set key
// ARGV[1]: instanceId
// ARGV[2]: batchSize
// ARGV[3]: leaseTTLSeconds
// ARGV[4]: nowUnix
// ARGV[5]: bucketPrefixWithProduct (seckill:quota:bucket:{spid}:)
// ARGV[6]: seckillProductId
// return: {allocated, currentBucket, reclaimed}
var quotaAllocateLuaScript = `
local globalKey = KEYS[1]
local bucketKey = KEYS[2]
local leaseKey = KEYS[3]
local productsKey = KEYS[4]

local instanceId = ARGV[1]
local batchSize = tonumber(ARGV[2])
local leaseTTL = tonumber(ARGV[3])
local now = tonumber(ARGV[4])
local bucketPrefix = ARGV[5]
local productId = ARGV[6]

local reclaimed = 0
local expired = redis.call('ZRANGEBYSCORE', leaseKey, '-inf', now)
for _, inst in ipairs(expired) do
    local expiredBucketKey = bucketPrefix .. inst
    local left = tonumber(redis.call('GET', expiredBucketKey) or 0)
    if left > 0 then
        redis.call('INCRBY', globalKey, left)
        reclaimed = reclaimed + left
    end
    redis.call('DEL', expiredBucketKey)
    redis.call('ZREM', leaseKey, inst)
end

local allocated = 0
if batchSize > 0 then
    local globalStock = tonumber(redis.call('GET', globalKey) or 0)
    if globalStock > 0 then
        allocated = math.min(batchSize, globalStock)
        redis.call('DECRBY', globalKey, allocated)
        redis.call('INCRBY', bucketKey, allocated)
    end
end

local currentBucket = tonumber(redis.call('GET', bucketKey) or 0)
if currentBucket > 0 then
    redis.call('ZADD', leaseKey, now + leaseTTL, instanceId)
    redis.call('SADD', productsKey, productId)
else
    redis.call('ZREM', leaseKey, instanceId)
end

if redis.call('ZCARD', leaseKey) == 0 then
    redis.call('SREM', productsKey, productId)
end

return {allocated, currentBucket, reclaimed}
`

// quotaConsumeLuaScript 消费实例桶配额做秒杀裁决
// KEYS[1]: instance bucket key
// KEYS[2]: user preempt key
// KEYS[3]: order status key
// ARGV[1]: quantity
// ARGV[2]: orderId
// ARGV[3]: userKeyTTL
// ARGV[4]: startTime
// ARGV[5]: endTime
// ARGV[6]: nowUnix
// ARGV[7]: productId
// ARGV[8]: amount
// ARGV[9]: productName
// ARGV[10]: orderStatusTTLSeconds
// return: {code, bucketRemaining}
var quotaConsumeLuaScript = `
local bucketKey = KEYS[1]
local userKey = KEYS[2]
local orderKey = KEYS[3]
local quantity = tonumber(ARGV[1])
local orderId = ARGV[2]
local ttl = tonumber(ARGV[3])
local startTime = tonumber(ARGV[4])
local endTime = tonumber(ARGV[5])
local now = tonumber(ARGV[6])
local productId = tonumber(ARGV[7] or "0")
local amount = tonumber(ARGV[8] or "0")
local productName = ARGV[9] or ""
local orderTTL = tonumber(ARGV[10] or "0")

if startTime > 0 and now < startTime then
    local currentBucket = tonumber(redis.call('GET', bucketKey) or 0)
    return {-3, currentBucket}
end
if endTime > 0 and now > endTime then
    local currentBucket = tonumber(redis.call('GET', bucketKey) or 0)
    return {-4, currentBucket}
end

local alreadyBought = redis.call('EXISTS', userKey)
if alreadyBought == 1 then
    local currentBucket = tonumber(redis.call('GET', bucketKey) or 0)
    return {-1, currentBucket}
end

local currentBucket = tonumber(redis.call('GET', bucketKey) or 0)
if currentBucket < quantity then
    return {0, currentBucket}
end

local newBucket = redis.call('DECRBY', bucketKey, quantity)
if newBucket < 0 then
    redis.call('INCRBY', bucketKey, quantity)
    return {0, currentBucket}
end

redis.call('SETEX', userKey, ttl, orderId)

local orderValue = 'pending:' .. tostring(productId) .. ':' .. tostring(quantity) .. ':' .. tostring(amount) .. ':' .. productName
if orderTTL > 0 then
    redis.call('SETEX', orderKey, orderTTL, orderValue)
else
    redis.call('SET', orderKey, orderValue)
end

return {1, newBucket}
`

// quotaReapLuaScript 回收某商品的过期租约配额
// KEYS[1]: global stock key
// KEYS[2]: lease zset key
// KEYS[3]: products set key
// ARGV[1]: nowUnix
// ARGV[2]: bucketPrefixWithProduct
// ARGV[3]: seckillProductId
// return: reclaimed
var quotaReapLuaScript = `
local globalKey = KEYS[1]
local leaseKey = KEYS[2]
local productsKey = KEYS[3]

local now = tonumber(ARGV[1])
local bucketPrefix = ARGV[2]
local productId = ARGV[3]

local reclaimed = 0
local expired = redis.call('ZRANGEBYSCORE', leaseKey, '-inf', now)
for _, inst in ipairs(expired) do
    local expiredBucketKey = bucketPrefix .. inst
    local left = tonumber(redis.call('GET', expiredBucketKey) or 0)
    if left > 0 then
        redis.call('INCRBY', globalKey, left)
        reclaimed = reclaimed + left
    end
    redis.call('DEL', expiredBucketKey)
    redis.call('ZREM', leaseKey, inst)
end

if redis.call('ZCARD', leaseKey) == 0 then
    redis.call('SREM', productsKey, productId)
end

return reclaimed
`

// SeckillRedis Redis 客户端封装
type SeckillRedis struct {
	client     *redis.Client
	localStock sync.Map // key: seckillProductId(int64) → value: *atomic.Int64，本地库存计数器
}

// NewSeckillRedis 创建 SeckillRedis 实例
func NewSeckillRedis(conf ClientConfig) (*SeckillRedis, error) {
	if conf.Host == "" {
		conf.Host = "127.0.0.1:6379"
	}
	if conf.PoolSize <= 0 {
		conf.PoolSize = defaultPoolSize
	}
	if conf.MinIdleConns <= 0 {
		conf.MinIdleConns = defaultMinIdleConns
	}
	if conf.DialTimeoutMs <= 0 {
		conf.DialTimeoutMs = defaultDialTimeoutMs
	}
	if conf.ReadTimeoutMs <= 0 {
		conf.ReadTimeoutMs = defaultRWTimeoutMs
	}
	if conf.WriteTimeoutMs <= 0 {
		conf.WriteTimeoutMs = defaultRWTimeoutMs
	}
	if conf.PoolTimeoutMs <= 0 {
		conf.PoolTimeoutMs = defaultPoolTimeoutMs
	}

	client := redis.NewClient(&redis.Options{
		Addr:         conf.Host,
		Password:     conf.Password,
		DB:           conf.DB,
		PoolSize:     conf.PoolSize,
		MinIdleConns: conf.MinIdleConns,
		DialTimeout:  time.Duration(conf.DialTimeoutMs) * time.Millisecond,
		ReadTimeout:  time.Duration(conf.ReadTimeoutMs) * time.Millisecond,
		WriteTimeout: time.Duration(conf.WriteTimeoutMs) * time.Millisecond,
		PoolTimeout:  time.Duration(conf.PoolTimeoutMs) * time.Millisecond,
	})
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("ping redis failed: %w", err)
	}

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
	ProductId        int64
	Amount           int64
	ProductName      string
	OrderStatusTTL   int64
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
	orderKey := KeyPrefixSeckillOrder + req.OrderId

	keys := []string{stockKey, userKey, orderKey}
	argv := []interface{}{
		req.Quantity,
		req.OrderId,
		req.TTL,
		req.StartTime,
		req.EndTime,
		time.Now().Unix(),
		req.ProductId,
		req.Amount,
		req.ProductName,
		req.OrderStatusTTL,
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

func (r *SeckillRedis) GetSeckillProductMeta(ctx context.Context, seckillProductId int64) (*SeckillProductMeta, error) {
	productId, seckillPrice, productName, startTime, endTime, err := r.GetSeckillProductInfo(ctx, seckillProductId)
	if err != nil {
		return nil, err
	}
	if productId == 0 && seckillPrice == 0 && startTime == 0 && endTime == 0 {
		return nil, nil
	}
	return &SeckillProductMeta{
		SeckillProductId: seckillProductId,
		ProductId:        productId,
		SeckillPrice:     seckillPrice,
		ProductName:      productName,
		StartTime:        startTime,
		EndTime:          endTime,
	}, nil
}

func (r *SeckillRedis) LoadAllSeckillProductMeta(ctx context.Context, scanCount int64) (map[int64]*SeckillProductMeta, error) {
	if scanCount <= 0 {
		scanCount = defaultScanCount
	}
	result := make(map[int64]*SeckillProductMeta)
	var cursor uint64

	for {
		keys, nextCursor, err := r.client.Scan(ctx, cursor, KeyPrefixSeckillInfo+"*", scanCount).Result()
		if err != nil {
			return nil, err
		}

		for _, key := range keys {
			spidText := strings.TrimPrefix(key, KeyPrefixSeckillInfo)
			seckillProductId, parseErr := strconv.ParseInt(spidText, 10, 64)
			if parseErr != nil {
				continue
			}
			meta, metaErr := r.GetSeckillProductMeta(ctx, seckillProductId)
			if metaErr != nil {
				return nil, metaErr
			}
			if meta != nil {
				result[seckillProductId] = meta
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return result, nil
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

// CompensateFailedOrder 原子执行超时失败补偿：
// 1. 仅当订单当前为 pending 时将其更新为 failed
// 2. 回补 Redis 秒杀库存
// 3. 删除用户占位 key（允许用户重试）
func (r *SeckillRedis) CompensateFailedOrder(
	ctx context.Context,
	orderId string,
	seckillProductId int64,
	userId int64,
	quantity int64,
	orderStatusTTL int64,
) (int, int64, error) {
	orderKey := KeyPrefixSeckillOrder + orderId
	stockKey := KeyPrefixSeckillStock + strconv.FormatInt(seckillProductId, 10)
	userKey := KeyPrefixSeckillUser + strconv.FormatInt(seckillProductId, 10) + ":" + strconv.FormatInt(userId, 10)

	raw, err := r.client.Eval(
		ctx,
		compensateFailedOrderLuaScript,
		[]string{orderKey, stockKey, userKey},
		quantity,
		orderStatusTTL,
	).Result()
	if err != nil {
		return 0, 0, fmt.Errorf("execute compensate failed lua failed: %w", err)
	}

	arr, ok := raw.([]interface{})
	if !ok || len(arr) != 2 {
		return 0, 0, fmt.Errorf("invalid compensate result: %v", raw)
	}

	code, ok1 := arr[0].(int64)
	stock, ok2 := arr[1].(int64)
	if !ok1 || !ok2 {
		return 0, 0, fmt.Errorf("invalid compensate result type: %v", raw)
	}

	return int(code), stock, nil
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

func quotaBucketKeyPrefix(seckillProductId int64) string {
	return KeyPrefixQuotaBucket + strconv.FormatInt(seckillProductId, 10) + ":"
}

func quotaBucketKey(seckillProductId int64, instanceID string) string {
	return quotaBucketKeyPrefix(seckillProductId) + instanceID
}

func quotaLeaseZSetKey(seckillProductId int64) string {
	return KeyPrefixQuotaLeaseZSet + strconv.FormatInt(seckillProductId, 10)
}

// EnsureQuota 批量领取本地配额（并回收过期租约）
func (r *SeckillRedis) EnsureQuota(ctx context.Context, seckillProductId int64, instanceID string, batchSize int64, leaseTTLSeconds int64) (int64, int64, error) {
	globalStockKey := KeyPrefixSeckillStock + strconv.FormatInt(seckillProductId, 10)
	bucketKey := quotaBucketKey(seckillProductId, instanceID)
	leaseKey := quotaLeaseZSetKey(seckillProductId)
	bucketPrefix := quotaBucketKeyPrefix(seckillProductId)

	keys := []string{globalStockKey, bucketKey, leaseKey, KeyQuotaProductsSet}
	argv := []interface{}{
		instanceID,
		batchSize,
		leaseTTLSeconds,
		time.Now().Unix(),
		bucketPrefix,
		strconv.FormatInt(seckillProductId, 10),
	}

	raw, err := r.client.Eval(ctx, quotaAllocateLuaScript, keys, argv...).Result()
	if err != nil {
		return 0, 0, err
	}

	arr, ok := raw.([]interface{})
	if !ok || len(arr) != 3 {
		return 0, 0, fmt.Errorf("invalid allocate result: %v", raw)
	}
	allocated, ok1 := arr[0].(int64)
	currentBucket, ok2 := arr[1].(int64)
	if !ok1 || !ok2 {
		return 0, 0, fmt.Errorf("invalid allocate result type: %v", raw)
	}
	return allocated, currentBucket, nil
}

// DoSeckillWithQuota 使用实例配额桶执行秒杀原子裁决
func (r *SeckillRedis) DoSeckillWithQuota(ctx context.Context, req *SeckillRequest, instanceID string) (*SeckillResult, error) {
	bucketKey := quotaBucketKey(req.SeckillProductId, instanceID)
	userKey := KeyPrefixSeckillUser + strconv.FormatInt(req.SeckillProductId, 10) + ":" + strconv.FormatInt(req.UserId, 10)
	orderKey := KeyPrefixSeckillOrder + req.OrderId

	keys := []string{bucketKey, userKey, orderKey}
	argv := []interface{}{
		req.Quantity,
		req.OrderId,
		req.TTL,
		req.StartTime,
		req.EndTime,
		time.Now().Unix(),
		req.ProductId,
		req.Amount,
		req.ProductName,
		req.OrderStatusTTL,
	}

	raw, err := r.client.Eval(ctx, quotaConsumeLuaScript, keys, argv...).Result()
	if err != nil {
		return nil, fmt.Errorf("execute quota consume lua failed: %w", err)
	}

	arr, ok := raw.([]interface{})
	if !ok || len(arr) != 2 {
		return nil, fmt.Errorf("invalid consume result: %v", raw)
	}
	code, ok1 := arr[0].(int64)
	stock, ok2 := arr[1].(int64)
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("invalid consume result type: %v", raw)
	}

	return &SeckillResult{Code: int(code), Stock: stock}, nil
}

// RenewLease 为当前实例续租（仅当桶内仍有配额）
func (r *SeckillRedis) RenewLease(ctx context.Context, seckillProductId int64, instanceID string, leaseTTLSeconds int64) error {
	bucketKey := quotaBucketKey(seckillProductId, instanceID)
	bucketLeft, err := r.client.Get(ctx, bucketKey).Int64()
	if err != nil && err != redis.Nil {
		return err
	}
	leaseKey := quotaLeaseZSetKey(seckillProductId)
	if bucketLeft <= 0 {
		return r.client.ZRem(ctx, leaseKey, instanceID).Err()
	}
	expireAt := time.Now().Unix() + leaseTTLSeconds
	return r.client.ZAdd(ctx, leaseKey, redis.Z{Score: float64(expireAt), Member: instanceID}).Err()
}

// RenewAllActiveLeases 遍历本地已追踪商品，为当前实例续租
func (r *SeckillRedis) RenewAllActiveLeases(ctx context.Context, instanceID string, leaseTTLSeconds int64) error {
	var firstErr error
	r.localStock.Range(func(key, value any) bool {
		spid, ok := key.(int64)
		if !ok {
			return true
		}
		counter, ok := value.(*atomic.Int64)
		if !ok {
			return true
		}
		if counter.Load() <= 0 {
			return true
		}
		if err := r.RenewLease(ctx, spid, instanceID, leaseTTLSeconds); err != nil && firstErr == nil {
			firstErr = err
		}
		return true
	})
	return firstErr
}

// ReapExpiredQuotaForProduct 回收单个商品的过期租约配额
func (r *SeckillRedis) ReapExpiredQuotaForProduct(ctx context.Context, seckillProductId int64) (int64, error) {
	lockKey := KeyPrefixQuotaReaperLock + strconv.FormatInt(seckillProductId, 10)
	lockOK, err := r.client.SetNX(ctx, lockKey, "1", 1200*time.Millisecond).Result()
	if err != nil || !lockOK {
		return 0, err
	}
	defer r.client.Del(ctx, lockKey)

	globalStockKey := KeyPrefixSeckillStock + strconv.FormatInt(seckillProductId, 10)
	leaseKey := quotaLeaseZSetKey(seckillProductId)
	bucketPrefix := quotaBucketKeyPrefix(seckillProductId)
	keys := []string{globalStockKey, leaseKey, KeyQuotaProductsSet}
	argv := []interface{}{
		time.Now().Unix(),
		bucketPrefix,
		strconv.FormatInt(seckillProductId, 10),
	}

	raw, err := r.client.Eval(ctx, quotaReapLuaScript, keys, argv...).Result()
	if err != nil {
		return 0, err
	}
	reclaimed, ok := raw.(int64)
	if !ok {
		return 0, fmt.Errorf("invalid reclaim result: %v", raw)
	}
	return reclaimed, nil
}

// ReapExpiredQuotaForAllProducts 扫描并回收所有有租约商品
func (r *SeckillRedis) ReapExpiredQuotaForAllProducts(ctx context.Context) (int64, error) {
	products, err := r.client.SMembers(ctx, KeyQuotaProductsSet).Result()
	if err != nil && err != redis.Nil {
		return 0, err
	}
	var reclaimedTotal int64
	var firstErr error
	for _, item := range products {
		spid, parseErr := strconv.ParseInt(item, 10, 64)
		if parseErr != nil {
			continue
		}
		reclaimed, reclaimErr := r.ReapExpiredQuotaForProduct(ctx, spid)
		if reclaimErr != nil && firstErr == nil {
			firstErr = reclaimErr
		}
		reclaimedTotal += reclaimed
	}
	return reclaimedTotal, firstErr
}

// Close 关闭连接
func (r *SeckillRedis) Close() error {
	return r.client.Close()
}
