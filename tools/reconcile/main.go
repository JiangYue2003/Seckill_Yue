package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"gopkg.in/yaml.v3"

	"github.com/seckill-mall/reconcile-tool/reconcile"
	commonpb "seckill-mall/common/common"
	seckillpb "seckill-mall/common/seckill"

	_ "github.com/go-sql-driver/mysql"
)

const (
	defaultMySQLDSN      = "root:Zz123456@tcp(localhost:3306)/seckill_mall?charset=utf8mb4&parseTime=True&loc=Local"
	defaultRedisAddr     = "127.0.0.1:6379"
	defaultSeckillRPC    = "127.0.0.1:9083"
	defaultWindowMinutes = 15
	defaultLagSeconds    = 90
	defaultBatchSize     = 500
	defaultMaxRepair     = 200
	defaultLockKey       = "reconcile:seckill:lock"
	defaultLockTTL       = 120
)

type cliOptions struct {
	MySQLDSN      string
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	SeckillRPC    string

	OrderConfigPath   string
	SeckillConfigPath string

	WindowMinutes int
	LagSeconds    int
	BatchSize     int
	DryRun        bool
	MaxRepair     int
	LockKey       string
	LockTTL       int64
}

type orderConfigYAML struct {
	MySQL struct {
		DataSource string `yaml:"DataSource"`
	} `yaml:"MySQL"`
}

type seckillConfigYAML struct {
	ListenOn     string `yaml:"ListenOn"`
	SeckillRedis struct {
		Host string `yaml:"Host"`
	} `yaml:"SeckillRedis"`
}

type sqlRepo struct {
	db *sql.DB
}

type redisStore struct {
	client *redis.Client
}

type seckillRPCClient struct {
	conn   *grpc.ClientConn
	client seckillpb.SeckillServiceClient
}

func main() {
	opts := parseFlags()
	if err := mergeConfigFromYAML(&opts); err != nil {
		log.Fatalf("load yaml config failed: %v", err)
	}
	fillDefaults(&opts)

	if err := run(opts); err != nil {
		log.Fatalf("reconcile failed: %v", err)
	}
}

func run(opts cliOptions) error {
	now := time.Now().Unix()
	windowEnd := now - int64(opts.LagSeconds)
	windowStart := windowEnd - int64(opts.WindowMinutes*60)

	db, err := sql.Open("mysql", opts.MySQLDSN)
	if err != nil {
		return fmt.Errorf("open mysql failed: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("mysql ping failed: %w", err)
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     opts.RedisAddr,
		Password: opts.RedisPassword,
		DB:       opts.RedisDB,
	})
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis ping failed: %w", err)
	}

	conn, err := grpc.NewClient(opts.SeckillRPC, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("grpc dial failed: %w", err)
	}
	defer conn.Close()

	repo := &sqlRepo{db: db}
	store := &redisStore{client: rdb}
	client := &seckillRPCClient{
		conn:   conn,
		client: seckillpb.NewSeckillServiceClient(conn),
	}

	cfg := reconcile.Config{
		WindowStartUnix: windowStart,
		WindowEndUnix:   windowEnd,
		BatchSize:       opts.BatchSize,
		DryRun:          opts.DryRun,
		MaxRepair:       opts.MaxRepair,
		LockKey:         opts.LockKey,
		LockTTLSeconds:  opts.LockTTL,
	}

	runner, err := reconcile.NewRunner(cfg, repo, store, client, log.Printf)
	if err != nil {
		return err
	}

	log.Printf(
		"level=info msg=\"reconcile start\" window_start=%d window_end=%d dry_run=%t batch_size=%d max_repair=%d",
		windowStart,
		windowEnd,
		opts.DryRun,
		opts.BatchSize,
		opts.MaxRepair,
	)

	sum, err := runner.Run(context.Background())
	if err != nil {
		return err
	}

	log.Printf(
		"level=info msg=\"reconcile done\" locked=%t scanned=%d attempted=%d succeeded=%d failed=%d skipped_limit=%d repairable=%v manual=%v",
		sum.Locked,
		sum.ScannedOrders,
		sum.RepairAttempted,
		sum.RepairSucceeded,
		sum.RepairFailed,
		sum.RepairSkippedLimit,
		sum.RepairableAnomalyCount,
		sum.ManualAnomalyCount,
	)
	return nil
}

