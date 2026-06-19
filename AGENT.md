# AGENT.md - Seckill Mall Contributor Guide

This file is the current operating contract for humans and coding agents.
Read code first, but use this document to avoid stepping into old architecture assumptions.

## 1. Current Reality

The repo is no longer a RocketMQ or single-stock-key seckill system.
The active runtime model is:

- `RabbitMQ` only
- `Redis` physical stock sharding in the hot path
- `MySQL` reservation/order/payment/outbox facts
- direct `gRPC` benchmark as the primary performance baseline

Primary services:

- `gateway`: HTTP entry, JWT auth, routing, optional rate limit
- `user-service`: account and token lifecycle
- `product-service`: product CRUD, seckill product setup, source-time stock sharding, Redis preload
- `seckill-service`: synchronous seckill admission, Redis hot path, reservation ledger, outbox creation
- `order-service`: RabbitMQ consumers, order transaction persistence, timeout compensation, payment domain
- `tools/reconcile`: drift scan and repair

## 2. Source of Truth and Invariants

Understand these before touching code:

1. Redis is the runtime stock authority for the seckill hot path.
- Total stock and shard stock are runtime truth for admission.
- Physical slot keys and reserve markers are part of the real hot path now.

2. `shard_no` is a required cross-service invariant.
- A successful reserve chooses one concrete shard.
- That `shard_no` must flow through reservation, MQ payload, order rows, payment progression, and compensation.
- Compensation must return stock to the original shard, not a random shard.

3. MySQL is the final business fact source.
- Redis hot status is a mirror.
- Final truth lives in:
  - `seckill_reservations`
  - `orders`
  - `seckill_orders`
  - `payments`
  - `payment_callbacks`
  - `event_outbox`
  - `processed_messages`

4. `product-service` owns source-time stock splitting.
- `seckill_stock_shards` records the physical shard layout.
- This does not mean seckill order creation synchronously decrements DB shard rows in the hot path.

5. RabbitMQ delivery is not the only reliability mechanism.
- Outbox is the durable emission boundary.
- Consumers are at-least-once and must stay idempotent.

## 3. Important Tables

Read these in `docs/schema.sql`:

- `seckill_stock_shards`
- `seckill_reservations`
- `orders`
- `seckill_orders`
- `payments`
- `payment_callbacks`
- `event_outbox`
- `processed_messages`

## 4. Main Execution Chains

### 4.1 Seckill synchronous chain

Current flow:

1. `gateway` may call `seckill-service`, but benchmark traffic often calls `seckill-service` directly.
2. `seckill-service` performs:
   - product metadata/Bloom filtering
   - optional local quota/local pre-decrement
   - Redis user claim
   - shard probe order selection
   - single-shard reserve Lua
   - finalize Lua on total stock + hot status + shard state
3. After Redis success, `seckill-service` synchronously persists:
   - `seckill_reservations`
   - `event_outbox`
4. Outbox publishing emits:
   - `reservation.created`
   - `reservation.timeout.check`

Code anchors:

- `seckill-service/internal/logic/seckill_logic.go`
- `seckill-service/internal/redis/seckill_redis.go`
- `seckill-service/internal/model/reservation_ledger.go`

### 4.2 Order async persistence chain

Current flow:

1. `order-service` consumes `reservation.created`
2. It uses `processed_messages` for idempotency
3. It transactionally persists:
   - `orders`
   - `seckill_orders`
   - reservation status projection
   - `order_status_logs`
   - `event_outbox(order.created)`
4. Payment subdomain later emits:
   - `payment.requested`
   - `payment.succeeded`
   - `order.completed`
5. Payment success updates:
   - reservation advancement
   - Redis hot order status `pending -> success`

Code anchors:

- `order-service/internal/service/order_service.go`
- `order-service/internal/model/seckill_order_tx_manager.go`
- `order-service/internal/model/payment_ledger.go`
- `order-service/internal/mq/consumer.go`

