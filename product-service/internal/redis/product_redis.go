package redis

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// SeckillRedis 秒杀相关 Redis 操作封装
type SeckillRedis struct {
	client *redis.Client
}

// NewSeckillRedis 创建 SeckillRedis 实例
func NewSeckillRedis(host string) (*SeckillRedis, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     host,
		Password: "",
		DB:       0,
	})
	return &SeckillRedis{client: client}, nil
}

// KeyPrefixSeckillStock 秒杀库存 Key
const KeyPrefixSeckillStock = "seckill:stock:"

// KeyPrefixSeckillInfo 秒杀商品信息 Key (productId:seckillPrice)
const KeyPrefixSeckillInfo = "seckill:info:"

// KeyPrefixSeckillProductName 秒杀商品名称 Key
const KeyPrefixSeckillProductName = "seckill:product_name:"

// InitSeckillProduct 初始化秒杀商品到 Redis（活动开始前调用）
// 设置：库存、商品信息（含时间）、商品名称
func (r *SeckillRedis) InitSeckillProduct(ctx context.Context, seckillProductId, productId, seckillPrice int64, productName string, seckillStock int64, startTime, endTime int64, ttlSeconds int64) error {
	// 设置秒杀库存
	stockKey := KeyPrefixSeckillStock + strconv.FormatInt(seckillProductId, 10)
	if err := r.client.Set(ctx, stockKey, seckillStock, time.Duration(ttlSeconds)*time.Second).Err(); err != nil {
		return fmt.Errorf("设置秒杀库存失败: %w", err)
	}

	// 设置秒杀商品信息 (productId:seckillPrice:startTime:endTime)
	infoKey := KeyPrefixSeckillInfo + strconv.FormatInt(seckillProductId, 10)
	infoValue := fmt.Sprintf("%d:%d:%d:%d", productId, seckillPrice, startTime, endTime)
	if err := r.client.Set(ctx, infoKey, infoValue, time.Duration(ttlSeconds)*time.Second).Err(); err != nil {
		return fmt.Errorf("设置秒杀商品信息失败: %w", err)
	}

	// 设置秒杀商品名称
	nameKey := KeyPrefixSeckillProductName + strconv.FormatInt(seckillProductId, 10)
	if err := r.client.Set(ctx, nameKey, productName, time.Duration(ttlSeconds)*time.Second).Err(); err != nil {
		return fmt.Errorf("设置秒杀商品名称失败: %w", err)
	}

	return nil
}

// UpdateSeckillStock 更新秒杀库存（在活动进行中修改库存）
func (r *SeckillRedis) UpdateSeckillStock(ctx context.Context, seckillProductId, stock int64, ttlSeconds int64) error {
	key := KeyPrefixSeckillStock + strconv.FormatInt(seckillProductId, 10)
	return r.client.Set(ctx, key, stock, time.Duration(ttlSeconds)*time.Second).Err()
}

// UpdateSeckillInfo 更新秒杀商品信息（seckillPrice）
func (r *SeckillRedis) UpdateSeckillInfo(ctx context.Context, seckillProductId, productId, seckillPrice int64, ttlSeconds int64) error {
	key := KeyPrefixSeckillInfo + strconv.FormatInt(seckillProductId, 10)
	value := fmt.Sprintf("%d:%d", productId, seckillPrice)
	return r.client.Set(ctx, key, value, time.Duration(ttlSeconds)*time.Second).Err()
}

// DeleteSeckillProduct 删除秒杀商品 Redis 数据（活动结束后调用）
func (r *SeckillRedis) DeleteSeckillProduct(ctx context.Context, seckillProductId int64) error {
	idStr := strconv.FormatInt(seckillProductId, 10)
	keys := []string{
		KeyPrefixSeckillStock + idStr,
		KeyPrefixSeckillInfo + idStr,
		KeyPrefixSeckillProductName + idStr,
	}
	return r.client.Del(ctx, keys...).Err()
}

// ========== 普通商品缓存 ==========

const (
	KeyPrefixProductDetail = "product:detail:" // 商品详情缓存 key 前缀
	ProductCacheBaseTTL    = 3600              // 基础TTL: 1小时(秒)
	ProductCacheJitter     = 600               // 随机抖动范围: 0~600s，防雪崩
	ProductCacheNullTTL    = 60                // 空值缓存TTL: 60s，防穿透
	ProductCacheNullValue  = "null"            // 空值标记（表示DB中确认不存在）
)

// GetProductCache 读商品缓存
// 返回 (value, found, err)：found=false 表示 cache miss；value="null" 表示已确认不存在
func (r *SeckillRedis) GetProductCache(ctx context.Context, productId int64) (string, bool, error) {
	key := KeyPrefixProductDetail + strconv.FormatInt(productId, 10)
	val, err := r.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", false, nil // cache miss
	}
	if err != nil {
		return "", false, err
	}
	return val, true, nil
}

// SetProductCache 写商品缓存，TTL = BaseTTL + rand jitter，防缓存雪崩
func (r *SeckillRedis) SetProductCache(ctx context.Context, productId int64, jsonValue string) error {
	key := KeyPrefixProductDetail + strconv.FormatInt(productId, 10)
	ttl := time.Duration(ProductCacheBaseTTL+rand.Intn(ProductCacheJitter)) * time.Second
	return r.client.Set(ctx, key, jsonValue, ttl).Err()
}

// SetProductCacheNull 缓存空值标记，防缓存穿透（短TTL）
func (r *SeckillRedis) SetProductCacheNull(ctx context.Context, productId int64) error {
	key := KeyPrefixProductDetail + strconv.FormatInt(productId, 10)
	return r.client.Set(ctx, key, ProductCacheNullValue, ProductCacheNullTTL*time.Second).Err()
}

// DeleteProductCache 删除商品缓存（写操作后主动失效）
func (r *SeckillRedis) DeleteProductCache(ctx context.Context, productId int64) error {
	key := KeyPrefixProductDetail + strconv.FormatInt(productId, 10)
	return r.client.Del(ctx, key).Err()
}

// Close 关闭连接
func (r *SeckillRedis) Close() error {
	return r.client.Close()
}
