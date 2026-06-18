package redis

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const defaultShardCount = 16

// SeckillRedis 秒杀相关 Redis 操作封装
type SeckillRedis struct {
	client redis.UniversalClient
}

// ClientConfig Redis 连接配置
type ClientConfig struct {
	Mode     string   // "single"(默认) | "cluster" | "sentinel"
	Addr     string   // single/sentinel: "host:port"
	Addrs    []string // cluster/sentinel: 节点列表
	Password string
}

// NewSeckillRedis 创建 SeckillRedis 实例，支持 single/cluster/sentinel 三种模式
func NewSeckillRedis(cfg ClientConfig) (*SeckillRedis, error) {
	var client redis.UniversalClient
	switch cfg.Mode {
	case "cluster":
		addrs := cfg.Addrs
		if len(addrs) == 0 && cfg.Addr != "" {
			addrs = []string{cfg.Addr}
		}
		client = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:    addrs,
			Password: cfg.Password,
		})
	default: // "single" or ""
		addr := cfg.Addr
		if addr == "" {
			addr = "127.0.0.1:6379"
		}
		client = redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: cfg.Password,
		})
	}
	return &SeckillRedis{client: client}, nil
}

// ---- key 构造函数（hash tag 格式，兼容 Redis Cluster）----

func keyStock(spid int64) string { return fmt.Sprintf("{%d}:sk:stock:total", spid) }
func keyShardStock(spid int64, shardNo int32) string {
	return fmt.Sprintf("{%d}:sk:stock:%d", spid, shardNo)
}
func keyShardMeta(spid int64) string { return fmt.Sprintf("{%d}:sk:stock:meta", spid) }
func keyInfo(spid int64) string      { return fmt.Sprintf("{%d}:sk:info", spid) }
func keyName(spid int64) string      { return fmt.Sprintf("{%d}:sk:name", spid) }

const (
	KeyPrefixProductDetail = "product:detail:" // 商品详情缓存 key 前缀（不参与 Lua，无需 hash tag）
	ProductCacheBaseTTL    = 3600              // 基础TTL: 1小时(秒)
	ProductCacheJitter     = 600               // 随机抖动范围: 0~600s，防雪崩
	ProductCacheNullTTL    = 60                // 空值缓存TTL: 60s，防穿透
	ProductCacheNullValue  = "null"            // 空值标记（表示DB中确认不存在）
)

// InitSeckillProduct 初始化秒杀商品到 Redis（活动开始前调用）
func (r *SeckillRedis) InitSeckillProduct(ctx context.Context, seckillProductId, productId, seckillPrice int64, productName string, seckillStock int64, startTime, endTime int64, ttlSeconds int64) error {
	ttl := time.Duration(ttlSeconds) * time.Second

	pipe := r.client.TxPipeline()
	pipe.Set(ctx, keyStock(seckillProductId), seckillStock, ttl)
	pipe.Set(ctx, keyShardMeta(seckillProductId), defaultShardCount, ttl)
	for shardNo, shardStock := range splitStockIntoShards(seckillStock) {
		pipe.Set(ctx, keyShardStock(seckillProductId, int32(shardNo)), shardStock, ttl)
	}
	infoValue := fmt.Sprintf("%d:%d:%d:%d", productId, seckillPrice, startTime, endTime)
	pipe.Set(ctx, keyInfo(seckillProductId), infoValue, ttl)
	pipe.Set(ctx, keyName(seckillProductId), productName, ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("设置秒杀商品到 Redis 失败: %w", err)
	}

	return nil
}

// UpdateSeckillStock 更新秒杀库存
func (r *SeckillRedis) UpdateSeckillStock(ctx context.Context, seckillProductId, stock int64, ttlSeconds int64) error {
	ttl := time.Duration(ttlSeconds) * time.Second
	pipe := r.client.TxPipeline()
	pipe.Set(ctx, keyStock(seckillProductId), stock, ttl)
	pipe.Set(ctx, keyShardMeta(seckillProductId), defaultShardCount, ttl)
	for shardNo, shardStock := range splitStockIntoShards(stock) {
		pipe.Set(ctx, keyShardStock(seckillProductId, int32(shardNo)), shardStock, ttl)
	}
	_, err := pipe.Exec(ctx)
	return err
}

// UpdateSeckillInfo 更新秒杀商品信息
func (r *SeckillRedis) UpdateSeckillInfo(ctx context.Context, seckillProductId, productId, seckillPrice, startTime, endTime int64, ttlSeconds int64) error {
	value := fmt.Sprintf("%d:%d:%d:%d", productId, seckillPrice, startTime, endTime)
	return r.client.Set(ctx, keyInfo(seckillProductId), value, time.Duration(ttlSeconds)*time.Second).Err()
}

// DeleteSeckillProduct 删除秒杀商品 Redis 数据
func (r *SeckillRedis) DeleteSeckillProduct(ctx context.Context, seckillProductId int64) error {
	keys := []string{
		keyStock(seckillProductId),
		keyShardMeta(seckillProductId),
		keyInfo(seckillProductId),
		keyName(seckillProductId),
	}
	for shardNo := int32(0); shardNo < defaultShardCount; shardNo++ {
		keys = append(keys, keyShardStock(seckillProductId, shardNo))
	}
	return r.client.Del(ctx, keys...).Err()
}

// GetProductCache 读商品缓存
func (r *SeckillRedis) GetProductCache(ctx context.Context, productId int64) (string, bool, error) {
	key := KeyPrefixProductDetail + strconv.FormatInt(productId, 10)
	val, err := r.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return val, true, nil
}

// SetProductCache 写商品缓存，TTL 加随机抖动防雪崩
func (r *SeckillRedis) SetProductCache(ctx context.Context, productId int64, jsonValue string) error {
	key := KeyPrefixProductDetail + strconv.FormatInt(productId, 10)
	ttl := time.Duration(ProductCacheBaseTTL+rand.Intn(ProductCacheJitter)) * time.Second
	return r.client.Set(ctx, key, jsonValue, ttl).Err()
}

// SetProductCacheNull 缓存空值标记，防缓存穿透
func (r *SeckillRedis) SetProductCacheNull(ctx context.Context, productId int64) error {
	key := KeyPrefixProductDetail + strconv.FormatInt(productId, 10)
	return r.client.Set(ctx, key, ProductCacheNullValue, ProductCacheNullTTL*time.Second).Err()
}

// DeleteProductCache 删除商品缓存
func (r *SeckillRedis) DeleteProductCache(ctx context.Context, productId int64) error {
	key := KeyPrefixProductDetail + strconv.FormatInt(productId, 10)
	return r.client.Del(ctx, key).Err()
}

// Close 关闭连接
func (r *SeckillRedis) Close() error {
	return r.client.Close()
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

func parseInfoValue(value string) (productID, seckillPrice, startTime, endTime int64) {
	parts := strings.Split(value, ":")
	if len(parts) > 0 {
		productID, _ = strconv.ParseInt(parts[0], 10, 64)
	}
	if len(parts) > 1 {
		seckillPrice, _ = strconv.ParseInt(parts[1], 10, 64)
	}
	if len(parts) > 2 {
		startTime, _ = strconv.ParseInt(parts[2], 10, 64)
	}
	if len(parts) > 3 {
		endTime, _ = strconv.ParseInt(parts[3], 10, 64)
	}
	return
}
