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
	defaultShardCount         = 16
	defaultProbeShardCount    = 4
	defaultTailDrainThreshold = 32
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
	Code    int   // 结果码: 1=成功, 0=库存不足, -1=已购买, -2=超限
	Stock   int64 // 剩余库存
	ShardNo int32
}

// DoSeckill 执行秒杀（原子性操作）
func (r *SeckillRedis) DoSeckill(ctx context.Context, req *SeckillRequest) (*SeckillResult, error) {
	userKey := keyUser(req.SeckillProductId, req.UserId)
	orderKey := keyOrder(req.SeckillProductId, req.OrderId)
	totalKey := keyStock(req.SeckillProductId)

	nowUnix := time.Now().Unix()
	if req.StartTime > 0 && nowUnix < req.StartTime {
		stock, _ := r.GetStock(ctx, req.SeckillProductId)
		return &SeckillResult{Code: LuaResultNotStarted, Stock: stock}, nil
	}
	if req.EndTime > 0 && nowUnix > req.EndTime {
		stock, _ := r.GetStock(ctx, req.SeckillProductId)
		return &SeckillResult{Code: LuaResultEnded, Stock: stock}, nil
	}

	exists, err := r.client.Exists(ctx, userKey).Result()
	if err != nil {
		return nil, fmt.Errorf("check user key failed: %w", err)
	}
	if exists > 0 {
		stock, _ := r.GetStock(ctx, req.SeckillProductId)
		return &SeckillResult{Code: LuaResultAlreadyBought, Stock: stock}, nil
	}

	totalStock, err := r.GetStock(ctx, req.SeckillProductId)
	if err != nil {
		return nil, err
	}
	if totalStock < req.Quantity {
		return &SeckillResult{Code: LuaResultStockNotEnough, Stock: totalStock}, nil
	}

	primary := shardNoForUser(req.UserId)
	probeOrder := shardProbeOrder(primary)
	probeLimit := defaultProbeShardCount
	if totalStock <= defaultTailDrainThreshold {
		probeLimit = defaultShardCount
	}
	if probeLimit > len(probeOrder) {
		probeLimit = len(probeOrder)
	}

	for _, shardNo := range probeOrder[:probeLimit] {
		pipe := r.client.TxPipeline()
		shardKey := keyShardStock(req.SeckillProductId, shardNo)
		decrShard := pipe.DecrBy(ctx, shardKey, req.Quantity)
		decrTotal := pipe.DecrBy(ctx, totalKey, req.Quantity)
		setUser := pipe.SetNX(ctx, userKey, req.OrderId, time.Duration(req.TTL)*time.Second)
		orderValue := fmt.Sprintf("%s:%s:%d", OrderStatusPending, req.OrderId, shardNo)
		setOrder := pipe.Set(ctx, orderKey, orderValue, time.Duration(req.OrderStatusTTL)*time.Second)
		_, execErr := pipe.Exec(ctx)
		if execErr != nil {
			_ = setUser.Err()
			_ = setOrder.Err()
		}

		shardLeft, shardErr := decrShard.Result()
		totalLeft, totalErr := decrTotal.Result()
		userSet, userErr := setUser.Result()
		orderErr := setOrder.Err()

		if execErr == nil && shardErr == nil && totalErr == nil && userErr == nil && orderErr == nil && userSet && shardLeft >= 0 && totalLeft >= 0 {
			return &SeckillResult{
				Code:    LuaResultSuccess,
				Stock:   totalLeft,
				ShardNo: shardNo,
			}, nil
		}

		pipe = r.client.TxPipeline()
		if userSet {
			pipe.Del(ctx, userKey)
		}
		if orderErr == nil {
			pipe.Del(ctx, orderKey)
		}
		if shardErr == nil {
			pipe.IncrBy(ctx, shardKey, req.Quantity)
		}
		if totalErr == nil {
			pipe.IncrBy(ctx, totalKey, req.Quantity)
		}
		_, _ = pipe.Exec(ctx)

		if userErr == nil && !userSet {
			stock, _ := r.GetStock(ctx, req.SeckillProductId)
			return &SeckillResult{Code: LuaResultAlreadyBought, Stock: stock}, nil
		}
	}

	stock, err := r.GetStock(ctx, req.SeckillProductId)
	if err != nil {
		return nil, err
	}
	return &SeckillResult{Code: LuaResultStockNotEnough, Stock: stock}, nil
}

