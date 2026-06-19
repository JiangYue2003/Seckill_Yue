package redis

import (
	"context"
	"fmt"
	"strings"
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

	meta, err := r.client.HGet(ctx, keyShardMeta(spid), metaFieldTotalSlots).Int64()
	if err != nil {
		t.Fatalf("get shard meta error = %v", err)
	}
	if meta != defaultShardCount {
		t.Fatalf("expected shard meta %d, got %d", defaultShardCount, meta)
	}
	soldOut, err := r.client.HGet(ctx, keyShardMeta(spid), metaFieldSoldOut).Int64()
	if err != nil {
		t.Fatalf("get sold_out flag error = %v", err)
	}
	if soldOut != 0 {
		t.Fatalf("expected sold_out=0, got %d", soldOut)
	}

	var sum int64
	for shardNo := int32(0); shardNo < defaultShardCount; shardNo++ {
		value, err := r.client.Get(ctx, keyShardStock(spid, shardNo)).Int64()
		if err != nil {
			t.Fatalf("get shard stock error: shard=%d err=%v", shardNo, err)
		}
		sum += value

		state, stateErr := r.client.HGet(ctx, keyShardState(spid), fmt.Sprintf("%d", shardNo)).Int64()
		if stateErr != nil {
			t.Fatalf("get shard state error: shard=%d err=%v", shardNo, stateErr)
		}
		wantState := int64(0)
		if value > 0 {
			wantState = 1
		}
		if state != wantState {
			t.Fatalf("expected shard state %d for shard=%d, got %d", wantState, shardNo, state)
		}
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

func TestUpdateSeckillStockMarksSoldOutWhenZero(t *testing.T) {
	r, _ := newTestSeckillRedis(t)
	ctx := context.Background()
	spid := int64(9105)

	if err := r.InitSeckillProduct(ctx, spid, 1001, 9900, "test", 16, 100, 200, 3600); err != nil {
		t.Fatalf("InitSeckillProduct() error = %v", err)
	}
	if err := r.UpdateSeckillStock(ctx, spid, 0, 3600); err != nil {
		t.Fatalf("UpdateSeckillStock() error = %v", err)
	}

	soldOut, err := r.client.HGet(ctx, keyShardMeta(spid), metaFieldSoldOut).Int64()
	if err != nil {
		t.Fatalf("get sold_out flag error = %v", err)
	}
	if soldOut != 1 {
		t.Fatalf("expected sold_out=1, got %d", soldOut)
	}

	for shardNo := int32(0); shardNo < defaultShardCount; shardNo++ {
		state, stateErr := r.client.HGet(ctx, keyShardState(spid), fmt.Sprintf("%d", shardNo)).Int64()
		if stateErr != nil {
			t.Fatalf("get shard state error: shard=%d err=%v", shardNo, stateErr)
		}
		if state != 0 {
			t.Fatalf("expected shard=%d state=0, got %d", shardNo, state)
		}
	}
}

func TestShardStockKeysUseDistinctPhysicalHashTags(t *testing.T) {
	spid := int64(9106)

	controlTag := hashTagOf(keyStock(spid))
	metaTag := hashTagOf(keyShardMeta(spid))
	stateTag := hashTagOf(keyShardState(spid))
	shard0Tag := hashTagOf(keyShardStock(spid, 0))
	shard1Tag := hashTagOf(keyShardStock(spid, 1))

	if controlTag == "" || metaTag == "" || stateTag == "" || shard0Tag == "" || shard1Tag == "" {
		t.Fatalf("expected all redis keys to contain hash tags")
	}
	if controlTag != metaTag || controlTag != stateTag {
		t.Fatalf("expected control plane keys to share one hash tag, got stock=%q meta=%q state=%q", controlTag, metaTag, stateTag)
	}
	if shard0Tag == controlTag {
		t.Fatalf("expected physical shard key to use a different hash tag from control plane, got %q", shard0Tag)
	}
	if shard0Tag == shard1Tag {
		t.Fatalf("expected physical shard keys to be distributed across tags, got shard0=%q shard1=%q", shard0Tag, shard1Tag)
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
		keyShardState(spid),
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

func hashTagOf(key string) string {
	start := strings.IndexByte(key, '{')
	if start < 0 {
		return ""
	}
	end := strings.IndexByte(key[start+1:], '}')
	if end < 0 {
		return ""
	}
	return key[start+1 : start+1+end]
}