### 4.3 Timeout compensation chain

Current flow:

1. Delay event enters RabbitMQ delay/check path through `TTL + DLX`
2. `order-service` checks whether the order exists in MySQL
3. If not, it calls `CompensateFailedOrder`
4. `seckill-service` verifies shard match and compensates:
   - `pending -> failed`
   - restore total stock
   - restore original shard stock
   - release user claim
   - clear reserve marker
   - release/fail reservation fact

Code anchors:

- `order-service/internal/service/order_service.go`
- `seckill-service/internal/logic/compensate_failed_order_logic.go`
- `seckill-service/internal/redis/seckill_redis.go`

## 5. Consistency Strategy

This repo uses final consistency with durable facts and repair, not synchronous cross-system transactions.

Key mechanisms:

- Redis atomic reserve/finalize on the hot path
- reservation and outbox persisted in the same DB transaction
- RabbitMQ consumer idempotency through `processed_messages`
- timeout compensation when DB order fact is missing
- reconcile tooling for MySQL/Redis drift

If you change consistency logic, update all of these together:

- runtime hot path
- schema or table usage
- compensation behavior
- reconcile detection/repair
- docs

## 6. Operational Notes

Runtime dependencies:

- `etcd`
- `MySQL`
- `Redis`
- `RabbitMQ`

`scripts/start-all.ps1`:

- starts service processes only
- does not boot infra
- defaults to `2` replicas for `seckill-service` and `order-service`
- keeps only one `gateway` unless you start another manually

Useful checks:

- `gateway /health`
- gRPC ports `9081`-`9084`
- replica ports `19083`, `19084`
- RabbitMQ queue depth:
  - `seckill_order_queue`
  - `seckill_order_check_queue`
  - `seckill_dead_queue`

## 7. Development Rules

1. Do not reintroduce old assumptions.
- `seckill-service` now legitimately uses MySQL for reservation/outbox persistence.
- Do not “fix” that back into a Redis-only service by assumption.

2. Keep `shard_no` end-to-end.
- Any new event or compensation path touching seckill stock must preserve shard identity.

3. Keep idempotency first.
- Consumer retries, callbacks, and status updates must stay repeatable.

4. Preserve status-machine monotonicity.
- Hot status and reservation/order/payment states must not move backward without an explicit allowed recovery rule.

5. Avoid undocumented fixture assumptions.
- Several older test helpers still write compatibility Redis keys directly.
- Do not infer the production hot path only from those helpers.

## 8. Common Failure Symptoms

1. Redis stock drops but DB order is missing.
- likely issue in outbox publish, consumer handling, or timeout compensation

2. Orders remain `pending`.
- payment progression did not complete
- or timeout check path did not converge

3. Compensation fails with shard mismatch.
- some path lost or corrupted `shard_no`

4. Benchmark results look too low or too noisy.
- confirm you are using direct gRPC benchmark, not gateway comparison path
- confirm replica ports and request timeout settings

## 9. Files To Read First

- `README.md`
- `scripts/start-all.ps1`
- `docs/schema.sql`
- `product-service/internal/model/product_model.go`
- `product-service/internal/redis/product_redis.go`
- `seckill-service/internal/logic/seckill_logic.go`
- `seckill-service/internal/redis/seckill_redis.go`
- `seckill-service/internal/model/reservation_ledger.go`
- `order-service/internal/service/order_service.go`
- `order-service/internal/model/seckill_order_tx_manager.go`
- `order-service/internal/model/payment_ledger.go`
- `tools/reconcile/reconcile/runner.go`

## 10. When Extending the System

Recommended sequence:

1. State the invariant you are changing
2. Confirm whether `shard_no`, outbox, or compensation semantics are affected
3. Update protobuf/contracts if needed
4. Implement runtime logic
5. Add or update compensation/reconcile
6. Verify with focused tests or benchmark path
7. Update docs immediately

If this document conflicts with code, code is authoritative. Then fix this document in the same task.
