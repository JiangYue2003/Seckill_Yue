package redis

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

// newTestRedis 启动 miniredis 并返回连接到它的 SeckillRedis（single 模式）
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

// initProduct 初始化一个秒杀商品到 Redis
func initProduct(t *testing.T, r *SeckillRedis, spid int64, stock int64, startOffset, endOffset int64) {
	t.Helper()
	now := time.Now().Unix()
	err := r.SetSeckillProductInfo(context.Background(), spid, 1001, 9900, "测试商品", now+startOffset, now+endOffset, 3600)
	if err != nil {
		t.Fatalf("SetSeckillProductInfo: %v", err)
	}
	if err := r.InitStock(context.Background(), spid, stock); err != nil {
		t.Fatalf("InitStock: %v", err)
	}
}

// ---- key 格式测试 ----

func TestKeyFormat_HashTag(t *testing.T) {
	spid := int64(101)
	tests := []struct {
		name string
		key  string
	}{
		{"stock", keyStock(spid)},
		{"user", keyUser(spid, 9527)},
		{"order", keyOrder(spid, "S101_abc")},
		{"info", keyInfo(spid)},
		{"name", keyName(spid)},
		{"qbucket", keyQBucket(spid, "inst-A")},
		{"qlease", keyQLease(spid)},
		{"qlock", keyQReaperLock(spid)},
	}
	tag := fmt.Sprintf("{%d}", spid)
	for _, tc := range tests {
		if len(tc.key) == 0 {
			t.Errorf("%s: empty key", tc.name)
		}
		if tc.key[:len(tag)] != tag {
			t.Errorf("%s: key %q does not start with hash tag %q", tc.name, tc.key, tag)
		}
	}
}

// ---- orderId 编码/解码测试 ----

func TestFormatAndParseOrderId(t *testing.T) {
	cases := []struct {
		spid  int64
		rawId string
	}{
		{101, "S1234567890"},
		{999, "S9876543210"},
		{1, "Sabc"},
	}
	for _, c := range cases {
		encoded := FormatOrderId(c.spid, c.rawId)
		got := ParseSpidFromOrderId(encoded)
		if got != c.spid {
			t.Errorf("spid=%d rawId=%s: encoded=%s parsed=%d", c.spid, c.rawId, encoded, got)
		}
	}
}

func TestParseSpidFromOrderId_OldFormat(t *testing.T) {
	// 旧格式（不含 spid 编码）应返回 0
	if got := ParseSpidFromOrderId("S1234567890"); got != 0 {
		t.Errorf("old format should return 0, got %d", got)
	}
}

// ---- NewSeckillRedis 单节点模式 ----

func TestNewSeckillRedis_Single(t *testing.T) {
	r := newTestRedis(t)
	if err := r.client.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("ping failed: %v", err)
	}
}

// ---- InitStock / GetStock ----

func TestInitAndGetStock(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	if err := r.InitStock(ctx, 101, 500); err != nil {
		t.Fatal(err)
	}
	stock, err := r.GetStock(ctx, 101)
	if err != nil {
		t.Fatal(err)
	}
	if stock != 500 {
		t.Errorf("want 500, got %d", stock)
	}
}

func TestRollbackStock(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	_ = r.InitStock(ctx, 101, 100)
	if err := r.RollbackStock(ctx, 101, 5); err != nil {
		t.Fatal(err)
	}
	stock, _ := r.GetStock(ctx, 101)
	if stock != 105 {
		t.Errorf("want 105, got %d", stock)
	}
}

// ---- SetOrderInfo / GetOrderInfo ----

func TestSetAndGetOrderInfo(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(101)
	orderId := FormatOrderId(spid, "S9999")
	info := &OrderInfo{
		Status:      OrderStatusPending,
		OrderId:     orderId,
		ProductId:   1001,
		Quantity:    1,
		Amount:      9900,
		ProductName: "测试商品",
	}
	if err := r.SetOrderInfo(ctx, spid, orderId, info, 3600); err != nil {
		t.Fatal(err)
	}
	got, err := r.GetOrderInfo(ctx, orderId)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected order info, got nil")
	}
	if got.Status != OrderStatusPending {
		t.Errorf("want status %s, got %s", OrderStatusPending, got.Status)
	}
}

