package svc

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"seckill-mall/seckill-service/internal/config"
	"seckill-mall/seckill-service/internal/metrics"
	"seckill-mall/seckill-service/internal/mq"
	"seckill-mall/seckill-service/internal/redis"

	"github.com/zeromicro/go-zero/core/logx"
)

type ServiceContext struct {
	Config           config.Config
	Redis            *redis.SeckillRedis
	AsyncProducer    *mq.AsyncProducer
	ProductMetaCache *ProductMetaCache
	ProductFilter    *ProductIDFilter
	InstanceID       string

	bgCtx    context.Context
	bgCancel context.CancelFunc
	bgWg     sync.WaitGroup
	stopOnce sync.Once
}

func NewServiceContext(c config.Config) *ServiceContext {
	// 初始化 Redis
	redisClient, err := redis.NewSeckillRedis(redis.ClientConfig{
		Host:           c.SeckillRedis.Host,
		PoolSize:       c.SeckillRedis.PoolSize,
		MinIdleConns:   c.SeckillRedis.MinIdleConns,
		DialTimeoutMs:  c.SeckillRedis.DialTimeoutMs,
		ReadTimeoutMs:  c.SeckillRedis.ReadTimeoutMs,
		WriteTimeoutMs: c.SeckillRedis.WriteTimeoutMs,
		PoolTimeoutMs:  c.SeckillRedis.PoolTimeoutMs,
	})
	if err != nil {
		logx.Errorf("failed to initialize redis: %v", err)
		panic(err)
	}

	// 初始化 RabbitMQ 同步生产者（底层引擎）
	producer, err := mq.NewProducer(c.RabbitMQ.URL, c.RabbitMQ.Exchange, c.RabbitMQ.RoutingKey)
	if err != nil {
		logx.Errorf("failed to initialize RabbitMQ producer: %v", err)
		panic(err)
	}

	// 初始化异步 MQ 生产者（上层封装，带 Channel 缓冲和后台 Worker）
	asyncProducer := mq.NewAsyncProducer(
		producer,
		c.AsyncProducer.BufferSize,
		c.AsyncProducer.WorkerCount,
		c.AsyncProducer.RetryCount,
		c.AsyncProducer.RetryInterval,
	)

	instanceID := buildInstanceID()
	productMetaCache := NewProductMetaCache(
		c.ProductMetaCache.Enabled,
		redisClient,
		c.ProductMetaCache.RefreshSeconds,
		c.ProductMetaCache.ScanCount,
	)
	productFilter := NewProductIDFilter(ProductIDFilterConfig{
		Enabled:                 c.Bloom.Enabled,
		ExpectedItems:           c.Bloom.ExpectedItems,
		FalsePositiveRate:       c.Bloom.FalsePositiveRate,
		NegativeCacheTTLSeconds: c.Bloom.NegativeCacheTTLSeconds,
		FallbackVerifyEnabled:   c.Bloom.FallbackVerifyEnabled,
	})
	ctx := &ServiceContext{
		Config:           c,
		Redis:            redisClient,
		AsyncProducer:    asyncProducer,
		ProductMetaCache: productMetaCache,
		ProductFilter:    productFilter,
		InstanceID:       instanceID,
	}

	if productMetaCache != nil && productMetaCache.Enabled() {
		if count, preloadErr := productMetaCache.Refresh(context.Background()); preloadErr != nil {
			logx.Errorf("preload product meta cache failed: %v", preloadErr)
		} else {
			logx.Infof("product meta cache preloaded: count=%d", count)
		}
		if productFilter != nil && productFilter.Enabled() {
			productFilter.Rebuild(productMetaCache.ProductIDs())
		}
		ctx.startProductMetaRefreshWorker()
	} else if productFilter != nil && productFilter.Enabled() {
		metaMap, loadErr := redisClient.LoadAllSeckillProductMeta(context.Background(), defaultMetaScanCount)
		if loadErr != nil {
			logx.Errorf("preload bloom filter from redis failed: %v", loadErr)
		} else {
			ids := make([]int64, 0, len(metaMap))
			for id := range metaMap {
				ids = append(ids, id)
			}
			productFilter.Rebuild(ids)
			logx.Infof("bloom filter preloaded without meta cache: count=%d", len(ids))
		}
	}

	if c.LocalQuota.Enabled {
		ctx.startQuotaBackgroundWorkers()
	}

	return ctx
}

func buildInstanceID() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	return fmt.Sprintf("%s:%d", host, os.Getpid())
}

func (s *ServiceContext) startQuotaBackgroundWorkers() {
	bgCtx := s.ensureBackgroundContext()

	heartbeatInterval := time.Duration(s.Config.LocalQuota.HeartbeatSeconds) * time.Second
	if heartbeatInterval <= 0 {
		heartbeatInterval = 5 * time.Second
	}

	reaperInterval := time.Duration(s.Config.LocalQuota.ReaperIntervalSeconds) * time.Second
	if reaperInterval <= 0 {
		reaperInterval = 2 * time.Second
	}

	s.bgWg.Add(2)
	go func() {
		defer s.bgWg.Done()
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-bgCtx.Done():
				return
			case <-ticker.C:
				if err := s.Redis.RenewAllActiveLeases(bgCtx, s.InstanceID, s.Config.LocalQuota.LeaseTTLSeconds); err != nil {
					metrics.SeckillQuotaLeaseRenewTotal.WithLabelValues("failed").Inc()
					logx.Errorf("renew local quota lease failed: %v", err)
				} else {
					metrics.SeckillQuotaLeaseRenewTotal.WithLabelValues("ok").Inc()
				}
			}
		}
	}()

	go func() {
		defer s.bgWg.Done()
		ticker := time.NewTicker(reaperInterval)
		defer ticker.Stop()
		for {
			select {
			case <-bgCtx.Done():
				return
			case <-ticker.C:
				reclaimed, err := s.Redis.ReapExpiredQuotaForAllProducts(bgCtx)
				if reclaimed > 0 {
					metrics.SeckillQuotaReclaimTotal.Add(float64(reclaimed))
				}
				if err != nil {
					logx.Errorf("reap expired quota failed: %v", err)
				}
			}
		}
	}()
}

func (s *ServiceContext) ensureBackgroundContext() context.Context {
	if s.bgCtx != nil {
		return s.bgCtx
	}
	bgCtx, cancel := context.WithCancel(context.Background())
	s.bgCtx = bgCtx
	s.bgCancel = cancel
	return bgCtx
}

func (s *ServiceContext) Stop() {
	s.stopOnce.Do(func() {
		logx.Info("ServiceContext stopping...")

		// 1. 停止后台 worker
		if s.bgCancel != nil {
			s.bgCancel()
			s.bgWg.Wait()
			logx.Info("Background workers stopped")
		}

		// 2. 关闭 AsyncProducer
		if s.AsyncProducer != nil {
			_ = s.AsyncProducer.Close()
		}

		// 3. 关闭 Redis 连接
		if s.Redis != nil {
			if err := s.Redis.Close(); err != nil {
				logx.Errorf("Failed to close Redis: %v", err)
			} else {
				logx.Info("Redis connection closed")
			}
		}

		logx.Info("ServiceContext stopped")
	})
}
