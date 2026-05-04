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
	Mode           string   // "single"(默认) | "cluster" | "sentinel"
	Addr           string   // single/sentinel: "host:port"
	Addrs          []string // cluster/sentinel: 节点列表
	MasterName     string   // sentinel 专用
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
// 功能：原子性完成 时间兜底校验 + 防重 + 库存扣减 + 最小状态落点写入
// 返回值：{结果码, 剩余库存}
// KEYS[1]: stockKey, KEYS[2]: userKey, KEYS[3]: orderKey
// ARGV[1]: quantity
// ARGV[2]: orderId
// ARGV[3]: userKeyTTLSeconds
// ARGV[4]: startTime
// ARGV[5]: endTime
// ARGV[6]: nowUnix
// ARGV[7]: orderStatusTTLSeconds
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
local orderTTL = tonumber(ARGV[7] or "0")

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

-- 5. 写入订单最小状态（status:orderId）
local orderValue = 'pending:' .. tostring(orderId)
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

// quotaAllocateLuaScript 批量领取配额（回收由后台 Reaper 统一执行）
// KEYS[1]: global stock key
// KEYS[2]: current instance bucket key
// KEYS[3]: lease zset key
// ARGV[1]: instanceId
// ARGV[2]: batchSize
// ARGV[3]: leaseTTLSeconds
// ARGV[4]: seckillProductId (unused, kept for arity compatibility)
// ARGV[5]: nowUnix
// return: {allocated, currentBucket}
var quotaAllocateLuaScript = `
local globalKey = KEYS[1]
local bucketKey = KEYS[2]
local leaseKey = KEYS[3]

local instanceId = ARGV[1]
local batchSize = tonumber(ARGV[2])
local leaseTTL = tonumber(ARGV[3])
local now = tonumber(ARGV[5])

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
else
    redis.call('ZREM', leaseKey, instanceId)
end

return {allocated, currentBucket}
`

// quotaConsumeLuaScript 消费实例桶配额做秒杀裁决（最小状态落点）
// KEYS[1]: instance bucket key
// KEYS[2]: user preempt key
// KEYS[3]: order status key
// ARGV[1]: quantity
// ARGV[2]: orderId
// ARGV[3]: userKeyTTL
// ARGV[4]: startTime
// ARGV[5]: endTime
// ARGV[6]: nowUnix
// ARGV[7]: orderStatusTTLSeconds
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
local orderTTL = tonumber(ARGV[7] or "0")

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

local orderValue = 'pending:' .. tostring(orderId)
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
// ARGV[1]: nowUnix
// ARGV[2]: bucketPrefixWithProduct
// return: reclaimed
var quotaReapLuaScript = `
local globalKey = KEYS[1]
local leaseKey = KEYS[2]

local now = tonumber(ARGV[1])
local bucketPrefix = ARGV[2]

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

return reclaimed
`

// SeckillRedis Redis 客户端封装
type SeckillRedis struct {
	client     redis.UniversalClient
	localStock sync.Map // key: seckillProductId(int64) → value: *atomic.Int64，本地库存计数器
}