// ---- DoSeckill 测试 ----

func makeSeckillReq(spid, uid int64, stock int64, startOffset, endOffset int64) *SeckillRequest {
	now := time.Now().Unix()
	orderId := FormatOrderId(spid, fmt.Sprintf("S%d", now))
	return &SeckillRequest{
		SeckillProductId: spid,
		UserId:           uid,
		OrderId:          orderId,
		Quantity:         1,
		TTL:              300,
		StartTime:        now + startOffset,
		EndTime:          now + endOffset,
		OrderStatusTTL:   86400,
	}
}

func TestDoSeckill_Success(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(101)
	initProduct(t, r, spid, 10, -10, 3600)

	req := makeSeckillReq(spid, 9527, 10, -10, 3600)
	result, err := r.DoSeckill(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != LuaResultSuccess {
		t.Errorf("want success(%d), got %d", LuaResultSuccess, result.Code)
	}
	stock, _ := r.GetStock(ctx, spid)
	if stock != 9 {
		t.Errorf("want stock=9, got %d", stock)
	}
}

func TestDoSeckill_StockOut(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(102)
	initProduct(t, r, spid, 0, -10, 3600)

	req := makeSeckillReq(spid, 9527, 0, -10, 3600)
	result, err := r.DoSeckill(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != LuaResultStockNotEnough {
		t.Errorf("want stock_not_enough(%d), got %d", LuaResultStockNotEnough, result.Code)
	}
}

func TestDoSeckill_AlreadyBought(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(103)
	initProduct(t, r, spid, 10, -10, 3600)

	req := makeSeckillReq(spid, 9527, 10, -10, 3600)
	r.DoSeckill(ctx, req) // 第一次成功

	req2 := makeSeckillReq(spid, 9527, 10, -10, 3600) // 同一用户再次秒杀
	result, err := r.DoSeckill(ctx, req2)
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != LuaResultAlreadyBought {
		t.Errorf("want already_bought(%d), got %d", LuaResultAlreadyBought, result.Code)
	}
}

func TestDoSeckill_NotStarted(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(104)
	initProduct(t, r, spid, 10, 3600, 7200) // 活动未开始

	req := makeSeckillReq(spid, 9527, 10, 3600, 7200)
	result, err := r.DoSeckill(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != LuaResultNotStarted {
		t.Errorf("want not_started(%d), got %d", LuaResultNotStarted, result.Code)
	}
}

func TestDoSeckill_Ended(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(105)
	initProduct(t, r, spid, 10, -7200, -3600) // 活动已结束

	req := makeSeckillReq(spid, 9527, 10, -7200, -3600)
	result, err := r.DoSeckill(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != LuaResultEnded {
		t.Errorf("want ended(%d), got %d", LuaResultEnded, result.Code)
	}
}

// ---- CompensateFailedOrder 测试 ----

func TestCompensate_Compensated(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(201)
	initProduct(t, r, spid, 10, -10, 3600)

	req := makeSeckillReq(spid, 9527, 10, -10, 3600)
	r.DoSeckill(ctx, req)

	stockBefore, _ := r.GetStock(ctx, spid)
	code, _, err := r.CompensateFailedOrder(ctx, req.OrderId, spid, req.UserId, 1, 86400)
	if err != nil {
		t.Fatal(err)
	}
	if code != CompensateResultCompensated {
		t.Errorf("want compensated(%d), got %d", CompensateResultCompensated, code)
	}
	stockAfter, _ := r.GetStock(ctx, spid)
	if stockAfter != stockBefore+1 {
		t.Errorf("stock should increase by 1: before=%d after=%d", stockBefore, stockAfter)
	}
}

func TestCompensate_AlreadyFailed(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(202)
	initProduct(t, r, spid, 10, -10, 3600)

	req := makeSeckillReq(spid, 9527, 10, -10, 3600)
	r.DoSeckill(ctx, req)
	r.CompensateFailedOrder(ctx, req.OrderId, spid, req.UserId, 1, 86400) // 第一次

	code, _, err := r.CompensateFailedOrder(ctx, req.OrderId, spid, req.UserId, 1, 86400) // 第二次
	if err != nil {
		t.Fatal(err)
	}
	if code != CompensateResultAlreadyFailed {
		t.Errorf("want already_failed(%d), got %d", CompensateResultAlreadyFailed, code)
	}
}

func TestCompensate_AlreadySuccess(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(203)
	initProduct(t, r, spid, 10, -10, 3600)

	req := makeSeckillReq(spid, 9527, 10, -10, 3600)
	r.DoSeckill(ctx, req)
	// 手动将订单状态设为 success
	r.SetOrderStatus(ctx, spid, req.OrderId, OrderStatusSuccess, 86400)

	code, _, err := r.CompensateFailedOrder(ctx, req.OrderId, spid, req.UserId, 1, 86400)
	if err != nil {
		t.Fatal(err)
	}
	if code != CompensateResultAlreadySuccess {
		t.Errorf("want already_success(%d), got %d", CompensateResultAlreadySuccess, code)
	}
}

// ---- EnsureQuota / DoSeckillWithQuota / ReapExpiredQuota 测试 ----

func TestEnsureQuota_Allocates(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(301)
	_ = r.InitStock(ctx, spid, 1000)

	allocated, bucket, err := r.EnsureQuota(ctx, spid, "inst-A", 200, 30)
	if err != nil {
		t.Fatal(err)
	}
	if allocated != 200 {
		t.Errorf("want allocated=200, got %d", allocated)
	}
	if bucket != 200 {
		t.Errorf("want bucket=200, got %d", bucket)
	}
	stock, _ := r.GetStock(ctx, spid)
	if stock != 800 {
		t.Errorf("want global stock=800, got %d", stock)
	}
}

func TestEnsureQuota_GlobalEmpty(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(302)
	_ = r.InitStock(ctx, spid, 0)

	allocated, _, err := r.EnsureQuota(ctx, spid, "inst-A", 200, 30)
	if err != nil {
		t.Fatal(err)
	}
	if allocated != 0 {
		t.Errorf("want allocated=0, got %d", allocated)
	}
}

func TestDoSeckillWithQuota_Success(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(303)
	initProduct(t, r, spid, 1000, -10, 3600)
	r.EnsureQuota(ctx, spid, "inst-A", 200, 30)

	req := makeSeckillReq(spid, 9527, 1000, -10, 3600)
	result, err := r.DoSeckillWithQuota(ctx, req, "inst-A")
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != LuaResultSuccess {
		t.Errorf("want success(%d), got %d", LuaResultSuccess, result.Code)
	}
}

func TestDoSeckillWithQuota_BucketEmpty(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(304)
	initProduct(t, r, spid, 1000, -10, 3600)
	// 不分配配额，桶为空

	req := makeSeckillReq(spid, 9527, 1000, -10, 3600)
	result, err := r.DoSeckillWithQuota(ctx, req, "inst-A")
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != LuaResultStockNotEnough {
		t.Errorf("want stock_not_enough(%d), got %d", LuaResultStockNotEnough, result.Code)
	}
}

func TestReapExpiredQuota(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()
	spid := int64(401)
	_ = r.InitStock(ctx, spid, 1000)

	// 分配配额，TTL=1s（租约 score = now+1）
	r.EnsureQuota(ctx, spid, "inst-A", 200, 1)

	// 等待租约真实过期（score < now）
	time.Sleep(2 * time.Second)

	reclaimed, err := r.ReapExpiredQuotaForProduct(ctx, spid)
	if err != nil {
		t.Fatal(err)
	}
	if reclaimed != 200 {
		t.Errorf("want reclaimed=200, got %d", reclaimed)
	}
	stock, _ := r.GetStock(ctx, spid)
	if stock != 1000 {
		t.Errorf("want stock restored to 1000, got %d", stock)
	}
}

func TestReapAllProducts_UsesLocalStock(t *testing.T) {
	r := newTestRedis(t)
	ctx := context.Background()

	// 初始化两个商品，分配配额，TTL=1s
	for _, spid := range []int64{501, 502} {
		_ = r.InitStock(ctx, spid, 500)
		r.EnsureQuota(ctx, spid, "inst-A", 100, 1)
		r.localStock.Store(spid, nil)
	}

	time.Sleep(2 * time.Second)

	total, err := r.ReapExpiredQuotaForAllProducts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if total != 200 {
		t.Errorf("want total reclaimed=200, got %d", total)
	}
}
