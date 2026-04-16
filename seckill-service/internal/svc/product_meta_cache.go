package svc

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"seckill-mall/seckill-service/internal/redis"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	defaultMetaRefreshSeconds = 3
	defaultMetaScanCount      = 500
)

type ProductMetaCache struct {
	enabled         bool
	redisClient     *redis.SeckillRedis
	refreshInterval time.Duration
	scanCount       int64

	mu   sync.Mutex
	data atomic.Value // map[int64]*redis.SeckillProductMeta
}

func NewProductMetaCache(enabled bool, redisClient *redis.SeckillRedis, refreshSeconds, scanCount int64) *ProductMetaCache {
	if refreshSeconds <= 0 {
		refreshSeconds = defaultMetaRefreshSeconds
	}
	if scanCount <= 0 {
		scanCount = defaultMetaScanCount
	}
	c := &ProductMetaCache{
		enabled:         enabled,
		redisClient:     redisClient,
		refreshInterval: time.Duration(refreshSeconds) * time.Second,
		scanCount:       scanCount,
	}
	c.data.Store(make(map[int64]*redis.SeckillProductMeta))
	return c
}

func (c *ProductMetaCache) Enabled() bool {
	return c != nil && c.enabled
}

func (c *ProductMetaCache) RefreshInterval() time.Duration {
	if c == nil {
		return time.Duration(defaultMetaRefreshSeconds) * time.Second
	}
	return c.refreshInterval
}

func (c *ProductMetaCache) Refresh(ctx context.Context) (int, error) {
	if c == nil || !c.enabled || c.redisClient == nil {
		return 0, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	metaMap, err := c.redisClient.LoadAllSeckillProductMeta(ctx, c.scanCount)
	if err != nil {
		return 0, err
	}
	c.data.Store(metaMap)
	return len(metaMap), nil
}

func (c *ProductMetaCache) Get(seckillProductId int64) (*redis.SeckillProductMeta, bool) {
	if c == nil || !c.enabled {
		return nil, false
	}
	raw := c.data.Load()
	if raw == nil {
		return nil, false
	}
	metaMap, ok := raw.(map[int64]*redis.SeckillProductMeta)
	if !ok {
		return nil, false
	}
	meta, exists := metaMap[seckillProductId]
	if !exists || meta == nil {
		return nil, false
	}
	return meta, true
}

func (c *ProductMetaCache) Upsert(meta *redis.SeckillProductMeta) {
	if c == nil || !c.enabled || meta == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	oldRaw := c.data.Load()
	oldMap, _ := oldRaw.(map[int64]*redis.SeckillProductMeta)
	newMap := make(map[int64]*redis.SeckillProductMeta, len(oldMap)+1)
	for k, v := range oldMap {
		newMap[k] = v
	}
	newMap[meta.SeckillProductId] = meta
	c.data.Store(newMap)
}

func (s *ServiceContext) GetSeckillProductMeta(ctx context.Context, seckillProductId int64) (*redis.SeckillProductMeta, error) {
	if s.ProductMetaCache != nil {
		if meta, ok := s.ProductMetaCache.Get(seckillProductId); ok {
			return meta, nil
		}
	}

	meta, err := s.Redis.GetSeckillProductMeta(ctx, seckillProductId)
	if err != nil {
		return nil, err
	}
	if meta != nil && s.ProductMetaCache != nil {
		s.ProductMetaCache.Upsert(meta)
	}
	return meta, nil
}

func (s *ServiceContext) startProductMetaRefreshWorker() {
	if s.ProductMetaCache == nil || !s.ProductMetaCache.Enabled() {
		return
	}

	bgCtx := s.ensureBackgroundContext()
	interval := s.ProductMetaCache.RefreshInterval()
	if interval <= 0 {
		interval = time.Duration(defaultMetaRefreshSeconds) * time.Second
	}

	s.bgWg.Add(1)
	go func() {
		defer s.bgWg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-bgCtx.Done():
				return
			case <-ticker.C:
				count, err := s.ProductMetaCache.Refresh(bgCtx)
				if err != nil {
					logx.Errorf("refresh product meta cache failed: %v", err)
					continue
				}
				logx.Debugf("product meta cache refreshed: count=%d", count)
			}
		}
	}()
}
