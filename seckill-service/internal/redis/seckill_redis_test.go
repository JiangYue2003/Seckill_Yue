package redis

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func newTestRedis(t *testing.T) *SeckillRedis {
	t.Helper()
	mr := miniredis.RunT(t)
	r, err := NewSeckillRedis(ClientConfig{
		Mode: "single",
		Addr: mr.Addr(),
	})
	if err != nil {
		t.Fatalf("NewSeckillRedis: %v", err)
	}
	return r
}

func initProduct(t *testing.T, r *SeckillRedis, spid int64, stock int64, startOffset, endOffset int64) {
	t.Helper()
	now := time.Now().Unix()
	if err := r.SetSeckillProductInfo(context.Background(), spid, 1001, 9900, "测试商品", now+startOffset, now+endOffset, 3600); err != nil {
		t.Fatalf("SetSeckillProductInfo: %v", err)
	}
	if err := r.InitStock(context.Background(), spid, stock); err != nil {
		t.Fatalf("InitStock: %v", err)
	}
}

func makeSeckillReq(spid, uid int64, startOffset, endOffset int64) *SeckillRequest {
	now := time.Now().Unix()
	orderID := FormatOrderId(spid, fmt.Sprintf("S%d_%d", uid, now))
	return &SeckillRequest{
		SeckillProductId: spid,
		UserId:           uid,
		OrderId:          orderID,
		Quantity:         1,
		TTL:              300,
		StartTime:        now + startOffset,
		EndTime:          now + endOffset,
		OrderStatusTTL:   86400,
	}
}

func TestKeyFormat_PhysicalShardIsolation(t *testing.T) {
	spid := int64(101)
	controlTag := hashTagOf(keyStock(spid))
	if controlTag == "" {
		t.Fatalf("expected control key to contain hash tag")
	}

	tests := []struct {
		name string
		key  string
		tag  string
	}{
		{"stock_total", keyStock(spid), controlTag},
		{"stock_meta", keyShardMeta(spid), controlTag},
		{"stock_state", keyShardState(spid), controlTag},
		{"user", keyUser(spid, 9527), controlTag},
		{"order", keyOrder(spid, "S101_abc"), controlTag},
		{"info", keyInfo(spid), controlTag},
		{"name", keyName(spid), controlTag},
	}

	for _, tc := range tests {
		if hashTagOf(tc.key) != tc.tag {
			t.Fatalf("%s: expected tag %q, got key=%q", tc.name, tc.tag, tc.key)
		}
	}

	shard0Tag := hashTagOf(keyShardStock(spid, 0))
	shard1Tag := hashTagOf(keyShardStock(spid, 1))
	if shard0Tag == controlTag {
		t.Fatalf("expected shard0 stock key to use a distinct physical tag, got %q", shard0Tag)
	}
	if shard1Tag == controlTag {
		t.Fatalf("expected shard1 stock key to use a distinct physical tag, got %q", shard1Tag)
	}
	if shard0Tag == shard1Tag {
		t.Fatalf("expected distinct physical tags for shard0 and shard1, got %q", shard0Tag)
	}

	reserveTag := hashTagOf(keyShardReserve(spid, 1, "S101_test"))
	if reserveTag != shard1Tag {
		t.Fatalf("expected shard reserve key to share shard tag=%q, got %q", shard1Tag, reserveTag)
	}
}

func TestInitStockDistributesAcrossPhysicalShards(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(102)

	if err := r.InitStock(ctx, spid, 50); err != nil {
		t.Fatal(err)
	}

	total, err := r.GetStock(ctx, spid)
	if err != nil {
		t.Fatal(err)
	}
	if total != 50 {
		t.Fatalf("expected total stock 50, got %d", total)
	}

	meta, err := r.client.HGet(ctx, keyShardMeta(spid), metaFieldTotalSlots).Int64()
	if err != nil {
		t.Fatalf("expected shard meta, err=%v", err)
	}
	if meta != defaultShardCount {
		t.Fatalf("expected shard meta %d, got %d", defaultShardCount, meta)
	}

	soldOut, err := r.client.HGet(ctx, keyShardMeta(spid), metaFieldSoldOut).Int64()
	if err != nil {
		t.Fatalf("expected sold_out flag, err=%v", err)
	}
	if soldOut != 0 {
		t.Fatalf("expected sold_out=0, got %d", soldOut)
	}

	var sum int64
	for shardNo := int32(0); shardNo < defaultShardCount; shardNo++ {
		got, err := r.client.Get(ctx, keyShardStock(spid, shardNo)).Int64()
		if err != nil {
			t.Fatalf("get shard stock failed: shard=%d err=%v", shardNo, err)
		}
		sum += got

		state, stateErr := r.client.HGet(ctx, keyShardState(spid), fmt.Sprintf("%d", shardNo)).Int64()
		if stateErr != nil {
			t.Fatalf("get shard state failed: shard=%d err=%v", shardNo, stateErr)
		}
		wantState := int64(0)
		if got > 0 {
			wantState = 1
		}
		if state != wantState {
			t.Fatalf("expected shard=%d state=%d, got %d", shardNo, wantState, state)
		}
	}
	if sum != 50 {
		t.Fatalf("expected shard stock sum 50, got %d", sum)
	}
}