// InitStock 初始化秒杀库存（活动开始前调用）
func (r *SeckillRedis) InitStock(ctx context.Context, seckillProductId int64, stock int64) error {
	pipe := r.client.TxPipeline()
	pipe.Set(ctx, keyStock(seckillProductId), stock, 0)
	pipe.Set(ctx, keyShardMeta(seckillProductId), defaultShardCount, 0)
	for shardNo, shardStock := range splitStockIntoShards(stock) {
		pipe.Set(ctx, keyShardStock(seckillProductId, int32(shardNo)), shardStock, 0)
	}
	_, err := pipe.Exec(ctx)
	return err
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
	current, err := r.GetOrderInfo(ctx, orderId)
	if err != nil {
		return err
	}
	shardNo := int32(0)
	if current != nil {
		shardNo = current.ShardNo
	}
	value := fmt.Sprintf("%s:%s:%d", status, orderId, shardNo)
	return r.client.Set(ctx, keyOrder(spid, orderId), value, time.Duration(ttl)*time.Second).Err()
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

func (r *SeckillRedis) RollbackShardStock(ctx context.Context, seckillProductId int64, shardNo int32, quantity int64) error {
	pipe := r.client.TxPipeline()
	pipe.IncrBy(ctx, keyStock(seckillProductId), quantity)
	pipe.IncrBy(ctx, keyShardStock(seckillProductId, shardNo), quantity)
	_, err := pipe.Exec(ctx)
	return err
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
	ShardNo     int32
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
			value = fmt.Sprintf("%s:%s:%d:%d:%d:%d:%s", info.Status, orderId, info.ShardNo, info.ProductId, info.Quantity, info.Amount, info.ProductName)
		} else {
			id := info.OrderId
			if id == "" {
				id = orderId
			}
			value = fmt.Sprintf("%s:%s:%d", info.Status, id, info.ShardNo)
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
	shardNo int32,
	orderStatusTTL int64,
) (int, int64, error) {
	orderKey := keyOrder(seckillProductId, orderId)
	userKey := keyUser(seckillProductId, userId)
	totalKey := keyStock(seckillProductId)
	shardKey := keyShardStock(seckillProductId, shardNo)

	info, err := r.GetOrderInfo(ctx, orderId)
	if err != nil {
		return 0, 0, err
	}
	if info == nil {
		stock, _ := r.GetStock(ctx, seckillProductId)
		return CompensateResultOrderNotFound, stock, nil
	}
	if info.ShardNo != shardNo {
		return CompensateResultInvalidStatus, 0, fmt.Errorf("shard mismatch for compensation: order=%s redis=%d req=%d", orderId, info.ShardNo, shardNo)
	}
	if info.Status == OrderStatusFailed {
		stock, _ := r.GetStock(ctx, seckillProductId)
		return CompensateResultAlreadyFailed, stock, nil
	}
	if info.Status == OrderStatusSuccess {
		stock, _ := r.GetStock(ctx, seckillProductId)
		return CompensateResultAlreadySuccess, stock, nil
	}
	if info.Status != OrderStatusPending {
		stock, _ := r.GetStock(ctx, seckillProductId)
		return CompensateResultInvalidStatus, stock, nil
	}

	pipe := r.client.TxPipeline()
	pipe.Set(ctx, orderKey, fmt.Sprintf("%s:%s:%d", OrderStatusFailed, orderId, shardNo), time.Duration(orderStatusTTL)*time.Second)
	pipe.IncrBy(ctx, totalKey, quantity)
	shardStock := pipe.IncrBy(ctx, shardKey, quantity)
	pipe.Del(ctx, userKey)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, 0, fmt.Errorf("execute compensate failed failed: %w", err)
	}
	stock, err := shardStock.Result()
	if err != nil {
		return 0, 0, err
	}
	totalStock, totalErr := r.GetStock(ctx, seckillProductId)
	if totalErr != nil {
		totalStock = stock
	}
	return CompensateResultCompensated, totalStock, nil
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

	// 新格式：
	// 1) status:orderId:shardNo
	// 2) status:orderId:shardNo:productId:quantity:amount:productName
	// 兼容旧格式：
	// 3) status:orderId
	// 4) status:productId:quantity:amount:productName
	parts := strings.SplitN(val, ":", 7)
	if len(parts) < 1 {
		return nil, fmt.Errorf("invalid order info format: %s", val)
	}

	info := &OrderInfo{Status: parts[0], OrderId: orderId}

	if len(parts) == 2 {
		info.OrderId = parts[1]
		return info, nil
	}

	if len(parts) == 3 {
		info.OrderId = parts[1]
		shardNo, err := strconv.ParseInt(parts[2], 10, 32)
		if err == nil {
			info.ShardNo = int32(shardNo)
			return info, nil
		}
	}

	if len(parts) >= 7 {
		info.OrderId = parts[1]
		shardNo, _ := strconv.ParseInt(parts[2], 10, 32)
		info.ShardNo = int32(shardNo)
		info.ProductId, _ = strconv.ParseInt(parts[3], 10, 64)
		info.Quantity, _ = strconv.ParseInt(parts[4], 10, 64)
		info.Amount, _ = strconv.ParseInt(parts[5], 10, 64)
		info.ProductName = parts[6]
		return info, nil
	}

	// 旧完整格式：status:productId:quantity:amount:productName
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

func keyStock(spid int64) string     { return fmt.Sprintf("{%d}:sk:stock:total", spid) }
func keyShardStock(spid int64, shardNo int32) string {
	return fmt.Sprintf("{%d}:sk:stock:%d", spid, shardNo)
}
func keyShardMeta(spid int64) string { return fmt.Sprintf("{%d}:sk:stock:meta", spid) }
func keyUser(spid, uid int64) string { return fmt.Sprintf("{%d}:sk:user:%d", spid, uid) }
func keyOrder(spid int64, orderId string) string {
	return fmt.Sprintf("{%d}:sk:order:%s", spid, orderId)
}
func keyInfo(spid int64) string { return fmt.Sprintf("{%d}:sk:info", spid) }
func keyName(spid int64) string { return fmt.Sprintf("{%d}:sk:name", spid) }

// KeyUser 导出版本，供 logic 包使用
func KeyUser(spid, uid int64) string { return keyUser(spid, uid) }

// KeyOrder 导出版本，供上层记录 Redis 热状态 key 快照
func KeyOrder(spid int64, orderId string) string { return keyOrder(spid, orderId) }

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

func shardNoForUser(userId int64) int32 {
	if userId < 0 {
		userId = -userId
	}
	return int32(userId % defaultShardCount)
}

func shardProbeOrder(primary int32) []int32 {
	order := make([]int32, 0, defaultShardCount)
	seen := make(map[int32]struct{}, defaultShardCount)
	add := func(shard int32) {
		if shard < 0 {
			return
		}
		shard = shard % defaultShardCount
		if _, ok := seen[shard]; ok {
			return
		}
		seen[shard] = struct{}{}
		order = append(order, shard)
	}

	add(primary)
	for i := int32(1); i < defaultProbeShardCount; i++ {
		add(primary + i)
	}
	for shard := int32(0); shard < defaultShardCount; shard++ {
		add(shard)
	}
	return order
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


// Close 关闭连接
func (r *SeckillRedis) Close() error {
	return r.client.Close()
}
