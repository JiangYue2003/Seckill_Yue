package redis

import (
	"context"
	"fmt"
	"strconv"
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

func TestKeyFormat_HashTag(t *testing.T) {
	spid := int64(101)
	tests := []struct {
		name string
		key  string
	}{
		{"stock_total", keyStock(spid)},
		{"stock_shard", keyShardStock(spid, 3)},
		{"stock_meta", keyShardMeta(spid)},
		{"user", keyUser(spid, 9527)},
		{"order", keyOrder(spid, "S101_abc")},
		{"info", keyInfo(spid)},
		{"name", keyName(spid)},
	}

	tag := fmt.Sprintf("{%d}", spid)
	for _, tc := range tests {
		if tc.key == "" {
			t.Fatalf("%s: empty key", tc.name)
		}
		if !strings.HasPrefix(tc.key, tag) {
			t.Fatalf("%s: key %q does not start with hash tag %q", tc.name, tc.key, tag)
		}
	}
}

func TestFormatAndParseOrderId(t *testing.T) {
	cases := []struct {
		spid  int64
		rawID string
	}{
		{101, "S1234567890"},
		{999, "S9876543210"},
		{1, "Sabc"},
	}
	for _, tc := range cases {
		encoded := FormatOrderId(tc.spid, tc.rawID)
		if got := ParseSpidFromOrderId(encoded); got != tc.spid {
			t.Fatalf("spid=%d raw=%s encoded=%s parsed=%d", tc.spid, tc.rawID, encoded, got)
		}
	}
}

func TestParseSpidFromOrderId_OldFormat(t *testing.T) {
	if got := ParseSpidFromOrderId("S1234567890"); got != 0 {
		t.Fatalf("expected old format to return 0, got %d", got)
	}
}

func TestNewSeckillRedis_Single(t *testing.T) {
	r := newTestRedis(t)
	if err := r.client.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("ping failed: %v", err)
	}
}

func TestInitStockDistributesAcrossShards(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(101)

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

	meta, err := r.client.Get(ctx, keyShardMeta(spid)).Int64()
	if err != nil {
		t.Fatalf("expected shard meta, err=%v", err)
	}
	if meta != defaultShardCount {
		t.Fatalf("expected shard meta %d, got %d", defaultShardCount, meta)
	}

	var sum int64
	for shardNo := int32(0); shardNo < defaultShardCount; shardNo++ {
		got, err := r.client.Get(ctx, keyShardStock(spid, shardNo)).Int64()
		if err != nil {
			t.Fatalf("get shard stock failed: shard=%d err=%v", shardNo, err)
		}
		sum += got
	}
	if sum != 50 {
		t.Fatalf("expected shard stock sum 50, got %d", sum)
	}
}

func TestRollbackStockOnlyUpdatesTotal(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(102)

	if err := r.InitStock(ctx, spid, 100); err != nil {
		t.Fatal(err)
	}
	if err := r.RollbackStock(ctx, spid, 5); err != nil {
		t.Fatal(err)
	}

	total, _ := r.GetStock(ctx, spid)
	if total != 105 {
		t.Fatalf("expected total 105, got %d", total)
	}
}

func TestRollbackShardStockUpdatesTotalAndShard(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(103)
	shardNo := int32(7)

	if err := r.InitStock(ctx, spid, 16); err != nil {
		t.Fatal(err)
	}
	beforeShard, err := r.client.Get(ctx, keyShardStock(spid, shardNo)).Int64()
	if err != nil {
		t.Fatal(err)
	}

	if err := r.RollbackShardStock(ctx, spid, shardNo, 3); err != nil {
		t.Fatal(err)
	}

	total, _ := r.GetStock(ctx, spid)
	if total != 19 {
		t.Fatalf("expected total 19, got %d", total)
	}
	afterShard, err := r.client.Get(ctx, keyShardStock(spid, shardNo)).Int64()
	if err != nil {
		t.Fatal(err)
	}
	if afterShard != beforeShard+3 {
		t.Fatalf("expected shard stock %d, got %d", beforeShard+3, afterShard)
	}
}

