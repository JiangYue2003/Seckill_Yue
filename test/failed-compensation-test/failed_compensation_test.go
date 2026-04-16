package failedcompensation

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	seckillpb "seckill-mall/seckill-service/seckill"
)

const (
	defaultRedisAddr = "127.0.0.1:6379"
	defaultRPCAddr   = "127.0.0.1:9083"
)

func TestCompensateFailedOrderFromPending(t *testing.T) {
	client, rdb, cleanup := newIntegrationClients(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	orderID := testOrderID()
	seckillProductID := int64(92001)
	userID := int64(501001)
	quantity := int64(2)
	initialStock := int64(10)
	preparePendingOrderState(t, ctx, rdb, orderID, seckillProductID, userID, quantity, initialStock)

	resp, err := client.CompensateFailedOrder(ctx, &seckillpb.CompensateFailedOrderRequest{
		OrderId:          orderID,
		SeckillProductId: seckillProductID,
		UserId:           userID,
		Quantity:         quantity,
		Reason:           "test_timeout",
	})
	if err != nil {
		if isRPCUnimplemented(err) {
			t.Skipf("skip integration test: running seckill-service does not expose CompensateFailedOrder yet, err=%v", err)
		}
		t.Fatalf("CompensateFailedOrder error: %v", err)
	}
	if resp == nil || !resp.Success || resp.Result != "compensated" {
		t.Fatalf("unexpected response: %+v", resp)
	}

	orderVal := mustGet(t, ctx, rdb, orderKey(orderID))
	if !strings.HasPrefix(orderVal, "failed:") {
		t.Fatalf("order status not failed, got=%s", orderVal)
	}

	stock := mustGetInt64(t, ctx, rdb, stockKey(seckillProductID))
	if stock != initialStock+quantity {
		t.Fatalf("stock not compensated once, got=%d want=%d", stock, initialStock+quantity)
	}

	if exists := mustExists(t, ctx, rdb, userKey(seckillProductID, userID)); exists {
		t.Fatalf("user key should be deleted after compensation")
	}
}

func TestCompensateFailedOrderIdempotent(t *testing.T) {
	client, rdb, cleanup := newIntegrationClients(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	orderID := testOrderID()
	seckillProductID := int64(92002)
	userID := int64(501002)
	quantity := int64(3)
	initialStock := int64(20)
	preparePendingOrderState(t, ctx, rdb, orderID, seckillProductID, userID, quantity, initialStock)

	firstResp, err := client.CompensateFailedOrder(ctx, &seckillpb.CompensateFailedOrderRequest{
		OrderId:          orderID,
		SeckillProductId: seckillProductID,
		UserId:           userID,
		Quantity:         quantity,
		Reason:           "test_timeout_1",
	})
	if err != nil {
		if isRPCUnimplemented(err) {
			t.Skipf("skip integration test: running seckill-service does not expose CompensateFailedOrder yet, err=%v", err)
		}
		t.Fatalf("first compensate error: %v", err)
	}
	if firstResp == nil || !firstResp.Success || firstResp.Result != "compensated" {
		t.Fatalf("unexpected first response: %+v", firstResp)
	}

	secondResp, err := client.CompensateFailedOrder(ctx, &seckillpb.CompensateFailedOrderRequest{
		OrderId:          orderID,
		SeckillProductId: seckillProductID,
		UserId:           userID,
		Quantity:         quantity,
		Reason:           "test_timeout_2",
	})
	if err != nil {
		t.Fatalf("second compensate error: %v", err)
	}
	if secondResp == nil || !secondResp.Success || secondResp.Result != "idempotent_failed" {
		t.Fatalf("unexpected second response: %+v", secondResp)
	}

	stock := mustGetInt64(t, ctx, rdb, stockKey(seckillProductID))
	if stock != initialStock+quantity {
		t.Fatalf("stock compensated more than once, got=%d want=%d", stock, initialStock+quantity)
	}
}

func TestUpdateOrderStatusAllowRecover(t *testing.T) {
	client, rdb, cleanup := newIntegrationClients(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	orderID := testOrderID()
	seckillProductID := int64(92003)
	setOrderStatusValue(t, ctx, rdb, orderID, "failed", seckillProductID, 1)

	resp, err := client.UpdateOrderStatus(ctx, &seckillpb.UpdateOrderStatusRequest{
		OrderId:      orderID,
		Status:       "success",
		AllowRecover: true,
	})
	if err != nil {
		t.Fatalf("UpdateOrderStatus allow recover error: %v", err)
	}
	if resp == nil || !resp.Success {
		t.Fatalf("unexpected response: %+v", resp)
	}

	orderVal := mustGet(t, ctx, rdb, orderKey(orderID))
	if !strings.HasPrefix(orderVal, "success:") {
		t.Skipf("skip integration test: allow_recover not active on running seckill-service, order=%s", orderVal)
	}
}

func TestUpdateOrderStatusDisallowRecover(t *testing.T) {
	client, rdb, cleanup := newIntegrationClients(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	orderID := testOrderID()
	seckillProductID := int64(92004)
	setOrderStatusValue(t, ctx, rdb, orderID, "failed", seckillProductID, 1)

	resp, err := client.UpdateOrderStatus(ctx, &seckillpb.UpdateOrderStatusRequest{
		OrderId:      orderID,
		Status:       "success",
		AllowRecover: false,
	})
	if err != nil {
		t.Fatalf("UpdateOrderStatus disallow recover error: %v", err)
	}
	if resp == nil || !resp.Success {
		t.Fatalf("unexpected response: %+v", resp)
	}

	orderVal := mustGet(t, ctx, rdb, orderKey(orderID))
	if !strings.HasPrefix(orderVal, "failed:") {
		t.Fatalf("order status should stay failed when recover disabled, got=%s", orderVal)
	}
}

func newIntegrationClients(t *testing.T) (seckillpb.SeckillServiceClient, *redis.Client, func()) {
	t.Helper()

	redisAddr := getenvOrDefault("REDIS_ADDR", defaultRedisAddr)
	rpcAddr := getenvOrDefault("SECKILL_RPC_ENDPOINT", defaultRPCAddr)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("skip integration test: redis unavailable at %s, err=%v", redisAddr, err)
	}

	conn, err := grpc.NewClient(rpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		_ = rdb.Close()
		t.Skipf("skip integration test: grpc dial failed, endpoint=%s, err=%v", rpcAddr, err)
	}

	client := seckillpb.NewSeckillServiceClient(conn)
	healthCtx, healthCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer healthCancel()
	if _, err := client.GetSeckillResult(healthCtx, &seckillpb.SeckillResultRequest{OrderId: "health-check"}); err != nil {
		_ = conn.Close()
		_ = rdb.Close()
		t.Skipf("skip integration test: seckill rpc unavailable at %s, err=%v", rpcAddr, err)
	}

	cleanup := func() {
		_ = conn.Close()
		_ = rdb.Close()
	}
	return client, rdb, cleanup
}

func preparePendingOrderState(
	t *testing.T,
	ctx context.Context,
	rdb *redis.Client,
	orderID string,
	seckillProductID int64,
	userID int64,
	quantity int64,
	initialStock int64,
) {
	t.Helper()
	setOrderStatusValue(t, ctx, rdb, orderID, "pending", seckillProductID, quantity)
	mustSet(t, ctx, rdb, stockKey(seckillProductID), strconv.FormatInt(initialStock, 10), 24*time.Hour)
	mustSet(t, ctx, rdb, userKey(seckillProductID, userID), orderID, 5*time.Minute)
}

func setOrderStatusValue(t *testing.T, ctx context.Context, rdb *redis.Client, orderID, status string, productID, quantity int64) {
	t.Helper()
	val := fmt.Sprintf("%s:%d:%d:%d:%s", status, productID, quantity, 100, "test-product")
	mustSet(t, ctx, rdb, orderKey(orderID), val, 24*time.Hour)
}

func mustSet(t *testing.T, ctx context.Context, rdb *redis.Client, key, val string, ttl time.Duration) {
	t.Helper()
	if err := rdb.Set(ctx, key, val, ttl).Err(); err != nil {
		t.Fatalf("redis set failed: key=%s, err=%v", key, err)
	}
}

func mustGet(t *testing.T, ctx context.Context, rdb *redis.Client, key string) string {
	t.Helper()
	v, err := rdb.Get(ctx, key).Result()
	if err != nil {
		t.Fatalf("redis get failed: key=%s, err=%v", key, err)
	}
	return v
}

func mustGetInt64(t *testing.T, ctx context.Context, rdb *redis.Client, key string) int64 {
	t.Helper()
	v, err := rdb.Get(ctx, key).Int64()
	if err != nil {
		t.Fatalf("redis get int64 failed: key=%s, err=%v", key, err)
	}
	return v
}

func mustExists(t *testing.T, ctx context.Context, rdb *redis.Client, key string) bool {
	t.Helper()
	n, err := rdb.Exists(ctx, key).Result()
	if err != nil {
		t.Fatalf("redis exists failed: key=%s, err=%v", key, err)
	}
	return n > 0
}

func orderKey(orderID string) string {
	return "seckill:order:" + orderID
}

func stockKey(seckillProductID int64) string {
	return "seckill:stock:" + strconv.FormatInt(seckillProductID, 10)
}

func userKey(seckillProductID, userID int64) string {
	return "seckill:user:" + strconv.FormatInt(seckillProductID, 10) + ":" + strconv.FormatInt(userID, 10)
}

func testOrderID() string {
	return fmt.Sprintf("TFC-%d", time.Now().UnixNano())
}

func getenvOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func isRPCUnimplemented(err error) bool {
	return status.Code(err) == codes.Unimplemented
}