func parseFlags() cliOptions {
	opts := cliOptions{}
	flag.StringVar(&opts.MySQLDSN, "mysql-dsn", "", "MySQL DSN")
	flag.StringVar(&opts.RedisAddr, "redis-addr", "", "Redis address")
	flag.StringVar(&opts.RedisPassword, "redis-password", "", "Redis password")
	flag.IntVar(&opts.RedisDB, "redis-db", 0, "Redis DB index")
	flag.StringVar(&opts.SeckillRPC, "seckill-rpc-endpoint", "", "Seckill RPC endpoint")

	flag.StringVar(&opts.OrderConfigPath, "order-config", "", "Path to order-service yaml (optional)")
	flag.StringVar(&opts.SeckillConfigPath, "seckill-config", "", "Path to seckill-service yaml (optional)")

	flag.IntVar(&opts.WindowMinutes, "window-minutes", defaultWindowMinutes, "Scan window in minutes")
	flag.IntVar(&opts.LagSeconds, "lag-seconds", defaultLagSeconds, "Skip recent data by lag seconds")
	flag.IntVar(&opts.BatchSize, "batch-size", defaultBatchSize, "Batch size")
	flag.BoolVar(&opts.DryRun, "dry-run", true, "Dry-run mode")
	flag.IntVar(&opts.MaxRepair, "max-repair", defaultMaxRepair, "Max repair operations per run")
	flag.StringVar(&opts.LockKey, "lock-key", defaultLockKey, "Distributed lock key")
	flag.Int64Var(&opts.LockTTL, "lock-ttl-seconds", defaultLockTTL, "Distributed lock TTL seconds")
	flag.Parse()
	return opts
}

func mergeConfigFromYAML(opts *cliOptions) error {
	if opts.OrderConfigPath != "" {
		raw, err := os.ReadFile(opts.OrderConfigPath)
		if err != nil {
			return err
		}
		var cfg orderConfigYAML
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return err
		}
		if strings.TrimSpace(opts.MySQLDSN) == "" {
			opts.MySQLDSN = strings.TrimSpace(cfg.MySQL.DataSource)
		}
	}

	if opts.SeckillConfigPath != "" {
		raw, err := os.ReadFile(opts.SeckillConfigPath)
		if err != nil {
			return err
		}
		var cfg seckillConfigYAML
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return err
		}
		if strings.TrimSpace(opts.RedisAddr) == "" {
			opts.RedisAddr = strings.TrimSpace(cfg.SeckillRedis.Host)
		}
		if strings.TrimSpace(opts.SeckillRPC) == "" {
			opts.SeckillRPC = strings.TrimSpace(cfg.ListenOn)
		}
	}

	return nil
}

func fillDefaults(opts *cliOptions) {
	if strings.TrimSpace(opts.MySQLDSN) == "" {
		opts.MySQLDSN = getenvOrDefault("MYSQL_DSN", defaultMySQLDSN)
	}
	if strings.TrimSpace(opts.RedisAddr) == "" {
		opts.RedisAddr = getenvOrDefault("REDIS_ADDR", defaultRedisAddr)
	}
	if strings.TrimSpace(opts.SeckillRPC) == "" {
		opts.SeckillRPC = getenvOrDefault("SECKILL_RPC_ENDPOINT", defaultSeckillRPC)
	}
}