func TestSetAndGetOrderInfo(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(104)
	orderID := FormatOrderId(spid, "S9999")
	info := &OrderInfo{
		Status:      OrderStatusPending,
		OrderId:     orderID,
		ShardNo:     5,
		ProductId:   1001,
		Quantity:    1,
		Amount:      9900,
		ProductName: "测试商品",
	}
	if err := r.SetOrderInfo(ctx, spid, orderID, info, 3600); err != nil {
		t.Fatal(err)
	}
	got, err := r.GetOrderInfo(ctx, orderID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected order info")
	}
	if got.Status != OrderStatusPending || got.OrderId != orderID || got.ShardNo != 5 {
		t.Fatalf("unexpected order info: %+v", got)
	}
}

func TestDoSeckill_Success(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(105)
	initProduct(t, r, spid, 10, -10, 3600)

	req := makeSeckillReq(spid, 9527, -10, 3600)
	result, err := r.DoSeckill(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != LuaResultSuccess {
		t.Fatalf("expected success, got %+v", result)
	}
	if result.ShardNo < 0 || result.ShardNo >= defaultShardCount {
		t.Fatalf("expected shard in [0,%d), got %d", defaultShardCount, result.ShardNo)
	}

	stock, _ := r.GetStock(ctx, spid)
	if stock != 9 {
		t.Fatalf("expected stock 9, got %d", stock)
	}

	rawStatus, err := r.GetOrderStatus(ctx, req.OrderId)
	if err != nil {
		t.Fatalf("GetOrderStatus() err=%v", err)
	}
	parts := strings.Split(rawStatus, ":")
	if len(parts) != 3 {
		t.Fatalf("expected status:orderId:shardNo, got %q", rawStatus)
	}
	if parts[0] != OrderStatusPending || parts[1] != req.OrderId {
		t.Fatalf("unexpected status payload: %q", rawStatus)
	}
	if shardValue, err := strconv.Atoi(parts[2]); err != nil || int32(shardValue) != result.ShardNo {
		t.Fatalf("expected shard %d in order status, got %q err=%v", result.ShardNo, parts[2], err)
	}
}

func TestDoSeckill_StockOut(t *testing.T) {
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

func TestDoSeckill_UsesFallbackShard(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(110)
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
}

func TestDoSeckill_TailDrainScansAllShards(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(111)
	if err := r.InitStock(ctx, spid, 16); err != nil {
		t.Fatal(err)
	}
	if err := r.SetSeckillProductInfo(ctx, spid, 1001, 9900, "测试商品", time.Now().Unix()-10, time.Now().Unix()+3600, 3600); err != nil {
		t.Fatal(err)
	}

	primary := shardNoForUser(42)
	probeOrder := shardProbeOrder(primary)
	for i := 0; i < defaultProbeShardCount; i++ {
		if err := r.client.Set(ctx, keyShardStock(spid, probeOrder[i]), 0, 0).Err(); err != nil {
			t.Fatal(err)
		}
	}

	if err := r.client.Set(ctx, keyStock(spid), defaultTailDrainThreshold, 0).Err(); err != nil {
		t.Fatal(err)
	}

	fallbackOutsideProbe := probeOrder[defaultProbeShardCount]
	if err := r.client.Set(ctx, keyShardStock(spid, fallbackOutsideProbe), 1, 0).Err(); err != nil {
		t.Fatal(err)
	}

	req := makeSeckillReq(spid, 42, -10, 3600)
	result, err := r.DoSeckill(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != LuaResultSuccess {
		t.Fatalf("expected tail-drain success, got %+v", result)
	}
	if result.ShardNo != fallbackOutsideProbe {
		t.Fatalf("expected shard %d, got %d", fallbackOutsideProbe, result.ShardNo)
	}
}

func TestCompensate_Compensated(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(201)
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
}

func TestCompensate_AlreadyFailed(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(202)
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

func TestCompensate_AlreadySuccess(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(203)
	initProduct(t, r, spid, 10, -10, 3600)

	req := makeSeckillReq(spid, 9527, -10, 3600)
	result, err := r.DoSeckill(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetOrderStatus(ctx, spid, req.OrderId, OrderStatusSuccess, 86400); err != nil {
		t.Fatal(err)
	}

	code, _, err := r.CompensateFailedOrder(ctx, req.OrderId, spid, req.UserId, 1, result.ShardNo, 86400)
	if err != nil {
		t.Fatal(err)
	}
	if code != CompensateResultAlreadySuccess {
		t.Fatalf("expected already success, got %d", code)
	}
}

func TestCompensate_ShardMismatchFails(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(204)
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