// NewSeckillRedis 创建 SeckillRedis 实例，支持 single/cluster/sentinel 三种模式
func NewSeckillRedis(conf ClientConfig) (*SeckillRedis, error) {
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

	dialTimeout := time.Duration(conf.DialTimeoutMs) * time.Millisecond
	readTimeout := time.Duration(conf.ReadTimeoutMs) * time.Millisecond
	writeTimeout := time.Duration(conf.WriteTimeoutMs) * time.Millisecond
	poolTimeout := time.Duration(conf.PoolTimeoutMs) * time.Millisecond

	var client redis.UniversalClient
	switch conf.Mode {
	case "cluster":
		addrs := conf.Addrs
		if len(addrs) == 0 && conf.Addr != "" {
			addrs = []string{conf.Addr}
		}
		client = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:        addrs,
			Password:     conf.Password,
			PoolSize:     conf.PoolSize,
			MinIdleConns: conf.MinIdleConns,
			DialTimeout:  dialTimeout,
			ReadTimeout:  readTimeout,
			WriteTimeout: writeTimeout,
			PoolTimeout:  poolTimeout,
		})
	case "sentinel":
		addrs := conf.Addrs
		if len(addrs) == 0 && conf.Addr != "" {
			addrs = []string{conf.Addr}
		}
		client = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:    conf.MasterName,
			SentinelAddrs: addrs,
			Password:      conf.Password,
			DB:            conf.DB,
			PoolSize:      conf.PoolSize,
			MinIdleConns:  conf.MinIdleConns,
			DialTimeout:   dialTimeout,
			ReadTimeout:   readTimeout,
			WriteTimeout:  writeTimeout,
			PoolTimeout:   poolTimeout,
		})
	default: // "single" or ""
		addr := conf.Addr
		if addr == "" {
			addr = "127.0.0.1:6379"
		}
		client = redis.NewClient(&redis.Options{
			Addr:         addr,
			Password:     conf.Password,
			DB:           conf.DB,
			PoolSize:     conf.PoolSize,
			MinIdleConns: conf.MinIdleConns,
			DialTimeout:  dialTimeout,
			ReadTimeout:  readTimeout,
			WriteTimeout: writeTimeout,
			PoolTimeout:  poolTimeout,
		})
	}

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
	OrderStatusTTL   int64
}

// SeckillResult 秒杀结果
type SeckillResult struct {
	Code  int   // 结果码: 1=成功, 0=库存不足, -1=已购买, -2=超限
	Stock int64 // 剩余库存
}