func TestTryClaimUserIsIdempotent(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(103)
	orderID := FormatOrderId(spid, "S10001")

	claimed, err := r.tryClaimUser(ctx, spid, 2001, orderID, 300)
	if err != nil {
		t.Fatalf("tryClaimUser() error = %v", err)
	}
	if !claimed {
		t.Fatal("expected first claim to succeed")
	}

	claimed, err = r.tryClaimUser(ctx, spid, 2001, FormatOrderId(spid, "S10002"), 300)
	if err != nil {
		t.Fatalf("tryClaimUser() second call error = %v", err)
	}
	if claimed {
		t.Fatal("expected duplicate claim to be rejected")
	}

	userOrderID, err := r.GetUserOrderId(ctx, keyUser(spid, 2001))
	if err != nil {
		t.Fatalf("GetUserOrderId() error = %v", err)
	}
	if userOrderID != orderID {
		t.Fatalf("expected claimed order id %q, got %q", orderID, userOrderID)
	}
}

func TestRunReserveShardScriptDeductsOnlyOnce(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(104)
	initProduct(t, r, spid, 16, -10, 3600)
	orderID := FormatOrderId(spid, "S10003")
	shardNo := int32(7)

	code, stockLeft, err := r.runReserveShardScript(ctx, spid, shardNo, orderID, 1, 300)
	if err != nil {
		t.Fatalf("runReserveShardScript() error = %v", err)
	}
	if code != shardReserveResultSuccess {
		t.Fatalf("expected first reserve success, got code=%d stockLeft=%d", code, stockLeft)
	}

	beforeRetry, err := r.client.Get(ctx, keyShardStock(spid, shardNo)).Int64()
	if err != nil {
		t.Fatalf("get shard stock before retry error = %v", err)
	}
	code, stockLeft, err = r.runReserveShardScript(ctx, spid, shardNo, orderID, 1, 300)
	if err != nil {
		t.Fatalf("runReserveShardScript() retry error = %v", err)
	}
	if code != shardReserveResultAlreadyReserved {
		t.Fatalf("expected idempotent reserve result, got code=%d stockLeft=%d", code, stockLeft)
	}
	afterRetry, err := r.client.Get(ctx, keyShardStock(spid, shardNo)).Int64()
	if err != nil {
		t.Fatalf("get shard stock after retry error = %v", err)
	}
	if beforeRetry != afterRetry {
		t.Fatalf("expected stock unchanged on idempotent reserve, before=%d after=%d", beforeRetry, afterRetry)
	}
}

