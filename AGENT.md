# AGENT.md - Seckill Mall System Guide for Human/AI Contributors

This document is the operational and architecture contract for this repository.
If you are an agent (or a new teammate), follow this file before editing code.

## 1. System Overview

Seckill Mall is a microservice system focused on high-concurrency seckill correctness and recoverability.

Core stack:
- Gateway: Gin HTTP
- Services: go-zero + gRPC + etcd
- Cache/atomic decisions: Redis + Lua
- Async pipeline: RabbitMQ
- Persistence: MySQL
- Observability: Prometheus + logs + optional tracing

Primary services:
- `gateway`: HTTP entry, JWT auth, routing, optional rate limit
- `user-service`: register/login/refresh/user profile
- `product-service`: product metadata + physical stock operations
- `seckill-service`: Redis-based seckill atomic decision and MQ enqueue
- `order-service`: MQ consumer, idempotent order persistence, timeout compensation
- `tools/reconcile`: consistency scan + repair tool

## 2. Source of Truth and Invariants

Understand these invariants before changing logic:

1. Seckill stock authority is Redis in hot path.
- Redis keys around `seckill:stock:*`, `seckill:user:*`, `seckill:order:*` are primary runtime truth.
- MySQL product stock is not the real-time authority for seckill hot path.

2. Order truth is MySQL `orders`.
- Order final status is determined by MySQL records and status transitions.
- Redis order status is a fast status mirror and can be repaired.

3. Idempotency keys:
- `order_id` is the global idempotency key for asynchronous order processing.
- `stock_logs` has unique key `(order_id, change_type)` for physical stock audit idempotency.

4. Allowed status transitions:
- Seckill redis status: usually `pending -> success` or `pending -> failed`.
- Compensation must not break monotonicity; use existing RPC/Lua guards.

## 3. High-Level Data Model

Important tables in `docs/schema.sql`:
- `orders`: order fact table
- `seckill_orders`: user-seckill relation and dedup aid
- `products`: product metadata + physical stock fields
- `stock_logs`: physical stock deduct/rollback audit

Meaning of `stock_logs` in this project:
- It is an audit and consistency signal for physical stock operations.
- It is not the only truth for seckill hot-path correctness.

## 4. Main Execution Chains

### 4.1 Seckill request chain (fast return)

1. Gateway authenticates and calls `seckill-service`.
2. `seckill-service` executes Redis Lua atomically:
- time window check
- one-user-one-order check
- stock deduct
- write minimal order status (`pending`)
3. Service asynchronously enqueues MQ messages:
- main order message
- delay/check message for timeout recovery
4. API returns quickly.

Code anchors:
- `seckill-service/internal/logic/seckill_logic.go`
- `seckill-service/internal/redis/seckill_redis.go`

### 4.2 Async persistence chain

1. `order-service` consumes seckill messages.
2. Performs idempotency check (`order_id`).
3. Batch writes `orders`, then `seckill_orders`.
4. Calls back seckill-service to update redis status to `success`.

Code anchors:
- `order-service/internal/service/order_service.go`
- `order-service/internal/batch/batch_writer.go`

### 4.3 Timeout compensation chain

1. Delay queue TTL moves message into check queue.
2. `order-service` checks if order exists in MySQL.
3. If missing, call `CompensateFailedOrder` RPC:
- atomic `pending -> failed`
- Redis stock rollback
- release user occupy key

Code anchors:
- `order-service/internal/service/order_service.go`
- `seckill-service/internal/logic/compensate_failed_order_logic.go`
- `seckill-service/internal/redis/seckill_redis.go`

## 5. Consistency Strategy (Important)

This project uses "final consistency + repair" rather than synchronous cross-system transactions.

Current consistency mechanisms:
- Redis Lua atomicity in seckill decision
- MQ at-least-once consumer with idempotent persistence
- Timeout compensation for missing DB orders
- Reconcile tool to detect and repair drift between MySQL and Redis views

Known risk window:
- Crash between "Redis success" and "MQ publish" can produce orphan `pending` state.
- Existing reconcile scans DB-windowed seckill orders; pure Redis orphan pending can be missed.
- If closing this gap is required, add pending-event replay (Redis pending set/stream) or equivalent.

## 6. Reconcile Tool Contract

`tools/reconcile` is a repair-capable consistency task.

It scans:
- seckill orders in a time window from MySQL
- matching redis order statuses
- stock log aggregate counts

It can repair:
- DB success but redis not success -> update redis status
- DB failed but redis pending/success -> trigger failed compensation

Code anchors:
- `tools/reconcile/main.go`
- `tools/reconcile/reconcile/runner.go`
- `tools/reconcile/reconcile/types.go`

## 7. Operational Runbook

Start dependencies:
- MySQL, Redis, RabbitMQ, etcd

Start services:
- gateway -> user -> product -> seckill -> order

Windows scripts:
- `scripts/start-all.ps1`
- `scripts/stop-all.ps1`

Basic health checks:
- Gateway `/health`
- Prometheus metrics ports for each service
- RabbitMQ queue depth for:
  - `seckill_order_queue`
  - `seckill_delay_queue`
  - `seckill_order_check_queue`
  - `seckill_dead_queue`

## 8. Development Rules for Agents

1. Do not break service boundaries.
- Seckill hot path must not directly use MySQL.
- Order persistence logic stays in `order-service`.

2. Keep idempotency first.
- Every async write/retry path must be idempotent by key.

3. Preserve status-machine safety.
- Avoid introducing invalid state transitions.
- If changing transitions, update compensation and reconcile rules together.

4. Prefer additive, observable changes.
- Add metrics/log labels for new branches in failure handling.

5. Update all three when touching consistency logic:
- Runtime logic
- Schema/migrations
- Reconcile detection/repair logic

## 9. Common Failure Symptoms and Likely Causes

1. Redis stock reduced but missing DB order:
- likely publish/consumer failure in async chain
- check delay/check queues, consumer status, compensation RPC logs

2. Redis `pending` not converging:
- callback `UpdateOrderStatus` not executed or failed
- or timeout compensation not triggered

3. MQ backlog growth:
- consumer down, retry storm, or DB slow writes
- inspect `order-service` consumer logs and DB latency

4. Inconsistent physical stock logs:
- normal for pure seckill hot path if no product-stock deduct is expected
- use table semantics correctly before declaring anomaly

## 10. Files You Should Read First

Architecture and flow:
- `README.md`

Seckill atomic + quota + compensation:
- `seckill-service/internal/logic/seckill_logic.go`
- `seckill-service/internal/redis/seckill_redis.go`
- `seckill-service/internal/logic/compensate_failed_order_logic.go`

Order async persistence:
- `order-service/internal/service/order_service.go`
- `order-service/internal/batch/batch_writer.go`
- `order-service/internal/mq/consumer.go`

Physical stock and stock logs:
- `product-service/internal/model/product_model.go`

Consistency repair:
- `tools/reconcile/main.go`
- `tools/reconcile/reconcile/runner.go`

Schema:
- `docs/schema.sql`

## 11. If You Need to Extend the System

Recommended sequence:
1. Define invariant and failure mode first.
2. Update protobuf/contracts.
3. Implement runtime logic with idempotency.
4. Add/adjust compensation.
5. Add/adjust reconcile anomaly checks and repairs.
6. Add benchmark/e2e test coverage.
7. Document behavior change in README/AGENT.md.

---

If this document conflicts with code behavior, code is authoritative; then update this file immediately.