func (r *sqlRepo) ListOrders(ctx context.Context, windowStartUnix, windowEndUnix int64, limit, offset int) ([]reconcile.OrderRow, error) {
	const query = `
SELECT
  o.order_id,
  o.reservation_id,
  o.payment_id,
  o.user_id,
  o.product_id,
  o.quantity,
  o.amount,
  o.status,
  o.pay_status,
  o.created_at,
  so.seckill_product_id,
  so.quantity,
  sr.reservation_id IS NOT NULL AS reservation_found,
  sr.user_id,
  sr.product_id,
  sr.quantity,
  sr.amount,
  sr.status,
  p.payment_id IS NOT NULL AS payment_found,
  p.user_id,
  p.amount,
  p.status,
  COALESCE(cb.callback_count, 0) AS callback_count,
  COALESCE(cb.verify_pass_count, 0) AS verify_pass_count,
  COALESCE(cb.verify_fail_count, 0) AS verify_fail_count,
  COALESCE(cb.process_succeeded_count, 0) AS process_succeeded_count,
  COALESCE(cb.process_failed_count, 0) AS process_failed_count,
  COALESCE(cb.process_duplicate_count, 0) AS process_duplicate_count,
  pm.message_id IS NOT NULL AS processed_message_found,
  pm.status
FROM orders o
LEFT JOIN seckill_orders so ON so.order_id = o.order_id
LEFT JOIN seckill_reservations sr ON sr.order_id = o.order_id
LEFT JOIN payments p ON p.order_id = o.order_id
LEFT JOIN (
  SELECT
    order_id,
    COUNT(*) AS callback_count,
    SUM(CASE WHEN verify_result = 1 THEN 1 ELSE 0 END) AS verify_pass_count,
    SUM(CASE WHEN verify_result = 2 THEN 1 ELSE 0 END) AS verify_fail_count,
    SUM(CASE WHEN process_result = 1 THEN 1 ELSE 0 END) AS process_succeeded_count,
    SUM(CASE WHEN process_result = 2 THEN 1 ELSE 0 END) AS process_failed_count,
    SUM(CASE WHEN process_result = 3 THEN 1 ELSE 0 END) AS process_duplicate_count
  FROM payment_callbacks
  GROUP BY order_id
) cb ON cb.order_id = o.order_id
LEFT JOIN processed_messages pm ON pm.message_id = o.order_id AND pm.consumer_name = 'order-service.seckill-order'
WHERE o.order_type = 1
  AND o.created_at >= ?
  AND o.created_at < ?
ORDER BY o.created_at ASC, o.order_id ASC
LIMIT ?
OFFSET ?`

	rows, err := r.db.QueryContext(ctx, query, windowStartUnix, windowEndUnix, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]reconcile.OrderRow, 0, limit)
	for rows.Next() {
		var row reconcile.OrderRow
		var reservationID sql.NullString
		var paymentID sql.NullString
		var seckillProductID sql.NullInt64
		var seckillQuantity sql.NullInt64
		var reservationFound bool
		var reservationUserID sql.NullInt64
		var reservationProductID sql.NullInt64
		var reservationQuantity sql.NullInt64
		var reservationAmount sql.NullInt64
		var reservationStatus sql.NullInt64
		var paymentFound bool
		var paymentUserID sql.NullInt64
		var paymentAmount sql.NullInt64
		var paymentStatus sql.NullInt64
		var processedFound bool
		var processedStatus sql.NullInt64
		if err := rows.Scan(
			&row.OrderID,
			&reservationID,
			&paymentID,
			&row.UserID,
			&row.ProductID,
			&row.Quantity,
			&row.Amount,
			&row.Status,
			&row.PayStatus,
			&row.CreatedAt,
			&seckillProductID,
			&seckillQuantity,
			&reservationFound,
			&reservationUserID,
			&reservationProductID,
			&reservationQuantity,
			&reservationAmount,
			&reservationStatus,
			&paymentFound,
			&paymentUserID,
			&paymentAmount,
			&paymentStatus,
			&row.CallbackCount,
			&row.CallbackVerifyPassCount,
			&row.CallbackVerifyFailCount,
			&row.CallbackProcessSucceededCount,
			&row.CallbackProcessFailedCount,
			&row.CallbackProcessDuplicateCount,
			&processedFound,
			&processedStatus,
		); err != nil {
			return nil, err
		}
		if reservationID.Valid {
			row.ReservationID = reservationID.String
		}
		if paymentID.Valid {
			row.PaymentID = paymentID.String
		}
		if seckillProductID.Valid {
			row.SeckillProductID = seckillProductID.Int64
		}
		if seckillQuantity.Valid {
			row.SeckillQuantity = seckillQuantity.Int64
		}
		row.ReservationFound = reservationFound
		if reservationUserID.Valid {
			row.ReservationUserID = reservationUserID.Int64
		}
		if reservationProductID.Valid {
			row.ReservationProductID = reservationProductID.Int64
		}
		if reservationQuantity.Valid {
			row.ReservationQuantity = reservationQuantity.Int64
		}
		if reservationAmount.Valid {
			row.ReservationAmount = reservationAmount.Int64
		}
		if reservationStatus.Valid {
			row.ReservationStatus = int32(reservationStatus.Int64)
		}
		row.PaymentFound = paymentFound
		if paymentUserID.Valid {
			row.PaymentUserID = paymentUserID.Int64
		}
		if paymentAmount.Valid {
			row.PaymentAmount = paymentAmount.Int64
		}
		if paymentStatus.Valid {
			row.PaymentStatus = int32(paymentStatus.Int64)
		}
		row.ProcessedMessageFound = processedFound
		if processedStatus.Valid {
			row.ProcessedMessageStatus = int32(processedStatus.Int64)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *sqlRepo) GetStockLogCounts(ctx context.Context, orderIDs []string) (map[string]reconcile.StockLogCount, error) {
	result := make(map[string]reconcile.StockLogCount, len(orderIDs))
	if len(orderIDs) == 0 {
		return result, nil
	}

	placeholders := make([]string, 0, len(orderIDs))
	args := make([]any, 0, len(orderIDs))
	for _, id := range orderIDs {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}

	query := fmt.Sprintf(`
SELECT
  order_id,
  SUM(CASE WHEN change_type = 1 THEN 1 ELSE 0 END) AS deduct_count,
  SUM(CASE WHEN change_type = 2 THEN 1 ELSE 0 END) AS rollback_count
FROM stock_logs
WHERE order_id IN (%s)
GROUP BY order_id`, strings.Join(placeholders, ","))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var orderID string
		var count reconcile.StockLogCount
		if err := rows.Scan(&orderID, &count.DeductCount, &count.RollbackCount); err != nil {
			return nil, err
		}
		result[orderID] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *sqlRepo) GetOutboxEvents(ctx context.Context, orderIDs []string) (map[string][]reconcile.OutboxEventRow, error) {
	result := make(map[string][]reconcile.OutboxEventRow, len(orderIDs))
	if len(orderIDs) == 0 {
		return result, nil
	}

	placeholders := make([]string, 0, len(orderIDs))
	args := make([]any, 0, len(orderIDs)*2)
	for _, id := range orderIDs {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	for _, id := range orderIDs {
		args = append(args, id)
	}

	query := fmt.Sprintf(`
SELECT id, aggregate_id, event_type, status, payload_json
FROM event_outbox
WHERE aggregate_id IN (%s)
   OR JSON_UNQUOTE(JSON_EXTRACT(payload_json, '$.order_id')) IN (%s)
ORDER BY id ASC`, strings.Join(placeholders, ","), strings.Join(placeholders, ","))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id          int64
			aggregateID string
			eventType   string
			status      int32
			payloadJSON string
		)
		if err := rows.Scan(&id, &aggregateID, &eventType, &status, &payloadJSON); err != nil {
			return nil, err
		}
		orderID := aggregateID
		if eventType == "reservation.created" || eventType == "reservation.timeout.check" || eventType == "reservation.released" || eventType == "reservation.advanced" {
			// reservation aggregate_id is reservation_id; current system keeps reservation_id == order_id
			orderID = aggregateID
		}
		result[orderID] = append(result[orderID], reconcile.OutboxEventRow{
			ID:          id,
			EventType:   eventType,
			Status:      status,
			PayloadJSON: payloadJSON,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *sqlRepo) RequeueOutbox(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	placeholders := make([]string, 0, len(ids))
	args := make([]any, 0, len(ids)+1)
	for _, id := range ids {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	args = append([]any{time.Now().Unix()}, args...)

	query := fmt.Sprintf(`
UPDATE event_outbox
SET status = 0, next_retry_at = 0, last_error = '', updated_at = ?
WHERE id IN (%s) AND status IN (0, 3)`, strings.Join(placeholders, ","))

	_, err := r.db.ExecContext(ctx, query, args...)
	return err
}

func (r *redisStore) AcquireLock(ctx context.Context, key, token string, ttlSeconds int64) (bool, error) {
	if ttlSeconds <= 0 {
		ttlSeconds = defaultLockTTL
	}
	return r.client.SetNX(ctx, key, token, time.Duration(ttlSeconds)*time.Second).Result()
}

func (r *redisStore) ReleaseLock(ctx context.Context, key, token string) error {
	const script = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`
	_, err := r.client.Eval(ctx, script, []string{key}, token).Result()
	return err
}

func (r *redisStore) GetOrderStatuses(ctx context.Context, orderIDs []string) (map[string]string, error) {
	out := make(map[string]string, len(orderIDs))
	if len(orderIDs) == 0 {
		return out, nil
	}

	pipe := r.client.Pipeline()
	cmds := make(map[string][]*redis.StringCmd, len(orderIDs))
	for _, orderID := range orderIDs {
		keys := buildRedisOrderKeys(orderID)
		orderCmds := make([]*redis.StringCmd, 0, len(keys))
		for _, key := range keys {
			orderCmds = append(orderCmds, pipe.Get(ctx, key))
		}
		cmds[orderID] = orderCmds
	}
	_, err := pipe.Exec(ctx)
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}

	for orderID, orderCmds := range cmds {
		resolved := false
		for _, cmd := range orderCmds {
			val, getErr := cmd.Result()
			if errors.Is(getErr, redis.Nil) {
				continue
			}
			if getErr != nil {
				return nil, getErr
			}
			out[orderID] = parseRedisStatus(val)
			resolved = true
			break
		}
		if !resolved {
			out[orderID] = reconcile.RedisStatusMissing
		}
	}
	return out, nil
}

func (s *seckillRPCClient) UpdateOrderStatus(ctx context.Context, orderID, status string, allowRecover bool) error {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resp, err := s.client.UpdateOrderStatus(reqCtx, &seckillpb.UpdateOrderStatusRequest{
		OrderId:      orderID,
		Status:       status,
		AllowRecover: allowRecover,
	})
	if err != nil {
		return err
	}
	if resp == nil || !resp.Success {
		if resp == nil {
			return fmt.Errorf("nil response")
		}
		return fmt.Errorf("rejected: %s", resp.Message)
	}
	return nil
}

func (s *seckillRPCClient) CompensateFailedOrder(
	ctx context.Context,
	orderID string,
	seckillProductID, userID, quantity int64,
	reason string,
) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resp, err := s.client.CompensateFailedOrder(reqCtx, &seckillpb.CompensateFailedOrderRequest{
		OrderId:          orderID,
		SeckillProductId: seckillProductID,
		UserId:           userID,
		Quantity:         quantity,
		Reason:           reason,
	})
	if err != nil {
		return "", err
	}
	if resp == nil || !resp.Success {
		if resp == nil {
			return "", fmt.Errorf("nil response")
		}
		return resp.Result, fmt.Errorf("rejected: %s", resp.Message)
	}
	return resp.Result, nil
}

func (s *seckillRPCClient) AdvanceReservation(ctx context.Context, orderID string, targetStatus int32, reason string, allowRecover bool) error {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := s.client.AdvanceReservation(reqCtx, &seckillpb.AdvanceReservationRequest{
		OrderId:      orderID,
		TargetStatus: toProtoReservationStatus(targetStatus),
		Reason:       reason,
		Operator:     "reconcile",
		AllowRecover: allowRecover,
	})
	if err != nil {
		return err
	}
	if resp == nil || !resp.Success {
		if resp == nil {
			return fmt.Errorf("nil response")
		}
		return fmt.Errorf("rejected: %s", resp.Message)
	}
	return nil
}

func parseRedisStatus(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return reconcile.RedisStatusMissing
	}
	parts := strings.SplitN(value, ":", 2)
	return strings.ToLower(strings.TrimSpace(parts[0]))
}

func buildRedisOrderKeys(orderID string) []string {
	spid := parseSpidFromOrderID(orderID)
	if spid <= 0 {
		return []string{"seckill:order:" + orderID}
	}
	return []string{
		fmt.Sprintf("{%d}:sk:order:%s", spid, orderID),
		"seckill:order:" + orderID,
	}
}

func parseSpidFromOrderID(orderID string) int64 {
	s := strings.TrimPrefix(orderID, "S")
	idx := strings.Index(s, "_")
	if idx < 0 {
		return 0
	}
	spid, err := strconv.ParseInt(s[:idx], 10, 64)
	if err != nil {
		return 0
	}
	return spid
}

func toProtoReservationStatus(status int32) commonpb.ReservationStatus {
	switch status {
	case reconcile.ReservationStatusReserved:
		return commonpb.ReservationStatus_RESERVATION_STATUS_RESERVED
	case reconcile.ReservationStatusOrderCreating:
		return commonpb.ReservationStatus_RESERVATION_STATUS_ORDER_CREATING
	case reconcile.ReservationStatusOrderCreated:
		return commonpb.ReservationStatus_RESERVATION_STATUS_ORDER_CREATED
	case reconcile.ReservationStatusPaying:
		return commonpb.ReservationStatus_RESERVATION_STATUS_PAYING
	case reconcile.ReservationStatusPaid:
		return commonpb.ReservationStatus_RESERVATION_STATUS_PAID
	case reconcile.ReservationStatusConsumed:
		return commonpb.ReservationStatus_RESERVATION_STATUS_CONSUMED
	case reconcile.ReservationStatusReleased:
		return commonpb.ReservationStatus_RESERVATION_STATUS_RELEASED
	case reconcile.ReservationStatusExpired:
		return commonpb.ReservationStatus_RESERVATION_STATUS_EXPIRED
	case reconcile.ReservationStatusFailed:
		return commonpb.ReservationStatus_RESERVATION_STATUS_FAILED
	default:
		return commonpb.ReservationStatus_RESERVATION_STATUS_RESERVED
	}
}

func getenvOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func atoiEnvOrDefault(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}