func TestDoSeckill_UsesFallbackShardWhenPrimaryEmpty(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(105)
	if err := r.InitStock(ctx, spid, 16); err != nil {
		t.Fatal(err)
	}
	if err := r.SetSeckillProductInfo(ctx, spid, 1001, 9900, "测试商品", time.Now().Unix()-10, time.Now().Unix()+3600, 3600); err != nil {
		t.Fatal(err)
	}

	primary := shardNoForUser(1)
	if err := r.client.Set(ctx, keyShardStock(spid, primary), 0, 0).Err(); err != nil {
		t.Fatal(err)
	}
	if err := r.client.HSet(ctx, keyShardState(spid), fmt.Sprintf("%d", primary), 0).Err(); err != nil {
		t.Fatal(err)
	}

	req := makeSeckillReq(spid, 1, -10, 3600)
	result, err := r.DoSeckill(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != LuaResultSuccess {
		t.Fatalf("expected success with fallback shard, got %+v", result)
	}
	if result.ShardNo == primary {
		t.Fatalf("expected fallback shard, got primary shard %d", primary)
	}

	orderInfo, err := r.GetOrderInfo(ctx, req.OrderId)
	if err != nil {
		t.Fatalf("GetOrderInfo() error = %v", err)
	}
	if orderInfo == nil || orderInfo.ShardNo != result.ShardNo {
		t.Fatalf("expected order info shard=%d, got %+v", result.ShardNo, orderInfo)
	}
}

func TestDoSeckill_SoldOutReleasesClaim(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(106)
	initProduct(t, r, spid, 0, -10, 3600)

	req := makeSeckillReq(spid, 9527, -10, 3600)
	result, err := r.DoSeckill(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != LuaResultStockNotEnough {
		t.Fatalf("expected stock not enough, got %+v", result)
	}

	exists, err := r.CheckUserKeyExists(ctx, keyUser(spid, req.UserId))
	if err != nil {
		t.Fatalf("CheckUserKeyExists() error = %v", err)
	}
	if exists {
		t.Fatalf("expected claim/user key to be released on sold out")
	}
}

func TestDoSeckill_AlreadyBought(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(107)
	initProduct(t, r, spid, 10, -10, 3600)

	first := makeSeckillReq(spid, 9527, -10, 3600)
	if _, err := r.DoSeckill(ctx, first); err != nil {
		t.Fatal(err)
	}

	second := makeSeckillReq(spid, 9527, -10, 3600)
	result, err := r.DoSeckill(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != LuaResultAlreadyBought {
		t.Fatalf("expected already bought, got %+v", result)
	}
}

func TestDoSeckill_NotStarted(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(108)
	initProduct(t, r, spid, 10, 3600, 7200)

	req := makeSeckillReq(spid, 9527, 3600, 7200)
	result, err := r.DoSeckill(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != LuaResultNotStarted {
		t.Fatalf("expected not started, got %+v", result)
	}
}

func TestDoSeckill_Ended(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(109)
	initProduct(t, r, spid, 10, -7200, -3600)

	req := makeSeckillReq(spid, 9527, -7200, -3600)
	result, err := r.DoSeckill(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != LuaResultEnded {
		t.Fatalf("expected ended, got %+v", result)
	}
}

func TestCompensate_CompensatedRestoresOriginalShard(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(110)
	initProduct(t, r, spid, 10, -10, 3600)

	req := makeSeckillReq(spid, 9527, -10, 3600)
	result, err := r.DoSeckill(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	stockBefore, _ := r.GetStock(ctx, spid)
	shardBefore, err := r.client.Get(ctx, keyShardStock(spid, result.ShardNo)).Int64()
	if err != nil {
		t.Fatal(err)
	}

	code, _, err := r.CompensateFailedOrder(ctx, req.OrderId, spid, req.UserId, 1, result.ShardNo, 86400)
	if err != nil {
		t.Fatal(err)
	}
	if code != CompensateResultCompensated {
		t.Fatalf("expected compensated, got %d", code)
	}

	stockAfter, _ := r.GetStock(ctx, spid)
	if stockAfter != stockBefore+1 {
		t.Fatalf("expected total stock restored by 1, before=%d after=%d", stockBefore, stockAfter)
	}
	shardAfter, err := r.client.Get(ctx, keyShardStock(spid, result.ShardNo)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	if shardAfter != shardBefore+1 {
		t.Fatalf("expected shard stock restored by 1, before=%d after=%d", shardBefore, shardAfter)
	}

	exists, err := r.CheckUserKeyExists(ctx, keyUser(spid, req.UserId))
	if err != nil {
		t.Fatalf("CheckUserKeyExists() error = %v", err)
	}
	if exists {
		t.Fatal("expected user key released after compensation")
	}

	if reserveExists := r.client.Exists(ctx, keyShardReserve(spid, result.ShardNo, req.OrderId)).Val(); reserveExists != 0 {
		t.Fatalf("expected shard reserve marker to be removed after compensation")
	}
}

func TestCompensate_AlreadyFailed(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(111)
	initProduct(t, r, spid, 10, -10, 3600)

	req := makeSeckillReq(spid, 9527, -10, 3600)
	result, err := r.DoSeckill(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := r.CompensateFailedOrder(ctx, req.OrderId, spid, req.UserId, 1, result.ShardNo, 86400); err != nil {
		t.Fatal(err)
	}
	code, _, err := r.CompensateFailedOrder(ctx, req.OrderId, spid, req.UserId, 1, result.ShardNo, 86400)
	if err != nil {
		t.Fatal(err)
	}
	if code != CompensateResultAlreadyFailed {
		t.Fatalf("expected already failed, got %d", code)
	}
}

func TestCompensate_ShardMismatchFails(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(112)
	initProduct(t, r, spid, 10, -10, 3600)

	req := makeSeckillReq(spid, 9527, -10, 3600)
	result, err := r.DoSeckill(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	wrongShard := (result.ShardNo + 1) % defaultShardCount
	code, _, err := r.CompensateFailedOrder(ctx, req.OrderId, spid, req.UserId, 1, wrongShard, 86400)
	if err == nil {
		t.Fatal("expected shard mismatch error")
	}
	if code != CompensateResultInvalidStatus {
		t.Fatalf("expected invalid status code, got %d", code)
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
