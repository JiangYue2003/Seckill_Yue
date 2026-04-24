package svc

import (
	"context"

	"seckill-mall/seckill-service/internal/metrics"

	"github.com/zeromicro/go-zero/core/logx"
)

// MayExistSeckillProduct 使用可配置过滤器（Bloom/Cuckoo）进行秒杀商品ID预过滤。
// 软拦截策略：
// 1. 过滤器 positive 直接放行；
// 2. 过滤器 negative 触发一次 Redis 回源确认；
// 3. Redis 确认不存在则短期负缓存并拒绝。
func (s *ServiceContext) MayExistSeckillProduct(ctx context.Context, seckillProductId int64) (bool, error) {
	if s == nil || s.ProductFilter == nil || !s.ProductFilter.Enabled() {
		return true, nil
	}

	if s.ProductFilter.MayContain(seckillProductId) {
		metrics.SeckillBloomCheckTotal.WithLabelValues("positive").Inc()
		return true, nil
	}
	metrics.SeckillBloomCheckTotal.WithLabelValues("negative").Inc()

	if !s.ProductFilter.FallbackVerifyEnabled() {
		metrics.SeckillBloomRejectTotal.Inc()
		return false, nil
	}

	if s.ProductFilter.IsKnownNotExist(seckillProductId) {
		metrics.SeckillBloomFallbackVerifyTotal.WithLabelValues("miss").Inc()
		metrics.SeckillBloomRejectTotal.Inc()
		return false, nil
	}

	meta, err := s.Redis.GetSeckillProductMeta(ctx, seckillProductId)
	if err != nil {
		metrics.SeckillBloomFallbackVerifyTotal.WithLabelValues("error").Inc()
		logx.WithContext(ctx).Errorf("product filter fallback verify failed: seckillProductId=%d, err=%v", seckillProductId, err)
		// fail-open：回源异常时不阻断合法请求
		return true, err
	}
	if meta == nil {
		metrics.SeckillBloomFallbackVerifyTotal.WithLabelValues("miss").Inc()
		s.ProductFilter.MarkNotExist(seckillProductId)
		metrics.SeckillBloomRejectTotal.Inc()
		return false, nil
	}

	metrics.SeckillBloomFallbackVerifyTotal.WithLabelValues("hit").Inc()
	if s.ProductMetaCache != nil {
		s.ProductMetaCache.Upsert(meta)
	}
	s.ProductFilter.Add(meta.SeckillProductId)
	return true, nil
}
