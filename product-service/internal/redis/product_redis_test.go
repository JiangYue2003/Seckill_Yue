package redis

import (
	"context"
	"fmt"
	"testing"

	"github.com/alicebob/miniredis/v2"
)

func newTestSeckillRedis(t *testing.T) (*SeckillRedis, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	r, err := NewSeckillRedis(ClientConfig{
		Mode: "single",
		Addr: mr.Addr(),
	})
	if err != nil {
		t.Fatalf("NewSeckillRedis() error = %v", err)
	}
	return r, mr
}

func TestInitSeckillProductCreatesTotalAndShardStocks(t *testing.T) {
	r, _ := newTestSeckillRedis(t)
	ctx := context.Background()
	spid := int64(9101)

	if err := r.InitSeckillProduct(ctx, spid, 1001, 9900, "test", 50, 100, 200, 3600); err != nil {
		t.Fatalf("InitSeckillProduct() error = %v", err)
	}

	total, err := r.client.Get(ctx, keyStock(spid)).Int64()
	if err != nil {
		t.Fatalf("get total stock error = %v", err)
	}
	if total != 50 {
		t.Fatalf("expected total stock 50, got %d", total)
	}

	meta, err := r.client.Get(ctx, keyShardMeta(spid)).Int64()
	if err != nil {
		t.Fatalf("get shard meta error = %v", err)
	}
	if meta != defaultShardCount {
		t.Fatalf("expected shard meta %d, got %d", defaultShardCount, meta)
	}

	var sum int64
	for shardNo := int32(0); shardNo < defaultShardCount; shardNo++ {
		value, err := r.client.Get(ctx, keyShardStock(spid, shardNo)).Int64()
		if err != nil {
			t.Fatalf("get shard stock error: shard=%d err=%v", shardNo, err)
		}
		sum += value
	}
	if sum != 50 {
		t.Fatalf("expected shard stock sum 50, got %d", sum)
	}
}

func TestUpdateSeckillStockRebuildsShardDistribution(t *testing.T) {
	r, _ := newTestSeckillRedis(t)
	ctx := context.Background()
	spid := int64(9102)

	if err := r.InitSeckillProduct(ctx, spid, 1001, 9900, "test", 16, 100, 200, 3600); err != nil {
		t.Fatalf("InitSeckillProduct() error = %v", err)
	}

	if err := r.UpdateSeckillStock(ctx, spid, 33, 3600); err != nil {
		t.Fatalf("UpdateSeckillStock() error = %v", err)
	}

	total, err := r.client.Get(ctx, keyStock(spid)).Int64()
	if err != nil {
		t.Fatalf("get total stock error = %v", err)
	}
	if total != 33 {
		t.Fatalf("expected total stock 33, got %d", total)
	}

	var sum int64
	for shardNo := int32(0); shardNo < defaultShardCount; shardNo++ {
		value, err := r.client.Get(ctx, keyShardStock(spid, shardNo)).Int64()
		if err != nil {
			t.Fatalf("get shard stock error: shard=%d err=%v", shardNo, err)
		}
		sum += value
	}
	if sum != 33 {
		t.Fatalf("expected shard stock sum 33, got %d", sum)
	}
}

func TestDeleteSeckillProductRemovesShardKeys(t *testing.T) {
	r, _ := newTestSeckillRedis(t)
	ctx := context.Background()
	spid := int64(9103)

	if err := r.InitSeckillProduct(ctx, spid, 1001, 9900, "test", 16, 100, 200, 3600); err != nil {
		t.Fatalf("InitSeckillProduct() error = %v", err)
	}
	if err := r.DeleteSeckillProduct(ctx, spid); err != nil {
		t.Fatalf("DeleteSeckillProduct() error = %v", err)
	}

	keys := []string{
		keyStock(spid),
		keyInfo(spid),
		keyName(spid),
		keyShardMeta(spid),
	}
	for shardNo := int32(0); shardNo < defaultShardCount; shardNo++ {
		keys = append(keys, keyShardStock(spid, shardNo))
	}
	for _, key := range keys {
		if exists := r.client.Exists(ctx, key).Val(); exists != 0 {
			t.Fatalf("expected key %s to be removed", key)
		}
	}
}

func TestUpdateSeckillInfoKeepsInfoFormatWithTimeRange(t *testing.T) {
	r, _ := newTestSeckillRedis(t)
	ctx := context.Background()
	spid := int64(9104)

	if err := r.InitSeckillProduct(ctx, spid, 1001, 9900, "test", 16, 100, 200, 3600); err != nil {
		t.Fatalf("InitSeckillProduct() error = %v", err)
	}
	if err := r.UpdateSeckillInfo(ctx, spid, 1002, 8800, 300, 600, 3600); err != nil {
		t.Fatalf("UpdateSeckillInfo() error = %v", err)
	}

	value, err := r.client.Get(ctx, keyInfo(spid)).Result()
	if err != nil {
		t.Fatalf("get info error = %v", err)
	}
	expected := fmt.Sprintf("%d:%d:%d:%d", 1002, 8800, 300, 600)
	if value != expected {
		t.Fatalf("expected info %q, got %q", expected, value)
	}
}