// DoSeckill 执行秒杀（原子性操作）
func (r *SeckillRedis) DoSeckill(ctx context.Context, req *SeckillRequest) (*SeckillResult, error) {
	stockKey := keyStock(req.SeckillProductId)
	userKey := keyUser(req.SeckillProductId, req.UserId)
	orderKey := keyOrder(req.SeckillProductId, req.OrderId)

	keys := []string{stockKey, userKey, orderKey}
	argv := []interface{}{
		req.Quantity,
		req.OrderId,
		req.TTL,
		req.StartTime,
		req.EndTime,
		time.Now().Unix(),
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
	return r.client.Set(ctx, keyStock(seckillProductId), stock, 0).Err()
}

// GetStock 获取秒杀库存
func (r *SeckillRedis) GetStock(ctx context.Context, seckillProductId int64) (int64, error) {
	val, err := r.client.Get(ctx, keyStock(seckillProductId)).Int64()
	if err != nil {
		if err == redis.Nil {
			return 0, nil
		}
		return 0, err
	}
	return val, nil
}

// SetOrderStatus 设置订单状态
func (r *SeckillRedis) SetOrderStatus(ctx context.Context, spid int64, orderId string, status string, ttl int64) error {
	return r.client.Set(ctx, keyOrder(spid, orderId), status, time.Duration(ttl)*time.Second).Err()
}

// GetOrderStatus 获取订单状态
func (r *SeckillRedis) GetOrderStatus(ctx context.Context, orderId string) (string, error) {
	spid := ParseSpidFromOrderId(orderId)
	var key string
	if spid == 0 {
		key = "seckill:order:" + orderId // 旧格式兼容
	} else {
		key = keyOrder(spid, orderId)
	}
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
	return r.client.IncrBy(ctx, keyStock(seckillProductId), quantity).Err()
}

// DeleteUserKey 删除用户购买记录（秒杀失败时调用）
func (r *SeckillRedis) DeleteUserKey(ctx context.Context, seckillProductId, userId int64) error {
	return r.client.Del(ctx, keyUser(seckillProductId, userId)).Err()
}

// SetSeckillProductInfo 设置秒杀商品信息（活动开始前调用）
// productId:seckillPrice:startTime:endTime 存储在 info key 中
// productName 单独存储，避免商品名称中包含冒号导致解析错误
func (r *SeckillRedis) SetSeckillProductInfo(ctx context.Context, seckillProductId, productId, seckillPrice int64, productName string, startTime, endTime int64, ttlSeconds int64) error {
	infoValue := fmt.Sprintf("%d:%d:%d:%d", productId, seckillPrice, startTime, endTime)
	if err := r.client.Set(ctx, keyInfo(seckillProductId), infoValue, time.Duration(ttlSeconds)*time.Second).Err(); err != nil {
		return err
	}
	return r.client.Set(ctx, keyName(seckillProductId), productName, time.Duration(ttlSeconds)*time.Second).Err()
}

// GetSeckillProductInfo 获取秒杀商品信息（返回 productId, seckillPrice, productName, startTime, endTime）
func (r *SeckillRedis) GetSeckillProductInfo(ctx context.Context, seckillProductId int64) (int64, int64, string, int64, int64, error) {
	val, err := r.client.Get(ctx, keyInfo(seckillProductId)).Result()
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

	productName, _ := r.client.Get(ctx, keyName(seckillProductId)).Result()
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

	scanNode := func(scanner interface {
		Scan(ctx context.Context, cursor uint64, match string, count int64) *redis.ScanCmd
	}) error {
		var cursor uint64
		for {
			keys, nextCursor, err := scanner.Scan(ctx, cursor, "{*}:sk:info", scanCount).Result()
			if err != nil {
				return err
			}
			for _, key := range keys {
				spid := parseSpidFromInfoKey(key)
				if spid == 0 {
					continue
				}
				meta, metaErr := r.GetSeckillProductMeta(ctx, spid)
				if metaErr != nil {
					return metaErr
				}
				if meta != nil {
					result[spid] = meta
				}
			}
			cursor = nextCursor
			if cursor == 0 {
				break
			}
		}
		return nil
	}

	// cluster 模式需要逐主节点 SCAN
	if cc, ok := r.client.(*redis.ClusterClient); ok {
		return result, cc.ForEachMaster(ctx, func(ctx context.Context, c *redis.Client) error {
			return scanNode(c)
		})
	}
	return result, scanNode(r.client.(interface {
		Scan(ctx context.Context, cursor uint64, match string, count int64) *redis.ScanCmd
	}))
}

// OrderInfo 订单信息（用于 GetSeckillResult 查询）
// 兼容三种格式：
// 1) 最小状态：status:orderId
// 2) 旧完整格式：status:productId:quantity:amount:productName
// 3) 纯状态：status
type OrderInfo struct {
	Status      string
	OrderId     string
	ProductId   int64
	Quantity    int64
	Amount      int64
	ProductName string
}

// FormatSeckillUserKey 格式化用户秒杀Key（保留兼容，内部改用 keyUser）
func FormatSeckillUserKey(seckillProductId, userId int64) string {
	return keyUser(seckillProductId, userId)
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

// SetOrderInfo 设置订单信息（用于 GetSeckillResult 查询）
// 详情缺失时，按最小状态格式写入：status:orderId
// 详情可用时，按完整格式写入：status:productId:quantity:amount:productName
func (r *SeckillRedis) SetOrderInfo(ctx context.Context, spid int64, orderId string, info *OrderInfo, ttl int64) error {
	key := keyOrder(spid, orderId)

	value := info.Status
	if info != nil {
		if info.ProductId > 0 || info.Quantity > 0 || info.Amount > 0 || info.ProductName != "" {
			value = fmt.Sprintf("%s:%d:%d:%d:%s", info.Status, info.ProductId, info.Quantity, info.Amount, info.ProductName)
		} else {
			id := info.OrderId
			if id == "" {
				id = orderId
			}
			value = fmt.Sprintf("%s:%s", info.Status, id)
		}
	}

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
	orderKey := keyOrder(seckillProductId, orderId)
	stockKey := keyStock(seckillProductId)
	userKey := keyUser(seckillProductId, userId)

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

// GetOrderInfo 获取订单信息
// 支持：
// - 最小状态：status:orderId
// - 完整格式：status:productId:quantity:amount:productName
// - 纯状态：status
func (r *SeckillRedis) GetOrderInfo(ctx context.Context, orderId string) (*OrderInfo, error) {
	spid := ParseSpidFromOrderId(orderId)
	var key string
	if spid == 0 {
		key = "seckill:order:" + orderId // 旧格式兼容
	} else {
		key = keyOrder(spid, orderId)
	}
	val, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}

	// 兼容旧格式：纯状态字符串
	if val == OrderStatusPending || val == OrderStatusSuccess || val == OrderStatusFailed {
		return &OrderInfo{Status: val, OrderId: orderId}, nil
	}

	// status:orderId 或 status:productId:quantity:amount:productName
	parts := strings.SplitN(val, ":", 5)
	if len(parts) < 1 {
		return nil, fmt.Errorf("invalid order info format: %s", val)
	}

	info := &OrderInfo{Status: parts[0], OrderId: orderId}

	if len(parts) == 2 {
		// 最小状态格式：status:orderId
		info.OrderId = parts[1]
		return info, nil
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

// GetOrInitLocalStockWithValue 使用指定初始值初始化本地库存计数器
func (r *SeckillRedis) GetOrInitLocalStockWithValue(seckillProductId int64, initial int64) *atomic.Int64 {
	if v, ok := r.localStock.Load(seckillProductId); ok {
		return v.(*atomic.Int64)
	}
	counter := &atomic.Int64{}
	counter.Store(initial)
	actual, _ := r.localStock.LoadOrStore(seckillProductId, counter)
	return actual.(*atomic.Int64)
}

// GetLocalStock 读取本地库存计数器（未初始化返回 0）
func (r *SeckillRedis) GetLocalStock(seckillProductId int64) int64 {
	v, ok := r.localStock.Load(seckillProductId)
	if !ok {
		return 0
	}
	return v.(*atomic.Int64).Load()
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

// ---- key 构造函数（hash tag 格式，兼容 Redis Cluster）----
// 所有同一商品的 key 使用 {spid} 作为 hash tag，保证落在同一 slot。
// 单节点 Redis 忽略 hash tag，行为与原来完全一致。

func keyStock(spid int64) string     { return fmt.Sprintf("{%d}:sk:stock", spid) }
func keyUser(spid, uid int64) string { return fmt.Sprintf("{%d}:sk:user:%d", spid, uid) }
func keyOrder(spid int64, orderId string) string {
	return fmt.Sprintf("{%d}:sk:order:%s", spid, orderId)
}
func keyInfo(spid int64) string { return fmt.Sprintf("{%d}:sk:info", spid) }
func keyName(spid int64) string { return fmt.Sprintf("{%d}:sk:name", spid) }
func keyQBucket(spid int64, instanceID string) string {
	return fmt.Sprintf("{%d}:sk:qbucket:%s", spid, instanceID)
}
func keyQBucketPrefix(spid int64) string { return fmt.Sprintf("{%d}:sk:qbucket:", spid) }
func keyQLease(spid int64) string        { return fmt.Sprintf("{%d}:sk:qlease", spid) }
func keyQReaperLock(spid int64) string   { return fmt.Sprintf("{%d}:sk:qlock", spid) }

// KeyUser 导出版本，供 logic 包使用
func KeyUser(spid, uid int64) string { return keyUser(spid, uid) }

// FormatOrderId 将 spid 编码进 orderId，格式：S{spid}_{rawId}
// 用于 Redis Cluster 模式下从 orderId 反推 spid 以构造正确的 key。
func FormatOrderId(spid int64, rawId string) string {
	raw := strings.TrimPrefix(rawId, "S")
	return fmt.Sprintf("S%d_%s", spid, raw)
}

// ParseSpidFromOrderId 从编码后的 orderId 解析 spid。
// 返回 0 表示旧格式（不含 spid 编码），调用方应降级处理。
func ParseSpidFromOrderId(orderId string) int64 {
	s := strings.TrimPrefix(orderId, "S")
	idx := strings.Index(s, "_")
	if idx < 0 {
		return 0
	}
	spid, err := strconv.ParseInt(s[:idx], 10, 64)
	if err != nil {
		return 0
	}
	return spid
}

// parseSpidFromInfoKey 从 {spid}:sk:info 格式的 key 中提取 spid
func parseSpidFromInfoKey(key string) int64 {
	if !strings.HasPrefix(key, "{") {
		return 0
	}
	end := strings.Index(key, "}")
	if end < 0 {
		return 0
	}
	spid, err := strconv.ParseInt(key[1:end], 10, 64)
	if err != nil {
		return 0
	}
	return spid
}

// EnsureQuota 批量领取本地配额（并回收过期租约）
func (r *SeckillRedis) EnsureQuota(ctx context.Context, seckillProductId int64, instanceID string, batchSize int64, leaseTTLSeconds int64) (int64, int64, error) {
	keys := []string{keyStock(seckillProductId), keyQBucket(seckillProductId, instanceID), keyQLease(seckillProductId)}
	argv := []interface{}{
		instanceID,
		batchSize,
		leaseTTLSeconds,
		strconv.FormatInt(seckillProductId, 10),
		time.Now().Unix(),
	}

	raw, err := r.client.Eval(ctx, quotaAllocateLuaScript, keys, argv...).Result()
	if err != nil {
		return 0, 0, err
	}

	arr, ok := raw.([]interface{})
	if !ok || len(arr) != 2 {
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
	bucketKey := keyQBucket(req.SeckillProductId, instanceID)
	userKey := keyUser(req.SeckillProductId, req.UserId)
	orderKey := keyOrder(req.SeckillProductId, req.OrderId)

	keys := []string{bucketKey, userKey, orderKey}
	argv := []interface{}{
		req.Quantity,
		req.OrderId,
		req.TTL,
		req.StartTime,
		req.EndTime,
		time.Now().Unix(),
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
	bucketLeft, err := r.client.Get(ctx, keyQBucket(seckillProductId, instanceID)).Int64()
	if err != nil && err != redis.Nil {
		return err
	}
	leaseKey := keyQLease(seckillProductId)
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
	lockOK, err := r.client.SetNX(ctx, keyQReaperLock(seckillProductId), "1", 1200*time.Millisecond).Result()
	if err != nil || !lockOK {
		return 0, err
	}
	defer r.client.Del(ctx, keyQReaperLock(seckillProductId))

	keys := []string{keyStock(seckillProductId), keyQLease(seckillProductId)}
	argv := []interface{}{
		time.Now().Unix(),
		keyQBucketPrefix(seckillProductId),
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

// ReapExpiredQuotaForAllProducts 遍历本地已追踪商品，回收所有过期租约配额
// 不再依赖 Redis 全局 products set，改为遍历内存中的 localStock
func (r *SeckillRedis) ReapExpiredQuotaForAllProducts(ctx context.Context) (int64, error) {
	var reclaimedTotal int64
	var firstErr error
	r.localStock.Range(func(key, _ any) bool {
		spid, ok := key.(int64)
		if !ok {
			return true
		}
		reclaimed, reclaimErr := r.ReapExpiredQuotaForProduct(ctx, spid)
		if reclaimErr != nil && firstErr == nil {
			firstErr = reclaimErr
		}
		reclaimedTotal += reclaimed
		return true
	})
	return reclaimedTotal, firstErr
}

// Close 关闭连接
func (r *SeckillRedis) Close() error {
	return r.client.Close()
}
